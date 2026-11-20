package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"
	"yingce/backend/internal/model"
)

// CreateCanvasConcatTask uses the saved canvas as the source of truth. The client
// supplies only the ordered node IDs, never resource paths or render arguments.
func (s *Service) CreateCanvasConcatTask(userID, canvasID, submissionID string, nodeIDs []string) (*model.Task, error) {
	if len(nodeIDs) < 2 || len(nodeIDs) > 40 {
		return nil, BadAuthRequest("请选择 2 至 40 个视频片段进行拼接")
	}
	if len(submissionID) < 8 || len(submissionID) > 36 {
		return nil, BadAuthRequest("拼接任务标识无效")
	}
	if existing, err := s.repo.TaskForSubmission(userID, submissionID); err == nil {
		if existing.Type != model.TaskTypeTimelineRender || existing.ProjectID != canvasID {
			return nil, BadAuthRequest("拼接任务标识已被使用")
		}
		return taskForOutput(*existing), nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	canvas, err := s.repo.CanvasProjectForUser(userID, canvasID)
	if err != nil {
		return nil, BadAuthRequest("画布不存在或无权访问")
	}
	var document struct {
		Nodes []struct {
			ID       string `json:"id"`
			Type     string `json:"type"`
			Metadata struct {
				StorageKey string `json:"storageKey"`
				DurationMs int64  `json:"durationMs"`
				Seconds    string `json:"seconds"`
			} `json:"metadata"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal([]byte(canvas.PayloadJSON), &document); err != nil {
		return nil, BadAuthRequest("画布内容无法读取")
	}
	byID := make(map[string]int, len(document.Nodes))
	for index, node := range document.Nodes {
		byID[node.ID] = index
	}
	project := renderProject{Version: 2, Tracks: []renderTrack{{ID: "canvas-video", Kind: "video"}}}
	seen := make(map[string]bool, len(nodeIDs))
	for index, nodeID := range nodeIDs {
		if seen[nodeID] {
			return nil, BadAuthRequest("拼接顺序中有重复镜头")
		}
		seen[nodeID] = true
		position, ok := byID[nodeID]
		if !ok || document.Nodes[position].Type != "video" {
			return nil, BadAuthRequest(fmt.Sprintf("第 %d 个镜头不是画布中的视频节点", index+1))
		}
		node := document.Nodes[position]
		resourceID := strings.TrimPrefix(node.Metadata.StorageKey, "resource:")
		if resourceID == node.Metadata.StorageKey || resourceID == "" {
			return nil, BadAuthRequest(fmt.Sprintf("第 %d 个镜头还未生成完成，请稍后重试", index+1))
		}
		resource, err := s.repo.ResourceForUser(userID, resourceID)
		if err != nil || resource.Status != model.ResourceStatusReady || !strings.HasPrefix(resource.MimeType, "video/") {
			return nil, BadAuthRequest(fmt.Sprintf("第 %d 个镜头的视频资源不可用", index+1))
		}
		duration := resource.DurationMs
		if duration <= 0 {
			duration = node.Metadata.DurationMs
		}
		if duration <= 0 {
			if seconds, parseErr := strconv.ParseFloat(node.Metadata.Seconds, 64); parseErr == nil {
				duration = int64(seconds * 1000)
			}
		}
		if duration < 500 || duration > 600000 {
			return nil, BadAuthRequest(fmt.Sprintf("第 %d 个镜头缺少有效时长", index+1))
		}
		clip := renderClip{ID: fmt.Sprintf("shot-%d", index+1), Kind: "video", TrackID: "canvas-video", StartMs: project.DurationMs, DurationMs: duration}
		clip.DirectMedia = &struct {
			ID         string `json:"id"`
			Kind       string `json:"kind"`
			StorageKey string `json:"storageKey"`
		}{ID: nodeID, Kind: "video", StorageKey: "resource:" + resourceID}
		project.Clips = append(project.Clips, clip)
		project.DurationMs += duration
		if index == 0 && resource.Height > resource.Width {
			project.Width, project.Height = 1080, 1920
		}
	}
	if project.DurationMs > 20*60*1000 {
		return nil, BadAuthRequest("拼接成片最多 20 分钟")
	}
	return s.CreateTimelineRenderTask(userID, TimelineRenderCreateRequest{ProjectID: canvasID, Timeline: project, SubmissionID: submissionID})
}
