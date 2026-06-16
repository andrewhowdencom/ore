package compaction

import (
	"github.com/andrewhowdencom/ore/models"
	"context"
	"errors"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_NoOpts_NeverCompacts(t *testing.T) {
	c := New()
	turns := []state.Turn{
		{Role: state.RoleUser},
		{Role: state.RoleAssistant},
		{Role: state.RoleUser},
	}
	result, didCompact, err := c.MaybeCompact(context.Background(), turns)
	require.NoError(t, err)
	assert.False(t, didCompact)
	assert.Equal(t, turns, result)
}

func TestMaybeCompact_TriggerDoesNotFire(t *testing.T) {
	c := New(
		WithTrigger(TurnCountTrigger{N: 3}),
		WithStrategy(dropFirstN{n: 2}),
	)
	turns := []state.Turn{
		{Role: state.RoleUser},
		{Role: state.RoleAssistant},
		{Role: state.RoleUser},
	}
	result, didCompact, err := c.MaybeCompact(context.Background(), turns)
	require.NoError(t, err)
	assert.False(t, didCompact)
	assert.Equal(t, turns, result)
}

func TestMaybeCompact_TriggerFires(t *testing.T) {
	c := New(
		WithTrigger(TurnCountTrigger{N: 3}),
		WithStrategy(dropFirstN{n: 2}),
	)
	turns := []state.Turn{
		{Role: state.RoleSystem},
		{Role: state.RoleUser},
		{Role: state.RoleAssistant},
		{Role: state.RoleUser},
	}
	result, didCompact, err := c.MaybeCompact(context.Background(), turns)
	require.NoError(t, err)
	assert.True(t, didCompact)
	assert.Len(t, result, 2)
	assert.Equal(t, state.RoleAssistant, result[0].Role)
	assert.Equal(t, state.RoleUser, result[1].Role)
}

func TestMaybeCompact_StrategyError(t *testing.T) {
	c := New(
		WithTrigger(TurnCountTrigger{N: 0}),
		WithStrategy(errorStrategy{msg: "strategy error"}),
	)
	turns := []state.Turn{{Role: state.RoleUser}}
	result, didCompact, err := c.MaybeCompact(context.Background(), turns)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "strategy error")
	assert.False(t, didCompact)
	assert.Equal(t, turns, result)
}

func TestMaybeCompact_NilTrigger_WithStrategy(t *testing.T) {
	c := New(
		WithStrategy(dropFirstN{n: 1}),
	)
	turns := []state.Turn{
		{Role: state.RoleUser},
		{Role: state.RoleAssistant},
	}
	result, didCompact, err := c.MaybeCompact(context.Background(), turns)
	require.NoError(t, err)
	assert.False(t, didCompact)
	assert.Equal(t, turns, result)
}

func TestTurnCountTrigger(t *testing.T) {
	tests := []struct {
		name     string
		triggerN int
		turns    int
		want     bool
	}{
		{"zero turns, N=1", 1, 0, false},
		{"one turn, N=1", 1, 1, false},
		{"two turns, N=1", 1, 2, true},
		{"exact threshold", 3, 3, false},
		{"one over threshold", 3, 4, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := TurnCountTrigger{N: tt.triggerN}
			turns := make([]state.Turn, tt.turns)
			assert.Equal(t, tt.want, tr.ShouldCompact(turns))
		})
	}
}

func TestTokenUsageTrigger(t *testing.T) {
	tests := []struct {
		name      string
		maxTokens int
		ratio     float64
		turns     []state.Turn
		want      bool
	}{
		{
			name:      "fires when Usage exceeds 80% of Window (default)",
			maxTokens: 10000,
			turns: []state.Turn{
				{Role: state.RoleUser, Artifacts: []artifact.Artifact{artifact.Usage{TotalTokens: 8001}}},
			},
			want: true,
		},
		{
			name:      "does not fire at 79% of Window (default)",
			maxTokens: 10000,
			turns: []state.Turn{
				{Role: state.RoleUser, Artifacts: []artifact.Artifact{artifact.Usage{TotalTokens: 7900}}},
			},
			want: false,
		},
		{
			name:      "Ratio 1.0 fires at exact Window",
			maxTokens: 10000,
			ratio:    1.0,
			turns: []state.Turn{
				{Role: state.RoleUser, Artifacts: []artifact.Artifact{artifact.Usage{TotalTokens: 10000}}},
			},
			want: true,
		},
		{
			name:      "Ratio 1.0 does not fire just below Window",
			maxTokens: 10000,
			ratio:    1.0,
			turns: []state.Turn{
				{Role: state.RoleUser, Artifacts: []artifact.Artifact{artifact.Usage{TotalTokens: 9999}}},
			},
			want: false,
		},
		{
			name:      "Ratio 0.5 fires at half Window",
			maxTokens: 10000,
			ratio:    0.5,
			turns: []state.Turn{
				{Role: state.RoleUser, Artifacts: []artifact.Artifact{artifact.Usage{TotalTokens: 5000}}},
			},
			want: true,
		},
		{
			name:      "does not fire when no Usage artifact",
			maxTokens: 10000,
			turns: []state.Turn{
				{Role: state.RoleUser, Artifacts: []artifact.Artifact{artifact.Text{Content: "hello"}}},
			},
			want: false,
		},
		{
			name:      "inspects most recent Usage only",
			maxTokens: 10000,
			turns: []state.Turn{
				{Role: state.RoleUser, Artifacts: []artifact.Artifact{artifact.Usage{TotalTokens: 10001}}},
				{Role: state.RoleAssistant, Artifacts: []artifact.Artifact{artifact.Usage{TotalTokens: 100}}},
			},
			want: false,
		},
		{
			name:      "empty turns",
			maxTokens: 10000,
			turns:     []state.Turn{},
			want:      false,
		},
		{
			name:      "zero Window returns false (graceful degradation)",
			maxTokens: 0,
			turns: []state.Turn{
				{Role: state.RoleUser, Artifacts: []artifact.Artifact{artifact.Usage{TotalTokens: 999999}}},
			},
			want: false,
		},
		{
			name:      "negative Ratio falls back to default (does not fire)",
			maxTokens: 10000,
			ratio:    -1,
			turns: []state.Turn{
				{Role: state.RoleUser, Artifacts: []artifact.Artifact{artifact.Usage{TotalTokens: 7999}}},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr := TokenUsageTrigger{
				Spec:  models.Spec{Window: tt.maxTokens},
				Ratio: tt.ratio,
			}
			assert.Equal(t, tt.want, tr.ShouldCompact(tt.turns))
		})
	}
}

