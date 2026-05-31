package session

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/andrewhowdencom/ore/state"
)

// Thread represents a persistent thread with identity,
// state, and metadata.
type Thread struct {
	// ID is the unique identifier for this thread (random UUID).
	ID string
	// State holds the mutable thread turn history.
	State *state.Buffer
	// CreatedAt is set when the thread is first created.
	CreatedAt time.Time
	// UpdatedAt is advanced on every successful Save.
	UpdatedAt time.Time
	// Metadata holds arbitrary key-value pairs for conduit-specific
	// thread mapping (e.g., external system identifiers).
	Metadata map[string]string
}

// MarshalJSON serializes the thread to JSON.
func (c *Thread) MarshalJSON() ([]byte, error) {
	type jsonThread struct {
		ID        string            `json:"id"`
		CreatedAt time.Time         `json:"created_at"`
		UpdatedAt time.Time         `json:"updated_at"`
		Metadata  map[string]string `json:"metadata,omitempty"`
		Turns     json.RawMessage   `json:"turns"`
	}

	turnsJSON, err := marshalTurns(c.State.Turns())
	if err != nil {
		return nil, fmt.Errorf("marshal turns: %w", err)
	}

	jc := jsonThread{
		ID:        c.ID,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
		Metadata:  c.Metadata,
		Turns:     turnsJSON,
	}

	return json.Marshal(jc)
}

// UnmarshalJSON deserializes a thread from JSON.
func (c *Thread) UnmarshalJSON(data []byte) error {
	type jsonThread struct {
		ID        string            `json:"id"`
		CreatedAt time.Time         `json:"created_at"`
		UpdatedAt time.Time         `json:"updated_at"`
		Metadata  map[string]string `json:"metadata,omitempty"`
		Turns     json.RawMessage   `json:"turns"`
	}

	var jc jsonThread
	if err := json.Unmarshal(data, &jc); err != nil {
		return fmt.Errorf("unmarshal thread: %w", err)
	}

	turns, err := unmarshalTurns(jc.Turns)
	if err != nil {
		return fmt.Errorf("unmarshal turns: %w", err)
	}

	c.ID = jc.ID
	c.CreatedAt = jc.CreatedAt
	c.UpdatedAt = jc.UpdatedAt
	if jc.Metadata != nil {
		c.Metadata = jc.Metadata
	} else {
		c.Metadata = make(map[string]string)
	}
	c.State = &state.Buffer{}
	for _, turn := range turns {
		c.State.Append(turn.Role, turn.Artifacts...)
	}

	return nil
}
