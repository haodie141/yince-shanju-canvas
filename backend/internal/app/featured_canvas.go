package app

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"
	"yingce/backend/internal/assets"
	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

const (
	featuredVideoSourceUpload = "upload"
	featuredVideoSourceShare  = "share"

	featuredStatusOK               = "ok"
	featuredStatusShareUnavailable = "share_unavailable"
	featuredStatusVideoUnavailable = "video_unavailable"
)

type FeaturedCanvasRequest struct {
	Title            string `json:"title"`
	Description      string `json:"description"`
	Category         string `json:"category"`
	VideoResourceID  string `json:"videoResourceId"`
	VideoSource      string `json:"videoSource"`
	CoverResourceID  string `json:"coverResourceId"`
	CoverSource      string `json:"coverSource"`
	ShareToken       string `json:"shareToken"`
	SnapshotID       string `json:"snapshotId"`
	Enabled          bool   `json:"enabled"`
	PersistCopy      bool   `json:"persistCopy"`
	ConsentConfirmed bool   `json:"consentConfirmed"`
}

type FeaturedCanvasView struct {
	ID             string `json:"id"`
	Title          string `json:"title"`
	Description    string `json:"description,omitempty"`
	Category       string `json:"category,omitempty"`
	VideoURL       string `json:"videoUrl,omitempty"`
	CoverURL       string `json:"coverUrl,omitempty"`
	CoverAvailable bool   `json:"coverAvailable"`
	CanvasURL      string `json:"canvasUrl,omitempty"`
	// SourceCanvasURL 只在管理员视图出现：持久副本就绪时 CanvasURL 指向副本链接，
	// 管理员编辑仍需要用源分享链接重新识别分享版本与素材。
	SourceCanvasURL   string     `json:"sourceCanvasUrl,omitempty"`
	VideoResourceID   string     `json:"videoResourceId,omitempty"`
	VideoSource       string     `json:"videoSource,omitempty"`
	CoverResourceID   string     `json:"coverResourceId,omitempty"`
	CoverSource       string     `json:"coverSource,omitempty"`
	CoverError        string     `json:"coverError,omitempty"`
	SnapshotID        string     `json:"snapshotId,omitempty"`
	CopySnapshotID    string     `json:"copySnapshotId,omitempty"`
	PersistCopy       bool       `json:"persistCopy"`
	CopyStatus        string     `json:"copyStatus"`
	CopySavedAt       *time.Time `json:"copySavedAt,omitempty"`
	CopyError         string     `json:"copyError,omitempty"`
	SourceShareStatus string     `json:"sourceShareStatus"` // ok|deleted|revoked|expired|copy_forbidden|missing
	ConsentAt         *time.Time `json:"consentAt,omitempty"`
	Enabled           bool       `json:"enabled"`
	SortOrder         int        `json:"sortOrder"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
	SnapshotAt        *time.Time `json:"snapshotAt,omitempty"`
	ShareEnabled      bool       `json:"shareEnabled"`
	AllowCopy         bool       `json:"allowCopy"`
	ShareExpiresAt    *time.Time `json:"shareExpiresAt,omitempty"`
	VideoAvailable    bool       `json:"videoAvailable"`
	Status            string     `json:"status"`
	StatusMessage     string     `json:"statusMessage,omitempty"`
}

type FeaturedShareAsset struct {
	ResourceID string `json:"resourceId"`
	Title      string `json:"title"`
	Available  bool   `json:"available"`
	Reason     string `json:"reason,omitempty"`
	URL        string `json:"url,omitempty"`
}

type FeaturedShareVideo = FeaturedShareAsset
type FeaturedShareImage = FeaturedShareAsset

type FeaturedSharePreview struct {
	Status            string               `json:"status"`
	Message           string               `json:"message,omitempty"`
	Title             string               `json:"title,omitempty"`
	ShareID           string               `json:"shareId,omitempty"`
	SnapshotID        string               `json:"snapshotId,omitempty"`
	SnapshotAt        *time.Time           `json:"snapshotAt,omitempty"`
	CurrentSnapshotID string               `json:"currentSnapshotId,omitempty"`
	CurrentSnapshotAt *time.Time           `json:"currentSnapshotAt,omitempty"`
	AllowCopy         bool                 `json:"allowCopy"`
	ExpiresAt         *time.Time           `json:"expiresAt,omitempty"`
	Videos            []FeaturedShareVideo `json:"videos"`
	Images            []FeaturedShareImage `json:"images"`
}

type FeaturedSharePreviewRequest struct {
	ShareToken string `json:"shareToken"`
	SnapshotID string `json:"snapshotId"`
}

type FeaturedReorderItem struct {
	ID        string `json:"id"`
	UpdatedAt string `json:"updatedAt"`
}

type FeaturedReorderRequest struct {
	Items []FeaturedReorderItem `json:"items"`
}

// featuredCanvasView evaluates the share snapshot before rendering. Public
// callers only receive entries in the "ok" state; administrators receive every
// entry with an explicit reason so they can repair broken showcases.
func (s *Service) featuredCanvasView(entry model.FeaturedCanvas, admin bool) (FeaturedCanvasView, string, string) {
	copyStatus := strings.TrimSpace(entry.CopyStatus)
	if copyStatus == "" {
		copyStatus = "none"
	}
	view := FeaturedCanvasView{
		ID: entry.ID, Title: entry.Title, Description: entry.Description, Category: entry.Category,
		SnapshotID: entry.SnapshotID, PersistCopy: entry.PersistCopy, CopyStatus: copyStatus, CopySavedAt: entry.CopySavedAt,
		CopyError: entry.CopyError, SourceShareStatus: "missing", ConsentAt: entry.ConsentAt, Enabled: entry.Enabled, SortOrder: entry.SortOrder,
		CreatedAt: entry.CreatedAt, UpdatedAt: entry.UpdatedAt, Status: featuredStatusOK,
	}
	if admin {
		view.VideoResourceID = entry.VideoResourceID
		view.VideoSource = entry.VideoSource
		view.CoverResourceID = entry.CoverResourceID
		view.CoverSource = entry.CoverSource
		view.CopySnapshotID = entry.CopySnapshotID
	}
	resource, err := s.repo.Resource(entry.VideoResourceID)
	if err == nil && resource.Kind == "video" && resource.Status == model.ResourceStatusReady {
		view.VideoAvailable = true
		if admin {
			view.VideoURL = "/api/admin/featured/" + url.PathEscape(entry.ID) + "/video"
		} else {
			view.VideoURL = "/api/public/featured/" + url.PathEscape(entry.ID) + "/video"
		}
	}
	if entry.CoverResourceID != "" {
		cover, coverErr := s.repo.Resource(entry.CoverResourceID)
		if coverErr == nil && cover.Kind == "image" && cover.Status == model.ResourceStatusReady {
			view.CoverAvailable = true
			if admin {
				view.CoverURL = "/api/admin/featured/" + url.PathEscape(entry.ID) + "/cover"
			} else {
				view.CoverURL = "/api/public/featured/" + url.PathEscape(entry.ID) + "/cover"
			}
		} else if admin {
			view.CoverError = "封面素材不可用，请重新选择或上传"
		}
	}
	copyReady := false
	copyURL := ""
	if entry.PersistCopy && entry.CopyStatus == "ready" && entry.CopySnapshotID != "" {
		if _, err := s.repo.FeaturedSnapshot(entry.CopySnapshotID); err == nil {
			if token, decryptErr := s.decryptSettingSecret(entry.CopyTokenCipher); decryptErr == nil && token != "" {
				copyReady = true
				copyURL = "/share/canvas/" + url.PathEscape(token) + "?snapshot=" + url.QueryEscape(entry.CopySnapshotID)
			} else if admin {
				view.CopyStatus = "failed"
				view.CopyError = "无法读取持久副本访问令牌，请重新保存副本"
			}
		} else if admin {
			view.CopyStatus = "failed"
			view.CopyError = "持久副本快照已不存在"
		}
	}
	snapshot, err := s.repo.FeaturedSnapshot(entry.SnapshotID)
	if err != nil {
		view.SourceShareStatus = "missing"
		if copyReady && view.VideoAvailable {
			view.CanvasURL = copyURL
			return view, featuredStatusOK, ""
		}
		return view, featuredStatusShareUnavailable, "关联的分享快照已不存在"
	}
	snapshotAt := snapshot.CreatedAt
	view.SnapshotAt = &snapshotAt
	share, err := s.repo.CanvasShareByID(snapshot.ShareID)
	if err != nil {
		// 分享行缺失来自原画布被删除；持久副本仍可展示、播放和克隆。
		view.SourceShareStatus = "deleted"
		if copyReady && view.VideoAvailable {
			view.CanvasURL = copyURL
			return view, featuredStatusOK, ""
		}
		return view, featuredStatusShareUnavailable, "来源画布或分享已被删除；未保存持久副本，无法展示"
	}
	if snapshot.UserID != share.UserID || snapshot.CanvasID != share.ProjectID {
		view.SourceShareStatus = "missing"
		return view, featuredStatusShareUnavailable, "精选快照与分享归属不一致"
	}
	view.ShareEnabled = share.Enabled
	view.AllowCopy = share.AllowCopy
	view.ShareExpiresAt = share.ExpiresAt
	if share.RevokedAt != nil || (!share.Enabled && share.CanvasDeletedAt == nil) {
		view.SourceShareStatus = "revoked"
		return view, featuredStatusShareUnavailable, "分享已被分享者关闭，已按授权规则下架"
	}
	if share.CanvasDeletedAt != nil {
		view.SourceShareStatus = "deleted"
		if copyReady && view.VideoAvailable {
			view.CanvasURL = copyURL
			return view, featuredStatusOK, ""
		}
		return view, featuredStatusShareUnavailable, "来源画布已删除；未保存持久副本，无法展示"
	}
	if share.ExpiresAt != nil && !share.ExpiresAt.After(time.Now()) {
		view.SourceShareStatus = "expired"
		return view, featuredStatusShareUnavailable, "分享链接已过期，已按授权规则下架"
	}
	if !share.AllowCopy {
		view.SourceShareStatus = "copy_forbidden"
		return view, featuredStatusShareUnavailable, "分享已禁止保存副本，已按授权规则下架"
	}
	view.SourceShareStatus = "ok"
	token, err := s.decryptSettingSecret(share.TokenCipher)
	if err != nil {
		return view, featuredStatusShareUnavailable, "无法读取分享链接"
	}
	sourceURL := "/share/canvas/" + url.PathEscape(token) + "?snapshot=" + url.QueryEscape(entry.SnapshotID)
	if copyReady {
		view.CanvasURL = copyURL
	} else {
		view.CanvasURL = sourceURL
	}
	if admin {
		view.SourceCanvasURL = sourceURL
	}
	if !view.VideoAvailable {
		return view, featuredStatusVideoUnavailable, "精选视频不可用，请重新选择视频"
	}
	return view, featuredStatusOK, ""
}

func (s *Service) FeaturedCanvases(actor *model.User, admin bool) ([]FeaturedCanvasView, error) {
	if admin {
		if err := s.RequireAdmin(actor); err != nil {
			return nil, err
		}
	}
	entries, err := s.repo.FeaturedCanvases(!admin)
	if err != nil {
		return nil, err
	}
	result := make([]FeaturedCanvasView, 0, len(entries))
	for _, entry := range entries {
		view, status, message := s.featuredCanvasView(entry, admin)
		if status != featuredStatusOK && !admin {
			continue
		}
		view.Status = status
		view.StatusMessage = message
		result = append(result, view)
	}
	return result, nil
}

func newFeaturedCopyToken() (string, error) {
	payload := make([]byte, 32)
	if _, err := rand.Read(payload); err != nil {
		return "", fmt.Errorf("生成精选副本令牌失败：%w", err)
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func featuredCopyTokenHash(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

// prepareFeaturedCopy validates the snapshot's pinned media and copyability
// before marking the entry as durably saved. It never creates a partial copy:
// any unavailable, foreign or personal-OSS resource aborts the save.
func (s *Service) prepareFeaturedCopy(repo *repository.Repository, entry *model.FeaturedCanvas, snapshot *model.CanvasShareSnapshot, shareToken string) error {
	pins, err := repo.CanvasShareSnapshotResources(snapshot.ID)
	if err != nil {
		return err
	}
	for _, pin := range pins {
		resource, err := repo.Resource(pin.ResourceID)
		if err != nil || resource.UserID != snapshot.UserID || resource.Status != model.ResourceStatusReady ||
			resource.Provider != pin.Provider || resource.Endpoint != pin.Endpoint || resource.Bucket != pin.Bucket || resource.ObjectKey != pin.ObjectKey {
			return BadAuthRequest("持久副本包含不可用或不属于分享者的素材：" + pin.ResourceID)
		}
		if err := repo.AuthorizeShareCloneStorage(pin); err != nil {
			return BadAuthRequest("持久副本包含个人对象存储素材，无法安全保存；请让作者改用平台存储或移除该素材")
		}
	}
	public, err := s.canvasDomain().PublicCanvasShareVersion(shareToken, snapshot.ID)
	if err != nil {
		return BadAuthRequest("无法读取所选分享版本，不能保存持久副本")
	}
	if len(public.CopyIssues) > 0 {
		return BadAuthRequest("持久副本包含暂不支持完整复制的内容：" + strings.Join(public.CopyIssues, "；"))
	}
	if !entry.PersistCopy || entry.CopySnapshotID != snapshot.ID || entry.CopyTokenCipher == "" || entry.CopyTokenHash == "" {
		token, err := newFeaturedCopyToken()
		if err != nil {
			return err
		}
		cipher, err := s.encryptSettingSecret(token)
		if err != nil {
			return err
		}
		entry.CopyTokenHash, entry.CopyTokenCipher = featuredCopyTokenHash(token), cipher
	}
	now := time.Now()
	entry.PersistCopy, entry.CopyStatus, entry.CopySnapshotID = true, "ready", snapshot.ID
	entry.CopySavedAt, entry.CopyError = &now, ""
	if entry.ConsentAt == nil {
		entry.ConsentAt = &now
	}
	return nil
}

// cleanupFeaturedSnapshot removes a snapshot and its resource pins only when
// the source share no longer exists and no share history or other featured
// entry still references it.
func (s *Service) cleanupFeaturedSnapshot(snapshotID string) {
	if strings.TrimSpace(snapshotID) == "" {
		return
	}
	snapshot, err := s.repo.FeaturedSnapshot(snapshotID)
	if err != nil {
		return
	}
	share, shareErr := s.repo.CanvasShareByID(snapshot.ShareID)
	if shareErr == nil {
		// 仍处于正常分享状态的快照可能被旧链接引用，保留；已撤回、停用或
		// 原画布已删除的分享不再保护该快照。
		if share.Enabled && share.RevokedAt == nil && share.CanvasDeletedAt == nil {
			return
		}
	} else if !errors.Is(shareErr, gorm.ErrRecordNotFound) {
		return
	}
	if refs, err := s.repo.FeaturedCopySnapshotReferences(snapshotID); err == nil && refs == 0 {
		_ = s.repo.DeleteFeaturedCopySnapshot(snapshotID)
	}
}

// resolveFeaturedCover validates an optional cover image. Share covers must be
// pinned in the bound snapshot and belong to the sharer; uploaded covers must
// belong to the acting administrator. An empty ID means "use the video first
// frame" and clears the stored cover.
func (s *Service) resolveFeaturedCover(repo *repository.Repository, actorID, shareUserID, snapshotID, resourceID, requestedSource string) (string, error) {
	resourceID, requestedSource = strings.TrimSpace(resourceID), strings.TrimSpace(requestedSource)
	if resourceID == "" {
		return "", nil
	}
	if len(resourceID) > 36 || assets.ValidID(resourceID) != resourceID {
		return "", BadAuthRequest("封面素材标识无效")
	}
	if requestedSource != "" && requestedSource != featuredVideoSourceUpload && requestedSource != featuredVideoSourceShare {
		return "", BadAuthRequest("请选择有效的封面来源")
	}
	cover, err := repo.Resource(resourceID)
	if err != nil || cover.Kind != "image" || cover.Status != model.ResourceStatusReady {
		return "", BadAuthRequest("请选择已上传完成的封面图片")
	}
	source := requestedSource
	if source == "" {
		if cover.UserID == actorID {
			source = featuredVideoSourceUpload
		} else {
			source = featuredVideoSourceShare
		}
	}
	if source == featuredVideoSourceUpload {
		if cover.UserID != actorID {
			return "", BadAuthRequest("上传封面必须属于当前管理员账号")
		}
		return source, nil
	}
	if shareUserID == "" || cover.UserID != shareUserID {
		return "", BadAuthRequest("只能从该分享画布的图片节点中选择封面")
	}
	pin, err := repo.CanvasShareSnapshotResource(snapshotID, cover.ID)
	if err != nil || pin.Provider != cover.Provider || pin.Endpoint != cover.Endpoint || pin.Bucket != cover.Bucket || pin.ObjectKey != cover.ObjectKey {
		return "", BadAuthRequest("该封面图片不在所选分享版本中，请重新选择")
	}
	return source, nil
}

// resolveFeaturedUploadedCover is the restrictive variant used when the source
// share is gone: only a cover owned by the acting administrator may be chosen.
func (s *Service) resolveFeaturedUploadedCover(repo *repository.Repository, actorID, resourceID, requestedSource string) (string, error) {
	resourceID, requestedSource = strings.TrimSpace(resourceID), strings.TrimSpace(requestedSource)
	if resourceID == "" {
		return "", nil
	}
	if len(resourceID) > 36 || assets.ValidID(resourceID) != resourceID {
		return "", BadAuthRequest("封面素材标识无效")
	}
	if requestedSource == featuredVideoSourceShare {
		return "", BadAuthRequest("来源分享不可用时不能选择分享内的封面，请上传新封面")
	}
	cover, err := repo.ResourceForUser(actorID, resourceID)
	if err != nil || cover.Kind != "image" || cover.Status != model.ResourceStatusReady {
		return "", BadAuthRequest("请选择已上传完成的封面图片")
	}
	return featuredVideoSourceUpload, nil
}

func (s *Service) saveFeaturedCopyMetadata(actor *model.User, existing *model.FeaturedCanvas, req FeaturedCanvasRequest) (*FeaturedCanvasView, error) {
	if req.VideoResourceID != existing.VideoResourceID {
		return nil, BadAuthRequest("来源分享不可用，不能更换展示视频；请让作者重新分享后再编辑")
	}
	if req.PersistCopy && !req.ConsentConfirmed && existing.ConsentAt == nil {
		return nil, BadAuthRequest("请确认已获得作者同意将作品用于精选并保存副本")
	}
	video, err := s.repo.Resource(existing.VideoResourceID)
	if err != nil || video.Kind != "video" || video.Status != model.ResourceStatusReady {
		return nil, BadAuthRequest("精选视频不可用，请让作者重新分享或重新上传")
	}
	var saved model.FeaturedCanvas
	err = s.repo.WithResourceObjectLock(video, func(repo *repository.Repository) error {
		current, err := repo.FeaturedCanvas(existing.ID)
		if err != nil {
			return err
		}
		if !current.PersistCopy || current.CopyStatus != "ready" || current.CopySnapshotID == "" {
			return BadAuthRequest("持久副本已不可用，请让作者重新分享后再编辑")
		}
		saved = *current
		saved.Title, saved.Description, saved.Category, saved.Enabled = req.Title, req.Description, req.Category, req.Enabled
		if req.CoverResourceID != current.CoverResourceID {
			coverSource, err := s.resolveFeaturedUploadedCover(repo, actor.ID, req.CoverResourceID, req.CoverSource)
			if err != nil {
				return err
			}
			saved.CoverResourceID, saved.CoverSource = strings.TrimSpace(req.CoverResourceID), coverSource
		}
		if !req.PersistCopy {
			saved.PersistCopy, saved.CopyStatus = false, "none"
			saved.CopySnapshotID, saved.CopySavedAt, saved.CopyError = "", nil, ""
			saved.CopyTokenHash, saved.CopyTokenCipher = "", ""
		}
		return repo.Save(&saved)
	})
	if err != nil {
		return nil, err
	}
	if !saved.PersistCopy {
		s.cleanupFeaturedSnapshot(existing.CopySnapshotID)
	}
	view, status, message := s.featuredCanvasView(saved, true)
	view.Status, view.StatusMessage = status, message
	return &view, nil
}
func (s *Service) SaveFeaturedCanvas(actor *model.User, id string, req FeaturedCanvasRequest) (*FeaturedCanvasView, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	req.Title, req.Description, req.Category, req.ShareToken = strings.TrimSpace(req.Title), strings.TrimSpace(req.Description), strings.TrimSpace(req.Category), strings.TrimSpace(req.ShareToken)
	req.VideoSource = strings.TrimSpace(req.VideoSource)
	req.CoverResourceID, req.CoverSource = strings.TrimSpace(req.CoverResourceID), strings.TrimSpace(req.CoverSource)
	if req.VideoSource != "" && req.VideoSource != featuredVideoSourceUpload && req.VideoSource != featuredVideoSourceShare {
		return nil, BadAuthRequest("请选择有效的视频来源")
	}
	if req.CoverSource != "" && req.CoverSource != featuredVideoSourceUpload && req.CoverSource != featuredVideoSourceShare {
		return nil, BadAuthRequest("请选择有效的封面来源")
	}
	if req.Title == "" || utf8.RuneCountInString(req.Title) > 160 || utf8.RuneCountInString(req.Description) > 500 || utf8.RuneCountInString(req.Category) > 80 || assets.ValidID(req.VideoResourceID) != req.VideoResourceID || req.VideoResourceID == "" {
		return nil, BadAuthRequest("请填写有效标题和视频")
	}
	var existing *model.FeaturedCanvas
	if id != "" {
		if len(id) > 36 || assets.ValidID(id) != id {
			return nil, BadAuthRequest("精选条目标识无效")
		}
		var err error
		existing, err = s.repo.FeaturedCanvas(id)
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, NotFound("精选内容不存在")
		}
		if err != nil {
			return nil, err
		}
	} else {
		id = newID()
	}
	if existing != nil && existing.PersistCopy && existing.CopyStatus == "ready" && req.ShareToken == "" {
		// 来源分享已删除或撤回时，管理员仍可修改标题、分类、简介和展示开关；
		// 视频与版本必须保持不变，避免在无授权分享的情况下替换内容。
		return s.saveFeaturedCopyMetadata(actor, existing, req)
	}
	if len(req.ShareToken) < 32 || len(req.ShareToken) > 128 {
		return nil, BadAuthRequest("请填写允许复制的画布分享链接")
	}
	hash := sha256.Sum256([]byte(req.ShareToken))
	shareHash := hex.EncodeToString(hash[:])
	reuseVideo := existing != nil && existing.VideoResourceID == req.VideoResourceID
	videoSource := req.VideoSource
	if reuseVideo && videoSource == "" {
		// 旧记录没有来源字段：保留原视频时沿用原来源语义，不要求重新选择。
		videoSource = strings.TrimSpace(existing.VideoSource)
	}
	if !reuseVideo && videoSource == "" {
		// 兼容未声明来源的旧客户端：仅当视频属于当前管理员时按上传处理。
		if resource, lookupErr := s.repo.ResourceForUser(actor.ID, req.VideoResourceID); lookupErr == nil && resource.Kind == "video" && resource.Status == model.ResourceStatusReady {
			videoSource = featuredVideoSourceUpload
		} else {
			return nil, BadAuthRequest("请选择视频来源")
		}
	}
	var video *model.Resource
	var err error
	switch {
	case reuseVideo:
		video, err = s.repo.Resource(req.VideoResourceID)
	case videoSource == featuredVideoSourceUpload:
		video, err = s.repo.ResourceForUser(actor.ID, req.VideoResourceID)
	default:
		video, err = s.repo.Resource(req.VideoResourceID)
	}
	if err != nil || video.Kind != "video" || video.Status != model.ResourceStatusReady {
		return nil, BadAuthRequest("请选择已上传完成的视频")
	}
	var saved model.FeaturedCanvas
	oldCopySnapshotID := ""
	if existing != nil {
		oldCopySnapshotID = existing.CopySnapshotID
	}
	err = s.repo.WithResourceObjectLock(video, func(repo *repository.Repository) error {
		current, err := repo.Resource(video.ID)
		if err != nil || current.Kind != "video" || current.Status != model.ResourceStatusReady || current.Provider != video.Provider || current.Endpoint != video.Endpoint || current.Bucket != video.Bucket || current.ObjectKey != video.ObjectKey {
			return BadAuthRequest("视频状态已变化，请重试")
		}
		share, err := repo.CanvasShareByTokenHash(shareHash)
		if err != nil || !share.Enabled || !share.AllowCopy || (share.ExpiresAt != nil && !share.ExpiresAt.After(time.Now())) {
			return BadAuthRequest("分享链接已失效或未允许保存副本，请联系分享者处理")
		}
		snapshotID := strings.TrimSpace(req.SnapshotID)
		if snapshotID == "" && existing != nil {
			// 编辑已有精选时默认保持原快照；只有分享者更新或管理员明确选择时才换版本。
			if previous, err := repo.FeaturedSnapshot(existing.SnapshotID); err == nil && previous.ShareID == share.ID {
				snapshotID = existing.SnapshotID
			}
		}
		if snapshotID == "" {
			if share.CurrentSnapshotID == nil {
				return BadAuthRequest("分享链接还没有可用的画布版本")
			}
			snapshotID = *share.CurrentSnapshotID
		}
		if assets.ValidID(snapshotID) != snapshotID || len(snapshotID) > 36 {
			return BadAuthRequest("画布分享版本无效")
		}
		snapshot, err := repo.CanvasShareSnapshot(share, snapshotID)
		if err != nil {
			return BadAuthRequest("画布分享快照不可用，请更新分享内容")
		}
		effectiveSource := videoSource
		if effectiveSource == "" && existing != nil {
			effectiveSource = strings.TrimSpace(existing.VideoSource)
		}
		if effectiveSource == featuredVideoSourceUpload {
			if !reuseVideo && current.UserID != actor.ID {
				return BadAuthRequest("上传视频必须属于当前管理员账号")
			}
		}
		if effectiveSource == featuredVideoSourceShare {
			if current.UserID != share.UserID {
				return BadAuthRequest("只能从该分享画布的视频节点中选择展示视频")
			}
			pin, err := repo.CanvasShareSnapshotResource(snapshot.ID, current.ID)
			if err != nil || pin.Provider != current.Provider || pin.Endpoint != current.Endpoint || pin.Bucket != current.Bucket || pin.ObjectKey != current.ObjectKey {
				return BadAuthRequest("该视频不在所选分享版本中，请重新选择")
			}
		}
		if existing == nil {
			order, err := repo.FeaturedNextSortOrder()
			if err != nil {
				return err
			}
			saved = model.FeaturedCanvas{ID: id, CreatedBy: actor.ID, SortOrder: order}
		} else {
			saved = *existing
		}
		saved.Title, saved.Description, saved.Category = req.Title, req.Description, req.Category
		saved.VideoResourceID, saved.VideoSource, saved.SnapshotID, saved.Enabled = current.ID, effectiveSource, snapshotID, req.Enabled
		switch {
		case req.CoverResourceID == "":
			saved.CoverResourceID, saved.CoverSource = "", ""
		case existing != nil && existing.CoverResourceID == req.CoverResourceID && existing.CoverSource == featuredVideoSourceUpload:
			// 管理员上传的封面不依赖分享版本；其他管理员编辑时保持原封面。
			saved.CoverResourceID, saved.CoverSource = req.CoverResourceID, existing.CoverSource
		default:
			coverSource, coverErr := s.resolveFeaturedCover(repo, actor.ID, share.UserID, snapshotID, req.CoverResourceID, req.CoverSource)
			if coverErr != nil {
				return coverErr
			}
			saved.CoverResourceID, saved.CoverSource = req.CoverResourceID, coverSource
		}
		if req.PersistCopy {
			if !req.ConsentConfirmed && (existing == nil || existing.ConsentAt == nil) {
				return BadAuthRequest("请确认已获得作者同意将作品用于精选并保存副本")
			}
			if err := s.prepareFeaturedCopy(repo, &saved, snapshot, req.ShareToken); err != nil {
				return err
			}
		} else {
			saved.PersistCopy, saved.CopyStatus = false, "none"
			saved.CopySnapshotID, saved.CopySavedAt, saved.CopyError = "", nil, ""
			saved.CopyTokenHash, saved.CopyTokenCipher = "", ""
		}
		return repo.Save(&saved)
	})
	if err != nil {
		return nil, err
	}
	if oldCopySnapshotID != "" && (!saved.PersistCopy || saved.CopySnapshotID != oldCopySnapshotID) {
		s.cleanupFeaturedSnapshot(oldCopySnapshotID)
	}
	view, status, message := s.featuredCanvasView(saved, true)
	view.Status, view.StatusMessage = status, message
	return &view, nil
}

func (s *Service) DeleteFeaturedCanvas(actor *model.User, id string) error {
	if err := s.RequireAdmin(actor); err != nil {
		return err
	}
	entry, err := s.repo.FeaturedCanvas(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return NotFound("精选内容不存在")
	}
	if err != nil {
		return err
	}
	if err := s.repo.DeleteFeaturedCanvas(id); err != nil {
		return err
	}
	if entry.CopySnapshotID != "" {
		s.cleanupFeaturedSnapshot(entry.CopySnapshotID)
	}
	if entry.SnapshotID != "" && entry.SnapshotID != entry.CopySnapshotID {
		s.cleanupFeaturedSnapshot(entry.SnapshotID)
	}
	return nil
}

func (s *Service) SetFeaturedCanvasEnabled(actor *model.User, id string, enabled bool) (*FeaturedCanvasView, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	entry, err := s.repo.SetFeaturedCanvasEnabled(id, enabled)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, NotFound("精选内容不存在")
	}
	if err != nil {
		return nil, err
	}
	view, status, message := s.featuredCanvasView(*entry, true)
	view.Status, view.StatusMessage = status, message
	return &view, nil
}

func (s *Service) ReorderFeaturedCanvases(actor *model.User, items []FeaturedReorderItem) ([]FeaturedCanvasView, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	if len(items) == 0 || len(items) > 200 {
		return nil, BadAuthRequest("精选排序列表无效")
	}
	ids := make([]string, 0, len(items))
	expected := make(map[string]time.Time, len(items))
	for _, item := range items {
		if assets.ValidID(item.ID) != item.ID || item.ID == "" {
			return nil, BadAuthRequest("精选条目标识无效")
		}
		parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(item.UpdatedAt))
		if err != nil {
			return nil, BadAuthRequest("精选更新时间无效，请刷新后重试")
		}
		ids = append(ids, item.ID)
		expected[item.ID] = parsed
	}
	if err := s.repo.ReorderFeaturedCanvases(ids, expected); err != nil {
		if errors.Is(err, repository.ErrFeaturedOrderChanged) {
			return nil, NewAppError(http.StatusConflict, "精选列表已被其他管理员修改，请刷新后重新排序")
		}
		return nil, err
	}
	return s.FeaturedCanvases(actor, true)
}

// FeaturedSharePreview resolves a share token and a specific snapshot so the
// administrator can preview the canvas and pick one of its video nodes before
// publishing. It never fetches arbitrary URLs: only pinned snapshot resources
// are returned, and only after owner and storage identity checks.
func (s *Service) FeaturedSharePreview(actor *model.User, req FeaturedSharePreviewRequest) (*FeaturedSharePreview, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	req.ShareToken, req.SnapshotID = strings.TrimSpace(req.ShareToken), strings.TrimSpace(req.SnapshotID)
	result := &FeaturedSharePreview{Status: "invalid", Message: "分享链接无效或已被删除", Videos: []FeaturedShareVideo{}, Images: []FeaturedShareImage{}}
	if len(req.ShareToken) < 32 || len(req.ShareToken) > 128 {
		return result, nil
	}
	hash := sha256.Sum256([]byte(req.ShareToken))
	share, err := s.repo.CanvasShareByTokenHash(hex.EncodeToString(hash[:]))
	if err != nil {
		return result, nil
	}
	result.ShareID = share.ID
	result.AllowCopy, result.ExpiresAt = share.AllowCopy, share.ExpiresAt
	if !share.Enabled {
		result.Status, result.Message = "share_disabled", "分享已被分享者关闭"
		return result, nil
	}
	if share.ExpiresAt != nil && !share.ExpiresAt.After(time.Now()) {
		result.Status, result.Message = "expired", "分享链接已过期"
		return result, nil
	}
	if share.CurrentSnapshotID != nil {
		if snapshot, err := s.repo.CanvasShareSnapshot(share, *share.CurrentSnapshotID); err == nil {
			result.CurrentSnapshotID = snapshot.ID
			at := snapshot.CreatedAt
			result.CurrentSnapshotAt = &at
		}
	}
	snapshotID := req.SnapshotID
	if snapshotID == "" {
		if share.CurrentSnapshotID == nil {
			result.Status, result.Message = "snapshot_missing", "该分享还没有可用的画布版本"
			return result, nil
		}
		snapshotID = *share.CurrentSnapshotID
	}
	if assets.ValidID(snapshotID) != snapshotID {
		result.Status, result.Message = "snapshot_missing", "画布分享版本无效"
		return result, nil
	}
	snapshot, err := s.repo.CanvasShareSnapshot(share, snapshotID)
	if err != nil {
		result.Status, result.Message = "snapshot_missing", "所选分享版本已不可用，请刷新分享链接"
		return result, nil
	}
	result.SnapshotID, result.Title = snapshot.ID, snapshot.Title
	at := snapshot.CreatedAt
	result.SnapshotAt = &at
	result.Videos = s.featuredShareAssets(share, snapshot, req.ShareToken, "video", "分享视频", "视频")
	result.Images = s.featuredShareAssets(share, snapshot, req.ShareToken, "image", "分享图片", "图片")
	if !share.AllowCopy {
		result.Status, result.Message = "copy_forbidden", "分享者已禁止保存副本，不能添加为首页精选"
		return result, nil
	}
	result.Status = "ok"
	if len(result.Videos) == 0 {
		result.Message = "该分享画布没有可用的视频节点，可以改为手动上传视频"
	}
	return result, nil
}

// featuredShareAssets returns only resources pinned in the selected snapshot
// whose owner and storage identity still match. Both the showcase video and
// the optional cover are chosen through this allow-list; arbitrary URLs never
// enter the result.
func (s *Service) featuredShareAssets(share *model.CanvasShare, snapshot *model.CanvasShareSnapshot, token, nodeType, defaultTitle, mediaLabel string) []FeaturedShareAsset {
	var payload struct {
		Nodes []struct {
			Type     string         `json:"type"`
			Title    string         `json:"title"`
			Metadata map[string]any `json:"metadata"`
		} `json:"nodes"`
	}
	assetsFound := make([]FeaturedShareAsset, 0)
	if json.Unmarshal([]byte(snapshot.PayloadJSON), &payload) != nil {
		return assetsFound
	}
	seen := make(map[string]bool, len(payload.Nodes))
	for index, node := range payload.Nodes {
		if node.Type != nodeType {
			continue
		}
		title := strings.TrimSpace(node.Title)
		if title == "" {
			title = defaultTitle + " " + itoa(index+1)
		}
		resourceID := assets.ResourceID(stringValue(node.Metadata["storageKey"]))
		if resourceID == "" {
			resourceID = assets.ResourceID(stringValue(node.Metadata["content"]))
		}
		if resourceID == "" {
			assetsFound = append(assetsFound, FeaturedShareAsset{Title: title, Reason: mediaLabel + "素材尚未保存到云端，无法使用"})
			continue
		}
		if seen[resourceID] {
			continue
		}
		seen[resourceID] = true
		asset := FeaturedShareAsset{ResourceID: resourceID, Title: title}
		pin, pinErr := s.repo.CanvasShareSnapshotResource(snapshot.ID, resourceID)
		resource, resourceErr := s.repo.Resource(resourceID)
		switch {
		case pinErr != nil:
			asset.Reason = "该" + mediaLabel + "不在所选分享版本中"
		case resourceErr != nil || resource.Kind != nodeType || resource.Status != model.ResourceStatusReady:
			asset.Reason = mediaLabel + "资源已不可用"
		case resource.UserID != share.UserID:
			asset.Reason = mediaLabel + "资源不属于分享者"
		case pin.Provider != resource.Provider || pin.Endpoint != resource.Endpoint || pin.Bucket != resource.Bucket || pin.ObjectKey != resource.ObjectKey:
			asset.Reason = mediaLabel + "素材存储位置已变化"
		default:
			asset.Available = true
			asset.URL = "/api/public/canvas-shares/" + url.PathEscape(token) + "/snapshots/" + url.PathEscape(snapshot.ID) + "/resources/" + url.PathEscape(resourceID) + "/file"
		}
		assetsFound = append(assetsFound, asset)
	}
	return assetsFound
}

func (s *Service) PrepareFeaturedVideo(id string, requireEnabled bool, options ResourceAccessOptions, rangeHeader string) (*ResourceDelivery, error) {
	entry, err := s.repo.FeaturedCanvas(id)
	if err != nil {
		return nil, NotFound("精选视频不存在")
	}
	if requireEnabled {
		if _, status, _ := s.featuredCanvasView(*entry, false); status != featuredStatusOK {
			return nil, NotFound("精选视频不存在")
		}
	}
	resource, err := s.repo.Resource(entry.VideoResourceID)
	if err != nil || resource.Kind != "video" || resource.Status != model.ResourceStatusReady {
		return nil, NotFound("精选视频不存在")
	}
	return s.prepareResourceDelivery(resource.UserID, resource, options, rangeHeader)
}

func (s *Service) PrepareAdminFeaturedVideo(actor *model.User, id string, options ResourceAccessOptions, rangeHeader string) (*ResourceDelivery, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	return s.PrepareFeaturedVideo(id, false, options, rangeHeader)
}

func (s *Service) PrepareFeaturedCover(id string, requireEnabled bool, options ResourceAccessOptions, rangeHeader string) (*ResourceDelivery, error) {
	entry, err := s.repo.FeaturedCanvas(id)
	if err != nil {
		return nil, NotFound("精选封面不存在")
	}
	if requireEnabled {
		if _, status, _ := s.featuredCanvasView(*entry, false); status != featuredStatusOK {
			return nil, NotFound("精选封面不存在")
		}
	}
	if entry.CoverResourceID == "" {
		return nil, NotFound("精选封面不存在")
	}
	resource, err := s.repo.Resource(entry.CoverResourceID)
	if err != nil || resource.Kind != "image" || resource.Status != model.ResourceStatusReady {
		return nil, NotFound("精选封面不存在")
	}
	return s.prepareResourceDelivery(resource.UserID, resource, options, rangeHeader)
}

func (s *Service) PrepareAdminFeaturedCover(actor *model.User, id string, options ResourceAccessOptions, rangeHeader string) (*ResourceDelivery, error) {
	if err := s.RequireAdmin(actor); err != nil {
		return nil, err
	}
	return s.PrepareFeaturedCover(id, false, options, rangeHeader)
}
