package ledger

import (
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPrepend_Turns_PrependsVirtual(t *testing.T) {
	base := NewThread()
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
	base := NewThread()
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
	base := NewThread()
	base.Append(RoleUser, artifact.Text{Content: "user"})

	v := Prepend(base, []Turn{})

	// Identity optimization: should return the base state directly
	assert.Equal(t, base, v)

	turns := v.Turns()
	assert.Len(t, turns, 1)
	assert.Equal(t, RoleUser, turns[0].Role)
}

func TestPrepend_Turns_DefensiveCopy(t *testing.T) {
	base := NewThread()
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
	base := NewThread()
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
	base := NewThread()

	v := Prepend(base, []Turn{
		{Role: RoleSystem, Artifacts: []artifact.Artifact{artifact.Text{Content: "system"}}},
	})

	turns := v.Turns()
	assert.Len(t, turns, 1)
	assert.Equal(t, RoleSystem, turns[0].Role)
}

func TestPrepend_Turns_DynamicBase(t *testing.T) {
	base := NewThread()
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

// TestPrependView_BaseState_ReturnsBase verifies that the prependView
// accessor exposed for cross-package prepend-aware composition (see
// compaction.Transform) returns the original base state unchanged.
func TestPrependView_BaseState_ReturnsBase(t *testing.T) {
	base := NewThread()
	base.Append(RoleUser, artifact.Text{Content: "user"})

	pv := Prepend(base, []Turn{
		{Role: RoleSystem, Artifacts: []artifact.Artifact{artifact.Text{Content: "system"}}},
	})

	// Round-trip the accessor to reach the underlying *prependView.
	pc, ok := pv.(interface{ BaseState() State })
	require.True(t, ok, "prependView must expose BaseState for cross-package composition")

	got := pc.BaseState()
	assert.Same(t, State(base), got, "BaseState must return the original base, not a copy")
}

// TestPrependView_VirtualTurns_ReturnsVirtual verifies that the
// prependView accessor exposed for cross-package prepend-aware
// composition returns the original virtual slice.
func TestPrependView_VirtualTurns_ReturnsVirtual(t *testing.T) {
	virtual := []Turn{
		{Role: RoleSystem, Artifacts: []artifact.Artifact{artifact.Text{Content: "system"}}},
	}
	base := NewThread()

	pv := Prepend(base, virtual)

	pc, ok := pv.(interface{ VirtualTurns() []Turn })
	require.True(t, ok, "prependView must expose VirtualTurns for cross-package composition")

	got := pc.VirtualTurns()
	assert.Len(t, got, 1)
	assert.Equal(t, "system", got[0].Artifacts[0].(artifact.Text).Content)
}

func TestNewView_Turns_ReturnsProjected(t *testing.T) {
	base := NewThread()
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
	base := NewThread()
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
	base := NewThread()
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
	base := NewThread()
	base.Append(RoleUser, artifact.Text{Content: "user"})

	v := NewView(base, []Turn{})

	// Identity optimization: should return the base state directly
	assert.Equal(t, base, v)

	turns := v.Turns()
	assert.Len(t, turns, 1)
	assert.Equal(t, RoleUser, turns[0].Role)
}

func TestNewView_Turns_Identity_NilTurns(t *testing.T) {
	base := NewThread()
	base.Append(RoleUser, artifact.Text{Content: "user"})

	v := NewView(base, nil)

	// Identity optimization: should return the base state directly
	assert.Equal(t, base, v)
}

func TestNewView_Turns_Filtering(t *testing.T) {
	base := NewThread()
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

// TestNewView_Meta_DelegatesToBase verifies that Meta() on a View returns
// the base's Meta, so writes through a view propagate to the underlying
// buffer.
func TestNewView_Meta_DelegatesToBase(t *testing.T) {
	base := NewThread()
	v := NewView(base, nil)

	v.Meta().Set("alpha", "1")

	got, ok := base.Meta().Get("alpha")
	assert.True(t, ok, "write through View's Meta must be visible on the base")
	assert.Equal(t, "1", got)
}

// TestPrepend_Meta_DelegatesToBase verifies the same delegation through a
// prependView wrapper.
func TestPrepend_Meta_DelegatesToBase(t *testing.T) {
	base := NewThread()
	pv := Prepend(base, nil)

	pv.Meta().Set("beta", "2")

	got, ok := base.Meta().Get("beta")
	assert.True(t, ok)
	assert.Equal(t, "2", got)
}

// TestThread_Meta_LazilyInitialized verifies that calling Meta() on a
// Thread with no prior writes does not allocate the underlying map
// until a Set happens. Reads against an unset map return ok=false
// cleanly.
func TestThread_Meta_LazilyInitialized(t *testing.T) {
	b := NewThread()

	v, ok := b.Meta().Get("never-set")
	assert.False(t, ok, "unset key must return ok=false")
	assert.Equal(t, "", v)

	b.Meta().Set("now-set", "value")
	v, ok = b.Meta().Get("now-set")
	assert.True(t, ok)
	assert.Equal(t, "value", v)
}

// TestThread_Meta_All_DefensiveCopy verifies that mutations to the
// map returned by All() do not affect the thread's metadata.
func TestThread_Meta_All_DefensiveCopy(t *testing.T) {
	b := NewThread()
	b.Meta().Set("k", "v")

	all := b.Meta().All()
	all["k"] = "tampered"
	all["new"] = "leaked"

	got, _ := b.Meta().Get("k")
	assert.Equal(t, "v", got, "tampering All() must not affect the underlying map")
	_, hasNew := b.Meta().Get("new")
	assert.False(t, hasNew, "new keys inserted into All() must not leak back")
}
