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

// mockStreamingProvider is a test double implementing provider.StreamingProvider.
type mockStreamingProvider struct {
	deltas    []artifact.Artifact
	artifacts []artifact.Artifact
	err       error
}

func (m *mockStreamingProvider) Invoke(ctx context.Context, s state.State) ([]artifact.Artifact, error) {
	return m.artifacts, m.err
}

func (m *mockStreamingProvider) InvokeStreaming(ctx context.Context, s state.State, deltasCh chan<- artifact.Artifact) ([]artifact.Artifact, error) {
	for _, d := range m.deltas {
		deltasCh <- d
	}
	return m.artifacts, m.err
}

// Compile-time interface check.
var _ provider.StreamingProvider = (*mockStreamingProvider)(nil)

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

func TestLoop_Turn_AppendsReasoningArtifact(t *testing.T) {
	loop := &Loop{}
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	mock := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "world"},
			artifact.Reasoning{Content: "Let me think..."},
		},
	}

	result, err := loop.Turn(context.Background(), mem, mock)
	require.NoError(t, err)
	assert.Same(t, mem, result)

	turns := mem.Turns()
	require.Len(t, turns, 2)

	last := turns[1]
	assert.Equal(t, state.RoleAssistant, last.Role)
	require.Len(t, last.Artifacts, 2)
	assert.Equal(t, "text", last.Artifacts[0].Kind())
	assert.Equal(t, "reasoning", last.Artifacts[1].Kind())
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

func TestLoop_Turn_Streaming(t *testing.T) {
	loop := &Loop{}
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)

	mock := &mockStreamingProvider{
		deltas: []artifact.Artifact{
			artifact.TextDelta{Content: "wor"},
			artifact.TextDelta{Content: "ld"},
		},
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "world!"},
		},
	}

	result, err := loop.Turn(context.Background(), mem, mock, ch)
	require.NoError(t, err)
	assert.Same(t, mem, result)

	// Verify deltas were emitted.
	require.Len(t, ch, 2)
	d1 := <-ch
	assert.Equal(t, "text_delta", d1.Kind())
	assert.Equal(t, "wor", d1.(artifact.TextDelta).Content)
	d2 := <-ch
	assert.Equal(t, "text_delta", d2.Kind())
	assert.Equal(t, "ld", d2.(artifact.TextDelta).Content)

	// Verify complete artifact appended.
	turns := mem.Turns()
	require.Len(t, turns, 2)
	last := turns[1]
	assert.Equal(t, state.RoleAssistant, last.Role)
	require.Len(t, last.Artifacts, 1)
	text, ok := last.Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "world!", text.Content)
}

func TestLoop_Turn_StreamingProviderWithNilChannel(t *testing.T) {
	loop := &Loop{}
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	mock := &mockStreamingProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "fallback"},
		},
	}

	// Pass nil explicitly via variadic — should fall back to Invoke.
	result, err := loop.Turn(context.Background(), mem, mock, nil)
	require.NoError(t, err)
	assert.Same(t, mem, result)

	turns := mem.Turns()
	require.Len(t, turns, 2)
	last := turns[1]
	require.Len(t, last.Artifacts, 1)
	text, ok := last.Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "fallback", text.Content)
}

func TestLoop_Turn_StreamingNonStreamingProvider(t *testing.T) {
	loop := &Loop{}
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	ch := make(chan artifact.Artifact, 10)

	// Regular provider (not StreamingProvider) with a delta channel.
	mock := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "no-stream"},
		},
	}

	result, err := loop.Turn(context.Background(), mem, mock, ch)
	require.NoError(t, err)
	assert.Same(t, mem, result)

	// No deltas should have been emitted because provider doesn't stream.
	assert.Len(t, ch, 0)

	turns := mem.Turns()
	require.Len(t, turns, 2)
	last := turns[1]
	require.Len(t, last.Artifacts, 1)
	text, ok := last.Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "no-stream", text.Content)
}
