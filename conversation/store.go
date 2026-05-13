package conversation

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/andrewhowdencom/ore/state"
)

// Store abstracts persistence for Conversation instances.
// Implementations must be safe for concurrent use.
type Store interface {
	// Create generates a new Conversation with a random UUID and stores it.
	Create() (*Conversation, error)
	// Get retrieves a Conversation by ID. The second return value is false
	// if the conversation does not exist.
	Get(id string) (*Conversation, bool)
	// Save persists the given Conversation, updating its UpdatedAt timestamp.
	Save(conv *Conversation) error
	// Delete removes a Conversation by ID and returns true if it existed.
	Delete(id string) bool
	// List returns all stored Conversations.
	List() ([]*Conversation, error)
}

// Conversation represents a persistent conversation with identity,
// state, and per-conversation locking.
type Conversation struct {
	// ID is the unique identifier for this conversation (random UUID).
	ID string
	// State holds the mutable conversation turn history.
	// It is not safe for concurrent use; callers must hold the lock.
	State *state.Memory
	// CreatedAt is set when the conversation is first created.
	CreatedAt time.Time
	// UpdatedAt is advanced on every successful Save.
	UpdatedAt time.Time
	mu        sync.Mutex
	busy      bool
}

// Lock attempts to acquire the conversation lock in a non-blocking manner.
// Returns true if the lock was acquired, or false if the conversation is
// already busy (another conduit holds the lock).
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
