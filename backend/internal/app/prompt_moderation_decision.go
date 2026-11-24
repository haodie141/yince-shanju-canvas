package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

const promptModerationMaxRunes = 5000

// promptModerationDecision is deliberately strict: unknown or malformed model
// output is an unavailable review, never an implicit approval.
type promptModerationDecision struct {
	Action string `json:"action"`
	Reason string `json:"reason,omitempty"`
}

func parsePromptModerationDecision(text string) (promptModerationDecision, error) {
	var decision promptModerationDecision
	decoder := json.NewDecoder(strings.NewReader(text))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return decision, errors.New("审核模型返回格式无效")
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] || (key != "action" && key != "reason") {
			return promptModerationDecision{}, errors.New("审核模型返回格式无效")
		}
		seen[key] = true
		var value string
		if err := decoder.Decode(&value); err != nil {
			return promptModerationDecision{}, errors.New("审核模型返回格式无效")
		}
		if key == "action" {
			decision.Action = value
		} else {
			decision.Reason = value
		}
	}
	if token, err := decoder.Token(); err != nil || token != json.Delim('}') {
		return promptModerationDecision{}, errors.New("审核模型返回格式无效")
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		return promptModerationDecision{}, errors.New("审核模型返回格式无效")
	}
	decision.Reason = strings.TrimSpace(decision.Reason)
	if decision.Action != "pass" && decision.Action != "reject" {
		return promptModerationDecision{}, errors.New("审核模型返回结果无效")
	}
	if decision.Action == "reject" && (decision.Reason == "" || utf8.RuneCountInString(decision.Reason) > 30) {
		return promptModerationDecision{}, errors.New("审核模型返回原因无效")
	}
	if decision.Action == "pass" && decision.Reason != "" {
		return promptModerationDecision{}, errors.New("审核模型返回格式无效")
	}
	return decision, nil
}

// Reject rather than truncate long prompts: trailing text must not bypass review.
func promptModerationInput(prompt string, limit int) (string, error) {
	if !utf8.ValidString(prompt) || limit <= 0 || utf8.RuneCountInString(prompt) > limit {
		return "", errors.New("提示词过长或格式无效，请精简后重新提交")
	}
	if len(bytes.TrimSpace([]byte(prompt))) == 0 {
		return "", errors.New("提示词不能为空")
	}
	return prompt, nil
}
