package ledger

import "github.com/andrewhowdencom/ore/artifact"

// View is a general State wrapper that returns a caller-provided slice of
// turns from Turns() while delegating Append() to a base State. This
// enables arbitrary state projections (compaction, filtering, reordering)
// without mutating the persistent conversation buffer.
type View struct {
	base  State
	turns []Turn
}

// NewView creates a state wrapper that returns the given turns from Turns()
// and delegates Append() to the base ledger. If turns is empty or nil, it
// returns the base state directly as an identity optimization.
func NewView(base State, turns []Turn) State {
	if len(turns) == 0 {
		return base
	}
	return &View{base: base, turns: turns}
}

// Turns returns a defensive copy of the projected turns.
func (v *View) Turns() []Turn {
	result := make([]Turn, 0, len(v.turns))
	result = append(result, v.turns...)
	return result
}

// Append delegates to the underlying base ledger.
func (v *View) Append(role Role, artifacts ...artifact.Artifact) {
	v.base.Append(role, artifacts...)
}

// Meta delegates to the base ledger. Views do not own metadata; they
// are projections of turns. Writes through a View's Meta are visible
// on the base ledger.
func (v *View) Meta() Meta {
	return v.base.Meta()
}

// Prepend returns a State view that projects virtual turns before the base
// state's current turns on every call to Turns(). Append() delegates to the
// base ledger. If virtual is empty, it returns the base state directly as an
// identity optimization.
func Prepend(base State, virtual []Turn) State {
	if len(virtual) == 0 {
		return base
	}
	return &prependView{base: base, virtual: virtual}
}

type prependView struct {
	base    State
	virtual []Turn
}

func (p *prependView) Turns() []Turn {
	baseTurns := p.base.Turns()
	result := make([]Turn, 0, len(p.virtual)+len(baseTurns))
	result = append(result, p.virtual...)
	result = append(result, baseTurns...)
	return result
}

func (p *prependView) Append(role Role, artifacts ...artifact.Artifact) {
	p.base.Append(role, artifacts...)
}

// Meta delegates to the base ledger. As with View, writes through a
// prependView's Meta are visible on the base ledger.
func (p *prependView) Meta() Meta {
	return p.base.Meta()
}

// BaseState returns the underlying base state this prependView wraps.
// Exposed so consumers that need to project the base while preserving
// the prepend chain (e.g. compaction.Transform) can reach the base
// without re-implementing prepend-aware indexing. The method is on an
// unexported type, so the capability is opt-in via type assertion.
func (p *prependView) BaseState() State { return p.base }

// VirtualTurns returns the virtual turns prepended in front of the
// base state's turns. See [prependView.BaseState] for the rationale.
func (p *prependView) VirtualTurns() []Turn { return p.virtual }
