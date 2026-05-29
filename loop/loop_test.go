package loop

import (
	"context"
	"errors"
	"fmt"
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

// contextCancellingProvider is a test double that cancels context after
// emitting one artifact, simulating an in-flight cancellation.
type contextCancellingProvider struct {
	cancel context.CancelFunc
}

func (p *contextCancellingProvider) Invoke(ctx context.Context, s state.State, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	select {
	case ch <- artifact.TextDelta{Content: "partial"}:
	case <-ctx.Done():
		return ctx.Err()
	}
	p.cancel()
	<-ctx.Done()
	return ctx.Err()
}

// Compile-time interface check.
var _ provider.Provider = (*contextCancellingProvider)(nil)

// mockHandler implements Handler for testing.
type mockHandler struct {
	called []artifact.Artifact
	err    error
	fn     func(ctx context.Context, art artifact.Artifact, e Emitter) error
}

func (m *mockHandler) Handle(ctx context.Context, art artifact.Artifact, e Emitter) error {
	m.called = append(m.called, art)
	if m.fn != nil {
		return m.fn(ctx, art, e)
	}
	return m.err
}

// stepWithState creates a Step that wires an OnEmit callback appending
// TurnCompleteEvent to the given state, plus any additional options.
func stepWithState(st state.State, opts ...Option) *Step {
	opts = append(opts, WithOnEmit(func(ctx context.Context, event OutputEvent) {
		if tc, ok := event.(TurnCompleteEvent); ok {
			st.Append(tc.Turn.Role, tc.Turn.Artifacts...)
		}
	}))
	return New(opts...)
}

// mockTransform implements Transform for testing.
type mockTransform struct {
	fn func(ctx context.Context, s state.State) (state.State, error)
}

func (m *mockTransform) Transform(ctx context.Context, s state.State) (state.State, error) {
	return m.fn(ctx, s)
}

// Compile-time interface check.
var _ Handler = (*mockHandler)(nil)
var _ Transform = (*mockTransform)(nil)

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
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	s := stepWithState(mem)
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
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	s := stepWithState(mem)
	wantErr := errors.New("provider failed")
	mock := &mockProvider{err: wantErr}

	_, err := s.Turn(context.Background(), mem, mock)
	require.ErrorIs(t, err, wantErr)

	assert.Len(t, mem.Turns(), 1)
}

func TestStep_Turn_AppendsReasoningArtifact(t *testing.T) {
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	s := stepWithState(mem)
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
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	s := stepWithState(mem)
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

func TestStep_Turn_Transform_Composition(t *testing.T) {
	var order []int
	tr1 := &mockTransform{
		fn: func(ctx context.Context, s state.State) (state.State, error) {
			order = append(order, 1)
			return s, nil
		},
	}
	tr2 := &mockTransform{
		fn: func(ctx context.Context, s state.State) (state.State, error) {
			order = append(order, 2)
			return s, nil
		},
	}
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	s := stepWithState(mem, WithTransforms(tr1, tr2))
	mock := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "world"},
		},
	}

	_, err := s.Turn(context.Background(), mem, mock)
	require.NoError(t, err)
	assert.Equal(t, []int{1, 2}, order)
}

func TestStep_Turn_Transform_ErrorAborts(t *testing.T) {
	wantErr := errors.New("transform failed")
	tr := &mockTransform{
		fn: func(ctx context.Context, s state.State) (state.State, error) {
			return s, wantErr
		},
	}
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	s := stepWithState(mem, WithTransforms(tr))
	mock := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "world"},
		},
	}

	_, err := s.Turn(context.Background(), mem, mock)
	require.ErrorIs(t, err, wantErr)
	assert.Contains(t, err.Error(), "transform failed")

	// State should not be mutated.
	assert.Len(t, mem.Turns(), 1)
}

func TestStep_Turn_Transform_Identity(t *testing.T) {
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	s := stepWithState(mem)
	mock := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "world"},
		},
	}

	result, err := s.Turn(context.Background(), mem, mock)
	require.NoError(t, err)
	assert.Same(t, mem, result)

	turns := mem.Turns()
	require.Len(t, turns, 2)
	assert.Equal(t, state.RoleAssistant, turns[1].Role)
}

func TestStep_Submit_DoesNotRunTransforms(t *testing.T) {
	var transformCalled bool
	tr := &mockTransform{
		fn: func(ctx context.Context, s state.State) (state.State, error) {
			transformCalled = true
			return s, nil
		},
	}
	mem := &state.Buffer{}

	s := stepWithState(mem, WithTransforms(tr))
	_, err := s.Submit(context.Background(), mem, state.RoleUser, artifact.Text{Content: "hello"})
	require.NoError(t, err)
	assert.False(t, transformCalled, "transforms must not run during Submit")

	mem.Append(state.RoleUser, artifact.Text{Content: "turn"})
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "world"},
		},
	}
	_, err = s.Turn(context.Background(), mem, prov)
	require.NoError(t, err)
	assert.True(t, transformCalled, "transforms must run during Turn")
}

