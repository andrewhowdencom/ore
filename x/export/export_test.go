package export

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/junk"
	"github.com/andrewhowdencom/ore/ledger"
)

// fixtureThread returns a thread containing at least one of each known
// artifact type (excluding deltas, which are never persisted).
func fixtureThread() *junk.Thread {
	buf := ledger.NewThread()

	// System turn with reasoning.
	buf.Append(ledger.RoleSystem, artifact.Text{Content: "You are a helpful assistant."})

	// User turn with text.
	buf.Append(ledger.RoleUser, artifact.Text{Content: "What is the capital of France?"})

	// Assistant turn with reasoning, text, and usage.
	buf.Append(ledger.RoleAssistant,
		artifact.Reasoning{Content: "The user is asking for the capital of France."},
		artifact.Text{Content: "The capital of France is **Paris**."},
		artifact.Usage{PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30},
	)

	// Tool call and its matching tool result in the SAME turn to
	// exercise the paired collapsible rendering in HTML.
	buf.Append(ledger.RoleAssistant,
		artifact.ToolCall{
			ID:        "call-geo-1",
			Name:      "geography",
			Arguments: `{"country":"France"}`,
		},
		artifact.ToolResult{
			ToolCallID: "call-geo-1",
			Content:    "Paris",
		},
	)

	// Separate tool turn to cover the "tool" role across all
	// formats. Its result is a follow-up call without a paired
	// call in the same turn.
	buf.Append(ledger.RoleTool, artifact.ToolResult{
		ToolCallID: "call-geo-2",
		Content:    "follow-up",
	})

	// Assistant turn with image.
	buf.Append(ledger.RoleAssistant,
		artifact.Text{Content: "Here is a map."},
		artifact.Image{URL: "https://example.com/map.png"},
	)

	return &junk.Thread{
		ID:        "fixture-thread",
		State:     buf,
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
			"The capital of France is **Paris**.",
			"[Tool Call: geography]",
			`{"country":"France"}`,
			"[Usage] prompt=20 completion=10 total=30",
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
			// Reasoning collapses under <details>.
			`<details class="reasoning-block">`,
			`<summary>Reasoning</summary>`,
			`<div class="reasoning-body">The user is asking for the capital of France.</div>`,
			// Text is markdown-rendered: **Paris** becomes <strong>Paris</strong>.
			`<div class="markdown">`,
			`<strong>Paris</strong>`,
			// ToolCall + ToolResult pair in same turn collapses under
			// one <details>, summary = tool name.
			`<details class="tool-block"><summary>geography</summary>`,
			`<div class="tool-call-name">Call</div>`,
			`<pre>{&#34;country&#34;:&#34;France&#34;}</pre>`,
			`<div class="tool-result-id">Result for call-geo-1</div>`,
			`>Paris<`,
			// Usage badge.
			`<span class="usage">Tokens: prompt=20 / completion=10 / total=30</span>`,
			// Image.
			`<img class="image" src="https://example.com/map.png" alt="Image">`,
			// Layout widened.
			`max-width: 960px`,
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