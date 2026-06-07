package compaction

import (
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
	artifacts []artifact.Artifact
	err       error
	called    bool
}

var _ provider.Provider = (*mockProvider)(nil)

func (m *mockProvider) Invoke(ctx context.Context, s state.State, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	m.called = true
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
		Provider:  prov,
		MaxTokens: 3,
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
	require.Len(t, result, 4)

	assert.Equal(t, state.RoleSystem, result[0].Role)
	require.Len(t, result[0].Artifacts, 1)
	text, ok := result[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "Summary of earlier discussion.", text.Content)

	assert.Equal(t, state.RoleAssistant, result[1].Role)
	assert.Equal(t, state.RoleUser, result[2].Role)
	assert.Equal(t, state.RoleAssistant, result[3].Role)
}

func TestSummarizeStrategy_NoOpWhenUnderBudget(t *testing.T) {
	prov := &mockProvider{}

	strategy := SummarizeStrategy{
		Provider:  prov,
		MaxTokens: 5,
	}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
	}

	result, err := strategy.Compact(context.Background(), turns)
	require.NoError(t, err)
	assert.False(t, prov.called)
	assert.Len(t, result, 2)
	assert.NotSame(t, &turns[0], &result[0])
}

func TestSummarizeStrategy_NoOpExactBudget(t *testing.T) {
	prov := &mockProvider{}

	strategy := SummarizeStrategy{
		Provider:  prov,
		MaxTokens: 3,
	}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
	}

	result, err := strategy.Compact(context.Background(), turns)
	require.NoError(t, err)
	assert.False(t, prov.called)
	assert.Len(t, result, 3)
	assert.NotSame(t, &turns[0], &result[0])
}

func TestSummarizeStrategy_PropagatesProviderError(t *testing.T) {
	wantErr := errors.New("provider failure")
	prov := &mockProvider{err: wantErr}

	strategy := SummarizeStrategy{
		Provider:  prov,
		MaxTokens: 2,
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
		Provider:  prov,
		MaxTokens: 1,
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
	require.Len(t, result, 2)

	require.Len(t, result[0].Artifacts, 1)
	text, ok := result[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "Actual summary.", text.Content)

	assert.Equal(t, state.RoleAssistant, result[1].Role)
}

func TestSummarizeStrategy_MultipleTextArtifactsConcatenated(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Part one. "},
			artifact.Text{Content: "Part two."},
		},
	}

	strategy := SummarizeStrategy{
		Provider:  prov,
		MaxTokens: 1,
	}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
	}

	result, err := strategy.Compact(context.Background(), turns)
	require.NoError(t, err)
	assert.True(t, prov.called)
	require.Len(t, result, 2)

	require.Len(t, result[0].Artifacts, 1)
	text, ok := result[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "Part one. Part two.", text.Content)

	assert.Equal(t, state.RoleUser, result[1].Role)
}

func TestSummarizeStrategy_ZeroMaxTokens(t *testing.T) {
	strategy := SummarizeStrategy{
		Provider:  &mockProvider{},
		MaxTokens: 0,
	}

	turns := []state.Turn{textTurn(state.RoleUser, "aaaa")}
	_, err := strategy.Compact(context.Background(), turns)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "MaxTokens must be > 0")
}

func TestSummarizeStrategy_LastTurnAloneExceedsBudget(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary of turn 0."},
		},
	}

	strategy := SummarizeStrategy{
		Provider:  prov,
		MaxTokens: 3,
	}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaaaaaaaaaaaaaa"),
	}

	result, err := strategy.Compact(context.Background(), turns)
	require.NoError(t, err)
	assert.True(t, prov.called)
	require.Len(t, result, 2)

	assert.Equal(t, state.RoleSystem, result[0].Role)
	require.Len(t, result[0].Artifacts, 1)
	text, ok := result[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "Summary of turn 0.", text.Content)

	assert.Equal(t, state.RoleAssistant, result[1].Role)
}

func TestSummarizeStrategy_EmptyTurns(t *testing.T) {
	prov := &mockProvider{}

	strategy := SummarizeStrategy{
		Provider:  prov,
		MaxTokens: 5,
	}

	result, err := strategy.Compact(context.Background(), []state.Turn{})
	require.NoError(t, err)
	assert.False(t, prov.called)
	assert.Empty(t, result)
}

func TestSummarizeStrategy_SingleTurnUnderBudget(t *testing.T) {
	prov := &mockProvider{}

	strategy := SummarizeStrategy{
		Provider:  prov,
		MaxTokens: 5,
	}

	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
	}

	result, err := strategy.Compact(context.Background(), turns)
	require.NoError(t, err)
	assert.False(t, prov.called)
	assert.Len(t, result, 1)
	assert.Equal(t, state.RoleUser, result[0].Role)
	assert.NotSame(t, &turns[0], &result[0])
}
