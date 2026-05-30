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
