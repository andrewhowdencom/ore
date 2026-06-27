package export

import (
	"fmt"
	"io"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/junk"
)

// Text writes a plain-text transcript of the conversation thread to w.
// Each turn is emitted with its role and timestamp, followed by every
// artifact rendered in a human-readable form.
func Text(w io.Writer, thread *junk.Thread) error {
	_, err := fmt.Fprintf(w, "Thread: %s\n", thread.ID)
	if err != nil {
		return fmt.Errorf("write thread header: %w", err)
	}

	if !thread.CreatedAt.IsZero() {
		_, err = fmt.Fprintf(w, "Created: %s\n", thread.CreatedAt.Format(time.RFC3339))
		if err != nil {
			return fmt.Errorf("write created at: %w", err)
		}
	}

	if !thread.UpdatedAt.IsZero() {
		_, err = fmt.Fprintf(w, "Updated: %s\n", thread.UpdatedAt.Format(time.RFC3339))
		if err != nil {
			return fmt.Errorf("write updated at: %w", err)
		}
	}

	if len(thread.Metadata) > 0 {
		_, err = fmt.Fprintln(w, "Metadata:")
		if err != nil {
			return fmt.Errorf("write metadata header: %w", err)
		}
		for k, v := range thread.Metadata {
			_, err = fmt.Fprintf(w, "  %s: %s\n", k, v)
			if err != nil {
				return fmt.Errorf("write metadata entry: %w", err)
			}
		}
	}

	_, err = fmt.Fprintln(w)
	if err != nil {
		return fmt.Errorf("write blank line: %w", err)
	}

	for _, turn := range thread.State.Turns() {
		ts := ""
		if !turn.Timestamp.IsZero() {
			ts = turn.Timestamp.Format(time.RFC3339)
		}

		_, err = fmt.Fprintf(w, "=== %s", turn.Role)
		if err != nil {
			return fmt.Errorf("write turn role: %w", err)
		}
		if ts != "" {
			_, err = fmt.Fprintf(w, " (%s)", ts)
			if err != nil {
				return fmt.Errorf("write turn timestamp: %w", err)
			}
		}
		_, err = fmt.Fprintln(w, " ===")
		if err != nil {
			return fmt.Errorf("write turn separator: %w", err)
		}

		for i, art := range turn.Artifacts {
			if i > 0 {
				_, err = fmt.Fprintln(w)
				if err != nil {
					return fmt.Errorf("write artifact separator: %w", err)
				}
			}
			if err := writeArtifactText(w, art); err != nil {
				return fmt.Errorf("write artifact: %w", err)
			}
		}

		_, err = fmt.Fprintln(w)
		if err != nil {
			return fmt.Errorf("write turn trailing blank: %w", err)
		}
	}

	return nil
}

func writeArtifactText(w io.Writer, art artifact.Artifact) error {
	switch a := art.(type) {
	case artifact.Text:
		_, err := fmt.Fprint(w, a.Content)
		return err
	case artifact.Reasoning:
		_, err := fmt.Fprintf(w, "[Reasoning]\n%s", a.Content)
		return err
	case artifact.ToolCall:
		_, err := fmt.Fprintf(w, "[Tool Call: %s]\n%s", a.Name, a.MarkdownString())
		return err
	case artifact.ToolResult:
		prefix := "[Tool Result"
		if a.ToolCallID != "" {
			prefix += ": " + a.ToolCallID
		}
		if a.IsError {
			prefix += " (error)"
		}
		prefix += "]\n"
		_, err := fmt.Fprint(w, prefix)
		if err != nil {
			return err
		}
		_, err = fmt.Fprint(w, a.MarkdownString())
		return err
	case artifact.Usage:
		_, err := fmt.Fprintf(w, "[Usage] prompt=%d completion=%d total=%d",
			a.PromptTokens, a.CompletionTokens, a.TotalTokens)
		return err
	case artifact.Image:
		_, err := fmt.Fprintf(w, "[Image: %s]", a.URL)
		return err
	default:
		_, err := fmt.Fprintf(w, "[Unknown artifact: %s]", art.Kind())
		return err
	}
}
