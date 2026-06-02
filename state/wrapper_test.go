package state

import (
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/stretchr/testify/assert"
)

func TestPrepend_Turns_PrependsVirtual(t *testing.T) {
	base := &Buffer{}
	base.Append(RoleUser, artifact.Text{Content: "user"})

	v := Prepend(base, []Turn{
		{Role: RoleSystem, Artifacts: []artifact.Artifact{artifact.Text{Content: "system"}}},
	})

	turns := v.Turns()
	assert.Len(t, turns, 2)
	assert.Equal(t, RoleSystem, turns[0].Role)
	assert.Equal(t, RoleUser, turns[1].Role)

	text, ok := turns[0].Artifacts[0].(artifact.Text)
	assert.True(t, ok)
	assert.Equal(t, "system", text.Content)
}

func TestPrepend_Turns_MultipleVirtual(t *testing.T) {
	base := &Buffer{}
	base.Append(RoleUser, artifact.Text{Content: "user"})

	v := Prepend(base, []Turn{
		{Role: RoleSystem, Artifacts: []artifact.Artifact{artifact.Text{Content: "system1"}}},
		{Role: RoleSystem, Artifacts: []artifact.Artifact{artifact.Text{Content: "system2"}}},
	})

	turns := v.Turns()
	assert.Len(t, turns, 3)
	assert.Equal(t, RoleSystem, turns[0].Role)
	assert.Equal(t, RoleSystem, turns[1].Role)
	assert.Equal(t, RoleUser, turns[2].Role)
}

func TestPrepend_Turns_EmptyVirtual(t *testing.T) {
	base := &Buffer{}
	base.Append(RoleUser, artifact.Text{Content: "user"})

	v := Prepend(base, []Turn{})

	// Identity optimization: should return the base state directly
	assert.Equal(t, base, v)

	turns := v.Turns()
	assert.Len(t, turns, 1)
	assert.Equal(t, RoleUser, turns[0].Role)
}

func TestPrepend_Turns_DefensiveCopy(t *testing.T) {
	base := &Buffer{}
	base.Append(RoleUser, artifact.Text{Content: "user"})

	virtual := []Turn{
		{Role: RoleSystem, Artifacts: []artifact.Artifact{artifact.Text{Content: "system"}}},
	}
	v := Prepend(base, virtual).(*prependView)

	turns1 := v.Turns()
	turns2 := v.Turns()

	// Modifying one should not affect the other
	turns1[0].Role = RoleUser
	assert.Equal(t, RoleSystem, turns2[0].Role)
}

func TestPrepend_Append_DelegatesToBase(t *testing.T) {
	base := &Buffer{}
	base.Append(RoleUser, artifact.Text{Content: "user"})

	v := Prepend(base, []Turn{
		{Role: RoleSystem, Artifacts: []artifact.Artifact{artifact.Text{Content: "system"}}},
	})

	v.Append(RoleAssistant, artifact.Text{Content: "assistant"})

	// Base state should have the appended turn
	baseTurns := base.Turns()
	assert.Len(t, baseTurns, 2)
	assert.Equal(t, RoleUser, baseTurns[0].Role)
	assert.Equal(t, RoleAssistant, baseTurns[1].Role)

	// Wrapped view should include virtual + base + appended
	turns := v.Turns()
	assert.Len(t, turns, 3)
	assert.Equal(t, RoleSystem, turns[0].Role)
	assert.Equal(t, RoleUser, turns[1].Role)
	assert.Equal(t, RoleAssistant, turns[2].Role)
}

func TestPrepend_Turns_EmptyBase(t *testing.T) {
	base := &Buffer{}

	v := Prepend(base, []Turn{
		{Role: RoleSystem, Artifacts: []artifact.Artifact{artifact.Text{Content: "system"}}},
	})

	turns := v.Turns()
	assert.Len(t, turns, 1)
	assert.Equal(t, RoleSystem, turns[0].Role)
}

