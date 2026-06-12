package tool

// Format declares how a tool's result should be rendered before being
// returned to the LLM. The zero value of Format is meaningful: it
// instructs the framework handler to apply default truncation (50 KB /
// 2000 lines, tail style) and no recovery hint. Tools that know their
// output is intentionally small (e.g. arithmetic) populate Format with
// TruncateConfig{} and an empty RecoveryHint to document that intent
// explicitly; the handler still consults the framework default, which is
// large enough not to truncate the result.
type Format struct {
	// Truncate configures byte and line caps on the LLM-facing string.
	// Zero values fall back to framework defaults. Use MaxBytes and/or
	// MaxLines to override; the smaller of the two caps wins.
	Truncate TruncateConfig

	// Style controls head-vs-tail truncation. The zero value is
	// StyleTail, which retains the END of the result (matching
	// terminal output conventions). StyleHead retains the START,
	// matching file-read conventions.
	Style TruncationStyle

	// RecoveryHint is a template string appended to a truncated
	// result. The framework substitutes {name} placeholders against
	// the truncation metadata; unknown placeholders are left
	// as-is. Examples:
	//
	//   "Use offset={next_offset} to continue reading."
	//   "Full output at {path}. Use grep/tail/head to extract."
	RecoveryHint string
}

// TruncateConfig configures the byte and line caps on a tool result.
// Zero values mean "use framework defaults" — the handler will
// substitute DefaultTruncateConfig() at application time.
type TruncateConfig struct {
	// MaxBytes is the maximum number of bytes to retain in the
	// LLM-facing result. Zero means use the framework default.
	MaxBytes int

	// MaxLines is the maximum number of newline-terminated lines to
	// retain. Zero means use the framework default. A trailing line
	// without a newline is not counted.
	MaxLines int
}

// TruncationStyle selects whether the kept portion of a truncated
// result is the head (start) or tail (end).
type TruncationStyle int

const (
	// StyleTail retains the END of the result. This is the zero
	// value and the default for most tools because terminal output
	// (errors, summaries) tends to live at the end.
	StyleTail TruncationStyle = iota

	// StyleHead retains the START of the result. Suitable for
	// tools that read the beginning of files or listings.
	StyleHead
)

// String returns a stable name for the truncation style. Used for
// observability metadata and the Style field on artifact.Truncation.
func (s TruncationStyle) String() string {
	switch s {
	case StyleHead:
		return "head"
	case StyleTail:
		return "tail"
	default:
		return "unknown"
	}
}

// FrameworkDefaultMaxBytes is the framework's default byte cap. It
// matches pi's truncate.ts default of 50 KB.
const FrameworkDefaultMaxBytes = 50_000

// FrameworkDefaultMaxLines is the framework's default line cap.
// It matches pi's truncate.ts default of 2000 lines.
const FrameworkDefaultMaxLines = 2000

// DefaultTruncateConfig returns the framework's default byte and
// line caps. The handler uses this when a tool's Format.Truncate has
// zero values.
func DefaultTruncateConfig() TruncateConfig {
	return TruncateConfig{
		MaxBytes: FrameworkDefaultMaxBytes,
		MaxLines: FrameworkDefaultMaxLines,
	}
}

// resolvedTruncateConfig returns the effective TruncateConfig for a
// Format. Zero fields are filled in from the framework defaults.
func (f Format) resolvedTruncateConfig() TruncateConfig {
	cfg := f.Truncate
	if cfg.MaxBytes <= 0 {
		cfg.MaxBytes = FrameworkDefaultMaxBytes
	}
	if cfg.MaxLines <= 0 {
		cfg.MaxLines = FrameworkDefaultMaxLines
	}
	return cfg
}
