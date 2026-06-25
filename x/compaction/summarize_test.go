package compaction

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/agent"
	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/cognitive"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubProvider is a test double implementing provider.Provider. It
// captures the state, spec, and options it received, then writes
// canned artifacts to the result channel and returns the configured
// error.
type stubProvider struct {
	artifacts    []artifact.Artifact
	err          error
	called       int32
	receivedSt   state.State
	receivedSpec models.Spec
	receivedOpts []provider.InvokeOption
}

var _ provider.Provider = (*stubProvider)(nil)

func (s *stubProvider) Invoke(_ context.Context, st state.State, spec models.Spec, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	atomic.AddInt32(&s.called, 1)
	s.receivedSt = st
	s.receivedSpec = spec
	s.receivedOpts = opts
	for _, a := range s.artifacts {
		ch <- a
	}
	return s.err
}

// newCompactorAgent builds an ephemeral SingleShot agent for the
// compactor tests, wired to the given provider. The agent is closed
// when the test ends.
func newCompactorAgent(t *testing.T, p provider.Provider) *agent.Agent {
	t.Helper()
	a := agent.New("test-compactor",
		agent.WithProvider(p),
		agent.WithSpec(models.Spec{}),
		agent.WithPattern(&cognitive.SingleShot{}),
	)
	t.Cleanup(func() { _ = a.Close() })
	return a
}

func textTurn(role state.Role, content string) state.Turn {
	return state.Turn{
		Role:      role,
		Artifacts: []artifact.Artifact{artifact.Text{Content: content}},
	}
}

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

func TestSummarize_EmptyTurns_ReturnsZeroValuesNoError(t *testing.T) {
	stub := &stubProvider{}
	a := newCompactorAgent(t, stub)

	turn, info, err := Summarize(context.Background(), a, []state.Turn{})
	require.NoError(t, err)
	assert.Equal(t, state.Turn{}, turn, "empty turns return the zero turn")
	assert.Equal(t, BoundaryInfo{}, info, "empty turns return zero BoundaryInfo")
	assert.Equal(t, int32(0), atomic.LoadInt32(&stub.called), "provider should not be called for empty turns")
}

func TestSummarize_ProducesTextOnlyTurn(t *testing.T) {
	stub := &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary of earlier discussion."},
		},
	}
	a := newCompactorAgent(t, stub)

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
	}

	turn, info, err := Summarize(context.Background(), a, turns)
	require.NoError(t, err)
	assert.Equal(t, state.RoleSystem, turn.Role)

	// The turn carries exactly one Text artifact — no Compaction sibling.
	require.Len(t, turn.Artifacts, 1, "compaction turn must carry only the LLM-facing Text artifact")
	summary := findText(t, turn)
	assert.Equal(t, "Summary of earlier discussion.", summary.Content)

	// The companion BoundaryInfo carries the metadata that used to live in
	// artifact.Compaction. Callers write this to state.Meta themselves.
	assert.Equal(t, len(turns), info.CompactedThrough)
	assert.Equal(t, len(turns), info.DroppedTurnCount)
	assert.Equal(t, StrategyNameSummarize, info.Strategy)
	assert.False(t, info.CreatedAt.IsZero(), "CreatedAt should be set")
	assert.Greater(t, info.DroppedTokenEstimate, int64(0), "DroppedTokenEstimate should reflect bytes of dropped artifacts")
}

func TestSummarize_PropagatesAgentError(t *testing.T) {
	wantErr := errors.New("provider failure")
	stub := &stubProvider{err: wantErr}
	a := newCompactorAgent(t, stub)

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
	}

	_, _, err := Summarize(context.Background(), a, turns)
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
	assert.Contains(t, err.Error(), "summarization agent run failed")
}

func TestSummarize_IgnoresNonTextArtifacts(t *testing.T) {
	stub := &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.Reasoning{Content: "thinking..."},
			artifact.Text{Content: "Actual summary."},
			artifact.Usage{TotalTokens: 42},
			artifact.ToolCall{Name: "test"},
		},
	}
	a := newCompactorAgent(t, stub)

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

	turn, _, err := Summarize(context.Background(), a, turns)
	require.NoError(t, err)
	summary := findText(t, turn)
	assert.Equal(t, "Actual summary.", summary.Content)
}

