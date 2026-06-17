package analytics

import (
	"fmt"
	"strings"
)

// Render formats a slice of Stats as a Markdown table.
//
// The table has columns Kind, Source, Count, Bytes, %. A totals row
// appears last, with the Kind, Count, and Bytes cells wrapped in
// **bold** markers. The % cell on the totals row is blank.
//
// Bytes are humanized to B/K/M/G with one decimal place for any
// unit ≥ 1 K (e.g. "8.2 K", "1.5 M"); integer byte counts are
// rendered as "{n} B". Counts are rendered as plain integers.
//
// The % column is the integer percent of each row's bytes against
// the total byte budget (rounded to the nearest integer). When
// totalBytes is zero (e.g. every artifact is artifact.Usage, which
// contributes zero bytes), the % column is replaced with "—" for
// every row.
//
// Input order is preserved. Callers that want a particular ordering
// must sort the input themselves; the canonical sort is
// (Kind, Source) lexicographic, returned by Analyze*.
//
// The empty case — len(stats) == 0 — returns the single line
// "No artifacts in this thread yet." with no trailing newline.
//
// The column widths are derived from the data rows; the bolded
// totals row can therefore overflow visually (the ** markers push
// the cell past the column width). This is intentional and mirrors
// the Markdown convention that totals rows may exceed the column
// header's width when rendered in monospace.
func Render(stats []Stats) string {
	if len(stats) == 0 {
		return "No artifacts in this thread yet."
	}

	// Pre-compute totals and per-row formatted cells.
	var totalBytes int64
	var totalCount int
	for _, s := range stats {
		totalBytes += s.Bytes
		totalCount += s.Count
	}

	type row struct {
		kind, source, count, bytes, pct string
	}
	rows := make([]row, len(stats))
	for i, s := range stats {
		rows[i] = row{
			kind:   s.Kind,
			source: s.Source,
			count:  fmt.Sprintf("%d", s.Count),
			bytes:  humanBytes(s.Bytes),
			pct:    pctOf(s.Bytes, totalBytes),
		}
	}
	totals := row{
		kind:  "**total**",
		count: fmt.Sprintf("**%d**", totalCount),
		bytes: "**" + humanBytes(totalBytes) + "**",
		pct:   "", // totals row has no share-of-total
	}

	// Column widths are derived from the data rows only. The bolded
	// totals row can overflow — see the doc comment above.
	kindW := len("Kind")
	sourceW := len("Source")
	countW := len("Count")
	bytesW := len("Bytes")
	pctW := len("%")
	for _, r := range rows {
		if w := len(r.kind); w > kindW {
			kindW = w
		}
		if w := len(r.source); w > sourceW {
			sourceW = w
		}
		if w := len(r.count); w > countW {
			countW = w
		}
		if w := len(r.bytes); w > bytesW {
			bytesW = w
		}
		if w := len(r.pct); w > pctW {
			pctW = w
		}
	}

	var b strings.Builder

	// Header row. Text columns are left-aligned, numeric columns are
	// right-aligned.
	writeRow(&b,
		[]cell{
			{"Kind", kindW, false},
			{"Source", sourceW, false},
			{"Count", countW, true},
			{"Bytes", bytesW, true},
			{"%", pctW, true},
		},
		false, // not the separator
	)

	// Separator row.
	b.WriteByte('|')
	b.WriteString(strings.Repeat("-", kindW+2))
	b.WriteByte('|')
	b.WriteString(strings.Repeat("-", sourceW+2))
	b.WriteByte('|')
	b.WriteString(strings.Repeat("-", countW+2))
	b.WriteByte('|')
	b.WriteString(strings.Repeat("-", bytesW+2))
	b.WriteByte('|')
	b.WriteString(strings.Repeat("-", pctW+2))
	b.WriteString("|\n")

	// Data rows. Each row ends with a trailing newline so the totals
	// row is the final line.
	for _, r := range rows {
		writeRow(&b,
			[]cell{
				{r.kind, kindW, false},
				{r.source, sourceW, false},
				{r.count, countW, true},
				{r.bytes, bytesW, true},
				{r.pct, pctW, true},
			},
			false,
		)
	}

	// Totals row. No trailing newline — matches the "no trailing
	// newline" contract of the empty case and avoids double-spacing
	// in the chat output.
	writeRow(&b,
		[]cell{
			{totals.kind, kindW, false},
			{totals.source, sourceW, false},
			{totals.count, countW, true},
			{totals.bytes, bytesW, true},
			{totals.pct, pctW, true},
		},
		true, // last row: no trailing newline
	)

	return b.String()
}

// cell is one column's value plus its target width and alignment.
type cell struct {
	s          string
	w          int
	rightAlign bool
}

// writeRow writes a Markdown table row to b. The format is:
//
//	| {cell0} | {cell1} | ... | {cellN} |
//
// Each cell is padded to its target width: text columns are
// left-aligned, numeric columns are right-aligned. When last is true
// the row has no trailing newline (the final row of the table).
func writeRow(b *strings.Builder, cells []cell, last bool) {
	for i, c := range cells {
		if i == 0 {
			b.WriteString("| ")
		} else {
			b.WriteString(" | ")
		}
		writeCell(b, c.s, c.w, c.rightAlign)
	}
	if last {
		b.WriteString(" |")
	} else {
		b.WriteString(" |\n")
	}
}

// writeCell writes s to b, padded to width w with spaces. When
// rightAlign is true, padding goes to the left (numeric columns);
// otherwise padding goes to the right (text columns). If s is
// longer than w, it is emitted verbatim and overflows the column —
// the caller's responsibility is to set w ≥ the widest expected
// content. No surrounding pipes or spaces are written.
func writeCell(b *strings.Builder, s string, w int, rightAlign bool) {
	pad := w - len(s)
	if pad <= 0 {
		b.WriteString(s)
		return
	}
	if rightAlign {
		b.WriteString(strings.Repeat(" ", pad))
		b.WriteString(s)
		return
	}
	b.WriteString(s)
	b.WriteString(strings.Repeat(" ", pad))
}

// humanBytes formats a byte count as a short string. Bytes under
// 1 KiB render as plain "{n} B"; larger values render with one
// decimal place and a unit suffix (K, M, G).
func humanBytes(n int64) string {
	const (
		kb = int64(1024)
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case n < kb:
		return fmt.Sprintf("%d B", n)
	case n < mb:
		return fmt.Sprintf("%.1f K", float64(n)/float64(kb))
	case n < gb:
		return fmt.Sprintf("%.1f M", float64(n)/float64(mb))
	default:
		return fmt.Sprintf("%.1f G", float64(n)/float64(gb))
	}
}

// pctOf formats the integer percent of n out of total. When total
// is zero (every artifact has zero payload bytes), it returns the
// em-dash placeholder so the column reads "—" rather than divide by
// zero. Rounding is integer truncation: the integer percent is
// floor(n*100/total), so a row with a fractional share of exactly
// 0.5 percent rounds down rather than up. This matches the
// behavior of the reference output in the spec, where 12.5% reads
// as "12" and 62.5% reads as "62".
func pctOf(n, total int64) string {
	if total == 0 {
		return "—"
	}
	return fmt.Sprintf("%d", (n*100)/total)
}