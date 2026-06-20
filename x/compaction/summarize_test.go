package compaction

import (
	"context"
	"errors"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/models"
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

// findCompaction extracts the artifact.Compaction from a turn's
// artifacts slice. It fails the test if none is present.
func findCompaction(t *testing.T, turn state.Turn) artifact.Compaction {
	t.Helper()
	for _, a := range turn.Artifacts {
		if c, ok := a.(artifact.Compaction); ok {
			return c
		}
	}
	t.Fatalf("expected artifact.Compaction on turn, got artifacts: %+v", turn.Artifacts)
	return artifact.Compaction{}
}

// findText extracts the artifact.Text from a turn's artifacts slice.
// It fails the test if none is present.
func findText(t *testing.T, turn state.Turn) artifact.Text {
	t.Helper()
	for _, a := range turn.Artifacts {
		if tx, ok := a.(artifact.Text); ok {
			return tx
		}
	}
	t.Fatalf("expected artifact.Text on turn, got artifacts: %+v", turn.Artifacts)
	return artifact.Text{}
}

func TestSummarize_ProducesCompactionTurn(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary of earlier discussion."},
		},
	}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
	}

	result, err := Summarize(context.Background(), prov, models.Spec{Name: "test-model"}, turns)
	require.NoError(t, err)
	assert.True(t, prov.called)

	// The result is a single turn with RoleSystem, carrying both
	// artifact.Text and artifact.Compaction.
	assert.Equal(t, state.RoleSystem, result.Role)

	summary := findText(t, result)
	assert.Equal(t, "Summary of earlier discussion.", summary.Content)

	comp := findCompaction(t, result)
	assert.Equal(t, len(turns), comp.CompactedThrough)
	assert.Equal(t, len(turns), comp.DroppedTurnCount)
	assert.Equal(t, StrategyNameSummarize, comp.Strategy)
	assert.Equal(t, "test-model", comp.Model)
	assert.False(t, comp.CreatedAt.IsZero(), "CreatedAt should be set")
	assert.Greater(t, comp.DroppedTokenEstimate, int64(0), "DroppedTokenEstimate should reflect bytes of dropped artifacts")
}

func TestSummarize_PropagatesProviderError(t *testing.T) {
	wantErr := errors.New("provider failure")
	prov := &mockProvider{err: wantErr}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
	}

	_, err := Summarize(context.Background(), prov, models.Spec{}, turns)
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
	assert.Contains(t, err.Error(), "summarization provider call failed")
}

func TestSummarize_IgnoresNonTextArtifacts(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Reasoning{Content: "thinking..."},
			artifact.Text{Content: "Actual summary."},
			artifact.Usage{TotalTokens: 42},
			artifact.ToolCall{Name: "test"},
		},
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

	result, err := Summarize(context.Background(), prov, models.Spec{}, turns)
	require.NoError(t, err)
	assert.True(t, prov.called)

	summary := findText(t, result)
	assert.Equal(t, "Actual summary.", summary.Content)
}

func TestSummarize_MultipleTextArtifactsConcatenated(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Part one. "},
			artifact.Text{Content: "Part two."},
		},
	}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
	}

	result, err := Summarize(context.Background(), prov, models.Spec{}, turns)
	require.NoError(t, err)
	assert.True(t, prov.called)

	summary := findText(t, result)
	assert.Equal(t, "Part one. Part two.", summary.Content)
}

func TestSummarize_EmptyTurns_ReturnsZeroTurnNoError(t *testing.T) {
	prov := &mockProvider{}

	result, err := Summarize(context.Background(), prov, models.Spec{}, []state.Turn{})
	require.NoError(t, err)
	assert.False(t, prov.called, "provider should not be called for empty turns")
	assert.Equal(t, state.Turn{}, result, "empty turns return the zero turn with no error")
}

func TestSummarize_SingleTurn(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary."},
		},
	}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
	}

	result, err := Summarize(context.Background(), prov, models.Spec{}, turns)
	require.NoError(t, err)
	assert.True(t, prov.called)
	assert.Equal(t, state.RoleSystem, result.Role)
	summary := findText(t, result)
	assert.Equal(t, "Summary.", summary.Content)
}

