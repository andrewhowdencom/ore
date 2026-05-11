package loop

import (
	"context"
	"errors"
	"testing"
	"time"

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

// Compile-time interface check.
var _ provider.Provider = (*mockProvider)(nil)

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

// collectEvents reads all available events from a channel until the timeout
// expires. It returns the collected events without closing the channel.
func collectEvents(ch <-chan OutputEvent, timeout time.Duration) []OutputEvent {
	var events []OutputEvent
	deadline := time.After(timeout)
	for {
		select {
		case event := <-ch:
			events = append(events, event)
		case <-deadline:
			return events
		}
	}
}

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

func TestStep_Turn_Handler(t *testing.T) {
	h := &mockHandler{}
	s := New(WithHandlers(h))
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	prov := &mockProvider{
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

	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "calling tool"},
			artifact.ToolCall{Name: "test", Arguments: "{}"},
		},
	}

	result, err := s.Turn(context.Background(), mem, prov)
	require.NoError(t, err)

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

	prov := &mockProvider{
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
	require.Len(t, turns, 4)
	assert.Equal(t, state.RoleUser, turns[0].Role)
	assert.Equal(t, state.RoleSystem, turns[1].Role)
	assert.Equal(t, state.RoleAssistant, turns[2].Role)
	assert.Equal(t, state.RoleTool, turns[3].Role)

	require.Len(t, h.called, 2)
	assert.Equal(t, "text", h.called[0].Kind())
	assert.Equal(t, "tool_call", h.called[1].Kind())

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
				return nil
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

	require.Len(t, h.called, 2)
	assert.Equal(t, "text", h.called[0].Kind())
	assert.Equal(t, "tool_call", h.called[1].Kind())
}

func TestStep_Turn_OutputEvents(t *testing.T) {
	s := New()
	ch := s.Subscribe("text_delta", "text", "turn_complete")
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "wor"},
			artifact.TextDelta{Content: "ld"},
			artifact.Text{Content: "world!"},
		},
	}

	result, err := s.Turn(context.Background(), mem, prov)
	require.NoError(t, err)
	assert.Same(t, mem, result)

	events := collectEvents(ch, 100*time.Millisecond)

	require.Len(t, events, 4)
	assert.Equal(t, "text_delta", events[0].Kind())
	assert.Equal(t, "wor", events[0].(artifact.TextDelta).Content)
	assert.Equal(t, "text_delta", events[1].Kind())
	assert.Equal(t, "ld", events[1].(artifact.TextDelta).Content)
	assert.Equal(t, "text", events[2].Kind())
	text, ok := events[2].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "world!", text.Content)
	assert.Equal(t, "turn_complete", events[3].Kind())
	assert.Equal(t, state.RoleAssistant, events[3].(TurnCompleteEvent).Turn.Role)
	require.Len(t, events[3].(TurnCompleteEvent).Turn.Artifacts, 1)
	text, ok = events[3].(TurnCompleteEvent).Turn.Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "world!", text.Content)
}

func TestStep_Turn_OutputEventsWithHandler(t *testing.T) {
	s := New(WithHandlers(&mockHandler{
		fn: func(ctx context.Context, art artifact.Artifact, s state.State) error {
			if art.Kind() == "tool_call" {
				s.Append(state.RoleTool, artifact.Text{Content: "tool result"})
			}
			return nil
		},
	}))
	ch := s.Subscribe("turn_complete")
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "calling tool"},
			artifact.ToolCall{Name: "test", Arguments: "{}"},
		},
	}

	result, err := s.Turn(context.Background(), mem, prov)
	require.NoError(t, err)

	events := collectEvents(ch, 100*time.Millisecond)

	// Only the assistant turn should emit a TurnCompleteEvent.
	require.Len(t, events, 1)
	assert.Equal(t, state.RoleAssistant, events[0].(TurnCompleteEvent).Turn.Role)

	// State should have User, Assistant, Tool.
	turns := result.Turns()
	require.Len(t, turns, 3)
	assert.Equal(t, state.RoleUser, turns[0].Role)
	assert.Equal(t, state.RoleAssistant, turns[1].Role)
	assert.Equal(t, state.RoleTool, turns[2].Role)
}

