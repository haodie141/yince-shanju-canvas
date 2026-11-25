package handler

import "testing"

func TestPromptOptimizerResponseError(t *testing.T) {
	request := []byte(`{"tools":[{"type":"function","function":{"name":"optimize_prompt"}}]}`)
	tests := []struct {
		name, response, want string
		streamed             bool
	}{
		{"valid tool stream", "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"x\",\"function\":{\"name\":\"optimize_prompt\",\"arguments\":\"{\\\"optimized\"}}]}}]}\n\ndata: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"Prompt\\\":\\\"hello\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n", "", true},
		{"invalid tool JSON", `{"choices":[{"message":{"tool_calls":[{"id":"x","function":{"name":"optimize_prompt","arguments":"{\"optimizedPrompt\">5: \"\"}"}}]}}]}`, "润色模型返回的工具参数不是有效 JSON", false},
		{"missing prompt", `{"choices":[{"message":{"tool_calls":[{"id":"x","function":{"name":"optimize_prompt","arguments":"{\"optimizedPrompt\":\"\"}"}}]}}]}`, "润色模型未返回有效的 optimizedPrompt", false},
		{"content fallback", `{"choices":[{"message":{"content":"Here is the result: {\"optimizedPrompt\":\"hello\"}"}}]}`, "", false},
		{"length limit", "data: {\"choices\":[{\"delta\":{},\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n", "润色结果因输出长度限制被截断", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := promptOptimizerResponseError(request, []byte(tc.response), tc.streamed); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
	if got := promptOptimizerResponseError([]byte(`{"tools":[]}`), []byte(`not json`), false); got != "" {
		t.Fatalf("unrelated request: %q", got)
	}
}
