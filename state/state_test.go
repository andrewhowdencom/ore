package state

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
		Role:      RoleUser,
		Artifacts: []artifact.Artifact{artifact.Text{Content: "hello"}},
		Timestamp: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	data, err := json.Marshal(turn)
	require.NoError(t, err)
	assert.JSONEq(t, `{"role":"user","artifacts":[{"kind":"text","content":"hello"}],"timestamp":"2024-01-01T00:00:00Z"}`, string(data))
}

func TestTurn_MarshalJSON_NoTimestamp(t *testing.T) {
	turn := Turn{
		Role:      RoleAssistant,
		Artifacts: []artifact.Artifact{},
	}
	data, err := json.Marshal(turn)
	require.NoError(t, err)
	assert.JSONEq(t, `{"role":"assistant","artifacts":[]}`, string(data))
}
