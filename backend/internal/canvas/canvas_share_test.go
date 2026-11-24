package canvas

import (
	"encoding/json"
	"strings"
	"testing"
	"yingce/backend/internal/assets"

	"yingce/backend/internal/model"
)

func TestCanvasShareTokenHashDoesNotExposeToken(t *testing.T) {
	token := "example-share-token"
	first := canvasShareTokenHash(token)
	if first == token || first != canvasShareTokenHash(token) || len(first) != 64 {
		t.Fatalf("unexpected token hash: %q", first)
	}
}

func TestPublicCanvasShareReportsCopyPermissionAndUnavailableMedia(t *testing.T) {
	svc := newCanvasOperationTestService(t)
	token := strings.Repeat("a", 43)
	project := &model.CanvasProject{ID: "p", UserID: "owner", PayloadJSON: `{"id":"p","nodes":[{"id":"a","type":"image","title":"本地图片","metadata":{"content":"blob:local"}},{"id":"b","type":"video","title":"视频","metadata":{"storageKey":"resource:video_1"}}]}`}
	share := &model.CanvasShare{ID: "s", UserID: "owner", ProjectID: "p", TokenHash: canvasShareTokenHash(token), Enabled: true, AllowCopy: true}
	if err := svc.repo.Create(project); err != nil { t.Fatal(err) }
	if err := svc.repo.Create(share); err != nil { t.Fatal(err) }
	result, err := svc.PublicCanvasShare(token)
	if err != nil { t.Fatal(err) }
	if !result.AllowCopy || len(result.CopyIssues) != 1 || !strings.Contains(result.CopyIssues[0], "本地图片") { t.Fatalf("unexpected copy preflight: %#v", result) }
	if _, err := svc.SetCanvasShareCopyPermission("another-user", "p", false); err == nil { t.Fatal("another user changed copy permission") }
}

func TestPublicCanvasProjectScrubsSecretsAndRewritesResources(t *testing.T) {
	payload := map[string]any{
		"id": "project-1", "title": "公开画布", "chatSessions": []any{map[string]any{"secret": "chat"}},
		"nodes": []any{map[string]any{
			"id": "node-1", "type": "image", "title": "镜头", "position": map[string]any{"x": 1, "y": 2}, "width": 320, "height": 240,
			"metadata": map[string]any{
				"content": "data:image/png;base64,secret", "storageKey": "resource:resource_123", "prompt": "保留提示词",
				"apiKey": "secret", "taskId": "task-1", "skillSnapshot": map[string]any{"apiKey": "nested-secret", "name": "技能"},
			},
		}},
		"connections": []any{},
	}
	raw, _ := json.Marshal(payload)
	project := &model.CanvasProject{ID: "project-1", Title: "公开画布", PayloadJSON: string(raw)}
	public, resources, err := publicCanvasProject(project, "share-token")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(public)
	text := string(encoded)
	for _, forbidden := range []string{"data:image", "secret", "task-1", `"secret":"chat"`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("public payload leaked %q: %s", forbidden, text)
		}
	}
	if !strings.Contains(text, "保留提示词") || !strings.Contains(text, "/api/public/canvas-shares/share-token/resources/resource_123/file") {
		t.Fatalf("public payload lost expected data: %s", text)
	}
	if !resources["resource_123"] {
		t.Fatal("referenced resource was not authorized")
	}
}

func TestCanvasResourceIDRejectsInvalidValues(t *testing.T) {
	for _, value := range []string{"resource:", "resource:../secret", "/api/resources/a%2Fb/file", "resource:" + strings.Repeat("a", 81)} {
		if got := assets.ResourceID(value); got != "" {
			t.Fatalf("expected invalid resource id for %q, got %q", value, got)
		}
	}
	if got := assets.ResourceID("/api/resources/abc_123/file"); got != "abc_123" {
		t.Fatalf("unexpected resource id: %q", got)
	}
}

func TestPublicCanvasProjectIncludesVersionAndReferenceResources(t *testing.T) {
	payload := `{"id":"p","nodes":[{"id":"n","type":"video","metadata":{"storageKey":"resource:current","references":["resource:ref"],"videoPreview":{"storageKey":"resource:preview"},"currentMediaVersionId":"v1","mediaVersions":[{"id":"v1","media":{"storageKey":"resource:old"},"generation":{"references":["resource:ref"],"taskId":"private"}}]}}]}`
	public, resources, err := publicCanvasProject(&model.CanvasProject{ID:"p", PayloadJSON:payload}, "token")
	if err != nil { t.Fatal(err) }
	encoded, _ := json.Marshal(public)
	for _, id := range []string{"current", "ref", "preview", "old"} {
		if !resources[id] || !strings.Contains(string(encoded), "/resources/"+id+"/file") { t.Fatalf("missing %s: %s", id, encoded) }
	}
	if strings.Contains(string(encoded), "resource:") || strings.Contains(string(encoded), "private") { t.Fatalf("private data leaked: %s", encoded) }
}

func TestShareCopyPreflightAllowsCloudVersionsAndRejectsLocalVersion(t *testing.T) {
	svc := newCanvasOperationTestService(t)
	token := strings.Repeat("b", 43)
	project := &model.CanvasProject{ID:"p", UserID:"owner", PayloadJSON:`{"id":"p","nodes":[{"id":"a","type":"image","metadata":{"storageKey":"resource:current","references":["resource:ref"],"mediaVersions":[{"media":{"storageKey":"resource:old"}}]}},{"id":"b","type":"image","title":"local","metadata":{"storageKey":"resource:current","mediaVersions":[{"media":{"content":"blob:local"}}]}}]}`}
	share := &model.CanvasShare{ID:"s", UserID:"owner", ProjectID:"p", TokenHash:canvasShareTokenHash(token), Enabled:true, AllowCopy:true}
	if err := svc.repo.Create(project); err != nil { t.Fatal(err) }
	if err := svc.repo.Create(share); err != nil { t.Fatal(err) }
	result, err := svc.PublicCanvasShare(token)
	if err != nil { t.Fatal(err) }
	if len(result.CopyIssues) != 1 || !strings.Contains(result.CopyIssues[0], "local") { t.Fatalf("unexpected copy issues: %#v", result.CopyIssues) }
}

func TestPublicCanvasProjectDropsUnmanagedMediaURL(t *testing.T) {
	payload := `{"id":"p","nodes":[{"id":"n","type":"image","title":"image","position":{"x":0,"y":0},"width":10,"height":10,"metadata":{"content":"https://tracker.example/image.png","prompt":"public prompt"}}],"connections":[]}`
	public, _, err := publicCanvasProject(&model.CanvasProject{ID: "p", PayloadJSON: payload}, "share-token")
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(public)
	if strings.Contains(string(encoded), "tracker.example") || !strings.Contains(string(encoded), "public prompt") {
		t.Fatalf("unexpected public payload: %s", encoded)
	}
}
