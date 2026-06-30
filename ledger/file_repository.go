package ledger

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// FileRepository is the default [Repository] implementation. Each
// thread is persisted as a single `.jsonl` file in a directory:
// `<dir>/<thread-id>.jsonl`. Each line is one [JournalEntry].
//
// FileRepository uses O_APPEND for atomic appends within a single
// process (POSIX guarantees that small writes to an append-only
// file are atomic on local filesystems). It is single-writer per
// thread: concurrent Save/Update calls from multiple goroutines on
// the same thread are serialized by a per-thread mutex.
//
// FileRepository is not safe across multiple processes. Cross-process
// safety would require file locking (flock), which is out of scope
// for v1.
type FileRepository struct {
	dir string

	// per-thread mutexes, lazily created.
	mu sync.Mutex
	locks map[string]*sync.Mutex
}

// NewFileRepository constructs a FileRepository backed by the given
// directory. The directory is created if it does not exist. Thread
// files are stored as `<dir>/<thread-id>.jsonl`.
func NewFileRepository(dir string) (*FileRepository, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create repository directory %q: %w", dir, err)
	}
	return &FileRepository{
		dir:   dir,
		locks: make(map[string]*sync.Mutex),
	}, nil
}

// lockFor returns the per-thread mutex, creating it if necessary.
func (r *FileRepository) lockFor(threadID string) *sync.Mutex {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.locks[threadID]
	if !ok {
		m = &sync.Mutex{}
		r.locks[threadID] = m
	}
	return m
}

// filePath returns the on-disk path for the thread's journal.
func (r *FileRepository) filePath(threadID string) string {
	return filepath.Join(r.dir, sanitizeThreadID(threadID)+".jsonl")
}

// sanitizeThreadID ensures the thread ID is a safe filename component.
// We strip path separators and replace them with underscores. Empty
// IDs are rejected (callers must use a non-empty identifier).
func sanitizeThreadID(threadID string) string {
	if threadID == "" {
		return "default"
	}
	// Replace any character that isn't a-z, A-Z, 0-9, '-', '_', or '.'
	// with an underscore. This is conservative; it tolerates UUIDs,
	// hashes, and most real-world identifiers.
	out := make([]byte, len(threadID))
	for i := 0; i < len(threadID); i++ {
		c := threadID[i]
		switch {
		case c >= 'a' && c <= 'z',
			c >= 'A' && c <= 'Z',
			c >= '0' && c <= '9',
			c == '-', c == '_', c == '.':
			out[i] = c
		default:
			out[i] = '_'
		}
	}
	return string(out)
}

// appendEntry writes a single JournalEntry as one line of JSONL to
// the thread's file. The file is opened with O_APPEND and O_CREATE.
// The mutex for the thread is held during the write.
func (r *FileRepository) appendEntry(threadID string, entry JournalEntry) error {
	mu := r.lockFor(threadID)
	mu.Lock()
	defer mu.Unlock()

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal journal entry: %w", err)
	}
	data = append(data, '\n')

	path := r.filePath(threadID)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open journal file %q: %w", path, err)
	}
	defer f.Close()

	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write journal entry to %q: %w", path, err)
	}
	return nil
}

// SaveTurn appends a TxAddTurn entry to the thread's journal.
func (r *FileRepository) SaveTurn(_ context.Context, threadID string, turn *Turn) error {
	if turn == nil {
		return fmt.Errorf("SaveTurn: turn is nil")
	}
	ts := now()
	payload, err := json.Marshal(AddTurnPayload{
		Timestamp: ts,
		Turn:      *turn,
	})
	if err != nil {
		return fmt.Errorf("marshal AddTurnPayload: %w", err)
	}
	return r.appendEntry(threadID, JournalEntry{
		TxType:    TxAddTurn,
		Timestamp: ts,
		Payload:   payload,
	})
}

// UpdateThreadTip appends a TxUpdateTip entry.
func (r *FileRepository) UpdateThreadTip(_ context.Context, threadID, turnID string) error {
	ts := now()
	payload, err := json.Marshal(UpdateTipPayload{
		Timestamp:  ts,
		CurrentTip: turnID,
	})
	if err != nil {
		return fmt.Errorf("marshal UpdateTipPayload: %w", err)
	}
	return r.appendEntry(threadID, JournalEntry{
		TxType:    TxUpdateTip,
		Timestamp: ts,
		Payload:   payload,
	})
}

// UpdateTurnControl appends a TxUpdateControl entry.
func (r *FileRepository) UpdateTurnControl(_ context.Context, threadID, turnID string, control TraversalControl) error {
	ts := now()
	payload, err := json.Marshal(UpdateControlPayload{
		Timestamp: ts,
		TurnID:    turnID,
		Control:   control,
	})
	if err != nil {
		return fmt.Errorf("marshal UpdateControlPayload: %w", err)
	}
	return r.appendEntry(threadID, JournalEntry{
		TxType:    TxUpdateControl,
		Timestamp: ts,
		Payload:   payload,
	})
}

// UpdateTurnParent appends a TxUpdateParent entry.
func (r *FileRepository) UpdateTurnParent(_ context.Context, threadID, turnID, parentID string) error {
	ts := now()
	payload, err := json.Marshal(UpdateParentPayload{
		Timestamp: ts,
		TurnID:    turnID,
		ParentID:  parentID,
	})
	if err != nil {
		return fmt.Errorf("marshal UpdateParentPayload: %w", err)
	}
	return r.appendEntry(threadID, JournalEntry{
		TxType:    TxUpdateParent,
		Timestamp: ts,
		Payload:   payload,
	})
}

// HydrateThread reads the thread's journal file line-by-line and
// replays it to reconstruct the in-memory state. A malformed line
// (truncated write, garbage) stops replay; the thread's state
// reflects the last well-formed entry.
//
// If the journal file does not exist, the result is an empty turns
// map and an empty current tip (no error).
func (r *FileRepository) HydrateThread(_ context.Context, threadID string) (map[string]*Turn, string, error) {
	path := r.filePath(threadID)
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return make(map[string]*Turn), "", nil
		}
		return nil, "", fmt.Errorf("open journal file %q: %w", path, err)
	}
	defer f.Close()

	turns := make(map[string]*Turn)
	var currentTip string

	scanner := bufio.NewScanner(f)
	// Allow large journal entries (default 64 KiB is too small for
	// large tool results). 16 MiB is generous.
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		// Skip empty lines (tolerate trailing newline at EOF).
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var entry JournalEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			// Stop replay at the first malformed line; tolerate.
			break
		}
		if err := applyEntry(turns, &currentTip, entry); err != nil {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		if !errors.Is(err, io.EOF) {
			return turns, currentTip, fmt.Errorf("read journal file %q: %w", path, err)
		}
	}
	return turns, currentTip, nil
}