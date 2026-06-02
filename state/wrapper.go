package state

import "github.com/andrewhowdencom/ore/artifact"

// VirtualTurnState wraps a base State, prepending virtual turns to the
// slice returned by Turns(). Append() delegates to the base state. This
// enables inference assembly transforms to inject content that appears
// in the provider's view without mutating the persistent conversation
// buffer.
type VirtualTurnState struct {
	base    State
	virtual []Turn
}

// NewVirtualTurnState creates a state wrapper that prepends virtual turns
// before the base state's turns. If virtual is empty, it returns the base
// state directly as an identity optimization.
func NewVirtualTurnState(base State, virtual []Turn) State {
	if len(virtual) == 0 {
		return base
	}
	return &VirtualTurnState{base: base, virtual: virtual}
}

// View is a general State wrapper that returns a caller-provided slice of
// turns from Turns() while delegating Append() to a base State. Unlike
// VirtualTurnState, it imposes no ordering constraint — the caller decides
// the full projected slice. This enables arbitrary state projections such
// as compaction, filtering, and reordering.
type View struct {
	base  State
	turns []Turn
}

// NewView creates a state wrapper that returns the given turns from Turns()
// and delegates Append() to the base state. If turns is empty, it returns
// the base state directly as an identity optimization.
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

// Append delegates to the underlying base state.
func (v *View) Append(role Role, artifacts ...artifact.Artifact) {
	v.base.Append(role, artifacts...)
}

// Prepend returns a View that projects virtual turns before the base state's
// turns. It is a convenience wrapper around NewView for the common case of
// prepending system prompts, guardrails, or other contextual turns.
func Prepend(base State, virtual []Turn) State {
	baseTurns := base.Turns()
	turns := make([]Turn, 0, len(virtual)+len(baseTurns))
	turns = append(turns, virtual...)
	turns = append(turns, baseTurns...)
	return NewView(base, turns)
}

// Turns returns a defensive copy of the virtual turns followed by the
// base state's turns.
func (v *VirtualTurnState) Turns() []Turn {
	baseTurns := v.base.Turns()
	result := make([]Turn, 0, len(v.virtual)+len(baseTurns))
	result = append(result, v.virtual...)
	result = append(result, baseTurns...)
	return result
}

// Append delegates to the underlying base state.
func (v *VirtualTurnState) Append(role Role, artifacts ...artifact.Artifact) {
	v.base.Append(role, artifacts...)
}
