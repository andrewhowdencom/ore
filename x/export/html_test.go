package export

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/state"
)

func TestHTML(t *testing.T) {
	tests := []struct {
		name       string
		thread     *session.Thread
		wantSubstr []string
	}{
		{
			name: "empty thread",
			thread: &session.Thread{
				ID:        "thread-a",
				State:     &state.Buffer{},
				CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			wantSubstr: []string{
				"<!DOCTYPE html>",
				"<title>Session thread-a</title>",
				"<span><strong>Created:</strong> 2024-01-01 00:00:00 UTC</span>",
			},
		},
		{
			name: "text turn",
			thread: func() *session.Thread {
				buf := &state.Buffer{}
				buf.Append(state.RoleUser, artifact.Text{Content: "Hello!"})
				return &session.Thread{
					ID:    "thread-b",
					State: buf,
				}
			}(),
			wantSubstr: []string{
				"turn-user",
				"user",
				"<div class=\"text-content\">Hello!</div>",
			},
		},
		{
			name: "assistant with reasoning and text",
			thread: func() *session.Thread {
				buf := &state.Buffer{}
				buf.Append(state.RoleAssistant,
					artifact.Reasoning{Content: "Let me think..."},
					artifact.Text{Content: "The answer is 42."},
				)
				return &session.Thread{
					ID:    "thread-c",
					State: buf,
				}
			}(),
			wantSubstr: []string{
				"turn-assistant",
				"<div class=\"reasoning\">Let me think...</div>",
				"<div class=\"text-content\">The answer is 42.</div>",
			},
		},
		{
			name: "tool call and result",
			thread: func() *session.Thread {
				buf := &state.Buffer{}
				buf.Append(state.RoleAssistant, artifact.ToolCall{
					ID:        "call-1",
					Name:      "calculator",
					Arguments: `{"expr":"1+1"}`,
				})
				buf.Append(state.RoleTool, artifact.ToolResult{
					ToolCallID: "call-1",
					Content:    "2",
				})
				return &session.Thread{
					ID:    "thread-d",
					State: buf,
				}
			}(),
			wantSubstr: []string{
				"turn-tool",
				"<div class=\"tool-call-name\">calculator</div>",
				"<div class=\"tool-result\">",
				"Result for call-1",
			},
		},
		{
			name: "usage and image",
			thread: func() *session.Thread {
				buf := &state.Buffer{}
				buf.Append(state.RoleAssistant,
					artifact.Text{Content: "Here is an image."},
					artifact.Image{URL: "https://example.com/img.png"},
					artifact.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
				)
				return &session.Thread{
					ID:    "thread-e",
					State: buf,
				}
			}(),
			wantSubstr: []string{
				"<img class=\"image\" src=\"https://example.com/img.png\" alt=\"Image\">",
				"Tokens: prompt=10 / completion=5 / total=15",
			},
		},
		{
			name: "metadata",
			thread: &session.Thread{
				ID:        "thread-f",
				State:     &state.Buffer{},
				Metadata:  map[string]string{"key1": "val1"},
				CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			wantSubstr: []string{
				"<span><strong>key1:</strong> val1</span>",
			},
		},
		{
			name: "error tool result",
			thread: func() *session.Thread {
				buf := &state.Buffer{}
				buf.Append(state.RoleTool, artifact.ToolResult{
					ToolCallID: "call-err",
					Content:    "something broke",
					IsError:    true,
				})
				return &session.Thread{
					ID:    "thread-g",
					State: buf,
				}
			}(),
			wantSubstr: []string{
				"tool-result-error",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := HTML(&buf, tt.thread); err != nil {
				t.Fatalf("HTML() error = %v", err)
			}
			got := buf.String()
			for _, want := range tt.wantSubstr {
				if !strings.Contains(got, want) {
					t.Errorf("HTML() output missing substring %q\ngot:\n%s", want, got)
				}
			}
		})
	}
}
