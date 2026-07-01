package ledger

import (
	"encoding/json"
	"fmt"

	"github.com/andrewhowdencom/ore/artifact"
)

// artifactWrapper is the JSON envelope for a single artifact.
//
// Artifacts are polymorphic: a single `[]artifact.Artifact` slice may
// contain any concrete type that implements the artifact.Artifact
// interface. The JSON wire format distinguishes them by a "kind" tag
// plus an opaque "data" object. The data is the artifact's own JSON
// representation (with its own MarshalJSON / UnmarshalJSON if any),
// not a generic struct.
type artifactWrapper struct {
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data"`
}

// isArtifactDelta reports whether the artifact implements artifact.Delta.
// Delta artifacts are ephemeral streaming fragments and must never be
// persisted to state.
func isArtifactDelta(a artifact.Artifact) bool {
	_, ok := a.(artifact.Delta)
	return ok
}

// marshalArtifacts serializes a slice of artifacts to JSON. Each
// artifact is wrapped with its Kind tag so the receiver can dispatch
// to the right concrete type on unmarshal via the registry.
//
// Delta artifacts are rejected with an error: persisting a delta
// would mean replaying ephemeral streaming fragments, which is
// nonsensical and dangerous.
func marshalArtifacts(artifacts []artifact.Artifact) (json.RawMessage, error) {
	wrappers := make([]artifactWrapper, len(artifacts))
	for i, a := range artifacts {
		if isArtifactDelta(a) {
			return nil, fmt.Errorf("delta artifact %q cannot be persisted", a.Kind())
		}
		data, err := json.Marshal(a)
		if err != nil {
			return nil, fmt.Errorf("marshal artifact %q: %w", a.Kind(), err)
		}
		wrappers[i] = artifactWrapper{
			Kind: a.Kind(),
			Data: data,
		}
	}
	return json.Marshal(wrappers)
}

// unmarshalArtifacts deserializes a JSON array into artifacts.
//
// Each factory is obtained from artifact.Registered() — the registry
// is populated by per-type init() blocks in the artifact package.
// Adding a new persistable artifact type in that package registers
// itself automatically; the drift-detection test in the artifact
// package catches missing registrations.
//
// The returned slice holds the dereferenced value (matching the
// in-memory shape), so type assertions like `.(artifact.Text)` work
// on round-tripped data — the silent failure mode that affected
// every value-form type assertion in production code is fixed here.
func unmarshalArtifacts(data []byte) ([]artifact.Artifact, error) {
	var wrappers []artifactWrapper
	if err := json.Unmarshal(data, &wrappers); err != nil {
		return nil, fmt.Errorf("unmarshal artifact wrappers: %w", err)
	}
	registry := artifact.Registered()
	artifacts := make([]artifact.Artifact, len(wrappers))
	for i, w := range wrappers {
		factory, ok := registry[w.Kind]
		if !ok {
			return nil, fmt.Errorf("unmarshal artifact %d: unknown artifact kind %q", i, w.Kind)
		}
		a := factory()
		if err := json.Unmarshal(w.Data, a); err != nil {
			return nil, fmt.Errorf("unmarshal artifact %q: %w", w.Kind, err)
		}
		artifacts[i] = dereferenceArtifact(a)
	}
	return artifacts, nil
}

// dereferenceArtifact unwraps a pointer to an artifact concrete type
// to its value. The set of cases covers every type in
// artifact.AllPersistent().
func dereferenceArtifact(a artifact.Artifact) artifact.Artifact {
	switch v := a.(type) {
	case *artifact.Text:
		return *v
	case *artifact.Reasoning:
		return *v
	case *artifact.ToolCall:
		return *v
	case *artifact.ToolResult:
		return *v
	case *artifact.Image:
		return *v
	case *artifact.Usage:
		return *v
	case *artifact.StopReason:
		return *v
	case *artifact.ReasoningSignature:
		return *v
	default:
		return a
	}
}