package state

import "github.com/andrewhowdencom/ore/artifact"

// Buffer is a simple in-memory implementation of State.
// It is not safe for concurrent use.
type Buffer struct {
	turns []Turn
}

// Turns returns a defensive copy of the internal turn slice.
// Note: this is a shallow copy of the slice itself; the Artifacts slices
// within each Turn are not deep-copied.
func (m *Buffer) Turns() []Turn {
	result := make([]Turn, len(m.turns))
	copy(result, m.turns)
	return result
}

// Append adds a new turn to the in-memory state.
func (m *Buffer) Append(role Role, artifacts ...artifact.Artifact) {
	m.turns = append(m.turns, Turn{
		Role:      role,
		Artifacts: artifacts,
	})
}
