package tool

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistry_Register_Overwrite(t *testing.T) {
	r := &registry{localTools: make(map[string]*localTool)}
	require.NoError(t, r.Register(Tool{Name: "test", Description: "", Schema: nil}, func(ctx context.Context, _ Sandbox, args map[string]any) (any, error) {
		return "first", nil
	}))
	require.NoError(t, r.Register(Tool{Name: "test", Description: "", Schema: nil}, func(ctx context.Context, _ Sandbox, args map[string]any) (any, error) {
		return "second", nil
	}))

	lt := r.localTools["test"]
	result, err := lt.fn(context.Background(), nil, nil)
	assert.NoError(t, err)
	assert.Equal(t, "second", result)
}

func TestRegistry_Register_Overwrite_Tools(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(Tool{Name: "test", Description: "first desc", Schema: map[string]any{"type": "object", "title": "first"}}, func(ctx context.Context, _ Sandbox, args map[string]any) (any, error) {
		return "first", nil
	}))
	require.NoError(t, r.Register(Tool{Name: "test", Description: "second desc", Schema: map[string]any{"type": "object", "title": "second"}}, func(ctx context.Context, _ Sandbox, args map[string]any) (any, error) {
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
	err := r.Register(Tool{Name: "bad", Description: "Bad schema", Schema: map[string]any{"type": "string"}}, func(ctx context.Context, _ Sandbox, args map[string]any) (any, error) {
		return nil, nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `schema root type must be "object"`)
	assert.Nil(t, r.localTools["bad"])
}

func TestRegistry_Register_UnknownTopLevelKey(t *testing.T) {
	r := &registry{localTools: make(map[string]*localTool)}
	err := r.Register(Tool{Name: "bad", Description: "Bad schema", Schema: map[string]any{
		"type": "object",
		"foo":  map[string]any{},
	}}, func(ctx context.Context, _ Sandbox, args map[string]any) (any, error) {
		return nil, nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `schema contains unknown top-level key "foo"`)
	assert.Nil(t, r.localTools["bad"])
}

func TestRegistry_Register_NestedNonSerializable(t *testing.T) {
	r := &registry{localTools: make(map[string]*localTool)}
	err := r.Register(Tool{Name: "bad", Description: "Bad schema", Schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"bad": map[string]any{"type": make(chan int)},
		},
	}}, func(ctx context.Context, _ Sandbox, args map[string]any) (any, error) {
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
			if err := r.Register(Tool{Name: name, Description: "", Schema: nil}, func(ctx context.Context, _ Sandbox, args map[string]any) (any, error) {
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
		result, err := lt.fn(context.Background(), nil, nil)
		assert.NoError(t, err)
		assert.Equal(t, i, result)
	}
}

func TestRegistry_Tools_LocalOnly(t *testing.T) {
	r := NewRegistry()
	require.NoError(t, r.Register(Tool{Name: "add", Description: "Add two numbers", Schema: map[string]any{"type": "object"}}, func(ctx context.Context, _ Sandbox, args map[string]any) (any, error) {
		return 0, nil
	}))

	tools := r.Tools()
	assert.Len(t, tools, 1)
	assert.Equal(t, "add", tools[0].Name)
	assert.Equal(t, "Add two numbers", tools[0].Description)
	assert.Equal(t, map[string]any{"type": "object"}, tools[0].Schema)
}

func TestRegistry_ConcurrentToolsReads(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup
	errCh := make(chan error, 100)

	// Concurrent writers registering distinct tools.
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := fmt.Sprintf("tool-%d", n)
			if err := r.Register(Tool{Name: name, Description: "", Schema: nil}, func(ctx context.Context, _ Sandbox, args map[string]any) (any, error) {
				return n, nil
			}); err != nil {
				errCh <- err
			}
		}(i)
	}

	// Concurrent readers calling Tools().
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tools := r.Tools()
			seen := make(map[string]bool)
			for _, tool := range tools {
				if seen[tool.Name] {
					t.Errorf("duplicate tool name %q in Tools() result", tool.Name)
					return
				}
				seen[tool.Name] = true
			}
		}()
	}

	wg.Wait()
	close(errCh)
	for err := range errCh {
		assert.NoError(t, err)
	}
}

func TestRegistry_ConcurrentOverwrite(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			_ = r.Register(Tool{Name: "test", Description: "", Schema: nil}, func(ctx context.Context, _ Sandbox, args map[string]any) (any, error) {
				return n, nil
			})
		}(i)
	}

	wg.Wait()

	// After all overwrites, the final entry should be one of the registered functions.
	fn, ok := r.Lookup("test")
	require.True(t, ok)
	result, err := fn(context.Background(), nil, nil)
	require.NoError(t, err)
	n, ok := result.(int)
	require.True(t, ok)
	assert.True(t, n >= 0 && n < 100, "result %d should be in range [0,100)", n)
}