func TestSummarize_PassesAllTurnsPlusPrompt(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary."},
		},
	}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
	}

	_, err := Summarize(context.Background(), prov, models.Spec{}, turns)
	require.NoError(t, err)
	assert.True(t, prov.called)

	// Provider receives all turns + appended user prompt
	require.Len(t, prov.receivedTurns, 5)
	assert.Equal(t, state.RoleUser, prov.receivedTurns[0].Role)
	assert.Equal(t, state.RoleAssistant, prov.receivedTurns[1].Role)
	assert.Equal(t, state.RoleUser, prov.receivedTurns[2].Role)
	assert.Equal(t, state.RoleAssistant, prov.receivedTurns[3].Role)
	assert.Equal(t, state.RoleUser, prov.receivedTurns[4].Role)
}

func TestSummarize_NilProvider(t *testing.T) {
	turns := []state.Turn{
		textTurn(state.RoleUser, "hello"),
	}
	_, err := Summarize(context.Background(), nil, models.Spec{}, turns)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider must not be nil")
}

func TestSummarize_NoTextArtifacts(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Usage{TotalTokens: 42},
			artifact.Reasoning{Content: "thinking..."},
			artifact.ToolCall{Name: "test"},
		},
	}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "bbbb"),
	}

	result, err := Summarize(context.Background(), prov, models.Spec{}, turns)
	require.NoError(t, err)
	assert.True(t, prov.called)
	assert.Equal(t, state.RoleSystem, result.Role)
	summary := findText(t, result)
	assert.Empty(t, summary.Content)
}

func TestSummarize_UsesDefaultPrompt(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary."},
		},
	}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
	}

	_, err := Summarize(context.Background(), prov, models.Spec{}, turns)
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

func TestSummarize_TextDeltaArtifacts(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Summary part one. "},
			artifact.TextDelta{Content: "Summary part two."},
		},
	}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
	}

	result, err := Summarize(context.Background(), prov, models.Spec{}, turns)
	require.NoError(t, err)
	assert.True(t, prov.called)
	summary := findText(t, result)
	assert.Equal(t, "Summary part one. Summary part two.", summary.Content)
}

func TestSummarize_MixedTextAndTextDelta(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Part A. "},
			artifact.TextDelta{Content: "Part B."},
		},
	}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
	}

	result, err := Summarize(context.Background(), prov, models.Spec{}, turns)
	require.NoError(t, err)
	assert.True(t, prov.called)
	summary := findText(t, result)
	assert.Equal(t, "Part A. Part B.", summary.Content)
}

func TestSummarize_TimestampNonZero(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary."},
		},
	}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
	}

	result, err := Summarize(context.Background(), prov, models.Spec{}, turns)
	require.NoError(t, err)
	assert.False(t, result.Timestamp.IsZero())
}

// extractMaxTokensOption finds a provider.MaxTokensOption in the
// received option slice. Summarize is contractually expected to pass
// exactly one MaxTokensOption; the helper isolates the value (and
// asserts uniqueness) for the tests below.
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

// TestSummarize_AppliesDefaultMaxTokens asserts that a zero-value
// Spec (MaxOutputTokens unset) requests the default 8192-token
// budget from the provider.
func TestSummarize_AppliesDefaultMaxTokens(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary."},
		},
	}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
	}

	_, err := Summarize(context.Background(), prov, models.Spec{}, turns)
	require.NoError(t, err)
	assert.True(t, prov.called)
	assert.Equal(t, int64(8192), extractMaxTokensOption(t, prov.receivedOpts))
}

// TestSummarize_AppliesCustomMaxTokens asserts that a non-zero
// MaxOutputTokens is forwarded verbatim.
func TestSummarize_AppliesCustomMaxTokens(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary."},
		},
	}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
	}

	_, err := Summarize(context.Background(), prov, models.Spec{MaxOutputTokens: 4096}, turns)
	require.NoError(t, err)
	assert.True(t, prov.called)
	assert.Equal(t, int64(4096), extractMaxTokensOption(t, prov.receivedOpts))
}

// TestSummarize_NegativeMaxTokensFallsBackToDefault asserts that an
// explicit negative value falls back to the default.
func TestSummarize_NegativeMaxTokensFallsBackToDefault(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary."},
		},
	}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
	}

	_, err := Summarize(context.Background(), prov, models.Spec{MaxOutputTokens: -1}, turns)
	require.NoError(t, err)
	assert.Equal(t, int64(8192), extractMaxTokensOption(t, prov.receivedOpts))
}