func TestStep_Turn_Transform_VirtualTurnStateChaining(t *testing.T) {
	var seenTurns []state.Turn
	tr1 := &mockTransform{
		fn: func(ctx context.Context, s state.State) (state.State, error) {
			return state.NewVirtualTurnState(s, []state.Turn{
				{Role: state.RoleSystem, Artifacts: []artifact.Artifact{artifact.Text{Content: "system"}}},
			}), nil
		},
	}
	tr2 := &mockTransform{
		fn: func(ctx context.Context, s state.State) (state.State, error) {
			seenTurns = s.Turns()
			return s, nil
		},
	}
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	s := stepWithState(mem, WithTransforms(tr1, tr2))
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "world"},
		},
	}
	_, err := s.Turn(context.Background(), mem, prov)
	require.NoError(t, err)

	require.Len(t, seenTurns, 2)
	assert.Equal(t, state.RoleSystem, seenTurns[0].Role)
	assert.Equal(t, state.RoleUser, seenTurns[1].Role)
}

func TestStep_Turn_Handler(t *testing.T) {
	h := &mockHandler{}
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	s := stepWithState(mem, WithHandlers(h))
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
		fn: func(ctx context.Context, art artifact.Artifact, e Emitter) error {
			if art.Kind() == "tool_call" {
				e.Emit(ctx, TurnCompleteEvent{
					Turn: state.Turn{
						Role:      state.RoleTool,
						Artifacts: []artifact.Artifact{artifact.Text{Content: "tool result"}},
					},
				})
			}
			return nil
		},
	}
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	s := stepWithState(mem, WithHandlers(h))
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
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	s := stepWithState(mem, WithHandlers(h))
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "world"},
		},
	}

	_, err := s.Turn(context.Background(), mem, prov)
	require.ErrorIs(t, err, wantErr)
}

func TestStep_Turn_UsageArtifact(t *testing.T) {
	var capturedUsage *artifact.Usage
	h := &mockHandler{
		fn: func(ctx context.Context, art artifact.Artifact, e Emitter) error {
			if u, ok := art.(artifact.Usage); ok {
				capturedUsage = &u
			}
			return nil
		},
	}

	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	s := stepWithState(mem, WithHandlers(h))
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
		fn: func(ctx context.Context, art artifact.Artifact, e Emitter) error {
			if art.Kind() == "text" {
				return nil
			}
			return errors.New("handler failed on second artifact")
		},
	}
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	s := stepWithState(mem, WithHandlers(h))
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
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	s := stepWithState(mem)
	ch := s.Subscribe("text_delta", "text", "turn_complete")
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

	require.Len(t, events, 5)
	assert.Equal(t, "text_delta", events[0].Kind())
	assert.Equal(t, "wor", events[0].(ArtifactEvent).Artifact.(artifact.TextDelta).Content)
	assert.Equal(t, "text_delta", events[1].Kind())
	assert.Equal(t, "ld", events[1].(ArtifactEvent).Artifact.(artifact.TextDelta).Content)
	assert.Equal(t, "text", events[2].Kind())
	ae, ok := events[2].(ArtifactEvent)
	require.True(t, ok)
	text, ok := ae.Artifact.(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "world", text.Content)
	assert.Equal(t, "text", events[3].Kind())
	ae, ok = events[3].(ArtifactEvent)
	require.True(t, ok)
	text, ok = ae.Artifact.(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "world!", text.Content)
	assert.Equal(t, "turn_complete", events[4].Kind())
	assert.Equal(t, state.RoleAssistant, events[4].(TurnCompleteEvent).Turn.Role)
	// Deltas are accumulated into ordered blocks: Text{"wor"} merges with
	// Text{"ld"} into Text{"world"}, then Text{"world!"} starts a new block.
	require.Len(t, events[4].(TurnCompleteEvent).Turn.Artifacts, 2)
	text, ok = events[4].(TurnCompleteEvent).Turn.Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "world", text.Content)
	text, ok = events[4].(TurnCompleteEvent).Turn.Artifacts[1].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "world!", text.Content)
}

