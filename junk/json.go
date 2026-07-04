package junk

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/andrewhowdencom/ore/ledger"
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
		ID:       id,
		State:    ledger.NewThread(),
		Metadata: make(map[string]string),
	}

	if err := s.Save(thread); err != nil {
		return nil, fmt.Errorf("save new thread: %w", err)
	}

	return thread, nil
}

// Get retrieves a thread by ID.
//
// Return values:
//
//   - (thread, nil): thread found and loaded successfully.
//   - (nil, ErrThreadNotFound): no thread with that ID exists.
//   - (nil, ErrThreadCorrupt wrapping cause): a thread file exists at
//     the expected path but cannot be parsed. The wrapped error is
//     the underlying unmarshal failure so callers can introspect it.
//
// The previous (thread, bool) signature silently returned (nil, false)
// for both "not found" and "corrupt", which made any parse failure
// indistinguishable from a missing file. Callers could not log or
// report the real cause. See issue #453.
func (s *JSONStore) Get(id string) (*Thread, error) {
	s.mu.RLock()
	thread, ok := s.cache[id]
	s.mu.RUnlock()

	if ok {
		return thread, nil
	}

	// Attempt to load from disk.
	path := filepath.Join(s.dir, id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrThreadNotFound
		}
		// Multi-%w so errors.Is(err, ErrThreadCorrupt) succeeds
		// and the underlying read error is also reachable.
		return nil, fmt.Errorf("read thread file %q: %w: %w", path, ErrThreadCorrupt, err)
	}

	thread = &Thread{}
	if err := json.Unmarshal(data, thread); err != nil {
		return nil, fmt.Errorf("unmarshal thread %q: %w: %w", id, ErrThreadCorrupt, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// Another goroutine may have loaded it while we were reading.
	if existing, ok := s.cache[id]; ok {
		return existing, nil
	}
	s.cache[id] = thread
	return thread, nil
}

// GetBy retrieves a thread by a metadata key-value pair.
// It scans all thread files on disk and returns the first match.
//
// If the scan encounters a thread file that exists but cannot be
// parsed, the parse error is returned wrapped in ErrThreadCorrupt.
// A missing thread file is silently skipped (matching the previous
// behavior; a single missing file in the scan is not the same as
// "the requested thread does not exist"). If the scan completes
// with no match, ErrThreadNotFound is returned.
func (s *JSONStore) GetBy(key, value string) (*Thread, error) {
	ids, err := s.listThreadIDs()
	if err != nil {
		return nil, fmt.Errorf("list thread ids: %w", err)
	}

	for _, id := range ids {
		thread, err := s.Get(id)
		if err != nil {
			if errors.Is(err, ErrThreadNotFound) {
				// Should not happen — listThreadIDs only returns
				// extant ids. Skip defensively.
				continue
			}
			if errors.Is(err, ErrThreadCorrupt) {
				// Surface the first corrupt file encountered.
				return nil, err
			}
			return nil, err
		}
		match := thread.Metadata[key] == value
		if match {
			return thread, nil
		}
	}
	return nil, ErrThreadNotFound
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
//
// Corrupt files (files that exist but cannot be parsed) are silently
// skipped, matching the long-standing behavior of this method. Direct
// lookups via Get surface corruption as ErrThreadCorrupt; bulk
// lookups via List intentionally do not, because a single corrupt
// file in the directory shouldn't prevent the caller from seeing the
// rest of the threads. Callers that need to find every corrupt file
// must walk the directory themselves.
func (s *JSONStore) List() ([]*Thread, error) {
	ids, err := s.listThreadIDs()
	if err != nil {
		return nil, err
	}

	result := make([]*Thread, 0, len(ids))
	for _, id := range ids {
		thread, err := s.Get(id)
		if err != nil {
			// Skip files that have been removed between listing
			// and reading, and skip corrupt files (see doc comment).
			continue
		}
		result = append(result, thread)
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
