package export

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/junk"
	"github.com/andrewhowdencom/ore/ledger"
)

func TestText(t *testing.T) {
	tests := []struct {
		name       string
		thread     *junk.Thread
		wantSubstr []string
	}{
		{
			name: "empty thread",
			thread: &junk.Thread{
				ID:        "thread-1",
				State:     ledger.NewThread(),
			},
			wantSubstr: []string{"Thread: thread-1", "Created: 2024-01-01T00:00:00Z"},
		},
		{
			name: "text turn",
			thread: func() *junk.Thread {
				buf := ledger.NewThread()
				buf.Append(ledger.RoleUser, artifact.Text{Content: "Hello!"})
				return &junk.Thread{
					ID:    "thread-2",
					State: buf,
				}
			}(),
			wantSubstr: []string{"=== user", "Hello!"},
		},
		{
			name: "assistant with reasoning and text",
			thread: func() *junk.Thread {
				buf := ledger.NewThread()
				buf.Append(ledger.RoleAssistant,
					artifact.Reasoning{Content: "Let me think..."},
					artifact.Text{Content: "The answer is 42."},
				)
				return &junk.Thread{
					ID:    "thread-3",
					State: buf,
				}
			}(),
			wantSubstr: []string{"[Reasoning]", "Let me think...", "The answer is 42."},
		},
		{
			name: "tool call and result",
			thread: func() *junk.Thread {
				buf := ledger.NewThread()
				buf.Append(ledger.RoleAssistant, artifact.ToolCall{
					ID:        "call-1",
					Name:      "calculator",
					Arguments: `{"expr":"1+1"}`,
				})
				buf.Append(ledger.RoleTool, artifact.ToolResult{
					ToolCallID: "call-1",
					Content:    "2",
				})
				return &junk.Thread{
					ID:    "thread-4",
					State: buf,
				}
			}(),
			wantSubstr: []string{"[Tool Call: calculator]", `{"expr":"1+1"}`, "[Tool Result: call-1]", "2"},
		},
		{
			name: "usage and image",
			thread: func() *junk.Thread {
				buf := ledger.NewThread()
				buf.Append(ledger.RoleAssistant,
					artifact.Text{Content: "Here is an image."},
					artifact.Image{URL: "https://example.com/img.png"},
					artifact.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
				)
				return &junk.Thread{
					ID:    "thread-5",
					State: buf,
				}
			}(),
			wantSubstr: []string{"Here is an image.", "[Image: https://example.com/img.png]", "prompt=10 completion=5 total=15"},
		},
		{
			name: "metadata",
			thread: &junk.Thread{
				ID:        "thread-6",
				State:     ledger.NewThread(),
				Metadata:  map[string]string{"key1": "val1", "key2": "val2"},
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
