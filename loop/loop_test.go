package loop

import (
	"context"
	"errors"
	"testing"

	"github.com/andrewhowdencom/tack/artifact"
	"github.com/andrewhowdencom/tack/provider"
	"github.com/andrewhowdencom/tack/state"
	"github.com/andrewhowdencom/tack/surface"
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

// mockStreamingProvider implements provider.StreamingProvider for testing.
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

// mockSurface implements surface.Surface for testing.
type mockSurface struct {
	deltas   []artifact.Artifact
	turns    []state.Turn
	statuses []string
}

func (m *mockSurface) Events() <-chan surface.Event {
	return nil
}

func (m *mockSurface) RenderDelta(ctx context.Context, delta artifact.Artifact) error {
	m.deltas = append(m.deltas, delta)
	return nil
}

func (m *mockSurface) RenderTurn(ctx context.Context, turn state.Turn) error {
	m.turns = append(m.turns, turn)
	return nil
}

func (m *mockSurface) SetStatus(ctx context.Context, status string) error {
	m.statuses = append(m.statuses, status)
	return nil
}

// Compile-time interface check.
var _ surface.Surface = (*mockSurface)(nil)

// mockHandler implements Handler for testing.
type mockHandler struct {
	called []artifact.Artifact
	err    error
	fn     func(ctx context.Context, art artifact.Artifact, s state.State) error
}

func (m *mockHandler) Handle(ctx context.Context, art artifact.Artifact, s state.State) error {
	m.called = append(m.called, art)
	if m.fn != nil {
		return m.fn(ctx, art, s)
	}
	return m.err
}

// Compile-time interface check.
var _ Handler = (*mockHandler)(nil)

func TestStep_Turn_AppendsArtifacts(t *testing.T) {
	s := New()
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	mock := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "world"},
			artifact.ToolCall{Name: "test"},
		},
	}

	result, err := s.Turn(context.Background(), mem, mock)
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

func TestStep_Turn_PropagatesError(t *testing.T) {
	s := New()
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	wantErr := errors.New("provider failed")
	mock := &mockProvider{err: wantErr}

	_, err := s.Turn(context.Background(), mem, mock)
	require.ErrorIs(t, err, wantErr)

	// State should not be mutated on error.
	assert.Len(t, mem.Turns(), 1)
}

func TestStep_Turn_AppendsReasoningArtifact(t *testing.T) {
	s := New()
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	mock := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "world"},
			artifact.Reasoning{Content: "Let me think..."},
		},
	}

	result, err := s.Turn(context.Background(), mem, mock)
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

func TestStep_Turn_EmptyArtifacts(t *testing.T) {
	s := New()
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	mock := &mockProvider{
		artifacts: []artifact.Artifact{},
	}

	_, err := s.Turn(context.Background(), mem, mock)
	require.NoError(t, err)

	turns := mem.Turns()
	require.Len(t, turns, 2)

	last := turns[1]
	assert.Equal(t, state.RoleAssistant, last.Role)
	assert.Empty(t, last.Artifacts)
}

func TestStep_Turn_Streaming(t *testing.T) {
	surf := &mockSurface{}
	s := New(WithSurface(surf))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	prov := &mockStreamingProvider{
		deltas: []artifact.Artifact{
			artifact.TextDelta{Content: "wor"},
			artifact.TextDelta{Content: "ld"},
		},
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "world!"},
		},
	}

	result, err := s.Turn(context.Background(), mem, prov)
	require.NoError(t, err)
	assert.Same(t, mem, result)

	// Verify deltas were rendered.
	require.Len(t, surf.deltas, 2)
	assert.Equal(t, "text_delta", surf.deltas[0].Kind())
	assert.Equal(t, "wor", surf.deltas[0].(artifact.TextDelta).Content)
	assert.Equal(t, "text_delta", surf.deltas[1].Kind())
	assert.Equal(t, "ld", surf.deltas[1].(artifact.TextDelta).Content)

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

func TestStep_Turn_ProviderErrorWithSurface(t *testing.T) {
	surf := &mockSurface{}
	s := New(WithSurface(surf))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	wantErr := context.Canceled
	prov := &mockStreamingProvider{err: wantErr}

	_, err := s.Turn(context.Background(), mem, prov)
	require.ErrorIs(t, err, wantErr)
}

