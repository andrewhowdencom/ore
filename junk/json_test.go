package junk

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONStore_CreateCreatesFile(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONStore(dir)
	require.NoError(t, err)

	thread, err := store.Create()
	require.NoError(t, err)

	path := filepath.Join(dir, thread.ID+".json")
	_, err = os.Stat(path)
	require.NoError(t, err, "expected file to exist after Create")
}

func TestJSONStore_SaveUpdatesFile(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONStore(dir)
	require.NoError(t, err)

	thread, err := store.Create()
	require.NoError(t, err)

	// Append a turn and save.
	thread.State.Append(ledger.RoleUser, artifact.Text{Content: "hello"})
	err = store.Save(thread)
	require.NoError(t, err)

	// Verify by loading into a new store (simulating restart).
	store2, err := NewJSONStore(dir)
	require.NoError(t, err)

	got, err := store2.Get(thread.ID)
	require.NoError(t, err)
	turns := got.State.Turns()
	require.Len(t, turns, 1)
	assert.Equal(t, ledger.RoleUser, turns[0].Role)
	require.Len(t, turns[0].Artifacts, 1)
	assert.Equal(t, "text", turns[0].Artifacts[0].Kind())
}

func TestJSONStore_GetLoadsFromDisk(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONStore(dir)
	require.NoError(t, err)

	thread, err := store.Create()
	require.NoError(t, err)
	thread.State.Append(ledger.RoleUser, artifact.Text{Content: "hello"})
	require.NoError(t, store.Save(thread))

	// Create a fresh store pointing at the same directory.
	store2, err := NewJSONStore(dir)
	require.NoError(t, err)

	got, err := store2.Get(thread.ID)
	require.NoError(t, err)
	assert.Equal(t, thread.ID, got.ID)
	assert.Len(t, got.State.Turns(), 1)
}

func TestJSONStore_DeleteRemovesFile(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONStore(dir)
	require.NoError(t, err)

	thread, err := store.Create()
	require.NoError(t, err)

	path := filepath.Join(dir, thread.ID+".json")
	_, err = os.Stat(path)
	require.NoError(t, err)

	ok := store.Delete(thread.ID)
	assert.True(t, ok)

	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err), "expected file to be removed")

	_, err = store.Get(thread.ID)
	assert.ErrorIs(t, err, ErrThreadNotFound)
}

func TestJSONStore_RestartRecoversThreads(t *testing.T) {
	dir := t.TempDir()

	// First store instance.
	store1, err := NewJSONStore(dir)
	require.NoError(t, err)

	thread1, err := store1.Create()
	require.NoError(t, err)
	thread1.State.Append(ledger.RoleUser, artifact.Text{Content: "msg1"})
	require.NoError(t, store1.Save(thread1))

	thread2, err := store1.Create()
	require.NoError(t, err)
	thread2.State.Append(ledger.RoleUser, artifact.Text{Content: "msg2"})
	require.NoError(t, store1.Save(thread2))

	// Second store instance (simulating process restart).
	store2, err := NewJSONStore(dir)
	require.NoError(t, err)

	got1, err := store2.Get(thread1.ID)
	require.NoError(t, err)
	assert.Len(t, got1.State.Turns(), 1)

	got2, err := store2.Get(thread2.ID)
	require.NoError(t, err)
	assert.Len(t, got2.State.Turns(), 1)
}

func TestJSONStore_RoundTripPreservesTurns(t *testing.T) {
	dir := t.TempDir()
	store1, err := NewJSONStore(dir)
	require.NoError(t, err)

	thread, err := store1.Create()
	require.NoError(t, err)

	thread.State.Append(ledger.RoleUser, artifact.Text{Content: "hello"})
	require.NoError(t, store1.Save(thread))

	store2, err := NewJSONStore(dir)
	require.NoError(t, err)

	got, err := store2.Get(thread.ID)
	require.NoError(t, err)
	assert.Equal(t, thread.ID, got.ID)
	turns := got.State.Turns()
	require.Len(t, turns, 1)
	assert.Equal(t, "text", turns[0].Artifacts[0].Kind())
}

func TestJSONStore_List(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONStore(dir)
	require.NoError(t, err)

	thread1, err := store.Create()
	require.NoError(t, err)
	thread2, err := store.Create()
	require.NoError(t, err)

	list, err := store.List()
	require.NoError(t, err)
	require.Len(t, list, 2)

	ids := make(map[string]bool)
	for _, thread := range list {
		ids[thread.ID] = true
	}
	assert.True(t, ids[thread1.ID])
	assert.True(t, ids[thread2.ID])
}

func TestJSONStore_ConcurrentCreate(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONStore(dir)
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := store.Create()
			require.NoError(t, err)
		}()
	}
	wg.Wait()
}

func TestJSONStore_CorruptedFile(t *testing.T) {
	dir := t.TempDir()

	// Write a corrupted JSON file.
	err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte("not json"), 0644)
	require.NoError(t, err)

	// Write a valid thread file.
	valid := &Thread{
		ID:    "good",
		State: ledger.NewThread(),
	}
	valid.State.Append(ledger.RoleUser, artifact.Text{Content: "hello"})
	data, err := json.Marshal(valid)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dir, "good.json"), data, 0644)
	require.NoError(t, err)

	// Create store — corrupted file should surface ErrThreadCorrupt on
