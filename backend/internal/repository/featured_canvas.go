package repository

import (
	"errors"
	"time"

	"gorm.io/gorm/clause"
	"yingce/backend/internal/model"
)

var ErrFeaturedOrderChanged = errors.New("精选列表已变化，请刷新后重试")

func (r *Repository) FeaturedCanvases(onlyEnabled bool) ([]model.FeaturedCanvas, error) {
	var entries []model.FeaturedCanvas
	query := r.db.Order("sort_order ASC, created_at DESC")
	if onlyEnabled {
		query = query.Where("enabled = ?", true)
	}
	err := query.Limit(200).Find(&entries).Error
	return entries, err
}

func (r *Repository) FeaturedCanvas(id string) (*model.FeaturedCanvas, error) {
	var entry model.FeaturedCanvas
	if err := r.db.First(&entry, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &entry, nil
}

func (r *Repository) FeaturedSnapshot(id string) (*model.CanvasShareSnapshot, error) {
	var snapshot model.CanvasShareSnapshot
	if err := r.db.First(&snapshot, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (r *Repository) CanvasShareByID(id string) (*model.CanvasShare, error) {
	var share model.CanvasShare
	if err := r.db.First(&share, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &share, nil
}

func (r *Repository) FeaturedCopyByTokenHash(tokenHash string) (*model.FeaturedCanvas, error) {
	var entry model.FeaturedCanvas
	if err := r.db.First(&entry, "copy_token_hash = ?", tokenHash).Error; err != nil {
		return nil, err
	}
	return &entry, nil
}

// FeaturedCopySnapshotReferences counts remaining references to a persisted
// copy snapshot. Cleanup only runs when the source share is gone and nothing
// else (share history or another featured item) still depends on the snapshot.
func (r *Repository) FeaturedCopySnapshotReferences(snapshotID string) (int64, error) {
	var shareCount, featuredCount int64
	if err := r.db.Model(&model.CanvasShare{}).Where("current_snapshot_id = ? AND enabled = ? AND revoked_at IS NULL AND canvas_deleted_at IS NULL", snapshotID, true).Count(&shareCount).Error; err != nil {
		return 0, err
	}
	if err := r.db.Model(&model.FeaturedCanvas{}).Where("snapshot_id = ? OR copy_snapshot_id = ?", snapshotID, snapshotID).Count(&featuredCount).Error; err != nil {
		return 0, err
	}
	return shareCount + featuredCount, nil
}

func (r *Repository) DeleteFeaturedCopySnapshot(snapshotID string) error {
	return r.Transaction(func(repo *Repository) error {
		var shareCount, featuredCount int64
		if err := repo.db.Model(&model.CanvasShare{}).Where("current_snapshot_id = ? AND enabled = ? AND revoked_at IS NULL AND canvas_deleted_at IS NULL", snapshotID, true).Count(&shareCount).Error; err != nil {
			return err
		}
		if err := repo.db.Model(&model.FeaturedCanvas{}).Where("snapshot_id = ? OR copy_snapshot_id = ?", snapshotID, snapshotID).Count(&featuredCount).Error; err != nil {
			return err
		}
		if shareCount+featuredCount > 0 {
			return nil
		}
		if err := repo.db.Where("snapshot_id = ?", snapshotID).Delete(&model.CanvasShareSnapshotResource{}).Error; err != nil {
			return err
		}
		return repo.db.Where("id = ?", snapshotID).Delete(&model.CanvasShareSnapshot{}).Error
	})
}

func (r *Repository) DeleteFeaturedCanvas(id string) error {
	return r.db.Delete(&model.FeaturedCanvas{}, "id = ?", id).Error
}

// FeaturedNextSortOrder appends new items after the current tail. Callers hold
// the same transaction that saves the entry so concurrent inserts keep a gap.
func (r *Repository) FeaturedNextSortOrder() (int, error) {
	var maxOrder int
	err := r.db.Model(&model.FeaturedCanvas{}).Select("COALESCE(MAX(sort_order), -1)").Scan(&maxOrder).Error
	return maxOrder + 1, err
}

// ReorderFeaturedCanvases rewrites the global order in one transaction. The
// expected update timestamps make concurrent administrator reorders conflict
// instead of silently overwriting each other.
func (r *Repository) ReorderFeaturedCanvases(ids []string, expectedUpdatedAt map[string]time.Time) error {
	return r.Transaction(func(repo *Repository) error {
		query := repo.db.Order("sort_order ASC, created_at DESC")
		if repo.Dialect() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		var entries []model.FeaturedCanvas
		if err := query.Find(&entries).Error; err != nil {
			return err
		}
		if len(entries) != len(ids) || len(expectedUpdatedAt) != len(entries) {
			return ErrFeaturedOrderChanged
		}
		seen := make(map[string]bool, len(ids))
		for _, id := range ids {
			if id == "" || seen[id] {
				return ErrFeaturedOrderChanged
			}
			seen[id] = true
		}
		for _, entry := range entries {
			expected, ok := expectedUpdatedAt[entry.ID]
			if !ok || !entry.UpdatedAt.Equal(expected) {
				return ErrFeaturedOrderChanged
			}
		}
		now := time.Now()
		for index, id := range ids {
			if err := repo.db.Model(&model.FeaturedCanvas{}).Where("id = ?", id).Updates(map[string]any{"sort_order": index, "updated_at": now}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (r *Repository) SetFeaturedCanvasEnabled(id string, enabled bool) (*model.FeaturedCanvas, error) {
	var entry model.FeaturedCanvas
	if err := r.db.First(&entry, "id = ?", id).Error; err != nil {
		return nil, err
	}
	if err := r.db.Model(&model.FeaturedCanvas{}).Where("id = ?", id).Updates(map[string]any{"enabled": enabled, "updated_at": time.Now()}).Error; err != nil {
		return nil, err
	}
	if err := r.db.First(&entry, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &entry, nil
}
