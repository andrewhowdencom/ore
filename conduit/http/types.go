// Package http implements an HTTP handler library for the ore framework,
// exposing loop.Step conversation primitives over HTTP with NDJSON streaming
// and SSE ambient channels.
package http

import (
	"encoding/json"
	"fmt"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/state"
)

// artifactJSON is the JSON representation of any artifact type.
type artifactJSON struct {
	Kind             string `json:"kind"`
	Content          string `json:"content,omitempty"`
	ID               string `json:"id,omitempty"`
	Name             string `json:"name,omitempty"`
	Arguments        string `json:"arguments,omitempty"`
	ToolCallID       string `json:"tool_call_id,omitempty"`
	IsError          bool   `json:"is_error,omitempty"`
	PromptTokens     int    `json:"prompt_tokens,omitempty"`
	CompletionTokens int    `json:"completion_tokens,omitempty"`
	TotalTokens      int    `json:"total_tokens,omitempty"`
	URL              string `json:"url,omitempty"`
}

// turnJSON is the JSON representation of a state.Turn.
type turnJSON struct {
	Role      string         `json:"role"`
	Artifacts []artifactJSON `json:"artifacts"`
}

// turnCompleteEventJSON is the JSON representation of a TurnCompleteEvent.
type turnCompleteEventJSON struct {
	Kind string    `json:"kind"`
	Turn turnJSON `json:"turn"`
}

// errorEventJSON is the JSON representation of an ErrorEvent.
type errorEventJSON struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

// completeEventJSON is an HTTP-package-specific event that signals the end
// of a message turn, carrying all new turns since the last request.
type completeEventJSON struct {
	Kind  string     `json:"kind"`
	Turns []turnJSON `json:"turns"`
}

// --- Marshal functions ---

// MarshalArtifact serializes an artifact.Artifact to JSON bytes.
// It supports all core artifact kinds (text, text_delta, reasoning,
// reasoning_delta, tool_call, tool_call_delta, tool_result, usage, image).
// Unknown artifact kinds are silently skipped (returns nil, nil).
func MarshalArtifact(art artifact.Artifact) ([]byte, error) {
	dto, ok := artifactToJSON(art)
	if !ok {
		return nil, nil
	}
	return json.Marshal(dto)
}

// artifactToJSON converts a framework artifact to its JSON DTO.
// Returns false for unsupported kinds, signaling the caller to skip.
func artifactToJSON(art artifact.Artifact) (*artifactJSON, bool) {
	switch a := art.(type) {
	case artifact.Text:
		return &artifactJSON{Kind: "text", Content: a.Content}, true
	case artifact.TextDelta:
		return &artifactJSON{Kind: "text_delta", Content: a.Content}, true
	case artifact.Reasoning:
		return &artifactJSON{Kind: "reasoning", Content: a.Content}, true
	case artifact.ReasoningDelta:
		return &artifactJSON{Kind: "reasoning_delta", Content: a.Content}, true
	case artifact.ToolCall:
		return &artifactJSON{Kind: "tool_call", ID: a.ID, Name: a.Name, Arguments: a.Arguments}, true
	case artifact.ToolCallDelta:
		return &artifactJSON{Kind: "tool_call_delta", ID: a.ID, Name: a.Name, Arguments: a.Arguments}, true
	case artifact.ToolResult:
		return &artifactJSON{Kind: "tool_result", ToolCallID: a.ToolCallID, Content: a.Content, IsError: a.IsError}, true
	case artifact.Usage:
		return &artifactJSON{
			Kind:             "usage",
			PromptTokens:     a.PromptTokens,
			CompletionTokens: a.CompletionTokens,
			TotalTokens:      a.TotalTokens,
		}, true
	case artifact.Image:
		return &artifactJSON{Kind: "image", URL: a.URL}, true
	default:
		return nil, false
	}
}

// MarshalOutputEvent serializes a loop.OutputEvent to JSON bytes.
// It handles TurnCompleteEvent, ErrorEvent, and all artifact.Artifact types.
// Unknown artifact kinds are silently skipped (returns nil, nil).
// Returns an error only for unsupported event kinds.
func MarshalOutputEvent(event loop.OutputEvent) ([]byte, error) {
	switch e := event.(type) {
	case loop.TurnCompleteEvent:
		turn, err := turnToJSON(e.Turn)
		if err != nil {
			return nil, err
		}
		return json.Marshal(turnCompleteEventJSON{Kind: "turn_complete", Turn: turn})
	case loop.ErrorEvent:
		return json.Marshal(errorEventJSON{Kind: "error", Message: e.Err.Error()})
	case artifact.Artifact:
		dto, ok := artifactToJSON(e)
		if !ok {
			return nil, nil
		}
		return json.Marshal(dto)
	default:
		return nil, fmt.Errorf("unsupported event kind: %s", event.Kind())
	}
}

