package conversation

import (
	"encoding/json"
	"fmt"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/state"
)

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
}

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
		artifacts[i] = a
	}
	return artifacts, nil
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
		}
	}
	return turns, nil
}
