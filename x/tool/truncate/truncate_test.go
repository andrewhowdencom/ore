package truncate

import (
	"strings"
	"testing"

	"github.com/andrewhowdencom/ore/tool"
)

// longString returns a string of n bytes containing the letter 'a'.
// Used as a baseline "no surprises" input for byte-cap tests.
func longString(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat("a", n)
}

// multiByteString builds a string that is n bytes long using
// 2-byte UTF-8 runes (Greek alpha, U+03B1). The string contains
// n/2 runes, and any byte index in [1, 3, 5, ...] is a continuation
// byte.
func multiByteString(n int) string {
	if n <= 0 {
		return ""
	}
	r := []byte{0xCE, 0xB1} // alpha
	out := make([]byte, 0, n)
	for len(out) < n {
		out = append(out, r...)
	}
	return string(out[:n])
}

func TestTruncate_UnderBothCaps_NoTruncation(t *testing.T) {
	t.Parallel()

	s := "short string"
	out, trunc := Truncate(s, tool.TruncateConfig{MaxBytes: 100, MaxLines: 100}, tool.StyleTail)

	if out != s {
		t.Errorf("out = %q, want %q", out, s)
	}
	if trunc.OriginalBytes != len(s) {
		t.Errorf("OriginalBytes = %d, want %d", trunc.OriginalBytes, len(s))
	}
	if trunc.ShownBytes != len(s) {
		t.Errorf("ShownBytes = %d, want %d", trunc.ShownBytes, len(s))
	}
	if trunc.Truncated() {
		t.Errorf("Truncated() = true, want false")
	}
}

func TestTruncate_EmptyString(t *testing.T) {
	t.Parallel()

	out, trunc := Truncate("", tool.TruncateConfig{MaxBytes: 50, MaxLines: 10}, tool.StyleTail)
	if out != "" {
		t.Errorf("out = %q, want empty", out)
	}
	if trunc.OriginalBytes != 0 || trunc.ShownBytes != 0 {
		t.Errorf("OriginalBytes=%d ShownBytes=%d, want both 0", trunc.OriginalBytes, trunc.ShownBytes)
	}
	if trunc.OriginalLines != 0 || trunc.ShownLines != 0 {
		t.Errorf("OriginalLines=%d ShownLines=%d, want both 0", trunc.OriginalLines, trunc.ShownLines)
	}
}

func TestTruncate_ByteCapTail(t *testing.T) {
	t.Parallel()

	s := longString(1000)
	out, trunc := Truncate(s, tool.TruncateConfig{MaxBytes: 50}, tool.StyleTail)

	if len(out) != 50 {
		t.Errorf("len(out) = %d, want 50", len(out))
	}
	// Tail: out should be the LAST 50 bytes.
	if out != s[len(s)-50:] {
		t.Errorf("out is not the tail")
	}
	if trunc.Style != "tail" {
		t.Errorf("Style = %q, want tail", trunc.Style)
	}
	if !trunc.Truncated() {
		t.Errorf("Truncated() = false, want true")
	}
	if trunc.OriginalBytes != 1000 {
		t.Errorf("OriginalBytes = %d, want 1000", trunc.OriginalBytes)
	}
	if trunc.ShownBytes != 50 {
		t.Errorf("ShownBytes = %d, want 50", trunc.ShownBytes)
	}
}

func TestTruncate_ByteCapHead(t *testing.T) {
	t.Parallel()

	s := longString(1000)
	out, trunc := Truncate(s, tool.TruncateConfig{MaxBytes: 50}, tool.StyleHead)

	if len(out) != 50 {
		t.Errorf("len(out) = %d, want 50", len(out))
	}
	// Head: out should be the FIRST 50 bytes.
	if out != s[:50] {
		t.Errorf("out is not the head")
	}
	if trunc.Style != "head" {
		t.Errorf("Style = %q, want head", trunc.Style)
	}
}

func TestTruncate_LineCapTail(t *testing.T) {
	t.Parallel()

	s := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	// 5 complete lines. Keep last 2: "line 4\nline 5\n" = 14 bytes.
	out, trunc := Truncate(s, tool.TruncateConfig{MaxLines: 2}, tool.StyleTail)

	if out != "line 4\nline 5\n" {
		t.Errorf("out = %q, want %q", out, "line 4\nline 5\n")
	}
	if trunc.ShownLines != 2 {
		t.Errorf("ShownLines = %d, want 2", trunc.ShownLines)
	}
	if trunc.OriginalLines != 5 {
		t.Errorf("OriginalLines = %d, want 5", trunc.OriginalLines)
	}
}

