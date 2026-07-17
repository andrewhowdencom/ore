package export

import (
	"fmt"
	"io"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
)

// Text writes a plain-text transcript of the conversation thread to w.
// Each turn is emitted with its role and timestamp, followed by every
// artifact rendered in a human-readable form.
func Text(w io.Writer, t Thread) error {
	if _, err := fmt.Fprintf(w, "Thread: %s\n", t.ID); err != nil {
		return fmt.Errorf("write thread header: %w", err)
	}

	if len(t.Metadata) > 0 {
		if _, err := fmt.Fprintln(w, "Metadata:"); err != nil {
			return fmt.Errorf("write metadata header: %w", err)
		}
		for k, v := range t.Metadata {
			if _, err := fmt.Fprintf(w, "  %s: %s\n", k, v); err != nil {
				return fmt.Errorf("write metadata entry: %w", err)
			}
		}
	}

	if _, err := fmt.Fprintln(w); err != nil {
		return fmt.Errorf("write blank line: %w", err)
	}

	for _, turn := range t.Turns {
		ts := ""
		if !turn.Timestamp.IsZero() {
			ts = turn.Timestamp.Format(time.RFC3339)
		}

		if _, err := fmt.Fprintf(w, "=== %s", turn.Role); err != nil {
			return fmt.Errorf("write turn role: %w", err)
		}
		if ts != "" {
			if _, err := fmt.Fprintf(w, " (%s)", ts); err != nil {
				return fmt.Errorf("write turn timestamp: %w", err)
			}
		}
		if _, err := fmt.Fprintln(w, " ==="); err != nil {
			return fmt.Errorf("write turn separator: %w", err)
		}

		for i, art := range turn.Artifacts {
			if i > 0 {
				if _, err := fmt.Fprintln(w); err != nil {
					return fmt.Errorf("write artifact separator: %w", err)
				}
			}
			if err := writeArtifactText(w, art); err != nil {
				return fmt.Errorf("write artifact: %w", err)
			}
		}

		if _, err := fmt.Fprintln(w); err != nil {
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