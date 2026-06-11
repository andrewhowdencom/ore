package tool

import (
	"context"
	"time"
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


