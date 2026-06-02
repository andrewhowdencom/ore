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
}

var _ provider.Provider = (*mockProvider)(nil)

func (m *mockProvider) Invoke(ctx context.Context, s state.State, ch chan<- artifact.Artifact, opts ...provider.InvokeOption) error {
	for _, art := range m.artifacts {
		select {
		case ch <- art:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return m.err
}

func TestSummarizeStrategy_ReducesTurns(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary of earlier discussion."},
		},
	}

	strategy := SummarizeStrategy{
		Provider:      prov,
		PreserveLastN: 2,
	}

	turns := []state.Turn{
		{Role: state.RoleUser, Artifacts: []artifact.Artifact{artifact.Text{Content: "hello"}}},
		{Role: state.RoleAssistant, Artifacts: []artifact.Artifact{artifact.Text{Content: "hi there"}}},
		{Role: state.RoleUser, Artifacts: []artifact.Artifact{artifact.Text{Content: "question 1"}}},
		{Role: state.RoleAssistant, Artifacts: []artifact.Artifact{artifact.Text{Content: "answer 1"}}},
		{Role: state.RoleUser, Artifacts: []artifact.Artifact{artifact.Text{Content: "question 2"}}},
	}

	result, err := strategy.Compact(context.Background(), turns)
	require.NoError(t, err)
	require.Len(t, result, 3)

	assert.Equal(t, state.RoleSystem, result[0].Role)
	require.Len(t, result[0].Artifacts, 1)
	text, ok := result[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "Summary of earlier discussion.", text.Content)

	assert.Equal(t, state.RoleAssistant, result[1].Role)
	assert.Equal(t, state.RoleUser, result[2].Role)
}

func TestSummarizeStrategy_NoOpWhenUnderThreshold(t *testing.T) {
	prov := &mockProvider{}

	strategy := SummarizeStrategy{
		Provider:      prov,
		PreserveLastN: 5,
	}

	turns := []state.Turn{
		{Role: state.RoleUser},
		{Role: state.RoleAssistant},
	}

	result, err := strategy.Compact(context.Background(), turns)
	require.NoError(t, err)
	assert.Len(t, result, 2)
	assert.NotSame(t, &turns[0], &result[0])
}

func TestSummarizeStrategy_NoOpExactThreshold(t *testing.T) {
	prov := &mockProvider{}

	strategy := SummarizeStrategy{
		Provider:      prov,
		PreserveLastN: 2,
	}

	turns := []state.Turn{
		{Role: state.RoleUser},
		{Role: state.RoleAssistant},
	}

	result, err := strategy.Compact(context.Background(), turns)
	require.NoError(t, err)
	assert.Len(t, result, 2)
}

func TestSummarizeStrategy_PropagatesProviderError(t *testing.T) {
	wantErr := errors.New("provider failure")
	prov := &mockProvider{err: wantErr}

	strategy := SummarizeStrategy{
		Provider:      prov,
		PreserveLastN: 1,
	}

	turns := []state.Turn{
		{Role: state.RoleUser},
		{Role: state.RoleAssistant},
		{Role: state.RoleUser},
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
		Provider:      prov,
		PreserveLastN: 0,
	}

	turns := []state.Turn{
		{Role: state.RoleUser, Artifacts: []artifact.Artifact{artifact.Text{Content: "hello"}}},
	}

	result, err := strategy.Compact(context.Background(), turns)
	require.NoError(t, err)
	require.Len(t, result, 1)
	require.Len(t, result[0].Artifacts, 1)
	text, ok := result[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "Actual summary.", text.Content)
}

func TestSummarizeStrategy_NegativePreserveLastN(t *testing.T) {
	strategy := SummarizeStrategy{
		Provider:      &mockProvider{},
		PreserveLastN: -1,
	}

	turns := []state.Turn{{Role: state.RoleUser}}
	_, err := strategy.Compact(context.Background(), turns)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PreserveLastN must be >= 0")
}

func TestSummarizeStrategy_MultipleTextArtifactsConcatenated(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Part one. "},
			artifact.Text{Content: "Part two."},
		},
	}

	strategy := SummarizeStrategy{
		Provider:      prov,
		PreserveLastN: 1,
	}

	turns := []state.Turn{
		{Role: state.RoleUser, Artifacts: []artifact.Artifact{artifact.Text{Content: "a"}}},
		{Role: state.RoleAssistant, Artifacts: []artifact.Artifact{artifact.Text{Content: "b"}}},
		{Role: state.RoleUser, Artifacts: []artifact.Artifact{artifact.Text{Content: "c"}}},
	}

	result, err := strategy.Compact(context.Background(), turns)
	require.NoError(t, err)
	require.Len(t, result, 2)
	require.Len(t, result[0].Artifacts, 1)
	text, ok := result[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "Part one. Part two.", text.Content)
}

func TestSummarizeStrategy_ZeroPreserveLastN(t *testing.T) {
	prov := &mockProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Summary of everything."},
		},
	}

	strategy := SummarizeStrategy{
		Provider:      prov,
		PreserveLastN: 0,
	}

	turns := []state.Turn{
		{Role: state.RoleUser, Artifacts: []artifact.Artifact{artifact.Text{Content: "hello"}}},
		{Role: state.RoleAssistant, Artifacts: []artifact.Artifact{artifact.Text{Content: "world"}}},
	}

	result, err := strategy.Compact(context.Background(), turns)
	require.NoError(t, err)
	require.Len(t, result, 1)
	assert.Equal(t, state.RoleSystem, result[0].Role)
}