func TestStep_Turn_AccumulatesInterleavedDeltas(t *testing.T) {
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	s := stepWithState(mem)
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Hello"},
			artifact.ReasoningDelta{Content: "think"},
			artifact.TextDelta{Content: " world"},
		},
	}

	result, err := s.Turn(context.Background(), mem, prov)
	require.NoError(t, err)
	assert.Same(t, mem, result)

	turns := mem.Turns()
	require.Len(t, turns, 2)

	last := turns[1]
	assert.Equal(t, state.RoleAssistant, last.Role)
	require.Len(t, last.Artifacts, 2)

	text, ok := last.Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "Hello world", text.Content)

	reasoning, ok := last.Artifacts[1].(artifact.Reasoning)
	require.True(t, ok)
	assert.Equal(t, "think", reasoning.Content)
}

func TestStep_Turn_AccumulatesAdjacentDeltas(t *testing.T) {
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	s := stepWithState(mem)
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Hello"},
			artifact.TextDelta{Content: " world"},
			artifact.ReasoningDelta{Content: "think"},
			artifact.ReasoningDelta{Content: "...done"},
		},
	}

	result, err := s.Turn(context.Background(), mem, prov)
	require.NoError(t, err)
	assert.Same(t, mem, result)

	turns := mem.Turns()
	require.Len(t, turns, 2)

	last := turns[1]
	assert.Equal(t, state.RoleAssistant, last.Role)
	require.Len(t, last.Artifacts, 2)

	text, ok := last.Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "Hello world", text.Content)

	reasoning, ok := last.Artifacts[1].(artifact.Reasoning)
	require.True(t, ok)
	assert.Equal(t, "think...done", reasoning.Content)
}

func TestStep_Turn_AccumulatesInterleavedToolCalls(t *testing.T) {
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	s := stepWithState(mem)
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.ToolCallDelta{Index: 0, ID: "call_1", Name: "search", Arguments: "query"},
			artifact.ToolCallDelta{Index: 1, ID: "call_2", Name: "calc", Arguments: "1+"},
			artifact.ToolCallDelta{Index: 0, Arguments: "=test"},
			artifact.ToolCallDelta{Index: 1, Arguments: "1"},
		},
	}

	result, err := s.Turn(context.Background(), mem, prov)
	require.NoError(t, err)
	assert.Same(t, mem, result)

	turns := mem.Turns()
	require.Len(t, turns, 2)

	last := turns[1]
	assert.Equal(t, state.RoleAssistant, last.Role)
	require.Len(t, last.Artifacts, 2)

	tc0, ok := last.Artifacts[0].(artifact.ToolCall)
	require.True(t, ok)
	assert.Equal(t, "call_1", tc0.ID)
	assert.Equal(t, "search", tc0.Name)
	assert.Equal(t, "query=test", tc0.Arguments)

	tc1, ok := last.Artifacts[1].(artifact.ToolCall)
	require.True(t, ok)
	assert.Equal(t, "call_2", tc1.ID)
	assert.Equal(t, "calc", tc1.Name)
	assert.Equal(t, "1+1", tc1.Arguments)
}

func TestStep_Turn_MultiKeyAccumulationOrder(t *testing.T) {
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	s := stepWithState(mem)
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Hello"},
			artifact.ToolCallDelta{Index: 0, ID: "call_1", Name: "search", Arguments: "q"},
			artifact.ReasoningDelta{Content: "think"},
			artifact.TextDelta{Content: " world"},
			artifact.ToolCallDelta{Index: 1, ID: "call_2", Name: "calc", Arguments: "1+"},
			artifact.ToolCallDelta{Index: 0, Arguments: "uery"},
			artifact.ToolCallDelta{Index: 1, Arguments: "1"},
			artifact.ReasoningDelta{Content: " done"},
		},
	}

	result, err := s.Turn(context.Background(), mem, prov)
	require.NoError(t, err)

	turns := result.Turns()
	last := turns[1]
	assert.Equal(t, state.RoleAssistant, last.Role)
	require.Len(t, last.Artifacts, 4)

	// Keys in insertion order: text, tool_call:0, reasoning, tool_call:1
	text, ok := last.Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "Hello world", text.Content)

	tc0, ok := last.Artifacts[1].(artifact.ToolCall)
	require.True(t, ok)
	assert.Equal(t, "call_1", tc0.ID)
	assert.Equal(t, "search", tc0.Name)
	assert.Equal(t, "query", tc0.Arguments)

	reasoning, ok := last.Artifacts[2].(artifact.Reasoning)
	require.True(t, ok)
	assert.Equal(t, "think done", reasoning.Content)

	tc1, ok := last.Artifacts[3].(artifact.ToolCall)
	require.True(t, ok)
	assert.Equal(t, "call_2", tc1.ID)
	assert.Equal(t, "calc", tc1.Name)
	assert.Equal(t, "1+1", tc1.Arguments)
}

