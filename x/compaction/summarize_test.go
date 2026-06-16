package compaction

import (
	"github.com/andrewhowdencom/ore/models"
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
	artifacts     []artifact.Artifact
	err           error
	called        bool
	receivedTurns []state.Turn
	receivedOpts  []provider.InvokeOption
}

var _ provider.Provider = (*mockProvider)(nil)

func (m *mockProvider) Invoke(ctx context.Context, s state.State, _ models.Spec, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	m.called = true
	m.receivedTurns = s.Turns()
	m.receivedOpts = opts
	for _, art := range m.artifacts {
		select {
		case ch <- art:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return m.err
}

// textTurn returns a state.Turn with a single artifact.Text artifact.
func textTurn(role state.Role, content string) state.Turn {
	return state.Turn{
		Role:      role,
		Artifacts: []artifact.Artifact{artifact.Text{Content: content}},
	}
}

func TestSummarizeStrategy_ReducesTurns(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary of earlier discussion."},
		},
	}

	strategy := SummarizeStrategy{
		Provider: prov,
	}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
	}

	result, err := strategy.Compact(context.Background(), turns)
	require.NoError(t, err)
	assert.True(t, prov.called)
	require.Len(t, result, 1)

	assert.Equal(t, state.RoleSystem, result[0].Role)
	require.Len(t, result[0].Artifacts, 1)
	text, ok := result[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "Summary of earlier discussion.", text.Content)
}

func TestSummarizeStrategy_PropagatesProviderError(t *testing.T) {
	wantErr := errors.New("provider failure")
	prov := &mockProvider{err: wantErr}

	strategy := SummarizeStrategy{
		Provider: prov,
	}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
	}

	_, err := strategy.Compact(context.Background(), turns)
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
	assert.Contains(t, err.Error(), "summarization provider call failed")
}

func TestSummarizeStrategy_IgnoresNonTextArtifacts(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Reasoning{Content: "thinking..."},
			artifact.Text{Content: "Actual summary."},
			artifact.Usage{TotalTokens: 42},
			artifact.ToolCall{Name: "test"},
		},
	}

	strategy := SummarizeStrategy{
		Provider: prov,
	}

	turns := []state.Turn{
		{
			Role: state.RoleUser,
			Artifacts: []artifact.Artifact{
				artifact.Text{Content: "aaaa"},
				artifact.Usage{TotalTokens: 10000},
				artifact.Reasoning{Content: "thinking..."},
				artifact.ToolCall{Name: "test"},
			},
		},
		textTurn(state.RoleAssistant, "aaaa"),
	}

	result, err := strategy.Compact(context.Background(), turns)
	require.NoError(t, err)
	assert.True(t, prov.called)
	require.Len(t, result, 1)

	require.Len(t, result[0].Artifacts, 1)
	text, ok := result[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "Actual summary.", text.Content)
}

func TestSummarizeStrategy_MultipleTextArtifactsConcatenated(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Part one. "},
			artifact.Text{Content: "Part two."},
		},
	}

	strategy := SummarizeStrategy{
		Provider: prov,
	}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
	}

	result, err := strategy.Compact(context.Background(), turns)
	require.NoError(t, err)
	assert.True(t, prov.called)
	require.Len(t, result, 1)

	require.Len(t, result[0].Artifacts, 1)
	text, ok := result[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "Part one. Part two.", text.Content)
}

func TestSummarizeStrategy_EmptyTurns(t *testing.T) {
	prov := &mockProvider{}

	strategy := SummarizeStrategy{
		Provider: prov,
	}

	result, err := strategy.Compact(context.Background(), []state.Turn{})
	require.NoError(t, err)
	assert.False(t, prov.called)
	assert.Empty(t, result)
}

func TestSummarizeStrategy_SingleTurn(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary."},
		},
	}

	strategy := SummarizeStrategy{
		Provider: prov,
	}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
	}

	result, err := strategy.Compact(context.Background(), turns)
	require.NoError(t, err)
	assert.True(t, prov.called)
	require.Len(t, result, 1)
	assert.Equal(t, state.RoleSystem, result[0].Role)
}

