package repository

import (
	"errors"
	"os"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"yingce/backend/internal/model"
)

func TestResourceObjectLockKeyMatchesSharedPhysicalIdentity(t *testing.T) {
	first := model.Resource{ID: "owner-resource", Provider: "local", ObjectKey: "a/b.png"}
	alias := model.Resource{ID: "recipient-resource", Provider: "", ObjectKey: "a/b.png"}
	a, err := resourceObjectLockKey(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := resourceObjectLockKey(alias)
	if err != nil || a != b {
		t.Fatalf("aliases must share lock: %d %d %v", a, b, err)
	}
	alias.Provider = " LOCAL "
	if key, err := resourceObjectLockKey(alias); err != nil || key != a {
		t.Fatalf("local provider spelling changed object lock: %d %v", key, err)
	}
	alias.Bucket = "other"
	c, err := resourceObjectLockKey(alias)
	if err != nil || c != a {
		t.Fatalf("local paths must ignore bucket: %d %v", c, err)
	}
	remote := model.Resource{Provider: "s3", Bucket: "one", ObjectKey: "a/b.png"}
	x, _ := resourceObjectLockKey(remote)
	remote.Bucket = "two"
	y, _ := resourceObjectLockKey(remote)
	if x == y {
		t.Fatal("different object storage buckets must use distinct locks")
	}
	if _, err := resourceObjectLockKey(model.Resource{}); err == nil {
		t.Fatal("empty object accepted")
	}
}

func TestPublishSnapshotWaitsForObjectDeletionPostgres(t *testing.T) {
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
	if err := db.AutoMigrate(&model.Resource{}, &model.ResourceDeletionJob{}, &model.CanvasProject{}, &model.CanvasShare{}, &model.CanvasShareSnapshot{}, &model.CanvasShareSnapshotResource{}); err != nil {
		t.Fatal(err)
	}
	project := model.CanvasProject{ID: "canvas-lock", UserID: "owner", Revision: 1, PayloadJSON: `{}`}
	share := model.CanvasShare{ID: "share-lock", UserID: "owner", ProjectID: project.ID, TokenHash: "lock-test-hash", Enabled: true}
	resource := model.Resource{ID: "resource-lock", UserID: "owner", Provider: "local", ObjectKey: "test/delete-race", Status: model.ResourceStatusReady}
	for _, item := range []any{&project, &share, &resource} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := New(db)
	locked, release := make(chan struct{}), make(chan struct{})
	deletion := make(chan error, 1)
	go func() {
		deletion <- repo.WithResourceObjectLock(&resource, func(tx *Repository) error { close(locked); <-release; return tx.DeleteResource("owner", resource.ID) })
	}()
	select {
	case <-locked:
	case <-time.After(5 * time.Second):
		t.Fatal("deletion lock timed out")
	}
	publication := make(chan error, 1)
	snapshot := model.CanvasShareSnapshot{ID: "snapshot-lock", ShareID: share.ID, UserID: "owner", CanvasID: project.ID, SourceRevision: 1, PayloadJSON: `{}`}
	go func() { publication <- repo.PublishCanvasShareSnapshot("owner", &snapshot, []string{resource.ID}) }()
	select {
	case err := <-publication:
		close(release)
		t.Fatalf("publication passed an active deletion: %v", err)
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	if err := <-deletion; err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-publication:
		if !errors.Is(err, ErrShareSnapshotResourceUnavailable) {
			t.Fatalf("expected unavailable resource: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("publication did not finish")
	}
	var count int64
	if err := db.Model(&model.CanvasShareSnapshot{}).Where("id = ?", snapshot.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("partial snapshot count %d: %v", count, err)
	}
}

func TestResourceObjectLockSerializesAliasesPostgres(t *testing.T) {
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
	repo := New(db)
	first := &model.Resource{ID: "one", Provider: "local", ObjectKey: "test/shared"}
	alias := &model.Resource{ID: "two", Provider: "", ObjectKey: "test/shared"}
	locked, release, entered := make(chan struct{}), make(chan struct{}), make(chan struct{})
	errs := make(chan error, 2)
	go func() {
		errs <- repo.WithResourceObjectLock(first, func(*Repository) error { close(locked); <-release; return nil })
	}()
	select {
	case <-locked:
	case <-time.After(5 * time.Second):
		t.Fatal("first lock timed out")
	}
	go func() {
		errs <- repo.WithResourceObjectLock(alias, func(*Repository) error { close(entered); return nil })
	}()
	select {
	case <-entered:
		close(release)
		t.Fatal("alias entered while object was locked")
	case <-time.After(200 * time.Millisecond):
	}
	close(release)
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("second lock did not proceed")
	}
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}
