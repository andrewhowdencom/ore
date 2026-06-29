package guardrails

import (
	"context"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransform_PrependsGuardrails(t *testing.T) {
	tr, err := New(WithRules("rule one", "rule two"))
	require.NoError(t, err)
	base := &ledger.Buffer{}
	base.Append(ledger.RoleUser, artifact.Text{Content: "hello"})

	result, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)

	turns := result.Turns()
	require.Len(t, turns, 3)
	assert.Equal(t, ledger.RoleUser, turns[0].Role)
	assert.Equal(t, ledger.RoleUser, turns[1].Role)
	assert.Equal(t, ledger.RoleUser, turns[2].Role)

	text0, ok := turns[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "rule one", text0.Content)

	text1, ok := turns[1].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "rule two", text1.Content)
}

func TestTransform_NoRules(t *testing.T) {
	tr, err := New()
	require.NoError(t, err)
	base := &ledger.Buffer{}
	base.Append(ledger.RoleUser, artifact.Text{Content: "hello"})

	result, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)

	// Identity optimization: should return base state directly
	assert.Equal(t, base, result)

	turns := result.Turns()
	require.Len(t, turns, 1)
}

func TestTransform_DelegatesAppend(t *testing.T) {
	tr, err := New(WithRules("rule"))
	require.NoError(t, err)
	base := &ledger.Buffer{}
	base.Append(ledger.RoleUser, artifact.Text{Content: "user"})

	result, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)

	result.Append(ledger.RoleAssistant, artifact.Text{Content: "assistant"})

	baseTurns := base.Turns()
	require.Len(t, baseTurns, 2)
	assert.Equal(t, ledger.RoleAssistant, baseTurns[1].Role)

	turns := result.Turns()
	require.Len(t, turns, 3)
	assert.Equal(t, ledger.RoleUser, turns[0].Role)
	assert.Equal(t, ledger.RoleUser, turns[1].Role)
	assert.Equal(t, ledger.RoleAssistant, turns[2].Role)
}

func TestRules(t *testing.T) {
	tr, err := New(WithRules("a", "b"))
	require.NoError(t, err)
	assert.Equal(t, []string{"a", "b"}, tr.(*Transform).Rules())
}


