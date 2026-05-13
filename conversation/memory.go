package conversation

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/andrewhowdencom/ore/state"
)

// MemoryStore is an in-memory Store implementation.
type MemoryStore struct {
	conversations map[string]*Conversation
	mu            sync.RWMutex
}

// NewMemoryStore creates a new empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		conversations: make(map[string]*Conversation),
	}
}

// Create generates a new Conversation with a random ID and stores it.
func (s *MemoryStore) Create() (*Conversation, error) {
	id, err := generateID()
	if err != nil {
		return nil, fmt.Errorf("generate conversation id: %w", err)
	}

	conv := &Conversation{
		ID:        id,
		State:     &state.Memory{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.conversations[id] = conv
	return conv, nil
}

// Get retrieves a conversation by ID.
func (s *MemoryStore) Get(id string) (*Conversation, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	conv, ok := s.conversations[id]
	return conv, ok
}

// Save updates the conversation's UpdatedAt and stores it.
func (s *MemoryStore) Save(conv *Conversation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	conv.UpdatedAt = time.Now()
	s.conversations[conv.ID] = conv
	return nil
}

// Delete removes a conversation from the store.
func (s *MemoryStore) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.conversations[id]
	delete(s.conversations, id)
	return ok
}

// generateID creates a random 32-character hex string (128 bits).
func generateID() (string, error) {
	b := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
