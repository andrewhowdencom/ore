package tool

import (
	"context"
	"testing"

	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
	toolpkg "github.com/andrewhowdencom/ore/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithFilteredTools_NoFilter(t *testing.T) {
	registry := toolpkg.NewRegistry()
	require.NoError(t, registry.Register("add", "Add two numbers", nil, func(context.Context, toolpkg.Sandbox, map[string]any) (any, error) {
		return nil, nil
	}))

	opt := WithFilteredTools(registry, nil)
	to, ok := opt.(provider.ToolsOption)
	require.True(t, ok)

	ctx := context.Background()
	mem := &state.Buffer{}
	tools := to.Tools(ctx, mem)

	assert.Len(t, tools, 1)
	assert.Equal(t, "add", tools[0].Name)
}

func TestWithFilteredTools_WithFilter(t *testing.T) {
	registry := toolpkg.NewRegistry()
	require.NoError(t, registry.Register("add", "Add two numbers", nil, func(context.Context, toolpkg.Sandbox, map[string]any) (any, error) {
		return nil, nil
	}))
	require.NoError(t, registry.Register("multiply", "Multiply two numbers", nil, func(context.Context, toolpkg.Sandbox, map[string]any) (any, error) {
		return nil, nil
	}))

	filter := func(ctx context.Context, st state.State, tools []provider.Tool) []provider.Tool {
		var result []provider.Tool
		for _, t := range tools {
			if t.Name == "add" {
				result = append(result, t)
			}
		}
		return result
	}

	opt := WithFilteredTools(registry, filter)
	to, ok := opt.(provider.ToolsOption)
	require.True(t, ok)

	ctx := context.Background()
	mem := &state.Buffer{}
	tools := to.Tools(ctx, mem)

	assert.Len(t, tools, 1)
	assert.Equal(t, "add", tools[0].Name)
}

func TestWithFilteredTools_EmptyResult(t *testing.T) {
	registry := toolpkg.NewRegistry()
	require.NoError(t, registry.Register("add", "Add two numbers", nil, func(context.Context, toolpkg.Sandbox, map[string]any) (any, error) {
		return nil, nil
	}))

	filter := func(ctx context.Context, st state.State, tools []provider.Tool) []provider.Tool {
		return nil
	}

	opt := WithFilteredTools(registry, filter)
	to, ok := opt.(provider.ToolsOption)
	require.True(t, ok)

	ctx := context.Background()
	mem := &state.Buffer{}
	tools := to.Tools(ctx, mem)

	assert.Empty(t, tools)
}

func TestWithFilteredTools_MutatesSlice(t *testing.T) {
	registry := toolpkg.NewRegistry()
	require.NoError(t, registry.Register("b", "Tool B", nil, func(context.Context, toolpkg.Sandbox, map[string]any) (any, error) {
		return nil, nil
	}))
	require.NoError(t, registry.Register("a", "Tool A", nil, func(context.Context, toolpkg.Sandbox, map[string]any) (any, error) {
		return nil, nil
	}))

	// Filter that reorders without mutating the original.
	filter := func(ctx context.Context, st state.State, tools []provider.Tool) []provider.Tool {
		result := make([]provider.Tool, len(tools))
		for i, t := range tools {
			result[len(tools)-1-i] = t
		}
		return result
	}

	opt := WithFilteredTools(registry, filter)
	to, ok := opt.(provider.ToolsOption)
	require.True(t, ok)

	ctx := context.Background()
	mem := &state.Buffer{}
	tools := to.Tools(ctx, mem)

	assert.Len(t, tools, 2)
	assert.Equal(t, "a", tools[0].Name)
	assert.Equal(t, "b", tools[1].Name)
}

func TestWithFilteredTools_Superset(t *testing.T) {
	registry := toolpkg.NewRegistry()
	require.NoError(t, registry.Register("original", "Original tool", nil, func(context.Context, toolpkg.Sandbox, map[string]any) (any, error) {
		return nil, nil
	}))

	// Filter that adds a tool not present in the registry.
	filter := func(ctx context.Context, st state.State, tools []provider.Tool) []provider.Tool {
		return append(tools, provider.Tool{
			Name:        "injected",
			Description: "Injected tool",
			Schema:      map[string]any{"type": "object"},
		})
	}

	opt := WithFilteredTools(registry, filter)
	to, ok := opt.(provider.ToolsOption)
	require.True(t, ok)

	ctx := context.Background()
	mem := &state.Buffer{}
	tools := to.Tools(ctx, mem)

	assert.Len(t, tools, 2)
	assert.Equal(t, "original", tools[0].Name)
	assert.Equal(t, "injected", tools[1].Name)
}

func TestWithFilteredTools_Panic(t *testing.T) {
	registry := toolpkg.NewRegistry()
	require.NoError(t, registry.Register("panic", "Panic tool", nil, func(context.Context, toolpkg.Sandbox, map[string]any) (any, error) {
		return nil, nil
	}))

	filter := func(ctx context.Context, st state.State, tools []provider.Tool) []provider.Tool {
		panic("filter panic")
	}

	opt := WithFilteredTools(registry, filter)
	to, ok := opt.(provider.ToolsOption)
	require.True(t, ok)

	ctx := context.Background()
	mem := &state.Buffer{}

	assert.Panics(t, func() {
		to.Tools(ctx, mem)
	})
}
