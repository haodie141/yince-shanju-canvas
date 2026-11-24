package canvas

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
	"yingce/backend/internal/assets"
	"yingce/backend/internal/kernel"

	"yingce/backend/internal/model"

	"gorm.io/gorm"
)

type CanvasShareRequest struct {
	ExpiresDays    int  `json:"expiresDays"`
	Rotate         bool `json:"rotate"`
	AllowCopy      bool `json:"allowCopy"`
	PreserveExpiry bool `json:"preserveExpiry"`
}

type CanvasShareStatus struct {
	Enabled    bool       `json:"enabled"`
	AllowCopy  bool       `json:"allowCopy"`
	Token      string     `json:"token,omitempty"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	CreatedAt  *time.Time `json:"createdAt,omitempty"`
	SnapshotAt *time.Time `json:"snapshotAt,omitempty"`
}

type PublicCanvasShare struct {
	Project    map[string]any `json:"project"`
	AllowCopy  bool           `json:"allowCopy"`
	CopyIssues []string       `json:"copyIssues,omitempty"`
	ExpiresAt  *time.Time     `json:"expiresAt,omitempty"`
	SnapshotID string         `json:"snapshotId,omitempty"`
}

var publicCanvasMetadataKeys = map[string]bool{
	"content": true, "textFormat": true, "composerContent": true, "prompt": true, "status": true, "fontSize": true,
	"generationMode": true, "generationType": true, "model": true, "size": true, "quality": true, "transparentBackground": true,
	"count": true, "seconds": true, "vquality": true, "generateAudio": true, "watermark": true,
	"audioVoice": true, "audioFormat": true, "audioSpeed": true, "audioInstructions": true,
	"naturalWidth": true, "naturalHeight": true, "freeResize": true, "isBatchRoot": true,
	"batchRootId": true, "batchChildIds": true, "batchUsesReferenceImages": true, "primaryImageId": true,
	"imageBatchExpanded": true, "mimeType": true, "bytes": true, "durationMs": true, "hasAudio": true, "assetTags": true,
	"workflowKind": true, "workflowTitle": true, "workflowDescription": true, "shotIndex": true,
	"sceneId": true, "characterIds": true, "referenceSetId": true, "referenceAssetNodeIds": true, "assetBindings": true,
	"characterName": true, "characterPrompt": true, "characterAliases": true, "characterView": true, "characterViewNodeIds": true,
	"videoEditOperation": true, "videoCameraMoveId": true, "videoCameraMovePrompt": true,
	"videoStartFrameNodeId": true, "videoEndFrameNodeId": true, "versionOfNodeId": true,
	"versionLabel": true, "versionPrimary": true, "previsSceneId": true, "previsShotId": true,
	"previsPreviewNodeId": true, "previsDepthNodeId": true, "previsDepthVideoNodeId": true, "previsNormalNodeId": true,
	"skillId": true, "skillVersion": true, "skillSnapshot": true, "storyboard": true,
	"storyboardShotDuration": true, "storyboardShotCount": true, "storyboardComposerHeight": true, "frame": true,
	"mediaVersions": true, "currentMediaVersionId": true, "videoPreview": true, "references": true,
}

var publicCanvasForbiddenKeys = map[string]bool{
	"apiKey": true, "storageKey": true, "taskId": true, "taskStatus": true, "taskProgress": true,
	"taskStage": true, "taskCreatedAt": true, "taskUpdatedAt": true,
	"errorDetails": true, "references": true,
}

func (s *Service) CanvasShareStatus(userID string, projectID string) (CanvasShareStatus, error) {
	if _, err := s.repo.CanvasProjectForUser(userID, projectID); err != nil {
		return CanvasShareStatus{}, err
	}
	share, err := s.repo.CanvasShareForProject(userID, projectID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return CanvasShareStatus{Enabled: false}, nil
	}
	if err != nil {
		return CanvasShareStatus{}, err
	}
	return s.canvasShareStatus(share)
}

func (s *Service) CreateCanvasShare(userID string, projectID string, req CanvasShareRequest) (CanvasShareStatus, error) {
	if req.ExpiresDays < 0 || req.ExpiresDays > 365 {
		return CanvasShareStatus{}, kernel.BadAuthRequest("分享有效期必须在 0 到 365 天之间")
	}
	project, err := s.repo.CanvasProjectForUser(userID, projectID)
	if err != nil {
		return CanvasShareStatus{}, err
	}
	prepared, err := prepareShareSnapshot(project)
	if err != nil {
		return CanvasShareStatus{}, err
	}
	share, err := s.repo.CanvasShareForProject(userID, projectID)
	var expected *model.CanvasShare
	if errors.Is(err, gorm.ErrRecordNotFound) {
		share = &model.CanvasShare{ID: kernel.NewID(), UserID: userID, ProjectID: projectID}
	} else if err != nil {
		return CanvasShareStatus{}, err
	} else {
		copy := *share
		expected = &copy
	}
	if req.Rotate || strings.TrimSpace(share.TokenCipher) == "" {
		token, tokenErr := newCanvasShareToken()
		if tokenErr != nil {
			return CanvasShareStatus{}, tokenErr
		}
		share.TokenHash = canvasShareTokenHash(token)
		share.TokenCipher, err = s.host.EncryptSecret(token)
		if err != nil {
			return CanvasShareStatus{}, err
		}
	}
	share.Enabled = true
	share.AllowCopy = req.AllowCopy
	share.RevokedAt = nil
	share.CanvasDeletedAt = nil
	if !req.PreserveExpiry || expected == nil {
		share.ExpiresAt = nil
		if req.ExpiresDays > 0 {
			expiresAt := time.Now().Add(time.Duration(req.ExpiresDays) * 24 * time.Hour)
			share.ExpiresAt = &expiresAt
		}
	}
	snapshot := &model.CanvasShareSnapshot{ID: kernel.NewID(), ShareID: share.ID, UserID: userID, CanvasID: projectID, SourceRevision: prepared.Project.Revision, Title: prepared.Project.Title, PayloadJSON: prepared.Project.PayloadJSON, CreatedAt: time.Now()}
	if err := s.repo.SaveCanvasShareAndPublish(share, expected, snapshot, prepared.ResourceIDs); err != nil {
		return CanvasShareStatus{}, err
	}
	share.CurrentSnapshotID = &snapshot.ID
	return s.canvasShareStatus(share)
}

func (s *Service) DeleteCanvasShare(userID string, projectID string) error {
	if _, err := s.repo.CanvasProjectForUser(userID, projectID); err != nil {
		return err
	}
	return s.repo.DeleteCanvasShare(userID, projectID)
}

func (s *Service) SetCanvasShareCopyPermission(userID string, projectID string, allow bool) (CanvasShareStatus, error) {
	if _, err := s.repo.CanvasProjectForUser(userID, projectID); err != nil {
		return CanvasShareStatus{}, err
	}
	share, err := s.repo.SetCanvasShareCopyPermission(userID, projectID, allow)
	if err != nil {
		return CanvasShareStatus{}, err
	}
	return s.canvasShareStatus(share)
}

func (s *Service) PublicCanvasShare(token string) (PublicCanvasShare, error) {
	return s.PublicCanvasShareVersion(token, "")
}

type publicCanvasAccess struct {
	share      *model.CanvasShare
	featured   *model.FeaturedCanvas
	snapshot   *model.CanvasShareSnapshot
	snapshotID string
	project    *model.CanvasProject
	allowCopy  bool
	expiresAt  *time.Time
}

// resolvePublicCanvasAccess accepts both regular share tokens and platform
// featured-copy tokens. A featured copy with a ready snapshot stays readable
// after the source canvas is deleted; if the source share still exists and was
// explicitly closed, expired or had copy permission revoked, access is denied.
func (s *Service) resolvePublicCanvasAccess(token string, snapshotID string) (*publicCanvasAccess, error) {
	token = strings.TrimSpace(token)
	if len(token) < 32 || len(token) > 128 {
		return nil, gorm.ErrRecordNotFound
	}
	hash := canvasShareTokenHash(token)
	share, err := s.repo.CanvasShareByTokenHash(hash)
	if err == nil {
		if !share.Enabled || (share.ExpiresAt != nil && !share.ExpiresAt.After(time.Now())) {
			return nil, gorm.ErrRecordNotFound
		}
		access := &publicCanvasAccess{share: share, allowCopy: share.AllowCopy, expiresAt: share.ExpiresAt}
		if snapshotID != "" {
			if assets.ValidID(snapshotID) != snapshotID {
				return nil, gorm.ErrRecordNotFound
			}
			snapshot, err := s.repo.CanvasShareSnapshot(share, snapshotID)
			if err != nil {
				return nil, err
			}
			access.snapshot, access.snapshotID, access.project = snapshot, snapshot.ID, canvasSnapshotProject(snapshot)
			return access, nil
		}
		if share.CurrentSnapshotID != nil {
			snapshot, err := s.repo.CanvasShareSnapshot(share, *share.CurrentSnapshotID)
			if err != nil {
				return nil, err
			}
			access.snapshot, access.snapshotID, access.project = snapshot, snapshot.ID, canvasSnapshotProject(snapshot)
			return access, nil
		}
		// Legacy links remain readable until their first explicit snapshot migration.
		project, err := s.repo.CanvasProjectForUser(share.UserID, share.ProjectID)
		if err != nil {
			return nil, err
		}
		access.project = project
		return access, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	entry, err := s.repo.FeaturedCopyByTokenHash(hash)
	if err != nil {
		return nil, err
	}
	if !entry.Enabled || !entry.PersistCopy || entry.CopyStatus != "ready" || entry.CopySnapshotID == "" {
		return nil, gorm.ErrRecordNotFound
	}
	if snapshotID != "" && snapshotID != entry.CopySnapshotID {
		return nil, gorm.ErrRecordNotFound
	}
	snapshot, err := s.repo.FeaturedSnapshot(entry.CopySnapshotID)
	if err != nil {
		return nil, gorm.ErrRecordNotFound
	}
	sourceShare, err := s.repo.CanvasShareByID(snapshot.ShareID)
	if err == nil {
		if sourceShare.RevokedAt != nil {
			return nil, gorm.ErrRecordNotFound
		}
		if sourceShare.CanvasDeletedAt == nil {
			if !sourceShare.Enabled || !sourceShare.AllowCopy || (sourceShare.ExpiresAt != nil && !sourceShare.ExpiresAt.After(time.Now())) {
				return nil, gorm.ErrRecordNotFound
			}
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	synthetic := &model.CanvasShare{ID: snapshot.ShareID, UserID: snapshot.UserID, ProjectID: snapshot.CanvasID, Enabled: true, AllowCopy: true, CurrentSnapshotID: &snapshot.ID}
	return &publicCanvasAccess{share: synthetic, featured: entry, snapshot: snapshot, snapshotID: snapshot.ID, project: canvasSnapshotProject(snapshot), allowCopy: true}, nil
}

func (s *Service) PublicCanvasShareVersion(token, snapshotID string) (PublicCanvasShare, error) {
	access, err := s.resolvePublicCanvasAccess(token, snapshotID)
	if err != nil {
		return PublicCanvasShare{}, err
	}
	resourceScope := token
	if access.snapshotID != "" {
		resourceScope += "/snapshots/" + access.snapshotID
	}
	publicProject, _, err := publicCanvasProject(access.project, resourceScope)
	if err != nil {
		return PublicCanvasShare{}, err
	}
	issues := canvasShareCopyIssues(access.project, access.allowCopy)
	result := PublicCanvasShare{Project: publicProject, AllowCopy: access.allowCopy, CopyIssues: issues, ExpiresAt: access.expiresAt, SnapshotID: access.snapshotID}
	return result, nil
}

func canvasShareCopyIssues(project *model.CanvasProject, allowCopy bool) []string {
	var issues []string
	if allowCopy {
		var source map[string]any
		if json.Unmarshal([]byte(project.PayloadJSON), &source) == nil {
			nodes, _ := source["nodes"].([]any)
			for _, raw := range nodes {
				node, _ := raw.(map[string]any)
				kind := kernel.StringValue(node["type"])
				meta, _ := node["metadata"].(map[string]any)
				title := kernel.DefaultString(kernel.StringValue(node["title"]), "未命名节点")
				if kind == "drawing" {
					issues = append(issues, title+"：绘图内容暂不支持复制")
					continue
				}
				if kind != "image" && kind != "video" && kind != "audio" && kind != "text" && kind != "frame" && kind != "script" {
					issues = append(issues, title+"：此类节点暂不支持完整复制")
					continue
				}
				for _, key := range []string{"subtitleEntries", "imageLayerGroup", "generationBatches", "drawingId"} {
					if meta[key] != nil {
						issues = append(issues, title+"：节点包含暂不支持复制的内容")
						break
					}
				}
				if versions, ok := meta["mediaVersions"].([]any); ok {
					for _, version := range versions {
						entry, _ := version.(map[string]any)
						media, _ := entry["media"].(map[string]any)
						if hasUnshareableMedia(media) {
							issues = append(issues, title+"：历史版本素材尚未保存到云端")
							break
						}
					}
				}
				if preview, ok := meta["videoPreview"].(map[string]any); ok && hasUnshareableMedia(preview) {
					issues = append(issues, title+"：视频预览素材尚未保存到云端")
				}
				for key, value := range meta {
					if key == "content" || key == "storageKey" {
						continue
					}
					encoded, _ := json.Marshal(value)
					if strings.Contains(string(encoded), "resource:") || strings.Contains(string(encoded), "/api/resources/") {
						if publicCanvasMetadataKeys[key] {
							continue
						}
						issues = append(issues, title+"：节点内的附加素材暂不支持复制")
						break
					}
				}
				if kind != "image" && kind != "video" && kind != "audio" {
					continue
				}
				key, content := kernel.StringValue(meta["storageKey"]), kernel.StringValue(meta["content"])
				if (key != "" || content != "") && assets.ResourceID(key) == "" && assets.ResourceID(content) == "" {
					issues = append(issues, title+"：素材尚未保存到云端或无法识别")
				}
			}
		}
	}
	return issues
}

func hasUnshareableMedia(media map[string]any) bool {
	key, content := kernel.StringValue(media["storageKey"]), kernel.StringValue(media["content"])
	if (key != "" || content != "") && assets.ResourceID(key) == "" && assets.ResourceID(content) == "" {
		return true
	}
	for _, field := range []string{"videoPreview", "imageRotation", "original", "variants"} {
		if nested, ok := media[field].(map[string]any); ok {
			if hasUnshareableMedia(nested) {
				return true
			}
			for _, value := range nested {
				if child, ok := value.(map[string]any); ok && hasUnshareableMedia(child) {
					return true
				}
			}
		}
	}
	return false
}

func (s *Service) OpenSharedCanvasResource(token string, resourceID string) (*model.Resource, io.ReadCloser, error) {
	stream, err := s.OpenSharedCanvasResourceRange(token, resourceID, "")
	if err != nil {
		return nil, nil, err
	}
	return stream.Resource, stream.Body, nil
}

func (s *Service) OpenSharedCanvasResourceRange(token string, resourceID string, rangeHeader string) (*assets.ResourceStream, error) {
	userID, resource, _, err := s.sharedCanvasResource(token, resourceID)
	if err != nil {
		return nil, err
	}
	return s.host.OpenResourceRange(userID, resource, rangeHeader)
}

func (s *Service) PrepareSharedCanvasResourceDelivery(token string, resourceID string, options assets.AccessOptions, rangeHeader string) (*assets.ResourceDelivery, error) {
	userID, resource, share, err := s.sharedCanvasResource(token, resourceID)
	if err != nil {
		return nil, err
	}
	if share.ExpiresAt != nil {
		options.ExpiresAt = *share.ExpiresAt
	}
	return s.host.PrepareResourceDelivery(userID, resource, options, rangeHeader)
}

// PrepareSharedCanvasSnapshotResourceDelivery keeps media requests bound to
// the version already opened by the visitor, even when the share is updated.
func (s *Service) PrepareSharedCanvasSnapshotResourceDelivery(token, snapshotID, resourceID string, options assets.AccessOptions, rangeHeader string) (*assets.ResourceDelivery, error) {
	if assets.ValidID(snapshotID) != snapshotID || snapshotID == "" || assets.ValidID(resourceID) != resourceID || resourceID == "" {
		return nil, gorm.ErrRecordNotFound
	}
	access, err := s.resolvePublicCanvasAccess(token, snapshotID)
	if err != nil {
		return nil, err
	}
	if access.snapshot == nil {
		return nil, gorm.ErrRecordNotFound
	}
	_, allowed, err := publicCanvasProject(access.project, token)
	if err != nil {
		return nil, err
	}
	if !allowed[resourceID] {
		return nil, gorm.ErrRecordNotFound
	}
	pin, err := s.repo.CanvasShareSnapshotResource(snapshotID, resourceID)
	if err != nil {
		return nil, err
	}
	var resource model.Resource
	if json.Unmarshal([]byte(pin.ResourceJSON), &resource) != nil || resource.ID != resourceID || resource.UserID != access.share.UserID || resource.Status != model.ResourceStatusReady || resource.Provider != pin.Provider || resource.Endpoint != pin.Endpoint || resource.Bucket != pin.Bucket || resource.ObjectKey != pin.ObjectKey {
		return nil, gorm.ErrRecordNotFound
	}
	resource.StorageSettingID = pin.StorageSettingID
	if access.expiresAt != nil {
		options.ExpiresAt = *access.expiresAt
	}
	return s.host.PrepareResourceDelivery(access.share.UserID, &resource, options, rangeHeader)
}

func (s *Service) sharedCanvasResource(token string, resourceID string) (string, *model.Resource, *model.CanvasShare, error) {
	share, project, err := s.sharedCanvasProject(token)
	if err != nil {
		return "", nil, nil, err
	}
	_, allowedResources, err := publicCanvasProject(project, token)
	if err != nil {
		return "", nil, nil, err
	}
	if !allowedResources[resourceID] {
		return "", nil, nil, gorm.ErrRecordNotFound
	}
	resource, err := s.repo.ResourceForUser(share.UserID, resourceID)
	if err != nil {
		return "", nil, nil, err
	}
	return share.UserID, resource, share, nil
}

func (s *Service) sharedCanvasProject(token string) (*model.CanvasShare, *model.CanvasProject, error) {
	access, err := s.resolvePublicCanvasAccess(token, "")
	if err != nil {
		return nil, nil, err
	}
	return access.share, access.project, nil
}

func canvasSnapshotProject(snapshot *model.CanvasShareSnapshot) *model.CanvasProject {
	return &model.CanvasProject{ID: snapshot.CanvasID, UserID: snapshot.UserID, Title: snapshot.Title, PayloadJSON: snapshot.PayloadJSON, Revision: snapshot.SourceRevision, UpdatedAt: snapshot.CreatedAt}
}

func (s *Service) canvasShareStatus(share *model.CanvasShare) (CanvasShareStatus, error) {
	if share == nil || !share.Enabled || (share.ExpiresAt != nil && !share.ExpiresAt.After(time.Now())) {
		return CanvasShareStatus{Enabled: false}, nil
	}
	token, err := s.host.DecryptSecret(share.TokenCipher)
	if err != nil {
		return CanvasShareStatus{}, err
	}
	createdAt := share.CreatedAt
	result := CanvasShareStatus{Enabled: true, AllowCopy: share.AllowCopy, Token: token, ExpiresAt: share.ExpiresAt, CreatedAt: &createdAt}
	if share.CurrentSnapshotID != nil {
		snapshot, err := s.repo.CanvasShareSnapshot(share, *share.CurrentSnapshotID)
		if err != nil {
			return CanvasShareStatus{}, err
		}
		result.SnapshotAt = &snapshot.CreatedAt
	}
	return result, nil
}

func newCanvasShareToken() (string, error) {
	payload := make([]byte, 32)
	if _, err := rand.Read(payload); err != nil {
		return "", fmt.Errorf("生成分享令牌失败：%w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func canvasShareTokenHash(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func publicCanvasProject(project *model.CanvasProject, token string) (map[string]any, map[string]bool, error) {
	var source map[string]any
	if project == nil || json.Unmarshal([]byte(project.PayloadJSON), &source) != nil {
		return nil, nil, errors.New("分享画布数据格式无效")
	}
	allowedResources := map[string]bool{}
	result := map[string]any{
		"id":             kernel.StringValue(source["id"]),
		"title":          kernel.DefaultString(kernel.StringValue(source["title"]), project.Title),
		"createdAt":      source["createdAt"],
		"updatedAt":      source["updatedAt"],
		"backgroundMode": source["backgroundMode"],
		"appearance":     scrubPublicCanvasValue(source["appearance"]),
		"showImageInfo":  source["showImageInfo"],
		"viewport":       scrubPublicCanvasValue(source["viewport"]),
		"connections":    publicCanvasConnections(source["connections"]),
		"chatSessions":   []any{},
		"activeChatId":   nil,
		"previsScenes":   []any{},
	}
	rawNodes, _ := source["nodes"].([]any)
	nodes := make([]any, 0, len(rawNodes))
	for _, rawNode := range rawNodes {
		if node, ok := publicCanvasNode(rawNode, token, allowedResources); ok {
			nodes = append(nodes, node)
		}
	}
	result["nodes"] = nodes
	return result, allowedResources, nil
}

func publicCanvasConnections(value any) []any {
	rawConnections, _ := value.([]any)
	result := make([]any, 0, len(rawConnections))
	for _, raw := range rawConnections {
		connection, _ := raw.(map[string]any)
		if connection == nil {
			continue
		}
		result = append(result, map[string]any{
			"id": connection["id"], "fromNodeId": connection["fromNodeId"], "toNodeId": connection["toNodeId"],
			"fromHandleId": connection["fromHandleId"], "toHandleId": connection["toHandleId"],
		})
	}
	return result
}

func publicCanvasNode(value any, token string, allowedResources map[string]bool) (map[string]any, bool) {
	node, _ := value.(map[string]any)
	if node == nil || kernel.StringValue(node["id"]) == "" || kernel.StringValue(node["type"]) == "" {
		return nil, false
	}
	result := map[string]any{
		"id": node["id"], "type": node["type"], "title": node["title"], "position": scrubPublicCanvasValue(node["position"]),
		"width": node["width"], "height": node["height"], "parentId": node["parentId"],
	}
	metadata, _ := node["metadata"].(map[string]any)
	publicMetadata := map[string]any{}
	for key, value := range metadata {
		if !publicCanvasMetadataKeys[key] {
			continue
		}
		publicMetadata[key] = publicCanvasMetadataValue(value, token, allowedResources)
	}
	if resourceID := assets.ResourceID(kernel.StringValue(metadata["storageKey"])); resourceID != "" {
		allowedResources[resourceID] = true
		publicMetadata["content"] = sharedCanvasResourceURL(token, resourceID)
	} else if resourceID := assets.ResourceID(kernel.StringValue(metadata["content"])); resourceID != "" {
		allowedResources[resourceID] = true
		publicMetadata["content"] = sharedCanvasResourceURL(token, resourceID)
	} else if nodeType := kernel.StringValue(node["type"]); nodeType == "image" || nodeType == "video" || nodeType == "audio" {
		delete(publicMetadata, "content")
	}
	delete(publicMetadata, "storageKey")
	result["metadata"] = publicMetadata
	return result, true
}

// Only metadata explicitly included in the public projection reaches this
// function. Rewrite embedded owned resources to share-scoped URLs; never pass
// the owner's storage keys or task identifiers to the visitor.
func publicCanvasMetadataValue(value any, token string, allowedResources map[string]bool) any {
	switch item := value.(type) {
	case map[string]any:
		result := map[string]any{}
		for key, child := range item {
			if publicCanvasForbiddenKeys[key] && key != "references" {
				continue
			}
			if key == "previewContent" && assets.ResourceID(kernel.StringValue(child)) == "" {
				continue
			}
			result[key] = publicCanvasMetadataValue(child, token, allowedResources)
		}
		if id := assets.ResourceID(kernel.StringValue(item["storageKey"])); id != "" {
			allowedResources[id] = true
			result["content"] = sharedCanvasResourceURL(token, id)
		} else if content := kernel.StringValue(item["content"]); strings.HasPrefix(content, "blob:") || strings.HasPrefix(content, "data:") {
			delete(result, "content")
		}
		return result
	case []any:
		result := make([]any, len(item))
		for i, child := range item {
			result[i] = publicCanvasMetadataValue(child, token, allowedResources)
		}
		return result
	case string:
		if id := assets.ResourceID(item); id != "" {
			allowedResources[id] = true
			return sharedCanvasResourceURL(token, id)
		}
		return item
	default:
		return item
	}
}

func scrubPublicCanvasValue(value any) any {
	switch item := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(item))
		for key, child := range item {
			if publicCanvasForbiddenKeys[key] {
				continue
			}
			result[key] = scrubPublicCanvasValue(child)
		}
		return result
	case []any:
		result := make([]any, len(item))
		for index, child := range item {
			result[index] = scrubPublicCanvasValue(child)
		}
		return result
	default:
		return value
	}
}

func sharedCanvasResourceURL(token string, resourceID string) string {
	return "/api/public/canvas-shares/" + token + "/resources/" + resourceID + "/file"
}
