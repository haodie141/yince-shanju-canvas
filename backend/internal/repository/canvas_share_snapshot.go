package repository

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"yingce/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrShareSnapshotChanged = errors.New("画布或分享状态已变化，请刷新后重试")
var ErrShareSnapshotResourceUnavailable = errors.New("分享素材不可用或正在删除")
var ErrShareSnapshotAlreadyMigrated = errors.New("分享已有固定快照")

func (r *Repository) LegacyCanvasShares(after string, limit int) ([]model.CanvasShare, error) {
	if limit <= 0 || limit > 200 {
		return nil, gorm.ErrInvalidData
	}
	var shares []model.CanvasShare
	err := r.db.Where("enabled = ? AND current_snapshot_id IS NULL AND id > ?", true, after).Order("id").Limit(limit).Find(&shares).Error
	return shares, err
}

func (r *Repository) CanvasShareSnapshot(share *model.CanvasShare, snapshotID string) (*model.CanvasShareSnapshot, error) {
	if share == nil || snapshotID == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var snapshot model.CanvasShareSnapshot
	if err := r.db.Where("id = ? AND share_id = ? AND user_id = ? AND canvas_id = ?", snapshotID, share.ID, share.UserID, share.ProjectID).First(&snapshot).Error; err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (r *Repository) CanvasShareSnapshotResource(snapshotID, resourceID string) (*model.CanvasShareSnapshotResource, error) {
	var pin model.CanvasShareSnapshotResource
	if err := r.db.Where("snapshot_id = ? AND resource_id = ?", snapshotID, resourceID).First(&pin).Error; err != nil {
		return nil, err
	}
	return &pin, nil
}

func (r *Repository) CanvasShareSnapshotResources(snapshotID string) ([]model.CanvasShareSnapshotResource, error) {
	var pins []model.CanvasShareSnapshotResource
	err := r.db.Where("snapshot_id = ?", snapshotID).Order("resource_id").Find(&pins).Error
	return pins, err
}

// SaveCanvasShareAndPublish saves share settings and freezes its project in one
// transaction. expected is nil only when this is a new share.
func (r *Repository) SaveCanvasShareAndPublish(share, expected *model.CanvasShare, snapshot *model.CanvasShareSnapshot, resourceIDs []string) error {
	if share == nil || snapshot == nil || share.ID != snapshot.ShareID || share.UserID != snapshot.UserID || share.ProjectID != snapshot.CanvasID {
		return ErrShareSnapshotChanged
	}
	return r.Transaction(func(repo *Repository) error {
		tx := repo.db
		var project model.CanvasProject
		query := tx.Where("id = ? AND user_id = ?", share.ProjectID, share.UserID)
		if r.Dialect() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.First(&project).Error; err != nil {
			return err
		}
		if project.Revision != snapshot.SourceRevision || project.PayloadJSON != snapshot.PayloadJSON {
			return ErrShareSnapshotChanged
		}
		var current model.CanvasShare
		query = tx.Where("user_id = ? AND project_id = ?", share.UserID, share.ProjectID)
		if r.Dialect() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		err := query.First(&current).Error
		if expected == nil {
			if err == nil {
				return ErrShareSnapshotChanged
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if err := tx.Create(share).Error; err != nil {
				return err
			}
		} else {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrShareSnapshotChanged
			}
			if err != nil {
				return err
			}
			if current.ID != expected.ID || current.TokenHash != expected.TokenHash || !current.UpdatedAt.Equal(expected.UpdatedAt) {
				return ErrShareSnapshotChanged
			}
			if err := tx.Save(share).Error; err != nil {
				return err
			}
		}
		return repo.PublishCanvasShareSnapshot(share.UserID, snapshot, resourceIDs)
	})
}

// PublishCanvasShareSnapshot commits a prepared snapshot, its resource pins,
// and the share pointer together. The caller supplies the exact saved revision
// and public resource set; no old snapshot is removed by this operation.
func (r *Repository) PublishCanvasShareSnapshot(userID string, snapshot *model.CanvasShareSnapshot, resourceIDs []string) error {
	return r.publishCanvasShareSnapshot(userID, snapshot, resourceIDs, false)
}

func (r *Repository) PublishLegacyCanvasShareSnapshot(userID string, snapshot *model.CanvasShareSnapshot, resourceIDs []string) error {
	return r.publishCanvasShareSnapshot(userID, snapshot, resourceIDs, true)
}

func (r *Repository) publishCanvasShareSnapshot(userID string, snapshot *model.CanvasShareSnapshot, resourceIDs []string, legacyOnly bool) error {
	if snapshot == nil || snapshot.ID == "" || snapshot.UserID != userID {
		return ErrShareSnapshotChanged
	}
	ids := slices.Clone(resourceIDs)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	return r.Transaction(func(repo *Repository) error {
		tx := repo.db
		var project model.CanvasProject
		query := tx.Where("id = ? AND user_id = ?", snapshot.CanvasID, userID)
		if r.Dialect() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.First(&project).Error; err != nil {
			return err
		}
		if project.Revision != snapshot.SourceRevision || project.PayloadJSON != snapshot.PayloadJSON {
			return ErrShareSnapshotChanged
		}
		var share model.CanvasShare
		query = tx.Where("id = ? AND user_id = ? AND project_id = ? AND enabled = ?", snapshot.ShareID, userID, snapshot.CanvasID, true)
		if r.Dialect() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.First(&share).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrShareSnapshotChanged
			}
			return err
		}
		if legacyOnly && share.CurrentSnapshotID != nil {
			return ErrShareSnapshotAlreadyMigrated
		}
		var resources []model.Resource
		if len(ids) > 0 {
			query = tx.Where("user_id = ? AND id IN ? AND status = ?", userID, ids, model.ResourceStatusReady).Order("id")
			if err := query.Find(&resources).Error; err != nil {
				return err
			}
			if len(resources) != len(ids) {
				return ErrShareSnapshotResourceUnavailable
			}
			if err := lockResourceObjects(tx, resources); err != nil {
				return err
			}
			// Lock physical objects before resource rows, then verify that the
			// observed objects did not change while waiting for the locks.
			var current []model.Resource
			query = tx.Where("user_id = ? AND id IN ? AND status = ?", userID, ids, model.ResourceStatusReady).Order("id")
			if r.Dialect() == "postgres" {
				query = query.Clauses(clause.Locking{Strength: "UPDATE"})
			}
			if err := query.Find(&current).Error; err != nil {
				return err
			}
			if len(current) != len(resources) {
				return ErrShareSnapshotResourceUnavailable
			}
			for i := range current {
				oldKey, oldErr := resourceObjectLockKey(resources[i])
				newKey, newErr := resourceObjectLockKey(current[i])
				if oldErr != nil || newErr != nil || resources[i].ID != current[i].ID || oldKey != newKey {
					return ErrShareSnapshotResourceUnavailable
				}
			}
			resources = current
			for _, resource := range resources {
				var count int64
				jobs := tx.Model(&model.ResourceDeletionJob{}).Where("object_key = ?", resource.ObjectKey)
				if strings.TrimSpace(resource.Provider) == "" || strings.EqualFold(strings.TrimSpace(resource.Provider), "local") {
					jobs = jobs.Where("LOWER(TRIM(provider)) IN ?", []string{"", "local"})
				} else {
					jobs = jobs.Where("LOWER(TRIM(provider)) = ? AND endpoint = ? AND bucket = ?", strings.ToLower(strings.TrimSpace(resource.Provider)), resource.Endpoint, resource.Bucket)
				}
				if err := jobs.Count(&count).Error; err != nil {
					return err
				}
				if count != 0 {
					return ErrShareSnapshotResourceUnavailable
				}
			}
		}
		if err := tx.Create(snapshot).Error; err != nil {
			return err
		}
		refs := make([]model.CanvasShareSnapshotResource, 0, len(ids))
		for _, resource := range resources {
			metadata, err := json.Marshal(resource)
			if err != nil {
				return err
			}
			refs = append(refs, model.CanvasShareSnapshotResource{
				SnapshotID: snapshot.ID, ResourceID: resource.ID, ResourceJSON: string(metadata), StorageSettingID: resource.StorageSettingID,
				Provider: resource.Provider, Endpoint: resource.Endpoint, Bucket: resource.Bucket, ObjectKey: resource.ObjectKey,
			})
		}
		if len(refs) > 0 {
			if err := tx.CreateInBatches(refs, 200).Error; err != nil {
				return err
			}
		}
		return tx.Model(&share).Updates(map[string]any{"current_snapshot_id": snapshot.ID, "updated_at": time.Now()}).Error
	})
}

