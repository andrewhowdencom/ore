// Package state defines the State interface and supporting types for ore's
// conversation history model.
package ledger

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
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

// TraversalControl directs how a turn participates in the active-path
// resolution walk. It is part of [Turn.Metadata] and is interpreted by
// [Thread.ResolveActivePath].
type TraversalControl string

const (
	// ControlContinue (the zero value) includes the turn in the active
	// path and continues tracing backward through the parent's ancestry.
	ControlContinue TraversalControl = "continue"

	// ControlStop includes the turn in the active path but immediately
	// terminates backward traversal. Use it for compaction summary turns
	// that absorb everything that came before them.
	ControlStop TraversalControl = "stop"

	// ControlSkip excludes the turn from the active path while continuing
	// to trace its ancestry. Use it to hide heavy tool results or
	// to prune intermediate logs without breaking chain connectivity.
	ControlSkip TraversalControl = "skip"
)

// Metadata carries per-turn control state. Future turn-level facts
// (provenance, attribution, retention) extend this struct additively.
type Metadata struct {
	// Control is a traversal directive interpreted by
	// [Thread.ResolveActivePath]. The empty string is treated as
	// [ControlContinue].
	Control TraversalControl `json:"control,omitempty"`
}

// Turn represents a single coordinate in the conversation tree.
//
// A turn carries both tree mechanics (ID, ParentID) and consumer-facing
// content (Role, Artifacts, Timestamp). The persistent tree unit IS the
// consumer-facing projection; there is no separate "Node" type.
//
// All turns except the root carry exactly one parent ID. The parent's
// ID is empty for root turns.
type Turn struct {
	// ID is the unique identifier of this turn. Generated on Append
	// using a cryptographically random source. Stable across the
	// lifetime of the turn.
	ID string `json:"id"`

	// ParentID is the ID of the turn immediately preceding this one
	// on the chain. Empty for root turns.
	ParentID string `json:"parent_id,omitempty"`

	// Role is the speaker for this turn.
	Role Role `json:"role"`

	// Artifacts is the polymorphic list of content blocks produced
	// by the speaker. Wire adapters serialize per provider schema.
	Artifacts []artifact.Artifact `json:"-"` // overridden by custom UnmarshalJSON/MarshalJSON

	// Timestamp is when the turn was appended. Set by the [Thread]
	// from a configured clock; serializable but omitted
	// from JSON when zero.
	Timestamp time.Time `json:"timestamp,omitempty"`

	// Metadata carries per-turn control state (see [TraversalControl]).
	Metadata Metadata `json:"metadata,omitempty"`
}

// MarshalJSON implements json.Marshaler, omitting the zero timestamp and
// empty metadata. ID is always emitted because every persisted turn
// must be addressable. Artifacts are wrapped with their Kind tag for
// round-trip via the artifact registry.
func (t Turn) MarshalJSON() ([]byte, error) {
	type alias Turn
	artifactsJSON, err := marshalArtifacts(t.Artifacts)
	if err != nil {
		return nil, fmt.Errorf("marshal artifacts: %w", err)
	}
	if t.Timestamp.IsZero() && t.Metadata.Control == "" {
		return json.Marshal(struct {
			ID        string          `json:"id"`
			ParentID  string          `json:"parent_id,omitempty"`
			Role      Role            `json:"role"`
			Artifacts json.RawMessage `json:"artifacts"`
		}{
			ID:        t.ID,
			ParentID:  t.ParentID,
			Role:      t.Role,
			Artifacts: artifactsJSON,
		})
	}
	if t.Timestamp.IsZero() {
		return json.Marshal(struct {
			ID        string          `json:"id"`
			ParentID  string          `json:"parent_id,omitempty"`
			Role      Role            `json:"role"`
			Artifacts json.RawMessage `json:"artifacts"`
			Metadata  Metadata        `json:"metadata,omitempty"`
		}{
			ID:        t.ID,
			ParentID:  t.ParentID,
			Role:      t.Role,
			Artifacts: artifactsJSON,
			Metadata:  t.Metadata,
		})
	}
	if t.Metadata.Control == "" {
		return json.Marshal(struct {
			ID        string          `json:"id"`
			ParentID  string          `json:"parent_id,omitempty"`
			Role      Role            `json:"role"`
			Artifacts json.RawMessage `json:"artifacts"`
			Timestamp time.Time       `json:"timestamp,omitempty"`
		}{
			ID:        t.ID,
			ParentID:  t.ParentID,
			Role:      t.Role,
			Artifacts: artifactsJSON,
			Timestamp: t.Timestamp,
		})
	}
	out, err := json.Marshal(alias(t))
	if err != nil {
		return nil, err
	}
	return replaceArtifactsInJSON(out, artifactsJSON)
}

