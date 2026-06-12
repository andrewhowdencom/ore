package bash

import (
	"fmt"
	"strings"

	"github.com/andrewhowdencom/ore/artifact"
)

// Result holds the output of a shell command execution. The
// streaming output capture in runCommand bounds each stream
// (stdout, stderr) to a rolling 2*frameworkDefaultTailCap tail in
// memory and, if a stream exceeds the cap, spills the full byte
// stream to a temp file. The path is exposed in StdoutPath /
// StderrPath so the LLM can read the full output via read_file.
type Result struct {
	Stdout     string             `json:"stdout"`
	Stderr     string             `json:"stderr"`
	ExitCode   int                `json:"exit_code"`
	StdoutPath string             `json:"stdout_path,omitempty"`
	StderrPath string             `json:"stderr_path,omitempty"`
	Truncation *artifact.Truncation `json:"truncation,omitempty"`
}

// MarshalMarkdown renders the result as human-readable Markdown.
// Fenced code blocks for stdout and stderr are omitted if empty.
func (r *Result) MarshalMarkdown() string {
	var md strings.Builder

	if r.Stdout != "" {
		md.WriteString("**stdout**\n\n```\n")
		md.WriteString(r.Stdout)
		md.WriteString("\n```\n\n")
	}

	if r.Stderr != "" {
		md.WriteString("**stderr**\n\n```\n")
		md.WriteString(r.Stderr)
		md.WriteString("\n```\n\n")
	}

	md.WriteString(fmt.Sprintf("**exit code:** %d", r.ExitCode))
	return md.String()
}

// MarshalLLM returns the LLM-facing string representation. It
// implements artifact.LLMRenderer; the framework handler respects
// this output verbatim (no further truncation). When the output
// was spilled to a temp file, the LLM-facing message includes a
// recovery hint pointing the model to the temp file.
//
// The recovery hint is rendered with the tool's standard
// template. The temp file path is included in the hint as {path}.
// A "X lines shown of Y total" notice is appended when stdout or
// stderr was truncated.
func (r *Result) MarshalLLM() string {
	var sb strings.Builder

	if r.Stdout != "" {
		sb.WriteString("**stdout**\n```\n")
		sb.WriteString(r.Stdout)
		sb.WriteString("\n```\n")
	}

	if r.Stderr != "" {
		sb.WriteString("\n**stderr**\n```\n")
		sb.WriteString(r.Stderr)
		sb.WriteString("\n```\n")
	}

	sb.WriteString(fmt.Sprintf("\n**exit code:** %d\n", r.ExitCode))

	if r.Truncation != nil && r.Truncation.Truncated() {
		sb.WriteString("\n[output truncated; full output at the temp file path(s) above]\n")
		// Compose a recovery hint that points the model at the
		// spilled file(s). If both streams spilled, mention both.
		var paths []string
		if r.StdoutPath != "" {
			paths = append(paths, fmt.Sprintf("stdout: %s", r.StdoutPath))
		}
		if r.StderrPath != "" {
			paths = append(paths, fmt.Sprintf("stderr: %s", r.StderrPath))
		}
		if len(paths) > 0 {
			sb.WriteString("Recovery: read ")
			sb.WriteString(strings.Join(paths, " and "))
			sb.WriteString(" with read_file, or use grep/tail/head on the file to extract the relevant lines.\n")
		}
	}

	return sb.String()
}

// Compile-time assertion: *Result implements artifact.LLMRenderer.
var _ artifact.LLMRenderer = (*Result)(nil)
