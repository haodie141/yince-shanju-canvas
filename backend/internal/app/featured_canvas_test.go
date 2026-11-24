package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"
	"yingce/backend/internal/model"
)

func TestFeaturedCanvasAdminAndPinnedVersion(t *testing.T) {
	svc, db, _ := newResourceDeletionTestService(t)
	admin := &model.User{ID: "admin", Role: model.UserRoleAdmin}
	otherAdmin := &model.User{ID: "other-admin", Role: model.UserRoleAdmin}
	normal := &model.User{ID: "visitor", Role: model.UserRoleUser}
	token := strings.Repeat("a", 43)
	hash := sha256.Sum256([]byte(token))
	snapshotID := "snapshot"
	payload := `{"nodes":[{"id":"video-node","type":"video","title":"成片","metadata":{"storageKey":"resource:share-video"}}],"connections":[]}`
	share := model.CanvasShare{ID: "share", UserID: normal.ID, ProjectID: "canvas", TokenHash: hex.EncodeToString(hash[:]), TokenCipher: token, Enabled: true, AllowCopy: true, CurrentSnapshotID: &snapshotID}
	snapshot := model.CanvasShareSnapshot{ID: snapshotID, ShareID: share.ID, UserID: normal.ID, CanvasID: share.ProjectID, Title: "分享作品", PayloadJSON: payload}
	adminVideo := model.Resource{ID: "admin-video", UserID: admin.ID, Kind: "video", Provider: "local", ObjectKey: "admin.mp4", Status: model.ResourceStatusReady}
	shareVideo := model.Resource{ID: "share-video", UserID: normal.ID, Kind: "video", Provider: "local", ObjectKey: "share.mp4", Status: model.ResourceStatusReady}
	unpinnedVideo := model.Resource{ID: "unpinned-video", UserID: normal.ID, Kind: "video", Provider: "local", ObjectKey: "unpinned.mp4", Status: model.ResourceStatusReady}
	pinJSON, err := json.Marshal(shareVideo)
	if err != nil {
		t.Fatal(err)
	}
	pin := model.CanvasShareSnapshotResource{SnapshotID: snapshot.ID, ResourceID: shareVideo.ID, ResourceJSON: string(pinJSON), Provider: "local", ObjectKey: shareVideo.ObjectKey}
	for _, item := range []any{&share, &snapshot, &adminVideo, &shareVideo, &unpinnedVideo, &pin} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	preview, err := svc.FeaturedSharePreview(admin, FeaturedSharePreviewRequest{ShareToken: token})
	if err != nil || preview.Status != "ok" || preview.Title != "分享作品" || len(preview.Videos) != 1 || !preview.Videos[0].Available || preview.Videos[0].ResourceID != shareVideo.ID {
		t.Fatalf("unexpected share preview: %#v %v", preview, err)
	}
	req := FeaturedCanvasRequest{Title: "作品", VideoResourceID: adminVideo.ID, VideoSource: "upload", ShareToken: token, Enabled: true}
	if _, err := svc.SaveFeaturedCanvas(normal, "", req); err == nil {
		t.Fatal("ordinary user published featured item")
	}
	item, err := svc.SaveFeaturedCanvas(admin, "", req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(item.CanvasURL, "snapshot="+snapshotID) {
		t.Fatalf("missing pinned version: %s", item.CanvasURL)
	}
	if _, err := svc.SaveFeaturedCanvas(otherAdmin, item.ID, req); err != nil {
		t.Fatalf("another administrator cannot edit the existing item while keeping its video: %v", err)
	}
	item, err = svc.SaveFeaturedCanvas(admin, item.ID, req)
	if err != nil {
		t.Fatalf("another administrator cannot edit the existing item: %v", err)
	}
	if _, err := svc.SaveFeaturedCanvas(otherAdmin, "", req); err == nil {
		t.Fatal("accepted another user's video for a new item")
	}
	// 编辑标题时即使分享者已发布新版本，也必须保持原绑定版本。
	newSnapshot := model.CanvasShareSnapshot{ID: "new-version", ShareID: share.ID, UserID: share.UserID, CanvasID: share.ProjectID, Title: "新版本", PayloadJSON: payload}
	if err := db.Create(&newSnapshot).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CanvasShare{}).Where("id = ?", share.ID).Update("current_snapshot_id", newSnapshot.ID).Error; err != nil {
		t.Fatal(err)
	}
	req.SnapshotID = ""
	updated, err := svc.SaveFeaturedCanvas(admin, item.ID, req)
	if err != nil || !strings.Contains(updated.CanvasURL, "snapshot="+snapshotID) {
		t.Fatalf("editing silently changed pinned version: %#v %v", updated, err)
	}
	req.SnapshotID = newSnapshot.ID
	updated, err = svc.SaveFeaturedCanvas(admin, item.ID, req)
	if err != nil || !strings.Contains(updated.CanvasURL, "snapshot="+newSnapshot.ID) {
		t.Fatalf("failed to bind the explicitly selected version: %#v %v", updated, err)
	}
	req.SnapshotID = "unrelated"
	if _, err := svc.SaveFeaturedCanvas(admin, item.ID, req); err == nil {
		t.Fatal("accepted a version outside the share")
	}
	// 分享快照内的视频必须带 pin 才能作为精选视频。
	req = FeaturedCanvasRequest{Title: "分享视频作品", VideoResourceID: shareVideo.ID, VideoSource: "share", ShareToken: token, SnapshotID: snapshotID, Enabled: true}
	item, err = svc.SaveFeaturedCanvas(admin, "", req)
	if err != nil {
		t.Fatalf("failed to select a pinned share video: %v", err)
	}
	req.VideoResourceID = unpinnedVideo.ID
	if _, err := svc.SaveFeaturedCanvas(admin, "", req); err == nil {
		t.Fatal("accepted a share video outside the pinned snapshot")
	}
	req.VideoResourceID = shareVideo.ID
	req.VideoSource = "upload"
	if _, err := svc.SaveFeaturedCanvas(admin, "", req); err == nil {
		t.Fatal("accepted another user's resource as an administrator upload")
	}
	visible, err := svc.FeaturedCanvases(nil, false)
	if err != nil || len(visible) != 2 {
		t.Fatalf("missing public entries: %#v %v", visible, err)
	}
	req.Enabled = false
	req.SnapshotID = snapshotID
	req.VideoSource = "share"
	if _, err := svc.SaveFeaturedCanvas(admin, item.ID, req); err != nil {
		t.Fatal(err)
	}
	visible, err = svc.FeaturedCanvases(nil, false)
	if err != nil || len(visible) != 1 {
		t.Fatalf("disabled item remains public: %#v %v", visible, err)
	}
	if err := db.Model(&share).Update("allow_copy", false).Error; err != nil {
		t.Fatal(err)
	}
	if preview, err := svc.FeaturedSharePreview(admin, FeaturedSharePreviewRequest{ShareToken: token}); err != nil || preview.Status != "copy_forbidden" {
		t.Fatalf("copy permission not reported: %#v %v", preview, err)
	}
	visible, err = svc.FeaturedCanvases(nil, false)
	if err != nil || len(visible) != 0 {
		t.Fatalf("invalid share remains public: %#v %v", visible, err)
	}
	editable, err := svc.FeaturedCanvases(admin, true)
	if err != nil || len(editable) != 2 {
		t.Fatalf("admin cannot repair entries: %#v %v", editable, err)
	}
}

