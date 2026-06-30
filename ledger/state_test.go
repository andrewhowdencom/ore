package ledger

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTurn_MarshalJSON(t *testing.T) {
	turn := Turn{
		ID:        "turn-1",
		Role:      RoleUser,
		Artifacts: []artifact.Artifact{artifact.Text{Content: "hello"}},
		Timestamp: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	data, err := json.Marshal(turn)
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"turn-1","role":"user","artifacts":[{"kind":"text","content":"hello"}],"timestamp":"2024-01-01T00:00:00Z"}`, string(data))
}

func TestTurn_MarshalJSON_NoTimestamp(t *testing.T) {
	turn := Turn{
		ID:        "turn-2",
		Role:      RoleAssistant,
		Artifacts: []artifact.Artifact{},
	}
	data, err := json.Marshal(turn)
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"turn-2","role":"assistant","artifacts":[]}`, string(data))
}

func TestTurn_MarshalJSON_WithParentID(t *testing.T) {
	turn := Turn{
		ID:        "turn-3",
		ParentID:  "turn-2",
		Role:      RoleAssistant,
		Artifacts: []artifact.Artifact{artifact.Text{Content: "reply"}},
	}
	data, err := json.Marshal(turn)
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"turn-3","parent_id":"turn-2","role":"assistant","artifacts":[{"kind":"text","content":"reply"}]}`, string(data))
}

func TestTurn_MarshalJSON_WithControl(t *testing.T) {
	turn := Turn{
		ID:        "summary-1",
		Role:      RoleAssistant,
		Artifacts: []artifact.Artifact{artifact.Text{Content: "summary"}},
		Metadata:  Metadata{Control: ControlStop},
	}
	data, err := json.Marshal(turn)
	require.NoError(t, err)
	assert.JSONEq(t, `{"id":"summary-1","role":"assistant","artifacts":[{"kind":"text","content":"summary"}],"metadata":{"control":"stop"}}`, string(data))
}

func TestTurn_UnmarshalJSON(t *testing.T) {
	data := []byte(`{"id":"turn-1","parent_id":"turn-0","role":"user","timestamp":"2024-01-01T00:00:00Z"}`)
	var got Turn
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, "turn-1", got.ID)
	assert.Equal(t, "turn-0", got.ParentID)
	assert.Equal(t, RoleUser, got.Role)
	assert.Equal(t, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), got.Timestamp)
}

func TestTurn_UnmarshalJSON_WithMetadata(t *testing.T) {
	data := []byte(`{"id":"s1","role":"assistant","metadata":{"control":"skip"}}`)
	var got Turn
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, ControlSkip, got.Metadata.Control)
}

func TestTraversalControl_Constants(t *testing.T) {
	assert.Equal(t, TraversalControl("continue"), ControlContinue)
	assert.Equal(t, TraversalControl("stop"), ControlStop)
	assert.Equal(t, TraversalControl("skip"), ControlSkip)
}

func TestGenerateTurnID_Unique(t *testing.T) {
	const n = 1000
	seen := make(map[string]struct{}, n)
	for i := 0; i < n; i++ {
		id := generateTurnID()
		assert.Len(t, id, 16, "ID should be 16 hex chars")
		_, dup := seen[id]
		assert.False(t, dup, "duplicate ID generated: %s", id)
		seen[id] = struct{}{}
	}
}
