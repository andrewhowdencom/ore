package tool

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRegistry_RegisterAndHandler(t *testing.T) {
	r := NewRegistry()
	r.Register("test", func(ctx context.Context, args map[string]any) (any, error) {
		return "ok", nil
	})

	h := r.Handler()
	assert.NotNil(t, h)
	assert.NotNil(t, h.registry)
	assert.Equal(t, r, h.registry)
}

func TestRegistry_Register_Overwrite(t *testing.T) {
	r := NewRegistry()
	r.Register("test", func(ctx context.Context, args map[string]any) (any, error) {
		return "first", nil
	})
	r.Register("test", func(ctx context.Context, args map[string]any) (any, error) {
		return "second", nil
	})

	fn := r.tools["test"]
	result, err := fn(nil, nil)
	assert.NoError(t, err)
	assert.Equal(t, "second", result)
}

func TestRegistry_ConcurrentRegistration(t *testing.T) {
	r := NewRegistry()
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			name := fmt.Sprintf("tool-%d", n)
			r.Register(name, func(ctx context.Context, args map[string]any) (any, error) {
				return n, nil
			})
		}(i)
	}

	wg.Wait()

	// Verify all tools were registered.
	for i := 0; i < 100; i++ {
		name := fmt.Sprintf("tool-%d", i)
		fn, ok := r.tools[name]
		assert.True(t, ok, "tool %s should be registered", name)
		result, err := fn(nil, nil)
		assert.NoError(t, err)
		assert.Equal(t, i, result)
	}
}
