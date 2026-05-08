package core

import (
	"context"
	"errors"
	"testing"

	"github.com/andrewhowdencom/tack/artifact"
	"github.com/andrewhowdencom/tack/provider"
	"github.com/andrewhowdencom/tack/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProvider is a test double implementing provider.Provider.
type mockProvider struct {
	artifacts []artifact.Artifact
	err       error
}

func (m *mockProvider) Invoke(ctx context.Context, s state.State) ([]artifact.Artifact, error) {
	return m.artifacts, m.err
}

// Compile-time interface check.
var _ provider.Provider = (*mockProvider)(nil)

func TestLoop_Turn_AppendsArtifacts(t *testing.T) {
	loop := &Loop{}
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	mock := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "world"},
			artifact.ToolCall{Name: "test"},
		},
	}

	result, err := loop.Turn(context.Background(), mem, mock)
	require.NoError(t, err)

	// Since state is mutable, result should be the same pointer.
	assert.Same(t, mem, result)

	turns := mem.Turns()
	require.Len(t, turns, 2)

	last := turns[1]
	assert.Equal(t, state.RoleAssistant, last.Role)
	require.Len(t, last.Artifacts, 2)
	assert.Equal(t, "text", last.Artifacts[0].Kind())
	assert.Equal(t, "tool_call", last.Artifacts[1].Kind())
}

func TestLoop_Turn_PropagatesError(t *testing.T) {
	loop := &Loop{}
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	wantErr := errors.New("provider failed")
	mock := &mockProvider{err: wantErr}

	_, err := loop.Turn(context.Background(), mem, mock)
	require.ErrorIs(t, err, wantErr)

	// State should not be mutated on error.
	assert.Len(t, mem.Turns(), 1)
}

func TestLoop_Turn_EmptyArtifacts(t *testing.T) {
	loop := &Loop{}
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	mock := &mockProvider{
		artifacts: []artifact.Artifact{},
	}

	_, err := loop.Turn(context.Background(), mem, mock)
	require.NoError(t, err)

	turns := mem.Turns()
	require.Len(t, turns, 2)

	last := turns[1]
	assert.Equal(t, state.RoleAssistant, last.Role)
	assert.Empty(t, last.Artifacts)
}
