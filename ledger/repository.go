package ledger

import (
	"context"
	"encoding/json"
	"time"
)

// TxType identifies the kind of mutation recorded in a [JournalEntry].
// Four transaction types are supported:
//
//   - TxAddTurn: a new turn is created in the thread.
//   - TxUpdateTip: the active tip pointer is moved.
//   - TxUpdateControl: a turn's [Metadata.Control] directive is changed.
//   - TxUpdateParent: a turn's [Turn.ParentID] link is changed.
//
// All transactions are append-only. Once written to the journal,
// the entry is never modified or reordered.
type TxType string

const (
	TxAddTurn       TxType = "add_turn"
	TxUpdateTip     TxType = "update_tip"
	TxUpdateControl TxType = "update_control"
	TxUpdateParent  TxType = "update_parent"
)

// JournalEntry is the on-disk shape of a single transaction.
//
// Each line in a thread's .jsonl file is one [JournalEntry]. The
// Payload is a type-specific struct encoded as [encoding/json.RawMessage];
// the concrete shape is selected by TxType.
type JournalEntry struct {
	TxType    TxType          `json:"tx_type"`
	Timestamp time.Time       `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

// AddTurnPayload is the payload of a TxAddTurn transaction. It
// captures a single [Turn] and the timestamp the transaction was
// recorded (which may differ from the turn's Timestamp if the clock
// is changed at runtime).
type AddTurnPayload struct {
	Timestamp time.Time `json:"timestamp"`
	Turn      Turn      `json:"turn"`
}

// UpdateTipPayload is the payload of a TxUpdateTip transaction.
type UpdateTipPayload struct {
	Timestamp time.Time `json:"timestamp"`
	CurrentTip string   `json:"current_tip"`
}

// UpdateControlPayload is the payload of a TxUpdateControl transaction.
type UpdateControlPayload struct {
	Timestamp time.Time         `json:"timestamp"`
	TurnID    string            `json:"turn_id"`
	Control   TraversalControl  `json:"control"`
}

// UpdateParentPayload is the payload of a TxUpdateParent transaction.
type UpdateParentPayload struct {
	Timestamp time.Time `json:"timestamp"`
	TurnID    string    `json:"turn_id"`
	ParentID  string    `json:"parent_id"`
}

// Repository persists thread state as an append-only journal of
// [JournalEntry] records. Implementations are single-writer: concurrent
// calls to Save* / Update* from multiple goroutines are not safe.
// The framework's serial pipeline is the contract.
//
// Implementations decide on the physical storage layout (e.g. one file
// per thread, one global file with thread_id in each entry, an
// in-memory store for tests). The interface makes no commitment to
// the wire format; only the journal entry shape is specified.
type Repository interface {
	// SaveTurn appends a TxAddTurn transaction to the thread's journal.
	// The turn's ID must be unique within the thread.
	SaveTurn(ctx context.Context, threadID string, turn *Turn) error

	// UpdateThreadTip appends a TxUpdateTip transaction that sets
	// the thread's CurrentTip to the given turn ID.
	UpdateThreadTip(ctx context.Context, threadID, turnID string) error

	// UpdateTurnControl appends a TxUpdateControl transaction that
	// sets the turn's Metadata.Control to the given value.
	UpdateTurnControl(ctx context.Context, threadID, turnID string, control TraversalControl) error

	// UpdateTurnParent appends a TxUpdateParent transaction that
	// re-parents the turn. The new parent ID is stored verbatim.
	UpdateTurnParent(ctx context.Context, threadID, turnID, parentID string) error

	// HydrateThread reconstructs the thread's in-memory state by
	// replaying its journal from top to bottom. Returns the turns
	// map and the latest current tip pointer.
	//
	// A malformed line (truncated last write, unrecognized payload)
	// is tolerated by stopping replay at that point: the thread's
	// state reflects the last well-formed entry. The error return is
	// reserved for hard failures (file not readable, permission
	// denied, etc.).
	HydrateThread(ctx context.Context, threadID string) (turns map[string]*Turn, currentTip string, err error)
}