func TestStep_Turn_OutputEventsWithHandler(t *testing.T) {
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	s := stepWithState(mem, WithHandlers(&mockHandler{
		fn: func(ctx context.Context, art artifact.Artifact, e Emitter) error {
			if art.Kind() == "tool_call" {
				e.Emit(ctx, TurnCompleteEvent{
					Turn: state.Turn{
						Role:      state.RoleTool,
						Artifacts: []artifact.Artifact{artifact.Text{Content: "tool result"}},
					},
				})
			}
			return nil
		},
	}))
	ch := s.Subscribe("turn_complete")
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "calling tool"},
			artifact.ToolCall{Name: "test", Arguments: "{}"},
		},
	}

	result, err := s.Turn(context.Background(), mem, prov)
	require.NoError(t, err)

	events := collectEvents(ch, 100*time.Millisecond)

	// Both the assistant turn and the tool turn should emit TurnCompleteEvents.
	require.Len(t, events, 2)
	assert.Equal(t, state.RoleAssistant, events[0].(TurnCompleteEvent).Turn.Role)
	assert.Equal(t, state.RoleTool, events[1].(TurnCompleteEvent).Turn.Role)

	// State should have User, Assistant, Tool.
	turns := result.Turns()
	require.Len(t, turns, 3)
	assert.Equal(t, state.RoleUser, turns[0].Role)
	assert.Equal(t, state.RoleAssistant, turns[1].Role)
	assert.Equal(t, state.RoleTool, turns[2].Role)
}

func TestStep_Turn_OutputEvents_OnlyCompletes(t *testing.T) {
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	s := stepWithState(mem)
	ch := s.Subscribe("text_delta", "turn_complete")
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
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	s := stepWithState(mem)
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
	// Deltas are still accumulated into state alongside complete artifacts.
	turns := mem.Turns()
	require.Len(t, turns, 2)
	last := turns[1]
	assert.Equal(t, state.RoleAssistant, last.Role)
	require.Len(t, last.Artifacts, 2)
	text, ok := last.Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "wor", text.Content)
	text, ok = last.Artifacts[1].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "world!", text.Content)
}

func TestStep_Turn_CompleteArtifactEvent(t *testing.T) {
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	s := stepWithState(mem)
	ch := s.Subscribe("text", "turn_complete")
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
	ae, ok := events[0].(ArtifactEvent)
	require.True(t, ok)
	text, ok := ae.Artifact.(artifact.Text)
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
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	s := stepWithState(mem)
	ch := s.Subscribe("text", "error")
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
	ae, ok := events[0].(ArtifactEvent)
	require.True(t, ok)
	text, ok := ae.Artifact.(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "partial", text.Content)
	assert.Equal(t, "error", events[1].Kind())
	assert.Equal(t, wantErr, events[1].(ErrorEvent).Err)

	// State should not be mutated.
	assert.Len(t, mem.Turns(), 1)
}

func TestStep_Turn_ErrorEvent(t *testing.T) {
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	s := stepWithState(mem)
	ch := s.Subscribe("error")
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

func TestStep_Turn_ContextCancellationMidAccumulation(t *testing.T) {
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	s := stepWithState(mem)
	ctx, cancel := context.WithCancel(context.Background())
	prov := &contextCancellingProvider{cancel: cancel}

	_, err := s.Turn(ctx, mem, prov)
	require.ErrorIs(t, err, context.Canceled)

	// State should not be mutated.
	assert.Len(t, mem.Turns(), 1)
}

func TestProcessCompleteEvent_Kind(t *testing.T) {
	event := ProcessCompleteEvent{Err: nil, Ctx: EventContext{Provenance: "test"}}
	assert.Equal(t, "process_complete", event.Kind())
	assert.Equal(t, EventContext{Provenance: "test"}, event.Context())
}

func TestProcessCompleteEvent_WithError(t *testing.T) {
	wantErr := errors.New("pipeline failed")
	event := ProcessCompleteEvent{Err: wantErr}
	assert.Equal(t, "process_complete", event.Kind())
	assert.Equal(t, wantErr, event.Err)
}

func TestStep_Submit_AppendsUserTurn(t *testing.T) {
	mem := &state.Buffer{}

	s := stepWithState(mem)
	result, err := s.Submit(context.Background(), mem, state.RoleUser, artifact.Text{Content: "hello"})
	require.NoError(t, err)
	assert.Same(t, mem, result)

	turns := mem.Turns()
	require.Len(t, turns, 1)

	last := turns[0]
	assert.Equal(t, state.RoleUser, last.Role)
	require.Len(t, last.Artifacts, 1)
	assert.Equal(t, "text", last.Artifacts[0].Kind())
	text, ok := last.Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "hello", text.Content)
}