func TestTruncate_LineCapHead(t *testing.T) {
	t.Parallel()

	s := "line 1\nline 2\nline 3\nline 4\nline 5\n"
	// Keep first 2: "line 1\nline 2\n" = 14 bytes.
	out, trunc := Truncate(s, tool.TruncateConfig{MaxLines: 2}, tool.StyleHead)

	if out != "line 1\nline 2\n" {
		t.Errorf("out = %q, want %q", out, "line 1\nline 2\n")
	}
	if trunc.ShownLines != 2 {
		t.Errorf("ShownLines = %d, want 2", trunc.ShownLines)
	}
}

func TestTruncate_LineCapDropsTrailingPartialLine_Tail(t *testing.T) {
	t.Parallel()

	s := "line 1\nline 2\nline 3"
	// 3 lines (last has no newline). Keep last 2 lines from the
	// end: "line 2\nline 3" = 12 bytes.
	out, trunc := Truncate(s, tool.TruncateConfig{MaxLines: 2}, tool.StyleTail)

	if out != "line 2\nline 3" {
		t.Errorf("out = %q, want %q", out, "line 2\nline 3")
	}
	if trunc.ShownLines != 2 {
		t.Errorf("ShownLines = %d, want 2", trunc.ShownLines)
	}
}

func TestTruncate_LineCapDropsTrailingPartialLine_Head(t *testing.T) {
	t.Parallel()

	s := "line 1\nline 2\nline 3"
	// Keep first 2 lines from the start: "line 1\nline 2\n" = 14
	// bytes. The "partial" trailing "line 3" is dropped.
	out, trunc := Truncate(s, tool.TruncateConfig{MaxLines: 2}, tool.StyleHead)

	if out != "line 1\nline 2\n" {
		t.Errorf("out = %q, want %q", out, "line 1\nline 2\n")
	}
	if trunc.ShownLines != 2 {
		t.Errorf("ShownLines = %d, want 2", trunc.ShownLines)
	}
}

func TestTruncate_ByteCapSmallerThanLineCap_Tail(t *testing.T) {
	t.Parallel()

	// 1000 bytes, 5 lines. MaxBytes=10, MaxLines=2.
	// Byte cap dominates: we keep 10 bytes from the end.
	s := strings.Repeat("a", 990) + "\nline2\nline3\nline4\nline5\n"
	out, trunc := Truncate(s, tool.TruncateConfig{MaxBytes: 10, MaxLines: 2}, tool.StyleTail)

	if len(out) != 10 {
		t.Errorf("len(out) = %d, want 10", len(out))
	}
	// The byte cap is 10 from the end, but it doesn't cleanly
	// align with newlines; the line cap cannot shrink it further
	// because there are not enough newlines in the last 10 bytes.
	if !trunc.Truncated() {
		t.Errorf("Truncated() = false, want true")
	}
}

func TestTruncate_ByteCapExactlyAtLineBoundary_Tail(t *testing.T) {
	t.Parallel()

	// Construct a string where 10 bytes from the end is exactly
	// one complete line. "lastline\n" is 9 bytes; "longlastline\n"
	// is 13 bytes. Use the 13-byte one and set byte cap to 13.
	s := "abc\n" + "longlastline\n" // 17 bytes total
	out, _ := Truncate(s, tool.TruncateConfig{MaxBytes: 13, MaxLines: 1}, tool.StyleTail)

	// Byte cap keeps 13 from end = "nglastline\n" — not aligned
	// with a newline. With MaxLines=1, we should drop down to the
	// last complete line.
	if !strings.HasSuffix(out, "longlastline\n") {
		t.Errorf("out = %q, want suffix %q", out, "longlastline\n")
	}
}

func TestTruncate_UTF8BoundarySafety_Head(t *testing.T) {
	t.Parallel()

	// 10 bytes of multi-byte runes (5 alphas). Cap at 5 bytes:
	// cuts in the middle of an alpha; must snap back.
	s := multiByteString(10) // 5 alphas
	out, trunc := Truncate(s, tool.TruncateConfig{MaxBytes: 5}, tool.StyleHead)

	// We must have stopped at a rune boundary. The 1st alpha
	// ends at byte 2; 2nd at byte 4; 3rd at byte 6. The snap
	// takes us to the start of the rune that begins at or before
	// byte 5 — that's the 3rd alpha at byte 4. So s[:4] = 2
	// alphas = 4 bytes.
	if len(out) != 4 {
		t.Errorf("len(out) = %d, want 4 (snapped to rune boundary)", len(out))
	}
	if trunc.ShownBytes != 4 {
		t.Errorf("ShownBytes = %d, want 4", trunc.ShownBytes)
	}
}

