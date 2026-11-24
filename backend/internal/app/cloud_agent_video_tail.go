package app

import (
	"strings"

	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

type cloudAgentVideoTailArgs struct {
	VideoNodeID string `json:"videoNodeId"`
	ImageNodeID string `json:"imageNodeId"`
}

type cloudAgentVideoTailPrepared struct {
	SourceID string
	ImageID  string
}

func cloudAgentVideoTailSource(repo *repository.Repository, userID, canvasID string, call cloudAgentCall) (*model.CanvasProject, map[string]any, map[string]any, cloudAgentVideoTailArgs, string, error) {
	var args cloudAgentVideoTailArgs
	if err := decodeCloudAgentJSONObject(call.Function.Arguments, &args); err != nil {
		return nil, nil, nil, args, "", cloudAgentJSONArgumentError(err)
	}
	for _, id := range []string{args.VideoNodeID, args.ImageNodeID} {
		if err := validateCloudAgentID(id, "节点ID", 80); err != nil {
			return nil, nil, nil, args, "", err
		}
	}
	canvas, err := repo.CanvasProjectForUser(userID, canvasID)
	if err != nil {
		return nil, nil, nil, args, "", err
	}
	doc, err := creationDocument(canvas.PayloadJSON)
	if err != nil {
		return nil, nil, nil, args, "", err
	}
	nodes, err := creationObjects(doc["nodes"])
	if err != nil {
		return nil, nil, nil, args, "", err
	}
	if nodes[args.ImageNodeID] != nil {
		return nil, nil, nil, args, "", BadAuthRequest("图片节点ID已存在，请使用新ID")
	}
	node := nodes[args.VideoNodeID]
	if node == nil || stringValue(node["type"]) != "video" {
		return nil, nil, nil, args, "", BadAuthRequest("视频节点不存在或不是视频")
	}
	meta, _ := node["metadata"].(map[string]any)
	key := stringValue(meta["storageKey"])
	if !strings.HasPrefix(key, "resource:") {
		return nil, nil, nil, args, "", BadAuthRequest("视频尚未保存到资源库，请等待视频生成完成")
	}
	id := strings.TrimPrefix(key, "resource:")
	resource, err := repo.ResourceForUser(userID, id)
	if err != nil || resource.Status != "ready" || !strings.HasPrefix(strings.ToLower(resource.MimeType), "video/") {
		return nil, nil, nil, args, "", BadAuthRequest("视频资源不存在、尚未就绪或无权访问")
	}
	return canvas, doc, node, args, id, nil
}

func cloudAgentPreviewVideoTail(repo *repository.Repository, userID, canvasID string, call cloudAgentCall) (cloudAgentApprovalPreview, error) {
	_, _, node, args, _, err := cloudAgentVideoTailSource(repo, userID, canvasID, call)
	if err != nil {
		return cloudAgentApprovalPreview{}, err
	}
	return cloudAgentApprovalPreview{Kind: "canvas", Title: "提取视频尾帧", Description: "从视频最后一帧创建图片节点，不会提交生成任务。", Items: []cloudAgentApprovalPreviewItem{{Operation: "add_node", NodeID: args.ImageNodeID, NodeType: "image", NodeTitle: stringValue(node["title"]) + " · 尾帧", Summary: "提取视频尾帧并创建图片节点"}}}, nil
}

func cloudAgentPrepareVideoTail(repo *repository.Repository, userID string, state *cloudAgentRuntime, call cloudAgentCall, services ...*Service) (any, error) {
	service := cloudAgentToolService(services)
	if service == nil {
		return nil, BadAuthRequest("视频处理服务不可用")
	}
	_, _, _, _, sourceID, err := cloudAgentVideoTailSource(repo, userID, state.Request.CanvasID, call)
	if err != nil {
		return nil, err
	}
	imageID, err := service.extractDramaTailFrame(userID, sourceID)
	if err != nil {
		return nil, err
	}
	return cloudAgentVideoTailPrepared{SourceID: sourceID, ImageID: imageID}, nil
}

// Execution is dispatched by the runtime so file processing stays outside its database transaction.
func applyCloudAgentVideoTailPlaceholder(*repository.Repository, string, string, cloudAgentCall, RuntimePolicySetting, ...cloudAgentMutationRecorder) (any, error) {
	return nil, BadAuthRequest("视频尾帧处理未准备")
}

func applyCloudAgentVideoTail(repo *repository.Repository, userID, canvasID string, call cloudAgentCall, policy RuntimePolicySetting, value any, recorder cloudAgentMutationRecorder) (any, error) {
	prepared, ok := value.(cloudAgentVideoTailPrepared)
	if !ok {
		return nil, BadAuthRequest("视频尾帧处理未准备")
	}
	canvas, doc, source, args, sourceID, err := cloudAgentVideoTailSource(repo, userID, canvasID, call)
	if err != nil {
		return nil, err
	}
	if sourceID != prepared.SourceID {
		return nil, BadAuthRequest("视频内容已变化，请重新提取尾帧")
	}
	image, err := repo.ResourceForUser(userID, prepared.ImageID)
	if err != nil || image.Status != "ready" || !strings.HasPrefix(image.MimeType, "image/") {
		return nil, BadAuthRequest("提取的图片资源不可用，请重试")
	}
	before, beforeJSON := cloudAgentCanvasHash(doc), canvas.PayloadJSON
	position, _ := source["position"].(map[string]any)
	x, _ := position["x"].(float64)
	y, _ := position["y"].(float64)
	sourceWidth, _ := source["width"].(float64)
	x += sourceWidth + 96
	width, height := 340.0, 240.0
	if image.Width > 0 && image.Height > 0 {
		height = width * float64(image.Height) / float64(image.Width)
		if height > 480 {
			width *= 480 / height
			height = 480
		}
	}
	title := strings.TrimSpace(stringValue(source["title"]))
	if title == "" {
		title = "视频"
	}
	child := creationAddedNode(CreationCanvasOp{ID: args.ImageNodeID, NodeType: "image", Title: title + " · 尾帧", X: &x, Y: &y, Width: &width, Height: &height, Metadata: map[string]any{"content": resourceFileURL(image.ID), "storageKey": "resource:" + image.ID, "status": "success", "naturalWidth": image.Width, "naturalHeight": image.Height}})
	doc["nodes"] = append(creationMaps(doc["nodes"]), child)
	if err := saveCloudAgentDocument(repo, canvas, doc, policy); err != nil {
		return nil, err
	}
	preview := cloudAgentPreviewVideoTailFromNode(args, title)
	if recorder != nil {
		if err := recorder(repo, cloudAgentMutationInput{UserID: userID, CanvasID: canvasID, StepID: call.ID, Operation: "canvas_extract_video_tail", BeforeSnapshotHash: before, AfterSnapshotHash: cloudAgentCanvasHash(doc), BeforeJSON: beforeJSON, Preview: &preview}); err != nil {
			return nil, err
		}
	}
	return map[string]any{"videoNodeId": args.VideoNodeID, "imageNodeId": args.ImageNodeID, "snapshotHash": cloudAgentCanvasHash(doc), "committed": true, "summary": "已提取视频尾帧并创建图片节点，可用作后续参考图"}, nil
}

func cloudAgentPreviewVideoTailFromNode(args cloudAgentVideoTailArgs, title string) cloudAgentApprovalPreview {
	return cloudAgentApprovalPreview{Kind: "canvas", Status: "applied", Title: "提取视频尾帧", Description: "已创建尾帧图片节点", Items: []cloudAgentApprovalPreviewItem{{Operation: "add_node", NodeID: args.ImageNodeID, NodeType: "image", NodeTitle: title + " · 尾帧", Summary: "提取视频尾帧并创建图片节点"}}}
}
