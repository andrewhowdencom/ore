package state

import "github.com/andrewhowdencom/tack/artifact"

// Memory is a simple in-memory implementation of State.
// It is not safe for concurrent use.
type Memory struct {
	turns []Turn
}

// Turns returns a defensive copy of the internal turn slice.
// Note: this is a shallow copy of the slice itself; the Artifacts slices
// within each Turn are not deep-copied.
func (m *Memory) Turns() []Turn {
	result := make([]Turn, len(m.turns))
	copy(result, m.turns)
	return result
}

// Append adds a new turn to the in-memory state.
func (m *Memory) Append(role Role, artifacts ...artifact.Artifact) {
	m.turns = append(m.turns, Turn{
		Role:      role,
		Artifacts: artifacts,
	})
}
