package app

import (
	"strings"
	"testing"
)

func TestPromptModerationDecision(t *testing.T) {
	for _, tc := range []struct {
		input string
		valid bool
	}{
		{`{"action":"pass"}`, true},
		{`{"action":"reject","reason":"违规内容"}`, true},
		{`{"action":"pass"} {"action":"reject","reason":"违规内容"}`, false},
		{`{"action":"pass","extra":true}`, false},
		{`{"action":"reject","action":"pass"}`, false},
		{`{"action":"reject"}`, false},
		{`{"action":"reject","reason":"` + strings.Repeat("违", 31) + `"}`, false},
		{`{"action":"unknown"}`, false},
		{"```json\n{\"action\":\"pass\"}\n```", false},
	} {
		_, err := parsePromptModerationDecision(tc.input)
		if (err == nil) != tc.valid {
			t.Errorf("input %q: valid=%v, error=%v", tc.input, tc.valid, err)
		}
	}
}

func TestPromptModerationInputDoesNotTruncate(t *testing.T) {
	prompt := strings.Repeat("画", promptModerationMaxRunes)
	if got, err := promptModerationInput(prompt, promptModerationMaxRunes); err != nil || got != prompt {
		t.Fatalf("boundary prompt changed or rejected: %v", err)
	}
	if _, err := promptModerationInput(prompt+"危", promptModerationMaxRunes); err == nil {
		t.Fatal("overlong prompt was accepted")
	}
}
