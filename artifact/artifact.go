// Package artifact defines the extensible Artifact interface and common concrete
// types used throughout ore. The Artifact interface exposes a public Kind()
// method to allow custom artifact types to be defined in other packages.
package artifact

import (
	"encoding/json"
	"strconv"
)

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

// MarshalJSON serializes Text to JSON.
func (t Text) MarshalJSON() ([]byte, error) {
	type output struct {
		Kind    string `json:"kind"`
		Content string `json:"content"`
	}
	return json.Marshal(output{Kind: "text", Content: t.Content})
}

// ToolCall represents a tool invocation artifact.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
	Value     any // NEW
}

// Kind returns the artifact kind identifier.
func (t ToolCall) Kind() string { return "tool_call" }

// LLMString returns a string representation of the tool call suitable
// for consumption by an LLM provider. It prefers the custom LLMRenderer
// on Value, falls back to json.Marshal of Value, and finally falls back
// to the raw Arguments string.
func (t ToolCall) LLMString() string {
	if t.Value != nil {
		if r, ok := t.Value.(LLMRenderer); ok {
			return r.MarshalLLM()
		}
		if b, err := json.Marshal(t.Value); err == nil {
			return string(b)
		}
	}
	return t.Arguments
}

// MarkdownString returns a string representation of the tool call
// suitable for human display. It prefers the custom MarkdownRenderer on
// Value, falls back to json.Marshal of Value, and finally falls back
// to the raw Arguments string.
func (t ToolCall) MarkdownString() string {
	if t.Value != nil {
		if r, ok := t.Value.(MarkdownRenderer); ok {
			return r.MarshalMarkdown()
		}
		if b, err := json.Marshal(t.Value); err == nil {
			return string(b)
		}
	}
	return t.Arguments
}

