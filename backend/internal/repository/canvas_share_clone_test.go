package repository

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"yingce/backend/internal/database"
	"yingce/backend/internal/model"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestCreateCanvasShareCloneAtomicQuotaAndRetry(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "clone.db")+"?_foreign_keys=on"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.MigrateSchema(db); err != nil {
		t.Fatal(err)
	}
	snapshotID := "snapshot"
	share := model.CanvasShare{ID: "share", UserID: "owner", ProjectID: "source", TokenHash: "hash", Enabled: true, AllowCopy: true, CurrentSnapshotID: &snapshotID}
	snapshot := model.CanvasShareSnapshot{ID: snapshotID, ShareID: share.ID, UserID: share.UserID, CanvasID: share.ProjectID, PayloadJSON: `{}`, CreatedAt: time.Now()}
	original := model.Resource{ID: "source-image", UserID: "owner", Kind: "image", Provider: "local", ObjectKey: "image.png", Size: 10, Status: model.ResourceStatusReady}
	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	pin := model.CanvasShareSnapshotResource{SnapshotID: snapshot.ID, ResourceID: original.ID, ResourceJSON: string(encoded), Provider: original.Provider, ObjectKey: original.ObjectKey}
	for _, item := range []any{&share, &snapshot, &pin} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := New(db)
	calls := 0
	build := func(_ model.CanvasShareSnapshot, _ []model.CanvasShareSnapshotResource) (*model.CanvasProject, map[string]model.Resource, error) {
		calls++
		alias := original
		alias.ID, alias.UserID = fmt.Sprintf("target-%d", calls), "recipient"
		project := &model.CanvasProject{ID: fmt.Sprintf("canvas-%d", calls), UserID: "recipient", Title: "clone", PayloadJSON: `{"nodes":[]}`}
		return project, map[string]model.Resource{original.ID: alias}, nil
	}
	quota := func(*Repository, bool, int64) error { return nil }
	clone, err := repo.CreateCanvasShareClone("recipient", "request", share.ID, snapshot.ID, "hash", 100, build, quota)
	if err != nil || clone.CanvasID != "canvas-1" {
		t.Fatalf("clone: %#v %v", clone, err)
	}
	again, err := repo.CreateCanvasShareClone("recipient", "request", share.ID, snapshot.ID, "hash", 100, build, quota)
	if err != nil || again.CanvasID != clone.CanvasID || calls != 1 {
		t.Fatalf("retry: %#v calls=%d %v", again, calls, err)
	}
	if _, err := repo.CreateCanvasShareClone("recipient", "request-2", share.ID, snapshot.ID, "wrong-hash", 100, build, quota); !errors.Is(err, ErrShareCloneUnavailable) {
		t.Fatalf("invalid token: %v", err)
	}
	if _, err := repo.CreateCanvasShareClone("recipient", "request-3", share.ID, snapshot.ID, "hash", 5, build, quota); !errors.Is(err, ErrUploadStorageLimit) {
		t.Fatalf("quota: %v", err)
	}
	conflicting := func(_ model.CanvasShareSnapshot, _ []model.CanvasShareSnapshotResource) (*model.CanvasProject, map[string]model.Resource, error) {
		alias := original
		alias.ID, alias.UserID = "rolled-back-resource", "recipient"
		return &model.CanvasProject{ID: "canvas-1", UserID: "recipient", PayloadJSON: `{}`}, map[string]model.Resource{original.ID: alias}, nil
	}
	if _, err := repo.CreateCanvasShareClone("recipient", "request-4", share.ID, snapshot.ID, "hash", 100, conflicting, quota); err == nil {
		t.Fatal("duplicate canvas should roll back its resource aliases")
	}
	if err := db.Model(&pin).Updates(map[string]any{"provider": "s3", "storage_setting_id": "private"}).Error; err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateCanvasShareClone("recipient", "request-5", share.ID, snapshot.ID, "hash", 100, build, quota); !errors.Is(err, ErrShareCloneUnsupportedStorage) {
		t.Fatalf("private storage should be rejected: %v", err)
	}
	for _, check := range []struct {
		table any
		want  int64
	}{{&model.CanvasProject{}, 1}, {&model.Resource{}, 1}, {&model.CanvasShareCloneReceipt{}, 1}} {
		var count int64
		if err := db.Model(check.table).Count(&count).Error; err != nil || count != check.want {
			t.Fatalf("partial clone count %d, want %d: %v", count, check.want, err)
		}
	}
}

