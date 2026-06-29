package analytics

import (
	"sort"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/junk"
	"github.com/andrewhowdencom/ore/ledger"
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
// artifacts whose ToolCallID has no matching ToolCall within the
// resolution scope (the slice passed to AnalyzeTurns, or the thread
// in AnalyzeStore). It is a private constant because the label is a
// presentation choice internal to the analytics package; callers
// that want to detect orphans can compare against this value.
const orphanToolSource = "(unknown)"

// toolNamesInTurns builds a ToolCallID→Name map for the given slice
// of turns, covering every tool_call artifact in the slice. The map
// is consulted by sourceFor to resolve tool_result Sources.
//
// The scope is the entire input slice — not a single turn. The
// framework emits tool_call in a RoleAssistant turn and tool_result
// in a separate RoleTool turn, so a per-turn map would always be
// empty when resolving a tool_result. The fix is to build one map
// per scope (the slice the caller passes to AnalyzeTurns, or the
// thread in AnalyzeStore).
func toolNamesInTurns(turns []ledger.Turn) map[string]string {
	m := make(map[string]string)
	for _, turn := range turns {
		for _, art := range turn.Artifacts {
			if tc, ok := art.(artifact.ToolCall); ok {
				m[tc.ID] = tc.Name
			}
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
//
// tool_result artifacts are resolved against tool_call artifacts in
// the same slice via a whole-slice ToolCallID→Name map. A result
// whose ToolCallID has no matching call anywhere in the slice
// buckets under orphanToolSource ("(unknown)").
func AnalyzeTurns(turns []ledger.Turn) []Stats {
	stats := make(map[statsKey]*Stats)
	nameByID := toolNamesInTurns(turns)

	for _, turn := range turns {
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
func AnalyzeThread(t *junk.Thread) []Stats {
	if t == nil || t.State == nil {
		return nil
	}
	return AnalyzeTurns(t.State.Turns())
}

// AnalyzeStore aggregates statistics across all threads in the store.
// It returns a merged, sorted slice of Stats.
//
// tool_result artifacts are resolved against tool_call artifacts
// within the same thread. Cross-thread resolution is out of scope:
// a result in thread A whose ToolCallID matches a call in thread B
// is treated as an orphan and buckets under orphanToolSource.
func AnalyzeStore(store junk.Store) ([]Stats, error) {
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
		thTurns := th.State.Turns()
		nameByID := toolNamesInTurns(thTurns)
		for _, turn := range thTurns {
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
// junk.Store).
//
// The function previously lived here and in x/telemetry/telemetry.go;
// both copies have been replaced with a call to llmbytes.Of.
