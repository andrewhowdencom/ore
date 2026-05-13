package conversation

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/andrewhowdencom/ore/state"
)

// JSONStore persists conversations as individual JSON files in a directory.
type JSONStore struct {
	dir   string
	mu    sync.RWMutex
	cache map[string]*Conversation
}

// NewJSONStore creates a new JSONStore backed by the given directory.
// The directory is created if it does not exist. Existing conversations
// are loaded from disk into the in-memory cache.
func NewJSONStore(dir string) (*JSONStore, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create store directory: %w", err)
	}

	cache := make(map[string]*Conversation)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read store directory: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")

		path := filepath.Join(dir, name)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}

		conv := &Conversation{}
		if err := json.Unmarshal(data, conv); err != nil {
			continue
		}
		cache[id] = conv
	}

	return &JSONStore{
		dir:   dir,
		cache: cache,
	}, nil
}

// Create generates a new Conversation with a random ID and persists it.
func (s *JSONStore) Create() (*Conversation, error) {
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

	if err := s.Save(conv); err != nil {
		return nil, fmt.Errorf("save new conversation: %w", err)
	}

	return conv, nil
}

// Get retrieves a conversation by ID. If not in cache, it attempts to
// load from disk.
func (s *JSONStore) Get(id string) (*Conversation, bool) {
	s.mu.RLock()
	conv, ok := s.cache[id]
	s.mu.RUnlock()

	if ok {
		return conv, true
	}

	// Attempt to load from disk.
	path := filepath.Join(s.dir, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}

	conv = &Conversation{}
	if err := json.Unmarshal(data, conv); err != nil {
		return nil, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// Another goroutine may have loaded it while we were reading.
	if existing, ok := s.cache[id]; ok {
		return existing, true
	}
	s.cache[id] = conv
	return conv, true
}

// Save writes the conversation to disk atomically and updates the cache.
func (s *JSONStore) Save(conv *Conversation) error {
	data, err := json.Marshal(conv)
	if err != nil {
		return fmt.Errorf("marshal conversation: %w", err)
	}

	tmpPath := filepath.Join(s.dir, conv.ID+".tmp")
	finalPath := filepath.Join(s.dir, conv.ID+".json")

	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	conv.UpdatedAt = time.Now()
	s.cache[conv.ID] = conv
	return nil
}

// Delete removes a conversation from the cache and deletes its file.
func (s *JSONStore) Delete(id string) bool {
	path := filepath.Join(s.dir, id+".json")

	s.mu.Lock()
	defer s.mu.Unlock()

	_, ok := s.cache[id]
	delete(s.cache, id)

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		// File removal error is non-fatal; the conversation is already
		// removed from the cache.
	}

	return ok
}

// List returns all conversations in the store.
func (s *JSONStore) List() ([]*Conversation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]*Conversation, 0, len(s.cache))
	for _, conv := range s.cache {
		result = append(result, conv)
	}
	return result, nil
}
