package loop

import (
	"context"
	"errors"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProvider is a test double implementing provider.Provider.
type mockProvider struct {
	artifacts []artifact.Artifact
	err       error
}

func (m *mockProvider) Invoke(ctx context.Context, s state.State, opts ...provider.InvokeOption) ([]artifact.Artifact, error) {
	return m.artifacts, m.err
}

// Compile-time interface checks.
var _ provider.Provider = (*mockProvider)(nil)

// mockStreamingProvider implements provider.StreamingProvider for testing.
type mockStreamingProvider struct {
	deltas    []artifact.Artifact
	artifacts []artifact.Artifact
	err       error
}

func (m *mockStreamingProvider) Invoke(ctx context.Context, s state.State, opts ...provider.InvokeOption) ([]artifact.Artifact, error) {
	return m.artifacts, m.err
}

func (m *mockStreamingProvider) InvokeStreaming(ctx context.Context, s state.State, deltasCh chan<- artifact.Artifact, opts ...provider.InvokeOption) ([]artifact.Artifact, error) {
	for _, d := range m.deltas {
		deltasCh <- d
	}
	return m.artifacts, m.err
}

// Compile-time interface checks.
var _ provider.StreamingProvider = (*mockStreamingProvider)(nil)

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

// mockBeforeTurn implements BeforeTurn for testing.
type mockBeforeTurn struct {
	fn func(ctx context.Context, s state.State) (state.State, error)
}

func (m *mockBeforeTurn) BeforeTurn(ctx context.Context, s state.State) (state.State, error) {
	return m.fn(ctx, s)
}

// Compile-time interface check.
var _ BeforeTurn = (*mockBeforeTurn)(nil)

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
	h := &mockHandler{}
	s := New(WithHandlers(h))
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
	h := &mockHandler{
		fn: func(ctx context.Context, art artifact.Artifact, s state.State) error {
			if art.Kind() == "tool_call" {
				s.Append(state.RoleTool, artifact.Text{Content: "tool result"})
			}
			return nil
		},
	}
	s := New(WithHandlers(h))
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
	wantErr := context.Canceled
	h := &mockHandler{err: wantErr}
	s := New(WithHandlers(h))
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

func TestStep_Turn_BeforeTurn_TransformsState(t *testing.T) {
	before := &mockBeforeTurn{
		fn: func(ctx context.Context, s state.State) (state.State, error) {
			// Append a system message before the provider call.
			s.Append(state.RoleSystem, artifact.Text{Content: "system-prompt"})
			return s, nil
		},
	}
	s := New(WithBeforeTurn(before))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	mock := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "world"},
		},
	}

	_, err := s.Turn(context.Background(), mem, mock)
	require.NoError(t, err)

	turns := mem.Turns()
	require.Len(t, turns, 3)
	assert.Equal(t, state.RoleUser, turns[0].Role)
	assert.Equal(t, state.RoleSystem, turns[1].Role)
	assert.Equal(t, state.RoleAssistant, turns[2].Role)
}

func TestStep_Turn_BeforeTurn_ErrorAborts(t *testing.T) {
	wantErr := errors.New("before turn failed")
	before := &mockBeforeTurn{
		fn: func(ctx context.Context, s state.State) (state.State, error) {
			return s, wantErr
		},
	}
	s := New(WithBeforeTurn(before))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	mock := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "world"},
		},
	}

	_, err := s.Turn(context.Background(), mem, mock)
	require.ErrorIs(t, err, wantErr)

	// Provider should not have been called, so only the user turn exists.
	assert.Len(t, mem.Turns(), 1)
}

func TestStep_Turn_BeforeTurn_MultipleInOrder(t *testing.T) {
	var order []int
	before1 := &mockBeforeTurn{
		fn: func(ctx context.Context, s state.State) (state.State, error) {
			order = append(order, 1)
			return s, nil
		},
	}
	before2 := &mockBeforeTurn{
		fn: func(ctx context.Context, s state.State) (state.State, error) {
			order = append(order, 2)
			return s, nil
		},
	}
	s := New(WithBeforeTurn(before1, before2))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	mock := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "world"},
		},
	}

	_, err := s.Turn(context.Background(), mem, mock)
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2}, order)
}

