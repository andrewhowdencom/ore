package cognitive

import (
	"context"
	"sync"
	"testing"

	"github.com/andrewhowdencom/tack/artifact"
	"github.com/andrewhowdencom/tack/loop"
	"github.com/andrewhowdencom/tack/provider"
	"github.com/andrewhowdencom/tack/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// simpleProvider always returns the same artifacts.
type simpleProvider struct {
	artifacts []artifact.Artifact
	err       error
}

func (p *simpleProvider) Invoke(ctx context.Context, s state.State) ([]artifact.Artifact, error) {
	return p.artifacts, p.err
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
	mem := &state.Memory{}

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
	mem := &state.Memory{}

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
	mem := &state.Memory{}

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
