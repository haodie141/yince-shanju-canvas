package canvas

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCloneCanvasShareCreatesRecipientCanvasAndRejectsIncomplete(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "clone-service.db")+"?_foreign_keys=on"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	token := strings.Repeat("a", 43)
	snapshotID := "snapshot"
	currentID := snapshotID
	share := model.CanvasShare{ID: "share", UserID: "owner", ProjectID: "source", TokenHash: canvasShareTokenHash(token), Enabled: true, AllowCopy: true, CurrentSnapshotID: &currentID}
	payload := `{"id":"source","nodes":[{"id":"node","type":"image","metadata":{"storageKey":"resource:image"}}],"connections":[]}`
	snapshot := model.CanvasShareSnapshot{ID: snapshotID, ShareID: share.ID, UserID: "owner", CanvasID: "source", Title: "示例", PayloadJSON: payload, SourceRevision: 1, CreatedAt: time.Now()}
	resource := model.Resource{ID: "image", UserID: "owner", Kind: "image", Status: model.ResourceStatusReady, Provider: "local", ObjectKey: "image.png", Size: 10}
	encoded, err := json.Marshal(resource)
	if err != nil {
		t.Fatal(err)
	}
	pin := model.CanvasShareSnapshotResource{SnapshotID: snapshot.ID, ResourceID: resource.ID, ResourceJSON: string(encoded), Provider: resource.Provider, ObjectKey: resource.ObjectKey}
	for _, item := range []any{&share, &snapshot, &pin} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := New(repository.New(db), nil)
	id, err := svc.CloneCanvasShare("recipient", token, snapshotID, "request")
	if err != nil || id == "" {
		t.Fatalf("clone: %q %v", id, err)
	}
	again, err := svc.CloneCanvasShare("recipient", token, snapshotID, "request")
	if err != nil || again != id {
		t.Fatalf("retry: %q %v", again, err)
	}
	var clone model.CanvasProject
	if err := db.First(&clone, "id = ? AND user_id = ?", id, "recipient").Error; err != nil {
		t.Fatal(err)
	}
	if strings.Contains(clone.PayloadJSON, "resource:image") || strings.Contains(clone.PayloadJSON, "clone-projection") || !strings.Contains(clone.PayloadJSON, "resource:") {
		t.Fatalf("references not rewritten: %s", clone.PayloadJSON)
	}
	newID := "snapshot-new"
	newSnapshot := model.CanvasShareSnapshot{ID: newID, ShareID: share.ID, UserID: share.UserID, CanvasID: share.ProjectID, PayloadJSON: `{"id":"source","nodes":[],"connections":[]}`}
	if err := db.Create(&newSnapshot).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&share).Update("current_snapshot_id", newID).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CloneCanvasShare("recipient", token, snapshotID, "historical-request"); err != nil {
		t.Fatalf("pinned historical version cannot be cloned: %v", err)
	}
	if err := db.Model(&snapshot).Update("payload_json", `{"id":"source","nodes":[{"id":"node","type":"drawing","metadata":{}}]}`).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CloneCanvasShare("recipient", token, snapshotID, "request-2"); err == nil {
		t.Fatal("incomplete clone accepted")
	}
}

func TestCreateCanvasSharePublishesImmutableSnapshotAndRollsBack(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "publish.db")+"?_foreign_keys=on"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	payload := `{"id":"source","nodes":[{"id":"node","type":"image","metadata":{"storageKey":"resource:image"}}],"connections":[]}`
	project := model.CanvasProject{ID: "source", UserID: "owner", Title: "原始画布", Revision: 1, PayloadJSON: payload}
	resource := model.Resource{ID: "image", UserID: "owner", Kind: "image", Status: model.ResourceStatusReady, Provider: "local", ObjectKey: "image.png"}
	for _, item := range []any{&project, &resource} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := New(repository.New(db), nil)
	first, err := svc.CreateCanvasShare("owner", "source", CanvasShareRequest{AllowCopy: true, ExpiresDays: 7})
	if err != nil || first.Token == "" {
		t.Fatalf("first share: %#v %v", first, err)
	}
	var share model.CanvasShare
	if err := db.First(&share, "user_id = ? AND project_id = ?", "owner", "source").Error; err != nil || share.CurrentSnapshotID == nil {
		t.Fatalf("snapshot pointer: %#v %v", share, err)
	}
	firstID := *share.CurrentSnapshotID
	updated := `{"id":"source","nodes":[],"connections":[]}`
	if err := db.Model(&project).Updates(map[string]any{"payload_json": updated, "revision": 2}).Error; err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateCanvasShare("owner", "source", CanvasShareRequest{AllowCopy: true, PreserveExpiry: true})
	if err != nil || first.Token != second.Token || first.ExpiresAt == nil || second.ExpiresAt == nil || !first.ExpiresAt.Equal(*second.ExpiresAt) || second.SnapshotAt == nil {
		t.Fatalf("update changed link or expiry: %#v %v", second, err)
	}
	if err := db.First(&share, "id = ?", share.ID).Error; err != nil || share.CurrentSnapshotID == nil || *share.CurrentSnapshotID == firstID {
		t.Fatalf("update did not publish: %#v %v", share, err)
	}
	secondID := *share.CurrentSnapshotID
	var original model.CanvasShareSnapshot
	if err := db.First(&original, "id = ?", firstID).Error; err != nil || original.PayloadJSON != payload {
		t.Fatalf("first snapshot changed: %#v %v", original, err)
	}
	bad := `{"id":"source","nodes":[{"id":"node","type":"image","metadata":{"storageKey":"resource:missing"}}]}`
	if err := db.Model(&project).Updates(map[string]any{"payload_json": bad, "revision": 3}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.CreateCanvasShare("owner", "source", CanvasShareRequest{Rotate: true}); err == nil {
		t.Fatal("published a missing resource")
	}
	if err := db.First(&share, "id = ?", share.ID).Error; err != nil || share.CurrentSnapshotID == nil || *share.CurrentSnapshotID != secondID || share.TokenHash != canvasShareTokenHash(first.Token) {
		t.Fatalf("failed update altered share: %#v %v", share, err)
	}
}
