package canvas

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"yingce/backend/internal/model"
)

func TestPrepareShareSnapshotPinsPublicResourceSetAndRevision(t *testing.T) {
	project := &model.CanvasProject{ID: "p", Revision: 7, PayloadJSON: `{"id":"p","nodes":[{"id":"n","type":"image","metadata":{"storageKey":"resource:current","references":["resource:reference"],"mediaVersions":[{"media":{"storageKey":"resource:previous"}}],"apiKey":"secret"}}]}`}
	snapshot, err := prepareShareSnapshot(project)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(snapshot.ResourceIDs, []string{"current", "previous", "reference"}) {
		t.Fatalf("unexpected resources: %v", snapshot.ResourceIDs)
	}
	project.Revision = 8
	project.PayloadJSON = `{"id":"p","nodes":[]}`
	public, _, err := publicCanvasProject(&snapshot.Project, "visitor-token")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(public)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Project.Revision != 7 || !strings.Contains(string(encoded), "/visitor-token/resources/current/file") || strings.Contains(string(encoded), "secret") {
		t.Fatalf("snapshot changed or leaked: %s", encoded)
	}
}

func TestPrepareShareSnapshotRejectsInvalidDocument(t *testing.T) {
	for _, project := range []*model.CanvasProject{nil, {PayloadJSON: "{"}, {PayloadJSON: "null"}} {
		if _, err := prepareShareSnapshot(project); err == nil {
			t.Fatalf("accepted invalid project: %#v", project)
		}
	}
}
