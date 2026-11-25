package canvas

import (
	"path/filepath"
	"strings"
	"testing"

	"yingce/backend/internal/database"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestMigrateLegacyCanvasSharesIsIndependentAndRepeatable(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "legacy.db")+"?_foreign_keys=on"), &gorm.Config{})
	if err != nil { t.Fatal(err) }
	if err := database.MigrateSchema(db); err != nil { t.Fatal(err) }
	token := strings.Repeat("a", 43)
	good := model.CanvasProject{ID:"good", UserID:"owner", Revision:1, PayloadJSON:`{"id":"good","nodes":[]}`}
	bad := model.CanvasProject{ID:"bad", UserID:"owner", Revision:1, PayloadJSON:`{"id":"bad","nodes":[{"id":"a","type":"image","metadata":{"storageKey":"resource:missing"}}]}`}
	share := model.CanvasShare{ID:"a", UserID:"owner", ProjectID:"good", Enabled:true, AllowCopy:true, TokenHash:canvasShareTokenHash(token), TokenCipher:token}
	broken := model.CanvasShare{ID:"b", UserID:"owner", ProjectID:"bad", Enabled:true, TokenHash:"other", TokenCipher:"cipher"}
	for _, item := range []any{&good,&bad,&share,&broken} { if err := db.Create(item).Error; err != nil { t.Fatal(err) } }
	svc := New(repository.New(db),nil)
	result, err := svc.MigrateLegacyCanvasShares("",100)
	if err != nil || result.Migrated != 1 || len(result.Failures) != 1 || result.Failures[0].ShareID != "b" { t.Fatalf("migration: %#v %v",result,err) }
	if err := db.First(&share,"id = ?","a").Error; err != nil || share.CurrentSnapshotID == nil || share.TokenHash != canvasShareTokenHash(token) || share.TokenCipher != token { t.Fatalf("share changed: %#v %v",share,err) }
	if err := db.First(&broken,"id = ?","b").Error; err != nil || broken.CurrentSnapshotID != nil || broken.TokenCipher != "cipher" { t.Fatalf("failed share changed: %#v %v",broken,err) }
	if err := db.Model(&good).Updates(map[string]any{"payload_json":`{"id":"good","nodes":[{"id":"x","type":"text"}]}`,"revision":2}).Error; err != nil { t.Fatal(err) }
	public, err := svc.PublicCanvasShare(token)
	if err != nil || len(public.Project["nodes"].([]any)) != 0 { t.Fatalf("snapshot changed with source: %#v %v",public,err) }
	again, err := svc.MigrateLegacyCanvasShares("",100)
	if err != nil || again.Migrated != 0 || len(again.Failures) != 1 { t.Fatalf("migration is not repeatable: %#v %v",again,err) }
}
