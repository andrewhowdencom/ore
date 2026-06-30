package compaction

import (
	"context"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/ledger"
)

// Transform is a loop.Transform that passes the conversation state
// through unchanged. Compaction is now expressed as a [ControlStop]
// directive on the summary turn; the active-path walk
// ([ledger.Thread.ResolveActivePath]) terminates at the summary,
// hiding everything that came before it from the LLM-facing view.
//
// When no compaction has been recorded, the walk traverses the
// full active path. When a summary has been recorded, the walk
// stops at the summary; the LLM sees the summary plus every turn
// that follows it.
//
// The transform is the identity because the tree structure already
// encodes the projection semantics; the Transform's job is to
// trigger the walk each turn, not to compute the projection.
//
// The transform is stateless and goroutine-safe; a single instance
// may be shared across many Step configurations.
type Transform struct{}

// NewTransform returns a configured Transform. No options are
// required today; the constructor exists to keep parity with the
// other transform packages (x/systemprompt, x/guardrails) and to
// leave room for future configuration (e.g. soft vs hard compaction,
// custom selector).
func NewTransform() loop.Transform { return &Transform{} }

// Transform implements loop.Transform. The compaction projection is
// already applied by [ledger.Thread.ResolveActivePath] when the
// summary turn carries [ledger.ControlStop]; this transform is
// the identity.
//
// Returning a fresh state object would force an unnecessary copy
// in callers that read the state (e.g. wire adapters). The
// identity return preserves the existing allocation.
func (t *Transform) Transform(_ context.Context, st ledger.State) (ledger.State, error) {
	if st == nil {
		return nil, nil
	}
	return st, nil
}