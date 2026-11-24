package repository

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"yingce/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPublishCanvasShareSnapshotAtomicAndRejectsChangedSource(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "share.db")+"?_foreign_keys=on"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Resource{}, &model.ResourceDeletionJob{}, &model.CanvasProject{}, &model.CanvasShare{}, &model.CanvasShareSnapshot{}, &model.CanvasShareSnapshotResource{}); err != nil {
		t.Fatal(err)
	}
	project := model.CanvasProject{ID: "canvas", UserID: "owner", Revision: 3, PayloadJSON: `{"nodes":[]}`}
	share := model.CanvasShare{ID: "share", UserID: "owner", ProjectID: "canvas", TokenHash: "hash", Enabled: true}
	resource := model.Resource{ID: "image", UserID: "owner", Status: model.ResourceStatusReady, Provider: "local", ObjectKey: "image.png", StorageSettingID: "storage-v1", PlaybackObjectKey: "playback.mp4"}
	for _, item := range []any{&project, &share, &resource} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := New(db)
	snapshot := model.CanvasShareSnapshot{ID: "snapshot-1", ShareID: "share", UserID: "owner", CanvasID: "canvas", SourceRevision: 3, PayloadJSON: project.PayloadJSON}
	if err := repo.PublishCanvasShareSnapshot("owner", &snapshot, []string{"image", "image"}); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&share, "id = ?", "share").Error; err != nil || share.CurrentSnapshotID == nil || *share.CurrentSnapshotID != snapshot.ID {
		t.Fatalf("share pointer: %#v, %v", share, err)
	}
	var count int64
	if err := db.Model(&model.CanvasShareSnapshotResource{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("pins=%d, err=%v", count, err)
	}
	if err := db.Model(&resource).Updates(map[string]any{"object_key": "changed.png", "playback_object_key": "changed.mp4", "storage_setting_id": "storage-v2"}).Error; err != nil {
		t.Fatal(err)
	}
	var pin model.CanvasShareSnapshotResource
	if err := db.First(&pin, "snapshot_id = ? AND resource_id = ?", snapshot.ID, resource.ID).Error; err != nil {
		t.Fatal(err)
	}
	var frozen model.Resource
	if err := json.Unmarshal([]byte(pin.ResourceJSON), &frozen); err != nil || frozen.ObjectKey != "image.png" || frozen.PlaybackObjectKey != "playback.mp4" || pin.StorageSettingID != "storage-v1" {
		t.Fatalf("resource metadata not frozen: %#v, pin=%#v, err=%v", frozen, pin, err)
	}
	for _, check := range []struct {
		resource *model.Resource
		want     bool
	}{
		{&model.Resource{ID: "other-user-id", Provider: "", ObjectKey: "image.png"}, true},
		{&model.Resource{Provider: "local", ObjectKey: "changed.png"}, false},
		{&model.Resource{Provider: "local", Bucket: "different", ObjectKey: "image.png"}, true},
		{nil, false},
	} {
		got, err := repo.CanvasShareReferencesObject(check.resource)
		if err != nil || got != check.want {
			t.Fatalf("object protection = %v, want %v; %v", got, check.want, err)
		}
	}
	if err := db.Delete(&resource).Error; err != nil {
		t.Fatalf("owner cannot delete resource record: %v", err)
	}
	if pinned, err := repo.CanvasShareReferencesObject(&frozen); err != nil || !pinned {
		t.Fatalf("snapshot lost its independent object reference: pinned=%v, err=%v", pinned, err)
	}
	if err := db.Model(&project).Update("revision", 4).Error; err != nil {
		t.Fatal(err)
	}
	changed := snapshot
	changed.ID = "snapshot-2"
	if err := repo.PublishCanvasShareSnapshot("owner", &changed, []string{"image"}); !errors.Is(err, ErrShareSnapshotChanged) {
		t.Fatalf("expected stale rejection: %v", err)
	}
	if err := db.Model(&model.CanvasShareSnapshot{}).Count(&count).Error; err != nil || count != 1 {
		t.Fatalf("partial snapshot persisted: %d %v", count, err)
	}
	if err := db.First(&share, "id = ?", "share").Error; err != nil || share.CurrentSnapshotID == nil || *share.CurrentSnapshotID != snapshot.ID {
		t.Fatalf("old share lost: %#v, %v", share, err)
	}
}

func TestPublishCanvasShareSnapshotRejectsPendingPhysicalDeletion(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "share.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Resource{}, &model.ResourceDeletionJob{}, &model.CanvasProject{}, &model.CanvasShare{}, &model.CanvasShareSnapshot{}, &model.CanvasShareSnapshotResource{}); err != nil {
		t.Fatal(err)
	}
	for _, item := range []any{&model.CanvasProject{ID: "canvas", UserID: "owner", Revision: 1, PayloadJSON: `{}`}, &model.CanvasShare{ID: "share", UserID: "owner", ProjectID: "canvas", TokenHash: "hash", Enabled: true}, &model.Resource{ID: "image", UserID: "owner", Status: model.ResourceStatusReady, Provider: "local", ObjectKey: "image.png"}, &model.ResourceDeletionJob{ID: "job", Provider: "", ObjectKey: "image.png"}} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	snapshot := &model.CanvasShareSnapshot{ID: "snapshot", ShareID: "share", UserID: "owner", CanvasID: "canvas", SourceRevision: 1, PayloadJSON: `{}`}
	if err := New(db).PublishCanvasShareSnapshot("owner", snapshot, []string{"image"}); !errors.Is(err, ErrShareSnapshotResourceUnavailable) {
		t.Fatalf("expected deletion conflict: %v", err)
	}
	var count int64
	if err := db.Model(&model.CanvasShareSnapshot{}).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("partial snapshot persisted: %d %v", count, err)
	}
}