func TestStep_Submit_EmitsTurnCompleteEvent(t *testing.T) {
	mem := &state.Buffer{}

	s := stepWithState(mem)
	ch := s.Subscribe("turn_complete")
	_, err := s.Submit(context.Background(), mem, state.RoleUser, artifact.Text{Content: "hello"})
	require.NoError(t, err)

	events := collectEvents(ch, 100*time.Millisecond)

	require.Len(t, events, 1)
	assert.Equal(t, "turn_complete", events[0].Kind())
	turnComplete, ok := events[0].(TurnCompleteEvent)
	require.True(t, ok)
	assert.Equal(t, state.RoleUser, turnComplete.Turn.Role)
	require.Len(t, turnComplete.Turn.Artifacts, 1)
	text, ok := turnComplete.Turn.Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "hello", text.Content)
}

func TestStep_Submit_Handler(t *testing.T) {
	h := &mockHandler{}
	mem := &state.Buffer{}

	s := stepWithState(mem, WithHandlers(h))
	_, err := s.Submit(context.Background(), mem, state.RoleUser, artifact.Text{Content: "hello"}, artifact.ToolCall{Name: "test"})
	require.NoError(t, err)

	require.Len(t, h.called, 2)
	assert.Equal(t, "text", h.called[0].Kind())
	assert.Equal(t, "tool_call", h.called[1].Kind())
}

func TestStep_Submit_HandlerError(t *testing.T) {
	wantErr := context.Canceled
	h := &mockHandler{err: wantErr}
	mem := &state.Buffer{}

	s := stepWithState(mem, WithHandlers(h))
	_, err := s.Submit(context.Background(), mem, state.RoleUser, artifact.Text{Content: "hello"})
	require.ErrorIs(t, err, wantErr)
}

func TestStep_Submit_MultipleInOrder(t *testing.T) {
	mem := &state.Buffer{}

	s := stepWithState(mem)
	_, err := s.Submit(context.Background(), mem, state.RoleUser, artifact.Text{Content: "first"})
	require.NoError(t, err)

	_, err = s.Submit(context.Background(), mem, state.RoleSystem, artifact.Text{Content: "second"})
	require.NoError(t, err)

	turns := mem.Turns()
	require.Len(t, turns, 2)
	assert.Equal(t, state.RoleUser, turns[0].Role)
	assert.Equal(t, state.RoleSystem, turns[1].Role)

	text, ok := turns[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "first", text.Content)

	text, ok = turns[1].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "second", text.Content)
}

func TestStep_Submit_ThenTurn_EventsInOrder(t *testing.T) {
	mem := &state.Buffer{}

	s := stepWithState(mem)
	ch := s.Subscribe("turn_complete")
	_, err := s.Submit(context.Background(), mem, state.RoleUser, artifact.Text{Content: "hello"})
	require.NoError(t, err)

	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "world"},
		},
	}

	_, err = s.Turn(context.Background(), mem, prov)
	require.NoError(t, err)

	events := collectEvents(ch, 100*time.Millisecond)

	require.Len(t, events, 2)
	assert.Equal(t, "turn_complete", events[0].Kind())
	assert.Equal(t, state.RoleUser, events[0].(TurnCompleteEvent).Turn.Role)
	assert.Equal(t, "turn_complete", events[1].Kind())
	assert.Equal(t, state.RoleAssistant, events[1].(TurnCompleteEvent).Turn.Role)
}

func TestStep_Submit_EmptyArtifacts(t *testing.T) {
	mem := &state.Buffer{}

	s := stepWithState(mem)
	_, err := s.Submit(context.Background(), mem, state.RoleUser)
	require.NoError(t, err)

	turns := mem.Turns()
	require.Len(t, turns, 1)
	last := turns[0]
	assert.Equal(t, state.RoleUser, last.Role)
	assert.Empty(t, last.Artifacts)
}

func TestStep_Submit_ToolRole(t *testing.T) {
	mem := &state.Buffer{}

	s := stepWithState(mem)
	result, err := s.Submit(context.Background(), mem, state.RoleTool, artifact.Text{Content: "tool result"})
	require.NoError(t, err)
	assert.Same(t, mem, result)

	turns := mem.Turns()
	require.Len(t, turns, 1)

	last := turns[0]
	assert.Equal(t, state.RoleTool, last.Role)
	require.Len(t, last.Artifacts, 1)
	assert.Equal(t, "text", last.Artifacts[0].Kind())
	text, ok := last.Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "tool result", text.Content)
}

