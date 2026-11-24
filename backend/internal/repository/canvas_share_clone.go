package repository

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"yingce/backend/internal/model"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrShareCloneUnavailable = errors.New("分享已失效、不可复制或内容已更新")
var ErrShareCloneRequestConflict = errors.New("克隆请求标识已被其他操作使用")
var ErrShareCloneUnsupportedStorage = errors.New("分享中包含无法安全复用的外部存储素材")

// Personal OSS credentials belong to the sharing user. Only local files and
// explicitly registered platform storage locations can be shared by alias.
func (r *Repository) AuthorizeShareCloneStorage(pin model.CanvasShareSnapshotResource) error {
	provider := strings.ToLower(strings.TrimSpace(pin.Provider))
	if provider == "" || provider == "local" {
		return nil
	}
	if pin.StorageSettingID == "" {
		return ErrShareCloneUnsupportedStorage
	}
	var location model.StorageLocation
	if err := r.db.Where("id = ? AND scope = ? AND owner_id = ?", pin.StorageSettingID, "platform", "").First(&location).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrShareCloneUnsupportedStorage
		}
		return err
	}
	if !strings.EqualFold(strings.TrimSpace(location.Provider), provider) {
		return ErrShareCloneUnsupportedStorage
	}
	return nil
}

// CreateCanvasShareClone atomically creates recipient-owned resource aliases,
// a canvas and an idempotency receipt. Storage location authorization and
// structured canvas quota checks both run inside the supplied transaction.
// This method must only be called after the share token has been authenticated.
func (r *Repository) CreateCanvasShareClone(userID, requestID, shareID, snapshotID, tokenHash string, storageLimit int64,
	build func(model.CanvasShareSnapshot, []model.CanvasShareSnapshotResource) (*model.CanvasProject, map[string]model.Resource, error),
	quota func(*Repository, bool, int64) error) (*model.CanvasShareCloneReceipt, error) {
	if userID == "" || requestID == "" || len(requestID) > 80 || shareID == "" || snapshotID == "" || tokenHash == "" || storageLimit <= 0 || build == nil || quota == nil {
		return nil, gorm.ErrInvalidData
	}
	var result model.CanvasShareCloneReceipt
	err := r.Transaction(func(repo *Repository) error {
		tx := repo.db
		if err := lockUploadStorage(tx, userID); err != nil {
			return err
		}
		var share model.CanvasShare
		query := tx.Where("id = ? AND token_hash = ?", shareID, tokenHash)
		if r.Dialect() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.First(&share).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrShareCloneUnavailable
			}
			return err
		}
		if !share.Enabled || !share.AllowCopy || share.CurrentSnapshotID == nil || (share.ExpiresAt != nil && !share.ExpiresAt.After(time.Now())) {
			return ErrShareCloneUnavailable
		}
		var receipt model.CanvasShareCloneReceipt
		err := tx.Where("user_id = ? AND request_id = ?", userID, requestID).First(&receipt).Error
		if err == nil {
			if receipt.ShareID != shareID || receipt.SnapshotID != snapshotID {
				return ErrShareCloneRequestConflict
			}
			result = receipt
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var snapshot model.CanvasShareSnapshot
		if err := tx.Where("id = ? AND share_id = ? AND user_id = ? AND canvas_id = ?", snapshotID, share.ID, share.UserID, share.ProjectID).First(&snapshot).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrShareCloneUnavailable
			}
			return err
		}
		var pins []model.CanvasShareSnapshotResource
		if err := tx.Where("snapshot_id = ?", snapshotID).Order("resource_id").Find(&pins).Error; err != nil {
			return err
		}
		objects := make([]model.Resource, 0, len(pins))
		for _, pin := range pins {
			objects = append(objects, model.Resource{Provider: pin.Provider, Endpoint: pin.Endpoint, Bucket: pin.Bucket, ObjectKey: pin.ObjectKey})
		}
		if err := lockResourceObjects(tx, objects); err != nil {
			return err
		}
		for _, pin := range pins {
			if err := repo.AuthorizeShareCloneStorage(pin); err != nil {
				return err
			}
		}
		project, aliases, err := build(snapshot, pins)
		if err != nil {
			return err
		}
		if project == nil || project.ID == "" || project.UserID != userID || project.ProjectID != "" || !json.Valid([]byte(project.PayloadJSON)) || len(aliases) != len(pins) {
			return gorm.ErrInvalidData
		}
		if err := quota(repo, true, int64(len(project.PayloadJSON))); err != nil {
			return err
		}
		newIDs := make(map[string]bool, len(pins))
		resources := make([]model.Resource, 0, len(pins))
		for _, pin := range pins {
			resource, ok := aliases[pin.ResourceID]
			var original model.Resource
			if !ok || json.Unmarshal([]byte(pin.ResourceJSON), &original) != nil || original.ID != pin.ResourceID || original.UserID != snapshot.UserID || original.Status != model.ResourceStatusReady ||
				original.Provider != pin.Provider || original.Endpoint != pin.Endpoint || original.Bucket != pin.Bucket || original.ObjectKey != pin.ObjectKey ||
				resource.ID == "" || resource.ID == pin.ResourceID || newIDs[resource.ID] || resource.UserID != userID || resource.Status != model.ResourceStatusReady ||
				resource.Provider != pin.Provider || resource.Endpoint != pin.Endpoint || resource.Bucket != pin.Bucket || resource.ObjectKey != pin.ObjectKey ||
				resource.StorageSettingID != pin.StorageSettingID || resource.Size != original.Size || resource.Kind != original.Kind || resource.PlaybackObjectKey != original.PlaybackObjectKey || resource.UploadKey != nil || resource.PublicURL != "" {
				return gorm.ErrInvalidData
			}
			newIDs[resource.ID] = true
			resources = append(resources, resource)
		}
		if len(resources) > 0 {
			if err := tx.CreateInBatches(resources, 200).Error; err != nil {
				return err
			}
		}
		stored, err := repo.UserStoredFileBytes(userID)
		if err != nil {
			return err
		}
		var pending int64
		if err := tx.Model(&model.UploadReservation{}).Select("COALESCE(SUM(size),0)").Where("user_id = ? AND expires_at > ?", userID, time.Now()).Scan(&pending).Error; err != nil {
			return err
		}
		if stored >= storageLimit || pending >= storageLimit-stored {
			return ErrUploadStorageLimit
		}
		project.Revision = 1
		if err := tx.Create(project).Error; err != nil {
			return err
		}
		result = model.CanvasShareCloneReceipt{UserID: userID, RequestID: requestID, ShareID: shareID, SnapshotID: snapshotID, CanvasID: project.ID, CreatedAt: time.Now()}
		if err := tx.Create(&result).Error; err != nil {
			return err
		}
		repo.canvasChanged(userID, project.ID)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// CreateFeaturedCanvasClone clones a platform-persisted featured copy. It
