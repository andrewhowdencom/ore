package ledger

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFileRepository_DirectoryCreated(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "journals")
	r, err := NewFileRepository(dir)
	require.NoError(t, err)
	require.NotNil(t, r)

	info, err := os.Stat(dir)
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestFileRepository_SaveTurnAndHydrate(t *testing.T) {
	dir := t.TempDir()
	r, err := NewFileRepository(dir)
	require.NoError(t, err)
	ctx := context.Background()

	turn := &Turn{
		ID:        "turn-1",
		Role:      RoleUser,
		Artifacts: []artifact.Artifact{artifact.Text{Content: "hello"}},
		Timestamp: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	require.NoError(t, r.SaveTurn(ctx, "thread-1", turn))

	// Verify file exists.
	path := filepath.Join(dir, "thread-1.jsonl")
	_, err = os.Stat(path)
	require.NoError(t, err)

	turns, tip, err := r.HydrateThread(ctx, "thread-1")
	require.NoError(t, err)
	require.Contains(t, turns, "turn-1")
	assert.Equal(t, RoleUser, turns["turn-1"].Role)
	assert.Empty(t, tip)
	require.Len(t, turns["turn-1"].Artifacts, 1)
	assert.Equal(t, "hello", turns["turn-1"].Artifacts[0].(artifact.Text).Content)
}

func TestFileRepository_FullRoundTrip(t *testing.T) {
	dir := t.TempDir()
	r, err := NewFileRepository(dir)
	require.NoError(t, err)
	ctx := context.Background()

	// Build: u1 → a1 → u2 → summary (Stop) → u3 → a3
	require.NoError(t, r.SaveTurn(ctx, "thread-1", &Turn{ID: "u1", Role: RoleUser, Timestamp: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}))
	require.NoError(t, r.SaveTurn(ctx, "thread-1", &Turn{ID: "a1", Role: RoleAssistant, ParentID: "u1", Timestamp: time.Date(2024, 1, 1, 0, 0, 1, 0, time.UTC)}))
	require.NoError(t, r.SaveTurn(ctx, "thread-1", &Turn{ID: "u2", Role: RoleUser, ParentID: "a1", Timestamp: time.Date(2024, 1, 1, 0, 0, 2, 0, time.UTC)}))
	require.NoError(t, r.SaveTurn(ctx, "thread-1", &Turn{
		ID:        "summary",
		Role:      RoleAssistant,
		ParentID:  "u2",
		Timestamp: time.Date(2024, 1, 1, 0, 0, 3, 0, time.UTC),
		Metadata:  Metadata{Control: ControlStop},
	}))
	require.NoError(t, r.UpdateThreadTip(ctx, "thread-1", "summary"))
	require.NoError(t, r.SaveTurn(ctx, "thread-1", &Turn{ID: "u3", Role: RoleUser, ParentID: "summary", Timestamp: time.Date(2024, 1, 1, 0, 0, 4, 0, time.UTC)}))
	require.NoError(t, r.SaveTurn(ctx, "thread-1", &Turn{ID: "a3", Role: RoleAssistant, ParentID: "u3", Timestamp: time.Date(2024, 1, 1, 0, 0, 5, 0, time.UTC)}))
	require.NoError(t, r.UpdateThreadTip(ctx, "thread-1", "a3"))

	// Reconstruct via HydrateThread.
	turns, tip, err := r.HydrateThread(ctx, "thread-1")
	require.NoError(t, err)
	assert.Equal(t, "a3", tip)
	require.Len(t, turns, 6)

	// Build a Thread and verify the walk.
	th := NewThread()
	for _, turn := range turns {
		th.SaveTurn(turn)
	}
	th.SetCurrentTip(tip)

	got := th.Turns()
	require.Len(t, got, 3)
	assert.Equal(t, "summary", got[0].ID)
	assert.Equal(t, "u3", got[1].ID)
	assert.Equal(t, "a3", got[2].ID)
}

func TestFileRepository_HydrateMissingFile(t *testing.T) {
	dir := t.TempDir()
	r, err := NewFileRepository(dir)
	require.NoError(t, err)

	turns, tip, err := r.HydrateThread(context.Background(), "nonexistent")
	require.NoError(t, err)
	assert.Empty(t, turns)
	assert.Empty(t, tip)
}

func TestFileRepository_TruncatedLastLineTolerated(t *testing.T) {
	dir := t.TempDir()
	r, err := NewFileRepository(dir)
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, r.SaveTurn(ctx, "thread-1", &Turn{ID: "turn-1", Role: RoleUser, Timestamp: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}))
	require.NoError(t, r.SaveTurn(ctx, "thread-1", &Turn{ID: "turn-2", Role: RoleUser, ParentID: "turn-1", Timestamp: time.Date(2024, 1, 1, 0, 0, 1, 0, time.UTC)}))

	// Append a partial (truncated) JSON line, simulating a crash mid-write.
	path := filepath.Join(dir, "thread-1.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	require.NoError(t, err)
	_, err = f.WriteString(`{"tx_type":"add_turn","timestamp":"2024-01-01T00:00:02Z","payload":{"timestamp":"2024-01-01T00:00:02Z","turn":{"id":"turn-3","role":"user`)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	// Hydrate should stop at the truncated line; turns 1 and 2 should be present.
	turns, _, err := r.HydrateThread(ctx, "thread-1")
	require.NoError(t, err)
	require.Contains(t, turns, "turn-1")
	require.Contains(t, turns, "turn-2")
	assert.NotContains(t, turns, "turn-3")
}

func TestFileRepository_MultipleThreads(t *testing.T) {
	dir := t.TempDir()
	r, err := NewFileRepository(dir)
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, r.SaveTurn(ctx, "thread-a", &Turn{ID: "u-a", Role: RoleUser, Timestamp: time.Now()}))
	require.NoError(t, r.SaveTurn(ctx, "thread-b", &Turn{ID: "u-b", Role: RoleUser, Timestamp: time.Now()}))

	turnsA, _, err := r.HydrateThread(ctx, "thread-a")
	require.NoError(t, err)
	assert.Contains(t, turnsA, "u-a")
	assert.NotContains(t, turnsA, "u-b")

	turnsB, _, err := r.HydrateThread(ctx, "thread-b")
	require.NoError(t, err)
	assert.Contains(t, turnsB, "u-b")
	assert.NotContains(t, turnsB, "u-a")
}

