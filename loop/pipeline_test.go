package loop

import (
	"context"
	"errors"
	"github.com/andrewhowdencom/ore/models"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockEmitter is a simple Emitter for testing handler-side emissions.
type mockEmitter struct {
	emitted []OutputEvent
}

func (m *mockEmitter) Emit(ctx context.Context, event OutputEvent) {
	m.emitted = append(m.emitted, event)
}

// TestPipeline_Turn_AccumulatesDeltas verifies that Pipeline.Turn accumulates
// adjacent TextDelta artifacts into a single Text block.
func TestPipeline_Turn_AccumulatesDeltas(t *testing.T) {
	spec := models.Spec{Name: "test-model"}
	p := newPipeline()
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Hello"},
			artifact.TextDelta{Content: " "},
			artifact.TextDelta{Content: "world"},
		},
	}

	var emittedArtifacts []artifact.Artifact
	st, accumulated, err := p.Turn(context.Background(), mem, spec, prov, func(art artifact.Artifact) {
		emittedArtifacts = append(emittedArtifacts, art)
	})

	require.NoError(t, err)
	require.Len(t, accumulated, 1)
	text, ok := accumulated[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "Hello world", text.Content)

	// Deltas are emitted individually to the callback, plus the final accumulated
	// Text artifact from the flush loop.
	require.Len(t, emittedArtifacts, 4)
	for i := 0; i < 3; i++ {
		_, ok := emittedArtifacts[i].(artifact.TextDelta)
		require.True(t, ok, "artifact %d should be TextDelta, got %T", i, emittedArtifacts[i])
	}
	_, ok = emittedArtifacts[3].(artifact.Text)
	require.True(t, ok, "artifact 3 should be Text, got %T", emittedArtifacts[3])

	// State is not mutated by Pipeline (only by EventBus via Step).
	assert.Equal(t, 1, len(st.Turns()))
}

// TestPipeline_Turn_FlushesAccumulatorsOnNonDelta verifies that accumulated
// deltas are flushed before a non-delta artifact is processed, and remaining
// deltas are flushed at stream end.
func TestPipeline_Turn_FlushesAccumulatorsOnNonDelta(t *testing.T) {
	spec := models.Spec{Name: "test-model"}
	p := newPipeline()
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Hello"},
			artifact.TextDelta{Content: " "},
			artifact.Text{Content: "interrupt"}, // non-delta flushes
			artifact.TextDelta{Content: "world"},
		},
	}

	var emittedArtifacts []artifact.Artifact
	st, accumulated, err := p.Turn(context.Background(), mem, spec, prov, func(art artifact.Artifact) {
		emittedArtifacts = append(emittedArtifacts, art)
	})

	require.NoError(t, err)
	require.Len(t, accumulated, 3)
	assert.Equal(t, "Hello ", accumulated[0].(artifact.Text).Content)
	assert.Equal(t, "interrupt", accumulated[1].(artifact.Text).Content)
	assert.Equal(t, "world", accumulated[2].(artifact.Text).Content)

	// Callback sees: 2 deltas + 1 flushed + 1 non-delta + 1 delta + 1 flushed.
	require.Len(t, emittedArtifacts, 6)
	assert.Equal(t, "Hello", emittedArtifacts[0].(artifact.TextDelta).Content)
	assert.Equal(t, " ", emittedArtifacts[1].(artifact.TextDelta).Content)
	assert.Equal(t, "Hello ", emittedArtifacts[2].(artifact.Text).Content)
	assert.Equal(t, "interrupt", emittedArtifacts[3].(artifact.Text).Content)
	assert.Equal(t, "world", emittedArtifacts[4].(artifact.TextDelta).Content)
	assert.Equal(t, "world", emittedArtifacts[5].(artifact.Text).Content)

	assert.Equal(t, 1, len(st.Turns()))
}

