package session

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/state"
)

// artifactRegistry is removed. The package-level registry in the
// artifact package is now populated by per-type init() blocks and
// exposed via artifact.Registered(). Keeping a parallel map here
// would defeat the drift-detection test — see issue #453.

// artifactWrapper is the JSON envelope for a single artifact.
type artifactWrapper struct {
	Kind string          `json:"kind"`
	Data json.RawMessage `json:"data"`
}

// turnWrapper is the JSON envelope for a single turn.
type turnWrapper struct {
	Role      string          `json:"role"`
	Artifacts json.RawMessage `json:"artifacts"`
	Timestamp time.Time       `json:"timestamp,omitempty"`
}

// isDelta reports whether the artifact implements artifact.Delta,
// indicating it is an ephemeral streaming fragment that must not be
// persisted to state.
func isDelta(a artifact.Artifact) bool {
	_, ok := a.(artifact.Delta)
	return ok
}

// marshalArtifacts serializes a slice of artifacts to JSON.
// Delta artifacts are rejected with an error.
func marshalArtifacts(artifacts []artifact.Artifact) ([]byte, error) {
	wrappers := make([]artifactWrapper, len(artifacts))
	for i, a := range artifacts {
		if isDelta(a) {
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
// Each factory returns a pointer (required by json.Unmarshal), but the
// returned slice holds the *dereferenced value* so the round-tripped
// shape is identical to what in-memory code constructs. This means
// every type assertion in the framework (e.g. .(artifact.Text))
// succeeds on round-tripped data — the silent failure mode that
// issue #416 surfaced for the byte counter, and that affected every
// value-form type assertion in production code, is fixed here.
//
// Factories are obtained from artifact.Registered() — the registry
// is populated by per-type init() blocks in the artifact package.
// Adding a new persistable artifact type in that package registers
// itself automatically; forgetting to add it triggers the
// drift-detection test in the artifact package.
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
// to its value. For nil or unknown shapes, the input is returned
// unchanged. This is the boundary that makes "round-tripped" and
// "in-memory" artifacts observationally identical at the slice level.
//
// The set of cases below must cover every type in the artifact
// package's Persistent manifest. The drift test in
// ore/artifact/drift_test.go asserts that this list and
// artifact.AllPersistent() stay in lock-step.
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
	case *artifact.Compaction:
		return *v
	default:
		return a
	}
}

// marshalTurns serializes a slice of turns to JSON.
func marshalTurns(turns []state.Turn) ([]byte, error) {
	wrappers := make([]turnWrapper, len(turns))
	for i, turn := range turns {
		artifactsJSON, err := marshalArtifacts(turn.Artifacts)
		if err != nil {
			return nil, fmt.Errorf("marshal turn %d artifacts: %w", i, err)
		}
		wrappers[i] = turnWrapper{
			Role:      string(turn.Role),
			Artifacts: artifactsJSON,
			Timestamp: turn.Timestamp,
		}
	}
	return json.Marshal(wrappers)
}

// unmarshalTurns deserializes a JSON array into turns.
func unmarshalTurns(data []byte) ([]state.Turn, error) {
	var wrappers []turnWrapper
	if err := json.Unmarshal(data, &wrappers); err != nil {
		return nil, fmt.Errorf("unmarshal turn wrappers: %w", err)
	}

	turns := make([]state.Turn, len(wrappers))
	for i, w := range wrappers {
		artifacts, err := unmarshalArtifacts(w.Artifacts)
		if err != nil {
			return nil, fmt.Errorf("unmarshal turn %d artifacts: %w", i, err)
		}
		turns[i] = state.Turn{
			Role:      state.Role(w.Role),
			Artifacts: artifacts,
			Timestamp: w.Timestamp,
		}
	}
	return turns, nil
}