// UnmarshalJSON implements json.Unmarshaler. Artifacts are unwrapped
// from their kind-tagged envelope via the artifact registry; the
// rest of the fields follow the struct tags.
func (t *Turn) UnmarshalJSON(data []byte) error {
	type alias Turn
	aux := struct {
		Artifacts json.RawMessage `json:"artifacts"`
		*alias
	}{
		alias: (*alias)(t),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if len(aux.Artifacts) > 0 {
		artifacts, err := unmarshalArtifacts(aux.Artifacts)
		if err != nil {
			return fmt.Errorf("unmarshal artifacts: %w", err)
		}
		t.Artifacts = artifacts
	}
	return nil
}

// replaceArtifactsInJSON swaps the placeholder "artifacts":null
// produced by the alias path with the wrapped envelope. The alias
// emits "artifacts":null (because []artifact.Artifact is hidden by
// `json:"-"` on the struct definition); we want the envelope on disk.
func replaceArtifactsInJSON(in, artifactsJSON []byte) ([]byte, error) {
	needle := []byte(`"artifacts":null`)
	idx := bytes.Index(in, needle)
	if idx < 0 {
		return in, nil
	}
	out := make([]byte, 0, len(in)-len(needle)+len(artifactsJSON))
	out = append(out, in[:idx]...)
	out = append(out, []byte(`"artifacts":`)...)
	out = append(out, artifactsJSON...)
	out = append(out, in[idx+len(needle):]...)
	return out, nil
}

// GenerateTurnID returns a 16-character hex string drawn from a
// cryptographically random source. Sufficient for uniqueness within
// a single thread without depending on an external UUID library.
//
// Exported so packages that build ephemeral [Thread] instances from
// raw [Turn] inputs (e.g. compaction) can assign IDs to turns that
// arrive without one, preserving the order-preserving semantics of
// loading a turn slice into a state implementation.
func GenerateTurnID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		// rand.Read on Linux/macOS does not fail in practice; fall
		// back to a time-based ID if it does.
		return "t-" + time.Now().UTC().Format("20060102T150405.000000000")
	}
	return hex.EncodeToString(b[:])
}

// State is a mutable conversation state that the core loop appends to.
//
// [Thread] is the canonical implementation; a tree-backed type whose
// active path is resolved by [Thread.ResolveActivePath].
type State interface {
	// Turns returns the active path through the conversation history
	// (in chronological order). For [Thread] this is the result of
	// [Thread.ResolveActivePath].
	Turns() []Turn

	// Append adds a new turn to the ledger. It mutates in place.
	Append(role Role, artifacts ...artifact.Artifact)

	// Meta returns the metadata context for this ledger. The handle
	// is live — writes propagate to the underlying state and are
	// visible to subsequent reads. See the [Meta] contract for
	// the serialization format and concurrency expectations.
	Meta() Meta
}

// Meta is the metadata context carried by a [State]. It mirrors the
// shape of [context.Context] but is keyed on string identifiers and
// is mutable: a conversation's metadata grows over time as turns are
// appended and processors add facts about the conversation as a whole
// (e.g. future checkpoint markers).
//
// Meta values are produced by [State.Meta]; they are not constructed
// directly. Each call to State.Meta returns a handle backed by the
// same underlying storage, so writes made through one handle are
// visible through any other handle for the same State.
//
// Concurrency: like [State] itself (and its in-memory implementation
// [Thread]), Meta is not safe for concurrent use. The framework's
// serial pipeline — the loop worker goroutine, the Transform chain,
// the session's worker — is the only contract; concurrent access from
// outside that pipeline is a future middleware concern.
type Meta interface {
	// Get returns the value stored for key and a boolean indicating
	// whether the key was present.
	Get(key string) (string, bool)

	// Set stores value under key, replacing any prior value.
	Set(key, value string)

	// All returns a defensive copy of the metadata map. Mutating
	// the returned map does not affect the underlying ledger.
	All() map[string]string
}