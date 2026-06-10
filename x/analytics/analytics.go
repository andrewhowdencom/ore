package analytics

import (
	"encoding/json"
	"sort"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/state"
)

// KindStats holds per-artifact-kind statistics: the kind name,
// the count of artifacts of that kind, and their aggregate byte size
// as seen by the LLM provider.
type KindStats struct {
	Kind  string
	Count int
	Bytes int64
}

// AnalyzeTurns aggregates per-kind statistics over a slice of turns.
// It returns a sorted (by Kind) slice of KindStats.
func AnalyzeTurns(turns []state.Turn) []KindStats {
	stats := make(map[string]*KindStats)

	for _, turn := range turns {
		for _, art := range turn.Artifacts {
			k := art.Kind()
			s, ok := stats[k]
			if !ok {
				s = &KindStats{Kind: k}
				stats[k] = s
			}
			s.Count++
			s.Bytes += countBytes(art)
		}
	}

	out := make([]KindStats, 0, len(stats))
	for _, s := range stats {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Kind < out[j].Kind
	})
	return out
}

// AnalyzeThread is a convenience wrapper that aggregates statistics
// for all turns in the given thread.
func AnalyzeThread(t *session.Thread) []KindStats {
	if t == nil || t.State == nil {
		return nil
	}
	return AnalyzeTurns(t.State.Turns())
}

// AnalyzeStore aggregates statistics across all threads in the store.
// It returns a merged, sorted slice of KindStats.
func AnalyzeStore(store session.Store) ([]KindStats, error) {
	threads, err := store.List()
	if err != nil {
		return nil, err
	}

	// Merge results from all threads into a single map.
	merged := make(map[string]*KindStats)
	for _, th := range threads {
		if th == nil || th.State == nil {
			continue
		}
		for _, turn := range th.State.Turns() {
			for _, art := range turn.Artifacts {
				k := art.Kind()
				s, ok := merged[k]
				if !ok {
					s = &KindStats{Kind: k}
					merged[k] = s
				}
				s.Count++
				s.Bytes += countBytes(art)
			}
		}
	}

	out := make([]KindStats, 0, len(merged))
	for _, s := range merged {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Kind < out[j].Kind
	})
	return out, nil
}

// countBytes returns the LLM-facing payload byte size for an artifact.
// Logic is duplicated from x/telemetry/telemetry.go — keep in sync.
//
//nolint:staticcheck // QF1012 — cross-reference comment, not a Go reference.
//
//	When updating this function, also update x/telemetry/telemetry.go:countBytes
//	to keep the two implementations consistent.
func countBytes(art artifact.Artifact) int64 {
	switch a := art.(type) {
	case artifact.Text:
		return int64(len(a.Content))
	case artifact.Reasoning:
		return int64(len(a.Content))
	case artifact.ToolCall:
		return int64(len(a.LLMString()))
	case artifact.ToolResult:
		return int64(len(a.LLMString()))
	case artifact.Image:
		return int64(len(a.URL))
	case artifact.Usage:
		return 0
	default:
		if b, err := json.Marshal(art); err == nil {
			return int64(len(b))
		}
		return 0
	}
}