func TestSummarizeStrategy_PassesAllTurns(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary."},
		},
	}

	strategy := SummarizeStrategy{
		Provider: prov,
	}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
	}

	result, err := strategy.Compact(context.Background(), turns)
	require.NoError(t, err)
	assert.True(t, prov.called)

	// Provider receives all turns + appended user prompt
	require.Len(t, prov.receivedTurns, 5)
	assert.Equal(t, state.RoleUser, prov.receivedTurns[0].Role)
	assert.Equal(t, state.RoleAssistant, prov.receivedTurns[1].Role)
	assert.Equal(t, state.RoleUser, prov.receivedTurns[2].Role)
	assert.Equal(t, state.RoleAssistant, prov.receivedTurns[3].Role)
	assert.Equal(t, state.RoleUser, prov.receivedTurns[4].Role)

	// Result: single summary turn
	require.Len(t, result, 1)
	assert.Equal(t, state.RoleSystem, result[0].Role)
}

func TestSummarizeStrategy_NilProvider(t *testing.T) {
	strategy := SummarizeStrategy{}
	turns := []state.Turn{
		textTurn(state.RoleUser, "hello"),
	}
	_, err := strategy.Compact(context.Background(), turns)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Provider must not be nil")
}

func TestSummarizeStrategy_NoTextArtifacts(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Usage{TotalTokens: 42},
			artifact.Reasoning{Content: "thinking..."},
			artifact.ToolCall{Name: "test"},
		},
	}

	strategy := SummarizeStrategy{
		Provider: prov,
	}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "bbbb"),
	}

	result, err := strategy.Compact(context.Background(), turns)
	require.NoError(t, err)
	assert.True(t, prov.called)
	require.Len(t, result, 1)
	assert.Equal(t, state.RoleSystem, result[0].Role)
	require.Len(t, result[0].Artifacts, 1)
	text, ok := result[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Empty(t, text.Content)
}

func TestSummarizeStrategy_UsesDefaultPrompt(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary."},
		},
	}

	strategy := SummarizeStrategy{
		Provider: prov,
		Prompt:   "",
	}

	// With non-empty turns, summarization is triggered.
	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
	}

	_, err := strategy.Compact(context.Background(), turns)
	require.NoError(t, err)
	assert.True(t, prov.called)

	// Provider receives all turns + appended user prompt.
	require.Len(t, prov.receivedTurns, 4)
	assert.Equal(t, state.RoleUser, prov.receivedTurns[3].Role)
	require.Len(t, prov.receivedTurns[3].Artifacts, 1)
	text, ok := prov.receivedTurns[3].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, defaultPrompt, text.Content)
}

func TestSummarizeStrategy_UsesCustomPrompt(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary."},
		},
	}

	strategy := SummarizeStrategy{
		Provider: prov,
		Prompt:   "Custom override prompt",
	}

	// With non-empty turns, summarization is triggered.
	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
	}

	_, err := strategy.Compact(context.Background(), turns)
	require.NoError(t, err)
	assert.True(t, prov.called)

	// Provider receives all turns + appended user prompt.
	require.Len(t, prov.receivedTurns, 4)
	assert.Equal(t, state.RoleUser, prov.receivedTurns[3].Role)
	require.Len(t, prov.receivedTurns[3].Artifacts, 1)
	text, ok := prov.receivedTurns[3].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "Custom override prompt", text.Content)
}

func TestSummarizeStrategy_TextDeltaArtifacts(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Summary part one. "},
			artifact.TextDelta{Content: "Summary part two."},
		},
	}

	strategy := SummarizeStrategy{
		Provider: prov,
	}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
	}

	result, err := strategy.Compact(context.Background(), turns)
	require.NoError(t, err)
	assert.True(t, prov.called)
	require.Len(t, result, 1)

	require.Len(t, result[0].Artifacts, 1)
	text, ok := result[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "Summary part one. Summary part two.", text.Content)
}

