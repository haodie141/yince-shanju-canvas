package canvas

import (
	"path/filepath"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"yingce/backend/internal/assets"
	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

type snapshotCaptureHost struct {
	nopHost
	resource *model.Resource
}

func (h *snapshotCaptureHost) PrepareResourceDelivery(_ string, resource *model.Resource, _ assets.AccessOptions, _ string) (*assets.ResourceDelivery, error) {
	h.resource = resource
	return &assets.ResourceDelivery{Resource: resource}, nil
}

func TestShareSnapshotReadStaysStableAcrossUpdateAndSourceRemoval(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "read.db")+"?_foreign_keys=on"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	payload := `{"id":"source","nodes":[{"id":"node","type":"image","metadata":{"storageKey":"resource:image"}}],"connections":[]}`
	project := model.CanvasProject{ID: "source", UserID: "owner", Title: "画布", Revision: 1, PayloadJSON: payload}
	resource := model.Resource{ID: "image", UserID: "owner", Kind: "image", Status: model.ResourceStatusReady, Provider: "local", ObjectKey: "image.png"}
	for _, item := range []any{&project, &resource} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	host := &snapshotCaptureHost{}
	svc := New(repository.New(db), host)
	share, err := svc.CreateCanvasShare("owner", "source", CanvasShareRequest{AllowCopy: true})
	if err != nil {
		t.Fatal(err)
	}
	first, err := svc.PublicCanvasShare(share.Token)
	if err != nil || first.SnapshotID == "" {
		t.Fatalf("missing snapshot: %#v %v", first, err)
	}
	if err := db.Model(&project).Updates(map[string]any{"payload_json": `{"id":"source","nodes":[],"connections":[]}`, "revision": 2}).Error; err != nil {
		t.Fatal(err)
	}
	stillFirst, err := svc.PublicCanvasShare(share.Token)
	if err != nil || stillFirst.SnapshotID != first.SnapshotID || len(stillFirst.Project["nodes"].([]any)) != 1 {
		t.Fatalf("live update leaked: %#v %v", stillFirst, err)
	}
	if _, err := svc.CreateCanvasShare("owner", "source", CanvasShareRequest{AllowCopy: true}); err != nil {
		t.Fatal(err)
	}
	second, err := svc.PublicCanvasShare(share.Token)
	if err != nil || second.SnapshotID == first.SnapshotID || len(second.Project["nodes"].([]any)) != 0 {
		t.Fatalf("share update failed: %#v %v", second, err)
	}
	pinned, err := svc.PublicCanvasShareVersion(share.Token, first.SnapshotID)
	if err != nil || pinned.SnapshotID != first.SnapshotID || len(pinned.Project["nodes"].([]any)) != 1 {
		t.Fatalf("pinned version changed: %#v %v", pinned, err)
	}
	if err := db.Delete(&resource).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PrepareSharedCanvasSnapshotResourceDelivery(share.Token, first.SnapshotID, "image", assets.AccessOptions{}, ""); err != nil || host.resource == nil || host.resource.ObjectKey != "image.png" {
		t.Fatalf("old media lost: %#v %v", host.resource, err)
	}
	if _, err := svc.PrepareSharedCanvasSnapshotResourceDelivery(share.Token, second.SnapshotID, "image", assets.AccessOptions{}, ""); err == nil {
		t.Fatal("new snapshot exposed unrelated resource")
	}
	if err := svc.DeleteCanvasShare("owner", "source"); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PrepareSharedCanvasSnapshotResourceDelivery(share.Token, first.SnapshotID, "image", assets.AccessOptions{}, ""); err == nil {
		t.Fatal("revoked share still serves media")
	}
}
