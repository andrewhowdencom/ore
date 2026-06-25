// Package state defines the State interface and supporting types for ore's
// conversation history model.
package state

import (
	"encoding/json"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
)

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
	Role      Role                `json:"role"`
	Artifacts []artifact.Artifact `json:"artifacts"`
	Timestamp time.Time           `json:"timestamp,omitempty"`
}

// MarshalJSON implements json.Marshaler, omitting the zero timestamp.
func (t Turn) MarshalJSON() ([]byte, error) {
	type alias Turn
	if t.Timestamp.IsZero() {
		return json.Marshal(struct {
			Role      Role                `json:"role"`
			Artifacts []artifact.Artifact `json:"artifacts"`
		}{
			Role:      t.Role,
			Artifacts: t.Artifacts,
		})
	}
	return json.Marshal(alias(t))
}

// State is a mutable conversation state that the core loop appends to.
type State interface {
	// Turns returns a defensive copy of the turn history.
	Turns() []Turn

	// Append adds a new turn to the state. It mutates in place.
	Append(role Role, artifacts ...artifact.Artifact)

	// Meta returns the metadata context for this state. The handle
	// is live — writes propagate to the underlying state and are
	// visible to subsequent reads. See the [Meta] contract for
	// the serialization format and concurrency expectations.
	Meta() Meta
}

// Meta is the metadata context carried by a [State]. It mirrors the
// shape of [context.Context] but is keyed on string identifiers and
// is mutable: a conversation's metadata grows over time as turns are
// appended and processors add facts about the conversation as a whole
// (e.g. compaction boundaries, future checkpoint markers).
//
// Meta values are produced by [State.Meta]; they are not constructed
// directly. Each call to State.Meta returns a handle backed by the
// same underlying storage, so writes made through one handle are
// visible through any other handle for the same State.
//
// Concurrency: like [State] itself (and its in-memory implementation
// [Buffer]), Meta is not safe for concurrent use. The framework's
// serial pipeline — the loop worker goroutine, the Transform chain,
// the session's worker — is the only contract; concurrent access
// from outside that pipeline is a future middleware concern.
type Meta interface {
	// Get returns the value stored for key and a boolean indicating
	// whether the key was present.
	Get(key string) (string, bool)

	// Set stores value under key, replacing any prior value.
	Set(key, value string)

	// All returns a defensive copy of the metadata map. Mutating
	// the returned map does not affect the underlying state.
	All() map[string]string
}