// TestPipeline_Turn_ErrorPropagatesProviderError verifies that a provider
// error is returned along with any accumulated artifacts.
func TestPipeline_Turn_ErrorPropagatesProviderError(t *testing.T) {
	spec := models.Spec{Name: "test-model"}
	p := newPipeline()
	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})

	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "partial"},
		},
		err: errors.New("provider failed"),
	}

	var emittedArtifacts []artifact.Artifact
	st, accumulated, err := p.Turn(context.Background(), mem, spec, prov, func(art artifact.Artifact) {
		emittedArtifacts = append(emittedArtifacts, art)
	})

	require.Error(t, err)
	assert.Equal(t, "provider failed", err.Error())
	require.Len(t, accumulated, 1)
	assert.Equal(t, "partial", accumulated[0].(artifact.Text).Content)

	// Callback received the streaming delta and the flushed accumulated Text.
	require.Len(t, emittedArtifacts, 2)
	assert.Equal(t, "partial", emittedArtifacts[0].(artifact.TextDelta).Content)
	assert.Equal(t, "partial", emittedArtifacts[1].(artifact.Text).Content)

	assert.Equal(t, 1, len(st.Turns()))
}

// TestPipeline_Turn_TransformErrorAborts verifies that transforms run in
// registration order and an error aborts the turn before the provider is
// invoked.
func TestPipeline_Turn_TransformErrorAborts(t *testing.T) {
	spec := models.Spec{Name: "test-model"}
	p := newPipeline()
	wantErr := errors.New("transform failed")
	p.transforms = []Transform{
		&mockTransform{fn: func(ctx context.Context, s state.State) (state.State, error) {
			return s, nil
		}},
		&mockTransform{fn: func(ctx context.Context, s state.State) (state.State, error) {
			return s, wantErr
		}},
		&mockTransform{fn: func(ctx context.Context, s state.State) (state.State, error) {
			t.Fatal("third transform should not run")
			return s, nil
		}},
	}

	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})
	prov := &mockProvider{}

	st, accumulated, err := p.Turn(context.Background(), mem, spec, prov, func(art artifact.Artifact) {})

	require.ErrorIs(t, err, wantErr)
	assert.Empty(t, accumulated)
	assert.Equal(t, 1, len(st.Turns()))
}

// TestPipeline_Turn_TransformOrdering verifies that transforms run in
// registration order and each receives the state returned by the previous one.
func TestPipeline_Turn_TransformOrdering(t *testing.T) {
	spec := models.Spec{Name: "test-model"}
	p := newPipeline()
	var order []int
	p.transforms = []Transform{
		&mockTransform{fn: func(ctx context.Context, s state.State) (state.State, error) {
			order = append(order, 1)
			return s, nil
		}},
		&mockTransform{fn: func(ctx context.Context, s state.State) (state.State, error) {
			order = append(order, 2)
			return s, nil
		}},
		&mockTransform{fn: func(ctx context.Context, s state.State) (state.State, error) {
			order = append(order, 3)
			return s, nil
		}},
	}

	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})
	prov := &mockProvider{artifacts: []artifact.Artifact{artifact.Text{Content: "response"}}}

	_, _, err := p.Turn(context.Background(), mem, spec, prov, func(art artifact.Artifact) {})
	require.NoError(t, err)

	assert.Equal(t, []int{1, 2, 3}, order)
}

// TestPipeline_RunHandlers_ErrorPropagates verifies that a handler returning
// an error aborts the turn and the error is propagated.
func TestPipeline_RunHandlers_ErrorPropagates(t *testing.T) {
	p := newPipeline()
	h := &mockHandler{err: errors.New("handler failed")}
	p.handlers = []Handler{h}

	artifacts := []artifact.Artifact{artifact.Text{Content: "hello"}}

	err := p.RunHandlers(context.Background(), artifacts, nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "handler failed")
	require.Len(t, h.called, 1)
}

