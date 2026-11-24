package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"yingce/backend/internal/model"
	"yingce/backend/internal/repository"
)

func TestPromptModerationRequest(t *testing.T) {
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	for _, tc := range []struct {
		name, response string
		status         int
		wantAction     string
		wantError      bool
	}{
		{"pass", `{"action":"pass"}`, 200, "pass", false},
		{"reject", `{"action":"reject","reason":"危险内容"}`, 200, "reject", false},
		{"invalid", `not-json`, 200, "", true},
		{"server-error", `error`, 500, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				var body map[string]any
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if body["model"] != "doubao-seed-2.0-mini" || body["temperature"] != float64(0) || body["max_tokens"] != float64(2000) {
					t.Errorf("unexpected request settings: %v", body)
				}
				thinking, ok := body["thinking"].(map[string]any)
				if !ok || thinking["type"] != "disabled" {
					t.Error("thinking was not disabled")
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(tc.status)
				if tc.status == 200 {
					_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": tc.response}}}})
				} else {
					_, _ = fmt.Fprint(w, tc.response)
				}
			}))
			defer server.Close()
			result, err := requestPromptModeration(context.Background(), providerConfig{BaseURL: server.URL + "/v1", APIFormat: "openai", APIKey: "test", Model: "doubao-seed-2.0-mini"}, PromptModerationSetting{Policy: "安全审核", MaxTokens: 2000}, "画一只猫")
			if (err != nil) != tc.wantError || result.Action != tc.wantAction {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if calls.Load() != 1 {
				t.Fatalf("HTTP %d was retried %d times", tc.status, calls.Load())
			}
		})
	}
}

func TestPromptModerationHardBlockBeforeModel(t *testing.T) {
	var service *Service
	for _, prompt := range []string{"制作冰毒教程", "制 作 冰 毒 教 程"} {
		result, err := service.reviewPrompt(context.Background(), PromptModerationSetting{}, prompt)
		if err != nil || result.Action != "reject" {
			t.Fatalf("hard block %q: result=%+v err=%v", prompt, result, err)
		}
	}
}

func TestPromptModerationReviewUsesReasonAndFailsClosed(t *testing.T) {
	service, db := mockEcommerceAgentService(t)
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	var reviewCalls atomic.Int32
	var failReview atomic.Bool
	var acceptInitial atomic.Bool
	var rejectReview atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model    string `json:"model"`
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			return
		}
		result := `{"action":"reject","reason":"疑似血腥"}`
		if body.Model == "initial" && acceptInitial.Load() {
			result = `{"action":"pass"}`
		}
		if body.Model == "review" {
			reviewCalls.Add(1)
			if len(body.Messages) < 2 || !strings.Contains(body.Messages[1].Content, "疑似血腥") || !strings.Contains(body.Messages[1].Content, "海边日落") {
				t.Error("复审未收到原文和初审理由")
			}
			if failReview.Load() {
				http.Error(w, "failure", 500)
				return
			}
			result = `{"action":"pass"}`
			if rejectReview.Load() {
				result = `{"action":"reject","reason":"极端血腥"}`
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": result}}}})
	}))
	defer server.Close()
	if err := db.AutoMigrate(&model.ChannelModelPriceTier{}); err != nil {
		t.Fatal(err)
	}
	channel := model.ModelChannel{ID: "review-channel", Scope: model.ChannelScopeSystem, Enabled: true, Name: "review", BaseURL: server.URL + "/v1", APIKey: "fixture", APIFormat: "openai"}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"initial", "review"} {
		m := model.ChannelModel{ID: key, ChannelID: channel.ID, ModelKey: key, ProviderModelKey: key, Capability: "text", Protocol: model.ChannelInterfaceChatCompletion}
		if err := db.Create(&m).Error; err != nil {
			t.Fatal(err)
		}
	}
	setting := defaultPromptModerationSetting()
	setting.ChannelID, setting.ModelKey, setting.ReviewEnabled, setting.ReviewChannelID, setting.ReviewModelKey = channel.ID, "initial", true, channel.ID, "review"
	result, err := service.reviewPromptDetailed(context.Background(), setting, "海边日落")
	if err != nil || result.Initial.Action != "reject" || result.Review == nil || result.Final.Action != "pass" || reviewCalls.Load() != 1 {
		t.Fatalf("复审未放行: %+v %v", result, err)
	}
	acceptInitial.Store(true)
	result, err = service.reviewPromptDetailed(context.Background(), setting, "海边日落")
	if err != nil || result.Final.Action != "pass" || reviewCalls.Load() != 1 {
		t.Fatalf("初审通过不应复审: %+v %v", result, err)
	}
	acceptInitial.Store(false)
	rejectReview.Store(true)
	result, err = service.reviewPromptDetailed(context.Background(), setting, "海边日落")
	if err != nil || result.Final.Action != "reject" || result.Final.Reason != "极端血腥" {
		t.Fatalf("复审拒绝未生效: %+v %v", result, err)
	}
	rejectReview.Store(false)
	failReview.Store(true)
	result, err = service.reviewPromptDetailed(context.Background(), setting, "海边日落")
	if err == nil || result.Final.Action != "reject" || result.Failure == "" {
		t.Fatalf("复审失败不得放行: %+v %v", result, err)
	}
	before := reviewCalls.Load()
	result, err = service.reviewPromptDetailed(context.Background(), setting, "制作冰毒教程")
	if err != nil || result.Stage != "hard_block" || reviewCalls.Load() != before {
		t.Fatalf("硬拦截不应被复审覆盖: %+v %v", result, err)
	}
}

