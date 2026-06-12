// Package truncate provides the default byte- and line-bounded
// truncation for tool results.
package truncate

import (
	"strings"
	"unicode/utf8"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/tool"
)

// Truncate returns the truncated string and a Truncation descriptor
// reporting what was removed. It is a no-op when s is within the
// configured caps: in that case the returned string equals s, the
// Truncation has OriginalBytes == ShownBytes, and Truncated()
// returns false.
//
// style determines whether the kept portion is the head (start) or
// tail (end) of the input. The zero value tool.StyleTail is the
// default and matches terminal-output conventions.
//
// Both MaxBytes and MaxLines are consulted. The smaller of the two
// kept lengths wins. Cuts are made at UTF-8 rune boundaries; a
// multi-byte rune at the cut site is never split. Line caps count
// "\n" characters; a trailing line without a newline is dropped
// because it is partial.
//
// The function is deterministic and side-effect-free: the input
// string is never modified, and the returned Truncation is a value
// type with no shared state.
func Truncate(s string, cfg tool.TruncateConfig, style tool.TruncationStyle) (string, artifact.Truncation) {
	originalBytes := len(s)
	originalLines := countLines(s)

	if !exceedsCaps(s, cfg) {
		return s, artifact.Truncation{
			OriginalBytes: originalBytes,
			OriginalLines: originalLines,
			ShownBytes:    originalBytes,
			ShownLines:    originalLines,
		}
	}

	kept := cutToCaps(s, cfg, style)
	shownBytes := len(kept)
	shownLines := countLines(kept)

	return kept, artifact.Truncation{
		OriginalBytes: originalBytes,
		OriginalLines: originalLines,
		ShownBytes:    shownBytes,
		ShownLines:    shownLines,
		Style:         style.String(),
	}
}

// exceedsCaps reports whether s would be truncated under cfg.
// Either cap being exceeded is sufficient. MaxBytes <= 0 and
// MaxLines <= 0 are skipped (zero is "use default", which the
// caller has already resolved before reaching this function).
func exceedsCaps(s string, cfg tool.TruncateConfig) bool {
	if cfg.MaxBytes > 0 && len(s) > cfg.MaxBytes {
		return true
	}
	if cfg.MaxLines > 0 && countLines(s) > cfg.MaxLines {
		return true
	}
	return false
}

// cutToCaps returns the longest prefix (StyleHead) or suffix
// (StyleTail) of s that fits within cfg. The result is a substring
// of s cut at UTF-8 rune boundaries and (for line caps) at line
// boundaries.
//
// For StyleHead, the returned string is s[:end] for some end
// position. For StyleTail, the returned string is s[start:] for
// some start position. The end and start positions are computed
// from cfg.MaxBytes (the byte cap) and cfg.MaxLines (the line
// cap), with the smaller of the two kept lengths winning.
func cutToCaps(s string, cfg tool.TruncateConfig, style tool.TruncationStyle) string {
	// Step 1: byte cap. cut is interpreted as a length to keep
	// from the appropriate end of s.
	cut := len(s)
	if cfg.MaxBytes > 0 {
		cut = cfg.MaxBytes
		if cut > len(s) {
			cut = len(s)
		}
	}

	// Step 2: line cap. May shrink cut further so we don't include
	// a partial line.
	if cfg.MaxLines > 0 {
		cut = cutAtLineBoundary(s, cut, cfg.MaxLines, style)
	}

	if style == tool.StyleHead {
		// cut is a position in s (end of the kept prefix).
		if cut < len(s) {
			cut = snapToRuneStart(s, cut)
		}
		return s[:cut]
	}

	// StyleTail: cut is a length; convert to a start position.
	start := len(s) - cut
	if start < 0 {
		start = 0
	}
	if start > 0 && start < len(s) {
		start = snapToRuneStart(s, start)
	}
	return s[start:]
}