func TestStep_Submit_HandlerErrorAfterPartialProcessing(t *testing.T) {
	h := &mockHandler{
		fn: func(ctx context.Context, art artifact.Artifact, e Emitter) error {
			if art.Kind() == "text" {
				return nil
			}
			return errors.New("handler failed on second artifact")
		},
	}
	mem := &state.Buffer{}

	s := stepWithState(mem, WithHandlers(h))
	_, err := s.Submit(context.Background(), mem, state.RoleUser, artifact.Text{Content: "hello"}, artifact.ToolCall{Name: "test"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "handler failed on second artifact")

	require.Len(t, h.called, 2)
	assert.Equal(t, "text", h.called[0].Kind())
	assert.Equal(t, "tool_call", h.called[1].Kind())

	// State should still have the turn appended (finalizeTurn appends
	// before running handlers).
	turns := mem.Turns()
	require.Len(t, turns, 1)
	assert.Equal(t, state.RoleUser, turns[0].Role)
	require.Len(t, turns[0].Artifacts, 2)
}

func TestStep_Submit_Multiple_EventsInOrder(t *testing.T) {
	mem := &state.Buffer{}

	s := stepWithState(mem)
	ch := s.Subscribe("turn_complete")
	_, err := s.Submit(context.Background(), mem, state.RoleUser, artifact.Text{Content: "first"})
	require.NoError(t, err)

	_, err = s.Submit(context.Background(), mem, state.RoleSystem, artifact.Text{Content: "second"})
	require.NoError(t, err)

	_, err = s.Submit(context.Background(), mem, state.RoleTool, artifact.Text{Content: "third"})
	require.NoError(t, err)

	events := collectEvents(ch, 100*time.Millisecond)

	require.Len(t, events, 3)
	assert.Equal(t, state.RoleUser, events[0].(TurnCompleteEvent).Turn.Role)
	assert.Equal(t, state.RoleSystem, events[1].(TurnCompleteEvent).Turn.Role)
	assert.Equal(t, state.RoleTool, events[2].(TurnCompleteEvent).Turn.Role)
}

func TestStep_Submit_EmptyArtifacts_ByRole(t *testing.T) {
	tests := []struct {
		name string
		role state.Role
	}{
		{"system", state.RoleSystem},
		{"tool", state.RoleTool},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mem := &state.Buffer{}

			s := stepWithState(mem)
			ch := s.Subscribe("turn_complete")
			_, err := s.Submit(context.Background(), mem, tt.role)
			require.NoError(t, err)

			turns := mem.Turns()
			require.Len(t, turns, 1)
			assert.Equal(t, tt.role, turns[0].Role)
			assert.Empty(t, turns[0].Artifacts)

			events := collectEvents(ch, 100*time.Millisecond)
			require.Len(t, events, 1)
			assert.Equal(t, "turn_complete", events[0].Kind())
			assert.Equal(t, tt.role, events[0].(TurnCompleteEvent).Turn.Role)
		})
	}
}

func TestStep_SetEventContext_PropagatesToSubmit(t *testing.T) {
	mem := &state.Buffer{}

	s := stepWithState(mem)
	ch := s.Subscribe("turn_complete")
	s.SetEventContext(EventContext{Provenance: "test-provenance"})
	_, err := s.Submit(context.Background(), mem, state.RoleUser, artifact.Text{Content: "hello"})
	require.NoError(t, err)

	events := collectEvents(ch, 100*time.Millisecond)

	require.Len(t, events, 1)
	tc, ok := events[0].(TurnCompleteEvent)
	require.True(t, ok)
	assert.Equal(t, "test-provenance", tc.Ctx.Provenance)
}

func TestStep_SetEventContext_PropagatesToTurn(t *testing.T) {
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	s := stepWithState(mem)
	ch := s.Subscribe("text", "turn_complete")
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "world1"},
			artifact.Text{Content: "world2"},
			artifact.Text{Content: "world3"},
		},
	}

	s.SetEventContext(EventContext{Provenance: "test-provenance"})
	_, err := s.Turn(context.Background(), mem, prov)
	require.NoError(t, err)

	events := collectEvents(ch, 100*time.Millisecond)

	require.Len(t, events, 4)
	ae, ok := events[0].(ArtifactEvent)
	require.True(t, ok)
	assert.Equal(t, "test-provenance", ae.Ctx.Provenance)
	ae, ok = events[1].(ArtifactEvent)
	require.True(t, ok)
	assert.Equal(t, "test-provenance", ae.Ctx.Provenance)
	ae, ok = events[2].(ArtifactEvent)
	require.True(t, ok)
	assert.Equal(t, "test-provenance", ae.Ctx.Provenance)

	tc, ok := events[3].(TurnCompleteEvent)
	require.True(t, ok)
	assert.Equal(t, "test-provenance", tc.Ctx.Provenance)
}