// MarshalJSON serializes ToolCall to JSON. The display field is only
// included when it differs from the raw arguments.
func (t ToolCall) MarshalJSON() ([]byte, error) {
	type output struct {
		Kind      string `json:"kind"`
		ID        string `json:"id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
		Display   string `json:"display,omitempty"`
	}
	display := t.MarkdownString()
	if display == t.Arguments {
		display = ""
	}
	return json.Marshal(output{
		Kind:      "tool_call",
		ID:        t.ID,
		Name:      t.Name,
		Arguments: t.Arguments,
		Display:   display,
	})
}

// StatusContributor is implemented by tool result values that carry
// ambient metadata to be broadcast to all subscribers via PropertiesEvent.
type StatusContributor interface {
	Status() map[string]string
}

// LLMRenderer is implemented by tool result values that know how to
// serialize themselves for consumption by an LLM provider.
type LLMRenderer interface {
	MarshalLLM() string
}

// MarkdownRenderer is implemented by tool result values that know how to
// render themselves as Markdown for human display.
type MarkdownRenderer interface {
	MarshalMarkdown() string
}

// ToolResult represents the result of executing a tool call.
// Content holds a JSON-marshaled fallback string for consumers that do not
// support custom rendering. Value holds the raw typed result, enabling
// downstream packages (providers, conduits) to apply custom serialization
// via LLMRenderer or MarkdownRenderer. When Value is nil or its type is
// not recognized, consumers fall back to Content.
//
// Truncation is set by the framework handler when a tool's result was
// bounded by Format or by the framework defaults. A nil Truncation
// means the result was not truncated.
type ToolResult struct {
	ToolCallID  string      `json:"tool_call_id"`
	Content     string      `json:"content"`
	Value       any         `json:"-"`
	IsError     bool        `json:"is_error"`
	Truncation  *Truncation `json:"truncation,omitempty"`
}

// Kind returns the artifact kind identifier.
func (t ToolResult) Kind() string { return "tool_result" }

// LLMString returns a string representation of the tool result suitable
// for consumption by an LLM provider. It prefers the custom LLMRenderer
// on Value, falls back to json.Marshal of Value, and finally falls back
// to the pre-serialized Content string.
func (t ToolResult) LLMString() string {
	if t.Value != nil {
		if r, ok := t.Value.(LLMRenderer); ok {
			return r.MarshalLLM()
		}
		if b, err := json.Marshal(t.Value); err == nil {
			return string(b)
		}
	}
	return t.Content
}

// MarkdownString returns a string representation of the tool result
// suitable for human display. It prefers the custom MarkdownRenderer on
// Value, falls back to json.Marshal of Value, and finally falls back
// to the pre-serialized Content string.
func (t ToolResult) MarkdownString() string {
	if t.Value != nil {
		if r, ok := t.Value.(MarkdownRenderer); ok {
			return r.MarshalMarkdown()
		}
		if b, err := json.MarshalIndent(t.Value, "", "  "); err == nil {
			return "```json\n" + string(b) + "\n```"
		}
	}
	return t.Content
}

// MarshalJSON serializes ToolResult to JSON.
func (t ToolResult) MarshalJSON() ([]byte, error) {
	type output struct {
		Kind       string      `json:"kind"`
		ToolCallID string      `json:"tool_call_id"`
		Content    string      `json:"content"`
		IsError    bool        `json:"is_error"`
		Truncation *Truncation `json:"truncation,omitempty"`
	}
	return json.Marshal(output{
		Kind:       "tool_result",
		ToolCallID: t.ToolCallID,
		Content:    t.MarkdownString(),
		IsError:    t.IsError,
		Truncation: t.Truncation,
	})
}

// Usage represents token consumption metadata from a provider response.
//
// CacheReadTokens and CacheWriteTokens are populated by providers that support
// prompt caching. On OpenAI native, CacheReadTokens is set from
// usage.prompt_tokens_details.cached_tokens. On Anthropic-style hosts (and
// Anthropic-via-OpenRouter), CacheReadTokens and CacheWriteTokens are set from
// usage.cache_read_input_tokens and usage.cache_creation_input_tokens
// respectively. Providers that do not report cache metadata leave both fields
// at their zero value; the `omitempty` tags keep them out of the JSON payload
// in that case so consumers can distinguish "no cache reported" from
// "explicitly zero".
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	CacheReadTokens  int `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int `json:"cache_write_tokens,omitempty"`
	// ReasoningSignature stores an opaque signature for reasoning replay.
	ReasoningSignature string `json:"reasoning_signature,omitempty"`
}

// Kind returns the artifact kind identifier.
func (u Usage) Kind() string { return "usage" }

// MarshalJSON serializes Usage to JSON.
func (u Usage) MarshalJSON() ([]byte, error) {
	type output struct {
		Kind             string `json:"kind"`
		PromptTokens     int    `json:"prompt_tokens"`
		CompletionTokens int    `json:"completion_tokens"`
		TotalTokens      int    `json:"total_tokens"`
		CacheReadTokens  int    `json:"cache_read_tokens,omitempty"`
		CacheWriteTokens int    `json:"cache_write_tokens,omitempty"`
		ReasoningSignature string `json:"reasoning_signature,omitempty"`
	}
	return json.Marshal(output{
		Kind:             "usage",
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      u.TotalTokens,
		CacheReadTokens:  u.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens,
		ReasoningSignature: u.ReasoningSignature,
	})
}

// Image represents an image artifact referenced by URL.
type Image struct {
	URL string
}

// Kind returns the artifact kind identifier.
func (i Image) Kind() string { return "image" }

// MarshalJSON serializes Image to JSON.
func (i Image) MarshalJSON() ([]byte, error) {
	type output struct {
		Kind string `json:"kind"`
		URL  string `json:"url"`
	}
	return json.Marshal(output{Kind: "image", URL: i.URL})
}

// Reasoning represents a reasoning or thinking content artifact.
type Reasoning struct {
	Content string
}

// Kind returns the artifact kind identifier.
func (r Reasoning) Kind() string { return "reasoning" }

// MarshalJSON serializes Reasoning to JSON.
func (r Reasoning) MarshalJSON() ([]byte, error) {
	type output struct {
		Kind    string `json:"kind"`
		Content string `json:"content"`
	}
	return json.Marshal(output{Kind: "reasoning", Content: r.Content})
}

// TextDelta represents a partial chunk of text content for streaming.
type TextDelta struct {
	Content string
}

// Kind returns the artifact kind identifier.
func (t TextDelta) Kind() string { return "text_delta" }

// IsDelta marks TextDelta as an ephemeral streaming fragment.
func (t TextDelta) IsDelta() {}

// MarshalJSON serializes TextDelta to JSON.
func (t TextDelta) MarshalJSON() ([]byte, error) {
	type output struct {
		Kind    string `json:"kind"`
		Content string `json:"content"`
	}
	return json.Marshal(output{Kind: "text_delta", Content: t.Content})
}

// ReasoningDelta represents a partial chunk of reasoning content for streaming.
type ReasoningDelta struct {
	Content string
}

// Kind returns the artifact kind identifier.
func (r ReasoningDelta) Kind() string { return "reasoning_delta" }

// IsDelta marks ReasoningDelta as an ephemeral streaming fragment.
func (r ReasoningDelta) IsDelta() {}

// MarshalJSON serializes ReasoningDelta to JSON.
func (r ReasoningDelta) MarshalJSON() ([]byte, error) {
	type output struct {
		Kind    string `json:"kind"`
		Content string `json:"content"`
	}
	return json.Marshal(output{Kind: "reasoning_delta", Content: r.Content})
}

// ToolCallDelta represents a partial chunk of a tool invocation for streaming.
// Index identifies which parallel tool call in the current turn this fragment
// belongs to, enabling the generic accumulator to merge chunks independently.
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

// MarshalJSON serializes ToolCallDelta to JSON.
func (t ToolCallDelta) MarshalJSON() ([]byte, error) {
	type output struct {
		Kind      string `json:"kind"`
		ID        string `json:"id"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
		Index     int    `json:"index"`
	}
	return json.Marshal(output{
		Kind:      "tool_call_delta",
		ID:        t.ID,
		Name:      t.Name,
		Arguments: t.Arguments,
		Index:     t.Index,
	})
}

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
// Value is not present in deltas and is left nil on seeding.
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
