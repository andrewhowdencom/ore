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
	Index            int    `json:"index,omitempty"`
	URL              string `json:"url,omitempty"`
}

// turnJSON is the JSON representation of a state.Turn.
type turnJSON struct {
	Role      string         `json:"role"`
	Artifacts []artifactJSON `json:"artifacts"`
}

// eventContextJSON is the JSON representation of an EventContext.
type eventContextJSON struct {
	Provenance string `json:"provenance,omitempty"`
}

// turnCompleteEventJSON is the JSON representation of a TurnCompleteEvent.
type turnCompleteEventJSON struct {
	Kind    string            `json:"kind"`
	Turn    turnJSON          `json:"turn"`
	Context *eventContextJSON `json:"context,omitempty"`
}

// errorEventJSON is the JSON representation of an ErrorEvent.
type errorEventJSON struct {
	Kind    string            `json:"kind"`
	Message string            `json:"message"`
	Context *eventContextJSON `json:"context,omitempty"`
}

// processCompleteEventJSON is the JSON representation of a ProcessCompleteEvent.
type processCompleteEventJSON struct {
	Kind    string            `json:"kind"`
	Error   string            `json:"error,omitempty"`
	Context *eventContextJSON `json:"context,omitempty"`
}

// artifactEventJSON is the JSON representation of an ArtifactEvent.
// It embeds artifactJSON so the artifact fields appear at the top level
// alongside the context envelope. The Context field is omitted when empty.
type artifactEventJSON struct {
	artifactJSON
	Context *eventContextJSON `json:"context,omitempty"`
}

// statusEventJSON is the JSON representation of a StatusEvent.
type statusEventJSON struct {
	Kind    string            `json:"kind"`
	Status  map[string]string `json:"status"`
	Context *eventContextJSON `json:"context,omitempty"`
}

// eventContextToJSON converts a loop.EventContext to a JSON DTO pointer.
// Returns nil when the context is empty so omitempty removes it from JSON.
func eventContextToJSON(ctx loop.EventContext) *eventContextJSON {
	if ctx.Provenance == "" {
		return nil
	}
	return &eventContextJSON{Provenance: ctx.Provenance}
}

// eventContextFromJSON converts a JSON DTO pointer to a loop.EventContext.
// Returns an empty EventContext when the pointer is nil.
func eventContextFromJSON(ctx *eventContextJSON) loop.EventContext {
	if ctx == nil {
		return loop.EventContext{}
	}
	return loop.EventContext{Provenance: ctx.Provenance}
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
		return &artifactJSON{Kind: "tool_call_delta", ID: a.ID, Name: a.Name, Arguments: a.Arguments, Index: a.Index}, true
	case artifact.ToolResult:
		return &artifactJSON{Kind: "tool_result", ToolCallID: a.ToolCallID, Content: a.MarkdownString(), IsError: a.IsError}, true
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
// It handles TurnCompleteEvent, ErrorEvent, ProcessCompleteEvent,
// and all loop.ArtifactEvent wrapper types that contain an artifact.Artifact.
// Unknown artifact kinds are silently skipped (returns nil, nil).
// Returns an error only for unsupported event kinds.
func MarshalOutputEvent(event loop.OutputEvent) ([]byte, error) {
	switch e := event.(type) {
	case loop.TurnCompleteEvent:
		turn, err := turnToJSON(e.Turn)
		if err != nil {
			return nil, err
		}
		return json.Marshal(turnCompleteEventJSON{
			Kind:    "turn_complete",
			Turn:    turn,
			Context: eventContextToJSON(e.Ctx),
		})
	case loop.ErrorEvent:
		return json.Marshal(errorEventJSON{
			Kind:    "error",
			Message: e.Err.Error(),
			Context: eventContextToJSON(e.Ctx),
		})
	case loop.ProcessCompleteEvent:
		dto := processCompleteEventJSON{
			Kind:    "process_complete",
			Context: eventContextToJSON(e.Ctx),
		}
		if e.Err != nil {
			dto.Error = e.Err.Error()
		}
		return json.Marshal(dto)
	case loop.ArtifactEvent:
		dto, ok := artifactToJSON(e.Artifact)
		if !ok {
			return nil, nil
		}
		return json.Marshal(artifactEventJSON{
			artifactJSON: *dto,
			Context:      eventContextToJSON(e.Ctx),
		})
	case loop.StatusEvent:
		return json.Marshal(statusEventJSON{
			Kind:    "status",
			Status:  e.Status,
			Context: eventContextToJSON(e.Ctx),
		})
	default:
		if m, ok := event.(json.Marshaler); ok {
			return m.MarshalJSON()
		}
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
		return artifact.ToolCallDelta{ID: dto.ID, Name: dto.Name, Arguments: dto.Arguments, Index: dto.Index}, nil
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
// It handles "turn_complete", "error", "process_complete", and all
// loop.ArtifactEvent types carrying artifact kinds. Returns an error for
// unsupported kinds or malformed JSON.
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
		return loop.TurnCompleteEvent{Turn: turn, Ctx: eventContextFromJSON(dto.Context)}, nil
	case "error":
		var dto errorEventJSON
		if err := json.Unmarshal(data, &dto); err != nil {
			return nil, err
		}
		return loop.ErrorEvent{Err: fmt.Errorf("%s", dto.Message), Ctx: eventContextFromJSON(dto.Context)}, nil
	case "process_complete":
		var dto processCompleteEventJSON
		if err := json.Unmarshal(data, &dto); err != nil {
			return nil, err
		}
		var err error
		if dto.Error != "" {
			err = fmt.Errorf("%s", dto.Error)
		}
		return loop.ProcessCompleteEvent{Err: err, Ctx: eventContextFromJSON(dto.Context)}, nil
	case "status":
		var dto statusEventJSON
		if err := json.Unmarshal(data, &dto); err != nil {
			return nil, err
		}
		return loop.StatusEvent{Status: dto.Status, Ctx: eventContextFromJSON(dto.Context)}, nil
	default:
		// Treat as artifact.
		var dto artifactEventJSON
		if err := json.Unmarshal(data, &dto); err != nil {
			return nil, err
		}
		art, err := artifactFromJSON(dto.artifactJSON)
		if err != nil {
			return nil, err
		}
		return loop.ArtifactEvent{Artifact: art, Ctx: eventContextFromJSON(dto.Context)}, nil
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
