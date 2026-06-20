// Package http implements an HTTP handler library for the ore framework,
// exposing loop.Step conversation primitives over HTTP with NDJSON streaming
// and SSE ambient channels.
package http

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/state"
	"go.opentelemetry.io/otel/propagation"
)

// artifactJSON is the JSON representation of any artifact type.
type artifactJSON struct {
	Kind             string `json:"kind"`
	Content          string `json:"content,omitempty"`
	ID               string `json:"id,omitempty"`
	Name             string `json:"name,omitempty"`
	Arguments        string `json:"arguments,omitempty"`
	Display          string `json:"display,omitempty"`
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
	Timestamp string         `json:"timestamp,omitempty"`
}

// eventContextJSON is the JSON representation of an event context.Context.
type eventContextJSON struct {
	Provenance  string `json:"provenance,omitempty"`
	Traceparent string `json:"traceparent,omitempty"`
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

// artifactEventJSON is the JSON representation of an ArtifactEvent.
// It embeds artifactJSON so the artifact fields appear at the top level
// alongside the context envelope. The Context field is omitted when empty.
type artifactEventJSON struct {
	artifactJSON
	Context *eventContextJSON `json:"context,omitempty"`
}

// propertiesEventJSON is the JSON representation of a PropertiesEvent.
type propertiesEventJSON struct {
	Kind       string            `json:"kind"`
	Properties map[string]string `json:"properties"`
	Context    *eventContextJSON `json:"context,omitempty"`
}

// lifecycleEventJSON is the JSON representation of a LifecycleEvent.
type lifecycleEventJSON struct {
	Kind    string            `json:"kind"`
	Phase   string            `json:"phase"`
	Context *eventContextJSON `json:"context,omitempty"`
}

// feedbackEventJSON is the JSON representation of a FeedbackEvent.
// Deprecated: the feedback channel was replaced by NoticeEvent in
// issue #485. New clients should emit and consume NoticeEvent instead.
// Kept temporarily for backward compatibility; will be removed once
// issue #486 (Feedback / FeedbackEvent removal) lands.
type feedbackEventJSON struct {
	Kind    string            `json:"kind"`
	Content string            `json:"content"`
	Context *eventContextJSON `json:"context,omitempty"`
}

// noticeEventJSON is the JSON representation of a NoticeEvent.
// Notice is the framework's unified ephemeral UI channel: every slash
// success, slash handler error (auto-converted), and other non-inference
// feedback reaches conduits through this kind. The Severity field lets
// conduits pick a rendering style; downstream consumers should ignore
// unknown severities and default to info styling.
type noticeEventJSON struct {
	Kind     string            `json:"kind"`
	Content  string            `json:"content"`
	Severity string            `json:"severity"`
	Context  *eventContextJSON `json:"context,omitempty"`
}

// eventContextFromJSON converts a JSON DTO pointer to a context.Context.
// Returns nil when the pointer is nil. If the DTO carries a traceparent,
// it is injected into the returned context via W3C TraceContext propagation.
func eventContextFromJSON(ctx *eventContextJSON) context.Context {
	if ctx == nil {
		return nil
	}
	result := loop.WithProvenance(context.Background(), ctx.Provenance)
	if ctx.Traceparent != "" {
		carrier := propagation.MapCarrier{}
		carrier.Set("traceparent", ctx.Traceparent)
		propagator := propagation.TraceContext{}
		result = propagator.Extract(result, carrier)
	}
	return result
}

// --- Marshal functions ---

// MarshalOutputEvent serializes a loop.OutputEvent to JSON bytes.
// It dispatches to the event's json.Marshaler implementation when
// available. Returns an error for unsupported event kinds.
func MarshalOutputEvent(event loop.OutputEvent) ([]byte, error) {
	if m, ok := event.(json.Marshaler); ok {
		return m.MarshalJSON()
	}
	return nil, fmt.Errorf("unsupported event kind: %s", event.Kind())
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
// It handles "turn_complete", "error", "properties", "lifecycle",
// and all loop.ArtifactEvent types carrying artifact kinds.
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
		return loop.TurnCompleteEvent{Turn: turn, Ctx: eventContextFromJSON(dto.Context)}, nil
	case "error":
		var dto errorEventJSON
		if err := json.Unmarshal(data, &dto); err != nil {
			return nil, err
		}
		return loop.ErrorEvent{Err: fmt.Errorf("%s", dto.Message), Ctx: eventContextFromJSON(dto.Context)}, nil
	case "properties":
		var dto propertiesEventJSON
		if err := json.Unmarshal(data, &dto); err != nil {
			return nil, err
		}
		return loop.PropertiesEvent{Properties: dto.Properties, Ctx: eventContextFromJSON(dto.Context)}, nil
	case "lifecycle":
		var dto lifecycleEventJSON
		if err := json.Unmarshal(data, &dto); err != nil {
			return nil, err
		}
		return loop.LifecycleEvent{Phase: dto.Phase, Ctx: eventContextFromJSON(dto.Context)}, nil
	case "feedback":
		var dto feedbackEventJSON
		if err := json.Unmarshal(data, &dto); err != nil {
			return nil, err
		}
		return loop.FeedbackEvent{Content: dto.Content, Ctx: eventContextFromJSON(dto.Context)}, nil
	case "notice":
		var dto noticeEventJSON
		if err := json.Unmarshal(data, &dto); err != nil {
			return nil, err
		}
		return loop.NoticeEvent{Notice: loop.Notice{
			Content:  dto.Content,
			Severity: severityFromJSON(dto.Severity),
		}, Ctx: eventContextFromJSON(dto.Context)}, nil
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
	turn := state.Turn{
		Role:      state.Role(dto.Role),
		Artifacts: artifacts,
	}
	if dto.Timestamp != "" {
		ts, err := time.Parse(time.RFC3339Nano, dto.Timestamp)
		if err != nil {
			return state.Turn{}, fmt.Errorf("parse turn timestamp: %w", err)
		}
		turn.Timestamp = ts
	}
	return turn, nil
}

// severityFromJSON converts the wire-format severity string into a
// loop.Severity. The wire format is the canonical human label
// ("Success", "Info", "Warn", "Error") because the marshaller emits
// the same form via Severity.String(). Unknown or empty values fall
// back to SeverityInfo so unrecognised severities — including a
// client speaking a slightly older protocol — still render as a
// neutral informational message rather than being dropped.
func severityFromJSON(s string) loop.Severity {
	switch s {
	case "Success":
		return loop.SeveritySuccess
	case "Info":
		return loop.SeverityInfo
	case "Warn":
		return loop.SeverityWarn
	case "Error":
		return loop.SeverityError
	default:
		return loop.SeverityInfo
	}
}
