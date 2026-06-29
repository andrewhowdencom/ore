// Package tui benchmark harness for the render-tick hot path.
//
// # Benchmark comparison
//
// Captured on Linux amd64, Intel Core i7-10700 @ 2.90GHz, Go 1.26.2,
// benchtime=2s, b.ReportAllocs(). ns/op is the median.
//
// Baseline (main@ee1a83b, before fix on plan/tui-render-tick-perf):
//
// | Bench | Sub-bench | ns/op | B/op | allocs/op |
// | --- | --- | --- | --- | --- |
// | BenchmarkRenderMarkdown | bytes=200 | 190,893 | 90,252 | 1,083 |
// | BenchmarkRenderMarkdown | bytes=2,000 | 856,370 | 263,123 | 8,117 |
// | BenchmarkRenderMarkdown | bytes=20,000 | 6,896,529 | 1,782,232 | 70,922 |
// | BenchmarkBuildContent | turns=10 | 591,436 | 276,812 | 782 |
// | BenchmarkBuildContent | turns=50 | 2,789,252 | 1,538,965 | 3,870 |
// | BenchmarkBuildContent | turns=100 | 4,904,556 | 3,240,224 | 7,724 |
// | BenchmarkBuildContent | turns=200 | 9,263,582 | 6,635,076 | 15,428 |
// | BenchmarkRenderTick | current_blocks=1 | 1,929,633 | 569,745 | 15,846 |
// | BenchmarkRenderTick | current_blocks=5 | 9,975,072 | 2,848,512 | 79,225 |
// | BenchmarkRenderTick | current_blocks=20 | 30,607,960 | 11,391,806 | 316,893 |
// | BenchmarkRenderTick | current_blocks=50 | 72,159,271 | 28,478,902 | 792,231 |
// | BenchmarkStreamingTick_Combined | short_session | 1,703,349 | 573,819 | 15,846 |
// | BenchmarkStreamingTick_Combined | long_session | 28,226,115 | 11,475,930 | 316,986 |
//
// After fix (plan/tui-render-tick-perf, commit 0ad6a92):
//
// | Bench | Sub-bench | ns/op | B/op | allocs/op | vs. baseline |
// | --- | --- | --- | --- | --- | --- |
// | BenchmarkRenderMarkdown | bytes=200 | 209,960 | 90,243 | 1,083 | ~unchanged |
// | BenchmarkRenderMarkdown | bytes=2,000 | 813,737 | 263,113 | 8,117 | ~unchanged |
// | BenchmarkRenderMarkdown | bytes=20,000 | 6,769,733 | 1,783,310 | 70,923 | ~unchanged |
// | BenchmarkBuildContent | turns=10 | 626,348 | 277,052 | 782 | ~unchanged |
// | BenchmarkBuildContent | turns=50 | 2,463,916 | 1,539,589 | 3,870 | ~unchanged |
// | BenchmarkBuildContent | turns=100 | 4,849,417 | 3,239,522 | 7,724 | ~unchanged |
// | BenchmarkBuildContent | turns=200 | 8,590,876 | 6,630,393 | 15,428 | ~unchanged |
// | BenchmarkRenderTick | current_blocks=1 | 12,608 | 224 | 2 | 153x faster |
// | BenchmarkRenderTick | current_blocks=5 | 47,167 | 1,024 | 2 | 211x faster |
// | BenchmarkRenderTick | current_blocks=20 | 181,179 | 2,767 | 2 | 169x faster |
// | BenchmarkRenderTick | current_blocks=50 | 434,713 | 5,670 | 2 | 166x faster |
// | BenchmarkStreamingTick_Combined | short_session | 251,647 | 4,203 | 2 | 6.8x faster |
// | BenchmarkStreamingTick_Combined | long_session | 2,592,262 | 46,480 | 12 | 10.9x faster |
//
// Key observations:
//   - Glamour allocates heavily per call: 1,083 allocs for a 200-byte string,
//     8,117 for 2,000 bytes. This is a known glamour characteristic; the
//     per-block glamour re-render is the dominant cost in renderTick.
//   - BenchmarkRenderTick scales linearly with current_blocks at
//     ~1.4 ms/block, consistent with each block paying a fresh glamour
//     render. With 20 blocks in currentTurn, the tick takes 30.6 ms —
//     nearly 2x the 16 ms debounce target.
//   - BenchmarkStreamingTick_Combined/long_session is 28.2 ms — 1.76x the
//     16 ms target. This is the worst case the user reported.
//   - BenchmarkBuildContent is the secondary cost. At 100 history turns
//     it is 4.9 ms, well under the target, so the buildContent leg of
//     the tick is acceptable on its own.
//
// Threshold: BenchmarkStreamingTick_Combined/long_session ≤ 16ms median
// (i.e. ≤ 16,000,000 ns/op). Result: 2.59 ms — threshold met with
// ~6x headroom.
//
// The fix (see model.go commit 0ad6a92) adds a width-tracked
// renderedWidth field to renderedBlock and centralises the
// stale-detection in rerenderIfStale. Only the streaming tick
// path was modified by the fix; BenchmarkRenderMarkdown and
// BenchmarkBuildContent are unchanged, confirming the fix targets
// exactly the wasted glamour re-renders and does not regress
// anything else.
package tui

