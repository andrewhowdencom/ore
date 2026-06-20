package analytics_test

import (
	"strings"
	"testing"

	"github.com/andrewhowdencom/ore/x/analytics"
)

// TestRender_Empty asserts the friendly empty-state message is
// returned when there are no Stats rows. No trailing newline, no
// table structure.
func TestRender_Empty(t *testing.T) {
	got := analytics.Render(nil)
	if want := "No artifacts in this thread yet."; got != want {
		t.Errorf("empty input: got %q, want %q", got, want)
	}

	got = analytics.Render([]analytics.Stats{})
	if want := "No artifacts in this thread yet."; got != want {
		t.Errorf("empty slice: got %q, want %q", got, want)
	}
}

// TestRender_SingleBucket asserts the table structure for one
// row: header, separator, one data row, totals row, no trailing
// newline. The % column reads "100" because the row accounts for
// all the bytes.
func TestRender_SingleBucket(t *testing.T) {
	stats := []analytics.Stats{
		{Kind: "text", Source: "", Count: 3, Bytes: 24},
	}
	got := analytics.Render(stats)

	// Every table row must start with '|'.
	for i, line := range strings.Split(got, "\n") {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "|") {
			t.Errorf("line %d (%q) does not start with '|'", i, line)
		}
	}

	// Header columns must appear in order.
	wantCols := []string{"Kind", "Source", "Count", "Bytes", "%"}
	header := strings.SplitN(got, "\n", 2)[0]
	for _, col := range wantCols {
		if !strings.Contains(header, col) {
			t.Errorf("header missing column %q: %q", col, header)
		}
	}

	// Single data row must be present with the expected values.
	if !strings.Contains(got, "text") {
		t.Errorf("data row missing kind 'text':\n%s", got)
	}
	if !strings.Contains(got, "3") {
		t.Errorf("data row missing count 3:\n%s", got)
	}
	if !strings.Contains(got, "24 B") {
		t.Errorf("data row missing bytes 24 B:\n%s", got)
	}
	if !strings.Contains(got, "100") {
		t.Errorf("data row missing percent 100:\n%s", got)
	}

	// Totals row must appear last and contain the bolded marker.
	// We don't assert it ends the string (it is followed by trailing
	// padding for the Bytes/% columns), only that it is present.
	if !strings.Contains(got, "**total**") {
		t.Errorf("totals row missing:\n%s", got)
	}
	// No trailing newline (per the docstring contract).
	if strings.HasSuffix(got, "\n") {
		t.Errorf("output has unexpected trailing newline:\n%q", got)
	}
}

// TestRender_MultipleKinds asserts that rows for different kinds
// each render in their own row, and the totals row sums their
// counts and bytes.
func TestRender_MultipleKinds(t *testing.T) {
	stats := []analytics.Stats{
		{Kind: "text", Source: "", Count: 5, Bytes: 100},
		{Kind: "reasoning", Source: "", Count: 2, Bytes: 200},
		{Kind: "image", Source: "", Count: 1, Bytes: 500},
	}
	got := analytics.Render(stats)

	// Each kind must appear in its own row.
	for _, want := range []string{"text", "reasoning", "image"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing kind %q:\n%s", want, got)
		}
	}

	// Total count = 5 + 2 + 1 = 8, total bytes = 800.
	if !strings.Contains(got, "**8**") {
		t.Errorf("totals count should be **8**:\n%s", got)
	}
	if !strings.Contains(got, "**800 B**") {
		t.Errorf("totals bytes should be **800 B**:\n%s", got)
	}

	// Percents via integer truncation (floor): 100/800=12, 200/800=25,
	// 500/800=62. We assert each percent is present in a cell context
	// to avoid matching the totals row or header by accident.
	for _, want := range []string{" 12 ", " 25 ", " 62 "} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing percent cell %q:\n%s", want, got)
		}
	}
}

// TestRender_MultipleSourcesWithinKind asserts that two (Kind,
// Source) buckets for the same Kind render as distinct rows, with
// each row showing its own count and percent.
func TestRender_MultipleSourcesWithinKind(t *testing.T) {
	stats := []analytics.Stats{
		// Order: (Kind, Source) sorted. tool_call has two sources.
		{Kind: "tool_call", Source: "bash", Count: 4, Bytes: 400},
		{Kind: "tool_call", Source: "filesystem", Count: 1, Bytes: 100},
		{Kind: "tool_result", Source: "bash", Count: 4, Bytes: 1600},
	}
	got := analytics.Render(stats)

	// All three source names must appear.
	for _, want := range []string{"bash", "filesystem"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing source %q:\n%s", want, got)
		}
	}

	// Total bytes = 400 + 100 + 1600 = 2100 = 2.05K → "2.1 K"
	// (one decimal). Total count = 4 + 1 + 4 = 9.
	if !strings.Contains(got, "**9**") {
		t.Errorf("totals count should be **9**:\n%s", got)
	}
	if !strings.Contains(got, "**2.1 K**") {
		t.Errorf("totals bytes should be **2.1 K**:\n%s", got)
	}
	// 100/2100 = 4.76% → 4 (truncation).
	if !strings.Contains(got, " 4 ") {
		t.Errorf("output missing percent cell for filesystem 4%%:\n%s", got)
	}
}