func TestCreateCanvasShareCloneConcurrentRetryPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_SHARE_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_SHARE_POSTGRES_DSN not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	if err := db.AutoMigrate(&model.UploadReservation{}, &model.ResourceDeletionJob{}, &model.Resource{}, &model.CanvasProject{}, &model.CanvasShare{}, &model.CanvasShareSnapshot{}, &model.CanvasShareSnapshotResource{}, &model.CanvasShareCloneReceipt{}); err != nil {
		t.Fatal(err)
	}
	snapshotID := "parallel-snapshot"
	share := model.CanvasShare{ID: "parallel-share", UserID: "owner", ProjectID: "source", TokenHash: "parallel-hash", Enabled: true, AllowCopy: true, CurrentSnapshotID: &snapshotID}
	snapshot := model.CanvasShareSnapshot{ID: snapshotID, ShareID: share.ID, UserID: share.UserID, CanvasID: share.ProjectID, PayloadJSON: `{}`, CreatedAt: time.Now()}
	for _, item := range []any{&share, &snapshot} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := New(db)
	var calls atomic.Int32
	build := func(_ model.CanvasShareSnapshot, _ []model.CanvasShareSnapshotResource) (*model.CanvasProject, map[string]model.Resource, error) {
		calls.Add(1)
		time.Sleep(150 * time.Millisecond)
		return &model.CanvasProject{ID: "parallel-canvas", UserID: "recipient", PayloadJSON: `{"nodes":[]}`}, map[string]model.Resource{}, nil
	}
	quota := func(*Repository, bool, int64) error { return nil }
	type result struct {
		receipt *model.CanvasShareCloneReceipt
		err     error
	}
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			receipt, err := repo.CreateCanvasShareClone("recipient", "parallel-request", share.ID, snapshot.ID, "parallel-hash", 100, build, quota)
			results <- result{receipt, err}
		}()
	}
	for i := 0; i < 2; i++ {
		select {
		case got := <-results:
			if got.err != nil || got.receipt.CanvasID != "parallel-canvas" {
				t.Fatalf("clone failed: %#v %v", got.receipt, got.err)
			}
		case <-time.After(10 * time.Second):
			t.Fatal("concurrent clone timed out")
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("created %d times", calls.Load())
	}
}

func TestAuthorizeShareCloneStorageRejectsPrivateCredentials(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "locations.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&model.StorageLocation{}); err != nil {
		t.Fatal(err)
	}
	for _, location := range []model.StorageLocation{
		{ID: "public", Scope: "platform", Provider: "s3", LocationDigest: "one"},
		{ID: "private", Scope: "user", OwnerID: "owner", Provider: "s3", LocationDigest: "two"},
	} {
		if err := db.Create(&location).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := New(db)
	for _, check := range []struct {
		pin  model.CanvasShareSnapshotResource
		want error
	}{
		{model.CanvasShareSnapshotResource{Provider: "local"}, nil},
		{model.CanvasShareSnapshotResource{Provider: "S3", StorageSettingID: "public"}, nil},
		{model.CanvasShareSnapshotResource{Provider: "s3", StorageSettingID: "private"}, ErrShareCloneUnsupportedStorage},
		{model.CanvasShareSnapshotResource{Provider: "s3"}, ErrShareCloneUnsupportedStorage},
		{model.CanvasShareSnapshotResource{Provider: "aliyun", StorageSettingID: "public"}, ErrShareCloneUnsupportedStorage},
	} {
		err := repo.AuthorizeShareCloneStorage(check.pin)
		if !errors.Is(err, check.want) {
			t.Fatalf("pin %#v: got %v, want %v", check.pin, err, check.want)
		}
	}
}
