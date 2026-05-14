package state

import (
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuffer_Empty(t *testing.T) {
	m := &Buffer{}
	assert.Empty(t, m.Turns())
}

func TestBuffer_AppendAndTurns(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*Buffer)
		wantLen  int
		wantRole Role
		wantKind string
	}{
		{
			name: "single user turn",
			setup: func(m *Buffer) {
				m.Append(RoleUser, artifact.Text{Content: "hello"})
			},
			wantLen:  1,
			wantRole: RoleUser,
			wantKind: "text",
		},
		{
			name: "multiple turns",
			setup: func(m *Buffer) {
				m.Append(RoleSystem, artifact.Text{Content: "sys"})
				m.Append(RoleUser, artifact.Text{Content: "usr"})
				m.Append(RoleAssistant, artifact.Text{Content: "asst"})
			},
			wantLen:  3,
			wantRole: RoleAssistant,
			wantKind: "text",
		},
		{
			name: "turn with multiple artifacts",
			setup: func(m *Buffer) {
				m.Append(RoleAssistant,
					artifact.Text{Content: "text1"},
					artifact.ToolCall{Name: "foo"},
				)
			},
			wantLen:  1,
			wantRole: RoleAssistant,
			wantKind: "text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Buffer{}
			tt.setup(m)

			turns := m.Turns()
			assert.Len(t, turns, tt.wantLen)

			last := turns[len(turns)-1]
			assert.Equal(t, tt.wantRole, last.Role)
			require.NotEmpty(t, last.Artifacts, "last turn has no artifacts")
			assert.Equal(t, tt.wantKind, last.Artifacts[0].Kind())
		})
	}
}

func TestBuffer_TurnsDefensiveCopy(t *testing.T) {
	m := &Buffer{}
	m.Append(RoleUser, artifact.Text{Content: "hello"})

	turns := m.Turns()
	require.Len(t, turns, 1)

	// Mutate the returned slice — should not affect internal state.
	_ = append(turns, Turn{Role: RoleAssistant})
	assert.Len(t, m.Turns(), 1, "modifying returned slice affected internal state")
}

func TestBuffer_AppendZeroArtifacts(t *testing.T) {
	m := &Buffer{}
	m.Append(RoleSystem)

	turns := m.Turns()
	require.Len(t, turns, 1)
	assert.Equal(t, RoleSystem, turns[0].Role)
	assert.Empty(t, turns[0].Artifacts)
}
