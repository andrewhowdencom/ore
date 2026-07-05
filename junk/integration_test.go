package junk

import (
	"encoding/json"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONStore_CrossConduitContinuity(t *testing.T) {
	dir := t.TempDir()

	// Step 1: Create a JSONStore and thread.
	store1, err := NewJSONStore(dir)
	require.NoError(t, err)

	thread, err := store1.Create()
	require.NoError(t, err)

	// Set metadata for conduit thread mapping.
	thread.Metadata["slack.thread_ts"] = "1234567890.123456"

	// Step 2: Append user and assistant turns.
	thread.State.Append(ledger.RoleUser, artifact.Text{Content: "hello"})
	thread.State.Append(ledger.RoleAssistant, artifact.Text{Content: "hi there"})

	// Step 3: Save the thread.
	// CreatedAt/UpdatedAt are no longer tracked; the conversation's
	// temporal data lives in the turn history.
	err = store1.Save(thread)
	require.NoError(t, err)

	// Step 4: Create a new JSONStore instance (simulating restart).
	store2, err := NewJSONStore(dir)
	require.NoError(t, err)

	// Step 5: Load the thread and verify turns.
	got, err := store2.Get(thread.ID)
	require.NoError(t, err)
	assert.Equal(t, thread.ID, got.ID)

	turns := got.State.Turns()
	require.Len(t, turns, 2)

	assert.Equal(t, ledger.RoleUser, turns[0].Role)
	require.Len(t, turns[0].Artifacts, 1)
	assert.Equal(t, "text", turns[0].Artifacts[0].Kind())
	assert.Equal(t, artifact.Text{Content: "hello"}, turns[0].Artifacts[0])

	assert.Equal(t, ledger.RoleAssistant, turns[1].Role)
	require.Len(t, turns[1].Artifacts, 1)
	assert.Equal(t, "text", turns[1].Artifacts[0].Kind())
	assert.Equal(t, artifact.Text{Content: "hi there"}, turns[1].Artifacts[0])

	// Step 6: Verify metadata. CreatedAt/UpdatedAt are no longer
	// persisted — turn timestamps carry the conversation's temporal data.
	v := got.Metadata["slack.thread_ts"]
	assert.Equal(t, "1234567890.123456", v)
}

func TestThread_MarshalJSON(t *testing.T) {
	thread := &Thread{
		ID:       "test-id",
		State:    ledger.NewThread(),
		Metadata: map[string]string{"channel_id": "123", "user_id": "abc"},
	}
	thread.State.Append(ledger.RoleUser, artifact.Text{Content: "hello"})
	thread.State.Append(ledger.RoleAssistant, artifact.Text{Content: "hi there"})

	data, err := json.Marshal(thread)
	require.NoError(t, err)

	got := &Thread{}
	err = json.Unmarshal(data, got)
	require.NoError(t, err)

	assert.Equal(t, thread.ID, got.ID)
	assert.Equal(t, thread.Metadata, got.Metadata)
	turns := got.State.Turns()
	require.Len(t, turns, 2)
	assert.Equal(t, ledger.RoleUser, turns[0].Role)
	assert.Equal(t, ledger.RoleAssistant, turns[1].Role)
}