func TestSummarize_MultipleTextArtifactsConcatenated(t *testing.T) {
	stub := &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Part one. "},
			artifact.Text{Content: "Part two."},
		},
	}
	a := newCompactorAgent(t, stub)

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
	}

	turn, _, err := Summarize(context.Background(), a, turns)
	require.NoError(t, err)
	summary := findText(t, turn)
	assert.Equal(t, "Part one. Part two.", summary.Content)
}

func TestSummarize_TextDeltaArtifacts(t *testing.T) {
	stub := &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Summary part one. "},
			artifact.TextDelta{Content: "Summary part two."},
		},
	}
	a := newCompactorAgent(t, stub)

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
	}

	turn, _, err := Summarize(context.Background(), a, turns)
	require.NoError(t, err)
	summary := findText(t, turn)
	assert.Equal(t, "Summary part one. Summary part two.", summary.Content)
}

func TestSummarize_MixedTextAndTextDelta(t *testing.T) {
	stub := &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Part A. "},
			artifact.TextDelta{Content: "Part B."},
		},
	}
	a := newCompactorAgent(t, stub)

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
	}

	turn, _, err := Summarize(context.Background(), a, turns)
	require.NoError(t, err)
	summary := findText(t, turn)
	assert.Equal(t, "Part A. Part B.", summary.Content)
}

func TestSummarize_NoTextArtifacts(t *testing.T) {
	stub := &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.Usage{TotalTokens: 42},
			artifact.Reasoning{Content: "thinking..."},
			artifact.ToolCall{Name: "test"},
		},
	}
	a := newCompactorAgent(t, stub)

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "bbbb"),
	}

	turn, _, err := Summarize(context.Background(), a, turns)
	require.NoError(t, err)
	assert.Equal(t, state.RoleSystem, turn.Role)
	require.Len(t, turn.Artifacts, 1, "compaction turn carries exactly one Text artifact, even when empty")
	summary := findText(t, turn)
	assert.Empty(t, summary.Content)
}

func TestSummarize_TimestampNonZero(t *testing.T) {
	stub := &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary."},
		},
	}
	a := newCompactorAgent(t, stub)

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
	}

	turn, info, err := Summarize(context.Background(), a, turns)
	require.NoError(t, err)
	assert.False(t, turn.Timestamp.IsZero())
	assert.False(t, info.CreatedAt.IsZero(), "BoundaryInfo.CreatedAt must be set")
}

func TestSummarize_UsesDefaultPrompt(t *testing.T) {
	stub := &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary."},
		},
	}
	a := newCompactorAgent(t, stub)

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
	}

	_, _, err := Summarize(context.Background(), a, turns)
	require.NoError(t, err)
	require.NotNil(t, stub.receivedSt)
	receivedTurns := stub.receivedSt.Turns()
	require.Len(t, receivedTurns, len(turns)+1)
	assert.Equal(t, state.RoleUser, receivedTurns[len(receivedTurns)-1].Role)
	require.Len(t, receivedTurns[len(receivedTurns)-1].Artifacts, 1)
	prompt, ok := receivedTurns[len(receivedTurns)-1].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, defaultPrompt, prompt.Content)
}

func TestSummarize_PassesAllTurnsPlusPrompt(t *testing.T) {
	stub := &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary."},
		},
	}
	a := newCompactorAgent(t, stub)

	turns := []state.Turn{
		textTurn(state.RoleUser, "u1"),
		textTurn(state.RoleAssistant, "a1"),
		textTurn(state.RoleUser, "u2"),
		textTurn(state.RoleAssistant, "a2"),
	}

	_, _, err := Summarize(context.Background(), a, turns)
	require.NoError(t, err)
	require.NotNil(t, stub.receivedSt)
	receivedTurns := stub.receivedSt.Turns()
	require.Len(t, receivedTurns, len(turns)+1)
	assert.Equal(t, state.RoleUser, receivedTurns[0].Role)
	assert.Equal(t, state.RoleAssistant, receivedTurns[1].Role)
	assert.Equal(t, state.RoleUser, receivedTurns[2].Role)
	assert.Equal(t, state.RoleAssistant, receivedTurns[3].Role)
	assert.Equal(t, state.RoleUser, receivedTurns[4].Role)
}

