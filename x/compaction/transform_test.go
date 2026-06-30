package compaction

import (
	"context"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTransform_NoBoundary_Identity verifies that without any
// ControlStop, the transform is the identity and the walk returns
// the full active path.
func TestTransform_NoBoundary_Identity(t *testing.T) {
	th := ledger.NewThread()
	th.Append(ledger.RoleUser, artifact.Text{Content: "u1"})
	th.Append(ledger.RoleAssistant, artifact.Text{Content: "a1"})

	tr := NewTransform()
	out, err := tr.Transform(context.Background(), th)
	require.NoError(t, err)

	got := out.Turns()
	require.Len(t, got, 2)
	assert.Equal(t, "u1", got[0].Artifacts[0].(artifact.Text).Content)
	assert.Equal(t, "a1", got[1].Artifacts[0].(artifact.Text).Content)
}

// TestTransform_EmptyBuffer_Identity verifies the empty case.
func TestTransform_EmptyBuffer_Identity(t *testing.T) {
	th := ledger.NewThread()
	tr := NewTransform()
	out, err := tr.Transform(context.Background(), th)
	require.NoError(t, err)
	assert.Empty(t, out.Turns())
}

// TestTransform_NilState_NilReturned verifies nil input.
func TestTransform_NilState_NilReturned(t *testing.T) {
	tr := NewTransform()
	out, err := tr.Transform(context.Background(), nil)
	require.NoError(t, err)
	assert.Nil(t, out)
}

// TestTransform_ControlStopAtEnd_ReturnsSummaryPlusAfter confirms
// that a ControlStop on the summary turn causes the walk to stop
// there; subsequent turns are still included.
func TestTransform_ControlStopAtEnd_ReturnsSummaryPlusAfter(t *testing.T) {
	// Build: u1 → a1 → u2 → summary (Stop) → u3 → a3
	th := ledger.NewThread()
	th.Append(ledger.RoleUser, artifact.Text{Content: "u1"})
	u1ID := th.CurrentTip
	th.Append(ledger.RoleAssistant, artifact.Text{Content: "a1"})
	th.Append(ledger.RoleUser, artifact.Text{Content: "u2"})
	u2ID := th.CurrentTip

	summary := &ledger.Turn{
		ID:        "summary-1",
		ParentID:  u2ID,
		Role:      ledger.RoleAssistant,
		Artifacts: []artifact.Artifact{artifact.Text{Content: "summary"}},
		Metadata:  ledger.Metadata{Control: ledger.ControlStop},
	}
	th.SaveTurn(summary)
	th.SetCurrentTip(summary.ID)
	th.Append(ledger.RoleUser, artifact.Text{Content: "u3"})
	u3ID := th.CurrentTip
	th.Append(ledger.RoleAssistant, artifact.Text{Content: "a3"})
	a3ID := th.CurrentTip
	// CurrentTip is now a3; the walk goes a3 → u3 → summary (Stop, terminate).

	tr := NewTransform()
	out, err := tr.Transform(context.Background(), th)
	require.NoError(t, err)

	got := out.Turns()
	require.Len(t, got, 3, "walk stops at summary; u3 and a3 are still in path")
	assert.Equal(t, "summary-1", got[0].ID)
	assert.Equal(t, u3ID, got[1].ID)
	assert.Equal(t, a3ID, got[2].ID)

	// u1, a1, u2 are still in the tree but invisible to the LLM.
	allTurns := th.AllTurns()
	found := false
	for _, t := range allTurns {
		if t.ID == u1ID {
			found = true
			break
		}
	}
	require.True(t, found, "u1 must still be in the underlying tree")
}

// TestTransform_ControlStopInMiddle_HidesEverythingBefore verifies
// the compaction semantic: a ControlStop on a summary turn hides
// everything before it from the LLM-facing view.
func TestTransform_ControlStopInMiddle_HidesEverythingBefore(t *testing.T) {
	th := ledger.NewThread()
	th.Append(ledger.RoleUser, artifact.Text{Content: "u1"})
	th.Append(ledger.RoleAssistant, artifact.Text{Content: "a1"})
	th.Append(ledger.RoleUser, artifact.Text{Content: "u2"})
	u2ID := th.CurrentTip

	summary := &ledger.Turn{
		ID:        "summary-1",
		ParentID:  u2ID,
		Role:      ledger.RoleAssistant,
		Artifacts: []artifact.Artifact{artifact.Text{Content: "summary"}},
		Metadata:  ledger.Metadata{Control: ledger.ControlStop},
	}
	th.SaveTurn(summary)
	th.SetCurrentTip(summary.ID)

	tr := NewTransform()
	out, err := tr.Transform(context.Background(), th)
	require.NoError(t, err)

	got := out.Turns()
	require.Len(t, got, 1)
	assert.Equal(t, "summary-1", got[0].ID)
}

// TestTransform_NoControlStop_PassesThroughEntireChain confirms that
// without ControlStop, the walk traverses the whole chain.
func TestTransform_NoControlStop_PassesThroughEntireChain(t *testing.T) {
	th := ledger.NewThread()
	for i := 0; i < 5; i++ {
		th.Append(ledger.RoleUser, artifact.Text{Content: "msg"})
	}

	tr := NewTransform()
	out, err := tr.Transform(context.Background(), th)
	require.NoError(t, err)
	assert.Len(t, out.Turns(), 5)
}

// TestTransform_DefensiveCopy verifies the returned turns are a
// defensive copy; mutating them doesn't affect the underlying
// thread.
func TestTransform_DefensiveCopy(t *testing.T) {
	th := ledger.NewThread()
	th.Append(ledger.RoleUser, artifact.Text{Content: "hello"})

	tr := NewTransform()
	out, err := tr.Transform(context.Background(), th)
	require.NoError(t, err)

	got := out.Turns()
	require.Len(t, got, 1)
	_ = append(got, ledger.Turn{Role: ledger.RoleAssistant})
	assert.Len(t, th.Turns(), 1, "mutating returned slice must not affect thread")
}

// TestTransform_NonStopTurn_Continues confirms that a turn without
// ControlStop (e.g., a normal system prompt at the chain start)
// doesn't terminate the walk.
func TestTransform_NonStopTurn_Continues(t *testing.T) {
	th := ledger.NewThread()
	th.Append(ledger.RoleSystem, artifact.Text{Content: "system prompt"})
	th.Append(ledger.RoleUser, artifact.Text{Content: "u1"})

	tr := NewTransform()
	out, err := tr.Transform(context.Background(), th)
	require.NoError(t, err)

	got := out.Turns()
	require.Len(t, got, 2)
	assert.Equal(t, ledger.RoleSystem, got[0].Role)
	assert.Equal(t, ledger.RoleUser, got[1].Role)
}

// TestTransform_ControlSkip_ExcludesTurn verifies that ControlSkip
// hides a turn while continuing the walk.
func TestTransform_ControlSkip_ExcludesTurn(t *testing.T) {
	th := ledger.NewThread()
	th.Append(ledger.RoleUser, artifact.Text{Content: "u1"})
	u1ID := th.CurrentTip
	th.Append(ledger.RoleAssistant, artifact.Text{Content: "a1-skip"})
	skipID := th.CurrentTip
	th.SetControl(skipID, ledger.ControlSkip)
	th.Append(ledger.RoleUser, artifact.Text{Content: "u2"})
	u2ID := th.CurrentTip
	th.SetCurrentTip(u2ID)

	tr := NewTransform()
	out, err := tr.Transform(context.Background(), th)
	require.NoError(t, err)

	got := out.Turns()
	require.Len(t, got, 2, "skip turn must not appear in path")
	assert.Equal(t, u1ID, got[0].ID)
	assert.Equal(t, u2ID, got[1].ID)
}

// TestTransform_StopThenSwitchTip confirms that switching CurrentTip
// after a ControlStop is recorded re-evaluates the walk from the
// new tip; if the new tip is on a different branch (no Stop), the
// full chain of that branch is visible.
func TestTransform_StopThenSwitchTip(t *testing.T) {
	th := ledger.NewThread()
	th.Append(ledger.RoleUser, artifact.Text{Content: "u1"})
	u1ID := th.CurrentTip
	th.Append(ledger.RoleAssistant, artifact.Text{Content: "a1"})
	a1ID := th.CurrentTip
	th.Append(ledger.RoleUser, artifact.Text{Content: "u2"})
	u2ID := th.CurrentTip

	// Create a summary on a parallel branch (sibling under a1).
	summary := &ledger.Turn{
		ID:        "summary",
		ParentID:  a1ID,
		Role:      ledger.RoleAssistant,
		Artifacts: []artifact.Artifact{artifact.Text{Content: "summary"}},
		Metadata:  ledger.Metadata{Control: ledger.ControlStop},
	}
	th.SaveTurn(summary)

	// Start on the u2 branch (no Stop).
	th.SetCurrentTip(u2ID)
	tr := NewTransform()
	out, err := tr.Transform(context.Background(), th)
	require.NoError(t, err)
	got := out.Turns()
	require.Len(t, got, 3, "u1, a1, u2 all visible when no Stop is reachable")
	assert.Equal(t, u1ID, got[0].ID)
	assert.Equal(t, a1ID, got[1].ID)
	assert.Equal(t, u2ID, got[2].ID)

	// Switch to summary; the walk stops there.
	th.SetCurrentTip("summary")
	out, err = tr.Transform(context.Background(), th)
	require.NoError(t, err)
	got = out.Turns()
	require.Len(t, got, 1)
	assert.Equal(t, "summary", got[0].ID)
}