func TestSummarizeStrategy_MixedTextAndTextDelta(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Part A. "},
			artifact.TextDelta{Content: "Part B."},
		},
	}

	strategy := SummarizeStrategy{
		Provider: prov,
	}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
	}

	result, err := strategy.Compact(context.Background(), turns)
	require.NoError(t, err)
	assert.True(t, prov.called)
	require.Len(t, result, 1)

	require.Len(t, result[0].Artifacts, 1)
	text, ok := result[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "Part A. Part B.", text.Content)
}

func TestSummarizeStrategy_TextDeltaTimestampNonZero(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Summary."},
		},
	}

	strategy := SummarizeStrategy{
		Provider: prov,
	}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
	}

	result, err := strategy.Compact(context.Background(), turns)
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.False(t, result[0].Timestamp.IsZero())
}

// extractMaxTokensOption finds a provider.MaxTokensOption in the
// received option slice. The summarize strategy is contractually
// expected to pass exactly one MaxTokensOption; the helper isolates
// the value (and asserts uniqueness) for the new tests below.
func extractMaxTokensOption(t *testing.T, opts []provider.InvokeOption) int64 {
	t.Helper()
	var got int64
	var count int
	for _, opt := range opts {
		mto, ok := opt.(provider.MaxTokensOption)
		if !ok {
			continue
		}
		count++
		got = mto.N
	}
	require.Equal(t, 1, count, "expected exactly one provider.MaxTokensOption, got %d", count)
	return got
}

// TestSummarizeStrategy_AppliesDefaultMaxTokens asserts that a
// zero-value SummarizeStrategy (MaxTokens unset) requests the
// default 8192-token budget from the provider. This is the
// self-sizing behavior that fixes the original '##' compaction
// bug: without an explicit budget, the Anthropic adapter used to
// default to 1 token, producing a one-token response.
func TestSummarizeStrategy_AppliesDefaultMaxTokens(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary."},
		},
	}

	strategy := SummarizeStrategy{Provider: prov}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
	}

	_, err := strategy.Compact(context.Background(), turns)
	require.NoError(t, err)
	assert.True(t, prov.called)
	assert.Equal(t, int64(8192), extractMaxTokensOption(t, prov.receivedOpts))
}

// TestSummarizeStrategy_AppliesCustomMaxTokens asserts that a
// non-zero MaxTokens is forwarded verbatim. Applications with
// unusual workloads (very long histories, or summaries intended
// for very large downstream contexts) can override the default
// via the struct field.
func TestSummarizeStrategy_AppliesCustomMaxTokens(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary."},
		},
	}

	strategy := SummarizeStrategy{Provider: prov, Spec: models.Spec{MaxOutputTokens: 4096}}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
	}

	_, err := strategy.Compact(context.Background(), turns)
	require.NoError(t, err)
	assert.True(t, prov.called)
	assert.Equal(t, int64(4096), extractMaxTokensOption(t, prov.receivedOpts))
}

// TestSummarizeStrategy_NegativeMaxTokensFallsBackToDefault
// asserts that an explicit negative value also falls back to the
// default. Zero is the unset value (Go's zero-value idiom); a
// negative value is a programming mistake, but the strategy
// recovers gracefully rather than sending a malformed budget
// request.
func TestSummarizeStrategy_NegativeMaxTokensFallsBackToDefault(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary."},
		},
	}

	strategy := SummarizeStrategy{Provider: prov, Spec: models.Spec{MaxOutputTokens: -1}}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
	}

	_, err := strategy.Compact(context.Background(), turns)
	require.NoError(t, err)
	assert.Equal(t, int64(8192), extractMaxTokensOption(t, prov.receivedOpts))
}

