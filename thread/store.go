package thread

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/andrewhowdencom/ore/state"
)

// Store abstracts persistence for Thread instances.
// Implementations must be safe for concurrent use.
type Store interface {
	// Create generates a new Thread with a random UUID and stores it.
	Create() (*Thread, error)
	// Get retrieves a Thread by ID. The second return value is false
	// if the thread does not exist.
	Get(id string) (*Thread, bool)
	// Save persists the given Thread, updating its UpdatedAt timestamp.
	Save(thread *Thread) error
	// Delete removes a Thread by ID and returns true if it existed.
	Delete(id string) bool
	// List returns all stored Threads.
	List() ([]*Thread, error)
}

// Thread represents a persistent thread with identity,
// state, and per-thread locking.
type Thread struct {
	// ID is the unique identifier for this thread (random UUID).
	ID string
	// State holds the mutable thread turn history.
	// It is not safe for concurrent use; callers must hold the lock.
	State *state.Memory
	// CreatedAt is set when the thread is first created.
	CreatedAt time.Time
	// UpdatedAt is advanced on every successful Save.
	UpdatedAt time.Time
	mu        sync.Mutex
	busy      bool
}

// Lock attempts to acquire the thread lock in a non-blocking manner.
// Returns true if the lock was acquired, or false if the thread is
// already busy (another conduit holds the lock).
func (c *Thread) Lock() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.busy {
		return false
	}
	c.busy = true
	return true
}

// Unlock releases the thread lock.
func (c *Thread) Unlock() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.busy = false
}

// MarshalJSON serializes the thread to JSON.
func (c *Thread) MarshalJSON() ([]byte, error) {
	type jsonThread struct {
		ID        string          `json:"id"`
		CreatedAt time.Time       `json:"created_at"`
		UpdatedAt time.Time       `json:"updated_at"`
		Turns     json.RawMessage `json:"turns"`
	}

	turnsJSON, err := marshalTurns(c.State.Turns())
	if err != nil {
		return nil, fmt.Errorf("marshal turns: %w", err)
	}

	jc := jsonThread{
		ID:        c.ID,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
		Turns:     turnsJSON,
	}

	return json.Marshal(jc)
}

// UnmarshalJSON deserializes a thread from JSON.
func (c *Thread) UnmarshalJSON(data []byte) error {
	type jsonThread struct {
		ID        string          `json:"id"`
		CreatedAt time.Time       `json:"created_at"`
		UpdatedAt time.Time       `json:"updated_at"`
		Turns     json.RawMessage `json:"turns"`
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
	c.State = &state.Memory{}
	for _, turn := range turns {
		c.State.Append(turn.Role, turn.Artifacts...)
	}

	return nil
}