func TestStep_Turn_NonStreamingProviderWithSurface(t *testing.T) {
	surf := &mockSurface{}
	s := New(WithSurface(surf))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	// Regular provider (not StreamingProvider).
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "no-stream"},
		},
	}

	result, err := s.Turn(context.Background(), mem, prov)
	require.NoError(t, err)
	assert.Same(t, mem, result)

	// No deltas should have been emitted because provider doesn't stream.
	assert.Len(t, surf.deltas, 0)

	turns := mem.Turns()
	require.Len(t, turns, 2)
	last := turns[1]
	require.Len(t, last.Artifacts, 1)
	text, ok := last.Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "no-stream", text.Content)
}

func TestStep_Turn_NoSurfaceStreamingProvider(t *testing.T) {
	s := New()
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	prov := &mockStreamingProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "fallback"},
		},
	}

	result, err := s.Turn(context.Background(), mem, prov)
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

func TestStep_Turn_Handler(t *testing.T) {
	surf := &mockSurface{}
	h := &mockHandler{}
	s := New(WithSurface(surf), WithHandlers(h))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	prov := &mockStreamingProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "world"},
			artifact.ToolCall{Name: "test", Arguments: "{}"},
		},
	}

	_, err := s.Turn(context.Background(), mem, prov)
	require.NoError(t, err)

	require.Len(t, h.called, 2)
	assert.Equal(t, "text", h.called[0].Kind())
	assert.Equal(t, "tool_call", h.called[1].Kind())
}

func TestStep_Turn_HandlerAppendsToolResult(t *testing.T) {
	surf := &mockSurface{}
	h := &mockHandler{
		fn: func(ctx context.Context, art artifact.Artifact, s state.State) error {
			if art.Kind() == "tool_call" {
				s.Append(state.RoleTool, artifact.Text{Content: "tool result"})
			}
			return nil
		},
	}
	s := New(WithSurface(surf), WithHandlers(h))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	prov := &mockStreamingProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "calling tool"},
			artifact.ToolCall{Name: "test", Arguments: "{}"},
		},
	}

	result, err := s.Turn(context.Background(), mem, prov)
	require.NoError(t, err)

	// Should have: User, Assistant, Tool
	turns := result.Turns()
	require.Len(t, turns, 3)
	assert.Equal(t, state.RoleUser, turns[0].Role)
	assert.Equal(t, state.RoleAssistant, turns[1].Role)
	assert.Equal(t, state.RoleTool, turns[2].Role)
}

func TestStep_Turn_HandlerError(t *testing.T) {
	surf := &mockSurface{}
	wantErr := context.Canceled
	h := &mockHandler{err: wantErr}
	s := New(WithSurface(surf), WithHandlers(h))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	prov := &mockStreamingProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "world"},
		},
	}

	_, err := s.Turn(context.Background(), mem, prov)
	require.ErrorIs(t, err, wantErr)
}

func TestStep_Turn_StreamingAndHandler(t *testing.T) {
	surf := &mockSurface{}
	h := &mockHandler{}
	s := New(WithSurface(surf), WithHandlers(h))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	prov := &mockStreamingProvider{
		deltas: []artifact.Artifact{
			artifact.TextDelta{Content: "wor"},
			artifact.TextDelta{Content: "ld"},
		},
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "world!"},
			artifact.ToolCall{Name: "test", Arguments: "{}"},
		},
	}

	result, err := s.Turn(context.Background(), mem, prov)
	require.NoError(t, err)
	assert.Same(t, mem, result)

	// Verify deltas were rendered.
	require.Len(t, surf.deltas, 2)
	assert.Equal(t, "text_delta", surf.deltas[0].Kind())
	assert.Equal(t, "wor", surf.deltas[0].(artifact.TextDelta).Content)
	assert.Equal(t, "text_delta", surf.deltas[1].Kind())
	assert.Equal(t, "ld", surf.deltas[1].(artifact.TextDelta).Content)

	// Verify complete artifacts appended.
	turns := mem.Turns()
	require.Len(t, turns, 2)
	last := turns[1]
	assert.Equal(t, state.RoleAssistant, last.Role)
	require.Len(t, last.Artifacts, 2)

	// Verify handler was called for both artifacts.
	require.Len(t, h.called, 2)
	assert.Equal(t, "text", h.called[0].Kind())
	assert.Equal(t, "tool_call", h.called[1].Kind())
}
