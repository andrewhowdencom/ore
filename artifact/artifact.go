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
	Name      string
	Arguments string
}

// Kind returns the artifact kind identifier.
func (t ToolCall) Kind() string { return "tool_call" }

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
