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

func TestHTML(t *testing.T) {
	tests := []struct {
		name       string
		thread     *junk.Thread
		wantSubstr []string
		wantAbsent []string
	}{
		{
			name: "empty thread",
			thread: &junk.Thread{
				ID:        "thread-a",
				State:     ledger.NewThread(),
				CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			wantSubstr: []string{
				"<!DOCTYPE html>",
				"<title>Session thread-a</title>",
				"<span><strong>Created:</strong> 2024-01-01 00:00:00 UTC</span>",
				`max-width: 960px`,
			},
		},
		{
			name: "text turn renders as markdown",
			thread: func() *junk.Thread {
				buf := ledger.NewThread()
				buf.Append(ledger.RoleUser, artifact.Text{Content: "Hello!"})
				return &junk.Thread{ID: "thread-b", State: buf}
			}(),
			wantSubstr: []string{
				"turn-user",
				"user",
				`<div class="markdown">`,
				`<p>Hello!</p>`,
			},
		},
		{
			name: "assistant with reasoning and text",
			thread: func() *junk.Thread {
				buf := ledger.NewThread()
				buf.Append(ledger.RoleAssistant,
					artifact.Reasoning{Content: "Let me think..."},
					artifact.Text{Content: "The answer is 42."},
				)
				return &junk.Thread{ID: "thread-c", State: buf}
			}(),
			wantSubstr: []string{
				"turn-assistant",
				`<details class="reasoning-block">`,
				`<summary>Reasoning</summary>`,
				`<div class="reasoning-body">Let me think...</div>`,
				`<div class="markdown">`,
				`<p>The answer is 42.</p>`,
			},
		},
		{
			// The common case: ToolCall in one turn, ToolResult in a
			// subsequent turn (assistant -> tool). They live in
			// DIFFERENT turns so they do not pair; each becomes its
			// own collapsible. The "<details>" sequence proves both
			// are present and each has its own summary.
			name: "tool call and result across separate turns",
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
				return &junk.Thread{ID: "thread-d", State: buf}
			}(),
			wantSubstr: []string{
				"turn-assistant",
				"turn-tool",
				`<details class="tool-block"><summary>calculator</summary>`,
				`<summary>Result (call-1)</summary>`,
				`Result for call-1`,
				`>2<`,
			},
		},
		{
			// Same-turn pairing: a ToolCall and its matching
			// ToolResult in the same turn collapse under ONE
			// <details> with the tool name as the summary.
			name: "tool call and result paired in same turn",
			thread: func() *junk.Thread {
				buf := ledger.NewThread()
				buf.Append(ledger.RoleAssistant,
					artifact.ToolCall{
						ID:        "call-paired",
						Name:      "read_file",
						Arguments: `{"path":"/etc/hostname"}`,
					},
					artifact.ToolResult{
						ToolCallID: "call-paired",
						Content:    "alpha\n",
					},
				)
				return &junk.Thread{ID: "thread-paired", State: buf}
			}(),
			wantSubstr: []string{
				// Exactly one open <details> for the pair; the
				// summary is the tool name. Both call body
				// and result body appear inside the same
				// collapsible.
				`<details class="tool-block"><summary>read_file</summary>`,
				`<div class="tool-body">`,
				`<div class="tool-call-name">Call</div>`,
				`<pre>{&#34;path&#34;:&#34;/etc/hostname&#34;}</pre>`,
				`<div class="tool-result-id">Result for call-paired</div>`,
			},
		},
		{
			// Markdown-rendered Text: **bold** becomes <strong>.
			// Code fences become <pre><code>.
			name: "markdown features render",
			thread: func() *junk.Thread {
				buf := ledger.NewThread()
				buf.Append(ledger.RoleUser, artifact.Text{
					Content: "Use **bold** and a code fence:\n\n```\nalpha\n```",
				})
				return &junk.Thread{ID: "thread-md", State: buf}
			}(),
			wantSubstr: []string{
				`<strong>bold</strong>`,
				`<pre><code>`,
				`alpha`,
			},
		},
		{
			// bluemonday UGCPolicy strips <script> entirely. The
			// literal "<script>" must not appear in the output.
			name: "script injection is sanitized",
			thread: func() *junk.Thread {
				buf := ledger.NewThread()
				buf.Append(ledger.RoleUser, artifact.Text{
					Content: "hi <script>alert(1)</script> there",
				})
				return &junk.Thread{ID: "thread-xss", State: buf}
			}(),
			wantAbsent: []string{
				`<script>alert(1)</script>`,
				`<script>`,
			},
		},
		{
			// Image URL with attribute-injection attempt: any " in
			// the URL must be escaped so the <img> tag stays
			// well-formed.
			name: "image url is escaped",
			thread: func() *junk.Thread {
				buf := ledger.NewThread()
				buf.Append(ledger.RoleAssistant, artifact.Image{
					URL: `https://example.com/a.png" onerror="alert(1)`,
				})
				return &junk.Thread{ID: "thread-img", State: buf}
			}(),
			wantSubstr: []string{
				`<img class="image" src="https://example.com/a.png&#34; onerror=&#34;alert(1)" alt="Image">`,
			},
			wantAbsent: []string{
				`onerror="alert(1)"`,
			},
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
				return &junk.Thread{ID: "thread-e", State: buf}
			}(),
			wantSubstr: []string{
				`<img class="image" src="https://example.com/img.png" alt="Image">`,
				`Tokens: prompt=10 / completion=5 / total=15`,
			},
		},
		{
			name: "metadata",
			thread: &junk.Thread{
				ID:        "thread-f",
				State:     ledger.NewThread(),
				Metadata:  map[string]string{"key1": "val1"},
				CreatedAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
			},
			wantSubstr: []string{
				`<span><strong>key1:</strong> val1</span>`,
			},
		},
		{
			name: "error tool result",
			thread: func() *junk.Thread {
				buf := ledger.NewThread()
				buf.Append(ledger.RoleTool, artifact.ToolResult{
					ToolCallID: "call-err",
					Content:    "something broke",
					IsError:    true,
				})
				return &junk.Thread{ID: "thread-g", State: buf}
			}(),
			wantSubstr: []string{
				`tool-result-error`,
			},
		},
		{
			// A truncated tool result surfaces a meta line below
			// the result body. The note has class
			// "truncation-note" and contains the byte/line counts
			// and the style.
			name: "truncated tool result surfaces note",
			thread: func() *junk.Thread {
				buf := ledger.NewThread()
				buf.Append(ledger.RoleTool, artifact.ToolResult{
					ToolCallID: "call-trunc",
					Content:    "shown portion",
					IsError:    false,
					Truncation: &artifact.Truncation{
						OriginalBytes: 50000,
						OriginalLines: 1000,
						ShownBytes:    5000,
						ShownLines:    100,
						Style:         "tail",
					},
				})
				return &junk.Thread{ID: "thread-trunc", State: buf}
			}(),
			wantSubstr: []string{
				`class="truncation-note"`,
				`Truncated: shown 5000 of 50000 bytes, 100 of 1000 lines (tail)`,
			},
		},
		{
			// Truncation with empty Style: no parenthetical is
			// appended.
			name: "truncation with empty style omits parens",
			thread: func() *junk.Thread {
				buf := ledger.NewThread()
				buf.Append(ledger.RoleTool, artifact.ToolResult{
					ToolCallID: "call-trunc-empty",
					Content:    "shown",
					Truncation: &artifact.Truncation{
						OriginalBytes: 1000,
						OriginalLines: 50,
						ShownBytes:    100,
						ShownLines:    5,
						Style:         "",
					},
				})
				return &junk.Thread{ID: "thread-trunc-empty", State: buf}
			}(),
			wantSubstr: []string{
				`class="truncation-note"`,
				`Truncated: shown 100 of 1000 bytes, 5 of 50 lines`,
			},
			wantAbsent: []string{
				`Truncated: shown 100 of 1000 bytes, 5 of 50 lines ()`,
			},
		},
		{
			// A non-truncated tool result must NOT surface a
			// truncation note even when Truncation is non-nil but
			// not "truncated".
			name: "untruncated tool result suppresses note",
			thread: func() *junk.Thread {
				buf := ledger.NewThread()
				buf.Append(ledger.RoleTool, artifact.ToolResult{
					ToolCallID: "call-untouched",
					Content:    "full content",
					Truncation: &artifact.Truncation{
						OriginalBytes: 12,
						OriginalLines: 1,
						ShownBytes:    12,
						ShownLines:    1,
					},
				})
				return &junk.Thread{ID: "thread-untouched", State: buf}
			}(),
			wantAbsent: []string{
				`Truncated: shown`,
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
			for _, miss := range tt.wantAbsent {
				if strings.Contains(got, miss) {
					t.Errorf("HTML() output contains forbidden substring %q\ngot:\n%s", miss, got)
				}
			}
		})
	}
}
