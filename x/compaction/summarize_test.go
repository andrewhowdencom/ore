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
	artifacts     []artifact.Artifact
	err           error
	called        bool
	receivedTurns []state.Turn
}

var _ provider.Provider = (*mockProvider)(nil)

func (m *mockProvider) Invoke(ctx context.Context, s state.State, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	m.called = true
	m.receivedTurns = s.Turns()
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
		Provider:  prov,
		MaxTokens: 1,
		Prompt:    "",
	}

	// 3 turns of 1 token each; total = 3 > 1 triggers summarization.
	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
	}

	_, err := strategy.Compact(context.Background(), turns)
	require.NoError(t, err)
	assert.True(t, prov.called)

	// Provider receives toSummarize (2 turns) + appended user prompt.
	require.Len(t, prov.receivedTurns, 3)
	assert.Equal(t, state.RoleUser, prov.receivedTurns[2].Role)
	require.Len(t, prov.receivedTurns[2].Artifacts, 1)
	text, ok := prov.receivedTurns[2].Artifacts[0].(artifact.Text)
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
		Provider:  prov,
		MaxTokens: 1,
		Prompt:    "Custom override prompt",
	}

	// 3 turns of 1 token each; total = 3 > 1 triggers summarization.
	turns := []state.Turn{
		textTurn(state.RoleUser, "aaaa"),
		textTurn(state.RoleAssistant, "aaaa"),
		textTurn(state.RoleUser, "aaaa"),
	}

	_, err := strategy.Compact(context.Background(), turns)
	require.NoError(t, err)
	assert.True(t, prov.called)

	// Provider receives toSummarize (2 turns) + appended user prompt.
	require.Len(t, prov.receivedTurns, 3)
	assert.Equal(t, state.RoleUser, prov.receivedTurns[2].Role)
	require.Len(t, prov.receivedTurns[2].Artifacts, 1)
	text, ok := prov.receivedTurns[2].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "Custom override prompt", text.Content)
}
