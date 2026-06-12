package artifact

// Truncation reports the relationship between the original tool
// result and the value that was actually surfaced to the LLM. It is
// emitted on artifact.ToolResult when a tool's output was bounded
// by the framework or by an explicit Format declaration.
//
// A nil *Truncation on a ToolResult means the result was not
// truncated. The zero-value Truncation is treated as "no truncation"
// by consumers that check Truncated.
type Truncation struct {
	// OriginalBytes is the byte length of the full, untruncated
	// result. Equal to ShownBytes when no truncation occurred.
	OriginalBytes int `json:"original_bytes,omitempty"`

	// OriginalLines is the line count of the full, untruncated
	// result. A trailing line without a newline is counted. Equal
	// to ShownLines when no truncation occurred.
	OriginalLines int `json:"original_lines,omitempty"`

	// ShownBytes is the byte length of the result that was
	// actually sent to the LLM. Always ≤ OriginalBytes.
	ShownBytes int `json:"shown_bytes"`

	// ShownLines is the line count of the result that was
	// actually sent to the LLM. Always ≤ OriginalLines.
	ShownLines int `json:"shown_lines"`

	// Style is "head" or "tail" — the truncation strategy that
	// was applied. Empty when no truncation occurred.
	Style string `json:"style,omitempty"`

	// RecoveryHint is the rendered (template-substituted) hint
	// that the framework appended to the truncated result. Empty
	// when no hint was configured or no truncation occurred.
	RecoveryHint string `json:"recovery_hint,omitempty"`
}

// Truncated reports whether t indicates that a result was
// shortened. A nil receiver returns false. A non-nil receiver with
// OriginalBytes > ShownBytes returns true.
func (t *Truncation) Truncated() bool {
	if t == nil {
		return false
	}
	return t.OriginalBytes > t.ShownBytes
}
