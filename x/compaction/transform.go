package compaction

import (
	"context"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/state"
)

// Transform is a loop.Transform that projects the buffer through the
// most recent artifact.Compaction marker.
//
// On every LLM call, the transform scans the buffer from the end for
// the latest turn carrying an artifact.Compaction. When found, it
// returns a state.View that exposes only the compaction turn and the
// turns that follow it. Pre-compaction turns remain in the canonical
// buffer (so analytics, audit, and replay still see them) but are
// invisible to the provider — the summary stands in for everything
// older than itself. This is the cumulative projection semantic
// agreed in the design phase: each compaction absorbs everything that
// preceded it.
//
// When no Compaction artifact is present, the transform returns the
// base state unchanged (identity).
//
// The transform is stateless and goroutine-safe; a single instance
// may be shared across many Step configurations. The scan is O(N) in
// the number of turns; for typical conversation histories this is
// negligible. If profiling later identifies the scan as a hot spot,
// the optimization is a cached "latest compaction index" on the
// thread — out of scope here.
type Transform struct{}

// NewTransform returns a configured Transform. No options are
// required today; the constructor exists to keep parity with the
// other transform packages (x/systemprompt, x/guardrails) and to
// leave room for future configuration (e.g. soft vs hard compaction,
// custom selector).
func NewTransform() loop.Transform { return &Transform{} }

// Transform implements loop.Transform. See the type-level doc for
// the projection semantics.
func (t *Transform) Transform(_ context.Context, st state.State) (state.State, error) {
	if st == nil {
		return nil, nil
	}

	turns := st.Turns()
	idx := latestCompactionIndex(turns)
	if idx < 0 {
		// No compaction in the buffer; the base state is already
		// the full projection.
		return st, nil
	}

	projected := make([]state.Turn, len(turns)-idx)
	copy(projected, turns[idx:])
	return state.NewView(st, projected), nil
}

// latestCompactionIndex returns the index of the most recent turn
// carrying an artifact.Compaction, or -1 if none is present. The
// scan walks backward so the latest marker wins — earlier compactions
// are absorbed by the projection.
func latestCompactionIndex(turns []state.Turn) int {
	for i := len(turns) - 1; i >= 0; i-- {
		for _, art := range turns[i].Artifacts {
			if _, ok := art.(artifact.Compaction); ok {
				return i
			}
		}
	}
	return -1
}
