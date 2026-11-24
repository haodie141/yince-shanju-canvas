package repository

import (
	"time"
	"yingce/backend/internal/model"
)

type PromptModerationLog struct {
	ID           string    `json:"id"`
	UserID       string    `json:"userId"`
	Username     string    `json:"username"`
	DisplayName  string    `json:"displayName"`
	Capability   string    `json:"capability"`
	ErrorCode    string    `json:"errorCode"`
	Error        string    `json:"error"`
	ResponseBody string    `json:"responseBody"`
	CreatedAt    time.Time `json:"createdAt"`
}

func (r *Repository) PromptModerationLogs(page, limit int) ([]PromptModerationLog, int64, error) {
	var total int64
	if err := r.db.Model(&model.ApiCallLog{}).Where("source = ?", "prompt_moderation").Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []PromptModerationLog
	err := r.db.Table("api_call_logs AS l").Select("l.id, l.user_id, COALESCE(u.username, '') AS username, COALESCE(u.display_name, '') AS display_name, l.capability, l.error_code, l.error, COALESCE(l.response_body, '') AS response_body, l.created_at").Joins("LEFT JOIN users AS u ON u.id = l.user_id").Where("l.source = ?", "prompt_moderation").Order("l.created_at DESC, l.id DESC").Offset((page - 1) * limit).Limit(limit).Scan(&rows).Error
	return rows, total, err
}
