package state

import (
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/stretchr/testify/assert"
)

func TestVirtualTurnState_Turns_PrependsVirtual(t *testing.T) {
	base := &Buffer{}
	base.Append(RoleUser, artifact.Text{Content: "user"})

	vts := NewVirtualTurnState(base, []Turn{
		{Role: RoleSystem, Artifacts: []artifact.Artifact{artifact.Text{Content: "system"}}},
	})

	turns := vts.Turns()
	assert.Len(t, turns, 2)
	assert.Equal(t, RoleSystem, turns[0].Role)
	assert.Equal(t, RoleUser, turns[1].Role)

	text, ok := turns[0].Artifacts[0].(artifact.Text)
	assert.True(t, ok)
	assert.Equal(t, "system", text.Content)
}

func TestVirtualTurnState_Turns_MultipleVirtual(t *testing.T) {
	base := &Buffer{}
	base.Append(RoleUser, artifact.Text{Content: "user"})

	vts := NewVirtualTurnState(base, []Turn{
		{Role: RoleSystem, Artifacts: []artifact.Artifact{artifact.Text{Content: "system1"}}},
		{Role: RoleSystem, Artifacts: []artifact.Artifact{artifact.Text{Content: "system2"}}},
	})

	turns := vts.Turns()
	assert.Len(t, turns, 3)
	assert.Equal(t, RoleSystem, turns[0].Role)
	assert.Equal(t, RoleSystem, turns[1].Role)
	assert.Equal(t, RoleUser, turns[2].Role)
}

func TestVirtualTurnState_Turns_EmptyVirtual(t *testing.T) {
	base := &Buffer{}
	base.Append(RoleUser, artifact.Text{Content: "user"})

	vts := NewVirtualTurnState(base, []Turn{})

	// Identity optimization: should return the base state directly
	assert.Equal(t, base, vts)

	turns := vts.Turns()
	assert.Len(t, turns, 1)
	assert.Equal(t, RoleUser, turns[0].Role)
}

func TestVirtualTurnState_Turns_DefensiveCopy(t *testing.T) {
	base := &Buffer{}
	base.Append(RoleUser, artifact.Text{Content: "user"})

	virtual := []Turn{
		{Role: RoleSystem, Artifacts: []artifact.Artifact{artifact.Text{Content: "system"}}},
	}
	vts := NewVirtualTurnState(base, virtual).(*VirtualTurnState)

	turns1 := vts.Turns()
	turns2 := vts.Turns()

	// Modifying one should not affect the other
	turns1[0].Role = RoleUser
	assert.Equal(t, RoleSystem, turns2[0].Role)
}

func TestVirtualTurnState_Append_DelegatesToBase(t *testing.T) {
	base := &Buffer{}
	base.Append(RoleUser, artifact.Text{Content: "user"})

	vts := NewVirtualTurnState(base, []Turn{
		{Role: RoleSystem, Artifacts: []artifact.Artifact{artifact.Text{Content: "system"}}},
	})

	vts.Append(RoleAssistant, artifact.Text{Content: "assistant"})

	// Base state should have the appended turn
	baseTurns := base.Turns()
	assert.Len(t, baseTurns, 2)
	assert.Equal(t, RoleUser, baseTurns[0].Role)
	assert.Equal(t, RoleAssistant, baseTurns[1].Role)

	// Wrapped view should include virtual + base + appended
	turns := vts.Turns()
	assert.Len(t, turns, 3)
	assert.Equal(t, RoleSystem, turns[0].Role)
	assert.Equal(t, RoleUser, turns[1].Role)
	assert.Equal(t, RoleAssistant, turns[2].Role)
}

func TestVirtualTurnState_Turns_EmptyBase(t *testing.T) {
	base := &Buffer{}

	vts := NewVirtualTurnState(base, []Turn{
		{Role: RoleSystem, Artifacts: []artifact.Artifact{artifact.Text{Content: "system"}}},
	})

	turns := vts.Turns()
	assert.Len(t, turns, 1)
	assert.Equal(t, RoleSystem, turns[0].Role)
}
