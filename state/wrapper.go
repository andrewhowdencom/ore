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
