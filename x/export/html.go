package export

import (
	"bytes"
	"fmt"
	"html"
	"html/template"
	"io"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/junk"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// md is a shared markdown renderer used to convert Text artifact
// content into HTML. goldmark.Markdown is safe for concurrent use
// per the upstream documentation, so a single package-level
// instance is sufficient.
var md = goldmark.New(
	goldmark.WithExtensions(extension.GFM),
)

// policy is the shared bluemonday UGCPolicy used to sanitize the
// output of the markdown renderer. *bluemonday.Policy is safe for
// concurrent use, so a single package-level instance is shared.
var policy = bluemonday.UGCPolicy()

// htmlTemplate renders the export document. CSS lives inline to keep
// the output a single self-contained file (no external <link> tags,
// no JavaScript, no remote fonts). Collapsibles use the native
// <details>/<summary> elements so the document works in email
// clients and JS-disabled browsers.
//
// Width, color, and spacing follow the existing palette but are
// tuned for wider reading widths (max-width: 960px) so the
// document does not look thin on displays >= 1920px.
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
			max-width: 960px;
			margin: 0 auto;
			padding: 24px;
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
		.unit {
			margin-bottom: 1em;
		}
		.unit:last-child {
			margin-bottom: 0;
		}
		.markdown p:first-child {
			margin-top: 0;
		}
		.markdown pre, .markdown code {
			background: #f7fafc;
			border: 1px solid #e2e8f0;
			border-radius: 4px;
			padding: 0.5em 0.75em;
			overflow-x: auto;
		}
		.markdown pre {
			padding: 0.75em 1em;
		}
		.markdown code {
			padding: 0.1em 0.3em;
			font-size: 0.9em;
		}
		.markdown blockquote {
			border-left: 3px solid #cbd5e0;
			margin: 0.75em 0;
			padding: 0.25em 0.75em;
			color: #4a5568;
		}
		.markdown table {
			border-collapse: collapse;
			margin: 0.75em 0;
		}
		.markdown th, .markdown td {
			border: 1px solid #e2e8f0;
			padding: 0.35em 0.65em;
			text-align: left;
		}
		details.reasoning-block,
		details.tool-block {
			margin: 0.5em 0;
			border: 1px solid #e2e8f0;
			border-radius: 4px;
			background: #f7fafc;
		}
		details.reasoning-block > summary,
		details.tool-block > summary {
			padding: 0.5em 0.75em;
			cursor: pointer;
			font-weight: 600;
			color: #2d3748;
		}
		details.reasoning-block[open] > summary,
		details.tool-block[open] > summary {
			border-bottom: 1px solid #e2e8f0;
		}
		details.reasoning-block > .reasoning-body,
		details.tool-block > .tool-body {
			padding: 0.75em;
			background: white;
		}
		details.tool-block > .tool-body pre {
			margin: 0;
			background: #fff5eb;
			padding: 0.5em;
			border-radius: 3px;
			overflow-x: auto;
		}
		.tool-call-name {
			font-weight: 600;
			color: #c05621;
			margin-bottom: 0.5em;
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
		.truncation-note {
			font-size: 0.75em;
			color: #718096;
			margin-top: 0.5em;
			font-style: italic;
		}
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
		{{ range .Units }}
		<div class="unit">
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

// htmlUnit is a pre-rendered chunk of HTML emitted inside a turn.
// One unit may bundle a paired ToolCall + ToolResult (so the user
// sees one collapsible per tool invocation) or a single artifact.
type htmlUnit struct {
	HTML template.HTML
}

// htmlTurn is the per-turn payload for the template.
type htmlTurn struct {
	Role      string
	RoleClass string
	Timestamp time.Time
	Units     []htmlUnit
}

// htmlData is the top-level template payload.
type htmlData struct {
	Thread *junk.Thread
	Turns  []htmlTurn
}

// HTML writes a self-contained HTML document representing the
// conversation thread to w. The output uses inline CSS, native
// <details>/<summary> collapsibles, and has no external
// dependencies. Text artifacts are rendered as markdown and
// sanitized by bluemonday.UGCPolicy before being embedded.
func HTML(w io.Writer, thread *junk.Thread) error {
	data := htmlData{
		Thread: thread,
		Turns:  make([]htmlTurn, 0, len(thread.State.Turns())),
	}

	for _, turn := range thread.State.Turns() {
		ht := htmlTurn{
			Role:      string(turn.Role),
			RoleClass: roleClass(turn.Role),
			Timestamp: turn.Timestamp,
			Units:     buildUnits(turn.Artifacts),
		}
		data.Turns = append(data.Turns, ht)
	}

	if err := tmpl.Execute(w, data); err != nil {
		return fmt.Errorf("execute html template: %w", err)
	}
	return nil
}

// buildUnits walks a turn's artifacts and pairs each ToolCall with
// the first subsequent ToolResult that references the same
// ToolCallID. A ToolCall without a matching ToolResult becomes a
// standalone collapsible; a ToolResult without a preceding matching
// ToolCall (unusual) also renders standalone.
func buildUnits(arts []artifact.Artifact) []htmlUnit {
	units := make([]htmlUnit, 0, len(arts))
	i := 0
	for i < len(arts) {
		art := arts[i]
		if tc, ok := art.(artifact.ToolCall); ok {
			matched := -1
			for j := i + 1; j < len(arts); j++ {
				tr, ok := arts[j].(artifact.ToolResult)
				if !ok {
					continue
				}
				if tr.ToolCallID == tc.ID {
					matched = j
					break
				}
			}
			if matched >= 0 {
				tr := arts[matched].(artifact.ToolResult)
				units = append(units, htmlUnit{HTML: renderToolPair(tc, tr)})
				i = matched + 1
				continue
			}
			units = append(units, htmlUnit{HTML: renderToolCallStandalone(tc)})
			i++
			continue
		}
		units = append(units, htmlUnit{HTML: renderArtifact(art)})
		i++
	}
	return units
}

func renderArtifact(art artifact.Artifact) template.HTML {
	switch a := art.(type) {
	case artifact.Text:
		return renderText(a)
	case artifact.Reasoning:
		return renderReasoning(a)
	case artifact.ToolResult:
		return renderToolResultStandalone(a)
	case artifact.Usage:
		return template.HTML(fmt.Sprintf(
			`<span class="usage">Tokens: prompt=%d / completion=%d / total=%d</span>`,
			a.PromptTokens, a.CompletionTokens, a.TotalTokens,
		))
	case artifact.Image:
		return template.HTML(fmt.Sprintf(
			`<img class="image" src="%s" alt="Image">`,
			escapeHTML(a.URL),
		))
	default:
		return template.HTML(fmt.Sprintf(
			`<div class="unknown">[Unknown artifact: %s]</div>`,
			escapeHTML(art.Kind()),
		))
	}
}

// renderText converts Text.Content markdown to sanitized HTML.
// Output is wrapped in a <div class="markdown"> for styling hooks.
// Goldmark's output goes through bluemonday.UGCPolicy which strips
// <script>, <iframe>, on* event handlers, javascript: URLs, and
// any other content the policy disallows. UGCPolicy is the right
// policy here because we trust the author of the conversation
// (the user) but the content itself originates from an LLM.
func renderText(a artifact.Text) template.HTML {
	if a.Content == "" {
		return template.HTML(`<div class="markdown"></div>`)
	}
	var buf bytes.Buffer
	if err := md.Convert([]byte(a.Content), &buf); err != nil {
		// Markdown conversion should not fail on any well-formed
		// input; if it does, fall back to escaped plaintext so the
		// export still renders something meaningful.
		return template.HTML(`<div class="markdown">` + escapeHTML(a.Content) + `</div>`)
	}
	sanitized := policy.SanitizeBytes(buf.Bytes())
	return template.HTML(`<div class="markdown">` + string(sanitized) + `</div>`)
}

// renderReasoning wraps Reasoning.Content in a <details> element
// collapsed by default, with "Reasoning" as the summary. The
// content is NOT markdown-rendered — reasoning text is typically
// stream-of-thought prose, and rendering it as markdown risks
// unintended visual changes (lists appearing where the model was
// just thinking in prose).
func renderReasoning(a artifact.Reasoning) template.HTML {
	return template.HTML(
		`<details class="reasoning-block"><summary>Reasoning</summary><div class="reasoning-body">` +
			escapeHTML(a.Content) +
			`</div></details>`,
	)
}

// renderToolPair renders a ToolCall and its matching ToolResult as
// a single collapsible. The tool name is the summary; clicking
// reveals the call body and the result body together.
func renderToolPair(tc artifact.ToolCall, tr artifact.ToolResult) template.HTML {
	return template.HTML(
		`<details class="tool-block"><summary>` +
			escapeHTML(tc.Name) +
			`</summary><div class="tool-body">` +
			`<div class="tool-call-name">Call</div><pre>` +
			escapeHTML(tc.MarkdownString()) +
			`</pre>` +
			renderToolResultInner(tr) +
			`</div></details>`,
	)
}

// renderToolCallStandalone renders a ToolCall without its result
// (the result was not in the same turn, or was never recorded).
func renderToolCallStandalone(tc artifact.ToolCall) template.HTML {
	return template.HTML(
		`<details class="tool-block"><summary>` +
			escapeHTML(tc.Name) +
			`</summary><div class="tool-body">` +
			`<pre>` +
			escapeHTML(tc.MarkdownString()) +
			`</pre></div></details>`,
	)
}

// renderToolResultStandalone renders a ToolResult without its
// matching ToolCall (unusual: the result is in the artifact stream
// without a corresponding call). It still wraps in <details> so
// the result remains collapsible.
func renderToolResultStandalone(tr artifact.ToolResult) template.HTML {
	return template.HTML(
		`<details class="tool-block"><summary>Result` +
			renderToolResultSummary(tr) +
			`</summary><div class="tool-body">` +
			renderToolResultInner(tr) +
			`</div></details>`,
	)
}

// renderToolResultInner returns the body of a tool result block:
// the optional ID header, the result body, and the truncation
// note when applicable. This is the inner content shared between
// the paired and standalone rendering paths.
func renderToolResultInner(tr artifact.ToolResult) string {
	var s string
	if tr.ToolCallID != "" {
		s += fmt.Sprintf(
			`<div class="tool-result-id">Result for %s</div>`,
			escapeHTML(tr.ToolCallID),
		)
	}
	cls := "tool-body-pre"
	if tr.IsError {
		cls += " tool-result-error"
	}
	s += fmt.Sprintf(`<pre class="%s">%s</pre>`, cls, escapeHTML(tr.MarkdownString()))

	if tr.Truncation != nil && tr.Truncation.Truncated() {
		style := escapeHTML(tr.Truncation.Style)
		note := fmt.Sprintf(
			"Truncated: shown %d of %d bytes, %d of %d lines",
			tr.Truncation.ShownBytes, tr.Truncation.OriginalBytes,
			tr.Truncation.ShownLines, tr.Truncation.OriginalLines,
		)
		if style != "" {
			note += fmt.Sprintf(" (%s)", style)
		}
		s += fmt.Sprintf(`<div class="truncation-note">%s</div>`, escapeHTML(note))
	}
	return s
}

// renderToolResultSummary is the parenthetical after "Result" in a
// standalone tool-result summary. For paired results the parent
// <details> already names the tool, so no parenthetical is needed.
func renderToolResultSummary(tr artifact.ToolResult) string {
	if tr.ToolCallID == "" {
		return ""
	}
	return " (" + escapeHTML(tr.ToolCallID) + ")"
}

func roleClass(r ledger.Role) string {
	switch r {
	case ledger.RoleUser:
		return "user"
	case ledger.RoleAssistant:
		return "assistant"
	case ledger.RoleSystem:
		return "system"
	case ledger.RoleTool:
		return "tool"
	default:
		return "unknown"
	}
}

// escapeHTML escapes the minimum set required to safely embed
// arbitrary LLM-provided text in HTML element and double-quoted
// attribute contexts. Attribute injection via unescaped " is
// prevented by escaping ".
func escapeHTML(s string) string {
	return html.EscapeString(s)
}
