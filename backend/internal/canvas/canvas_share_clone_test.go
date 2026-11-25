package canvas

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"yingce/backend/internal/model"
)

func TestPrepareShareCloneRewritesNestedMediaWithoutPrivateData(t *testing.T) {
	project := &model.CanvasProject{ID: "source", PayloadJSON: `{"id":"source","chatSessions":[{"secret":"private chat"}],"nodes":[{"id":"node","type":"video","metadata":{"storageKey":"resource:video","apiKey":"secret","references":["resource:image"],"videoPreview":{"storageKey":"resource:preview"},"mediaVersions":[{"media":{"storageKey":"resource:previous"}}]}}],"connections":[]}`}
	aliases := map[string]string{"video": "new-video", "image": "new-image", "preview": "new-preview", "previous": "new-previous"}
	data, err := prepareShareClone(project, "new-canvas", aliases)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{`"id":"new-canvas"`, `"storageKey":"resource:new-video"`, `/api/resources/new-video/file`, `resource:new-image`, `resource:new-preview`, `resource:new-previous`} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q: %s", want, text)
		}
	}
	for _, forbidden := range []string{"private chat", "secret", "clone-projection", "resource:video", "resource:image"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("unexpected %q: %s", forbidden, text)
		}
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if len(result["chatSessions"].([]any)) != 0 {
		t.Fatal("chat history copied")
	}
}

func TestCloneShareResourceRetainsFileButNotUploadIdentity(t *testing.T) {
	uploadKey := "owner-upload"
	source := model.Resource{ID: "original", UserID: "owner", Kind: "video", Status: model.ResourceStatusReady, Provider: "local", ObjectKey: "video.mp4", PlaybackObjectKey: "playback.mp4", StorageSettingID: "private", PublicURL: "https://example.invalid/signed", UploadKey: &uploadKey}
	encoded, err := json.Marshal(source)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := model.CanvasShareSnapshot{ID: "snapshot", UserID: "owner"}
	pin := model.CanvasShareSnapshotResource{SnapshotID: snapshot.ID, ResourceID: source.ID, ResourceJSON: string(encoded), StorageSettingID: source.StorageSettingID, Provider: source.Provider, ObjectKey: source.ObjectKey}
	now := time.Unix(12345, 0)
	clone, err := cloneShareResource(snapshot, pin, "recipient", "new-resource", now)
	if err != nil {
		t.Fatal(err)
	}
	if clone.ID != "new-resource" || clone.UserID != "recipient" || clone.ObjectKey != "video.mp4" || clone.PlaybackObjectKey != "playback.mp4" || clone.StorageSettingID != "private" || clone.PublicURL != "" || clone.UploadKey != nil || !clone.CreatedAt.Equal(now) {
		t.Fatalf("invalid alias: %#v", clone)
	}
	pin.ObjectKey = "different.mp4"
	if _, err := cloneShareResource(snapshot, pin, "recipient", "new-resource", now); err == nil {
		t.Fatal("accepted conflicting physical identity")
	}
	pin.ObjectKey = source.ObjectKey
	snapshot.UserID = "another-owner"
	if _, err := cloneShareResource(snapshot, pin, "recipient", "new-resource", now); err == nil {
		t.Fatal("accepted a resource belonging to a different owner")
	}
}

func TestPrepareShareCloneRejectsMissingOrDuplicateAliases(t *testing.T) {
	project := &model.CanvasProject{PayloadJSON: `{"nodes":[{"id":"a","type":"image","metadata":{"storageKey":"resource:one","references":["resource:two"]}}]}`}
	for _, aliases := range []map[string]string{{}, {"one": "new-one"}, {"one": "same", "two": "same"}, {"one": "one", "two": "new-two"}} {
		if _, err := prepareShareClone(project, "new-canvas", aliases); err == nil {
			t.Fatalf("accepted aliases %#v", aliases)
		}
	}
	if _, err := prepareShareClone(project, "", map[string]string{"one": "a", "two": "b"}); err == nil {
		t.Fatal("accepted empty canvas ID")
	}
}