// cutAtLineBoundary shrinks cut so the kept region contains
// exactly linesKept complete newline-terminated lines.
//
// For StyleHead, the candidate is s[:cut]. We walk forward from
// the start of the candidate, counting newlines; the boundary is
// the byte just after the linesKept-th newline. The kept region
// INCLUDES that newline, so any partial trailing line is dropped.
//
// For StyleTail, the candidate is s[len(s)-cut:]. We count the
// total lines in the candidate, and drop the leading
// (totalLines - linesKept) lines. The boundary is the byte just
// after the (totalLines - linesKept)-th newline in the candidate.
// The kept region starts AFTER that newline, so the partial
// leading line is dropped.
//
// In either style, if the candidate does not contain enough
// complete lines to satisfy the constraint, the function returns
// the unmodified cut — the byte cap dominates, and the partial
// lines on the boundary are kept (truncated by the byte cap).
func cutAtLineBoundary(s string, cut, linesKept int, style tool.TruncationStyle) int {
	if cut > len(s) {
		cut = len(s)
	}
	if style == tool.StyleHead {
		// Walk forward through s[:cut], counting newlines.
		// Return the byte position just after the linesKept-th
		// newline.
		idx := -1
		for i := 0; i < linesKept; i++ {
			next := strings.IndexByte(s[idx+1:cut], '\n')
			if next < 0 {
				// Not enough newlines in the candidate;
				// byte cap dominates.
				return cut
			}
			idx = idx + 1 + next
		}
		// idx is the position of the linesKept-th newline; the
		// kept prefix is s[:idx+1].
		return idx + 1
	}

	// StyleTail: candidate is s[len(s)-cut:].
	cs := len(s) - cut
	candidate := s[cs:]
	totalLines := countLines(candidate)
	linesToDrop := totalLines - linesKept
	if linesToDrop <= 0 {
		// Candidate already has linesKept or fewer lines; byte
		// cap dominates.
		return cut
	}
	// Walk forward through the candidate, counting newlines.
	// The boundary is just after the linesToDrop-th newline.
	// Then the kept tail is everything after, of length
	// (cut - (boundaryPosition - cs)) = (cut - linesToDropNewlinesTotalBytes).
	// We return the new "cut" length = cut - (newCs - cs).
	idx := 0
	for i := 0; i < linesToDrop; i++ {
		next := strings.IndexByte(candidate[idx+1:], '\n')
		if next < 0 {
			// Should not happen given the linesToDrop>0 check,
			// but be defensive.
			return cut
		}
		idx = idx + 1 + next
	}
	// idx is the offset in the candidate of the linesToDrop-th
	// newline. We want to keep everything after that newline;
	// the new tail starts at offset idx+1 in the candidate,
	// which is position cs+idx+1 in s. The new cut length is
	// len(s) - (cs + idx + 1) = cut - (idx + 1).
	return cut - (idx + 1)
}

// snapToRuneStart returns the largest index ≤ i such that s[i:]
// starts at a UTF-8 rune boundary. If s[i] is already at a rune
// boundary, i is returned unchanged. If i is 0 or i == len(s), i
// is returned unchanged.
func snapToRuneStart(s string, i int) int {
	if i <= 0 || i >= len(s) {
		return i
	}
	// Walk backward until we find a byte that is NOT a UTF-8
	// continuation byte (0b10xxxxxx). The rune boundary is just
	// after that byte — i.e., at its position.
	for j := i; j > 0; j-- {
		if utf8.RuneStart(s[j]) {
			return j
		}
	}
	// Should not reach here: there's always a rune start at index 0.
	return 0
}

// countLines counts newline characters in s. A trailing line
// without a newline is counted (so an empty string has 0 lines,
// "a" has 1 line, "a\n" has 1 line, "a\nb" has 2 lines).
func countLines(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	// If the string doesn't end with \n, the last line is implicit.
	if s[len(s)-1] != '\n' {
		n++
	}
	return n
}
