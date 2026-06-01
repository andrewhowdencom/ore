package tool

import (
	"context"

	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
	toolpkg "github.com/andrewhowdencom/ore/tool"
)

// ToolFilter is a function that selects a subset of tools based on runtime
// context and state. It receives the full tool list from a registry and
// returns the subset that should be presented to the provider for the current
// turn.
type ToolFilter func(ctx context.Context, st state.State, tools []toolpkg.Tool) []toolpkg.Tool

// WithFilteredTools returns a provider.InvokeOption that resolves tools from
// the given registry and applies the filter function before passing them to the
// provider. If filter is nil, all tools from the registry are returned
// unmodified.
func WithFilteredTools(registry toolpkg.Registry, filter ToolFilter) provider.InvokeOption {
	return provider.ToolsOption{
		Tools: func(ctx context.Context, st state.State) []toolpkg.Tool {
			all := registry.Tools()
			if filter == nil {
				return all
			}
			return filter(ctx, st, all)
		},
	}
}
