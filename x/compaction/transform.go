package compaction

import (
	"context"
	"strconv"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/state"
)

// Transform is a loop.Transform that projects the buffer through the
// latest compaction boundary recorded in state.Meta.
//
// On every LLM call, the transform reads the boundary index from
// state.Meta under [MetaKeyBoundaryIndex]. When present, it returns a
// state.View that exposes only the compaction turn and the turns that
// follow it. Pre-compaction turns remain in the canonical buffer (so
// analytics, audit, and replay still see them) but are invisible to
// the provider — the summary stands in for everything older than
// itself. This is the cumulative projection semantic: each compaction
// absorbs everything that preceded it.
//
// When no boundary is recorded, the transform returns the base state
// unchanged (identity).
//
// The transform is stateless and goroutine-safe; a single instance
// may be shared across many Step configurations. The cost of looking
// up the boundary is O(1); the projection itself is a slice copy of
// size O(N - idx). For typical conversation histories both are
// negligible.
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
	idx, ok := readBoundaryIndex(st)
	if !ok {
		// No compaction in the buffer; the base state is already
		// the full projection.
		return st, nil
	}
	if idx < 0 || idx >= len(turns) {
		// The boundary was set against a different buffer shape
		// (e.g. an out-of-range index after a partial reset).
		// Treat as no boundary; the caller can re-MarkBoundary
		// against the new state.
		return st, nil
	}

	projected := make([]state.Turn, len(turns)-idx)
	copy(projected, turns[idx:])
	return state.NewView(st, projected), nil
}

// readBoundaryIndex pulls the boundary index from the state's metadata
// channel. The "ok" return distinguishes "no boundary set" (false,
// empty string) from "boundary set to a non-integer" (false, malformed
// value); both fall through to the identity path in Transform.
func readBoundaryIndex(st state.State) (int, bool) {
	raw, ok := st.Meta().Get(MetaKeyBoundaryIndex)
	if !ok {
		return 0, false
	}
	idx, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return idx, true
}