func TestStep_Turn_OutputEvents_OnlyCompletes(t *testing.T) {
	s := New()
	ch := s.Subscribe("text_delta", "turn_complete")
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "world"},
		},
	}

	result, err := s.Turn(context.Background(), mem, prov)
	require.NoError(t, err)
	assert.Same(t, mem, result)

	events := collectEvents(ch, 100*time.Millisecond)

	// No deltas because provider doesn't emit any.
	require.Len(t, events, 1)
	assert.Equal(t, "turn_complete", events[0].Kind())
	assert.Equal(t, state.RoleAssistant, events[0].(TurnCompleteEvent).Turn.Role)

	turns := result.Turns()
	require.Len(t, turns, 2)
	last := turns[1]
	assert.Equal(t, state.RoleAssistant, last.Role)
	require.Len(t, last.Artifacts, 1)
	text, ok := last.Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "world", text.Content)
}

func TestStep_Turn_DeltasDroppedWithoutSubscriber(t *testing.T) {
	s := New()
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "wor"},
			artifact.Text{Content: "world!"},
		},
	}

	result, err := s.Turn(context.Background(), mem, prov)
	require.NoError(t, err)
	assert.Same(t, mem, result)

	// No subscribers, so deltas are dropped by the FanOut.
	// Complete artifact is still appended to state.
	turns := mem.Turns()
	require.Len(t, turns, 2)
	last := turns[1]
	assert.Equal(t, state.RoleAssistant, last.Role)
	require.Len(t, last.Artifacts, 1)
	text, ok := last.Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "world!", text.Content)
}

func TestStep_Turn_CompleteArtifactEvent(t *testing.T) {
	s := New()
	ch := s.Subscribe("text", "turn_complete")
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "hello"},
		},
	}

	result, err := s.Turn(context.Background(), mem, prov)
	require.NoError(t, err)
	assert.Same(t, mem, result)

	events := collectEvents(ch, 100*time.Millisecond)

	require.Len(t, events, 2)
	assert.Equal(t, "text", events[0].Kind())
	text, ok := events[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "hello", text.Content)
	assert.Equal(t, "turn_complete", events[1].Kind())
	assert.Equal(t, state.RoleAssistant, events[1].(TurnCompleteEvent).Turn.Role)

	// Complete artifact should also be in state.
	turns := mem.Turns()
	require.Len(t, turns, 2)
	last := turns[1]
	assert.Equal(t, state.RoleAssistant, last.Role)
	require.Len(t, last.Artifacts, 1)
	text, ok = last.Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "hello", text.Content)
}

func TestStep_Turn_ErrorEmitsCompleteArtifacts(t *testing.T) {
	s := New()
	ch := s.Subscribe("text", "error")
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	wantErr := errors.New("provider failed")
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "partial"},
		},
		err: wantErr,
	}

	_, err := s.Turn(context.Background(), mem, prov)
	require.ErrorIs(t, err, wantErr)

	events := collectEvents(ch, 100*time.Millisecond)

	require.Len(t, events, 2)
	assert.Equal(t, "text", events[0].Kind())
	text, ok := events[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "partial", text.Content)
	assert.Equal(t, "error", events[1].Kind())
	assert.Equal(t, wantErr, events[1].(ErrorEvent).Err)

	// State should not be mutated.
	assert.Len(t, mem.Turns(), 1)
}

func TestStep_Turn_ErrorEvent(t *testing.T) {
	s := New()
	ch := s.Subscribe("error")
	mem := &state.Memory{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	wantErr := errors.New("provider failed")
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "partial"},
		},
		err: wantErr,
	}

	_, err := s.Turn(context.Background(), mem, prov)
	require.ErrorIs(t, err, wantErr)

	events := collectEvents(ch, 100*time.Millisecond)

	require.Len(t, events, 1)
	assert.Equal(t, "error", events[0].Kind())
	assert.Equal(t, wantErr, events[0].(ErrorEvent).Err)

	// State should not be mutated.
	assert.Len(t, mem.Turns(), 1)
}
