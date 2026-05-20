package guardrails

import (
	"context"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransform_PrependsGuardrails(t *testing.T) {
	tr := New(WithRules("rule one", "rule two"))
	base := &state.Buffer{}
	base.Append(state.RoleUser, artifact.Text{Content: "hello"})

	result, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)

	turns := result.Turns()
	require.Len(t, turns, 3)
	assert.Equal(t, state.RoleUser, turns[0].Role)
	assert.Equal(t, state.RoleUser, turns[1].Role)
	assert.Equal(t, state.RoleUser, turns[2].Role)

	text0, ok := turns[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "rule one", text0.Content)

	text1, ok := turns[1].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "rule two", text1.Content)
}

func TestTransform_NoRules(t *testing.T) {
	tr := New()
	base := &state.Buffer{}
	base.Append(state.RoleUser, artifact.Text{Content: "hello"})

	result, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)

	// Identity optimization: should return base state directly
	assert.Equal(t, base, result)

	turns := result.Turns()
	require.Len(t, turns, 1)
}

func TestTransform_DelegatesAppend(t *testing.T) {
	tr := New(WithRules("rule"))
	base := &state.Buffer{}
	base.Append(state.RoleUser, artifact.Text{Content: "user"})

	result, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)

	result.Append(state.RoleAssistant, artifact.Text{Content: "assistant"})

	baseTurns := base.Turns()
	require.Len(t, baseTurns, 2)
	assert.Equal(t, state.RoleAssistant, baseTurns[1].Role)

	turns := result.Turns()
	require.Len(t, turns, 3)
	assert.Equal(t, state.RoleUser, turns[0].Role)
	assert.Equal(t, state.RoleUser, turns[1].Role)
	assert.Equal(t, state.RoleAssistant, turns[2].Role)
}

func TestRules(t *testing.T) {
	tr := New(WithRules("a", "b")).(*Transform)
	assert.Equal(t, []string{"a", "b"}, tr.Rules())
}

func TestFromConfig(t *testing.T) {
	opts := FromConfig(Config{Rules: []string{"a", "b"}})
	require.Len(t, opts, 1)

	tr := New(opts...).(*Transform)
	assert.Equal(t, []string{"a", "b"}, tr.Rules())
}

func TestFromConfig_Empty(t *testing.T) {
	opts := FromConfig(Config{})
	assert.Empty(t, opts)
}

func TestOptionsFromMap(t *testing.T) {
	opts, err := OptionsFromMap(map[string]any{"rules": []any{"one", "two"}})
	require.NoError(t, err)
	require.Len(t, opts, 1)

	tr := New(opts...).(*Transform)
	assert.Equal(t, []string{"one", "two"}, tr.Rules())
}

func TestOptionsFromMap_Empty(t *testing.T) {
	opts, err := OptionsFromMap(map[string]any{})
	require.NoError(t, err)
	assert.Empty(t, opts)
}
