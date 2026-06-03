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

func TestText(t *testing.T) {
	tests := []struct {
		name       string
		thread     *session.Thread
		wantSubstr []string
	}{
		{
			name: "empty thread",
			thread: &session.Thread{
				ID:        "thread-1",
				State:     &state.Buffer{},
				CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
				UpdatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			wantSubstr: []string{"Thread: thread-1", "Created: 2024-01-01T00:00:00Z"},
		},
		{
			name: "text turn",
			thread: func() *session.Thread {
				buf := &state.Buffer{}
				buf.Append(state.RoleUser, artifact.Text{Content: "Hello!"})
				return &session.Thread{
					ID:    "thread-2",
					State: buf,
				}
			}(),
			wantSubstr: []string{"=== user", "Hello!"},
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
					ID:    "thread-3",
					State: buf,
				}
			}(),
			wantSubstr: []string{"[Reasoning]", "Let me think...", "The answer is 42."},
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
					ID:    "thread-4",
					State: buf,
				}
			}(),
			wantSubstr: []string{"[Tool Call: calculator]", `{"expr":"1+1"}`, "[Tool Result: call-1]", "2"},
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
					ID:    "thread-5",
					State: buf,
				}
			}(),
			wantSubstr: []string{"Here is an image.", "[Image: https://example.com/img.png]", "prompt=10 completion=5 total=15"},
		},
		{
			name: "metadata",
			thread: &session.Thread{
				ID:        "thread-6",
				State:     &state.Buffer{},
				Metadata:  map[string]string{"key1": "val1", "key2": "val2"},
				CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			wantSubstr: []string{"key1: val1", "key2: val2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := Text(&buf, tt.thread); err != nil {
				t.Fatalf("Text() error = %v", err)
			}
			got := buf.String()
			for _, want := range tt.wantSubstr {
				if !strings.Contains(got, want) {
					t.Errorf("Text() output missing substring %q\ngot:\n%s", want, got)
				}
			}
		})
	}
}
