package session

// Store abstracts persistence for Thread instances.
// Implementations must be safe for concurrent use.
//
// Get and GetBy return errors so callers can distinguish a missing
// thread from a corrupt one. The previous (Thread, bool) signature
// conflated the two cases, which made any parse failure
// indistinguishable from "no such thread" and prevented useful
// diagnostics. Implementations return ErrThreadNotFound for the
// former and wrap their cause in ErrThreadCorrupt for the latter.
// See issue #453.
type Store interface {
	// Create generates a new Thread with a random UUID and stores it.
	Create() (*Thread, error)
	// Get retrieves a Thread by ID. Returns ErrThreadNotFound if no
	// such thread exists, or an error wrapping ErrThreadCorrupt if
	// the thread file is present but unparseable.
	Get(id string) (*Thread, error)
	// GetBy retrieves a Thread by a metadata key-value pair.
	// Returns ErrThreadNotFound if no thread matches, or an error
	// wrapping ErrThreadCorrupt if any thread file encountered during
	// the scan is unparseable.
	GetBy(key, value string) (*Thread, error)
	// Save persists the given Thread, updating its UpdatedAt timestamp.
	Save(thread *Thread) error
	// Delete removes a Thread by ID and returns true if it existed.
	Delete(id string) bool
	// List returns all stored Threads.
	List() ([]*Thread, error)
}