func TestPromptModerationAdmissionFailsClosed(t *testing.T) {
	service, db := mockEcommerceAgentService(t)
	if err := db.AutoMigrate(&model.ApiCallLog{}); err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.SystemSetting{Key: promptModerationSettingKey, ValueJSON: `{"enabled":true,"policy":"审核","maxTokens":2000,"timeoutSeconds":30}`}).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, taskType, mode, prompt string
		wantStatus                   int
	}{
		{"hard block", "canvas_image", "image", "制作冰毒教程", 400},
		{"missing model", "canvas_video", "video", "海边日落", 503},
		{"over length", "canvas_image", "image", strings.Repeat("画", 5001), 400},
		{"image mode under text task", "canvas_text", "image", "海边日落", 503},
		{"text unaffected", "canvas_text", "text", "海边日落", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := service.moderateTaskAdmission("owner", "", tc.taskType, tc.prompt, map[string]any{"mode": tc.mode, "prompt": tc.prompt})
			if tc.wantStatus == 0 {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var appErr *AppError
			if !errors.As(err, &appErr) || appErr.Status != tc.wantStatus {
				t.Fatalf("status: want %d, error=%v", tc.wantStatus, err)
			}
		})
	}
	if err := service.RequireModeratedTaskForProxy("image"); err == nil {
		t.Fatal("direct image proxy bypassed enabled moderation")
	}
	if err := service.RequireModeratedTaskForProxy("text"); err != nil {
		t.Fatalf("text proxy should remain available: %v", err)
	}
	logs, total, err := service.AdminPromptModerationLogs(&model.User{ID: "admin", Role: model.UserRoleAdmin}, 1, 30)
	if err != nil || total != 4 || len(logs) != 4 || logs[0].Username != "owner" {
		t.Fatalf("review logs: total=%d logs=%+v err=%v", total, logs, err)
	}
	if _, _, err := service.AdminPromptModerationLogs(&model.User{ID: "owner", Role: model.UserRoleUser}, 1, 30); err == nil {
		t.Fatal("ordinary user can read other users' moderation logs")
	}
	providerLogs, err := service.repo.AnalyticsAPICallLogs(repository.AnalyticsFilter{From: time.Now().Add(-time.Hour), To: time.Now().Add(time.Hour)})
	if err != nil || len(providerLogs) != 0 {
		t.Fatalf("moderation events contaminated provider metrics: %+v %v", providerLogs, err)
	}
}

