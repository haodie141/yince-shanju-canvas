package repository

import (
	"strings"
	"time"

	"gorm.io/gorm"
	"yingce/backend/internal/model"
)

// promptModerationPreviewRunes 是列表里提示词摘要的最大字符数；全文只在详情接口返回。
const promptModerationPreviewRunes = 120

type PromptModerationLog struct {
	ID            string    `json:"id"`
	UserID        string    `json:"userId"`
	Username      string    `json:"username"`
	DisplayName   string    `json:"displayName"`
	Capability    string    `json:"capability"`
	ErrorCode     string    `json:"errorCode"`
	Error         string    `json:"error"`
	ResponseBody  string    `json:"responseBody"`
	RequestBody   string    `json:"-"`
	PromptPreview string    `json:"promptPreview"`
	PromptLength  int       `json:"promptLength"`
	CreatedAt     time.Time `json:"createdAt"`
}

// PromptModerationLogDetail 只在管理员查看单条记录时返回完整提示词。
type PromptModerationLogDetail struct {
	ID           string    `json:"id"`
	UserID       string    `json:"userId"`
	Username     string    `json:"username"`
	DisplayName  string    `json:"displayName"`
	Capability   string    `json:"capability"`
	ErrorCode    string    `json:"errorCode"`
	Error        string    `json:"error"`
	ResponseBody string    `json:"responseBody"`
	Prompt       string    `json:"prompt"`
	PromptLength int       `json:"promptLength"`
	CreatedAt    time.Time `json:"createdAt"`
}

func promptModerationPreview(text string) string {
	cleaned := strings.Join(strings.Fields(text), " ")
	runes := []rune(cleaned)
	if len(runes) <= promptModerationPreviewRunes {
		return cleaned
	}
	return string(runes[:promptModerationPreviewRunes]) + "…"
}

func (r *Repository) PromptModerationLogs(page, limit int) ([]PromptModerationLog, int64, error) {
	var total int64
	if err := r.db.Model(&model.ApiCallLog{}).Where("source = ?", "prompt_moderation").Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []PromptModerationLog
	err := r.db.Table("api_call_logs AS l").Select("l.id, l.user_id, COALESCE(u.username, '') AS username, COALESCE(u.display_name, '') AS display_name, l.capability, l.error_code, l.error, COALESCE(l.response_body, '') AS response_body, COALESCE(l.request_body, '') AS request_body, l.created_at").Joins("LEFT JOIN users AS u ON u.id = l.user_id").Where("l.source = ?", "prompt_moderation").Order("l.created_at DESC, l.id DESC").Offset((page - 1) * limit).Limit(limit).Scan(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	for index := range rows {
		rows[index].PromptPreview = promptModerationPreview(rows[index].RequestBody)
		rows[index].PromptLength = len([]rune(rows[index].RequestBody))
	}
	return rows, total, nil
}

func (r *Repository) PromptModerationLog(id string) (*PromptModerationLogDetail, error) {
	var row PromptModerationLogDetail
	if err := r.db.Table("api_call_logs AS l").Select("l.id, l.user_id, COALESCE(u.username, '') AS username, COALESCE(u.display_name, '') AS display_name, l.capability, l.error_code, l.error, COALESCE(l.response_body, '') AS response_body, COALESCE(l.request_body, '') AS prompt, l.created_at").Joins("LEFT JOIN users AS u ON u.id = l.user_id").Where("l.id = ? AND l.source = ?", id, "prompt_moderation").Scan(&row).Error; err != nil {
		return nil, err
	}
	if row.ID == "" {
		return nil, gorm.ErrRecordNotFound
	}
	row.PromptLength = len([]rune(row.Prompt))
	return &row, nil
}
