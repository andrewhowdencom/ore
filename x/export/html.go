package export

import (
	"fmt"
	"html/template"
	"io"
	"strings"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/state"
)

const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
	<meta charset="UTF-8">
	<meta name="viewport" content="width=device-width, initial-scale=1.0">
	<title>Session {{ .Thread.ID }}</title>
	<style>
		body {
			font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
			line-height: 1.6;
			max-width: 800px;
			margin: 0 auto;
			padding: 20px;
			background: #f5f5f5;
			color: #333;
		}
		h1 {
			font-size: 1.5em;
			margin-bottom: 0.5em;
			color: #222;
		}
		.meta {
			font-size: 0.85em;
			color: #666;
			margin-bottom: 2em;
			padding-bottom: 1em;
			border-bottom: 1px solid #ddd;
		}
		.meta span {
			display: block;
			margin-bottom: 0.2em;
		}
		.turn {
			margin-bottom: 1.5em;
			padding: 1em;
			border-radius: 8px;
			background: white;
			box-shadow: 0 1px 3px rgba(0,0,0,0.1);
		}
		.turn-header {
			font-weight: 600;
			font-size: 0.8em;
			text-transform: uppercase;
			letter-spacing: 0.05em;
			margin-bottom: 0.75em;
			padding-bottom: 0.5em;
			border-bottom: 2px solid;
		}
		.turn-user .turn-header { color: #2c5282; border-color: #bee3f8; }
		.turn-assistant .turn-header { color: #276749; border-color: #c6f6d5; }
		.turn-system .turn-header { color: #744210; border-color: #feebc8; }
		.turn-tool .turn-header { color: #702459; border-color: #fed7e2; }
		.artifact {
			margin-bottom: 1em;
		}
		.artifact:last-child {
			margin-bottom: 0;
		}
		.text-content {
			white-space: pre-wrap;
			word-wrap: break-word;
		}
		.reasoning {
			background: #faf5ff;
			border-left: 3px solid #9f7aea;
			padding: 0.75em 1em;
			color: #553c9a;
			font-style: italic;
		}
		.tool-call {
			background: #fffaf0;
			border: 1px solid #fbd38d;
			border-radius: 4px;
			padding: 0.75em;
		}
		.tool-call-name {
			font-weight: 600;
			color: #c05621;
			margin-bottom: 0.5em;
		}
		.tool-call pre {
			margin: 0;
			background: #fff5eb;
			padding: 0.5em;
			border-radius: 3px;
			overflow-x: auto;
		}
		.tool-result {
			background: #f0fff4;
			border: 1px solid #9ae6b4;
			border-radius: 4px;
			padding: 0.75em;
		}
		.tool-result-id {
			font-size: 0.8em;
			color: #38a169;
			margin-bottom: 0.5em;
		}
		.tool-result-error {
			background: #fff5f5;
			border-color: #feb2b2;
		}
		.tool-result-error .tool-result-id { color: #e53e3e; }
		.usage {
			font-size: 0.75em;
			color: #718096;
			background: #edf2f7;
			padding: 0.4em 0.75em;
			border-radius: 3px;
			display: inline-block;
		}
		.image {
			max-width: 100%;
			border-radius: 4px;
			margin-top: 0.5em;
		}
		.unknown {
			color: #a0aec0;
			font-style: italic;
			font-size: 0.9em;
		}
		pre, code {
			font-family: "SFMono-Regular", Consolas, "Liberation Mono", Menlo, monospace;
			font-size: 0.9em;
		}
	</style>
</head>
<body>
	<h1>Session {{ .Thread.ID }}</h1>
	<div class="meta">
		<span><strong>Created:</strong> {{ .Thread.CreatedAt.Format "2006-01-02 15:04:05 MST" }}</span>
		{{ if not .Thread.UpdatedAt.IsZero }}<span><strong>Updated:</strong> {{ .Thread.UpdatedAt.Format "2006-01-02 15:04:05 MST" }}</span>{{ end }}
		{{ range $k, $v := .Thread.Metadata }}<span><strong>{{ $k }}:</strong> {{ $v }}</span>{{ end }}
	</div>
	{{ range .Turns }}
	<div class="turn turn-{{ .RoleClass }}">
		<div class="turn-header">{{ .Role }} {{ if not .Timestamp.IsZero }}<span style="font-weight:400;color:#a0aec0;">· {{ .Timestamp.Format "15:04:05" }}</span>{{ end }}</div>
		{{ range .Artifacts }}
		<div class="artifact">
			{{ .HTML }}
		</div>
		{{ end }}
	</div>
	{{ end }}
</body>
</html>
`

var tmpl *template.Template

func init() {
	tmpl = template.Must(template.New("export").Parse(htmlTemplate))
}

// htmlArtifact wraps an artifact with its rendered HTML.
type htmlArtifact struct {
	HTML template.HTML
}

// htmlTurn wraps a turn for template rendering.
type htmlTurn struct {
	Role      string
	RoleClass string
	Timestamp time.Time
	Artifacts []htmlArtifact
}

// htmlData is the top-level template data.
type htmlData struct {
	Thread *session.Thread
	Turns  []htmlTurn
}

// HTML writes a self-contained HTML document representing the conversation
// thread to w. The output uses inline CSS and has no external dependencies.
func HTML(w io.Writer, thread *session.Thread) error {
	data := htmlData{
		Thread: thread,
		Turns:  make([]htmlTurn, 0, len(thread.State.Turns())),
	}

	for _, turn := range thread.State.Turns() {
		ht := htmlTurn{
			Role:      string(turn.Role),
			RoleClass: roleClass(turn.Role),
			Timestamp: turn.Timestamp,
			Artifacts: make([]htmlArtifact, 0, len(turn.Artifacts)),
		}
		for _, art := range turn.Artifacts {
			ht.Artifacts = append(ht.Artifacts, htmlArtifact{
				HTML: template.HTML(artifactHTML(art)),
			})
		}
		data.Turns = append(data.Turns, ht)
	}

	if err := tmpl.Execute(w, data); err != nil {
		return fmt.Errorf("execute html template: %w", err)
	}
	return nil
}

func roleClass(r state.Role) string {
	switch r {
	case state.RoleUser:
		return "user"
	case state.RoleAssistant:
		return "assistant"
	case state.RoleSystem:
		return "system"
	case state.RoleTool:
		return "tool"
	default:
		return "unknown"
	}
}

func artifactHTML(art artifact.Artifact) string {
	switch a := art.(type) {
	case artifact.Text:
		return fmt.Sprintf("<div class=\"text-content\">%s</div>", escapeHTML(a.Content))
	case artifact.Reasoning:
		return fmt.Sprintf("<div class=\"reasoning\">%s</div>", escapeHTML(a.Content))
	case artifact.ToolCall:
		return fmt.Sprintf(
			"<div class=\"tool-call\"><div class=\"tool-call-name\">%s</div><pre>%s</pre></div>",
			escapeHTML(a.Name),
			escapeHTML(a.MarkdownString()),
		)
	case artifact.ToolResult:
		cls := "tool-result"
		if a.IsError {
			cls += " tool-result-error"
		}
		id := ""
		if a.ToolCallID != "" {
			id = fmt.Sprintf("<div class=\"tool-result-id\">Result for %s</div>", escapeHTML(a.ToolCallID))
		}
		return fmt.Sprintf(
			"<div class=\"%s\">%s<pre>%s</pre></div>",
			cls,
			id,
			escapeHTML(a.MarkdownString()),
		)
	case artifact.Usage:
		return fmt.Sprintf(
			"<span class=\"usage\">Tokens: prompt=%d / completion=%d / total=%d</span>",
			a.PromptTokens, a.CompletionTokens, a.TotalTokens,
		)
	case artifact.Image:
		return fmt.Sprintf("<img class=\"image\" src=\"%s\" alt=\"Image\">", escapeHTML(a.URL))
	default:
		return fmt.Sprintf("<div class=\"unknown\">[Unknown artifact: %s]</div>", escapeHTML(art.Kind()))
	}
}

func escapeHTML(s string) string {
	// Minimal escaping for HTML attribute and text contexts.
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}
