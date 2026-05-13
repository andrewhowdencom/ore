package conversation

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONStore_CrossConduitContinuity(t *testing.T) {
	dir := t.TempDir()

	// Step 1: Create a JSONStore and conversation.
	store1, err := NewJSONStore(dir)
	require.NoError(t, err)

	conv, err := store1.Create()
	require.NoError(t, err)
	createdAt := conv.CreatedAt

	// Step 2: Append user and assistant turns.
	conv.State.Append(state.RoleUser, artifact.Text{Content: "hello"})
	conv.State.Append(state.RoleAssistant, artifact.Text{Content: "hi there"})

	// Step 3: Save the conversation.
	time.Sleep(1 * time.Millisecond) // ensure time advances
	err = store1.Save(conv)
	require.NoError(t, err)

	// Step 4: Create a new JSONStore instance (simulating restart).
	store2, err := NewJSONStore(dir)
	require.NoError(t, err)

	// Step 5: Load the conversation and verify turns.
	got, ok := store2.Get(conv.ID)
	require.True(t, ok)
	assert.Equal(t, conv.ID, got.ID)

	turns := got.State.Turns()
	require.Len(t, turns, 2)

	assert.Equal(t, state.RoleUser, turns[0].Role)
	require.Len(t, turns[0].Artifacts, 1)
	assert.Equal(t, "text", turns[0].Artifacts[0].Kind())
	assert.Equal(t, &artifact.Text{Content: "hello"}, turns[0].Artifacts[0])

	assert.Equal(t, state.RoleAssistant, turns[1].Role)
	require.Len(t, turns[1].Artifacts, 1)
	assert.Equal(t, "text", turns[1].Artifacts[0].Kind())
	assert.Equal(t, &artifact.Text{Content: "hi there"}, turns[1].Artifacts[0])

	// Step 6: Verify timestamps.
	assert.True(t, createdAt.Equal(got.CreatedAt), "CreatedAt should be preserved")
	assert.True(t, got.UpdatedAt.After(createdAt), "UpdatedAt should reflect the save")
}

func TestConversation_MarshalJSON(t *testing.T) {
	conv := &Conversation{
		ID:        "test-id",
		State:     &state.Memory{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	conv.State.Append(state.RoleUser, artifact.Text{Content: "hello"})
	conv.State.Append(state.RoleAssistant, artifact.Text{Content: "hi there"})

	data, err := json.Marshal(conv)
	require.NoError(t, err)

	got := &Conversation{}
	err = json.Unmarshal(data, got)
	require.NoError(t, err)

	assert.Equal(t, conv.ID, got.ID)
	assert.True(t, conv.CreatedAt.Equal(got.CreatedAt))
	assert.True(t, conv.UpdatedAt.Equal(got.UpdatedAt))
	turns := got.State.Turns()
	require.Len(t, turns, 2)
	assert.Equal(t, state.RoleUser, turns[0].Role)
	assert.Equal(t, state.RoleAssistant, turns[1].Role)
}
