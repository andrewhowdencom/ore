package export

import (
	"bytes"
	"strings"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/ledger"
)

func TestText(t *testing.T) {
	tests := []struct {
		name       string
		thread     Thread
		wantSubstr []string
	}{
		{
			name: "empty thread",
			thread: Thread{
				ID: "thread-1",
			},
			wantSubstr: []string{"Thread: thread-1"},
		},
		{
			name: "text turn",
			thread: func() Thread {
				buf := ledger.NewThread()
				buf.Append(ledger.RoleUser, artifact.Text{Content: "Hello!"})
				return Thread{
					ID:    "thread-2",
					Turns: buf.Turns(),
				}
			}(),
			wantSubstr: []string{"=== user", "Hello!"},
		},
		{
			name: "assistant with reasoning and text",
			thread: func() Thread {
				buf := ledger.NewThread()
				buf.Append(ledger.RoleAssistant,
					artifact.Reasoning{Content: "Let me think..."},
					artifact.Text{Content: "The answer is 42."},
				)
				return Thread{
					ID:    "thread-3",
					Turns: buf.Turns(),
				}
			}(),
			wantSubstr: []string{
				"=== assistant",
				"[Reasoning]",
				"Let me think...",
				"The answer is 42.",
			},
		},
		{
			name: "tool call and matching tool result",
			thread: func() Thread {
				buf := ledger.NewThread()
				buf.Append(ledger.RoleAssistant, artifact.ToolCall{
					ID:        "call-1",
					Name:      "calc",
					Arguments: `{"expr":"2+2"}`,
				})
				buf.Append(ledger.RoleTool, artifact.ToolResult{
					ToolCallID: "call-1",
					Content:    "4",
				})
				return Thread{
					ID:    "thread-4",
					Turns: buf.Turns(),
				}
			}(),
			wantSubstr: []string{
				"=== assistant",
				"[Tool Call: calc]",
				`{"expr":"2+2"}`,
				"=== tool",
				"[Tool Result: call-1]",
				"4",
			},
		},
		{
			name: "image and usage",
			thread: func() Thread {
				buf := ledger.NewThread()
				buf.Append(ledger.RoleAssistant,
					artifact.Text{Content: "Here is an image."},
					artifact.Image{URL: "https://example.com/img.png"},
					artifact.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
				)
				return Thread{
					ID:    "thread-5",
					Turns: buf.Turns(),
				}
			}(),
			wantSubstr: []string{"Here is an image.", "[Image: https://example.com/img.png]", "prompt=10 completion=5 total=15"},
		},
		{
			name: "metadata",
			thread: Thread{
				ID:       "thread-6",
				Metadata: map[string]string{"key1": "val1", "key2": "val2"},
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