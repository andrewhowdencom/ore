package conversation

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/andrewhowdencom/ore/state"
)

// Store defines the interface for conversation persistence.
type Store interface {
	Create() (*Conversation, error)
	Get(id string) (*Conversation, bool)
	Save(conv *Conversation) error
	Delete(id string) bool
	List() ([]*Conversation, error)
}

// Conversation represents a persistent conversation with identity,
// state, and locking.
type Conversation struct {
	ID        string
	State     *state.Memory
	CreatedAt time.Time
	UpdatedAt time.Time
	mu        sync.Mutex
	busy      bool
}

// Lock attempts to acquire the conversation lock.
// Returns false if the conversation is already busy.
func (c *Conversation) Lock() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.busy {
		return false
	}
	c.busy = true
	return true
}

// Unlock releases the conversation lock.
func (c *Conversation) Unlock() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.busy = false
}

// MarshalJSON serializes the conversation to JSON.
func (c *Conversation) MarshalJSON() ([]byte, error) {
	type jsonConversation struct {
		ID        string          `json:"id"`
		CreatedAt time.Time       `json:"created_at"`
		UpdatedAt time.Time       `json:"updated_at"`
		Turns     json.RawMessage `json:"turns"`
	}

	turnsJSON, err := marshalTurns(c.State.Turns())
	if err != nil {
		return nil, fmt.Errorf("marshal turns: %w", err)
	}

	jc := jsonConversation{
		ID:        c.ID,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
		Turns:     turnsJSON,
	}

	return json.Marshal(jc)
}

// UnmarshalJSON deserializes a conversation from JSON.
func (c *Conversation) UnmarshalJSON(data []byte) error {
	type jsonConversation struct {
		ID        string          `json:"id"`
		CreatedAt time.Time       `json:"created_at"`
		UpdatedAt time.Time       `json:"updated_at"`
		Turns     json.RawMessage `json:"turns"`
	}

	var jc jsonConversation
	if err := json.Unmarshal(data, &jc); err != nil {
		return fmt.Errorf("unmarshal conversation: %w", err)
	}

	turns, err := unmarshalTurns(jc.Turns)
	if err != nil {
		return fmt.Errorf("unmarshal turns: %w", err)
	}

	c.ID = jc.ID
	c.CreatedAt = jc.CreatedAt
	c.UpdatedAt = jc.UpdatedAt
	c.State = &state.Memory{}
	for _, turn := range turns {
		c.State.Append(turn.Role, turn.Artifacts...)
	}

	return nil
}
