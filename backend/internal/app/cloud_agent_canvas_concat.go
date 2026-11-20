package app

import (
	"fmt"
	"math"
	"strings"
	"time"

	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

type cloudAgentCanvasConcatArgs struct {
	NodeIDs []string `json:"nodeIds"`
}

func cloudAgentCanvasConcatInput(repo *repository.Repository, userID, canvasID string, call cloudAgentCall) (*model.CanvasProject, map[string]any, cloudAgentCanvasConcatArgs, []string, error) {
	var args cloudAgentCanvasConcatArgs
	if err := decodeCloudAgentJSONObject(call.Function.Arguments, &args); err != nil {
		return nil, nil, args, nil, cloudAgentJSONArgumentError(err)
	}
	if len(args.NodeIDs) < 2 || len(args.NodeIDs) > 40 {
		return nil, nil, args, nil, cloudAgentFieldError("nodeIds", "invalid_length", "请选择 2 至 40 个视频节点")
	}
	canvas, err := repo.CanvasProjectForUser(userID, canvasID)
	if err != nil {
		return nil, nil, args, nil, BadAuthRequest("画布不存在或无权访问")
	}
	doc, err := creationDocument(canvas.PayloadJSON)
	if err != nil {
		return nil, nil, args, nil, err
	}
	byID := cloudAgentLayoutNodesByID(doc)
	titles := make([]string, 0, len(args.NodeIDs))
	seen := map[string]bool{}
	for index, id := range args.NodeIDs {
		if seen[id] {
			return nil, nil, args, nil, cloudAgentFieldError("nodeIds", "duplicate", "拼接顺序中有重复节点")
		}
		seen[id] = true
		node := byID[id]
		if node == nil || stringValue(node["type"]) != "video" {
			return nil, nil, args, nil, cloudAgentFieldError("nodeIds", "invalid_value", fmt.Sprintf("第 %d 个节点不是当前画布的视频", index+1))
		}
		metadata, _ := node["metadata"].(map[string]any)
		if !strings.HasPrefix(stringValue(metadata["storageKey"]), "resource:") {
			return nil, nil, args, nil, cloudAgentFieldError("nodeIds", "not_ready", fmt.Sprintf("第 %d 个视频尚未生成完成", index+1))
		}
		title := stringValue(node["title"])
		if title == "" {
			title = id
		}
		titles = append(titles, title)
	}
	return canvas, doc, args, titles, nil
}

func cloudAgentPreviewCanvasConcatVideos(repo *repository.Repository, userID, canvasID string, call cloudAgentCall) (cloudAgentApprovalPreview, error) {
	_, _, args, titles, err := cloudAgentCanvasConcatInput(repo, userID, canvasID, call)
	if err != nil {
		return cloudAgentApprovalPreview{}, err
	}
	items := make([]cloudAgentApprovalPreviewItem, 0, len(args.NodeIDs))
	for index, id := range args.NodeIDs {
		items = append(items, cloudAgentApprovalPreviewItem{Operation: "concat_video", NodeID: id, NodeTitle: titles[index], NodeType: "video", NodeTypeLabel: "视频", Summary: fmt.Sprintf("%d. %s", index+1, titles[index])})
	}
	return cloudAgentApprovalPreview{Kind: "canvas", Title: "自动拼接视频", Description: fmt.Sprintf("按顺序拼接 %d 个镜头，保留原音；本地渲染不扣模型积分。", len(args.NodeIDs)), Items: items}, nil
}

func cloudAgentHandleCanvasConcatVideos(repo *repository.Repository, userID string, state *cloudAgentRuntime, call cloudAgentCall, services ...*Service) (any, error) {
	if len(services) == 0 || services[0] == nil || state == nil || state.RuntimeRunID == "" || call.ID == "" {
		return nil, BadAuthRequest("Agent 拼接服务不可用")
	}
	svc := services[0]
	canvasID := state.Request.CanvasID
	canvas, doc, args, _, err := cloudAgentCanvasConcatInput(repo, userID, canvasID, call)
	if err != nil {
		return nil, err
	}
	submissionID := cloudAgentID(userID, "canvas-concat:"+state.RuntimeRunID+":"+call.ID)
	task, err := svc.CreateCanvasConcatTask(userID, canvasID, submissionID, args.NodeIDs)
	if err != nil {
		return nil, err
	}
	outputID := submissionID + "-video"
	if cloudAgentLayoutNodesByID(doc)[outputID] == nil {
		left, right, bottom := math.Inf(1), math.Inf(-1), math.Inf(-1)
		byID := cloudAgentLayoutNodesByID(doc)
		for _, id := range args.NodeIDs {
			node := byID[id]
			position, _ := node["position"].(map[string]any)
			x, _ := position["x"].(float64)
			y, _ := position["y"].(float64)
			width, _ := node["width"].(float64)
			height, _ := node["height"].(float64)
			left = math.Min(left, x)
			right = math.Max(right, x+width)
			bottom = math.Max(bottom, y+height)
		}
		x, y, width, height := (left+right-720)/2, bottom+96, 720.0, 405.0
		output := creationAddedNode(CreationCanvasOp{Type: "add_node", ID: outputID, NodeType: "video", Title: fmt.Sprintf("自动成片 · %d 镜头", len(args.NodeIDs)), X: &x, Y: &y, Width: &width, Height: &height, Metadata: map[string]any{
			"status": "loading", "videoEditOperation": "concat", "concatSourceNodeIds": args.NodeIDs,
			"concatSubmissionId": submissionID, "concatTaskId": task.ID,
		}})
		beforeHash := cloudAgentCanvasHash(doc)
		beforeJSON := canvas.PayloadJSON
		output["createdAt"] = time.Now().UTC().Format(time.RFC3339Nano)
		output["updatedAt"] = output["createdAt"]
		doc["nodes"] = append(doc["nodes"].([]any), output)
		policy, err := svc.RuntimePolicy()
		if err != nil {
			return nil, err
		}
		if err := saveCloudAgentDocument(repo, canvas, doc, policy); err != nil {
			return nil, err
		}
		_ = recordCloudAgentCanvasMutation(repo, cloudAgentMutationInput{
			RunID: state.RuntimeRunID, UserID: userID, CanvasID: canvasID, StepID: call.ID,
			Operation: "canvas_concat_videos", BeforeSnapshotHash: beforeHash,
			AfterSnapshotHash: cloudAgentCanvasHash(doc), BeforeJSON: beforeJSON, HasSubmittedTask: true,
		})
	}
	return map[string]any{"taskId": task.ID, "nodeId": outputID, "status": task.Status, "submitted": true, "next": "已提交本地拼接任务；用 task_get 查询进度，成片将回写到画布节点。不要重复提交。"}, nil
}
