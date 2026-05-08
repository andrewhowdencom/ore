package step

import (
	"context"
	"testing"

	"github.com/andrewhowdencom/tack/artifact"
	"github.com/andrewhowdencom/tack/core"
	"github.com/andrewhowdencom/tack/provider"
	"github.com/andrewhowdencom/tack/state"
	"github.com/andrewhowdencom/tack/surface"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

var _ Handler = (*mockHandler)(nil)

func TestStep_Execute_Streaming(t *testing.T) {
	surf := &mockSurface{}
	loop := &core.Loop{}
	s := New(loop, surf)

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

	result, err := s.Execute(context.Background(), mem, prov)
	require.NoError(t, err)
	assert.Same(t, mem, result)

	// Verify deltas were rendered.
	require.Len(t, surf.deltas, 2)
	assert.Equal(t, "text_delta", surf.deltas[0].Kind())
	assert.Equal(t, "wor", surf.deltas[0].(artifact.TextDelta).Content)
	assert.Equal(t, "text_delta", surf.deltas[1].Kind())
	assert.Equal(t, "ld", surf.deltas[1].(artifact.TextDelta).Content)

	// Verify complete artifact appended.
	turns := result.Turns()
	require.Len(t, turns, 2)
	last := turns[1]
	assert.Equal(t, state.RoleAssistant, last.Role)
	require.Len(t, last.Artifacts, 1)
	text, ok := last.Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "world!", text.Content)
}

func TestStep_Execute_Handler(t *testing.T) {
	surf := &mockSurface{}
	h := &mockHandler{}
	loop := &core.Loop{}
	s := New(loop, surf, h)

	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	prov := &mockStreamingProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "world"},
			artifact.ToolCall{Name: "test", Arguments: "{}"},
		},
	}

	_, err := s.Execute(context.Background(), mem, prov)
	require.NoError(t, err)

	require.Len(t, h.called, 2)
	assert.Equal(t, "text", h.called[0].Kind())
	assert.Equal(t, "tool_call", h.called[1].Kind())
}

func TestStep_Execute_HandlerAppendsToolResult(t *testing.T) {
	surf := &mockSurface{}
	h := &mockHandler{
		fn: func(ctx context.Context, art artifact.Artifact, s state.State) error {
			if art.Kind() == "tool_call" {
				s.Append(state.RoleTool, artifact.Text{Content: "tool result"})
			}
			return nil
		},
	}
	loop := &core.Loop{}
	s := New(loop, surf, h)

	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	prov := &mockStreamingProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "calling tool"},
			artifact.ToolCall{Name: "test", Arguments: "{}"},
		},
	}

	result, err := s.Execute(context.Background(), mem, prov)
	require.NoError(t, err)

	// Should have: User, Assistant, Tool
	turns := result.Turns()
	require.Len(t, turns, 3)
	assert.Equal(t, state.RoleUser, turns[0].Role)
	assert.Equal(t, state.RoleAssistant, turns[1].Role)
	assert.Equal(t, state.RoleTool, turns[2].Role)
}

func TestStep_Execute_ProviderError(t *testing.T) {
	surf := &mockSurface{}
	loop := &core.Loop{}
	s := New(loop, surf)

	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	wantErr := context.Canceled
	prov := &mockStreamingProvider{err: wantErr}

	_, err := s.Execute(context.Background(), mem, prov)
	require.ErrorIs(t, err, wantErr)
}

func TestStep_Execute_HandlerError(t *testing.T) {
	surf := &mockSurface{}
	wantErr := context.Canceled
	h := &mockHandler{err: wantErr}
	loop := &core.Loop{}
	s := New(loop, surf, h)

	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	prov := &mockStreamingProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "world"},
		},
	}

	_, err := s.Execute(context.Background(), mem, prov)
	require.ErrorIs(t, err, wantErr)
}

func TestStep_Execute_NonStreamingProvider(t *testing.T) {
	surf := &mockSurface{}
	loop := &core.Loop{}
	s := New(loop, surf)

	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	// Regular provider (not StreamingProvider).
	prov := &mockNonStreamingProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "no-stream"},
		},
	}

	result, err := s.Execute(context.Background(), mem, prov)
	require.NoError(t, err)

	// No deltas should have been emitted.
	assert.Len(t, surf.deltas, 0)

	turns := result.Turns()
	require.Len(t, turns, 2)
	last := turns[1]
	require.Len(t, last.Artifacts, 1)
	text, ok := last.Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "no-stream", text.Content)
}

// mockNonStreamingProvider implements only provider.Provider (not StreamingProvider).
type mockNonStreamingProvider struct {
	artifacts []artifact.Artifact
}

func (m *mockNonStreamingProvider) Invoke(ctx context.Context, s state.State) ([]artifact.Artifact, error) {
	return m.artifacts, nil
}

var _ provider.Provider = (*mockNonStreamingProvider)(nil)
