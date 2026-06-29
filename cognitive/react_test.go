package cognitive

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// simpleProvider always returns the same artifacts.
type simpleProvider struct {
	artifacts []artifact.Artifact
	err       error
}

func (p *simpleProvider) Invoke(ctx context.Context, s ledger.State, _ models.Spec, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
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

func (p *countingProvider) Invoke(ctx context.Context, s ledger.State, _ models.Spec, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
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

func (p *cancelCheckingProvider) Invoke(ctx context.Context, s ledger.State, _ models.Spec, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
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
	fn func(ctx context.Context, art artifact.Artifact, e loop.Emitter) error
}

func (h *testHandler) Handle(ctx context.Context, art artifact.Artifact, e loop.Emitter) error {
	if h.fn != nil {
		return h.fn(ctx, art, e)
	}
	return nil
}

var _ loop.Handler = (*testHandler)(nil)

func TestReAct_SingleTurn(t *testing.T) {
	mem := &ledger.Buffer{}
	mem.Append(ledger.RoleUser, artifact.Text{Content: "hi"})

	s := loop.New(loop.WithOnEmit(func(ctx context.Context, event loop.OutputEvent) {
		if tc, ok := event.(loop.TurnCompleteEvent); ok {
			mem.Append(tc.Turn.Role, tc.Turn.Artifacts...)
		}
	}))

	prov := &simpleProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "hello!"},
		},
	}

	r := &ReAct{
		Step:     s,
		Provider: prov,
	}

	result, err := r.Run(context.Background(), mem)
	require.NoError(t, err)

	turns := result.Turns()
	require.Len(t, turns, 2)
	assert.Equal(t, ledger.RoleUser, turns[0].Role)
	assert.Equal(t, ledger.RoleAssistant, turns[1].Role)
	assert.Equal(t, "hello!", turns[1].Artifacts[0].(artifact.Text).Content)
}

func TestReAct_ToolLoop(t *testing.T) {
	mem := &ledger.Buffer{}
	mem.Append(ledger.RoleUser, artifact.Text{Content: "do something"})

	toolHandler := &testHandler{
		fn: func(ctx context.Context, art artifact.Artifact, e loop.Emitter) error {
			if art.Kind() == "tool_call" {
				e.Emit(ctx, loop.TurnCompleteEvent{
					Turn: ledger.Turn{
						Role:      ledger.RoleTool,
						Artifacts: []artifact.Artifact{artifact.Text{Content: "tool result"}},
					},
				})
			}
			return nil
		},
	}

	s := loop.New(
		loop.WithHandlers(toolHandler),
		loop.WithOnEmit(func(ctx context.Context, event loop.OutputEvent) {
			if tc, ok := event.(loop.TurnCompleteEvent); ok {
				mem.Append(tc.Turn.Role, tc.Turn.Artifacts...)
			}
		}),
	)

	prov := &countingProvider{}

	r := &ReAct{
		Step:     s,
		Provider: prov,
	}

	result, err := r.Run(context.Background(), mem)
	require.NoError(t, err)

	// State should have: User, Assistant (tool call), Tool, Assistant (final).
	turns := result.Turns()
	require.Len(t, turns, 4)
	assert.Equal(t, ledger.RoleUser, turns[0].Role)
	assert.Equal(t, ledger.RoleAssistant, turns[1].Role)
	assert.Equal(t, ledger.RoleTool, turns[2].Role)
	assert.Equal(t, ledger.RoleAssistant, turns[3].Role)
	assert.Equal(t, "done!", turns[3].Artifacts[0].(artifact.Text).Content)
}

func TestReAct_ProviderError(t *testing.T) {
	mem := &ledger.Buffer{}
	mem.Append(ledger.RoleUser, artifact.Text{Content: "hi"})

	s := loop.New(loop.WithOnEmit(func(ctx context.Context, event loop.OutputEvent) {
		if tc, ok := event.(loop.TurnCompleteEvent); ok {
			mem.Append(tc.Turn.Role, tc.Turn.Artifacts...)
		}
	}))

	wantErr := context.Canceled
	prov := &simpleProvider{err: wantErr}

	r := &ReAct{
		Step:     s,
		Provider: prov,
	}

	_, err := r.Run(context.Background(), mem)
	require.ErrorIs(t, err, wantErr)
}

func TestReAct_HandlerError(t *testing.T) {
	mem := &ledger.Buffer{}
	mem.Append(ledger.RoleUser, artifact.Text{Content: "do something"})

	wantErr := errors.New("handler failed")
	toolHandler := &testHandler{
		fn: func(ctx context.Context, art artifact.Artifact, e loop.Emitter) error {
			if art.Kind() == "tool_call" {
				return wantErr
			}
			return nil
		},
	}

	s := loop.New(
		loop.WithHandlers(toolHandler),
		loop.WithOnEmit(func(ctx context.Context, event loop.OutputEvent) {
			if tc, ok := event.(loop.TurnCompleteEvent); ok {
				mem.Append(tc.Turn.Role, tc.Turn.Artifacts...)
			}
		}),
	)

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

	_, err := r.Run(context.Background(), mem)
	require.ErrorIs(t, err, wantErr)
}

func TestReAct_ContextCancellation(t *testing.T) {
	mem := &ledger.Buffer{}
	mem.Append(ledger.RoleUser, artifact.Text{Content: "do something"})

	prov := &cancelCheckingProvider{}
	s := loop.New(loop.WithOnEmit(func(ctx context.Context, event loop.OutputEvent) {
		if tc, ok := event.(loop.TurnCompleteEvent); ok {
			mem.Append(tc.Turn.Role, tc.Turn.Artifacts...)
		}
	}))
	r := &ReAct{
		Step:     s,
		Provider: prov,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	_, err := r.Run(ctx, mem)
	require.ErrorIs(t, err, context.Canceled)
}
