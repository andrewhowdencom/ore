package http

import (
	"context"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/thread"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOptionsFromMap_Valid(t *testing.T) {
	opts, err := OptionsFromMap(map[string]any{"addr": ":0", "ui": false})
	require.NoError(t, err)

	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())
	h := newTestHandler(t, mgr, opts...)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- h.Start(ctx)
	}()

	// Give the server time to bind.
	time.Sleep(50 * time.Millisecond)

	cancel()
	select {
	case err := <-errCh:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return within 2 seconds after context cancellation")
	}
}

func TestOptionsFromMap_Empty(t *testing.T) {
	opts, err := OptionsFromMap(map[string]any{})
	require.NoError(t, err)
	require.Empty(t, opts)
}

func TestOptionsFromMap_InvalidType(t *testing.T) {
	_, err := OptionsFromMap(map[string]any{"addr": map[string]any{}})
	require.Error(t, err)
}

func TestFromConfig_Addr(t *testing.T) {
	opts := FromConfig(Config{Addr: ":9090"})
	require.Len(t, opts, 1)

	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())
	h := newTestHandler(t, mgr, opts...)

	assert.NotNil(t, h)
}

func TestFromConfig_UI(t *testing.T) {
	// UI=nil (not specified) produces no options.
	opts := FromConfig(Config{})
	require.Empty(t, opts)

	// UI=true produces no options.
	uiTrue := true
	opts = FromConfig(Config{UI: &uiTrue})
	require.Empty(t, opts)

	// UI=false produces WithoutUI option.
	uiFalse := false
	opts = FromConfig(Config{UI: &uiFalse})
	require.Len(t, opts, 1)
}

func TestFromConfig_Combined(t *testing.T) {
	uiFalse := false
	opts := FromConfig(Config{Addr: ":9090", UI: &uiFalse})
	require.Len(t, opts, 2)

	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())
	h := newTestHandler(t, mgr, opts...)

	assert.Equal(t, ":9090", h.addr)
	assert.False(t, h.withUI)
}

func TestOptionsFromMap_UnknownFields(t *testing.T) {
	opts, err := OptionsFromMap(map[string]any{"addr": ":0", "unknown_key": "ignored"})
	require.NoError(t, err)
	require.Len(t, opts, 1)

	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())
	h := newTestHandler(t, mgr, opts...)

	assert.Equal(t, ":0", h.addr)
}