// TestRender_OrphanUnknownSource asserts that the literal "(unknown)"
// source label flows through Render as-is. The label is owned by
// x/analytics (see orphanToolSource in analytics.go); Render must
// not transform it.
func TestRender_OrphanUnknownSource(t *testing.T) {
	stats := []analytics.Stats{
		{Kind: "tool_call", Source: "bash", Count: 1, Bytes: 100},
		{Kind: "tool_result", Source: "(unknown)", Count: 2, Bytes: 200},
	}
	got := analytics.Render(stats)

	if !strings.Contains(got, "(unknown)") {
		t.Errorf("output missing orphan label:\n%s", got)
	}
	// The orphan row's bytes (200 of 300 total) → 66% (truncated from
	// 66.66...).
	if !strings.Contains(got, " 66 ") {
		t.Errorf("output missing orphan row percent (66):\n%s", got)
	}
}

// TestRender_HumanBytes_Giga asserts that byte counts ≥ 1 GiB
// humanize to the G unit with one decimal place.
func TestRender_HumanBytes_Giga(t *testing.T) {
	stats := []analytics.Stats{
		{Kind: "text", Source: "", Count: 1, Bytes: 1 << 30},        // 1.0 G
		{Kind: "tool_result", Source: "bash", Count: 1, Bytes: 1<<30 + 512*1024*1024}, // 1.5 G
	}
	got := analytics.Render(stats)

	if !strings.Contains(got, "1.0 G") {
		t.Errorf("expected 1.0 G humanization:\n%s", got)
	}
	if !strings.Contains(got, "1.5 G") {
		t.Errorf("expected 1.5 G humanization:\n%s", got)
	}
	// Total = 2.5 G.
	if !strings.Contains(got, "**2.5 G**") {
		t.Errorf("expected **2.5 G** totals:\n%s", got)
	}
}

// TestRender_HumanBytes_Scale asserts the humanization ladder:
// bytes < 1 KiB render as integer "B"; values ≥ 1 KiB but < 1 MiB
// render as one-decimal "K"; values ≥ 1 MiB but < 1 GiB render as
// one-decimal "M".
func TestRender_HumanBytes_Scale(t *testing.T) {
	stats := []analytics.Stats{
		{Kind: "a", Source: "", Count: 1, Bytes: 512},          // 512 B
		{Kind: "b", Source: "", Count: 1, Bytes: 1024},         // 1.0 K
		{Kind: "c", Source: "", Count: 1, Bytes: 1024 + 512},   // 1.5 K
		{Kind: "d", Source: "", Count: 1, Bytes: 1 << 20},      // 1.0 M
		{Kind: "e", Source: "", Count: 1, Bytes: (1 << 20) + (512 << 10)}, // 1.5 M
	}
	got := analytics.Render(stats)

	wantSubstrings := []string{
		"512 B",
		"1.0 K",
		"1.5 K",
		"1.0 M",
		"1.5 M",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
}

// TestRender_TotalBytesZero asserts that when totalBytes is zero
// (e.g. every artifact is artifact.Usage, which contributes 0
// bytes), the % column renders as "—" rather than dividing by zero.
// The totals row's % cell is blank (no share of a zero total).
func TestRender_TotalBytesZero(t *testing.T) {
	stats := []analytics.Stats{
		{Kind: "usage", Source: "", Count: 4, Bytes: 0},
	}
	got := analytics.Render(stats)

	// The data row should show "—" in the % column.
	if !strings.Contains(got, "—") {
		t.Errorf("output missing em-dash placeholder:\n%s", got)
	}
	// No division-by-zero panic; the totals row still renders.
	if !strings.Contains(got, "**total**") {
		t.Errorf("totals row missing:\n%s", got)
	}
	if !strings.Contains(got, "**4**") {
		t.Errorf("totals count missing:\n%s", got)
	}
	if !strings.Contains(got, "**0 B**") {
		t.Errorf("totals bytes missing:\n%s", got)
	}
}

// TestRender_InputOrderPreserved asserts that Render does not
// re-sort the input; the rows appear in the caller's order. This
// is the contract documented in the godoc — callers that want a
// particular sort pre-sort via Analyze*.
func TestRender_InputOrderPreserved(t *testing.T) {
	stats := []analytics.Stats{
		// Deliberately not (Kind, Source) lexicographic.
		{Kind: "z", Source: "", Count: 1, Bytes: 100},
		{Kind: "a", Source: "", Count: 1, Bytes: 100},
		{Kind: "m", Source: "", Count: 1, Bytes: 100},
	}
	got := analytics.Render(stats)

	lines := strings.Split(got, "\n")
	// lines[0] = header, lines[1] = separator, lines[2..4] = data rows
	// (no trailing newline so the last line is the totals row).
	wantOrder := []string{"| z ", "| a ", "| m "}
	for i, want := range wantOrder {
		if !strings.HasPrefix(lines[2+i], want) {
			t.Errorf("data row %d: want prefix %q, got %q", i, want, lines[2+i])
		}
	}
}

// TestRender_StructuralShape asserts the row count: header +
// separator + N data rows + 1 totals row. This is a coarse but
// useful check against accidental row drops or duplicates.
func TestRender_StructuralShape(t *testing.T) {
	stats := []analytics.Stats{
		{Kind: "text", Source: "", Count: 1, Bytes: 10},
		{Kind: "text", Source: "", Count: 1, Bytes: 10},
		{Kind: "tool_call", Source: "bash", Count: 1, Bytes: 50},
	}
	got := analytics.Render(stats)

	// 3 data rows yields 6 non-empty lines: 1 header + 1 separator +
// 3 data + 1 totals. Render does not emit a trailing newline, so
// strings.Split yields exactly 6 segments.
	lines := strings.Split(got, "\n")
	wantLines := 6
	if len(lines) != wantLines {
		t.Fatalf("want %d lines (header, separator, 3 data, totals), got %d:\n%s", wantLines, len(lines), got)
	}
	if lines[wantLines-1] == "" {
		t.Errorf("last line should be the totals row, not empty (no trailing newline expected):\n%s", got)
	}
}