// errorStrategy is a test double that always returns an error.
type errorStrategy struct {
	msg string
}

func (e errorStrategy) Compact(_ context.Context, turns []state.Turn) ([]state.Turn, error) {
	return turns, errors.New(e.msg)
}

// dropFirstN is a test double that drops the first N turns.
type dropFirstN struct {
	n int
}

func (d dropFirstN) Compact(_ context.Context, turns []state.Turn) ([]state.Turn, error) {
	if d.n >= len(turns) {
		return turns, nil
	}
	return turns[d.n:], nil
}

func TestMaybeCompact_MultipleStrategies(t *testing.T) {
	c := New(
		WithTrigger(TurnCountTrigger{N: 3}),
		WithStrategy(dropFirstN{n: 2}),
		WithStrategy(dropFirstN{n: 2}),
	)
	turns := []state.Turn{
		{Role: state.RoleSystem},
		{Role: state.RoleUser},
		{Role: state.RoleAssistant},
		{Role: state.RoleUser},
		{Role: state.RoleAssistant},
	}
	result, didCompact, err := c.MaybeCompact(context.Background(), turns)
	require.NoError(t, err)
	assert.True(t, didCompact)
	assert.Len(t, result, 1)
	assert.Equal(t, state.RoleAssistant, result[0].Role)
}

func TestMaybeCompact_MultipleStrategies_ErrorPropagation(t *testing.T) {
	c := New(
		WithTrigger(TurnCountTrigger{N: 0}),
		WithStrategy(dropFirstN{n: 2}),
		WithStrategy(errorStrategy{msg: "second strategy failed"}),
	)
	turns := []state.Turn{
		{Role: state.RoleSystem},
		{Role: state.RoleUser},
		{Role: state.RoleAssistant},
	}
	result, didCompact, err := c.MaybeCompact(context.Background(), turns)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "compaction strategy failed")
	assert.Contains(t, err.Error(), "second strategy failed")
	assert.False(t, didCompact)
	assert.Equal(t, turns, result)
}

func TestWithStrategy_NilIgnored(t *testing.T) {
	c := New(
		WithTrigger(TurnCountTrigger{N: 0}),
		WithStrategy(nil),
		WithStrategy(dropFirstN{n: 1}),
	)
	turns := []state.Turn{
		{Role: state.RoleUser},
		{Role: state.RoleAssistant},
	}
	result, didCompact, err := c.MaybeCompact(context.Background(), turns)
	require.NoError(t, err)
	assert.True(t, didCompact)
	assert.Len(t, result, 1)
}

func TestMaybeCompact_TriggerOnly_NeverCompacts(t *testing.T) {
	c := New(
		WithTrigger(TurnCountTrigger{N: 0}),
	)
	turns := []state.Turn{{Role: state.RoleUser}}
	result, didCompact, err := c.MaybeCompact(context.Background(), turns)
	require.NoError(t, err)
	assert.False(t, didCompact)
	assert.Equal(t, turns, result)
}

func TestForceCompact_BypassesTrigger(t *testing.T) {
	c := New(
		WithTrigger(TurnCountTrigger{N: 100}), // trigger won't fire
		WithStrategy(dropFirstN{n: 1}),
	)
	turns := []state.Turn{
		{Role: state.RoleSystem},
		{Role: state.RoleUser},
		{Role: state.RoleAssistant},
	}
	result, didCompact, err := c.ForceCompact(context.Background(), turns)
	require.NoError(t, err)
	assert.True(t, didCompact)
	assert.Len(t, result, 2)
	assert.Equal(t, state.RoleUser, result[0].Role)
	assert.Equal(t, state.RoleAssistant, result[1].Role)
}

func TestForceCompact_NoStrategies(t *testing.T) {
	c := New(
		WithTrigger(TurnCountTrigger{N: 0}),
	)
	turns := []state.Turn{{Role: state.RoleUser}}
	result, didCompact, err := c.ForceCompact(context.Background(), turns)
	require.NoError(t, err)
	assert.False(t, didCompact)
	assert.Equal(t, turns, result)
}

func TestForceCompact_StrategyError(t *testing.T) {
	c := New(
		WithStrategy(errorStrategy{msg: "strategy error"}),
	)
	turns := []state.Turn{{Role: state.RoleUser}}
	result, didCompact, err := c.ForceCompact(context.Background(), turns)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "strategy error")
	assert.False(t, didCompact)
	assert.Equal(t, turns, result)
}

