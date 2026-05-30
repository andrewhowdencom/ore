package session

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

// JSONStore persists threads as individual JSON files in a directory.
type JSONStore struct {
	dir   string
	mu    sync.RWMutex
	cache map[string]*Thread
}

// NewJSONStore creates a new JSONStore backed by the given directory.
// The directory is created if it does not exist. Thread data is loaded
// lazily on first access via Get, List, or GetBy.
//
// Malformed or unreadable .json files are silently skipped during access.
func NewJSONStore(dir string) (*JSONStore, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create store directory: %w", err)
	}

	return &JSONStore{
		dir:   dir,
		cache: make(map[string]*Thread),
	}, nil
}

// Create generates a new Thread with a random ID and persists it.
func (s *JSONStore) Create() (*Thread, error) {
	id, err := generateID()
	if err != nil {
		return nil, fmt.Errorf("generate thread id: %w", err)
	}

	thread := &Thread{
		ID:        id,
		State:     &state.Buffer{},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Metadata:  make(map[string]string),
	}

	if err := s.Save(thread); err != nil {
		return nil, fmt.Errorf("save new thread: %w", err)
	}

	return thread, nil
}

// Get retrieves a thread by ID. If not in cache, it attempts to
// load from disk.
func (s *JSONStore) Get(id string) (*Thread, bool) {
	s.mu.RLock()
	thread, ok := s.cache[id]
	s.mu.RUnlock()

	if ok {
		return thread, true
	}

	// Attempt to load from disk.
	path := filepath.Join(s.dir, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}

	thread = &Thread{}
	if err := json.Unmarshal(data, thread); err != nil {
		return nil, false
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// Another goroutine may have loaded it while we were reading.
	if existing, ok := s.cache[id]; ok {
		return existing, true
	}
	s.cache[id] = thread
	return thread, true
}

// GetBy retrieves a thread by a metadata key-value pair.
// It scans all thread files on disk and returns the first match.
func (s *JSONStore) GetBy(key, value string) (*Thread, bool) {
	ids, err := s.listThreadIDs()
	if err != nil {
		return nil, false
	}

	for _, id := range ids {
		thread, ok := s.Get(id)
		if !ok {
			continue
		}
		thread.metaMu.RLock()
		match := thread.Metadata[key] == value
		thread.metaMu.RUnlock()
		if match {
			return thread, true
		}
	}
	return nil, false
}

// Save writes the thread to disk atomically (via a temporary file
// and os.Rename) and updates the in-memory cache. The thread's
// UpdatedAt timestamp is also advanced.
func (s *JSONStore) Save(thread *Thread) error {
	data, err := json.Marshal(thread)
	if err != nil {
		return fmt.Errorf("marshal thread: %w", err)
	}

	tmpPath := filepath.Join(s.dir, thread.ID+".tmp")
	finalPath := filepath.Join(s.dir, thread.ID+".json")

	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}

	if err := os.Rename(tmpPath, finalPath); err != nil {
		return fmt.Errorf("rename temp file: %w", err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	thread.UpdatedAt = time.Now()
	s.cache[thread.ID] = thread
	return nil
}

// Delete removes a thread from the cache and deletes its file.
func (s *JSONStore) Delete(id string) bool {
	path := filepath.Join(s.dir, id+".json")

	s.mu.Lock()
	defer s.mu.Unlock()

	_, ok := s.cache[id]
	delete(s.cache, id)

	_ = os.Remove(path)

	return ok
}

// List returns all threads in the store.
func (s *JSONStore) List() ([]*Thread, error) {
	ids, err := s.listThreadIDs()
	if err != nil {
		return nil, err
	}

	result := make([]*Thread, 0, len(ids))
	for _, id := range ids {
		if thread, ok := s.Get(id); ok {
			result = append(result, thread)
		}
	}
	return result, nil
}

// listThreadIDs returns the IDs of all .json files in the store directory.
func (s *JSONStore) listThreadIDs() ([]string, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}

	var ids []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		id := strings.TrimSuffix(name, ".json")
		ids = append(ids, id)
	}
	return ids, nil
}