// TestSummarize_TruncatedResultReturnsError asserts the central
// failure mode: when the provider's final StopReason is Length,
// Summarize returns the zero turn wrapped with ErrTruncatedSummary.
// Callers are expected to NOT append anything to the buffer.
func TestSummarize_TruncatedResultReturnsError(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "##"},
			artifact.StopReason{Reason: artifact.StopReasonLength},
		},
	}

	turns := []state.Turn{
		textTurn(state.RoleUser, "a very long history to summarize"),
		textTurn(state.RoleAssistant, "first response"),
		textTurn(state.RoleUser, "more context"),
		textTurn(state.RoleAssistant, "more response"),
	}

	result, err := Summarize(context.Background(), prov, models.Spec{}, turns)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTruncatedSummary)
	// Zero turn returned so the caller doesn't accidentally append.
	assert.Equal(t, state.Turn{}, result)
}

// TestSummarize_StopReasonStopDoesNotError asserts that a normal
// completion (StopReasonStop) does not trigger ErrTruncatedSummary.
func TestSummarize_StopReasonStopDoesNotError(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary content."},
			artifact.StopReason{Reason: artifact.StopReasonStop},
		},
	}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
	}

	result, err := Summarize(context.Background(), prov, models.Spec{}, turns)
	require.NoError(t, err)
	summary := findText(t, result)
	assert.Equal(t, "Summary content.", summary.Content)
}

// TestSummarize_StopReasonToolUseDoesNotError asserts that a
// tool_use reason does not trigger ErrTruncatedSummary.
func TestSummarize_StopReasonToolUseDoesNotError(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Partial summary "},
			artifact.Text{Content: "before tool call."},
			artifact.StopReason{Reason: artifact.StopReasonToolUse},
		},
	}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
	}

	result, err := Summarize(context.Background(), prov, models.Spec{}, turns)
	require.NoError(t, err)
	summary := findText(t, result)
	assert.Equal(t, "Partial summary before tool call.", summary.Content)
}

// TestSummarize_NoStopReasonDoesNotError asserts that the absence
// of a StopReason is treated as "no truncation reported".
func TestSummarize_NoStopReasonDoesNotError(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary."},
		},
	}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
	}

	result, err := Summarize(context.Background(), prov, models.Spec{}, turns)
	require.NoError(t, err)
	summary := findText(t, result)
	assert.Equal(t, "Summary.", summary.Content)
}

// TestSummarize_StopReason_InterleavedTextAndReason asserts that
// the channel-drain goroutine captures the latest non-empty
// StopReason even when interleaved with text deltas.
func TestSummarize_StopReason_InterleavedTextAndReason(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Summary "},
			artifact.TextDelta{Content: "content."},
			artifact.StopReason{Reason: artifact.StopReasonLength},
		},
	}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
	}

	_, err := Summarize(context.Background(), prov, models.Spec{}, turns)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTruncatedSummary)
}

// TestSummarize_DroppedTokenEstimateFromDroppedArtifacts asserts
// that the artifact.Compaction.DroppedTokenEstimate reflects the
// sum of llmbytes.Of over every artifact in the input slice.
func TestSummarize_DroppedTokenEstimateFromDroppedArtifacts(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary."},
		},
	}

	// 3 turns with 2 text artifacts each: the DroppedTokenEstimate
	// should equal the sum of byte lengths of those text artifacts
	// (4 chars * 3 turns * 2 artifacts = 24 bytes, plus any
	// additional fixed costs — but artifact.Text byte size is just
	// len(Content), so 8+8 + 5+5 + 3+3 = 32? Let me use clearly
	// sized content to avoid arithmetic errors in the assertion).
	turns := []state.Turn{
		{
			Role: state.RoleUser,
			Artifacts: []artifact.Artifact{
				artifact.Text{Content: "1234567890"}, // 10 bytes
				artifact.Text{Content: "12345"},     // 5 bytes
			},
		},
		{
			Role: state.RoleAssistant,
			Artifacts: []artifact.Artifact{
				artifact.Text{Content: "12345"}, // 5 bytes
			},
		},
	}

	result, err := Summarize(context.Background(), prov, models.Spec{}, turns)
	require.NoError(t, err)
	comp := findCompaction(t, result)
	assert.Equal(t, int64(20), comp.DroppedTokenEstimate, "should sum bytes of all dropped artifacts")
}
