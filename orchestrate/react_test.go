package orchestrate

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/andrewhowdencom/tack/artifact"
	"github.com/andrewhowdencom/tack/loop"
	"github.com/andrewhowdencom/tack/provider"
	"github.com/andrewhowdencom/tack/state"
	"github.com/andrewhowdencom/tack/surface"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockSurface is a test double for surface.Surface.
type mockSurface struct {
	eventsCh chan surface.Event
	deltas   []artifact.Artifact
	turns    []state.Turn
	statuses []string
}

func (m *mockSurface) Events() <-chan surface.Event {
	return m.eventsCh
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

// simpleProvider always returns the same artifacts.
type simpleProvider struct {
	artifacts []artifact.Artifact
}

func (p *simpleProvider) Invoke(ctx context.Context, s state.State) ([]artifact.Artifact, error) {
	return p.artifacts, nil
}

func (p *simpleProvider) InvokeStreaming(ctx context.Context, s state.State, deltasCh chan<- artifact.Artifact) ([]artifact.Artifact, error) {
	return p.Invoke(ctx, s)
}

var _ provider.StreamingProvider = (*simpleProvider)(nil)

// countingProvider returns different artifacts on successive calls.
type countingProvider struct {
	mu        sync.Mutex
	callCount int
}

func (p *countingProvider) Invoke(ctx context.Context, s state.State) ([]artifact.Artifact, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.callCount++
	switch p.callCount {
	case 1:
		return []artifact.Artifact{
			artifact.Text{Content: "calling tool"},
			artifact.ToolCall{Name: "test", Arguments: "{}"},
		}, nil
	default:
		return []artifact.Artifact{
			artifact.Text{Content: "done!"},
		}, nil
	}
}

func (p *countingProvider) InvokeStreaming(ctx context.Context, s state.State, deltasCh chan<- artifact.Artifact) ([]artifact.Artifact, error) {
	return p.Invoke(ctx, s)
}

var _ provider.StreamingProvider = (*countingProvider)(nil)

// slowProvider waits for context cancellation, signaling when it starts.
type slowProvider struct {
	once      sync.Once
	startedCh chan struct{}
}

func (p *slowProvider) Invoke(ctx context.Context, s state.State) ([]artifact.Artifact, error) {
	p.once.Do(func() { close(p.startedCh) })
	<-ctx.Done()
	return nil, ctx.Err()
}

func (p *slowProvider) InvokeStreaming(ctx context.Context, s state.State, deltasCh chan<- artifact.Artifact) ([]artifact.Artifact, error) {
	return p.Invoke(ctx, s)
}

var _ provider.StreamingProvider = (*slowProvider)(nil)

// testHandler implements loop.Handler for testing.
type testHandler struct {
	fn func(ctx context.Context, art artifact.Artifact, s state.State) error
}

func (h *testHandler) Handle(ctx context.Context, art artifact.Artifact, s state.State) error {
	if h.fn != nil {
		return h.fn(ctx, art, s)
	}
	return nil
}

var _ loop.Handler = (*testHandler)(nil)

func TestReAct_SingleTurn(t *testing.T) {
	eventsCh := make(chan surface.Event, 10)
	surf := &mockSurface{eventsCh: eventsCh}
	mem := &state.Memory{}

	s := loop.New(loop.WithSurface(surf))

	prov := &simpleProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "hello!"},
		},
	}

	r := &ReAct{
		State:    mem,
		Step:     s,
		Surface:  surf,
		Provider: prov,
	}

	var wg sync.WaitGroup
	wg.Add(1)
	var runErr error
	go func() {
		defer wg.Done()
		runErr = r.Run(context.Background())
	}()

	eventsCh <- surface.UserMessageEvent{Content: "hi"}
	close(eventsCh)

	wg.Wait()
	require.NoError(t, runErr)

	turns := mem.Turns()
	require.Len(t, turns, 2)
	assert.Equal(t, state.RoleUser, turns[0].Role)
	assert.Equal(t, state.RoleAssistant, turns[1].Role)
	assert.Equal(t, "hello!", turns[1].Artifacts[0].(artifact.Text).Content)

	require.Contains(t, surf.statuses, "thinking...")
}

func TestReAct_ToolLoop(t *testing.T) {
	eventsCh := make(chan surface.Event, 10)
	surf := &mockSurface{eventsCh: eventsCh}
	mem := &state.Memory{}

	toolHandler := &testHandler{
		fn: func(ctx context.Context, art artifact.Artifact, s state.State) error {
			if art.Kind() == "tool_call" {
				s.Append(state.RoleTool, artifact.Text{Content: "tool result"})
			}
			return nil
		},
	}

	s := loop.New(loop.WithSurface(surf), loop.WithHandlers(toolHandler))

	prov := &countingProvider{}

	r := &ReAct{
		State:    mem,
		Step:     s,
		Surface:  surf,
		Provider: prov,
	}

	var wg sync.WaitGroup
	wg.Add(1)
	var runErr error
	go func() {
		defer wg.Done()
		runErr = r.Run(context.Background())
	}()

	eventsCh <- surface.UserMessageEvent{Content: "do something"}
	close(eventsCh)

	wg.Wait()
	require.NoError(t, runErr)

	// State should have: User, Assistant (tool call), Tool, Assistant (final).
	turns := mem.Turns()
	require.Len(t, turns, 4)
	assert.Equal(t, state.RoleUser, turns[0].Role)
	assert.Equal(t, state.RoleAssistant, turns[1].Role)
	assert.Equal(t, state.RoleTool, turns[2].Role)
	assert.Equal(t, state.RoleAssistant, turns[3].Role)
	assert.Equal(t, "done!", turns[3].Artifacts[0].(artifact.Text).Content)

	// Surface should have rendered the new turns.
	require.Len(t, surf.turns, 3)
	assert.Equal(t, state.RoleAssistant, surf.turns[0].Role)
	assert.Equal(t, state.RoleTool, surf.turns[1].Role)
	assert.Equal(t, state.RoleAssistant, surf.turns[2].Role)

	require.Contains(t, surf.statuses, "thinking...")
	require.Contains(t, surf.statuses, "calling tool...")
}

func TestReAct_Interrupt(t *testing.T) {
	eventsCh := make(chan surface.Event, 10)
	surf := &mockSurface{eventsCh: eventsCh}
	mem := &state.Memory{}

	s := loop.New(loop.WithSurface(surf))

	prov := &slowProvider{startedCh: make(chan struct{})}

	r := &ReAct{
		State:    mem,
		Step:     s,
		Surface:  surf,
		Provider: prov,
	}

	var wg sync.WaitGroup
	wg.Add(1)
	var runErr error
	go func() {
		defer wg.Done()
		runErr = r.Run(context.Background())
	}()

	// Send user message and wait for provider to start processing.
	eventsCh <- surface.UserMessageEvent{Content: "hi"}
	<-prov.startedCh
	time.Sleep(50 * time.Millisecond)

	// Send interrupt to cancel the in-progress turn.
	eventsCh <- surface.InterruptEvent{}
	close(eventsCh)

	wg.Wait()
	assert.NoError(t, runErr)
}
