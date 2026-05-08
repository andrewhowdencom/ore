package main

import (
	"context"
	"testing"

	"github.com/andrewhowdencom/tack/artifact"
	"github.com/andrewhowdencom/tack/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestModel_RenderDelta(t *testing.T) {
	m := newModel()
	err := m.RenderDelta(context.Background(), artifact.TextDelta{Content: "hello"})
	require.NoError(t, err)
	assert.Equal(t, "hello", m.streamBuffer.String())
}

func TestModel_RenderDelta_Reasoning(t *testing.T) {
	m := newModel()
	err := m.RenderDelta(context.Background(), artifact.ReasoningDelta{Content: "thinking"})
	require.NoError(t, err)
	assert.Equal(t, "thinking", m.streamBuffer.String())
}

func TestModel_RenderTurn(t *testing.T) {
	m := newModel()
	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "hello world"},
		},
	}
	err := m.RenderTurn(context.Background(), turn)
	require.NoError(t, err)
	require.Len(t, m.turns, 1)
	assert.Equal(t, state.RoleAssistant, m.turns[0].role)
	assert.Equal(t, "hello world", m.turns[0].text)
	assert.Empty(t, m.streamBuffer.String())
}

func TestModel_RenderTurn_ResetsStreamBuffer(t *testing.T) {
	m := newModel()
	m.streamBuffer.WriteString("partial")

	turn := state.Turn{
		Role: state.RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "complete"},
		},
	}
	err := m.RenderTurn(context.Background(), turn)
	require.NoError(t, err)
	assert.Empty(t, m.streamBuffer.String())
}

func TestModel_SetStatus(t *testing.T) {
	m := newModel()
	err := m.SetStatus(context.Background(), "thinking...")
	require.NoError(t, err)
	assert.Equal(t, "thinking...", m.status)
}

func TestModel_Events(t *testing.T) {
	m := newModel()
	ch := m.Events()
	require.NotNil(t, ch)
}
