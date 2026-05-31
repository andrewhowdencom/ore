package bash

import "fmt"

// Result holds the output of a shell command execution.
type Result struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
}

// MarshalMarkdown renders the result as human-readable Markdown.
// Fenced code blocks for stdout and stderr are omitted if empty.
func (r *Result) MarshalMarkdown() string {
	var md string

	if r.Stdout != "" {
		md += "**stdout**\n\n```\n" + r.Stdout + "\n```\n\n"
	}

	if r.Stderr != "" {
		md += "**stderr**\n\n```\n" + r.Stderr + "\n```\n\n"
	}

	md += fmt.Sprintf("**exit code:** %d", r.ExitCode)

	return md
}
