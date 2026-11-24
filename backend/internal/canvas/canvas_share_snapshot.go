package canvas

import (
	"encoding/json"
	"errors"
	"sort"

	"yingce/backend/internal/model"
)

// shareSnapshot is the immutable saved project revision and the resources
// exposed by its public projection. Persistence and resource pinning must be
// committed together before this snapshot can be published.
type shareSnapshot struct {
	Project     model.CanvasProject
	ResourceIDs []string
}

func prepareShareSnapshot(project *model.CanvasProject) (shareSnapshot, error) {
	var document map[string]any
	if project == nil || json.Unmarshal([]byte(project.PayloadJSON), &document) != nil || document == nil {
		return shareSnapshot{}, errors.New("分享画布数据格式无效")
	}
	_, allowed, err := publicCanvasProject(project, "snapshot")
	if err != nil {
		return shareSnapshot{}, err
	}
	ids := make([]string, 0, len(allowed))
	for id := range allowed {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return shareSnapshot{Project: *project, ResourceIDs: ids}, nil
}