func TestStep_SetEventContext_PropagatesToError(t *testing.T) {
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	s := stepWithState(mem)
	ch := s.Subscribe("error")
	wantErr := errors.New("provider failed")
	prov := &mockProvider{err: wantErr}

	s.SetEventContext(EventContext{Provenance: "test-provenance"})
	_, err := s.Turn(context.Background(), mem, prov)
	require.ErrorIs(t, err, wantErr)

	events := collectEvents(ch, 100*time.Millisecond)

	require.Len(t, events, 1)
	ee, ok := events[0].(ErrorEvent)
	require.True(t, ok)
	assert.Equal(t, "test-provenance", ee.Ctx.Provenance)
}

func TestStep_SetEventContext_Cleared(t *testing.T) {
	mem := &state.Buffer{}

	s := stepWithState(mem)
	ch := s.Subscribe("turn_complete")
	s.SetEventContext(EventContext{Provenance: "first"})
	_, err := s.Submit(context.Background(), mem, state.RoleUser, artifact.Text{Content: "hello"})
	require.NoError(t, err)

	events := collectEvents(ch, 100*time.Millisecond)
	require.Len(t, events, 1)
	tc, ok := events[0].(TurnCompleteEvent)
	require.True(t, ok)
	assert.Equal(t, "first", tc.Ctx.Provenance)

	// Second submit with cleared context should have empty provenance
	s.SetEventContext(EventContext{})
	_, err = s.Submit(context.Background(), mem, state.RoleUser, artifact.Text{Content: "again"})
	require.NoError(t, err)

	events = collectEvents(ch, 100*time.Millisecond)
	require.Len(t, events, 1)
	tc, ok = events[0].(TurnCompleteEvent)
	require.True(t, ok)
	assert.Empty(t, tc.Ctx.Provenance)
}

func TestStep_ContextClearedOnError(t *testing.T) {
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	s := stepWithState(mem)
	ch := s.Subscribe("error", "turn_complete")
	wantErr := errors.New("provider failed")
	prov := &mockProvider{err: wantErr}

	s.SetEventContext(EventContext{Provenance: "first"})
	_, err := s.Turn(context.Background(), mem, prov)
	require.ErrorIs(t, err, wantErr)

	events := collectEvents(ch, 100*time.Millisecond)
	require.Len(t, events, 1)
	ee, ok := events[0].(ErrorEvent)
	require.True(t, ok)
	assert.Equal(t, "first", ee.Ctx.Provenance)

	// Subsequent submit without setting context should have empty provenance
	_, err = s.Submit(context.Background(), mem, state.RoleUser, artifact.Text{Content: "again"})
	require.NoError(t, err)

	events = collectEvents(ch, 100*time.Millisecond)
	require.Len(t, events, 1)
	tc, ok := events[0].(TurnCompleteEvent)
	require.True(t, ok)
	assert.Empty(t, tc.Ctx.Provenance)
}

// testCustomEvent is a test-only OutputEvent for verifying Emit() with custom events.
type testCustomEvent struct {
	Value string
	Ctx   EventContext
}

func (e testCustomEvent) Kind() string          { return "test_custom" }
func (e testCustomEvent) Context() EventContext { return e.Ctx }

func TestStep_Emit_DeliversCustomEvents(t *testing.T) {
	s := New()
	ch := s.Subscribe("test_custom")

	s.Emit(context.Background(), testCustomEvent{Value: "hello", Ctx: EventContext{Provenance: "test"}})

	events := collectEvents(ch, 100*time.Millisecond)
	require.Len(t, events, 1)
	custom, ok := events[0].(testCustomEvent)
	require.True(t, ok)
	assert.Equal(t, "hello", custom.Value)
	assert.Equal(t, "test", custom.Ctx.Provenance)
}

func TestOnEmit_MultipleCallbacks_Order(t *testing.T) {
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	var callbackOrder []string

	cb1 := func(ctx context.Context, event OutputEvent) {
		if tc, ok := event.(TurnCompleteEvent); ok {
			callbackOrder = append(callbackOrder, "cb1_before")
			mem.Append(tc.Turn.Role, tc.Turn.Artifacts...)
			callbackOrder = append(callbackOrder, "cb1_after")
		}
	}

	cb2 := func(ctx context.Context, event OutputEvent) {
		if _, ok := event.(TurnCompleteEvent); ok {
			callbackOrder = append(callbackOrder, "cb2_before")
			// Verify cb1 already ran by checking state
			turns := mem.Turns()
			if len(turns) >= 2 {
				callbackOrder = append(callbackOrder, "cb2_state_ok")
			}
			callbackOrder = append(callbackOrder, "cb2_after")
		}
	}

	s := New(WithOnEmit(cb1, cb2))

	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "world"},
		},
	}

	_, err := s.Turn(context.Background(), mem, prov)
	require.NoError(t, err)

	// Verify cb1 ran completely before cb2 started, and cb2 observed cb1's state mutation
	assert.Equal(t, []string{"cb1_before", "cb1_after", "cb2_before", "cb2_state_ok", "cb2_after"}, callbackOrder)
}

