package tool

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"
)

// Sandbox is the base interface for all sandbox implementations.
// A nil sandbox means no isolation (tools execute against the host).
type Sandbox interface {
	Name() string
}

// FileSandbox provides filesystem isolation for tool execution.
// Tools type-assert the received Sandbox to FileSandbox when they need
// path resolution or working directory constraints.
type FileSandbox interface {
	Sandbox
	ResolvePath(path string) (string, error)
	WorkingDirectory() string
}

// ExecSandbox provides process isolation for tool execution.
// Tools type-assert the received Sandbox to ExecSandbox when they need
// to delegate command execution to the sandbox.
type ExecSandbox interface {
	Sandbox
	Run(ctx context.Context, cmd, dir string, timeout time.Duration) (stdout, stderr string, exitCode int, err error)
}

// Example describes a single few-shot usage example for a tool.
type Example struct {
	// Input is the JSON arguments passed to the tool.
	Input map[string]any
	// Output is the expected result produced by the tool.
	Output any
	// Explanation is a human-readable note describing why the example
	// produces the given output.
	Explanation string
}

// Tool describes a callable tool exposed to an LLM provider.
type Tool struct {
	Name        string
	Description string
	// Schema defines the JSON Schema for the tool's parameters.
	Schema map[string]any
	// DisplayHint is an optional formatter that receives parsed JSON
	// arguments and returns a displayable value (implementing
	// MarkdownRenderer / LLMRenderer). When nil, conduits fall back to
	// raw JSON Arguments.
	DisplayHint func(map[string]any) any
	// MaxBytes is a hard ceiling on the serialized output size in bytes.
	// If the tool's JSON result exceeds this limit, the framework truncates
	// the content and appends a formatted truncation hint. Zero means no
	// limit.
	MaxBytes int
	// TruncationHint is a template string appended when MaxBytes is exceeded.
	// The placeholder ${N} is replaced with the actual total byte count.
	TruncationHint string
	// Examples is an optional list of few-shot input/output pairs that
	// illustrate how the tool should be used. They are not sent to the
	// provider by default; applications may opt-in via systemprompt
	// transforms or other middleware.
	Examples []Example
}

// ToolFunc is a callable tool implementation. It receives a resolved sandbox
// (may be nil if no sandbox is configured) and parsed JSON arguments as a
// map[string]any and returns any result value, which is JSON-serialized
// before being sent back to the LLM.
type ToolFunc func(ctx context.Context, sb Sandbox, args map[string]any) (any, error)

// RemoteSource represents an external source of tools (e.g., an MCP server).
// The Registry consumes this interface without importing the concrete MCP
// package, allowing clean extension without import cycles.
type RemoteSource interface {
	// Name returns the namespace prefix for tools from this source.
	Name() string
	// Tools returns the list of tools available from this source (un-namespaced).
	Tools() []Tool
	// Call invokes a tool by name with the given arguments.
	Call(ctx context.Context, name string, args map[string]any) (any, error)
}

// TruncateContent truncates content to a valid UTF-8 boundary so that the
// total length (truncated content + formatted hint) does not exceed maxBytes.
// If maxBytes is 0 or len(content) <= maxBytes, content is returned unchanged.
// The hintTemplate may contain ${N} which is replaced with totalBytes.
func TruncateContent(content string, maxBytes int, totalBytes int, hintTemplate string) string {
	if maxBytes <= 0 || len(content) <= maxBytes {
		return content
	}
	hint := strings.ReplaceAll(hintTemplate, "${N}", fmt.Sprintf("%d", totalBytes))
	maxContent := maxBytes - len(hint)
	if maxContent <= 0 {
		return hint
	}
	truncated := truncateToValidUTF8(content, maxContent)
	return truncated + hint
}

func truncateToValidUTF8(s string, maxBytes int) string {
	if maxBytes >= len(s) {
		return s
	}
	for maxBytes > 0 && !utf8.ValidString(s[:maxBytes]) {
		maxBytes--
	}
	return s[:maxBytes]
}