func TestFileRepository_UpdateControl(t *testing.T) {
	dir := t.TempDir()
	r, err := NewFileRepository(dir)
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, r.SaveTurn(ctx, "thread-1", &Turn{ID: "t1", Role: RoleUser, Timestamp: time.Now()}))
	require.NoError(t, r.UpdateTurnControl(ctx, "thread-1", "t1", ControlSkip))

	turns, _, err := r.HydrateThread(ctx, "thread-1")
	require.NoError(t, err)
	assert.Equal(t, ControlSkip, turns["t1"].Metadata.Control)
}

func TestFileRepository_UpdateParent(t *testing.T) {
	dir := t.TempDir()
	r, err := NewFileRepository(dir)
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, r.SaveTurn(ctx, "thread-1", &Turn{ID: "t1", Role: RoleUser, Timestamp: time.Now()}))
	require.NoError(t, r.SaveTurn(ctx, "thread-1", &Turn{ID: "t2", Role: RoleUser, ParentID: "t1", Timestamp: time.Now()}))
	require.NoError(t, r.UpdateTurnParent(ctx, "thread-1", "t2", ""))

	turns, _, err := r.HydrateThread(ctx, "thread-1")
	require.NoError(t, err)
	assert.Equal(t, "", turns["t2"].ParentID)
}

func TestFileRepository_SanitizeThreadID(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"abc-123", "abc-123"},
		{"abc/123", "abc_123"},
		{"../escape", ".._escape"},
		{"with spaces", "with_spaces"},
		{"with.dots.ok", "with.dots.ok"},
		{"", "default"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, sanitizeThreadID(tt.in))
		})
	}
}

func TestFileRepository_AppendIsAppendOnly(t *testing.T) {
	dir := t.TempDir()
	r, err := NewFileRepository(dir)
	require.NoError(t, err)
	ctx := context.Background()

	require.NoError(t, r.SaveTurn(ctx, "thread-1", &Turn{ID: "t1", Role: RoleUser, Timestamp: time.Now()}))
	require.NoError(t, r.SaveTurn(ctx, "thread-1", &Turn{ID: "t2", Role: RoleUser, ParentID: "t1", Timestamp: time.Now()}))
	require.NoError(t, r.SaveTurn(ctx, "thread-1", &Turn{ID: "t3", Role: RoleUser, ParentID: "t2", Timestamp: time.Now()}))

	// File should have exactly 3 lines (plus a trailing newline).
	path := filepath.Join(dir, "thread-1.jsonl")
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}
	assert.Equal(t, 3, lines, "each append must produce exactly one line")
}