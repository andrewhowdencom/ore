package thread

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
	threads map[string]*Thread
	mu            sync.RWMutex
}

// NewMemoryStore creates a new empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		threads: make(map[string]*Thread),
	}
}

// Create generates a new Thread with a random ID and stores it.
func (s *MemoryStore) Create() (*Thread, error) {
	id, err := generateID()
	if err != nil {
		return nil, fmt.Errorf("generate thread id: %w", err)
	}

	thread := &Thread{
		ID:        id,
		State:     &state.Memory{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.threads[id] = thread
	return thread, nil
}

// Get retrieves a thread by ID.
func (s *MemoryStore) Get(id string) (*Thread, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	thread, ok := s.threads[id]
	return thread, ok
}

// Save updates the thread's UpdatedAt and stores it.
func (s *MemoryStore) Save(thread *Thread) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	thread.UpdatedAt = time.Now()
	s.threads[thread.ID] = thread
	return nil
}

// Delete removes a thread from the store.
func (s *MemoryStore) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.threads[id]
	delete(s.threads, id)
	return ok
}

// List returns all threads in the store.
func (s *MemoryStore) List() ([]*Thread, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*Thread, 0, len(s.threads))
	for _, thread := range s.threads {
		result = append(result, thread)
	}
	return result, nil
}

// generateID creates a random 32-character hex string (128 bits).
func generateID() (string, error) {
	b := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