// TestPipeline_RunHandlers_MultipleHandlers verifies that multiple handlers
// are invoked on each artifact in registration order.
func TestPipeline_RunHandlers_MultipleHandlers(t *testing.T) {
	p := newPipeline()
	h1 := &mockHandler{}
	h2 := &mockHandler{}
	p.handlers = []Handler{h1, h2}

	artifacts := []artifact.Artifact{
		artifact.Text{Content: "hello"},
		artifact.ToolCall{Name: "test", Arguments: "{}"},
	}

	err := p.RunHandlers(context.Background(), artifacts, nil)
	require.NoError(t, err)

	require.Len(t, h1.called, 2)
	require.Len(t, h2.called, 2)
	assert.Equal(t, "text", h1.called[0].Kind())
	assert.Equal(t, "tool_call", h1.called[1].Kind())
}

// TestPipeline_RunHandlers_EmitterPassedToHandler verifies that the provided
// Emitter is passed through to handlers.
func TestPipeline_RunHandlers_EmitterPassedToHandler(t *testing.T) {
	p := newPipeline()
	var capturedEmitter Emitter
	h := &mockHandler{
		fn: func(ctx context.Context, art artifact.Artifact, e Emitter) error {
			capturedEmitter = e
			return nil
		},
	}
	p.handlers = []Handler{h}

	artifacts := []artifact.Artifact{artifact.Text{Content: "hello"}}
	emitter := &mockEmitter{}

	err := p.RunHandlers(context.Background(), artifacts, emitter)
	require.NoError(t, err)
	assert.Equal(t, emitter, capturedEmitter)
}

// TestPipeline_Turn_ApplyDisplayHints verifies that Pipeline.Turn attaches
// display hints to ToolCall artifacts when a matching ToolsOption is present.
func TestPipeline_Turn_ApplyDisplayHints(t *testing.T) {
	spec := models.Spec{Name: "test-model"}
	p := newPipeline()
	p.invokeOpts = []provider.InvokeOption{
		provider.ToolsOption{
			Tools: func(ctx context.Context, s state.State) []tool.Tool {
				return []tool.Tool{
					{
						Name: "test",
						DisplayHint: func(args map[string]any) any {
							return "hint: " + args["query"].(string)
						},
					},
				}
			},
		},
	}

	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.ToolCall{Name: "test", Arguments: `{"query":"hello"}`},
		},
	}

	st, accumulated, err := p.Turn(context.Background(), mem, spec, prov, func(art artifact.Artifact) {})
	require.NoError(t, err)
	require.Len(t, accumulated, 1)

	tc, ok := accumulated[0].(artifact.ToolCall)
	require.True(t, ok)
	assert.Equal(t, "hint: hello", tc.Display)
	assert.Equal(t, 1, len(st.Turns()))
}

// TestPipeline_Turn_ApplyDisplayHints_NoMatchingHint verifies that ToolCall
// artifacts are left unchanged when no matching display hint is found.
func TestPipeline_Turn_ApplyDisplayHints_NoMatchingHint(t *testing.T) {
	spec := models.Spec{Name: "test-model"}
	p := newPipeline()
	p.invokeOpts = []provider.InvokeOption{
		provider.ToolsOption{
			Tools: func(ctx context.Context, s state.State) []tool.Tool {
				return []tool.Tool{
					{
						Name: "other",
						DisplayHint: func(args map[string]any) any {
							return "hint"
						},
					},
				}
			},
		},
	}

	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hello"})
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.ToolCall{Name: "test", Arguments: `{"query":"hello"}`},
		},
	}

	st, accumulated, err := p.Turn(context.Background(), mem, spec, prov, func(art artifact.Artifact) {})
	require.NoError(t, err)
	require.Len(t, accumulated, 1)

	tc, ok := accumulated[0].(artifact.ToolCall)
	require.True(t, ok)
	assert.Nil(t, tc.Display)
	assert.Equal(t, 1, len(st.Turns()))
}
