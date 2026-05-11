// Package state defines the State interface and supporting types for ore's
// conversation history model.
package state

import "github.com/andrewhowdencom/ore/artifact"

// Role represents the role of a participant in a conversation turn.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// Turn represents a single turn in the conversation history.
type Turn struct {
	Role      Role
	Artifacts []artifact.Artifact
}

// State is a mutable conversation state that the core loop appends to.
type State interface {
	// Turns returns a defensive copy of the turn history.
	Turns() []Turn

	// Append adds a new turn to the state. It mutates in place.
	Append(role Role, artifacts ...artifact.Artifact)
}
