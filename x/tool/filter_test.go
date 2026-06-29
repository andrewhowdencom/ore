package tool

import (
	"context"
	"testing"

	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/ledger"
	toolpkg "github.com/andrewhowdencom/ore/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWithFilteredTools_NoFilter(t *testing.T) {
	registry := toolpkg.NewRegistry()
	require.NoError(t, registry.Register(toolpkg.Tool{Name: "add", Description: "Add two numbers", Schema: nil}, func(context.Context, toolpkg.Sandbox, map[string]any) (any, error) {
		return nil, nil
	}))

	opt := WithFilteredTools(registry, nil)
	to, ok := opt.(provider.ToolsOption)
	require.True(t, ok)

	ctx := context.Background()
	mem := &ledger.Buffer{}
	tools := to.Tools(ctx, mem)

	assert.Len(t, tools, 1)
	assert.Equal(t, "add", tools[0].Name)
}

func TestWithFilteredTools_WithFilter(t *testing.T) {
	registry := toolpkg.NewRegistry()
	require.NoError(t, registry.Register(toolpkg.Tool{Name: "add", Description: "Add two numbers", Schema: nil}, func(context.Context, toolpkg.Sandbox, map[string]any) (any, error) {
		return nil, nil
	}))
	require.NoError(t, registry.Register(toolpkg.Tool{Name: "multiply", Description: "Multiply two numbers", Schema: nil}, func(context.Context, toolpkg.Sandbox, map[string]any) (any, error) {
		return nil, nil
	}))

	filter := func(ctx context.Context, st ledger.State, tools []toolpkg.Tool) []toolpkg.Tool {
		var result []toolpkg.Tool
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
	mem := &ledger.Buffer{}
	tools := to.Tools(ctx, mem)

	assert.Len(t, tools, 1)
	assert.Equal(t, "add", tools[0].Name)
}

func TestWithFilteredTools_EmptyResult(t *testing.T) {
	registry := toolpkg.NewRegistry()
	require.NoError(t, registry.Register(toolpkg.Tool{Name: "add", Description: "Add two numbers", Schema: nil}, func(context.Context, toolpkg.Sandbox, map[string]any) (any, error) {
		return nil, nil
	}))

	filter := func(ctx context.Context, st ledger.State, tools []toolpkg.Tool) []toolpkg.Tool {
		return nil
	}

	opt := WithFilteredTools(registry, filter)
	to, ok := opt.(provider.ToolsOption)
	require.True(t, ok)

	ctx := context.Background()
	mem := &ledger.Buffer{}
	tools := to.Tools(ctx, mem)

	assert.Empty(t, tools)
}

func TestWithFilteredTools_MutatesSlice(t *testing.T) {
	registry := toolpkg.NewRegistry()
	require.NoError(t, registry.Register(toolpkg.Tool{Name: "b", Description: "Tool B", Schema: nil}, func(context.Context, toolpkg.Sandbox, map[string]any) (any, error) {
		return nil, nil
	}))
	require.NoError(t, registry.Register(toolpkg.Tool{Name: "a", Description: "Tool A", Schema: nil}, func(context.Context, toolpkg.Sandbox, map[string]any) (any, error) {
		return nil, nil
	}))

	// Capture the order the registry returns so we can assert reversal
	// regardless of non-deterministic map iteration order.
	var inputOrder []string

	// Filter that reorders without mutating the original.
	filter := func(ctx context.Context, st ledger.State, tools []toolpkg.Tool) []toolpkg.Tool {
		inputOrder = make([]string, len(tools))
		for i, t := range tools {
			inputOrder[i] = t.Name
		}
		result := make([]toolpkg.Tool, len(tools))
		for i, t := range tools {
			result[len(tools)-1-i] = t
		}
		return result
	}

	opt := WithFilteredTools(registry, filter)
	to, ok := opt.(provider.ToolsOption)
	require.True(t, ok)

	ctx := context.Background()
	mem := &ledger.Buffer{}
	tools := to.Tools(ctx, mem)

	require.Len(t, tools, 2)
	// Verify the filter reversed the input order.
	assert.Equal(t, inputOrder[1], tools[0].Name)
	assert.Equal(t, inputOrder[0], tools[1].Name)
}

func TestWithFilteredTools_Superset(t *testing.T) {
	registry := toolpkg.NewRegistry()
	require.NoError(t, registry.Register(toolpkg.Tool{Name: "original", Description: "Original tool", Schema: nil}, func(context.Context, toolpkg.Sandbox, map[string]any) (any, error) {
		return nil, nil
	}))

	// Filter that adds a tool not present in the registry.
	filter := func(ctx context.Context, st ledger.State, tools []toolpkg.Tool) []toolpkg.Tool {
		return append(tools, toolpkg.Tool{
			Name:        "injected",
			Description: "Injected tool",
			Schema:      map[string]any{"type": "object"},
		})
	}

	opt := WithFilteredTools(registry, filter)
	to, ok := opt.(provider.ToolsOption)
	require.True(t, ok)

	ctx := context.Background()
	mem := &ledger.Buffer{}
	tools := to.Tools(ctx, mem)

	assert.Len(t, tools, 2)
	assert.Equal(t, "original", tools[0].Name)
	assert.Equal(t, "injected", tools[1].Name)
}

func TestWithFilteredTools_Panic(t *testing.T) {
	registry := toolpkg.NewRegistry()
	require.NoError(t, registry.Register(toolpkg.Tool{Name: "panic", Description: "Panic tool", Schema: nil}, func(context.Context, toolpkg.Sandbox, map[string]any) (any, error) {
		return nil, nil
	}))

	filter := func(ctx context.Context, st ledger.State, tools []toolpkg.Tool) []toolpkg.Tool {
		panic("filter panic")
	}

	opt := WithFilteredTools(registry, filter)
	to, ok := opt.(provider.ToolsOption)
	require.True(t, ok)

	ctx := context.Background()
	mem := &ledger.Buffer{}

	assert.Panics(t, func() {
		to.Tools(ctx, mem)
	})
}