// re-checks the copy eligibility and source share authorization inside the
// transaction so a concurrent revoke cannot slip through.
func (r *Repository) CreateFeaturedCanvasClone(userID, requestID, featuredID, snapshotID, tokenHash string, storageLimit int64,
	build func(model.CanvasShareSnapshot, []model.CanvasShareSnapshotResource) (*model.CanvasProject, map[string]model.Resource, error),
	quota func(*Repository, bool, int64) error) (*model.CanvasShareCloneReceipt, error) {
	if userID == "" || requestID == "" || len(requestID) > 80 || featuredID == "" || snapshotID == "" || tokenHash == "" || storageLimit <= 0 || build == nil || quota == nil {
		return nil, gorm.ErrInvalidData
	}
	hash := strings.TrimSpace(tokenHash)
	var result model.CanvasShareCloneReceipt
	err := r.Transaction(func(repo *Repository) error {
		tx := repo.db
		if err := lockUploadStorage(tx, userID); err != nil {
			return err
		}
		var entry model.FeaturedCanvas
		query := tx.Where("id = ?", featuredID)
		if r.Dialect() == "postgres" {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.First(&entry).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrShareCloneUnavailable
			}
			return err
		}
		if !entry.Enabled || !entry.PersistCopy || entry.CopyStatus != "ready" || entry.CopySnapshotID != snapshotID || entry.CopyTokenHash != hash {
			return ErrShareCloneUnavailable
		}
		var receipt model.CanvasShareCloneReceipt
		err := tx.Where("user_id = ? AND request_id = ?", userID, requestID).First(&receipt).Error
		if err == nil {
			if receipt.ShareID != featuredID || receipt.SnapshotID != snapshotID {
				return ErrShareCloneRequestConflict
			}
			result = receipt
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var snapshot model.CanvasShareSnapshot
		if err := tx.Where("id = ?", snapshotID).First(&snapshot).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrShareCloneUnavailable
			}
			return err
		}
		var sourceShare model.CanvasShare
		shareErr := tx.Where("id = ?", snapshot.ShareID).First(&sourceShare).Error
		if shareErr == nil {
			if sourceShare.RevokedAt != nil {
				return ErrShareCloneUnavailable
			}
			if sourceShare.CanvasDeletedAt == nil && (!sourceShare.Enabled || !sourceShare.AllowCopy || (sourceShare.ExpiresAt != nil && !sourceShare.ExpiresAt.After(time.Now()))) {
				return ErrShareCloneUnavailable
			}
		} else if !errors.Is(shareErr, gorm.ErrRecordNotFound) {
			return shareErr
		}
		var pins []model.CanvasShareSnapshotResource
		if err := tx.Where("snapshot_id = ?", snapshotID).Order("resource_id").Find(&pins).Error; err != nil {
			return err
		}
		objects := make([]model.Resource, 0, len(pins))
		for _, pin := range pins {
			objects = append(objects, model.Resource{Provider: pin.Provider, Endpoint: pin.Endpoint, Bucket: pin.Bucket, ObjectKey: pin.ObjectKey})
		}
		if err := lockResourceObjects(tx, objects); err != nil {
			return err
		}
		for _, pin := range pins {
			if err := repo.AuthorizeShareCloneStorage(pin); err != nil {
				return err
			}
		}
		project, aliases, err := build(snapshot, pins)
		if err != nil {
			return err
		}
		if project == nil || project.ID == "" || project.UserID != userID || project.ProjectID != "" || !json.Valid([]byte(project.PayloadJSON)) || len(aliases) != len(pins) {
			return gorm.ErrInvalidData
		}
		if err := quota(repo, true, int64(len(project.PayloadJSON))); err != nil {
			return err
		}
		newIDs := make(map[string]bool, len(pins))
		resources := make([]model.Resource, 0, len(pins))
		for _, pin := range pins {
			resource, ok := aliases[pin.ResourceID]
			var original model.Resource
			if !ok || json.Unmarshal([]byte(pin.ResourceJSON), &original) != nil || original.ID != pin.ResourceID || original.UserID != snapshot.UserID || original.Status != model.ResourceStatusReady ||
				original.Provider != pin.Provider || original.Endpoint != pin.Endpoint || original.Bucket != pin.Bucket || original.ObjectKey != pin.ObjectKey ||
				resource.ID == "" || resource.ID == pin.ResourceID || newIDs[resource.ID] || resource.UserID != userID || resource.Status != model.ResourceStatusReady ||
				resource.Provider != pin.Provider || resource.Endpoint != pin.Endpoint || resource.Bucket != pin.Bucket || resource.ObjectKey != pin.ObjectKey ||
				resource.StorageSettingID != pin.StorageSettingID || resource.Size != original.Size || resource.Kind != original.Kind || resource.PlaybackObjectKey != original.PlaybackObjectKey || resource.UploadKey != nil || resource.PublicURL != "" {
				return gorm.ErrInvalidData
			}
			newIDs[resource.ID] = true
			resources = append(resources, resource)
		}
		if len(resources) > 0 {
			if err := tx.CreateInBatches(resources, 200).Error; err != nil {
				return err
			}
		}
		stored, err := repo.UserStoredFileBytes(userID)
		if err != nil {
			return err
		}
		var pending int64
		if err := tx.Model(&model.UploadReservation{}).Select("COALESCE(SUM(size),0)").Where("user_id = ? AND expires_at > ?", userID, time.Now()).Scan(&pending).Error; err != nil {
			return err
		}
		if stored >= storageLimit || pending >= storageLimit-stored {
			return ErrUploadStorageLimit
		}
		project.Revision = 1
		if err := tx.Create(project).Error; err != nil {
			return err
		}
		result = model.CanvasShareCloneReceipt{UserID: userID, RequestID: requestID, ShareID: featuredID, SnapshotID: snapshotID, CanvasID: project.ID, CreatedAt: time.Now()}
		if err := tx.Create(&result).Error; err != nil {
			return err
		}
		repo.canvasChanged(userID, project.ID)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}