// direct lookup (the old code silently swallowed this and reported
// "not found", which is the bug this test now guards against).
// List() continues to skip corrupt files because it scans all IDs;
// the per-file lookup is what distinguishes the two cases.
	store, err := NewJSONStore(dir)
	require.NoError(t, err)

	// good should be loadable.
	got, err := store.Get("good")
	require.NoError(t, err)
	assert.Len(t, got.State.Turns(), 1)

	// bad should be reported as corrupt, not as missing.
	_, err = store.Get("bad")
	assert.ErrorIs(t, err, ErrThreadCorrupt)

	// List should only include good (corrupt files are skipped on scan).
	list, err := store.List()
	require.NoError(t, err)
	assert.Len(t, list, 1)
	assert.Equal(t, "good", list[0].ID)
}

func TestJSONStore_ConcurrentCreateSaveGet(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONStore(dir)
	require.NoError(t, err)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			thread, err := store.Create()
			require.NoError(t, err)
			thread.State.Append(ledger.RoleUser, artifact.Text{Content: fmt.Sprintf("msg-%d", i)})
			require.NoError(t, store.Save(thread))

			got, err := store.Get(thread.ID)
			require.NoError(t, err)
			assert.Len(t, got.State.Turns(), 1)
		}(i)
	}
	wg.Wait()

	// Verify all 50 threads exist.
	list, err := store.List()
	require.NoError(t, err)
	assert.Len(t, list, 50)
}

func TestJSONStore_GetBy(t *testing.T) {
	dir := t.TempDir()
	store1, err := NewJSONStore(dir)
	require.NoError(t, err)

	thread1, err := store1.Create()
	require.NoError(t, err)
	_, err = store1.Create()
	require.NoError(t, err)

	thread1.Metadata["slack.thread_ts"] = "1234567890.123456"
	err = store1.Save(thread1)
	require.NoError(t, err)

	// Verify via fresh store (restart simulation).
	store2, err := NewJSONStore(dir)
	require.NoError(t, err)

	got, err := store2.GetBy("slack.thread_ts", "1234567890.123456")
	require.NoError(t, err)
	assert.Equal(t, thread1.ID, got.ID)

	_, err = store2.GetBy("slack.thread_ts", "999")
	assert.ErrorIs(t, err, ErrThreadNotFound)
}

func TestJSONStore_GetBy_NotFound(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONStore(dir)
	require.NoError(t, err)

	_, err = store.Create()
	require.NoError(t, err)

	_, err = store.GetBy("channel_id", "999")
	assert.ErrorIs(t, err, ErrThreadNotFound)
}

func TestJSONStore_LegacyJSONNoMetadata(t *testing.T) {
	dir := t.TempDir()

	// Write a JSON file in the old format (no metadata field).
	oldJSON := `{"id":"legacy-thread","created_at":"2024-01-01T00:00:00Z","updated_at":"2024-01-01T00:00:00Z","turns":[]}`
	err := os.WriteFile(filepath.Join(dir, "legacy-thread.json"), []byte(oldJSON), 0644)
	require.NoError(t, err)

	store, err := NewJSONStore(dir)
	require.NoError(t, err)

	got, err := store.Get("legacy-thread")
	require.NoError(t, err)
	assert.NotNil(t, got.Metadata)
	assert.Len(t, got.Metadata, 0)

	_, err = store.GetBy("any_key", "any_value")
	assert.ErrorIs(t, err, ErrThreadNotFound)
}

func TestJSONStore_GetBy_Duplicate(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONStore(dir)
	require.NoError(t, err)

	thread1, err := store.Create()
	require.NoError(t, err)
	thread2, err := store.Create()
	require.NoError(t, err)

	thread1.Metadata["channel_id"] = "same"
	thread2.Metadata["channel_id"] = "same"
	require.NoError(t, store.Save(thread1))
	require.NoError(t, store.Save(thread2))

	got, err := store.GetBy("channel_id", "same")
	require.NoError(t, err)
	assert.True(t, got.ID == thread1.ID || got.ID == thread2.ID)
}

func TestJSONStore_GetBy_AfterDelete(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONStore(dir)
	require.NoError(t, err)

	thread, err := store.Create()
	require.NoError(t, err)

	thread.Metadata["channel_id"] = "123"
	require.NoError(t, store.Save(thread))

	ok := store.Delete(thread.ID)
	assert.True(t, ok)

	_, err = store.GetBy("channel_id", "123")
	assert.ErrorIs(t, err, ErrThreadNotFound)
}

func TestJSONStore_GetBy_EmptyMetadata(t *testing.T) {
	dir := t.TempDir()
	store, err := NewJSONStore(dir)
	require.NoError(t, err)

	_, err = store.Create()
	require.NoError(t, err)

	_, err = store.GetBy("any_key", "any_value")
	assert.ErrorIs(t, err, ErrThreadNotFound)
}
