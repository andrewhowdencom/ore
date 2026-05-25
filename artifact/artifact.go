// Package artifact defines the extensible Artifact interface and common concrete
// types used throughout ore. The Artifact interface exposes a public Kind()
// method to allow custom artifact types to be defined in other packages.
package artifact

import "strconv"

// Artifact is the base interface for all LLM response artifacts.
type Artifact interface {
	Kind() string
}

// Delta marks artifacts that are ephemeral streaming fragments.
// These must never be persisted to state; only complete artifacts should be.
type Delta interface {
	Artifact
	IsDelta()
}

// Accumulable marks delta artifacts that can be merged into complete artifacts
// by a generic accumulator. Implementations must return a stable
// AccumulatorKey and define MergeInto to combine the delta with an existing
// accumulated artifact or seed a new one when acc is nil.
type Accumulable interface {
	Delta
	AccumulatorKey() string
	MergeInto(acc Artifact) Artifact
}

// Text represents a text content artifact.
type Text struct {
	Content string
}

// Kind returns the artifact kind identifier.
func (t Text) Kind() string { return "text" }

// ToolCall represents a tool invocation artifact.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
}

// Kind returns the artifact kind identifier.
func (t ToolCall) Kind() string { return "tool_call" }

// ToolResult represents the result of executing a tool call.
type ToolResult struct {
	ToolCallID string
	Content    string
	IsError    bool
}

// Kind returns the artifact kind identifier.
func (t ToolResult) Kind() string { return "tool_result" }

// Usage represents token consumption metadata from a provider response.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// Kind returns the artifact kind identifier.
func (u Usage) Kind() string { return "usage" }

// Image represents an image artifact referenced by URL.
type Image struct {
	URL string
}

// Kind returns the artifact kind identifier.
func (i Image) Kind() string { return "image" }

// Reasoning represents a reasoning or thinking content artifact.
type Reasoning struct {
	Content string
}

// Kind returns the artifact kind identifier.
func (r Reasoning) Kind() string { return "reasoning" }

// TextDelta represents a partial chunk of text content for streaming.
type TextDelta struct {
	Content string
}

// Kind returns the artifact kind identifier.
func (t TextDelta) Kind() string { return "text_delta" }

// IsDelta marks TextDelta as an ephemeral streaming fragment.
func (t TextDelta) IsDelta() {}

// ReasoningDelta represents a partial chunk of reasoning content for streaming.
type ReasoningDelta struct {
	Content string
}

// Kind returns the artifact kind identifier.
func (r ReasoningDelta) Kind() string { return "reasoning_delta" }

// IsDelta marks ReasoningDelta as an ephemeral streaming fragment.
func (r ReasoningDelta) IsDelta() {}

// ToolCallDelta represents a partial chunk of a tool invocation for streaming.
type ToolCallDelta struct {
	Index     int
	ID        string
	Name      string
	Arguments string
}

// Kind returns the artifact kind identifier.
func (t ToolCallDelta) Kind() string { return "tool_call_delta" }

// IsDelta marks ToolCallDelta as an ephemeral streaming fragment.
func (t ToolCallDelta) IsDelta() {}

// AccumulatorKey returns a stable routing key for the generic accumulator.
// TextDelta accumulates into a single "text" block.
func (d TextDelta) AccumulatorKey() string { return "text" }

// MergeInto merges the delta into an existing Text artifact or seeds a new one.
func (d TextDelta) MergeInto(acc Artifact) Artifact {
	if acc == nil {
		return Text(d)
	}
	text := acc.(Text)
	text.Content += d.Content
	return text
}

// AccumulatorKey returns a stable routing key for the generic accumulator.
// ReasoningDelta accumulates into a single "reasoning" block.
func (d ReasoningDelta) AccumulatorKey() string { return "reasoning" }

// MergeInto merges the delta into an existing Reasoning artifact or seeds a new one.
func (d ReasoningDelta) MergeInto(acc Artifact) Artifact {
	if acc == nil {
		return Reasoning(d)
	}
	reasoning := acc.(Reasoning)
	reasoning.Content += d.Content
	return reasoning
}

// AccumulatorKey returns a stable routing key for the generic accumulator.
// ToolCallDelta accumulates per-index: "tool_call:0", "tool_call:1", etc.
func (d ToolCallDelta) AccumulatorKey() string {
	return "tool_call:" + strconv.Itoa(d.Index)
}

// MergeInto merges the delta into an existing ToolCall artifact or seeds a new one.
// ID uses latest-wins semantics; Name and Arguments are concatenated.
func (d ToolCallDelta) MergeInto(acc Artifact) Artifact {
	if acc == nil {
		return ToolCall{
			ID:        d.ID,
			Name:      d.Name,
			Arguments: d.Arguments,
		}
	}
	tc := acc.(ToolCall)
	if d.ID != "" {
		tc.ID = d.ID
	}
	tc.Name += d.Name
	tc.Arguments += d.Arguments
	return tc
}
