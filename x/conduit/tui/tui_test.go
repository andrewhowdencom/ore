package tui

import (
	"context"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/thread"
	"github.com/stretchr/testify/require"
)

// mockProvider is a provider.Provider implementation for testing.
type mockProvider struct {
	artifacts []artifact.Artifact
	err       error
}

func (m *mockProvider) Invoke(ctx context.Context, s state.State, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	for _, art := range m.artifacts {
		select {
		case ch <- art:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return m.err
}

// simpleProcessor runs a single Step.Turn with the mock provider.
func simpleProcessor() session.TurnProcessor {
	return func(ctx context.Context, step *loop.Step, st state.State, prov provider.Provider) (state.State, error) {
		return step.Turn(ctx, st, prov)
	}
}

func TestNew(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())

	c, err := New(mgr)
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestNew_Events(t *testing.T) {
	store := thread.NewMemoryStore()
	prov := &mockProvider{}
	mgr := session.NewManager(store, prov, func() *loop.Step { return loop.New() }, simpleProcessor())

	c, err := New(mgr)
	require.NoError(t, err)
	require.NotNil(t, c)
}
