package app

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"yingce/backend/internal/model"
)

func TestFrozenResourcePlaybackDoesNotRequireSourceRow(t *testing.T) {
	svc, _, dataDir := newResourceDeletionTestService(t)
	path := filepath.Join(dataDir, playbackDirName, "saved.mp4")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil { t.Fatal(err) }
	if err := os.WriteFile(path, []byte("playback"), 0o640); err != nil { t.Fatal(err) }
	resource := &model.Resource{ID:"deleted-source", UserID:"owner", Provider:"local", Status:model.ResourceStatusReady, PlaybackStatus:model.PlaybackStatusReady, PlaybackObjectKey:"saved.mp4"}
	stream, err := svc.openResourcePlayback(resource)
	if err != nil { t.Fatal(err) }
	defer stream.Body.Close()
	bytes, err := io.ReadAll(stream.Body)
	if err != nil || string(bytes) != "playback" { t.Fatalf("playback: %q %v",bytes,err) }
}

func TestResourceDeletionWorkerDefersSharedSnapshotObject(t *testing.T) {
	svc, db, dataDir := newResourceDeletionTestService(t)
	key := "users/owner/image/shared.png"
	path := filepath.Join(dataDir, "resources", filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("image"), 0o640); err != nil {
		t.Fatal(err)
	}
	snapshot := model.CanvasShareSnapshot{ID: "snapshot", ShareID: "share", UserID: "owner", CanvasID: "canvas", PayloadJSON: `{}`, CreatedAt: time.Now()}
	pin := model.CanvasShareSnapshotResource{SnapshotID: snapshot.ID, ResourceID: "removed-source", ResourceJSON: `{}`, Provider: "local", ObjectKey: key}
	job := model.ResourceDeletionJob{ID: "job", UserID: "owner", ResourceID: pin.ResourceID, Provider: "local", ObjectKey: key, Status: model.ResourceDeletionStatusPending, NextAttemptAt: time.Now().Add(-time.Second)}
	for _, item := range []any{&snapshot, &pin, &job} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc.drainResourceDeletionJobs(1)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("shared object was removed: %v", err)
	}
	if err := db.First(&job, "id = ?", "job").Error; err != nil || job.Attempts == 0 {
		t.Fatalf("job not retained for retry: %#v, %v", job, err)
	}
	if err := db.Delete(&pin).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&job).Update("next_attempt_at", time.Now().Add(-time.Second)).Error; err != nil {
		t.Fatal(err)
	}
	svc.drainResourceDeletionJobs(1)
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("unreferenced object not removed: %v", err)
	}
}

func TestDeleteFreshResourcePreservesSharedSnapshot(t *testing.T) {
	svc, db, dataDir := newResourceDeletionTestService(t)
	key := "users/owner/image/fresh.png"
	path := filepath.Join(dataDir, "resources", filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("image"), 0o640); err != nil {
		t.Fatal(err)
	}
	resource := model.Resource{ID: "fresh", UserID: "owner", Provider: "local", ObjectKey: key, Status: model.ResourceStatusReady}
	snapshot := model.CanvasShareSnapshot{ID: "snapshot", ShareID: "share", UserID: "owner", CanvasID: "canvas", PayloadJSON: `{}`, CreatedAt: time.Now()}
	pin := model.CanvasShareSnapshotResource{SnapshotID: snapshot.ID, ResourceID: resource.ID, ResourceJSON: `{}`, Provider: "local", ObjectKey: key}
	for _, item := range []any{&resource, &snapshot, &pin} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := svc.deleteFreshResource(&resource); err == nil {
		t.Fatal("removed a shared fresh resource")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("shared file was removed: %v", err)
	}
	if err := db.First(&model.Resource{}, "id = ?", resource.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&pin).Error; err != nil {
		t.Fatal(err)
	}
	if err := svc.deleteFreshResource(&resource); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("unreferenced file not removed: %v", err)
	}
}

func TestOffloadLocalCleanupPreservesSharedSnapshot(t *testing.T) {
	svc, db, dataDir := newResourceDeletionTestService(t)
	key := "users/owner/video/offloaded.mp4"
	path := filepath.Join(dataDir, "resources", filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("video"), 0o640); err != nil {
		t.Fatal(err)
	}
	snapshot := model.CanvasShareSnapshot{ID: "snapshot", ShareID: "share", UserID: "owner", CanvasID: "canvas", PayloadJSON: `{}`, CreatedAt: time.Now()}
	pin := model.CanvasShareSnapshotResource{SnapshotID: snapshot.ID, ResourceID: "old-local", ResourceJSON: `{}`, Provider: "local", ObjectKey: key}
	for _, item := range []any{&snapshot, &pin} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	if svc.removeUnreferencedLocalObject(key, "") {
		t.Fatal("removed offloaded file held by share")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&pin).Error; err != nil {
		t.Fatal(err)
	}
	if !svc.removeUnreferencedLocalObject(key, "") {
		t.Fatal("did not remove unreferenced local file")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file still exists: %v", err)
	}
}

func TestDeleteFreshResourcePreservesFeaturedVideo(t *testing.T) {
	svc, db, dataDir := newResourceDeletionTestService(t)
	key := "users/owner/video/featured.mp4"
	path := filepath.Join(dataDir, "resources", filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil { t.Fatal(err) }
	if err := os.WriteFile(path, []byte("video"), 0o640); err != nil { t.Fatal(err) }
	resource := model.Resource{ID:"video", UserID:"owner", Kind:"video", Provider:"local", ObjectKey:key, Status:model.ResourceStatusReady}
	snapshot := model.CanvasShareSnapshot{ID:"snapshot", ShareID:"share", UserID:"owner", CanvasID:"canvas", PayloadJSON:`{}`, CreatedAt:time.Now()}
	featured := model.FeaturedCanvas{ID:"featured", Title:"作品", CreatedBy:"owner", VideoResourceID:resource.ID, SnapshotID:snapshot.ID}
	for _, item := range []any{&resource,&snapshot,&featured} { if err := db.Create(item).Error; err != nil { t.Fatal(err) } }
	if err := svc.deleteFreshResource(&resource); err == nil { t.Fatal("deleted video used by featured entry") }
	if _, err := os.Stat(path); err != nil { t.Fatalf("featured video removed: %v",err) }
	if err := db.Delete(&featured).Error; err != nil { t.Fatal(err) }
	if err := svc.deleteFreshResource(&resource); err != nil { t.Fatal(err) }
	if _, err := os.Stat(path); !os.IsNotExist(err) { t.Fatalf("unreferenced video not removed: %v",err) }
}
