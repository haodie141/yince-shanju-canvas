package repository

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"yingce/backend/internal/model"

	"gorm.io/gorm"
)

// resourceObjectLockKey identifies the physical object shared by resource
// aliases. A hash collision only serializes two unrelated objects.
func resourceObjectLockKey(resource model.Resource) (int64, error) {
	if strings.TrimSpace(resource.ObjectKey) == "" {
		return 0, errors.New("素材存储路径为空")
	}
	provider := strings.ToLower(strings.TrimSpace(resource.Provider))
	endpoint, bucket := resource.Endpoint, resource.Bucket
	if provider == "" || provider == "local" {
		provider = "local"
		endpoint, bucket = "", ""
	}
	encoded, err := json.Marshal([]string{provider, endpoint, bucket, resource.ObjectKey})
	if err != nil {
		return 0, err
	}
	digest := sha256.Sum256(encoded)
	return int64(binary.BigEndian.Uint64(digest[:8])), nil
}

// lockResourceObjects must run inside the same transaction as the operation
// that creates a reference or deletes an object. All keys are acquired in a
// stable order so concurrent multi-object operations cannot deadlock.
func lockResourceObjects(tx *gorm.DB, resources []model.Resource) error {
	keys := make([]int64, 0, len(resources))
	for _, resource := range resources {
		key, err := resourceObjectLockKey(resource)
		if err != nil {
			return err
		}
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	switch tx.Dialector.Name() {
	case "postgres":
		for i, key := range keys {
			if i > 0 && keys[i-1] == key {
				continue
			}
			if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", key).Error; err != nil {
				return err
			}
		}
		return nil
	case "sqlite":
		// A no-op UPDATE obtains SQLite's database-wide write lock until commit.
		return tx.Exec("UPDATE resource_deletion_jobs SET id = id WHERE 1 = 0").Error
	default:
		return errors.New("当前数据库不支持素材共享锁")
	}
}

// WithResourceObjectLock keeps the lock until fn completes and its transaction
// commits. fn must use the supplied repository for all database reads.
func (r *Repository) WithResourceObjectLock(resource *model.Resource, fn func(*Repository) error) error {
	if resource == nil || fn == nil {
		return errors.New("素材或删除操作无效")
	}
	return r.Transaction(func(locked *Repository) error {
		if err := lockResourceObjects(locked.db, []model.Resource{*resource}); err != nil {
			return err
		}
		return fn(locked)
	})
}
