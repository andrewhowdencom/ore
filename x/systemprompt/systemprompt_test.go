package systemprompt

import (
	"context"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransform_PrependSystemPrompt(t *testing.T) {
	tr, err := New(WithContent("You are a helpful assistant."))
	require.NoError(t, err)
	base := &state.Buffer{}
	base.Append(state.RoleUser, artifact.Text{Content: "hello"})

	result, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)

	turns := result.Turns()
	require.Len(t, turns, 2)
	assert.Equal(t, state.RoleSystem, turns[0].Role)
	assert.Equal(t, state.RoleUser, turns[1].Role)

	text, ok := turns[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "You are a helpful assistant.", text.Content)
}

func TestTransform_EmptyContent(t *testing.T) {
	tr, err := New()
	require.NoError(t, err)
	base := &state.Buffer{}
	base.Append(state.RoleUser, artifact.Text{Content: "hello"})

	result, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)

	turns := result.Turns()
	require.Len(t, turns, 2)
	assert.Equal(t, state.RoleSystem, turns[0].Role)

	text, ok := turns[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Empty(t, text.Content)
}

func TestTransform_DelegatesAppend(t *testing.T) {
	tr, err := New(WithContent("system"))
	require.NoError(t, err)
	base := &state.Buffer{}
	base.Append(state.RoleUser, artifact.Text{Content: "user"})

	result, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)

	result.Append(state.RoleAssistant, artifact.Text{Content: "assistant"})

	// Base state should have the appended turn
	baseTurns := base.Turns()
	require.Len(t, baseTurns, 2)
	assert.Equal(t, state.RoleAssistant, baseTurns[1].Role)

	// Wrapped view should have virtual + base + appended
	turns := result.Turns()
	require.Len(t, turns, 3)
	assert.Equal(t, state.RoleSystem, turns[0].Role)
	assert.Equal(t, state.RoleUser, turns[1].Role)
	assert.Equal(t, state.RoleAssistant, turns[2].Role)
}