import (
	"fmt"
	"testing"

	"charm.land/bubbles/v2/viewport"
	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/x/conduit/tui/theme"
)

// newBenchModel returns a model with the viewport sized for the bench. It
// reuses newTestModel() so the textarea widget is properly initialized.
func newBenchModel(width, height int) model {
	m := newTestModel()
	m.viewport = viewport.New(viewport.WithWidth(width), viewport.WithHeight(height))
	m.theme = theme.Dark()
	return m
}

// syntheticText returns a string of approximately n bytes of plausible
// English-like content (a repeating cycle of common words). It is
// deterministic — same n always produces the same string.
func syntheticText(n int) string {
	if n <= 0 {
		return ""
	}
	const cycle = "the quick brown fox jumps over the lazy dog while pondering the next step in a long-running task. "
	var b []byte
	for len(b) < n {
		b = append(b, cycle...)
	}
	return string(b[:n])
}

// syntheticToolResult returns a string of approximately n bytes of
// plausible tool output (a mix of ls-style lines).
func syntheticToolResult(n int) string {
	if n <= 0 {
		return ""
	}
	const line = "file_001.txt  1234  user  group  rw-r--r--  2024-01-15 10:23\n"
	var b []byte
	for len(b) < n {
		b = append(b, line...)
	}
	return string(b[:n])
}

// syntheticToolCall returns a fixed-shape tool call with realistic
// arguments. ID is unique per index so tool_call/tool_result pairing can
// be exercised if needed.
func syntheticToolCall(i int) artifact.ToolCall {
	return artifact.ToolCall{
		ID:   fmt.Sprintf("call_%04d", i),
		Name: "bash",
		Arguments: fmt.Sprintf(
			`{"command":"ls -la /tmp/run_%04d","timeout":30}`,
			i,
		),
	}
}

// syntheticReasoning returns a string of approximately n bytes of plausible
// reasoning text. Uses the same corpus as syntheticText for simplicity.
func syntheticReasoning(n int) string { return syntheticText(n) }

// makeHistoryTurns builds n assistant turns, each with blocksPerTurn blocks
// (a mix of text, reasoning, tool_call, and tool_result). Each block's
// `rendered` field is pre-populated via renderArtifact so the bench
// measures steady-state re-rendering, not first-time rendering.
//
// The model argument is passed by pointer so renderArtifact can use the
// model's viewport width and theme.
func makeHistoryTurns(m *model, n, blocksPerTurn int) []renderedTurn {
	turns := make([]renderedTurn, 0, n)
	for i := 0; i < n; i++ {
		var blocks []renderedBlock
		for j := 0; j < blocksPerTurn; j++ {
			kind := j % 4
			var art artifact.Artifact
			switch kind {
			case 0:
				art = artifact.Text{Content: syntheticText(200)}
			case 1:
				art = artifact.Reasoning{Content: syntheticReasoning(300)}
			case 2:
				art = syntheticToolCall(i*blocksPerTurn + j)
			case 3:
				art = artifact.ToolResult{
					ToolCallID: fmt.Sprintf("call_%04d", i*blocksPerTurn+j-1),
					Content:    syntheticToolResult(2000),
				}
			}
			block := m.renderArtifact(art, ledger.RoleAssistant)
			if block.kind != "" {
				blocks = append(blocks, block)
			}
		}
		turns = append(turns, renderedTurn{
			role:   ledger.RoleAssistant,
			blocks: blocks,
		})
	}
	return turns
}

