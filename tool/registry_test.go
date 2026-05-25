package tool

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/andrewhowdencom/ore/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistry_Register_Overwrite(t *testing.T) {
	r := &registry{localTools: make(map[string]*localTool)}
	require.NoError(t, r.Register("test", "", nil, func(ctx context.Context, args map[string]any) (any, error) {
		return "first", nil
	}))
	require.NoError(t, r.Register("test", "", nil, func(ctx context.Context, args map[string]any) (any, error) {
		return "second", nil
	}))

	lt := r.localTools["test"]
	result, err := lt.fn(nil, nil)
	assert.NoError(t, err)
	assert.Equal(t, "second", result)
}

func TestRegistry_Register_Overwrite_Tools(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register("test", "first desc", map[string]any{"type": "object", "title": "first"}, func(ctx context.Context, args map[string]any) (any, error) {
		return "first", nil
	}))
	require.NoError(t, r.Register("test", "second desc", map[string]any{"type": "object", "title": "second"}, func(ctx context.Context, args map[string]any) (any, error) {
		return "second", nil
	}))

	tools := r.Tools()
	require.Len(t, tools, 1)
	assert.Equal(t, "test", tools[0].Name)
	assert.Equal(t, "second desc", tools[0].Description)
	assert.Equal(t, map[string]any{"type": "object", "title": "second"}, tools[0].Schema)
}

func TestRegistry_Register_InvalidSchema(t *testing.T) {
	r := &registry{localTools: make(map[string]*localTool)}
	err := r.Register("bad", "Bad schema", map[string]any{"type": "string"}, func(ctx context.Context, args map[string]any) (any, error) {
		return nil, nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `schema root type must be "object"`)
	assert.Nil(t, r.localTools["bad"])
}

func TestRegistry_Register_UnknownTopLevelKey(t *testing.T) {
	r := &registry{localTools: make(map[string]*localTool)}
	err := r.Register("bad", "Bad schema", map[string]any{
		"type": "object",
		"foo":  map[string]any{},
	}, func(ctx context.Context, args map[string]any) (any, error) {
		return nil, nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `schema contains unknown top-level key "foo"`)
	assert.Nil(t, r.localTools["bad"])
}

func TestRegistry_Register_NestedNonSerializable(t *testing.T) {
	r := &registry{localTools: make(map[string]*localTool)}
	err := r.Register("bad", "Bad schema", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"bad": map[string]any{"type": make(chan int)},
		},
	}, func(ctx context.Context, args map[string]any) (any, error) {
		return nil, nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema is not JSON-serializable")
	assert.Nil(t, r.localTools["bad"])
}

func TestRegistry_ConcurrentRegistration(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup
	errCh := make(chan error, 100)

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := fmt.Sprintf("tool-%d", n)
			if err := r.Register(name, "", nil, func(ctx context.Context, args map[string]any) (any, error) {
				return n, nil
			}); err != nil {
				errCh <- err
			}
		}(i)
	}

	wg.Wait()
	close(errCh)

	for err := range errCh {
		assert.NoError(t, err)
	}

	// Verify all tools were registered.
	concrete := r.(*registry)
	for i := 0; i < 100; i++ {
		name := fmt.Sprintf("tool-%d", i)
		lt, ok := concrete.localTools[name]
		assert.True(t, ok, "tool %s should be registered", name)
		result, err := lt.fn(nil, nil)
		assert.NoError(t, err)
		assert.Equal(t, i, result)
	}
}

func TestRegistry_Tools_LocalOnly(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register("add", "Add two numbers", map[string]any{"type": "object"}, func(ctx context.Context, args map[string]any) (any, error) {
		return 0, nil
	}))

	tools := r.Tools()
	assert.Len(t, tools, 1)
	assert.Equal(t, "add", tools[0].Name)
	assert.Equal(t, "Add two numbers", tools[0].Description)
	assert.Equal(t, map[string]any{"type": "object"}, tools[0].Schema)
}

type mockRemoteSource struct {
	name  string
	tools []provider.Tool
}

func (m *mockRemoteSource) Name() string { return m.name }
func (m *mockRemoteSource) Tools() []provider.Tool {
	t := make([]provider.Tool, len(m.tools))
	copy(t, m.tools)
	return t
}
func (m *mockRemoteSource) Call(ctx context.Context, name string, args map[string]any) (any, error) {
	return "remote-result", nil
}

func TestRegistry_Tools_WithRemoteSource(t *testing.T) {
	remote := &mockRemoteSource{
		name: "filesystem",
		tools: []provider.Tool{
			{Name: "read_file", Description: "Read a file", Schema: map[string]any{"type": "object"}},
		},
	}

	r := NewRegistry(WithMCPServer(remote))
	require.NoError(t, r.Register("add", "Add two numbers", nil, func(ctx context.Context, args map[string]any) (any, error) {
		return 0, nil
	}))

	tools := r.Tools()
	assert.Len(t, tools, 2)

	var names []string
	for _, tool := range tools {
		names = append(names, tool.Name)
	}
	assert.Contains(t, names, "add")
	assert.Contains(t, names, "filesystem/read_file")
}
