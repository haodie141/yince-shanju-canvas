package app

import (
	"testing"

	"yingce/backend/internal/model"
)

func TestCloudAgentVideoTailCreatesReusableImage(t *testing.T) {
	s, db, _, _ := creationTestService(t)
	canvas := &model.CanvasProject{ID: "tail-canvas", UserID: "user", PayloadJSON: `{"nodes":[{"id":"video","type":"video","title":"片段","position":{"x":10,"y":20},"width":320,"height":180,"metadata":{"storageKey":"resource:video-res","status":"success"}}],"connections":[]}`}
	for _, item := range []any{canvas,
		&model.Resource{ID: "video-res", UserID: "user", Kind: "video", Status: model.ResourceStatusReady, MimeType: "video/mp4"},
		&model.Resource{ID: "tail-res", UserID: "user", Kind: "image", Status: model.ResourceStatusReady, MimeType: "image/png", Width: 1920, Height: 1080},
	} {
		if err := db.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}
	s.dramaTailFrameHook = func(userID, resourceID string) (string, error) {
		if userID != "user" || resourceID != "video-res" {
			t.Fatal("unexpected source", userID, resourceID)
		}
		return "tail-res", nil
	}
	call := cloudAgentCall{ID: "tail-call"}
	call.Function.Name = "canvas_extract_video_tail"
	call.Function.Arguments = `{"videoNodeId":"video","imageNodeId":"tail"}`
	state := &cloudAgentRuntime{Request: CloudAgentRequest{CanvasID: canvas.ID}}
	prepared, err := cloudAgentPrepareVideoTail(s.repo, "user", state, call, s)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := s.RuntimePolicy()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = applyCloudAgentVideoTail(s.repo, "user", canvas.ID, call, policy, prepared, nil); err != nil {
		t.Fatal(err)
	}
	updated, err := s.repo.CanvasProjectForUser("user", canvas.ID)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := creationDocument(updated.PayloadJSON)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := creationObjects(doc["nodes"])
	if err != nil {
		t.Fatal(err)
	}
	ref, field, err := cloudAgentReference(s.repo, "user", nodes["tail"])
	if err != nil || field != "referenceImages" || ref["storageKey"] != "resource:tail-res" {
		t.Fatalf("尾帧无法用作参考图: %#v %s %v", ref, field, err)
	}
	if _, err = cloudAgentPrepareVideoTail(s.repo, "user", state, call, s); err == nil {
		t.Fatal("不应覆盖已有图片节点")
	}
	if _, err = cloudAgentPrepareVideoTail(s.repo, "other-user", state, call, s); err == nil {
		t.Fatal("不应访问其他用户的画布")
	}
}
