package platform

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"yingce/backend/internal/kernel"
	"yingce/backend/internal/model"

	"gorm.io/gorm"
)

// 全站默认模型：新用户首次登录时写入本地配置，用户仍可在账号设置里单独修改。
const defaultModelsSettingKey = "default_models"

// 与前端模型选项编码保持一致，形如 channelId::modelKey。
const defaultModelSeparator = "::"

type DefaultModels struct {
	ImageModel     string `json:"imageModel"`
	VideoModel     string `json:"videoModel"`
	AgentTextModel string `json:"agentTextModel"`
	OptimizerModel string `json:"optimizerModel"`
	AudioModel     string `json:"audioModel"`
}

type PublicDefaultModels struct {
	DefaultModels
	Configured bool      `json:"configured"`
	UpdatedAt  time.Time `json:"updatedAt,omitempty"`
}

func (s *Service) DefaultModels() (*PublicDefaultModels, error) {
	setting, value, err := s.readDefaultModels()
	if err != nil {
		return nil, err
	}
	return publicDefaultModels(setting, value), nil
}

func (s *Service) AdminDefaultModels(actor *model.User) (*PublicDefaultModels, error) {
	if err := s.requireAdmin(actor); err != nil {
		return nil, err
	}
	return s.DefaultModels()
}

func (s *Service) UpdateDefaultModels(actor *model.User, value DefaultModels) (*PublicDefaultModels, error) {
	if err := s.requireAdmin(actor); err != nil {
		return nil, err
	}
	validated, err := s.validateDefaultModels(value)
	if err != nil {
		return nil, err
	}
	current, before, err := s.readDefaultModels()
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(validated)
	if err != nil {
		return nil, err
	}
	setting := model.SystemSetting{Key: defaultModelsSettingKey, ValueJSON: string(encoded), UpdatedBy: actor.ID}
	if current != nil {
		setting.CreatedAt = current.CreatedAt
	}
	if err := s.repo.SaveSystemSetting(&setting); err != nil {
		return nil, err
	}
	if err := s.appendAdminAudit(actor, "default_models.update", "system_setting", defaultModelsSettingKey, "更新全站默认模型", map[string]any{"before": before, "after": validated}); err != nil {
		return nil, err
	}
	return publicDefaultModels(&setting, validated), nil
}

func (s *Service) readDefaultModels() (*model.SystemSetting, DefaultModels, error) {
	setting, err := s.repo.SystemSettingOptional(defaultModelsSettingKey)
	if err != nil {
		return nil, DefaultModels{}, err
	}
	value := DefaultModels{}
	if setting != nil && strings.TrimSpace(setting.ValueJSON) != "" {
		if err := json.Unmarshal([]byte(setting.ValueJSON), &value); err != nil {
			return nil, DefaultModels{}, kernel.BadAuthRequest("默认模型配置无法解析")
		}
	}
	return setting, value, nil
}

func publicDefaultModels(setting *model.SystemSetting, value DefaultModels) *PublicDefaultModels {
	result := &PublicDefaultModels{DefaultModels: value}
	if setting != nil {
		result.Configured = true
		result.UpdatedAt = setting.UpdatedAt
	}
	return result
}

// validateDefaultModels 校验每个默认值仍指向启用中的系统渠道模型，并核对能力类型。
func (s *Service) validateDefaultModels(value DefaultModels) (DefaultModels, error) {
	slots := []struct {
		label      string
		raw        string
		capability string
		out        *string
	}{
		{"默认生图模型", value.ImageModel, "image", &value.ImageModel},
		{"默认视频模型", value.VideoModel, "video", &value.VideoModel},
		{"默认 Agent 文本模型", value.AgentTextModel, "text", &value.AgentTextModel},
		{"默认润色模型", value.OptimizerModel, "text", &value.OptimizerModel},
		{"默认音频模型", value.AudioModel, "audio", &value.AudioModel},
	}
	for _, slot := range slots {
		normalized, err := s.validateDefaultModel(slot.label, slot.raw, slot.capability)
		if err != nil {
			return DefaultModels{}, err
		}
		*slot.out = normalized
	}
	return value, nil
}

func (s *Service) validateDefaultModel(label, raw, capability string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	parts := strings.SplitN(raw, defaultModelSeparator, 2)
	if len(parts) != 2 {
		return "", kernel.BadAuthRequest(label + "格式无效")
	}
	channelID, modelKey := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	if channelID == "" || modelKey == "" {
		return "", kernel.BadAuthRequest(label + "格式无效")
	}
	if _, err := s.repo.SystemChannel(channelID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", kernel.BadAuthRequest(label + "所选渠道不可用")
		}
		return "", err
	}
	item, err := s.repo.ChannelModelByKey(channelID, modelKey)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", kernel.BadAuthRequest(label + "所选模型不可用")
		}
		return "", err
	}
	if item.Capability != capability {
		return "", kernel.BadAuthRequest(label + "需要选择" + capabilityName(capability) + "模型")
	}
	return channelID + defaultModelSeparator + modelKey, nil
}

func capabilityName(capability string) string {
	switch capability {
	case "image":
		return "图片"
	case "video":
		return "视频"
	case "audio":
		return "音频"
	default:
		return "文本"
	}
}