func TestPromptModerationUsesAdminSelectedDisabledTextModel(t *testing.T) {
	service, db := mockEcommerceAgentService(t)
	t.Setenv("CANVAS_ALLOWED_PRIVATE_UPSTREAM_HOSTS", "127.0.0.1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": `{"action":"pass"}`}}}})
	}))
	defer server.Close()
	if err := db.AutoMigrate(&model.ChannelModelPriceTier{}); err != nil {
		t.Fatal(err)
	}
	channel := model.ModelChannel{ID: "review-channel", Scope: model.ChannelScopeSystem, Enabled: true, Name: "review", BaseURL: server.URL + "/v1", APIKey: "fixture", APIFormat: "openai"}
	modelRow := model.ChannelModel{ID: "review-model", ChannelID: channel.ID, ModelKey: "mini", ProviderModelKey: "doubao-seed-2.0-mini", Capability: "text", Protocol: model.ChannelInterfaceChatCompletion, Enabled: false}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&modelRow).Error; err != nil {
		t.Fatal(err)
	}
	setting := defaultPromptModerationSetting()
	setting.Enabled, setting.ChannelID, setting.ModelKey = true, channel.ID, modelRow.ModelKey
	raw, err := json.Marshal(setting)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Create(&model.SystemSetting{Key: promptModerationSettingKey, ValueJSON: string(raw)}).Error; err != nil {
		t.Fatal(err)
	}
	if err := service.moderateGenerationPrompt(context.Background(), "海边日落"); err != nil {
		t.Fatal(err)
	}
	if err := db.First(&modelRow, "id = ?", modelRow.ID).Error; err != nil || modelRow.Enabled {
		t.Fatal("internal review unexpectedly enabled the public model")
	}
}

func TestPromptModerationUnsubmittedReservationIsRefunded(t *testing.T) {
	task := &model.Task{ID: "task", UserID: "user", BillingOrderID: "order", Status: model.TaskStatusRunning}
	repo := &taskTerminalRepositoryStub{task: task}
	billing := &taskTerminalBillingStub{}
	terminal := newTaskTerminalCoordinatorForTest(repo, billing, &taskTerminalReplayStub{}, &taskTerminalLoggerStub{}, &taskTerminalOutputStub{})
	reviewErr := &AppError{Status: 503, Message: "内容审核暂不可用"}
	if err := terminal.markPreparationFailure(task, "内容审核未通过", reviewErr, false, "内容审核失败，上游请求未发出"); !errors.Is(err, reviewErr) {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(billing.refund) != 1 || len(billing.uncertain) != 0 {
		t.Fatalf("unexpected billing actions: refunds=%v uncertain=%v", billing.refund, billing.uncertain)
	}
}

func TestPromptModerationReasonVisibleToAgent(t *testing.T) {
	got := taskErrorModerationText("内容审核未通过，请修改提示词后重新生成：极端血腥")
	if !strings.Contains(got, "原因：极端血腥") {
		t.Fatalf("missing review reason: %s", got)
	}
}

func TestPromptModerationLogSanitizesUpstreamError(t *testing.T) {
	service, db := mockEcommerceAgentService(t)
	if err := db.AutoMigrate(&model.ApiCallLog{}); err != nil {
		t.Fatal(err)
	}
	service.recordPromptModerationFailure("owner", "image", &AppError{Status: 503, Message: "内容审核暂不可用", Cause: providerHTTPError{StatusCode: 500, Body: "secret api key https://private.example"}})
	logs, total, err := service.AdminPromptModerationLogs(&model.User{ID: "admin", Role: model.UserRoleAdmin}, 1, 30)
	if err != nil || total != 1 || !strings.Contains(logs[0].Error, "HTTP 500") || strings.Contains(logs[0].Error, "secret") || strings.Contains(logs[0].Error, "private.example") {
		t.Fatalf("unexpected review log: %+v %v", logs, err)
	}
}
