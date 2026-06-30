package ledger

import (
	"context"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMemoryRepository_Empty(t *testing.T) {
	r := NewMemoryRepository()
	turns, tip, err := r.HydrateThread(context.Background(), "thread-1")
	require.NoError(t, err)
	assert.Empty(t, turns)
	assert.Empty(t, tip)
}

func TestMemoryRepository_SaveTurnAndHydrate(t *testing.T) {
	r := NewMemoryRepository()
	ctx := context.Background()

	turn := &Turn{
		ID:        "turn-1",
		Role:      RoleUser,
		Artifacts: []artifact.Artifact{artifact.Text{Content: "hello"}},
	}
	require.NoError(t, r.SaveTurn(ctx, "thread-1", turn))

	turns, tip, err := r.HydrateThread(ctx, "thread-1")
	require.NoError(t, err)
	require.Contains(t, turns, "turn-1")
	assert.Equal(t, RoleUser, turns["turn-1"].Role)
	assert.Empty(t, tip, "SaveTurn does not change the current tip")
}

func TestMemoryRepository_UpdateTip(t *testing.T) {
	r := NewMemoryRepository()
	ctx := context.Background()

	require.NoError(t, r.SaveTurn(ctx, "thread-1", &Turn{ID: "turn-1", Role: RoleUser}))
	require.NoError(t, r.UpdateThreadTip(ctx, "thread-1", "turn-1"))

	_, tip, err := r.HydrateThread(ctx, "thread-1")
	require.NoError(t, err)
	assert.Equal(t, "turn-1", tip)
}

func TestMemoryRepository_UpdateControl(t *testing.T) {
	r := NewMemoryRepository()
	ctx := context.Background()

	require.NoError(t, r.SaveTurn(ctx, "thread-1", &Turn{ID: "turn-1", Role: RoleUser}))
	require.NoError(t, r.UpdateTurnControl(ctx, "thread-1", "turn-1", ControlStop))

	turns, _, err := r.HydrateThread(ctx, "thread-1")
	require.NoError(t, err)
	assert.Equal(t, ControlStop, turns["turn-1"].Metadata.Control)
}

func TestMemoryRepository_UpdateParent(t *testing.T) {
	r := NewMemoryRepository()
	ctx := context.Background()

	require.NoError(t, r.SaveTurn(ctx, "thread-1", &Turn{ID: "turn-1", Role: RoleUser}))
	require.NoError(t, r.SaveTurn(ctx, "thread-1", &Turn{ID: "turn-2", Role: RoleAssistant, ParentID: "turn-1"}))
	require.NoError(t, r.UpdateTurnParent(ctx, "thread-1", "turn-2", ""))

	turns, _, err := r.HydrateThread(ctx, "thread-1")
	require.NoError(t, err)
	assert.Equal(t, "", turns["turn-2"].ParentID)
}

func TestMemoryRepository_FullRoundTrip(t *testing.T) {
	r := NewMemoryRepository()
	ctx := context.Background()

	// Build: u1 → a1 → u2 → summary (Stop) → u3 → a3
	require.NoError(t, r.SaveTurn(ctx, "thread-1", &Turn{ID: "u1", Role: RoleUser, Timestamp: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}))
	require.NoError(t, r.SaveTurn(ctx, "thread-1", &Turn{ID: "a1", Role: RoleAssistant, ParentID: "u1", Timestamp: time.Date(2024, 1, 1, 0, 0, 1, 0, time.UTC)}))
	require.NoError(t, r.SaveTurn(ctx, "thread-1", &Turn{ID: "u2", Role: RoleUser, ParentID: "a1", Timestamp: time.Date(2024, 1, 1, 0, 0, 2, 0, time.UTC)}))
	require.NoError(t, r.SaveTurn(ctx, "thread-1", &Turn{ID: "summary", Role: RoleAssistant, ParentID: "u2", Timestamp: time.Date(2024, 1, 1, 0, 0, 3, 0, time.UTC), Metadata: Metadata{Control: ControlStop}}))
	require.NoError(t, r.UpdateThreadTip(ctx, "thread-1", "summary"))
	require.NoError(t, r.SaveTurn(ctx, "thread-1", &Turn{ID: "u3", Role: RoleUser, ParentID: "summary", Timestamp: time.Date(2024, 1, 1, 0, 0, 4, 0, time.UTC)}))
	require.NoError(t, r.SaveTurn(ctx, "thread-1", &Turn{ID: "a3", Role: RoleAssistant, ParentID: "u3", Timestamp: time.Date(2024, 1, 1, 0, 0, 5, 0, time.UTC)}))
	require.NoError(t, r.UpdateThreadTip(ctx, "thread-1", "a3"))

	turns, tip, err := r.HydrateThread(ctx, "thread-1")
	require.NoError(t, err)
	assert.Equal(t, "a3", tip)
	require.Len(t, turns, 6, "u1, a1, u2, summary, u3, a3")
	assert.Equal(t, "u1", turns["u1"].ID)
	assert.Equal(t, ControlStop, turns["summary"].Metadata.Control)

	// Build a Thread from the hydrated state and verify the walk.
	th := NewThread()
	for _, turn := range turns {
		th.SaveTurn(turn)
	}
	th.SetCurrentTip(tip)

	got := th.Turns()
	require.Len(t, got, 3, "walk must stop at summary")
	assert.Equal(t, "summary", got[0].ID)
	assert.Equal(t, "u3", got[1].ID)
	assert.Equal(t, "a3", got[2].ID)
}

func TestMemoryRepository_MalformedEntryStopsReplay(t *testing.T) {
	r := NewMemoryRepository()
	ctx := context.Background()

	// Inject a journal with one good entry and one malformed entry.
	r.journals["t1"] = []JournalEntry{
		{
			TxType:    TxAddTurn,
			Timestamp: time.Now(),
			Payload:   []byte(`{"timestamp":"2024-01-01T00:00:00Z","turn":{"id":"good","role":"user","artifacts":[]}}`),
		},
		{
			TxType:    TxUpdateTip,
			Timestamp: time.Now(),
			Payload:   []byte(`{"this is not valid json`), // truncated/corrupted
		},
	}

	turns, tip, err := r.HydrateThread(ctx, "t1")
	require.NoError(t, err, "replay should tolerate malformed lines by stopping, not erroring")
	require.Contains(t, turns, "good")
	assert.Empty(t, tip, "the bad UpdateTip should never be applied")
}

func TestMemoryRepository_UnknownTxTypeStopsReplay(t *testing.T) {
	r := NewMemoryRepository()
	r.journals["t1"] = []JournalEntry{
		{
			TxType:    TxAddTurn,
			Timestamp: time.Now(),
			Payload:   []byte(`{"timestamp":"2024-01-01T00:00:00Z","turn":{"id":"good","role":"user","artifacts":[]}}`),
		},
		{
			TxType:    "future_tx_type",
			Timestamp: time.Now(),
			Payload:   []byte(`{}`),
		},
	}
	turns, _, err := r.HydrateThread(context.Background(), "t1")
	require.NoError(t, err)
	require.Contains(t, turns, "good")
}

func TestMemoryRepository_JournalReturnsCopy(t *testing.T) {
	r := NewMemoryRepository()
	r.journals["t1"] = []JournalEntry{
		{TxType: TxAddTurn, Payload: []byte(`{}`)},
	}

	journal := r.Journal("t1")
	require.Len(t, journal, 1)
	journal[0].TxType = "mutated"

	// Internal journal is unchanged.
	internal := r.Journal("t1")
	assert.Equal(t, TxAddTurn, internal[0].TxType)
}

func TestMemoryRepository_ArtifactsRoundTrip(t *testing.T) {
	r := NewMemoryRepository()
	ctx := context.Background()

	turn := &Turn{
		ID:        "turn-with-artifacts",
		Role:      RoleAssistant,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: "hello"},
			artifact.ToolCall{ID: "tc-1", Name: "search", Arguments: `{"q":"ore"}`},
		},
	}
	require.NoError(t, r.SaveTurn(ctx, "thread-1", turn))

	turns, _, err := r.HydrateThread(ctx, "thread-1")
	require.NoError(t, err)
	require.Contains(t, turns, "turn-with-artifacts")
	require.Len(t, turns["turn-with-artifacts"].Artifacts, 2)

	text, ok := turns["turn-with-artifacts"].Artifacts[0].(artifact.Text)
	require.True(t, ok, "first artifact must be Text after round-trip")
	assert.Equal(t, "hello", text.Content)

	tc, ok := turns["turn-with-artifacts"].Artifacts[1].(artifact.ToolCall)
	require.True(t, ok, "second artifact must be ToolCall after round-trip")
	assert.Equal(t, "tc-1", tc.ID)
	assert.Equal(t, "search", tc.Name)
}

func TestMemoryRepository_UpdateControlOnUnknownTurnIsNoop(t *testing.T) {
	r := NewMemoryRepository()
	ctx := context.Background()

	// UpdateControl for a turn that doesn't exist should not error.
	require.NoError(t, r.UpdateTurnControl(ctx, "t1", "nonexistent", ControlStop))
	turns, _, err := r.HydrateThread(ctx, "t1")
	require.NoError(t, err)
	assert.Empty(t, turns)
}

func TestMemoryRepository_UpdateParentOnUnknownTurnIsNoop(t *testing.T) {
	r := NewMemoryRepository()
	ctx := context.Background()
	require.NoError(t, r.UpdateTurnParent(ctx, "t1", "nonexistent", "p1"))
	turns, _, err := r.HydrateThread(ctx, "t1")
	require.NoError(t, err)
	assert.Empty(t, turns)
}
