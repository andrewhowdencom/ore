package ledger

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJournalEntry_MarshalJSON(t *testing.T) {
	entry := JournalEntry{
		TxType:    TxAddTurn,
		Timestamp: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		Payload:   json.RawMessage(`{"foo":"bar"}`),
	}
	data, err := json.Marshal(entry)
	require.NoError(t, err)
	assert.JSONEq(t, `{"tx_type":"add_turn","timestamp":"2024-01-01T00:00:00Z","payload":{"foo":"bar"}}`, string(data))
}

func TestJournalEntry_UnmarshalJSON(t *testing.T) {
	data := []byte(`{"tx_type":"update_tip","timestamp":"2024-01-01T00:00:00Z","payload":{"current_tip":"turn-5"}}`)
	var got JournalEntry
	require.NoError(t, json.Unmarshal(data, &got))
	assert.Equal(t, TxUpdateTip, got.TxType)
	assert.Equal(t, time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC), got.Timestamp)
	assert.JSONEq(t, `{"current_tip":"turn-5"}`, string(got.Payload))
}

func TestTxType_Constants(t *testing.T) {
	assert.Equal(t, TxType("add_turn"), TxAddTurn)
	assert.Equal(t, TxType("update_tip"), TxUpdateTip)
	assert.Equal(t, TxType("update_control"), TxUpdateControl)
	assert.Equal(t, TxType("update_parent"), TxUpdateParent)
}

func TestPayloads_RoundTrip(t *testing.T) {
	ts := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		payload any
	}{
		{"add_turn", AddTurnPayload{
			Timestamp: ts,
			Turn: Turn{
				ID:        "turn-1",
				Role:      RoleUser,
				Artifacts: nil,
			},
		}},
		{"update_tip", UpdateTipPayload{
			Timestamp: ts,
			CurrentTip: "turn-1",
		}},
		{"update_control", UpdateControlPayload{
			Timestamp: ts,
			TurnID:    "turn-1",
			Control:   ControlStop,
		}},
		{"update_parent", UpdateParentPayload{
			Timestamp: ts,
			TurnID:    "turn-1",
			ParentID:  "turn-0",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := json.Marshal(tt.payload)
			require.NoError(t, err)
			entry := JournalEntry{
				TxType:    TxType(tt.name),
				Timestamp: ts,
				Payload:   data,
			}
			wire, err := json.Marshal(entry)
			require.NoError(t, err)

			var got JournalEntry
			require.NoError(t, json.Unmarshal(wire, &got))
			assert.Equal(t, tt.name, string(got.TxType))
			assert.JSONEq(t, string(data), string(got.Payload))
		})
	}
}

func TestRepository_InterfaceCheck(t *testing.T) {
	// A trivial in-memory implementation should satisfy Repository.
	// (Implementation arrives in the next task; this test only checks
	// the interface compiles correctly here.)
	var _ Repository = (*testRepository)(nil)
}

type testRepository struct{}

func (*testRepository) SaveTurn(_ context.Context, _ string, _ *Turn) error { return nil }
func (*testRepository) UpdateThreadTip(_ context.Context, _, _ string) error   { return nil }
func (*testRepository) UpdateTurnControl(_ context.Context, _, _ string, _ TraversalControl) error {
	return nil
}
func (*testRepository) UpdateTurnParent(_ context.Context, _, _, _ string) error { return nil }
func (*testRepository) HydrateThread(_ context.Context, _ string) (map[string]*Turn, string, error) {
	return nil, "", nil
}