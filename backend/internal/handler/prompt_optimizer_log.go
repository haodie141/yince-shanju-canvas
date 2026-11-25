package handler

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
)

// promptOptimizerResponseError validates only the optimizer's Chat Completions
// tool result; an HTTP 200 stream can still contain invalid tool arguments.
func promptOptimizerResponseError(request, response []byte, streamed bool) string {
	var input struct {
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	if json.Unmarshal(request, &input) != nil {
		return ""
	}
	isOptimizer := false
	for _, tool := range input.Tools {
		if tool.Function.Name == "optimize_prompt" {
			isOptimizer = true
			break
		}
	}
	if !isOptimizer {
		return ""
	}

	type call struct {
		Index    int    `json:"index"`
		ID       string `json:"id"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	type message struct {
		Content   string `json:"content"`
		ToolCalls []call `json:"tool_calls"`
	}
	type event struct {
		Choices []struct {
			Delta        message `json:"delta"`
			Message      message `json:"message"`
			FinishReason string  `json:"finish_reason"`
		} `json:"choices"`
	}
	type accumulated struct {
		id, name string
		args     strings.Builder
	}
	calls := map[int]*accumulated{}
	var content strings.Builder
	finish := ""
	consume := func(raw []byte) bool {
		var value event
		if json.Unmarshal(raw, &value) != nil {
			return false
		}
		for _, choice := range value.Choices {
			if choice.FinishReason != "" {
				finish = choice.FinishReason
			}
			for _, msg := range []message{choice.Delta, choice.Message} {
				content.WriteString(msg.Content)
				for _, item := range msg.ToolCalls {
					c := calls[item.Index]
					if c == nil {
						c = &accumulated{}
						calls[item.Index] = c
					}
					if item.ID != "" {
						c.id = item.ID
					}
					if item.Function.Name != "" {
						c.name = item.Function.Name
					}
					c.args.WriteString(item.Function.Arguments)
				}
			}
		}
		return true
	}
	if streamed {
		scanner := bufio.NewScanner(bytes.NewReader(response))
		scanner.Buffer(make([]byte, 64<<10), len(response)+1)
		var lines []string
		flush := func() bool {
			raw := strings.TrimSpace(strings.Join(lines, "\n"))
			lines = nil
			return raw == "" || raw == "[DONE]" || consume([]byte(raw))
		}
		for scanner.Scan() {
			line := strings.TrimSuffix(scanner.Text(), "\r")
			if line == "" {
				if !flush() {
					return "润色响应流格式无效"
				}
				continue
			}
			if strings.HasPrefix(line, "data:") {
				lines = append(lines, strings.TrimPrefix(strings.TrimPrefix(line, "data:"), " "))
			}
		}
		if scanner.Err() != nil || !flush() {
			return "润色响应流格式无效"
		}
	} else if !consume(response) {
		return "润色响应格式无效"
	}

	value := content.String()
	chosenIndex, chosen := 0, false
	for index, c := range calls {
		if c.id != "" && c.name == "optimize_prompt" && (!chosen || index < chosenIndex) {
			chosenIndex, chosen = index, true
		}
	}
	if chosen && calls[chosenIndex].args.Len() > 0 {
		value = calls[chosenIndex].args.String()
	}
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "```") {
		value = strings.TrimSpace(value[3:])
		if len(value) >= 4 && strings.EqualFold(value[:4], "json") {
			value = strings.TrimSpace(value[4:])
		}
	}
	value = strings.TrimSuffix(strings.TrimSpace(value), "```")
	var output struct {
		OptimizedPrompt string `json:"optimizedPrompt"`
	}
	if json.Unmarshal([]byte(value), &output) != nil {
		start, end := strings.Index(value, "{"), strings.LastIndex(value, "}")
		if start < 0 || end <= start || json.Unmarshal([]byte(value[start:end+1]), &output) != nil {
			if finish == "length" {
				return "润色结果因输出长度限制被截断"
			}
			return "润色模型返回的工具参数不是有效 JSON"
		}
	}
	if strings.TrimSpace(output.OptimizedPrompt) == "" {
		if finish == "length" {
			return "润色结果因输出长度限制被截断"
		}
		return "润色模型未返回有效的 optimizedPrompt"
	}
	return ""
}
