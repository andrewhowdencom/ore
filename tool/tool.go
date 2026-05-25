package tool

import (
	"context"
	"time"

	"github.com/andrewhowdencom/ore/provider"
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
	Tools() []provider.Tool
	// Call invokes a tool by name with the given arguments.
	Call(ctx context.Context, name string, args map[string]any) (any, error)
}
