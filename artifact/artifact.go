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
//
// Arguments is the JSON object the model streamed; it is the source
// of truth for the wire format that providers serialize back to the
// upstream API. Display is an optional, opaque value attached by
// applyDisplayHints in the loop pipeline; it is for human rendering
// only (TUI, exporters, log viewers) and is never consulted by the
// wire-format code path. The two fields are deliberately decoupled
// so a tool's display choice cannot corrupt the wire format.
type ToolCall struct {
	ID        string
	Name      string
	Arguments string
	Display   any
}

// Kind returns the artifact kind identifier.
func (t ToolCall) Kind() string { return "tool_call" }

// LLMString returns the LLM-visible string representation of the tool
// call. For a tool call, the LLM sees the wire format — the JSON
// object the model originally streamed — so this returns Arguments
// directly. The display value is intentionally ignored: display is a
// human-rendering concern, not a wire-format concern.
//
// This method exists primarily to support x/llmbytes' byte counting,
// which estimates the LLM-visible size of each artifact. Returning
// Arguments gives a more accurate estimate than the previous
// json.Marshal-of-Display fallback, which frequently diverged from
// the actual wire size for tools that return non-JSON display values.
func (t ToolCall) LLMString() string {
	return t.Arguments
}

// MarkdownString returns a human-readable representation of the tool
// call for display layers (TUI, exporters, log viewers). The lookup
// order is:
//
//  1. If Display implements MarkdownRenderer, use MarshalMarkdown.
//  2. If Display is a string, return it as-is. This is the common
//     case for tools whose DisplayHint returns a pre-formatted label
//     (e.g. "📁 list_directory(/path)"). The previous implementation
//     json.Marshaled strings and embedded literal quotes; this is
//     corrected here.
//  3. Otherwise, fall back to json.Marshal of Display so structured
//     values still render usefully.
//  4. When Display is nil, fall through to Arguments.
//
// Display is the single source of truth for human rendering; Arguments
// is intentionally not consulted unless Display is nil. Providers
// never call this method — it is for display consumers only.
func (t ToolCall) MarkdownString() string {
	if r, ok := t.Display.(MarkdownRenderer); ok {
		return r.MarshalMarkdown()
	}
	if s, ok := t.Display.(string); ok {
		return s
	}
	if t.Display != nil {
		if b, err := json.Marshal(t.Display); err == nil {
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
	ToolCallID string      `json:"tool_call_id"`
	Content    string      `json:"content"`
	Value      any         `json:"-"`
	IsError    bool        `json:"is_error"`
	Truncation *Truncation `json:"truncation,omitempty"`
}

// Kind returns the artifact kind identifier.
func (t ToolResult) Kind() string { return "tool_result" }

// LLMString returns a string representation of the tool result suitable
// for consumption by an LLM provider. It prefers the custom LLMRenderer
// on Value, falls back to json.Marshal of Value, and finally falls back
// to the pre-serialized Content string.
//
// When the result carries an error, the pre-serialized Content wins
// regardless of what Value holds. This guarantees the `**Error:** <err>`
// footer that the framework handler appended after truncation reaches
// the LLM, even when Value is a typed result that would otherwise be
// re-marshaled (and would drop the footer). Without this short-circuit,
// a tool that returns (result, err) on the error path would render
// only the partial result to the model — silently discarding the error.
func (t ToolResult) LLMString() string {
	if t.IsError && t.Content != "" {
		return t.Content
	}
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
	if t.IsError && t.Content != "" {
		return t.Content
	}
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
//
// ThinkingTokens is the count of output tokens consumed by the model's
// extended-thinking / reasoning phase. Anthropic and Anthropic-via-OpenRouter
// surface this on the streaming message_delta usage block.
//
// The field is a pointer to distinguish three states:
//
//   - nil — the provider did not report thinking tokens at all (e.g., a
//     proxy that omits `output_tokens_details` from the usage block).
//     Callers should treat this as "unknown", not as zero.
//   - non-nil pointing to 0 — the provider reported zero thinking
//     tokens (e.g., thinking was enabled but the model did not reason,
//     or `adaptive` thinking returned nothing). This is a meaningful
//     count of zero, distinct from "unknown".
//   - non-nil pointing to N — the provider reported N thinking tokens.
//
// The `omitempty` JSON tag drops the field from the payload when the
// pointer is nil; an explicit zero is encoded as `"thinking_tokens": 0`.
type Usage struct {
	PromptTokens     int  `json:"prompt_tokens"`
	CompletionTokens int  `json:"completion_tokens"`
	TotalTokens      int  `json:"total_tokens"`
	CacheReadTokens  int  `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int  `json:"cache_write_tokens,omitempty"`
	ThinkingTokens   *int `json:"thinking_tokens,omitempty"`
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
		ThinkingTokens   *int   `json:"thinking_tokens,omitempty"`
	}
	return json.Marshal(output{
		Kind:             "usage",
		PromptTokens:     u.PromptTokens,
		CompletionTokens: u.CompletionTokens,
		TotalTokens:      u.TotalTokens,
		CacheReadTokens:  u.CacheReadTokens,
		CacheWriteTokens: u.CacheWriteTokens,
		ThinkingTokens:   u.ThinkingTokens,
	})
}

// StopReasonKind is a canonical, provider-agnostic description of why a
// model stopped generating. Adapters translate provider-specific values
// (Anthropic stop_reason, OpenAI finish_reason) into one of these
// constants at the read-side, so downstream code never has to know
// which provider produced the stream.
//
// The empty string is not a valid reason. Adapters should not emit a
// StopReason when the upstream did not report a reason; consumers
// should treat a missing StopReason as equivalent to StopReasonOther
// for forward compatibility.
//
// Adding a new value is non-breaking; renaming a value is breaking.
type StopReasonKind string

const (
	// StopReasonStop indicates the model finished normally — it produced
	// a complete response without hitting a length cap, calling a tool,
	// or being interrupted by a safety filter.
	StopReasonStop StopReasonKind = "stop"

	// StopReasonLength indicates the model hit a token-output cap
	// (Anthropic max_tokens, OpenAI length). The response may be
	// truncated; consumers that cannot tolerate truncation should
	// surface an error.
	StopReasonLength StopReasonKind = "length"

	// StopReasonToolUse indicates the model emitted a tool invocation
	// block. Adapters emit a ToolCall artifact alongside.
	StopReasonToolUse StopReasonKind = "tool_use"

	// StopReasonRefusal indicates the model declined to produce a
	// response due to a safety filter (Anthropic refusal, OpenAI
	// content_filter).
	StopReasonRefusal StopReasonKind = "refusal"

	// StopReasonOther is the catch-all for upstream values not covered
	// by the canonical set (e.g. Anthropic stop_sequence, or any new
	// reason a future adapter introduces). Forward-compatible: new
	// adapters can map unknown values to this without breaking
	// existing consumers.
	StopReasonOther StopReasonKind = "other"
)

// StopReason is the artifact emitted by adapters on the streaming
// channel to communicate why the model stopped generating. It is
// emitted immediately before the final Usage artifact at the end of
// a successful stream.
//
// The Reason field is the canonical StopReasonKind. Adapters are
// responsible for translating their provider-specific vocabulary
// (Anthropic stop_reason, OpenAI finish_reason) into the canonical
// set at the read-side; consumers can switch on Reason without
// caring which provider produced the stream.
type StopReason struct {
	Reason StopReasonKind
}

// Kind returns the artifact kind identifier.
func (s StopReason) Kind() string { return "stop_reason" }

// MarshalJSON serializes StopReason to JSON.
func (s StopReason) MarshalJSON() ([]byte, error) {
	type output struct {
		Kind   string         `json:"kind"`
		Reason StopReasonKind `json:"reason"`
	}
	return json.Marshal(output{
		Kind:   "stop_reason",
		Reason: s.Reason,
	})
}

// Compaction was removed: the boundary marker that previously lived
// in the artifact stream has moved to state.Meta. The replacement
// type is x/compaction.BoundaryInfo, which is JSON-serialized for
// storage under the ore.compaction.boundary.* keys in state.Meta.
// See the x/compaction package for the new contract.
//
// The Compaction artifact kind was originally added so the Transform
// could scan the buffer for a "read up until this mark" marker
// without the Transform having to know about metadata outside the
// stream. The implementation conflated control-plane metadata with
// LLM-facing content: the wire's onlyText predicate rejected the
// [Compaction, Text] shape and silently dropped the entire turn,
// causing the LLM to lose pre-compaction context. See issue #499.
//
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

// ReasoningSignature represents an opaque, provider-issued signature that
// anchors a prior reasoning block to the next request. It is emitted by
// providers that support extended-thinking / reasoning replay, and is
// consumed by request serializers to attach the right wire shape to the
// assistant content array of the next turn.
//
// The three fields are the minimal representation that lets a single
// artifact type carry every wire shape the supported upstreams produce:
//
//   - Provider discriminates the upstream. Today the supported values are
//     "anthropic" and "openai"; new providers extend the set.
//   - SubKind discriminates the wire shape that the upstream expects for
//     replay. Defined values:
//   - "signature": Anthropic thinking-block signature (the opaque
//     signature that anchors extended-thinking across turns; arrives on
//     the response side of ThinkingBlock).
//   - "redacted": Anthropic redacted_thinking block data (encrypted
//     reasoning; arrives on the response side of RedactedThinkingBlock).
//   - "encrypted": OpenAI / OpenRouter reasoning.encrypted entry in
//     reasoning_details[] on the chat-completions wire.
//   - Data is opaque. Consumers MUST NOT parse it; the request
//     serializer routes it to the correct upstream shape based on
//     (Provider, SubKind).
//
// The struct field is named SubKind (not Kind) to avoid a name collision
// with the artifact interface's Kind() method. The explicit JSON tags
// are required so the wire-level `sub_kind` key is matched to the
// `SubKind` field on the way in; without them, encoding/json's
// case-insensitive field matching would route `sub_kind` to a `Kind`
// field (which does not exist) instead, silently dropping the value.
//
// ReasoningSignature is intentionally not Delta / Accumulable: signatures
// are complete artifacts emitted at the close of a reasoning block,
// never incrementally.
type ReasoningSignature struct {
	Provider string `json:"provider"`
	SubKind  string `json:"sub_kind"`
	Data     string `json:"data"`
}

// Kind returns the artifact kind identifier.
func (r ReasoningSignature) Kind() string { return "reasoning_signature" }

// MarshalJSON serializes ReasoningSignature to JSON.
func (r ReasoningSignature) MarshalJSON() ([]byte, error) {
	type output struct {
		Kind     string `json:"kind"`
		Provider string `json:"provider"`
		SubKind  string `json:"sub_kind"`
		Data     string `json:"data"`
	}
	return json.Marshal(output{
		Kind:     "reasoning_signature",
		Provider: r.Provider,
		SubKind:  r.SubKind,
		Data:     r.Data,
	})
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
// Display is not present in deltas and is left nil on seeding; it is
// populated later by applyDisplayHints in the loop pipeline.
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

// isPersistent marks the persistable concrete types in this package.
// Each type below gains both an isPersistent() method and an init()
// that registers its factory with the package-level registry. Delta
// types deliberately do not implement this method.

// isPersistent marks Text as persistable.
func (Text) isPersistent() {}

// isPersistent marks ToolCall as persistable.
func (ToolCall) isPersistent() {}

// isPersistent marks ToolResult as persistable.
func (ToolResult) isPersistent() {}

// isPersistent marks Usage as persistable.
func (Usage) isPersistent() {}

// isPersistent marks Image as persistable.
func (Image) isPersistent() {}

// isPersistent marks Reasoning as persistable.
func (Reasoning) isPersistent() {}

// init blocks: register each persistable type's factory with the
// package-level registry. The factories return zero-value instances;
// consumers use them to seed a typed pointer for json.Unmarshal.

// Text
func init() {
	Register("text", func() Artifact { return &Text{} })
}

// ToolCall
func init() {
	Register("tool_call", func() Artifact { return &ToolCall{} })
}

// ToolResult
func init() {
	Register("tool_result", func() Artifact { return &ToolResult{} })
}

// Usage
func init() {
	Register("usage", func() Artifact { return &Usage{} })
}

// Image
func init() {
	Register("image", func() Artifact { return &Image{} })
}

// Reasoning
func init() {
	Register("reasoning", func() Artifact { return &Reasoning{} })
}

// StopReason
func init() {
	Register("stop_reason", func() Artifact { return &StopReason{} })
}

// ReasoningSignature
func init() {
	Register("reasoning_signature", func() Artifact { return &ReasoningSignature{} })
}