func TestPrepend_Turns_DynamicBase(t *testing.T) {
	base := &Buffer{}
	base.Append(RoleUser, artifact.Text{Content: "user"})

	v := Prepend(base, []Turn{
		{Role: RoleSystem, Artifacts: []artifact.Artifact{artifact.Text{Content: "system"}}},
	})

	// Mutate base after view creation
	base.Append(RoleAssistant, artifact.Text{Content: "assistant"})

	// Prepend view should reflect the mutation (dynamic)
	turns := v.Turns()
	assert.Len(t, turns, 3)
	assert.Equal(t, RoleSystem, turns[0].Role)
	assert.Equal(t, RoleUser, turns[1].Role)
	assert.Equal(t, RoleAssistant, turns[2].Role)
}

func TestNewView_Turns_ReturnsProjected(t *testing.T) {
	base := &Buffer{}
	base.Append(RoleUser, artifact.Text{Content: "user"})

	projected := []Turn{
		{Role: RoleAssistant, Artifacts: []artifact.Artifact{artifact.Text{Content: "assistant"}}},
	}
	v := NewView(base, projected)

	turns := v.Turns()
	assert.Len(t, turns, 1)
	assert.Equal(t, RoleAssistant, turns[0].Role)
}

func TestNewView_Turns_DefensiveCopy(t *testing.T) {
	base := &Buffer{}
	base.Append(RoleUser, artifact.Text{Content: "user"})

	projected := []Turn{
		{Role: RoleSystem, Artifacts: []artifact.Artifact{artifact.Text{Content: "system"}}},
	}
	v := NewView(base, projected).(*View)

	turns1 := v.Turns()
	turns2 := v.Turns()

	turns1[0].Role = RoleUser
	assert.Equal(t, RoleSystem, turns2[0].Role)
}

func TestNewView_Append_DelegatesToBase(t *testing.T) {
	base := &Buffer{}
	base.Append(RoleUser, artifact.Text{Content: "user"})

	projected := []Turn{
		{Role: RoleSystem, Artifacts: []artifact.Artifact{artifact.Text{Content: "system"}}},
	}
	v := NewView(base, projected)

	v.Append(RoleAssistant, artifact.Text{Content: "assistant"})

	// Base state should have the appended turn
	baseTurns := base.Turns()
	assert.Len(t, baseTurns, 2)
	assert.Equal(t, RoleUser, baseTurns[0].Role)
	assert.Equal(t, RoleAssistant, baseTurns[1].Role)

	// View projection unchanged (static snapshot)
	turns := v.Turns()
	assert.Len(t, turns, 1)
	assert.Equal(t, RoleSystem, turns[0].Role)
}

func TestNewView_Turns_EmptyTurns(t *testing.T) {
	base := &Buffer{}
	base.Append(RoleUser, artifact.Text{Content: "user"})

	v := NewView(base, []Turn{})

	// Identity optimization: should return the base state directly
	assert.Equal(t, base, v)

	turns := v.Turns()
	assert.Len(t, turns, 1)
	assert.Equal(t, RoleUser, turns[0].Role)
}

func TestNewView_Turns_Identity_NilTurns(t *testing.T) {
	base := &Buffer{}
	base.Append(RoleUser, artifact.Text{Content: "user"})

	v := NewView(base, nil)

	// Identity optimization: should return the base state directly
	assert.Equal(t, base, v)
}

func TestNewView_Turns_Filtering(t *testing.T) {
	base := &Buffer{}
	base.Append(RoleSystem, artifact.Text{Content: "system"})
	base.Append(RoleUser, artifact.Text{Content: "user"})
	base.Append(RoleAssistant, artifact.Text{Content: "assistant"})

	// Projection: filter out system turns (compaction use case)
	all := base.Turns()
	var projected []Turn
	for _, turn := range all {
		if turn.Role != RoleSystem {
			projected = append(projected, turn)
		}
	}
	v := NewView(base, projected)

	turns := v.Turns()
	assert.Len(t, turns, 2)
	assert.Equal(t, RoleUser, turns[0].Role)
	assert.Equal(t, RoleAssistant, turns[1].Role)
}
