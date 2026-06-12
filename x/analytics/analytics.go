package analytics

import (
	"sort"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/x/llmbytes"
)

// Stats holds per-(Kind, Source) statistics: the artifact kind, the
// source identifier (the tool name for tool_call and tool_result
// artifacts; empty for all other kinds), the count of artifacts in
// the bucket, and their aggregate byte size as seen by the LLM
// provider.
type Stats struct {
	Kind   string
	Source string
	Count  int
	Bytes  int64
}

// statsKey identifies a Stats bucket. Two artifacts with the same
// (Kind, Source) pair aggregate into the same bucket. It is
// unexported because callers should not need to construct one — the
// public Stats type carries the same fields and is the unit of
// identity at the API boundary.
type statsKey struct {
	kind   string
	source string
}

// orphanToolSource is the Source identifier used for tool_result
// artifacts whose ToolCallID has no matching ToolCall in the same
// turn. It is a private constant because the label is a presentation
// choice internal to the analytics package; callers that want to
// detect orphans can compare against this value.
const orphanToolSource = "(unknown)"

// toolNamesInTurn builds a ToolCallID→Name map for the given turn's
// tool_call artifacts. The map is consulted by sourceFor to resolve
// tool_result Sources. The map is intentionally scoped to a single
// turn: cross-turn resolution is out of scope.
func toolNamesInTurn(turn state.Turn) map[string]string {
	m := make(map[string]string)
	for _, art := range turn.Artifacts {
		if tc, ok := art.(artifact.ToolCall); ok {
			m[tc.ID] = tc.Name
		}
	}
	return m
}

// sourceFor returns the Source identifier for an artifact. For
// tool_call artifacts, the Source is the artifact's Name. For
// tool_result artifacts, the Source is the Name of the originating
// tool_call looked up by ToolCallID in nameByID; if no such
// tool_call exists, the Source is orphanToolSource. For all other
// kinds, the Source is empty.
func sourceFor(art artifact.Artifact, nameByID map[string]string) string {
	switch a := art.(type) {
	case artifact.ToolCall:
		return a.Name
	case artifact.ToolResult:
		if name, ok := nameByID[a.ToolCallID]; ok {
			return name
		}
		return orphanToolSource
	default:
		return ""
	}
}

// AnalyzeTurns aggregates per-(Kind, Source) statistics over a slice
// of turns. It returns a slice of Stats sorted lexicographically by
// (Kind, Source).
func AnalyzeTurns(turns []state.Turn) []Stats {
	stats := make(map[statsKey]*Stats)

	for _, turn := range turns {
		nameByID := toolNamesInTurn(turn)
		for _, art := range turn.Artifacts {
			key := statsKey{kind: art.Kind(), source: sourceFor(art, nameByID)}
			s, ok := stats[key]
			if !ok {
				s = &Stats{Kind: key.kind, Source: key.source}
				stats[key] = s
			}
			s.Count++
			s.Bytes += llmbytes.Of(art)
		}
	}

	out := make([]Stats, 0, len(stats))
	for _, s := range stats {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Source < out[j].Source
	})
	return out
}

// AnalyzeThread is a convenience wrapper that aggregates statistics
// for all turns in the given thread.
func AnalyzeThread(t *session.Thread) []Stats {
	if t == nil || t.State == nil {
		return nil
	}
	return AnalyzeTurns(t.State.Turns())
}

// AnalyzeStore aggregates statistics across all threads in the store.
// It returns a merged, sorted slice of Stats.
func AnalyzeStore(store session.Store) ([]Stats, error) {
	threads, err := store.List()
	if err != nil {
		return nil, err
	}

	// Merge results from all threads into a single map.
	merged := make(map[statsKey]*Stats)
	for _, th := range threads {
		if th == nil || th.State == nil {
			continue
		}
		for _, turn := range th.State.Turns() {
			nameByID := toolNamesInTurn(turn)
			for _, art := range turn.Artifacts {
				key := statsKey{kind: art.Kind(), source: sourceFor(art, nameByID)}
				s, ok := merged[key]
				if !ok {
					s = &Stats{Kind: key.kind, Source: key.source}
					merged[key] = s
				}
				s.Count++
				s.Bytes += llmbytes.Of(art)
			}
		}
	}

	out := make([]Stats, 0, len(merged))
	for _, s := range merged {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Source < out[j].Source
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