// makeToolCallBlocks builds n tool_call + n tool_result blocks (alternating)
// in currentTurn.blocks. All blocks are pre-populated via renderArtifact.
func makeToolCallBlocks(m *model, n int) []renderedBlock {
	var blocks []renderedBlock
	for i := 0; i < n; i++ {
		tc := m.renderArtifact(syntheticToolCall(i), ledger.RoleAssistant)
		if tc.kind != "" {
			blocks = append(blocks, tc)
		}
		tr := m.renderArtifact(artifact.ToolResult{
			ToolCallID: fmt.Sprintf("call_%04d", i),
			Content:    syntheticToolResult(2000),
		}, ledger.RoleAssistant)
		if tr.kind != "" {
			blocks = append(blocks, tr)
		}
	}
	return blocks
}

// -- Benchmarks --

// BenchmarkRenderMarkdown measures the pure glamour cost of rendering a
// single block, parameterized by source size. It isolates Cost A's
// per-block glamour overhead from any model-state machinery.
func BenchmarkRenderMarkdown(b *testing.B) {
	for _, n := range []int{200, 2_000, 20_000} {
		b.Run(fmt.Sprintf("bytes=%d", n), func(b *testing.B) {
			m := newBenchModel(80, 40)
			src := syntheticText(n)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = m.renderMarkdown(src, m.viewport.Width())
			}
		})
	}
}

// BenchmarkBuildContent measures the pure cost of buildContent — assembling
// the cached content string from prior turns. It isolates Cost B (string
// work and gap stitching) from glamour.
func BenchmarkBuildContent(b *testing.B) {
	for _, turns := range []int{10, 50, 100, 200} {
		b.Run(fmt.Sprintf("turns=%d", turns), func(b *testing.B) {
			m := newBenchModel(80, 40)
			m.turns = makeHistoryTurns(&m, turns, 5)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m.contentDirty = true
				_ = m.buildContent()
			}
		})
	}
}

// BenchmarkRenderTick measures the end-to-end cost of one renderTickMsg
// processing pass, parameterized by currentTurn size. The bench
// pre-populates currentTurn with n tool-call blocks (the case where Cost A
// re-renders the most), so the loop body runs the worst-case scenario.
func BenchmarkRenderTick(b *testing.B) {
	for _, n := range []int{1, 5, 20, 50} {
		b.Run(fmt.Sprintf("current_blocks=%d", n), func(b *testing.B) {
			m := newBenchModel(80, 40)
			m.currentTurn.blocks = makeToolCallBlocks(&m, n)
			m.renderScheduled = true
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				newM, _ := m.Update(renderTickMsg{})
				m = *(newM.(*model))
				m.renderScheduled = true // re-arm for next iteration
			}
		})
	}
}

// BenchmarkStreamingTick_Combined is the realistic scenario: history + a
// streaming current turn. Two configurations are tracked:
//
//   - short_session: 10 history turns × 5 blocks, currentTurn 1 tool call.
//     Represents a fresh session in normal use.
//   - long_session: 100 history turns × 5 blocks, currentTurn 20 tool calls
//     + 20 tool results. Represents the worst case the user reported.
//
// The long_session sub-bench is the threshold check (≤ 16ms median).
func BenchmarkStreamingTick_Combined(b *testing.B) {
	cases := []struct {
		name    string
		history int
		toolsIn int
	}{
		{"short_session", 10, 1},
		{"long_session", 100, 20},
	}
	for _, tc := range cases {
		b.Run(tc.name, func(b *testing.B) {
			m := newBenchModel(80, 40)
			m.turns = makeHistoryTurns(&m, tc.history, 5)
			m.currentTurn.blocks = makeToolCallBlocks(&m, tc.toolsIn)
			m.renderScheduled = true
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				newM, _ := m.Update(renderTickMsg{})
				m = *(newM.(*model))
				m.renderScheduled = true
			}
		})
	}
}