// CanvasShareReferencesObject checks the underlying object identity rather
// than the resource ID. Callers must serialize it with publication and deletion.
func (r *Repository) CanvasShareReferencesObject(resource *model.Resource) (bool, error) {
	if resource == nil || strings.TrimSpace(resource.ObjectKey) == "" {
		return false, nil
	}
	query := r.db.Model(&model.CanvasShareSnapshotResource{}).Where("object_key = ?", resource.ObjectKey)
	if strings.TrimSpace(resource.Provider) == "" || strings.EqualFold(strings.TrimSpace(resource.Provider), "local") {
		query = query.Where("LOWER(TRIM(provider)) IN ?", []string{"", "local"})
	} else {
		query = query.Where("LOWER(TRIM(provider)) = ? AND endpoint = ? AND bucket = ?", strings.ToLower(strings.TrimSpace(resource.Provider)), resource.Endpoint, resource.Bucket)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// FeaturedCanvasReferencesObject protects published and draft showcase videos
// and covers even when another resource record aliases the same physical file.
func (r *Repository) FeaturedCanvasReferencesObject(resource *model.Resource) (bool, error) {
	if resource == nil || strings.TrimSpace(resource.ObjectKey) == "" {
		return false, nil
	}
	videoIDs := r.db.Model(&model.FeaturedCanvas{}).Select("video_resource_id")
	coverIDs := r.db.Model(&model.FeaturedCanvas{}).Where("cover_resource_id <> ''").Select("cover_resource_id")
	query := r.db.Model(&model.Resource{}).Where("(id IN (?) OR id IN (?)) AND object_key = ?", videoIDs, coverIDs, resource.ObjectKey)
	if strings.TrimSpace(resource.Provider) == "" || strings.EqualFold(strings.TrimSpace(resource.Provider), "local") {
		query = query.Where("LOWER(TRIM(provider)) IN ?", []string{"", "local"})
	} else {
		query = query.Where("LOWER(TRIM(provider)) = ? AND endpoint = ? AND bucket = ?", strings.ToLower(strings.TrimSpace(resource.Provider)), resource.Endpoint, resource.Bucket)
	}
	var count int64
	if err := query.Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