func TestStep_Turn_BeforeTurnAndHandler_EndToEnd(t *testing.T) {
	before := &mockBeforeTurn{
		fn: func(ctx context.Context, s state.State) (state.State, error) {
			s.Append(state.RoleSystem, artifact.Text{Content: "system prompt"})
			return s, nil
		},
	}

	h := &mockHandler{
		fn: func(ctx context.Context, art artifact.Artifact, s state.State) error {
			if tc, ok := art.(artifact.ToolCall); ok {
				s.Append(state.RoleTool, artifact.ToolResult{
					ToolCallID: tc.ID,
					Content:    "result",
				})
			}
			return nil
		},
	}

	s := New(WithBeforeTurn(before), WithHandlers(h))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "calling tool"},
			artifact.ToolCall{ID: "call_1", Name: "test", Arguments: "{}"},
		},
	}

	result, err := s.Turn(context.Background(), mem, prov)
	require.NoError(t, err)

	turns := result.Turns()
	require.Len(t, turns, 4) // User, System (BeforeTurn), Assistant, Tool (handler)
	assert.Equal(t, state.RoleUser, turns[0].Role)
	assert.Equal(t, state.RoleSystem, turns[1].Role)
	assert.Equal(t, state.RoleAssistant, turns[2].Role)
	assert.Equal(t, state.RoleTool, turns[3].Role)

	// Verify the handler processed both artifacts.
	require.Len(t, h.called, 2)
	assert.Equal(t, "text", h.called[0].Kind())
	assert.Equal(t, "tool_call", h.called[1].Kind())

	// Verify the tool result was appended.
	last := turns[3]
	require.Len(t, last.Artifacts, 1)
	tr, ok := last.Artifacts[0].(artifact.ToolResult)
	require.True(t, ok)
	assert.Equal(t, "call_1", tr.ToolCallID)
	assert.Equal(t, "result", tr.Content)
}

func TestStep_Turn_UsageArtifact(t *testing.T) {
	var capturedUsage *artifact.Usage
	h := &mockHandler{
		fn: func(ctx context.Context, art artifact.Artifact, s state.State) error {
			if u, ok := art.(artifact.Usage); ok {
				capturedUsage = &u
			}
			return nil
		},
	}

	s := New(WithHandlers(h))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "world"},
			artifact.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		},
	}

	_, err := s.Turn(context.Background(), mem, prov)
	require.NoError(t, err)

	require.NotNil(t, capturedUsage)
	assert.Equal(t, 10, capturedUsage.PromptTokens)
	assert.Equal(t, 5, capturedUsage.CompletionTokens)
	assert.Equal(t, 15, capturedUsage.TotalTokens)
}

func TestStep_Turn_HandlerErrorAfterPartialProcessing(t *testing.T) {
	h := &mockHandler{
		fn: func(ctx context.Context, art artifact.Artifact, s state.State) error {
			if art.Kind() == "text" {
				return nil // succeed on first artifact
			}
			return errors.New("handler failed on second artifact")
		},
	}
	s := New(WithHandlers(h))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "world"},
			artifact.ToolCall{ID: "call_1", Name: "test", Arguments: "{}"},
		},
	}

	_, err := s.Turn(context.Background(), mem, prov)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "handler failed on second artifact")

	// Both artifacts were passed to the handler; the second caused the error.
	require.Len(t, h.called, 2)
	assert.Equal(t, "text", h.called[0].Kind())
	assert.Equal(t, "tool_call", h.called[1].Kind())
}

