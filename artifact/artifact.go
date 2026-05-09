// Package artifact defines the extensible Artifact interface and common concrete
// types used throughout tack. The Artifact interface exposes a public Kind()
// method to allow custom artifact types to be defined in other packages.
package artifact

// Artifact is the base interface for all LLM response artifacts.
type Artifact interface {
	Kind() string
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

// ReasoningDelta represents a partial chunk of reasoning content for streaming.
type ReasoningDelta struct {
	Content string
}

// Kind returns the artifact kind identifier.
func (r ReasoningDelta) Kind() string { return "reasoning_delta" }

// ToolCallDelta represents a partial chunk of a tool invocation for streaming.
type ToolCallDelta struct {
	ID        string
	Name      string
	Arguments string
}

// Kind returns the artifact kind identifier.
func (t ToolCallDelta) Kind() string { return "tool_call_delta" }