type mockRemoteSource struct {
	name  string
	tools []Tool
}

func (m *mockRemoteSource) Name() string { return m.name }
func (m *mockRemoteSource) Tools() []Tool {
	t := make([]Tool, len(m.tools))
	copy(t, m.tools)
	return t
}
func (m *mockRemoteSource) Call(ctx context.Context, name string, args map[string]any) (any, error) {
	return "remote-result", nil
}

type mockSandbox struct {
	name string
}

func (m *mockSandbox) Name() string { return m.name }

func TestRegistry_Register_EmptyName(t *testing.T) {
	r := NewRegistry()
	err := r.Register(Tool{Name: "", Description: "empty", Schema: nil}, func(ctx context.Context, _ Sandbox, args map[string]any) (any, error) {
		return nil, nil
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool name cannot be empty")
}

func TestRegistry_Register_NilFunc(t *testing.T) {
	r := NewRegistry()
	err := r.Register(Tool{Name: "test", Description: "test", Schema: nil}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tool function cannot be nil")
}

func TestRegistry_RegisterSandbox(t *testing.T) {
	r := NewRegistry().(SandboxRegistry)
	var sb Sandbox = &mockSandbox{name: "test"}
	r.RegisterSandbox("test", sb)

	found, ok := r.LookupSandbox("test")
	require.True(t, ok)
	assert.Equal(t, sb, found)
}

func TestRegistry_RegisterSandbox_Overwrite(t *testing.T) {
	r := NewRegistry().(SandboxRegistry)
	first := &mockSandbox{name: "first"}
	second := &mockSandbox{name: "second"}

	r.RegisterSandbox("test", first)
	r.RegisterSandbox("test", second)

	sb, ok := r.LookupSandbox("test")
	require.True(t, ok)
	assert.Equal(t, "second", sb.Name())
}

func TestRegistry_RegisterSandbox_EmptyName(t *testing.T) {
	r := NewRegistry().(SandboxRegistry)
	sb := &mockSandbox{name: "empty"}
	r.RegisterSandbox("", sb)

	found, ok := r.LookupSandbox("")
	assert.True(t, ok)
	assert.Equal(t, sb, found)
}

func TestRegistry_LookupSandbox_NotFound(t *testing.T) {
	r := NewRegistry().(SandboxRegistry)
	_, ok := r.LookupSandbox("missing")
	assert.False(t, ok)
}

func TestRegistry_SetDefaultSandbox(t *testing.T) {
	r := NewRegistry().(SandboxRegistry)
	var sb Sandbox = &mockSandbox{name: "default"}
	r.SetDefaultSandbox(sb)

	assert.Equal(t, sb, r.DefaultSandbox())
}

func TestRegistry_DefaultSandbox_Nil(t *testing.T) {
	r := NewRegistry().(SandboxRegistry)
	assert.Nil(t, r.DefaultSandbox())
}

func TestRegistry_ConcurrentSandboxRegistration(t *testing.T) {
	r := NewRegistry().(SandboxRegistry)
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := fmt.Sprintf("sandbox-%d", n)
			r.RegisterSandbox(name, &mockSandbox{name: name})
		}(i)
	}

	wg.Wait()

	for i := 0; i < 100; i++ {
		name := fmt.Sprintf("sandbox-%d", i)
		sb, ok := r.LookupSandbox(name)
		assert.True(t, ok, "sandbox %s should be registered", name)
		assert.Equal(t, name, sb.Name())
	}
}

func TestRegistry_Tools_WithRemoteSource(t *testing.T) {
	remote := &mockRemoteSource{
		name: "filesystem",
		tools: []Tool{
			{Name: "read_file", Description: "Read a file", Schema: map[string]any{"type": "object"}},
		},
	}

	r := NewRegistry(WithMCPServer(remote))
	require.NoError(t, r.Register(Tool{Name: "add", Description: "Add two numbers", Schema: nil}, func(ctx context.Context, _ Sandbox, args map[string]any) (any, error) {
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
