package database

import (
	"path/filepath"
	"testing"

	"yingce/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCanvasShareSnapshotMigrationPreservesExistingShares(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "shares.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE canvas_shares (id TEXT PRIMARY KEY, user_id TEXT, project_id TEXT, token_hash TEXT, token_cipher TEXT, enabled BOOLEAN, allow_copy BOOLEAN)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`INSERT INTO canvas_shares(id,user_id,project_id,token_hash,token_cipher,enabled,allow_copy) VALUES ('old','owner','canvas','hash','cipher',1,1)`).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.Resource{}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := migrateCanvasShareSnapshots(db); err != nil {
			t.Fatal(err)
		}
	}
	var share model.CanvasShare
	if err := db.First(&share, "id = ?", "old").Error; err != nil {
		t.Fatal(err)
	}
	if share.TokenHash != "hash" || share.TokenCipher != "cipher" || !share.Enabled || !share.AllowCopy || share.CurrentSnapshotID != nil {
		t.Fatalf("existing share changed: %#v", share)
	}
	if !db.Migrator().HasTable(&model.CanvasShareSnapshot{}) || !db.Migrator().HasTable(&model.CanvasShareSnapshotResource{}) || !db.Migrator().HasTable(&model.CanvasShareCloneReceipt{}) || !db.Migrator().HasTable(&model.FeaturedCanvas{}) {
		t.Fatal("share snapshot tables not created")
	}
}
