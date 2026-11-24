package database

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yingce/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestFeaturedCanvasProtectsVideoAndSnapshot(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "featured.db")+"?_foreign_keys=on"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Resource{}, &model.CanvasShareSnapshot{}, &model.FeaturedCanvas{}); err != nil {
		t.Fatal(err)
	}
	video := model.Resource{ID: "video", UserID: "admin", Kind: "video", Status: model.ResourceStatusReady, ObjectKey: "private/path.mp4"}
	snapshot := model.CanvasShareSnapshot{ID: "snapshot", ShareID: "share", UserID: "admin", CanvasID: "canvas", PayloadJSON: `{"apiKey":"private"}`, CreatedAt: time.Now()}
	featured := model.FeaturedCanvas{ID: "featured", Title: "作品", VideoResourceID: video.ID, SnapshotID: snapshot.ID, CreatedBy: "admin", Video: &video, Snapshot: &snapshot}
	for _, item := range []any{&video, &snapshot} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	// Insert without associations; the referenced records already exist.
	if err := db.Omit("Video", "Snapshot").Create(&featured).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Delete(&video).Error; err == nil {
		t.Fatal("featured video can be deleted")
	}
	if err := db.Delete(&snapshot).Error; err == nil {
		t.Fatal("featured snapshot can be deleted")
	}
	encoded, err := json.Marshal(featured)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "private") || strings.Contains(string(encoded), "admin") {
		t.Fatalf("internal details exposed: %s", encoded)
	}
}
