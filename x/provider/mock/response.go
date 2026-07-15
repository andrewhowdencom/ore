package mock

// Response is the high-level, wire-format-agnostic canned response emitted
// by a mock server on each HTTP request. Each vendor sub-package
// translates this into the wire's exact SSE frame format.
//
// Zero values are meaningful: a Response with only Text set produces a
// vanilla text reply; a Response with empty fields produces a stream that
// ends immediately with the configured (or default) finish reason and
// usage. Omitempty tags keep the JSON serialization compact for the most
// common case of a single-field response.
type Response struct {
	// Text is the assistant's final text content. The vendor sub-package
	// streams it in chunks via SSE delta events.
	Text string `json:"text,omitempty"`

	// Reasoning is the model's chain-of-thought. Anthropic-style thinking
	// models and OpenAI `reasoning_content` routes consume this field.
	// When non-empty, the vendor emits `thinking_delta` (Anthropic) or
	// `delta.reasoning_content` (OpenAI) events ahead of the text block.
	Reasoning string `json:"reasoning,omitempty"`

	// Signature carries the encrypted reasoning signature that Anthropic
	// thinking models return alongside thinking content. When non-empty,
	// the Anthropic vendor emits a `signature_delta` event after the
	// reasoning block so the next turn can replay it.
	Signature string `json:"signature,omitempty"`

	// ToolCalls are the tool invocations the model requests. The vendor
	// emits one content block per tool call, with a streamed
	// `input_json_delta` carrying the partial arguments.
	ToolCalls []ToolCall `json:"tool_calls,omitempty"`

	// StopReason is the finish reason emitted on the trailing delta.
	// Defaults to "stop" (OpenAI) or "end_turn" (Anthropic) when zero,
	// depending on the vendor.
	StopReason string `json:"stop_reason,omitempty"`

	// Usage is the token-accounting block surfaced on the trailing delta
	// (OpenAI) or the closing `message_delta` (Anthropic). Optional; a
	// zero Usage is omitted from the wire.
	Usage *Usage `json:"usage,omitempty"`
}

// ToolCall is a single tool invocation requested by a canned response.
// The vendor sub-package streams the tool block in SSE-delta chunks:
// `content_block_start` with the id and name, then `input_json_delta`
// events carrying fragments of Arguments.
type ToolCall struct {
	// ID is the unique identifier for this tool call (e.g. "call_1" for
	// OpenAI, "toolu_1" for Anthropic).
	ID string `json:"id"`

	// Name is the registered tool name (e.g. "search", "calculator").
	Name string `json:"name"`

	// Arguments is the raw JSON argument payload. The vendor may stream
	// it in chunks for realism; the bytes are forwarded unchanged.
	Arguments string `json:"arguments"`
}

// Usage reports token accounting for the canned response. Optional; a
// nil *Usage is treated as "no usage emitted".
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}