// turnToJSON converts a state.Turn to its JSON DTO.
// Artifacts with unsupported kinds are silently skipped.
func turnToJSON(t state.Turn) (turnJSON, error) {
	var artifacts []artifactJSON
	for _, art := range t.Artifacts {
		dto, ok := artifactToJSON(art)
		if !ok {
			continue // skip unknown artifact kinds
		}
		artifacts = append(artifacts, *dto)
	}
	return turnJSON{
		Role:      string(t.Role),
		Artifacts: artifacts,
	}, nil
}

// MarshalCompleteEvent serializes a complete event carrying all new turns
// produced during a message handler invocation. The returned JSON has
// kind "complete" and a "turns" array.
func MarshalCompleteEvent(turns []state.Turn) ([]byte, error) {
	turnsJSON := make([]turnJSON, len(turns))
	for i, t := range turns {
		turn, err := turnToJSON(t)
		if err != nil {
			return nil, err
		}
		turnsJSON[i] = turn
	}
	return json.Marshal(completeEventJSON{
		Kind:  "complete",
		Turns: turnsJSON,
	})
}

// --- Unmarshal functions ---

// UnmarshalArtifact deserializes JSON bytes into an artifact.Artifact.
// It supports all core artifact kinds. Returns an error for unsupported
// kinds or malformed JSON.
func UnmarshalArtifact(data []byte) (artifact.Artifact, error) {
	var dto artifactJSON
	if err := json.Unmarshal(data, &dto); err != nil {
		return nil, err
	}
	return artifactFromJSON(dto)
}

// artifactFromJSON converts an artifactJSON DTO to a framework artifact.
// Returns an error for unsupported kinds.
func artifactFromJSON(dto artifactJSON) (artifact.Artifact, error) {
	switch dto.Kind {
	case "text":
		return artifact.Text{Content: dto.Content}, nil
	case "text_delta":
		return artifact.TextDelta{Content: dto.Content}, nil
	case "reasoning":
		return artifact.Reasoning{Content: dto.Content}, nil
	case "reasoning_delta":
		return artifact.ReasoningDelta{Content: dto.Content}, nil
	case "tool_call":
		return artifact.ToolCall{ID: dto.ID, Name: dto.Name, Arguments: dto.Arguments}, nil
	case "tool_call_delta":
		return artifact.ToolCallDelta{ID: dto.ID, Name: dto.Name, Arguments: dto.Arguments}, nil
	case "tool_result":
		return artifact.ToolResult{ToolCallID: dto.ToolCallID, Content: dto.Content, IsError: dto.IsError}, nil
	case "usage":
		return artifact.Usage{PromptTokens: dto.PromptTokens, CompletionTokens: dto.CompletionTokens, TotalTokens: dto.TotalTokens}, nil
	case "image":
		return artifact.Image{URL: dto.URL}, nil
	default:
		return nil, fmt.Errorf("unsupported artifact kind: %s", dto.Kind)
	}
}

// UnmarshalOutputEvent deserializes JSON bytes into a loop.OutputEvent.
// It handles "turn_complete", "error", and all artifact kinds.
// Returns an error for unsupported kinds or malformed JSON.
func UnmarshalOutputEvent(data []byte) (loop.OutputEvent, error) {
	var peek struct {
		Kind string `json:"kind"`
	}
	if err := json.Unmarshal(data, &peek); err != nil {
		return nil, err
	}

	switch peek.Kind {
	case "turn_complete":
		var dto turnCompleteEventJSON
		if err := json.Unmarshal(data, &dto); err != nil {
			return nil, err
		}
		turn, err := turnFromJSON(dto.Turn)
		if err != nil {
			return nil, err
		}
		return loop.TurnCompleteEvent{Turn: turn}, nil
	case "error":
		var dto errorEventJSON
		if err := json.Unmarshal(data, &dto); err != nil {
			return nil, err
		}
		return loop.ErrorEvent{Err: fmt.Errorf("%s", dto.Message)}, nil
	default:
		// Treat as artifact.
		return UnmarshalArtifact(data)
	}
}

// turnFromJSON converts a turnJSON DTO to a state.Turn.
// Returns an error if any artifact in the turn has an unsupported kind.
func turnFromJSON(dto turnJSON) (state.Turn, error) {
	artifacts := make([]artifact.Artifact, len(dto.Artifacts))
	for i, artDTO := range dto.Artifacts {
		art, err := artifactFromJSON(artDTO)
		if err != nil {
			return state.Turn{}, fmt.Errorf("artifact at index %d: %w", i, err)
		}
		artifacts[i] = art
	}
	return state.Turn{
		Role:      state.Role(dto.Role),
		Artifacts: artifacts,
	}, nil
}
