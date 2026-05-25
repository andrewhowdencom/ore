package stdio

import (
	"bytes"
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

func simpleProcessor() session.TurnProcessor {
	return func(ctx context.Context, step *loop.Step, st state.State, prov provider.Provider) (state.State, error) {
		return step.Turn(ctx, st, prov)
	}
}

func newManager(prov provider.Provider) *session.Manager {
	store := thread.NewMemoryStore()
	return session.NewManager(store, prov, func(*thread.Thread) (*loop.Step, error) { return loop.New(), nil }, simpleProcessor())
}

func TestNew_NilManager(t *testing.T) {
	c, err := New(nil)
	require.Error(t, err)
	require.Nil(t, c)
}

func TestNew_Defaults(t *testing.T) {
	prov := &mockProvider{}
	mgr := newManager(prov)
	c, err := New(mgr)
	require.NoError(t, err)
	require.NotNil(t, c)

	s, ok := c.(*stdio)
	require.True(t, ok)
	require.NotNil(t, s.in)
	require.NotNil(t, s.out)
}

func TestNew_WithThreadID(t *testing.T) {
	prov := &mockProvider{}
	mgr := newManager(prov)
	c, err := New(mgr, WithThreadID("test-thread"))
	require.NoError(t, err)
	require.NotNil(t, c)

	s, ok := c.(*stdio)
	require.True(t, ok)
	require.Equal(t, "test-thread", s.threadID)
}

func TestNew_WithInput(t *testing.T) {
	prov := &mockProvider{}
	mgr := newManager(prov)
	in := bytes.NewBufferString("hello")
	c, err := New(mgr, WithInput(in))
	require.NoError(t, err)

	s, ok := c.(*stdio)
	require.True(t, ok)
	require.Equal(t, in, s.in)
}

func TestNew_WithOutput(t *testing.T) {
	prov := &mockProvider{}
	mgr := newManager(prov)
	out := &bytes.Buffer{}
	c, err := New(mgr, WithOutput(out))
	require.NoError(t, err)

	s, ok := c.(*stdio)
	require.True(t, ok)
	require.Equal(t, out, s.out)
}

func TestDescriptor(t *testing.T) {
	require.Equal(t, "stdio", Descriptor.Name)
	require.NotEmpty(t, Descriptor.Description)
	require.NotEmpty(t, Descriptor.Capabilities)
}
