package compaction

import (
	"context"
	"strconv"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/ledger"
)

// Transform is a loop.Transform that projects the buffer through the
// latest compaction boundary recorded in ledger.Meta.
//
// On every LLM call, the transform reads the boundary index from
// ledger.Meta under [MetaKeyBoundaryIndex]. When present, it returns a
// state that exposes only the compaction turn and the turns that follow
// it. Pre-compaction turns remain in the canonical buffer (so analytics,
// audit, and replay still see them) but are invisible to the provider —
// the summary stands in for everything older than itself. This is the
// cumulative projection semantic: each compaction absorbs everything that
// preceded it.
//
// If the input state has a prepend chain on top of the buffer (e.g. from
// x/systemprompt or x/guardrails), the transform preserves the chain on
// top of the projected view. This is required for the persona/safety
// rules to remain visible to the LLM after compaction; without it the
// prepend would be silently dropped — see
// https://github.com/andrewhowdencom/ore/issues/500.
//
// When no boundary is recorded, the transform returns the base state
// unchanged (identity).
//
// The transform is stateless and goroutine-safe; a single instance
// may be shared across many Step configurations. The cost of looking
// up the boundary is O(1); the prepend-peel is O(depth) where depth is
// the number of nested prepend wrappers (1 in the realistic pipeline);
// the projection itself is a slice copy of size O(N - idx). For typical
// conversation histories all three are negligible.
type Transform struct{}

// NewTransform returns a configured Transform. No options are
// required today; the constructor exists to keep parity with the
// other transform packages (x/systemprompt, x/guardrails) and to
// leave room for future configuration (e.g. soft vs hard compaction,
// custom selector).
func NewTransform() loop.Transform { return &Transform{} }

// Transform implements loop.Transform. See the type-level doc for
// the projection semantics.
func (t *Transform) Transform(_ context.Context, st ledger.State) (ledger.State, error) {
	if st == nil {
		return nil, nil
	}

	idx, ok := readBoundaryIndex(st)
	if !ok {
		// No compaction in the buffer; the base state is already
		// the full projection.
		return st, nil
	}

	// Peel any prepend chain so the boundary index (which is in the
	// base's turn space) lines up with the slice we project. The
	// accumulated virtual turns are re-prepended in outermost-first
	// order on top of the projected view.
	var (
		base    ledger.State = st
		virtual []ledger.Turn
	)
	for {
		pc, ok := base.(interface {
			BaseState() ledger.State
			VirtualTurns() []ledger.Turn
		})
		if !ok {
			break
		}
		// Outer prepend wraps the result; in the LLM-facing view,
		// outer's virtual turns come first.
		virtual = append(virtual, pc.VirtualTurns()...)
		base = pc.BaseState()
	}

	baseTurns := base.Turns()
	if idx < 0 || idx >= len(baseTurns) {
		// The boundary was set against a different buffer shape
		// (e.g. an out-of-range index after a partial reset).
		// Treat as no boundary; the caller can re-MarkBoundary
		// against the new ledger.
		return st, nil
	}

	projected := append([]ledger.Turn(nil), baseTurns[idx:]...)
	return ledger.Prepend(ledger.NewView(base, projected), virtual), nil
}

// readBoundaryIndex pulls the boundary index from the state's metadata
// channel. The "ok" return distinguishes "no boundary set" (false,
// empty string) from "boundary set to a non-integer" (false, malformed
// value); both fall through to the identity path in Transform.
func readBoundaryIndex(st ledger.State) (int, bool) {
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
