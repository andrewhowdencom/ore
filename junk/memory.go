package junk

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"sync"

	"github.com/andrewhowdencom/ore/ledger"
)

// MemoryStore is an in-memory Store implementation.
type MemoryStore struct {
	threads map[string]*Thread
	mu      sync.RWMutex
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
		ID:       id,
		State:    ledger.NewThread(),
		Metadata: make(map[string]string),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.threads[id] = thread
	return thread, nil
}

// Get retrieves a thread by ID.
//
// Returns ErrThreadNotFound if no thread with the given ID is stored.
// The previous (Thread, bool) signature has been replaced so callers
// can distinguish a miss from a corruption; see issue #453.
func (s *MemoryStore) Get(id string) (*Thread, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	thread, ok := s.threads[id]
	if !ok {
		return nil, ErrThreadNotFound
	}
	return thread, nil
}

// GetBy retrieves a thread by a metadata key-value pair.
// It performs a linear scan over all threads and returns the first match.
//
// Returns ErrThreadNotFound if no thread matches the key-value pair.
// In-memory corruption is not possible (no disk reads), so this
// implementation never returns ErrThreadCorrupt.
func (s *MemoryStore) GetBy(key, value string) (*Thread, error) {
	s.mu.RLock()
	candidates := make([]*Thread, 0, len(s.threads))
	for _, thread := range s.threads {
		candidates = append(candidates, thread)
	}
	s.mu.RUnlock()

	for _, thread := range candidates {
		match := thread.Metadata[key] == value
		if match {
			return thread, nil
		}
	}
	return nil, ErrThreadNotFound
}

// Save updates the thread's UpdatedAt and stores it.
func (s *MemoryStore) Save(thread *Thread) error {
	s.mu.Lock()
	defer s.mu.Unlock()
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
