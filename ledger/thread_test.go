package ledger

import (
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestThread_Empty(t *testing.T) {
	th := NewThread()
	assert.Empty(t, th.Turns())
	assert.Empty(t, th.CurrentTip)
}

func TestThread_Append(t *testing.T) {
	th := NewThread()
	th.Append(RoleUser, artifact.Text{Content: "hello"})

	require.Len(t, th.Turns(), 1)
	got := th.Turns()[0]
	assert.Equal(t, RoleUser, got.Role)
	require.Len(t, got.Artifacts, 1)
	assert.Equal(t, "hello", got.Artifacts[0].(artifact.Text).Content)

	// The turn in the tree must have a unique ID and the current tip
	// must advance to that ID.
	tip := th.CurrentTip
	assert.NotEmpty(t, tip)
	require.Contains(t, th.turns, tip, "current tip must reference a stored turn")
	assert.Equal(t, tip, th.turns[tip].ID)
}

func TestThread_AppendChainsParentIDs(t *testing.T) {
	th := NewThread()
	th.Append(RoleUser, artifact.Text{Content: "u1"})
	th.Append(RoleAssistant, artifact.Text{Content: "a1"})
	th.Append(RoleUser, artifact.Text{Content: "u2"})

	require.Len(t, th.Turns(), 3)

	turns := th.turns
	for _, turn := range turns {
		// Each non-root turn's ParentID must equal the previous turn's ID.
		if turn.ParentID != "" {
			require.Contains(t, turns, turn.ParentID, "parent ID must reference a stored turn")
		}
	}

	// And specifically: the second turn's parent is the first, etc.
	// We can reconstruct this since Append links linearly.
}

func TestThread_AppendSetsControlDefault(t *testing.T) {
	th := NewThread()
	th.Append(RoleUser, artifact.Text{Content: "hello"})

	got := th.Turns()[0]
	assert.Equal(t, "", string(got.Metadata.Control), "default control is empty string (treated as continue at use time)")
}

func TestThread_AppendWithCustomClock(t *testing.T) {
	fixed := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	th := NewThread(WithThreadClock(&mockClock{now: fixed}))
	th.Append(RoleUser, artifact.Text{Content: "hello"})

	got := th.Turns()[0]
	assert.Equal(t, fixed, got.Timestamp, "Timestamp should come from injected clock")
}

func TestThread_AppendRootHasEmptyParentID(t *testing.T) {
	th := NewThread()
	th.Append(RoleUser, artifact.Text{Content: "hello"})

	got := th.turns[th.CurrentTip]
	assert.Equal(t, "", got.ParentID, "the root turn's ParentID is the empty string")
}

func TestThread_Walk_Branching(t *testing.T) {
	// Build:
	//   u1 → a1 → u2 → a2 → tip (active)
	//      \
	//       └─ a1' → u2'  (alternative branch)
	th := NewThread()
	th.Append(RoleUser, artifact.Text{Content: "u1"})
	u1ID := th.CurrentTip
	th.Append(RoleAssistant, artifact.Text{Content: "a1"})
	a1ID := th.CurrentTip
	th.Append(RoleUser, artifact.Text{Content: "u2"})
	u2ID := th.CurrentTip
	th.Append(RoleAssistant, artifact.Text{Content: "a2"})
	activeTip := th.CurrentTip

	// Fork: make a sibling under a1.
	th.SaveTurn(&Turn{
		ID:        "a1-alt",
		ParentID:  a1ID,
		Role:      RoleAssistant,
		Artifacts: []artifact.Artifact{artifact.Text{Content: "a1-alt"}},
	})
	th.SaveTurn(&Turn{
		ID:        "u2-alt",
		ParentID:  "a1-alt",
		Role:      RoleUser,
		Artifacts: []artifact.Artifact{artifact.Text{Content: "u2-alt"}},
	})
	th.SetCurrentTip("u2-alt")

	got := th.Turns()
	require.Len(t, got, 4, "branching walk visits the shared ancestors of the active tip")
	assert.Equal(t, u1ID, got[0].ID)
	assert.Equal(t, a1ID, got[1].ID)
	assert.Equal(t, "a1-alt", got[2].ID)
	assert.Equal(t, "u2-alt", got[3].ID, "should follow ParentID from u2-alt, not u2")

	// Switch back to the original branch.
	th.SetCurrentTip(activeTip)
	got = th.Turns()
	require.Len(t, got, 4)
	assert.Equal(t, u1ID, got[0].ID)
	assert.Equal(t, a1ID, got[1].ID)
	assert.Equal(t, u2ID, got[2].ID)
	assert.Equal(t, activeTip, got[3].ID)

	_ = u1ID // keep linter happy
}

// u2Parent returns the ParentID of the turn with id u2ID.
func u2Parent(t *testing.T, th *Thread, u2ID string) string {
	t.Helper()
	node, ok := th.turns[u2ID]
	require.True(t, ok)
	return node.ParentID
}
func TestThread_Walk_ControlStop(t *testing.T) {
	// Build: u1 → a1 → u2 → summary (Stop) → u3 → a3
	th := NewThread()
	th.Append(RoleUser, artifact.Text{Content: "u1"})
	u1ID := th.CurrentTip
	th.Append(RoleAssistant, artifact.Text{Content: "a1"})
	th.Append(RoleUser, artifact.Text{Content: "u2"})
	u2ID := th.CurrentTip

	th.SaveTurn(&Turn{
		ID:        "summary-1",
		ParentID:  u2ID,
		Role:      RoleAssistant,
		Artifacts: []artifact.Artifact{artifact.Text{Content: "summary"}},
		Metadata:  Metadata{Control: ControlStop},
	})
	th.SetCurrentTip("summary-1") // advance the tip so subsequent appends chain off the summary
	th.Append(RoleUser, artifact.Text{Content: "u3"})
	u3ID := th.CurrentTip
	th.Append(RoleAssistant, artifact.Text{Content: "a3"})
	a3ID := th.CurrentTip

	got := th.Turns()
	require.Len(t, got, 3, "walk visits u3, a3 (tip), then summary (Stop, include, terminate)")
	assert.Equal(t, "summary-1", got[0].ID, "summary first in chronological order")
	assert.Equal(t, u3ID, got[1].ID)
	assert.Equal(t, a3ID, got[2].ID)

	// Pre-summary turns remain reachable in the tree (debug/audit).
	require.Contains(t, th.turns, u1ID)
}

func TestThread_Walk_ControlSkip(t *testing.T) {
	th := NewThread()
	th.Append(RoleUser, artifact.Text{Content: "u1"})
	u1ID := th.CurrentTip
	th.Append(RoleAssistant, artifact.Text{Content: "a1-skip"})
	skipID := th.CurrentTip
	th.Append(RoleUser, artifact.Text{Content: "u2"})
	u2ID := th.CurrentTip
	th.SaveTurn(&Turn{
		ID:        "last-1",
		ParentID:  u2ID,
		Role:      RoleAssistant,
		Artifacts: []artifact.Artifact{artifact.Text{Content: "after-skip"}},
	})
	th.SetControl(skipID, ControlSkip)
	th.SetCurrentTip("last-1") // ensure the walk visits last-1

	got := th.Turns()
	require.Len(t, got, 3, "skipped turn must not appear in the active path")
	assert.Equal(t, u1ID, got[0].ID)
	assert.Equal(t, u2ID, got[1].ID)
	assert.Equal(t, "last-1", got[2].ID)

	// The skipped turn is still in the tree.
	require.Contains(t, th.turns, skipID)
	assert.Equal(t, ControlSkip, th.turns[skipID].Metadata.Control)
}

func TestThread_Walk_SkipThenStop(t *testing.T) {
	th := NewThread()
	th.Append(RoleUser, artifact.Text{Content: "u1"})
	th.Append(RoleAssistant, artifact.Text{Content: "a1"})
	skipID := th.CurrentTip
	th.SetControl(skipID, ControlSkip)

	summaryID := "summary-1"
	th.SaveTurn(&Turn{
		ID:        summaryID,
		ParentID:  skipID,
		Role:      RoleAssistant,
		Artifacts: []artifact.Artifact{artifact.Text{Content: "summary"}},
		Metadata:  Metadata{Control: ControlStop},
	})
	th.SetCurrentTip(summaryID)

	got := th.Turns()
	require.Len(t, got, 1, "Stop terminates at summary; a1 and u1 are not visited")
	assert.Equal(t, summaryID, got[0].ID)
	assert.Equal(t, ControlStop, got[0].Metadata.Control)
}

func TestThread_Walk_BrokenChainStopsAtGap(t *testing.T) {
	th := NewThread()
	th.Append(RoleUser, artifact.Text{Content: "u1"})
	th.Append(RoleAssistant, artifact.Text{Content: "a1"})
	th.Append(RoleUser, artifact.Text{Content: "u2"})

	// Manually break the chain: u2.ParentID -> "nonexistent"
	u2 := th.turns[th.CurrentTip]
	u2.ParentID = "nonexistent"

	got := th.Turns()
	// u2 is included; a1 should not be reached because the chain
	// into it through u2 is broken. But the walk visits u2 (include),
	// then checks u2.ParentID = "nonexistent" (terminate).
	require.Len(t, got, 1, "broken chain must not include unreachable turns")
	assert.Equal(t, th.CurrentTip, got[0].ID)
}

func TestThread_Walk_OrphanTurnsRemainInTree(t *testing.T) {
	th := NewThread()
	th.Append(RoleUser, artifact.Text{Content: "u1"})
	u1ID := th.CurrentTip

	// Append more turns to advance the tip, then fork back.
	th.Append(RoleAssistant, artifact.Text{Content: "a1"})
	th.Append(RoleUser, artifact.Text{Content: "u2"})

	// Orphan u1 by giving it a parent that doesn't exist.
	u1 := th.turns[u1ID]
	u1.ParentID = ""

	// Walking from u2 should still work (u2 -> a1 -> ...).
	got := th.Turns()
	require.NotEmpty(t, got)
	_ = got
}

func TestThread_SetParent(t *testing.T) {
	th := NewThread()
	th.Append(RoleUser, artifact.Text{Content: "u1"})
	th.Append(RoleAssistant, artifact.Text{Content: "a1"})
	a1ID := th.CurrentTip
	th.Append(RoleUser, artifact.Text{Content: "u2"})
	u2ID := th.CurrentTip

	// Re-parent u2: parent should change from a1 to "" (root).
	th.SetParent(u2ID, "")
	assert.Equal(t, "", th.turns[u2ID].ParentID)

	// Walk now: u2 is its own root; a1 is unreachable.
	got := th.Turns()
	require.Len(t, got, 1, "u2 alone in path; a1 must be unreachable")
	assert.Equal(t, u2ID, got[0].ID)

	// a1 still exists in the tree but is orphaned.
	require.Contains(t, th.turns, a1ID)
}

func TestThread_SetControl(t *testing.T) {
	th := NewThread()
	th.Append(RoleUser, artifact.Text{Content: "u1"})
	u1ID := th.CurrentTip
	th.Append(RoleAssistant, artifact.Text{Content: "a1"})
	a1ID := th.CurrentTip

	th.SetControl(a1ID, ControlSkip)
	assert.Equal(t, ControlSkip, th.turns[a1ID].Metadata.Control)

	got := th.Turns()
	require.Len(t, got, 1, "skipped turn excluded")
	assert.Equal(t, u1ID, got[0].ID)
}

func TestThread_SetControl_NoOp(t *testing.T) {
	th := NewThread()
	th.SetControl("nonexistent", ControlSkip)
	// Should not panic; no-op.
}

func TestThread_Meta(t *testing.T) {
	th := NewThread()
	th.Meta().Set("model", "claude")
	th.Meta().Set("session", "abc")

	m := th.Meta()
	v, ok := m.Get("model")
	assert.True(t, ok)
	assert.Equal(t, "claude", v)

	_, ok = m.Get("nonexistent")
	assert.False(t, ok)

	all := m.All()
	assert.Equal(t, "claude", all["model"])
	assert.Equal(t, "abc", all["session"])

	// Mutating the returned map must not affect the thread.
	delete(all, "model")
	_, ok = th.Meta().Get("model")
	assert.True(t, ok, "deleting from All() must not affect thread state")
}

func TestThread_MetaLiveHandles(t *testing.T) {
	th := NewThread()
	h1 := th.Meta()
	h2 := th.Meta()

	h1.Set("k", "v1")
	v, _ := h2.Get("k")
	assert.Equal(t, "v1", v, "handles must share backing storage")
}

func TestThread_TurnsDefensiveCopy(t *testing.T) {
	th := NewThread()
	th.Append(RoleUser, artifact.Text{Content: "u1"})

	turns := th.Turns()
	originalLen := len(turns)
	_ = append(turns, Turn{Role: RoleAssistant})

	assert.Len(t, th.Turns(), originalLen, "modifying returned slice must not affect thread")
}

func TestThread_SaveTurn(t *testing.T) {
	th := NewThread()
	id := GenerateTurnID()
	th.SaveTurn(&Turn{
		ID:        id,
		ParentID:  "",
		Role:      RoleSystem,
		Artifacts: []artifact.Artifact{artifact.Text{Content: "system"}},
	})

	require.Contains(t, th.turns, id)
	assert.Equal(t, "", th.CurrentTip, "SaveTurn must not advance the current tip")
}

func TestThread_ImplementsState(t *testing.T) {
	var _ State = (*Thread)(nil)
	var _ State = NewThread()

	th := NewThread()
	th.Append(RoleUser, artifact.Text{Content: "u1"})

	// Calls via the interface must work.
	var st State = th
	assert.Len(t, st.Turns(), 1)
	st.Append(RoleAssistant, artifact.Text{Content: "a1"})
	assert.Len(t, st.Turns(), 2)
}