func TestTruncate_UTF8BoundarySafety_Tail(t *testing.T) {
	t.Parallel()

	// 10 bytes (5 alphas). Tail cap at 5 bytes: starts at
	// position 5, which is the 2nd byte of the 3rd alpha. Must
	// snap back to the start of the 3rd alpha at position 4.
	s := multiByteString(10)
	out, trunc := Truncate(s, tool.TruncateConfig{MaxBytes: 5}, tool.StyleTail)

	if len(out) != 6 {
		t.Errorf("len(out) = %d, want 6 (3 alphas)", len(out))
	}
	// The kept tail is s[4:] = 3 alphas = 6 bytes.
	if out != s[4:] {
		t.Errorf("out is not s[4:]")
	}
	if trunc.ShownBytes != 6 {
		t.Errorf("ShownBytes = %d, want 6", trunc.ShownBytes)
	}
}

func TestTruncate_DefaultStyle_IsTail(t *testing.T) {
	t.Parallel()

	s := longString(100)
	out, trunc := Truncate(s, tool.TruncateConfig{MaxBytes: 10}, tool.StyleTail)

	if out != s[len(s)-10:] {
		t.Errorf("StyleTail should keep the last 10 bytes")
	}
	if trunc.Style != "tail" {
		t.Errorf("Style = %q, want tail", trunc.Style)
	}
}

func TestTruncate_ByteCapLargerThanInput_NoTruncation(t *testing.T) {
	t.Parallel()

	s := "short"
	out, trunc := Truncate(s, tool.TruncateConfig{MaxBytes: 1000}, tool.StyleTail)

	if out != s {
		t.Errorf("out = %q, want %q", out, s)
	}
	if trunc.Truncated() {
		t.Errorf("Truncated() = true, want false")
	}
}

func TestTruncate_LineCount_EmptyString(t *testing.T) {
	t.Parallel()

	if got := countLines(""); got != 0 {
		t.Errorf("countLines(\"\") = %d, want 0", got)
	}
}

func TestTruncate_LineCount_NoNewline(t *testing.T) {
	t.Parallel()

	if got := countLines("hello"); got != 1 {
		t.Errorf("countLines(\"hello\") = %d, want 1", got)
	}
}

func TestTruncate_LineCount_TrailingNewline(t *testing.T) {
	t.Parallel()

	if got := countLines("a\n"); got != 1 {
		t.Errorf("countLines(\"a\\n\") = %d, want 1", got)
	}
}

func TestTruncate_LineCount_Multiple(t *testing.T) {
	t.Parallel()

	if got := countLines("a\nb\nc\n"); got != 3 {
		t.Errorf("countLines(\"a\\nb\\nc\\n\") = %d, want 3", got)
	}
	if got := countLines("a\nb\nc"); got != 3 {
		t.Errorf("countLines(\"a\\nb\\nc\") = %d, want 3", got)
	}
}

func TestSnapToRuneStart_AtBoundary(t *testing.T) {
	t.Parallel()

	s := "hello" // all ASCII
	if got := snapToRuneStart(s, 0); got != 0 {
		t.Errorf("snapToRuneStart at 0 = %d, want 0", got)
	}
	if got := snapToRuneStart(s, 3); got != 3 {
		t.Errorf("snapToRuneStart at 3 = %d, want 3", got)
	}
	if got := snapToRuneStart(s, 5); got != 5 {
		t.Errorf("snapToRuneStart at 5 (len) = %d, want 5", got)
	}
}

func TestSnapToRuneStart_InMultiByteSequence(t *testing.T) {
	t.Parallel()

	// "aαb" = 1 + 2 + 1 = 4 bytes.
	s := "aαb"
	// Index 1 is the start of α. Index 2 is the continuation.
	if got := snapToRuneStart(s, 1); got != 1 {
		t.Errorf("snapToRuneStart at α start = %d, want 1", got)
	}
	if got := snapToRuneStart(s, 2); got != 1 {
		t.Errorf("snapToRuneStart at α continuation = %d, want 1", got)
	}
	if got := snapToRuneStart(s, 3); got != 3 {
		t.Errorf("snapToRuneStart at b = %d, want 3", got)
	}
}
