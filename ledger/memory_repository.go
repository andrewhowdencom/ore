package ledger

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// now is the package-level clock source. It is a var (not const) so
// tests can substitute a deterministic clock; production code uses
// [time.Now] by default.
var now = time.Now

// MemoryRepository is an in-memory implementation of [Repository].
// It holds each thread's journal as a slice of [JournalEntry] in
// append order and replays the slice to reconstruct state.
//
// MemoryRepository is safe for concurrent use across goroutines; the
// underlying append is guarded by a per-thread mutex. This is the only
// Repository implementation that supports concurrent writers.
//
// MemoryRepository is intended primarily for tests and ephemeral
// use; production code should use [FileRepository].
type MemoryRepository struct {
	mu      sync.Mutex
	journals map[string][]JournalEntry
}

// NewMemoryRepository constructs an empty MemoryRepository.
func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		journals: make(map[string][]JournalEntry),
	}
}

// SaveTurn appends a TxAddTurn entry to the thread's journal.
func (r *MemoryRepository) SaveTurn(_ context.Context, threadID string, turn *Turn) error {
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
	entry := JournalEntry{
		TxType:    TxAddTurn,
		Timestamp: ts,
		Payload:   payload,
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.journals[threadID] = append(r.journals[threadID], entry)
	return nil
}

// UpdateThreadTip appends a TxUpdateTip entry to the thread's journal.
func (r *MemoryRepository) UpdateThreadTip(_ context.Context, threadID, turnID string) error {
	ts := now()
	payload, err := json.Marshal(UpdateTipPayload{
		Timestamp:  ts,
		CurrentTip: turnID,
	})
	if err != nil {
		return fmt.Errorf("marshal UpdateTipPayload: %w", err)
	}
	entry := JournalEntry{
		TxType:    TxUpdateTip,
		Timestamp: ts,
		Payload:   payload,
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.journals[threadID] = append(r.journals[threadID], entry)
	return nil
}

// UpdateTurnControl appends a TxUpdateControl entry to the thread's
// journal.
func (r *MemoryRepository) UpdateTurnControl(_ context.Context, threadID, turnID string, control TraversalControl) error {
	ts := now()
	payload, err := json.Marshal(UpdateControlPayload{
		Timestamp: ts,
		TurnID:    turnID,
		Control:   control,
	})
	if err != nil {
		return fmt.Errorf("marshal UpdateControlPayload: %w", err)
	}
	entry := JournalEntry{
		TxType:    TxUpdateControl,
		Timestamp: ts,
		Payload:   payload,
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.journals[threadID] = append(r.journals[threadID], entry)
	return nil
}

// UpdateTurnParent appends a TxUpdateParent entry to the thread's
// journal.
func (r *MemoryRepository) UpdateTurnParent(_ context.Context, threadID, turnID, parentID string) error {
	ts := now()
	payload, err := json.Marshal(UpdateParentPayload{
		Timestamp: ts,
		TurnID:    turnID,
		ParentID:  parentID,
	})
	if err != nil {
		return fmt.Errorf("marshal UpdateParentPayload: %w", err)
	}
	entry := JournalEntry{
		TxType:    TxUpdateParent,
		Timestamp: ts,
		Payload:   payload,
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.journals[threadID] = append(r.journals[threadID], entry)
	return nil
}

// HydrateThread replays the thread's journal from top to bottom to
// reconstruct its state. A malformed entry stops the replay; the
// thread's state reflects the last well-formed entry.
//
// If the thread has no journal entries, the result is an empty
// turns map and an empty current tip.
func (r *MemoryRepository) HydrateThread(_ context.Context, threadID string) (map[string]*Turn, string, error) {
	r.mu.Lock()
	entries := r.journals[threadID]
	r.mu.Unlock()

	// Copy the entries so we can release the lock while replaying.
	entriesCopy := make([]JournalEntry, len(entries))
	copy(entriesCopy, entries)

	turns := make(map[string]*Turn)
	var currentTip string

	for _, entry := range entriesCopy {
		if err := applyEntry(turns, &currentTip, entry); err != nil {
			// Tolerate malformed lines: stop replay at the first error.
			// The thread's state reflects the last well-formed entry.
			return turns, currentTip, nil
		}
	}

	return turns, currentTip, nil
}

// applyEntry applies a single journal entry to the in-memory state.
// On any error, the caller stops replaying.
func applyEntry(turns map[string]*Turn, currentTip *string, entry JournalEntry) error {
	switch entry.TxType {
	case TxAddTurn:
		var p AddTurnPayload
		if err := json.Unmarshal(entry.Payload, &p); err != nil {
			return fmt.Errorf("unmarshal AddTurnPayload: %w", err)
		}
		// Last-write-wins for the turn (idempotent on rehydrate).
		turn := p.Turn
		turns[turn.ID] = &turn

	case TxUpdateTip:
		var p UpdateTipPayload
		if err := json.Unmarshal(entry.Payload, &p); err != nil {
			return fmt.Errorf("unmarshal UpdateTipPayload: %w", err)
		}
		*currentTip = p.CurrentTip

	case TxUpdateControl:
		var p UpdateControlPayload
		if err := json.Unmarshal(entry.Payload, &p); err != nil {
			return fmt.Errorf("unmarshal UpdateControlPayload: %w", err)
		}
		if turn, ok := turns[p.TurnID]; ok {
			turn.Metadata.Control = p.Control
		}
		// If the turn is not in the map, this is a no-op (the turn
		// may not have been persisted yet, or the entry references a
		// turn that was deleted — either way, the result is
		// consistent).

	case TxUpdateParent:
		var p UpdateParentPayload
		if err := json.Unmarshal(entry.Payload, &p); err != nil {
			return fmt.Errorf("unmarshal UpdateParentPayload: %w", err)
		}
		if turn, ok := turns[p.TurnID]; ok {
			turn.ParentID = p.ParentID
		}

	default:
		// Unknown transaction type: stop replay (forward compatibility).
		return fmt.Errorf("unknown tx_type %q", entry.TxType)
	}

	return nil
}

// Journal returns a copy of the journal entries for the given thread.
// Useful for tests; not part of the Repository interface.
func (r *MemoryRepository) Journal(threadID string) []JournalEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	src := r.journals[threadID]
	out := make([]JournalEntry, len(src))
	copy(out, src)
	return out
}
