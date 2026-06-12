package session

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/state"
)

// artifactRegistry maps artifact Kind() strings to factory functions
// that produce the corresponding concrete type. It is used during
// unmarshaling to instantiate the correct artifact struct.
//
// Factories return a pointer because json.Unmarshal requires a
// pointer target. The pointer is dereferenced in unmarshalArtifacts
// before being stored in the returned slice, so consumers see value
// types — matching what in-memory construction (e.g. artifact.Text{})
// produces. This is the single shape observed at every other boundary
// in the framework.
var artifactRegistry = map[string]func() artifact.Artifact{
	"text":        func() artifact.Artifact { return &artifact.Text{} },
	"tool_call":   func() artifact.Artifact { return &artifact.ToolCall{} },
	"tool_result": func() artifact.Artifact { return &artifact.ToolResult{} },
	"usage":       func() artifact.Artifact { return &artifact.Usage{} },
	"image":       func() artifact.Artifact { return &artifact.Image{} },
	"reasoning":   func() artifact.Artifact { return &artifact.Reasoning{} },
}

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
func unmarshalArtifacts(data []byte) ([]artifact.Artifact, error) {
	var wrappers []artifactWrapper
	if err := json.Unmarshal(data, &wrappers); err != nil {
		return nil, fmt.Errorf("unmarshal artifact wrappers: %w", err)
	}

	artifacts := make([]artifact.Artifact, len(wrappers))
	for i, w := range wrappers {
		factory, ok := artifactRegistry[w.Kind]
		if !ok {
			return nil, fmt.Errorf("unknown artifact kind %q", w.Kind)
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