func TestFeaturedPersistentCopySurvivesCanvasDeletion(t *testing.T) {
	svc, db, _ := newResourceDeletionTestService(t)
	admin := &model.User{ID: "admin", Role: model.UserRoleAdmin}
	owner := &model.User{ID: "owner", Role: model.UserRoleUser}
	token := strings.Repeat("b", 43)
	hash := sha256.Sum256([]byte(token))
	snapshotID := "copy-snapshot"
	payload := `{"nodes":[{"id":"video-node","type":"video","title":"成片","metadata":{"storageKey":"resource:copy-video"}}],"connections":[]}`
	project := model.CanvasProject{ID: "copy-canvas", UserID: owner.ID, Title: "持久作品", PayloadJSON: payload, Revision: 1}
	share := model.CanvasShare{ID: "copy-share", UserID: owner.ID, ProjectID: project.ID, TokenHash: hex.EncodeToString(hash[:]), TokenCipher: token, Enabled: true, AllowCopy: true, CurrentSnapshotID: &snapshotID}
	snapshot := model.CanvasShareSnapshot{ID: snapshotID, ShareID: share.ID, UserID: owner.ID, CanvasID: project.ID, Title: "持久作品", PayloadJSON: payload, SourceRevision: 1}
	video := model.Resource{ID: "copy-video", UserID: owner.ID, Kind: "video", Provider: "local", ObjectKey: "copy.mp4", Status: model.ResourceStatusReady}
	pinJSON, err := json.Marshal(video)
	if err != nil {
		t.Fatal(err)
	}
	pin := model.CanvasShareSnapshotResource{SnapshotID: snapshot.ID, ResourceID: video.ID, ResourceJSON: string(pinJSON), Provider: "local", ObjectKey: video.ObjectKey}
	for _, item := range []any{&project, &share, &snapshot, &video, &pin} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	item, err := svc.SaveFeaturedCanvas(admin, "", FeaturedCanvasRequest{
		Title: "持久作品", VideoResourceID: video.ID, VideoSource: "share", ShareToken: token,
		SnapshotID: snapshotID, Enabled: true, PersistCopy: true, ConsentConfirmed: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !item.PersistCopy || item.CopyStatus != "ready" || !strings.Contains(item.CanvasURL, "/share/canvas/") || !strings.Contains(item.CanvasURL, "snapshot="+snapshotID) {
		t.Fatalf("persistent copy not ready: %#v", item)
	}
	// 管理员编辑仍需要源分享链接；副本链接只用于首页展示与克隆。
	if want := "/share/canvas/" + token + "?snapshot=" + snapshotID; item.SourceCanvasURL != want {
		t.Fatalf("admin source link = %q, want %q", item.SourceCanvasURL, want)
	}
	entry, err := svc.repo.FeaturedCanvas(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	copyToken, err := svc.decryptSettingSecret(entry.CopyTokenCipher)
	if err != nil || copyToken == "" {
		t.Fatalf("missing copy token: %v", err)
	}
	visible, err := svc.FeaturedCanvases(nil, false)
	if err != nil || len(visible) != 1 || visible[0].SourceShareStatus != "ok" {
		t.Fatalf("copy not visible before deletion: %#v %v", visible, err)
	}
	// 删除原画布：分享行变成墓碑，快照和素材 pin 保留。
	if err := svc.canvasDomain().DeleteUserCanvasProject(owner.ID, project.ID); err != nil {
		t.Fatal(err)
	}
	visible, err = svc.FeaturedCanvases(nil, false)
	if err != nil || len(visible) != 1 || visible[0].SourceShareStatus != "deleted" || !strings.Contains(visible[0].CanvasURL, "/share/canvas/") {
		t.Fatalf("persistent copy disappeared after canvas deletion: %#v %v", visible, err)
	}
	public, err := svc.canvasDomain().PublicCanvasShareVersion(copyToken, snapshotID)
	if err != nil || public.SnapshotID != snapshotID {
		t.Fatalf("copy share page unavailable after canvas deletion: %#v %v", public, err)
	}
	// 作者主动撤回后，即使原画布随后被删除，持久副本也必须继续下架。
	now := time.Now()
	if err := db.Model(&model.CanvasShare{}).Where("id = ?", share.ID).Update("revoked_at", now).Error; err != nil {
		t.Fatal(err)
	}
	visible, err = svc.FeaturedCanvases(nil, false)
	if err != nil || len(visible) != 0 {
		t.Fatalf("revoked copy remains public: %#v %v", visible, err)
	}
	if _, err := svc.canvasDomain().PublicCanvasShareVersion(copyToken, snapshotID); err == nil {
		t.Fatal("revoked copy token still readable")
	}
	if err := svc.DeleteFeaturedCanvas(admin, item.ID); err != nil {
		t.Fatal(err)
	}
	var snapshotCount, pinCount int64
	if err := db.Model(&model.CanvasShareSnapshot{}).Where("id = ?", snapshotID).Count(&snapshotCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.CanvasShareSnapshotResource{}).Where("snapshot_id = ?", snapshotID).Count(&pinCount).Error; err != nil {
		t.Fatal(err)
	}
	if snapshotCount != 0 || pinCount != 0 {
		t.Fatalf("copy snapshot not released: snapshots=%d pins=%d", snapshotCount, pinCount)
	}
}

func TestFeaturedPersistentCopyRequiresConsent(t *testing.T) {
	svc, db, _ := newResourceDeletionTestService(t)
	admin := &model.User{ID: "admin", Role: model.UserRoleAdmin}
	owner := &model.User{ID: "owner", Role: model.UserRoleUser}
	token := strings.Repeat("c", 43)
	hash := sha256.Sum256([]byte(token))
	snapshotID := "consent-snapshot"
	share := model.CanvasShare{ID: "consent-share", UserID: owner.ID, ProjectID: "consent-canvas", TokenHash: hex.EncodeToString(hash[:]), TokenCipher: token, Enabled: true, AllowCopy: true, CurrentSnapshotID: &snapshotID}
	snapshot := model.CanvasShareSnapshot{ID: snapshotID, ShareID: share.ID, UserID: owner.ID, CanvasID: share.ProjectID, Title: "同意", PayloadJSON: `{}`, SourceRevision: 1}
	video := model.Resource{ID: "consent-video", UserID: admin.ID, Kind: "video", Provider: "local", ObjectKey: "consent.mp4", Status: model.ResourceStatusReady}
	for _, item := range []any{&share, &snapshot, &video} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	_, err := svc.SaveFeaturedCanvas(admin, "", FeaturedCanvasRequest{
		Title: "同意", VideoResourceID: video.ID, VideoSource: "upload", ShareToken: token,
		SnapshotID: snapshotID, Enabled: true, PersistCopy: true,
	})
	if err == nil {
		t.Fatal("persistent copy saved without author consent")
	}
	item, err := svc.SaveFeaturedCanvas(admin, "", FeaturedCanvasRequest{
		Title: "同意", VideoResourceID: video.ID, VideoSource: "upload", ShareToken: token,
		SnapshotID: snapshotID, Enabled: true, PersistCopy: true, ConsentConfirmed: true,
	})
	if err != nil || item.CopyStatus != "ready" {
		t.Fatalf("consented persistent copy failed: %#v %v", item, err)
	}
}

func TestFeaturedCanvasCoverValidation(t *testing.T) {
	svc, db, _ := newResourceDeletionTestService(t)
	admin := &model.User{ID: "admin", Role: model.UserRoleAdmin}
	otherAdmin := &model.User{ID: "other-admin", Role: model.UserRoleAdmin}
	owner := &model.User{ID: "owner", Role: model.UserRoleUser}
	token := strings.Repeat("d", 43)
	hash := sha256.Sum256([]byte(token))
	snapshotID := "cover-snapshot"
	payload := `{"nodes":[{"id":"video-node","type":"video","title":"成片","metadata":{"storageKey":"resource:cover-video"}},{"id":"image-node","type":"image","title":"封面图","metadata":{"storageKey":"resource:share-cover"}}],"connections":[]}`
	share := model.CanvasShare{ID: "cover-share", UserID: owner.ID, ProjectID: "cover-canvas", TokenHash: hex.EncodeToString(hash[:]), TokenCipher: token, Enabled: true, AllowCopy: true, CurrentSnapshotID: &snapshotID}
	snapshot := model.CanvasShareSnapshot{ID: snapshotID, ShareID: share.ID, UserID: owner.ID, CanvasID: share.ProjectID, Title: "封面作品", PayloadJSON: payload}
	shareVideo := model.Resource{ID: "cover-video", UserID: owner.ID, Kind: "video", Provider: "local", ObjectKey: "cover.mp4", Status: model.ResourceStatusReady}
	shareCover := model.Resource{ID: "share-cover", UserID: owner.ID, Kind: "image", Provider: "local", ObjectKey: "cover.png", Status: model.ResourceStatusReady}
	unpinnedCover := model.Resource{ID: "unpinned-cover", UserID: owner.ID, Kind: "image", Provider: "local", ObjectKey: "unpinned.png", Status: model.ResourceStatusReady}
	adminCover := model.Resource{ID: "admin-cover", UserID: admin.ID, Kind: "image", Provider: "local", ObjectKey: "admin-cover.png", Status: model.ResourceStatusReady}
	otherCover := model.Resource{ID: "other-cover", UserID: otherAdmin.ID, Kind: "image", Provider: "local", ObjectKey: "other-cover.png", Status: model.ResourceStatusReady}
	videoPinJSON, err := json.Marshal(shareVideo)
	if err != nil {
		t.Fatal(err)
	}
	coverPinJSON, err := json.Marshal(shareCover)
	if err != nil {
		t.Fatal(err)
	}
	videoPin := model.CanvasShareSnapshotResource{SnapshotID: snapshot.ID, ResourceID: shareVideo.ID, ResourceJSON: string(videoPinJSON), Provider: "local", ObjectKey: shareVideo.ObjectKey}
	coverPin := model.CanvasShareSnapshotResource{SnapshotID: snapshot.ID, ResourceID: shareCover.ID, ResourceJSON: string(coverPinJSON), Provider: "local", ObjectKey: shareCover.ObjectKey}
	for _, item := range []any{&share, &snapshot, &shareVideo, &shareCover, &unpinnedCover, &adminCover, &otherCover, &videoPin, &coverPin} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	preview, err := svc.FeaturedSharePreview(admin, FeaturedSharePreviewRequest{ShareToken: token})
	if err != nil || preview.Status != "ok" || len(preview.Images) != 1 || !preview.Images[0].Available {
		t.Fatalf("share cover not listed: %#v %v", preview, err)
	}
	req := FeaturedCanvasRequest{
		Title: "封面作品", VideoResourceID: shareVideo.ID, VideoSource: "share", ShareToken: token, SnapshotID: snapshotID,
		Enabled: true, CoverResourceID: shareCover.ID, CoverSource: "share",
	}
	item, err := svc.SaveFeaturedCanvas(admin, "", req)
	if err != nil {
		t.Fatalf("failed to select a pinned share cover: %v", err)
	}
	if !item.CoverAvailable || !strings.Contains(item.CoverURL, "/admin/featured/") || item.CoverSource != "share" || item.CoverResourceID != shareCover.ID {
		t.Fatalf("share cover not exposed to admin: %#v", item)
	}
	public, err := svc.FeaturedCanvases(nil, false)
	if err != nil || len(public) != 1 || !public[0].CoverAvailable || !strings.Contains(public[0].CoverURL, "/public/featured/") || public[0].CoverResourceID != "" {
		t.Fatalf("share cover not exposed to public: %#v %v", public, err)
	}
	req.CoverResourceID = unpinnedCover.ID
	req.CoverSource = "share"
	if _, err := svc.SaveFeaturedCanvas(admin, item.ID, req); err == nil {
		t.Fatal("accepted a share cover outside the pinned snapshot")
	}
	req.CoverResourceID = shareCover.ID
	req.CoverSource = "upload"
	if _, err := svc.SaveFeaturedCanvas(admin, item.ID, req); err == nil {
		t.Fatal("accepted another user's image as an uploaded cover")
	}
	req.CoverResourceID = adminCover.ID
	req.CoverSource = "upload"
	item, err = svc.SaveFeaturedCanvas(admin, item.ID, req)
	if err != nil || item.CoverSource != "upload" || item.CoverResourceID != adminCover.ID {
		t.Fatalf("failed to select an uploaded cover: %#v %v", item, err)
	}
	req.CoverResourceID = otherCover.ID
	if _, err := svc.SaveFeaturedCanvas(admin, item.ID, req); err == nil {
		t.Fatal("accepted another administrator's image as an uploaded cover")
	}
	req.CoverResourceID = ""
	req.CoverSource = ""
	item, err = svc.SaveFeaturedCanvas(admin, item.ID, req)
	if err != nil || item.CoverAvailable || item.CoverURL != "" || item.CoverResourceID != "" {
		t.Fatalf("cover not cleared: %#v %v", item, err)
	}
	if _, err := svc.PrepareFeaturedCover(item.ID, true, ResourceAccessOptions{}, ""); err == nil {
		t.Fatal("cleared cover still served")
	}
}

func TestFeaturedCanvasReorderConflict(t *testing.T) {
	svc, db, _ := newResourceDeletionTestService(t)
	admin := &model.User{ID: "admin", Role: model.UserRoleAdmin}
	first := model.FeaturedCanvas{ID: "first", Title: "一", VideoResourceID: "video", SnapshotID: "snapshot", CreatedBy: admin.ID, SortOrder: 0}
	second := model.FeaturedCanvas{ID: "second", Title: "二", VideoResourceID: "video", SnapshotID: "snapshot", CreatedBy: admin.ID, SortOrder: 1}
	if err := db.Create(&first).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&second).Error; err != nil {
		t.Fatal(err)
	}
	items := []FeaturedReorderItem{
		{ID: second.ID, UpdatedAt: second.UpdatedAt.Format(time.RFC3339Nano)},
		{ID: first.ID, UpdatedAt: first.UpdatedAt.Format(time.RFC3339Nano)},
	}
	result, err := svc.ReorderFeaturedCanvases(admin, items)
	if err != nil {
		t.Fatal(err)
	}
	if result[0].ID != second.ID || result[1].ID != first.ID {
		t.Fatalf("unexpected order: %#v", result)
	}
	// 使用旧时间戳再次排序应冲突，而不是覆盖其他管理员的提交。
	if _, err := svc.ReorderFeaturedCanvases(admin, items); err == nil {
		t.Fatal("stale timestamps overwrote a newer order")
	}
}
