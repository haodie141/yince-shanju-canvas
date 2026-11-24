package canvas

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"yingce/backend/internal/assets"
	"yingce/backend/internal/kernel"
	"yingce/backend/internal/model"
)

// CloneCanvasShare builds and commits a clone of the version the visitor saw.
// A regular share and a platform featured copy use the same public token route;
// featured copies stay clonable after the source canvas is deleted but stop
// working when the source share is explicitly closed or loses copy permission.
func (s *Service) CloneCanvasShare(userID, token, snapshotID, requestID string) (string, error) {
	token = strings.TrimSpace(token)
	if userID == "" || len(token) < 32 || len(token) > 128 || snapshotID == "" || requestID == "" {
		return "", kernel.BadAuthRequest("分享或克隆请求无效")
	}
	access, err := s.resolvePublicCanvasAccess(token, snapshotID)
	if err != nil {
		return "", err
	}
	if !access.allowCopy {
		return "", kernel.BadAuthRequest("分享者已关闭保存副本权限")
	}
	quota, err := s.host.CanvasQuotaCheck(userID)
	if err != nil {
		return "", err
	}
	limit, err := s.host.CanvasCloneStorageLimit(userID)
	if err != nil {
		return "", err
	}
	hash := canvasShareTokenHash(token)
	build := func(snapshot model.CanvasShareSnapshot, pins []model.CanvasShareSnapshotResource) (*model.CanvasProject, map[string]model.Resource, error) {
		source := &model.CanvasProject{ID: snapshot.CanvasID, UserID: snapshot.UserID, Title: snapshot.Title, PayloadJSON: snapshot.PayloadJSON, Revision: snapshot.SourceRevision}
		if issues := canvasShareCopyIssues(source, true); len(issues) > 0 {
			return nil, nil, kernel.BadAuthRequest(fmt.Sprintf("画布暂不能完整复制：%s", strings.Join(issues, "；")))
		}
		now := time.Now()
		aliases := make(map[string]model.Resource, len(pins))
		aliasIDs := make(map[string]string, len(pins))
		for _, pin := range pins {
			id := kernel.NewID()
			resource, err := cloneShareResource(snapshot, pin, userID, id, now)
			if err != nil {
				return nil, nil, err
			}
			aliases[pin.ResourceID], aliasIDs[pin.ResourceID] = *resource, id
		}
		id := kernel.NewID()
		payload, err := prepareShareClone(source, id, aliasIDs)
		if err != nil {
			return nil, nil, err
		}
		return &model.CanvasProject{ID: id, UserID: userID, Title: snapshot.Title, PayloadJSON: string(payload), CreatedAt: now, UpdatedAt: now}, aliases, nil
	}
	var canvasID string
	err = s.host.WithStorageLock(func() error {
		var receipt *model.CanvasShareCloneReceipt
		if access.featured != nil {
			receipt, err = s.repo.CreateFeaturedCanvasClone(userID, requestID, access.featured.ID, access.snapshotID, hash, limit, build, quota)
		} else {
			receipt, err = s.repo.CreateCanvasShareClone(userID, requestID, access.share.ID, access.snapshotID, hash, limit, build, quota)
		}
		if err != nil {
			return err
		}
		canvasID = receipt.CanvasID
		return nil
	})
	return canvasID, err
}

// prepareShareClone builds an independent canvas document from the public
// projection of a frozen snapshot. Resource aliases must already be reserved
// for the recipient in the same transaction that persists the returned data.
func prepareShareClone(snapshot *model.CanvasProject, canvasID string, resourceAliases map[string]string) ([]byte, error) {
	if canvasID == "" || assets.ValidID(canvasID) != canvasID {
		return nil, errors.New("克隆画布标识无效")
	}
	const projectionToken = "clone-projection"
	document, resources, err := publicCanvasProject(snapshot, projectionToken)
	if err != nil {
		return nil, err
	}
	urls := make(map[string]string, len(resources))
	used := make(map[string]bool, len(resources))
	for oldID := range resources {
		newID := resourceAliases[oldID]
		if newID == "" || assets.ValidID(newID) != newID || newID == oldID || used[newID] {
			return nil, errors.New("克隆素材引用不完整或无效")
		}
		urls[sharedCanvasResourceURL(projectionToken, oldID)] = newID
		used[newID] = true
	}
	document = rewriteCloneResources(document, "", urls).(map[string]any)
	document["id"] = canvasID
	return json.Marshal(document)
}

// cloneShareResource creates a recipient-owned alias for a frozen file. The
// caller must authorize the storage location and commit the alias with the
// cloned canvas while holding the object-lifetime lock.
func cloneShareResource(snapshot model.CanvasShareSnapshot, pin model.CanvasShareSnapshotResource, recipientID, resourceID string, now time.Time) (*model.Resource, error) {
	if recipientID == "" || resourceID == "" || assets.ValidID(recipientID) != recipientID || assets.ValidID(resourceID) != resourceID || pin.ResourceID == resourceID {
		return nil, errors.New("克隆素材标识无效")
	}
	var resource model.Resource
	if err := json.Unmarshal([]byte(pin.ResourceJSON), &resource); err != nil || snapshot.ID == "" || snapshot.UserID == "" || pin.SnapshotID != snapshot.ID || resource.UserID != snapshot.UserID || resource.ID != pin.ResourceID || resource.Status != model.ResourceStatusReady || resource.ObjectKey == "" ||
		resource.Provider != pin.Provider || resource.Endpoint != pin.Endpoint || resource.Bucket != pin.Bucket || resource.ObjectKey != pin.ObjectKey {
		return nil, errors.New("分享素材记录无效")
	}
	resource.ID = resourceID
	resource.UserID = recipientID
	resource.StorageSettingID = pin.StorageSettingID
	resource.UploadKey = nil
	resource.PublicURL = ""
	resource.Error = ""
	resource.CreatedAt = now
	resource.UpdatedAt = now
	return &resource, nil
}

func rewriteCloneResources(value any, key string, urls map[string]string) any {
	switch item := value.(type) {
	case string:
		if id := urls[item]; id != "" {
			if key == "content" || key == "previewContent" {
				return assets.FileURL(id)
			}
			return "resource:" + id
		}
		return item
	case []any:
		result := make([]any, len(item))
		for i, child := range item {
			result[i] = rewriteCloneResources(child, key, urls)
		}
		return result
	case map[string]any:
		result := make(map[string]any, len(item)+1)
		for name, child := range item {
			result[name] = rewriteCloneResources(child, name, urls)
		}
		if content, ok := item["content"].(string); ok && urls[content] != "" {
			result["storageKey"] = "resource:" + urls[content]
		}
		return result
	default:
		return value
	}
}
