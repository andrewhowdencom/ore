package artifact

import "sync"

// Persistent marks an Artifact whose instances may be serialized to a
// thread store. The marker is sealed to this package by an unexported
// method, mirroring the design pattern that the package's Artifact
// interface uses for its public Kind() method: external packages can
// implement Artifact (the public surface), but only types declared
// here can implement Persistent (the persistence side).
//
// Every concrete type in this package that wants to be round-tripped
// through the session store must implement Persistent and call
// Register in its init() block. Delta kinds (TextDelta, ReasoningDelta,
// ToolCallDelta) deliberately do not — they are ephemeral streaming
// fragments and must never be persisted.
//
// The accompanying drift-detection test verifies that the set of
// registered kinds matches the set of Persistent types in this
// package; a new persistable type cannot be added without also
// updating that test, so the registry cannot drift silently.
type Persistent interface {
	Artifact
	isPersistent()
}

// registry holds the kind → factory map populated by per-type
// init() blocks. The map is read-only after package init completes;
// Register uses a mutex so that registering from init (or, defensively,
// from tests in other packages) is safe.
var (
	registryMu sync.RWMutex
	registry   = map[string]func() Artifact{}
)

// Register associates a kind identifier with a factory that produces
// a fresh pointer to a concrete Artifact type (e.g. `&Text{}`).
//
// The factory must return a pointer because the unmarshaler in
// ore/junk calls json.Unmarshal on its result, which requires a
// non-nil pointer target. Callers that want the round-tripped slice
// to contain value types (matching the in-memory shape) should
// return pointers here; the unmarshaler dereferences after Unmarshal
// completes.
//
// Register is intended to be called from init() blocks in this
// package; calling it after init is permitted but unusual and will
// be caught by the drift-detection test if the registered kind is
// not in the persistent type set.
func Register(kind string, factory func() Artifact) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[kind] = factory
}

// Registered returns a copy of the registry mapping kind identifiers
// to factories. Returning a copy prevents callers from mutating the
// package-level map.
func Registered() map[string]func() Artifact {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make(map[string]func() Artifact, len(registry))
	for k, v := range registry {
		out[k] = v
	}
	return out
}

// AllPersistent returns the canonical list of zero-value instances of
// every persistable concrete type in this package. It is the manifest
// the drift-detection test compares against Registered().
//
// Adding a new persistable artifact type requires four edits:
//
//  1. Declare the type and implement Artifact (Kind() method).
//  2. Implement Persistent (the unexported isPersistent() method,
//     sealed to this package).
//  3. Add an init() block above that calls Register.
//  4. Add a zero-value instance of the new type to this list.
//
// The drift test fails when this list and Registered() disagree, so
// the four edits cannot drift silently.
func AllPersistent() []Artifact {
	out := make([]Artifact, len(allPersistent))
	copy(out, allPersistent)
	return out
}

// allPersistent is the source of truth for the set of persistable
// artifact kinds. Each entry must have a corresponding Register call
// in an init() block above; the drift test in drift_test.go enforces
// this invariant.
var allPersistent = []Artifact{
	Text{},
	ToolCall{},
	ToolResult{},
	Usage{},
	Image{},
	Reasoning{},
	StopReason{},
	ReasoningSignature{},
}
