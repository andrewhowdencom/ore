package export

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/state"
)

// fixtureThread returns a thread containing at least one of each known
// artifact type (excluding deltas, which are never persisted).
func fixtureThread() *session.Thread {
	buf := &state.Buffer{}

	// System turn with reasoning.
	buf.Append(state.RoleSystem, artifact.Text{Content: "You are a helpful assistant."})

	// User turn with text.
	buf.Append(state.RoleUser, artifact.Text{Content: "What is the capital of France?"})

	// Assistant turn with reasoning, text, tool call, and usage.
	buf.Append(state.RoleAssistant,
		artifact.Reasoning{Content: "The user is asking for the capital of France."},
		artifact.Text{Content: "The capital of France is Paris."},
		artifact.ToolCall{
			ID:        "call-geo-1",
			Name:      "geography",
			Arguments: `{"country":"France"}`,
		},
		artifact.Usage{PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30},
	)

	// Tool turn with result.
	buf.Append(state.RoleTool, artifact.ToolResult{
		ToolCallID: "call-geo-1",
		Content:    "Paris",
	})

	// Assistant turn with image.
	buf.Append(state.RoleAssistant,
		artifact.Text{Content: "Here is a map."},
		artifact.Image{URL: "https://example.com/map.png"},
	)

	return &session.Thread{
		ID:        "fixture-thread",
		State:     buf,
		CreatedAt: time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC),
		UpdatedAt: time.Date(2024, 6, 1, 12, 5, 0, 0, time.UTC),
		Metadata:  map[string]string{"source": "integration-test"},
	}
}

func TestExportAllFormats(t *testing.T) {
	thread := fixtureThread()

	t.Run("text", func(t *testing.T) {
		var buf bytes.Buffer
		if err := Text(&buf, thread); err != nil {
			t.Fatalf("Text() error = %v", err)
		}
		got := buf.String()

		wantSubstrs := []string{
			"Thread: fixture-thread",
			"source: integration-test",
			"=== system",
			"You are a helpful assistant.",
			"=== user",
			"What is the capital of France?",
			"=== assistant",
			"[Reasoning]",
			"The user is asking for the capital of France.",
			"The capital of France is Paris.",
			"[Tool Call: geography]",
			`{"country":"France"}`,
			"[Usage] prompt=20 completion=10 total=30",
			"=== tool",
			"[Tool Result: call-geo-1]",
			"Paris",
			"[Image: https://example.com/map.png]",
		}
		for _, want := range wantSubstrs {
			if !strings.Contains(got, want) {
				t.Errorf("Text() missing substring %q\ngot:\n%s", want, got)
			}
		}
	})

	t.Run("html", func(t *testing.T) {
		var buf bytes.Buffer
		if err := HTML(&buf, thread); err != nil {
			t.Fatalf("HTML() error = %v", err)
		}
		got := buf.String()

		wantSubstrs := []string{
			"<!DOCTYPE html>",
			"<title>Session fixture-thread</title>",
			"<span><strong>source:</strong> integration-test</span>",
			"turn-system",
			"You are a helpful assistant.",
			"turn-user",
			"What is the capital of France?",
			"turn-assistant",
			"<div class=\"reasoning\">The user is asking for the capital of France.</div>",
			"The capital of France is Paris.",
			"<div class=\"tool-call-name\">geography</div>",
			"<div class=\"tool-result\">",
			"Result for call-geo-1",
			"Paris",
			"<span class=\"usage\">Tokens: prompt=20 / completion=10 / total=30</span>",
			"<img class=\"image\" src=\"https://example.com/map.png\" alt=\"Image\">",
		}
		for _, want := range wantSubstrs {
			if !strings.Contains(got, want) {
				t.Errorf("HTML() missing substring %q\ngot:\n%s", want, got)
			}
		}
	})

	t.Run("json", func(t *testing.T) {
		var buf bytes.Buffer
		if err := JSON(&buf, thread); err != nil {
			t.Fatalf("JSON() error = %v", err)
		}
		got := buf.String()

		// Must be valid JSON.
		var check map[string]any
		if err := json.Unmarshal([]byte(got), &check); err != nil {
			t.Fatalf("JSON() output is not valid JSON: %v\noutput:\n%s", err, got)
		}

		wantSubstrs := []string{
			`"id": "fixture-thread"`,
			`"source": "integration-test"`,
			`"role": "system"`,
			`"role": "user"`,
			`"role": "assistant"`,
			`"role": "tool"`,
			"You are a helpful assistant.",
			"What is the capital of France?",
			"Paris",
			`"kind": "text"`,
			`"kind": "reasoning"`,
			`"kind": "tool_call"`,
			`"kind": "tool_result"`,
			`"kind": "usage"`,
			`"kind": "image"`,
		}
		for _, want := range wantSubstrs {
			if !strings.Contains(got, want) {
				t.Errorf("JSON() missing substring %q\ngot:\n%s", want, got)
			}
		}
	})
}
