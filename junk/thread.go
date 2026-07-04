package junk

import (
	"encoding/json"
	"fmt"

	"github.com/andrewhowdencom/ore/ledger"
)

// Thread represents a persistent thread with identity, state, and
// metadata.
//
// State is the tree-backed [ledger.Thread] that owns the turn
// history. The Metadata field is conduit-specific (e.g., mapping
// to external system identifiers) and is independent of the
// per-conversation ledger.Meta().
//
// ID is the address by which the Store locates this thread (e.g.,
// the on-disk filename). It is assigned by the Store at creation
// time and is not stored on the inner ledger.Thread (per the
// "ID is external" principle). CreatedAt and UpdatedAt are no longer
// persisted; turn timestamps are the conversation's temporal data.
type Thread struct {
	// ID is the unique identifier for this thread (random UUID).
	ID string
	// State holds the mutable thread turn history.
	State *ledger.Thread
	// Metadata holds arbitrary key-value pairs for conduit-specific
	// thread mapping (e.g., external system identifiers).
	Metadata map[string]string
}

// threadJSON is the on-disk wire format for a Thread. The shape is
// intentionally similar to the previous Buffer-based format but
// carries tree mechanics: every turn has ID and ParentID, and the
// thread has a CurrentTip pointer that selects the active branch.
//
// CreatedAt and UpdatedAt have been removed; conversations are
// temporal via their turn history. The ID is preserved because the
// Store uses it as the addressing key (e.g., filename on disk).
type threadJSON struct {
	ID         string            `json:"id"`
	CurrentTip string            `json:"current_tip,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Turns      []ledger.Turn     `json:"turns"`
}

// MarshalJSON serializes the thread to JSON. The format is the
// threadJSON shape above.
func (c *Thread) MarshalJSON() ([]byte, error) {
	if c.State == nil {
		return nil, fmt.Errorf("junk.Thread.MarshalJSON: State is nil")
	}

	// Serialize every turn in the tree (not just the active path) so
	// that alternate branches are preserved. The active path is
	// reconstructed on hydrate via Thread.Turns().
	allTurns := c.State.AllTurns()

	jc := threadJSON{
		ID:         c.ID,
		CurrentTip: c.State.CurrentTip,
		Metadata:   c.Metadata,
		Turns:      allTurns,
	}

	return json.Marshal(jc)
}

// UnmarshalJSON deserializes the thread from JSON. The State is
// reconstructed as a fresh [ledger.Thread] with all turns populated
// and the CurrentTip set. CreatedAt/UpdatedAt are not stored in
// the new format and are left at their zero values.
func (c *Thread) UnmarshalJSON(data []byte) error {
	var jc threadJSON
	if err := json.Unmarshal(data, &jc); err != nil {
		return fmt.Errorf("unmarshal thread: %w", err)
	}

	c.ID = jc.ID
	if jc.Metadata != nil {
		c.Metadata = jc.Metadata
	} else {
		c.Metadata = make(map[string]string)
	}

	c.State = ledger.NewThread()
	for i := range jc.Turns {
		turn := jc.Turns[i]
		c.State.SaveTurn(&turn)
	}
	c.State.SetCurrentTip(jc.CurrentTip)

	return nil
}

// GetMetadata retrieves a metadata value from the thread.
func (c *Thread) GetMetadata(key string) (string, bool) {
	v, ok := c.Metadata[key]
	return v, ok
}

// SetMetadata sets a metadata value on the thread.
func (c *Thread) SetMetadata(key, value string) {
	c.Metadata[key] = value
}