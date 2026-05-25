package tool

import (
	"context"

	"github.com/andrewhowdencom/ore/provider"
)

// ToolFunc is a callable tool implementation. It receives parsed JSON arguments
// as a map and returns any result value, which is JSON-serialized before being
// sent back to the LLM.
type ToolFunc func(ctx context.Context, args map[string]any) (any, error)

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