func TestSummarize_TruncatedResultReturnsError(t *testing.T) {
	stub := &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "##"},
			artifact.StopReason{Reason: artifact.StopReasonLength},
		},
	}
	a := newCompactorAgent(t, stub)

	turns := []state.Turn{
		textTurn(state.RoleUser, "a very long history to summarize"),
		textTurn(state.RoleAssistant, "first response"),
		textTurn(state.RoleUser, "more context"),
		textTurn(state.RoleAssistant, "more response"),
	}

	turn, info, err := Summarize(context.Background(), a, turns)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTruncatedSummary)
	assert.Equal(t, state.Turn{}, turn, "zero turn returned so the caller doesn't accidentally append")
	assert.Equal(t, BoundaryInfo{}, info, "zero BoundaryInfo returned on truncation")
}

func TestSummarize_StopReasonStopDoesNotError(t *testing.T) {
	stub := &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary content."},
			artifact.StopReason{Reason: artifact.StopReasonStop},
		},
	}
	a := newCompactorAgent(t, stub)

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
	}

	turn, _, err := Summarize(context.Background(), a, turns)
	require.NoError(t, err)
	summary := findText(t, turn)
	assert.Equal(t, "Summary content.", summary.Content)
}

func TestSummarize_StopReasonToolUseDoesNotError(t *testing.T) {
	stub := &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Partial summary "},
			artifact.Text{Content: "before tool call."},
			artifact.StopReason{Reason: artifact.StopReasonToolUse},
		},
	}
	a := newCompactorAgent(t, stub)

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
	}

	turn, _, err := Summarize(context.Background(), a, turns)
	require.NoError(t, err)
	summary := findText(t, turn)
	assert.Equal(t, "Partial summary before tool call.", summary.Content)
}

func TestSummarize_NoStopReasonDoesNotError(t *testing.T) {
	stub := &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary."},
		},
	}
	a := newCompactorAgent(t, stub)

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
	}

	turn, _, err := Summarize(context.Background(), a, turns)
	require.NoError(t, err)
	summary := findText(t, turn)
	assert.Equal(t, "Summary.", summary.Content)
}

func TestSummarize_StopReason_InterleavedTextAndReason(t *testing.T) {
	stub := &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "Summary "},
			artifact.TextDelta{Content: "content."},
			artifact.StopReason{Reason: artifact.StopReasonLength},
		},
	}
	a := newCompactorAgent(t, stub)

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
	}

	_, _, err := Summarize(context.Background(), a, turns)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTruncatedSummary)
}

func TestSummarize_DroppedTokenEstimateFromDroppedArtifacts(t *testing.T) {
	stub := &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary."},
		},
	}
	a := newCompactorAgent(t, stub)

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

	_, info, err := Summarize(context.Background(), a, turns)
	require.NoError(t, err)
	assert.Equal(t, int64(20), info.DroppedTokenEstimate, "should sum bytes of all dropped artifacts")
}

func TestSummarize_SpecNameForwardedToBoundaryInfo(t *testing.T) {
	stub := &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary."},
		},
	}
	// Build agent with a non-empty spec.Name; the compactor's wrap
	// function forwards it to BoundaryInfo.Model.
	a := agent.New("test-compactor",
		agent.WithProvider(stub),
		agent.WithSpec(models.Spec{Name: "specific-model"}),
		agent.WithPattern(&cognitive.SingleShot{}),
	)
	t.Cleanup(func() { _ = a.Close() })

	turns := []state.Turn{textTurn(state.RoleUser, "aaaa")}

	_, info, err := Summarize(context.Background(), a, turns)
	require.NoError(t, err)
	assert.Equal(t, "specific-model", info.Model)
}

func TestBoundaryInfo_RoundTrip(t *testing.T) {
	// encodeBoundaryInfo followed by decodeBoundaryInfo must produce
	// a value that compares equal to the original.
	original := BoundaryInfo{
		CompactedThrough:     12,
		DroppedTurnCount:     12,
		DroppedTokenEstimate: 4096,
		Strategy:             "summarize",
		Model:                "claude-opus-4-5",
		CreatedAt:            time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	encoded, err := encodeBoundaryInfo(original)
	require.NoError(t, err)

	got, err := decodeBoundaryInfo(encoded)
	require.NoError(t, err)
	assert.Equal(t, original, got)
}

func TestBoundaryInfo_DecodeEmpty(t *testing.T) {
	// Decoding an empty string returns the zero BoundaryInfo without
	// error — this is the "no boundary set" signal.
	got, err := decodeBoundaryInfo("")
	require.NoError(t, err)
	assert.Equal(t, BoundaryInfo{}, got)
}