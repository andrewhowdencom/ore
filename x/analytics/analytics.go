package analytics

import (
	"sort"

	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/x/llmbytes"
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
			s.Bytes += llmbytes.Of(art)
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
				s.Bytes += llmbytes.Of(art)
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

// countBytes was removed in favor of x/llmbytes.Of, which is the
// single canonical implementation shared with x/telemetry. Keeping
// the two packages in lockstep by hand was the proximate cause of
// issue #416 (the pointer-case regression that miscounted bytes for
// every artifact that had been JSON round-tripped through a
// session.Store).
//
// The function previously lived here and in x/telemetry/telemetry.go;
// both copies have been replaced with a call to llmbytes.Of.
