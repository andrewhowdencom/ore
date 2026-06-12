package truncate

import (
	"strconv"
	"strings"

	"github.com/andrewhowdencom/ore/artifact"
)

// RenderHint substitutes {name} placeholders in tmpl with values
// from meta. Unknown placeholders are left as-is so a tool author
// can see the typo in the result rather than silently dropping the
// hint.
//
// The set of placeholders is the public field set of
// artifact.Truncation, plus a few pre-computed values that are
// commonly useful in tool descriptions:
//
//   - {original_bytes}, {original_lines}: pre-truncation sizes
//   - {shown_bytes}, {shown_lines}: post-truncation sizes
//   - {style}: "head" or "tail"
//   - {next_offset}: shown_lines + 1 (useful for read_file hints)
//   - {path}, {offset}, {limit}: extra fields a tool may attach
//
// Extras are tool-specific metadata for the recovery hint. They
// are passed in as a map[string]string; the function does not
// mutate the map.
func RenderHint(tmpl string, meta artifact.Truncation, extras ...map[string]string) string {
	if tmpl == "" {
		return ""
	}

	values := map[string]string{
		"original_bytes": strconv.Itoa(meta.OriginalBytes),
		"original_lines": strconv.Itoa(meta.OriginalLines),
		"shown_bytes":    strconv.Itoa(meta.ShownBytes),
		"shown_lines":    strconv.Itoa(meta.ShownLines),
		"style":          meta.Style,
		"recovery_hint":  meta.RecoveryHint,
		"next_offset":    strconv.Itoa(meta.ShownLines + 1),
	}
	for _, m := range extras {
		for k, v := range m {
			values[k] = v
		}
	}

	// Build a [name, value, name, value, ...] slice for strings.NewReplacer.
	pairs := make([]string, 0, 2*len(values))
	for k, v := range values {
		pairs = append(pairs, "{"+k+"}", v)
	}
	return strings.NewReplacer(pairs...).Replace(tmpl)
}