// TestSummarizeStrategy_TruncatedResultReturnsError asserts the
// central failure mode: when the provider's final StopReason is
// Length, Compact returns the original turns unchanged wrapped
// with ErrTruncatedSummary. This is the direct test of the
// compaction bug fix — the strategy no longer silently writes
// a one-token "##" into the conversation buffer.
func TestSummarizeStrategy_TruncatedResultReturnsError(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "##"},
			artifact.StopReason{Reason: artifact.StopReasonLength},
		},
	}

	strategy := SummarizeStrategy{Provider: prov}

	turns := []state.Turn{
		textTurn(state.RoleUser, "a very long history to summarize"),
		textTurn(state.RoleAssistant, "first response"),
		textTurn(state.RoleUser, "more context"),
		textTurn(state.RoleAssistant, "more response"),
	}

	result, err := strategy.Compact(context.Background(), turns)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTruncatedSummary)
	// Original turns returned unchanged.
	assert.Equal(t, turns, result)
}

// TestSummarizeStrategy_StopReasonStopDoesNotError asserts that a
// normal completion (StopReasonStop, the canonical 'end_turn /
// stop' mapping) does not trigger ErrTruncatedSummary. The
// strategy should produce the regular single-turn summary.
func TestSummarizeStrategy_StopReasonStopDoesNotError(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary content."},
			artifact.StopReason{Reason: artifact.StopReasonStop},
		},
	}

	strategy := SummarizeStrategy{Provider: prov}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
	}

	result, err := strategy.Compact(context.Background(), turns)
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, state.RoleSystem, result[0].Role)
	require.Len(t, result[0].Artifacts, 1)
	text, ok := result[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "Summary content.", text.Content)
}

// TestSummarizeStrategy_StopReasonToolUseDoesNotError asserts that
// a tool_use reason (the model emitted a tool call rather than a
// text response) does not trigger ErrTruncatedSummary. The
// strategy is not a tool-caller, so a tool_use reason is an
// oddity (it would mean the model tried to invoke a tool in
// response to the summary prompt), but it is not a truncation
// failure — callers can decide how to react.
func TestSummarizeStrategy_StopReasonToolUseDoesNotError(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Partial summary "},
			artifact.Text{Content: "before tool call."},
			artifact.StopReason{Reason: artifact.StopReasonToolUse},
		},
	}

	strategy := SummarizeStrategy{Provider: prov}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
	}

	result, err := strategy.Compact(context.Background(), turns)
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "Partial summary before tool call.", result[0].Artifacts[0].(artifact.Text).Content)
}

// TestSummarizeStrategy_NoStopReasonDoesNotError asserts that the
// absence of a StopReason on the channel (e.g. a future adapter
// that has not yet been updated, or a mock that does not emit
// one) is treated as "no truncation reported" rather than as an
// error. This is a forward-compat guarantee: older code paths
// that don't yet emit StopReason keep working.
func TestSummarizeStrategy_NoStopReasonDoesNotError(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary."},
		},
	}

	strategy := SummarizeStrategy{Provider: prov}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
	}

	result, err := strategy.Compact(context.Background(), turns)
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, "Summary.", result[0].Artifacts[0].(artifact.Text).Content)
}

// TestSummarizeStrategy_StopReason_InterleavedTextAndReason
// asserts that the channel-drain goroutine correctly captures
// the latest non-empty StopReason even when text and the reason
// are interleaved on the channel. The openai wire format sends
// the reason on the same final delta as the text, so the
// goroutine may see a text delta followed by a reason in either
// order depending on goroutine scheduling.
func TestSummarizeStrategy_StopReason_InterleavedTextAndReason(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Summary "},
			artifact.TextDelta{Content: "content."},
			artifact.StopReason{Reason: artifact.StopReasonLength},
		},
	}

	strategy := SummarizeStrategy{Provider: prov}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
	}

	result, err := strategy.Compact(context.Background(), turns)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTruncatedSummary)
	assert.Equal(t, turns, result)
}