func TestEmit_NoCallbacks(t *testing.T) {
	s := New()
	ch := s.Subscribe("turn_complete")
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "world"},
		},
	}

	_, err := s.Turn(context.Background(), mem, prov)
	require.NoError(t, err)

	// Event should still be emitted to FanOut even with no OnEmit callbacks
	events := collectEvents(ch, 100*time.Millisecond)
	require.Len(t, events, 1)
	tc, ok := events[0].(TurnCompleteEvent)
	require.True(t, ok)
	assert.Equal(t, state.RoleAssistant, tc.Turn.Role)

	// State should NOT be mutated because no OnEmit callback was wired
	assert.Len(t, mem.Turns(), 1) // only the initial user turn
}

func TestOnEmit_ErrorEvent_ContextPropagation(t *testing.T) {
	var receivedCtx EventContext
	cb := func(ctx context.Context, event OutputEvent) {
		receivedCtx = event.Context()
	}

	s := New(WithOnEmit(cb))
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	wantErr := errors.New("provider failed")
	prov := &mockProvider{err: wantErr}

	s.SetEventContext(EventContext{Provenance: "test-provenance"})
	_, err := s.Turn(context.Background(), mem, prov)
	require.ErrorIs(t, err, wantErr)

	assert.Equal(t, "test-provenance", receivedCtx.Provenance)
}

func TestPropertiesEvent_Kind(t *testing.T) {
	event := PropertiesEvent{Properties: map[string]string{"key": "val"}}
	assert.Equal(t, "properties", event.Kind())
}

func TestPropertiesEvent_Context(t *testing.T) {
	event := PropertiesEvent{
		Properties: map[string]string{"key": "val"},
		Ctx:    EventContext{Provenance: "test"},
	}
	assert.Equal(t, EventContext{Provenance: "test"}, event.Context())
}

func TestPropertiesEvent_EmitAndReceive(t *testing.T) {
	s := New()
	ch := s.Subscribe("properties")

	s.Emit(context.Background(), PropertiesEvent{
		Properties: map[string]string{"thread_id": "abc-123", "state": "thinking..."},
		Ctx:    EventContext{Provenance: "test"},
	})

	events := collectEvents(ch, 100*time.Millisecond)
	require.Len(t, events, 1)
	status, ok := events[0].(PropertiesEvent)
	require.True(t, ok)
	assert.Equal(t, "abc-123", status.Properties["thread_id"])
	assert.Equal(t, "thinking...", status.Properties["state"])
	assert.Equal(t, "test", status.Ctx.Provenance)
}

func TestLifecycleEvent_Kind(t *testing.T) {
	event := LifecycleEvent{Phase: "submitted", Ctx: EventContext{Provenance: "test"}}
	assert.Equal(t, "lifecycle", event.Kind())
}

func TestLifecycleEvent_Context(t *testing.T) {
	ctx := EventContext{Provenance: "http"}
	event := LifecycleEvent{Phase: "done", Ctx: ctx}
	assert.Equal(t, ctx, event.Context())
}

func TestTurn_LifecyclePhases(t *testing.T) {
	tests := []struct {
		name      string
		artifacts []artifact.Artifact
		wantErr   error
		wantPhases []string
	}{
		{
			name:      "artifacts emit submitted streaming done",
			artifacts: []artifact.Artifact{artifact.TextDelta{Content: "Hello"}},
			wantPhases: []string{"submitted", "streaming", "done"},
		},
		{
			name:      "no artifacts emit submitted done without streaming",
			artifacts: nil,
			wantPhases: []string{"submitted", "done"},
		},
		{
			name:      "provider error emits submitted only",
			artifacts: nil,
			wantErr:   fmt.Errorf("provider failed"),
			wantPhases: []string{"submitted"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			step := New()
			lifecycleCh := step.Subscribe("lifecycle")

			prov := &mockProvider{artifacts: tt.artifacts, err: tt.wantErr}
			st := &state.Buffer{}

			_, err := step.Turn(context.Background(), st, prov)
			if tt.wantErr != nil {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			events := collectEvents(lifecycleCh, 100*time.Millisecond)
			var phases []string
			for _, e := range events {
				if le, ok := e.(LifecycleEvent); ok {
					phases = append(phases, le.Phase)
				}
			}
			assert.Equal(t, tt.wantPhases, phases)
		})
	}
}
