package app

import (
	"encoding/json"
	"testing"

	"yingce/backend/internal/model"
)

func TestCanvasConcatUsesSavedVideoOrderAndSubmissionIsIdempotent(t *testing.T) {
	svc, db := newTimelineTaskTestService(t)
	for _, resource := range []model.Resource{
		{ID: "clip-one", UserID: "usr-render-test", Kind: "media", Status: model.ResourceStatusReady, MimeType: "video/mp4", DurationMs: 2000, Width: 1920, Height: 1080},
		{ID: "clip-two", UserID: "usr-render-test", Kind: "media", Status: model.ResourceStatusReady, MimeType: "video/mp4", DurationMs: 3000, Width: 1920, Height: 1080},
	} {
		if err := db.Create(&resource).Error; err != nil {
			t.Fatal(err)
		}
	}
	canvas := model.CanvasProject{ID: "canvas-concat", UserID: "usr-render-test", PayloadJSON: `{"nodes":[{"id":"one","type":"video","metadata":{"storageKey":"resource:clip-one"}},{"id":"two","type":"video","metadata":{"storageKey":"resource:clip-two"}}],"connections":[]}`}
	if err := db.Create(&canvas).Error; err != nil {
		t.Fatal(err)
	}
	task, err := svc.CreateCanvasConcatTask("usr-render-test", canvas.ID, "concat-submission-1", []string{"two", "one"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := svc.CreateCanvasConcatTask("usr-render-test", canvas.ID, "concat-submission-1", []string{"two", "one"})
	if err != nil || again.ID != task.ID {
		t.Fatalf("duplicate submission created another task: first=%s second=%v err=%v", task.ID, again, err)
	}
	stored, err := svc.repo.TaskForUser("usr-render-test", task.ID)
	if err != nil {
		t.Fatal(err)
	}
	var input timelineRenderInput
	if err := json.Unmarshal([]byte(stored.InputJSON), &input); err != nil {
		t.Fatal(err)
	}
	if len(input.Timeline.Clips) != 2 || input.Timeline.Clips[0].DirectMedia.StorageKey != "resource:clip-two" || input.Timeline.Clips[1].StartMs != 3000 || input.Timeline.DurationMs != 5000 {
		t.Fatalf("wrong ordered render timeline: %+v", input.Timeline)
	}
	if _, err := svc.CreateCanvasConcatTask("someone-else", canvas.ID, "concat-submission-2", []string{"one", "two"}); err == nil {
		t.Fatal("another user could render this canvas")
	}
}

func TestCloudAgentCanSubmitCanvasConcatAndCreateOutputNode(t *testing.T) {
	svc, db := newTimelineTaskTestService(t)
	for _, id := range []string{"first-video", "second-video"} {
		if err := db.Create(&model.Resource{ID: id, UserID: "usr-render-test", Kind: "media", Status: model.ResourceStatusReady, MimeType: "video/mp4", DurationMs: 1500, Width: 1920, Height: 1080}).Error; err != nil {
			t.Fatal(err)
		}
	}
	canvas := model.CanvasProject{ID: "agent-concat-canvas", UserID: "usr-render-test", PayloadJSON: `{"id":"agent-concat-canvas","title":"Test","nodes":[{"id":"first","type":"video","title":"First","position":{"x":0,"y":0},"width":720,"height":405,"metadata":{"storageKey":"resource:first-video","status":"success"}},{"id":"second","type":"video","title":"Second","position":{"x":760,"y":0},"width":720,"height":405,"metadata":{"storageKey":"resource:second-video","status":"success"}}],"connections":[]}`}
	if err := db.Create(&canvas).Error; err != nil {
		t.Fatal(err)
	}
	state := &cloudAgentRuntime{Request: CloudAgentRequest{CanvasID: canvas.ID}, RuntimeRunID: "run-concat-test"}
	call := cloudAgentCall{ID: "call-concat-test"}
	call.Function.Name = "canvas_concat_videos"
	call.Function.Arguments = `{"nodeIds":["second","first"]}`
	if _, err := cloudAgentPreviewCanvasConcatVideos(svc.repo, "usr-render-test", canvas.ID, call); err != nil {
		t.Fatal(err)
	}
	result, err := cloudAgentHandleCanvasConcatVideos(svc.repo, "usr-render-test", state, call, svc)
	if err != nil {
		t.Fatal(err)
	}
	first := result.(map[string]any)
	if first["taskId"] == "" || first["nodeId"] == "" {
		t.Fatalf("missing task or node: %#v", first)
	}
	again, err := cloudAgentHandleCanvasConcatVideos(svc.repo, "usr-render-test", state, call, svc)
	if err != nil || again.(map[string]any)["taskId"] != first["taskId"] {
		t.Fatalf("agent replay submitted twice: first=%#v again=%#v err=%v", first, again, err)
	}
	stored, err := svc.repo.CanvasProjectForUser("usr-render-test", canvas.ID)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Nodes []struct {
			ID       string `json:"id"`
			Metadata struct {
				ConcatTaskID string `json:"concatTaskId"`
			} `json:"metadata"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(stored.PayloadJSON), &doc); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, node := range doc.Nodes {
		if node.ID == first["nodeId"] {
			count++
			if node.Metadata.ConcatTaskID != first["taskId"] {
				t.Fatalf("output node points to wrong task: %#v", node)
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected one output node, got %d", count)
	}
}