func TestStep_Turn_OutputEvents(t *testing.T) {
	outputCh := make(chan OutputEvent, 10)
	s := New(WithOutput(outputCh))
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

	// Verify output events were emitted.
	close(outputCh)
	var deltas []OutputEvent
	var turnCompletes []OutputEvent
	for event := range outputCh {
		switch event.Kind() {
		case "delta":
			deltas = append(deltas, event)
		case "turn_complete":
			turnCompletes = append(turnCompletes, event)
		}
	}

	require.Len(t, deltas, 2)
	assert.Equal(t, "wor", deltas[0].(DeltaEvent).Delta.(artifact.TextDelta).Content)
	assert.Equal(t, "ld", deltas[1].(DeltaEvent).Delta.(artifact.TextDelta).Content)

	require.Len(t, turnCompletes, 1)
	assert.Equal(t, state.RoleAssistant, turnCompletes[0].(TurnCompleteEvent).Turn.Role)
	require.Len(t, turnCompletes[0].(TurnCompleteEvent).Turn.Artifacts, 1)
	text, ok := turnCompletes[0].(TurnCompleteEvent).Turn.Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "world!", text.Content)
}

func TestStep_Turn_OutputEventsWithHandler(t *testing.T) {
	outputCh := make(chan OutputEvent, 10)
	h := &mockHandler{
		fn: func(ctx context.Context, art artifact.Artifact, s state.State) error {
			if art.Kind() == "tool_call" {
				s.Append(state.RoleTool, artifact.Text{Content: "tool result"})
			}
			return nil
		},
	}
	s := New(WithOutput(outputCh), WithHandlers(h))
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

	close(outputCh)
	var turnCompletes []OutputEvent
	for event := range outputCh {
		if event.Kind() == "turn_complete" {
			turnCompletes = append(turnCompletes, event)
		}
	}

	// Only the assistant turn should emit a TurnCompleteEvent.
	require.Len(t, turnCompletes, 1)
	assert.Equal(t, state.RoleAssistant, turnCompletes[0].(TurnCompleteEvent).Turn.Role)

	// State should have User, Assistant, Tool.
	turns := result.Turns()
	require.Len(t, turns, 3)
	assert.Equal(t, state.RoleUser, turns[0].Role)
	assert.Equal(t, state.RoleAssistant, turns[1].Role)
	assert.Equal(t, state.RoleTool, turns[2].Role)
}

func TestStep_Turn_OutputEvents_NonStreamingProvider(t *testing.T) {
	outputCh := make(chan OutputEvent, 10)
	s := New(WithOutput(outputCh))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	// Regular provider (not StreamingProvider).
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "world"},
		},
	}

	result, err := s.Turn(context.Background(), mem, prov)
	require.NoError(t, err)
	assert.Same(t, mem, result)

	close(outputCh)
	var deltas []OutputEvent
	var turnCompletes []OutputEvent
	for event := range outputCh {
		switch event.Kind() {
		case "delta":
			deltas = append(deltas, event)
		case "turn_complete":
			turnCompletes = append(turnCompletes, event)
		}
	}

	// No deltas because provider doesn't stream.
	assert.Len(t, deltas, 0)
	// One TurnCompleteEvent with the complete turn.
	require.Len(t, turnCompletes, 1)
	assert.Equal(t, state.RoleAssistant, turnCompletes[0].(TurnCompleteEvent).Turn.Role)

	turns := result.Turns()
	require.Len(t, turns, 2)
	last := turns[1]
	assert.Equal(t, state.RoleAssistant, last.Role)
	require.Len(t, last.Artifacts, 1)
	text, ok := last.Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "world", text.Content)
}

func TestStep_Turn_NoOutputEventsWithoutChannel(t *testing.T) {
	s := New()
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	prov := &mockStreamingProvider{
		deltas: []artifact.Artifact{
			artifact.TextDelta{Content: "wor"},
		},
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "world!"},
		},
	}

	result, err := s.Turn(context.Background(), mem, prov)
	require.NoError(t, err)
	assert.Same(t, mem, result)

	// No output channel configured, so no events should be emitted.
	// The streaming provider should still return artifacts correctly.
	turns := mem.Turns()
	require.Len(t, turns, 2)
	last := turns[1]
	assert.Equal(t, state.RoleAssistant, last.Role)
	require.Len(t, last.Artifacts, 1)
	text, ok := last.Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "world!", text.Content)
}
