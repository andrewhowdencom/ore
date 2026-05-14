package cognitive

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// simpleProvider always returns the same artifacts.
type simpleProvider struct {
	artifacts []artifact.Artifact
	err       error
}

func (p *simpleProvider) Invoke(ctx context.Context, s state.State, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	for _, art := range p.artifacts {
		select {
		case ch <- art:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return p.err
}

var _ provider.Provider = (*simpleProvider)(nil)

// countingProvider returns different artifacts on successive calls.
type countingProvider struct {
	mu        sync.Mutex
	callCount int
}

func (p *countingProvider) Invoke(ctx context.Context, s state.State, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.callCount++
	var artifacts []artifact.Artifact
	switch p.callCount {
	case 1:
		artifacts = []artifact.Artifact{
			artifact.Text{Content: "calling tool"},
			artifact.ToolCall{Name: "test", Arguments: "{}"},
		}
	default:
		artifacts = []artifact.Artifact{
			artifact.Text{Content: "done!"},
		}
	}
	for _, art := range artifacts {
		select {
		case ch <- art:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

var _ provider.Provider = (*countingProvider)(nil)

// cancelCheckingProvider checks ctx.Err() before returning artifacts.
type cancelCheckingProvider struct{}

func (p *cancelCheckingProvider) Invoke(ctx context.Context, s state.State, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case ch <- artifact.ToolCall{Name: "test", Arguments: "{}"}:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

var _ provider.Provider = (*cancelCheckingProvider)(nil)

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
	mem := &state.Buffer{}

	s := loop.New()

	prov := &simpleProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "hello!"},
		},
	}

	r := &ReAct{
		Step:     s,
		Provider: prov,
	}

	mem.Append(state.RoleUser, artifact.Text{Content: "hi"})
	result, err := r.Run(context.Background(), mem)
	require.NoError(t, err)

	turns := result.Turns()
	require.Len(t, turns, 2)
	assert.Equal(t, state.RoleUser, turns[0].Role)
	assert.Equal(t, state.RoleAssistant, turns[1].Role)
	assert.Equal(t, "hello!", turns[1].Artifacts[0].(artifact.Text).Content)
}

func TestReAct_ToolLoop(t *testing.T) {
	mem := &state.Buffer{}

	toolHandler := &testHandler{
		fn: func(ctx context.Context, art artifact.Artifact, s state.State) error {
			if art.Kind() == "tool_call" {
				s.Append(state.RoleTool, artifact.Text{Content: "tool result"})
			}
			return nil
		},
	}

	s := loop.New(loop.WithHandlers(toolHandler))

	prov := &countingProvider{}

	r := &ReAct{
		Step:     s,
		Provider: prov,
	}

	mem.Append(state.RoleUser, artifact.Text{Content: "do something"})
	result, err := r.Run(context.Background(), mem)
	require.NoError(t, err)

	// State should have: User, Assistant (tool call), Tool, Assistant (final).
	turns := result.Turns()
	require.Len(t, turns, 4)
	assert.Equal(t, state.RoleUser, turns[0].Role)
	assert.Equal(t, state.RoleAssistant, turns[1].Role)
	assert.Equal(t, state.RoleTool, turns[2].Role)
	assert.Equal(t, state.RoleAssistant, turns[3].Role)
	assert.Equal(t, "done!", turns[3].Artifacts[0].(artifact.Text).Content)
}

func TestReAct_ProviderError(t *testing.T) {
	mem := &state.Buffer{}

	s := loop.New()

	wantErr := context.Canceled
	prov := &simpleProvider{err: wantErr}

	r := &ReAct{
		Step:     s,
		Provider: prov,
	}

	mem.Append(state.RoleUser, artifact.Text{Content: "hi"})
	_, err := r.Run(context.Background(), mem)
	require.ErrorIs(t, err, wantErr)
}

func TestReAct_HandlerError(t *testing.T) {
	mem := &state.Buffer{}

	wantErr := errors.New("handler failed")
	toolHandler := &testHandler{
		fn: func(ctx context.Context, art artifact.Artifact, s state.State) error {
			if art.Kind() == "tool_call" {
				return wantErr
			}
			return nil
		},
	}

	s := loop.New(loop.WithHandlers(toolHandler))

	prov := &simpleProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "calling tool"},
			artifact.ToolCall{Name: "test", Arguments: "{}"},
		},
	}

	r := &ReAct{
		Step:     s,
		Provider: prov,
	}

	mem.Append(state.RoleUser, artifact.Text{Content: "do something"})
	_, err := r.Run(context.Background(), mem)
	require.ErrorIs(t, err, wantErr)
}

func TestReAct_ContextCancellation(t *testing.T) {
	mem := &state.Buffer{}

	prov := &cancelCheckingProvider{}
	s := loop.New()
	r := &ReAct{
		Step:     s,
		Provider: prov,
	}

	mem.Append(state.RoleUser, artifact.Text{Content: "do something"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, err := r.Run(ctx, mem)
	require.ErrorIs(t, err, context.Canceled)
}
