package session

// Store abstracts persistence for Thread instances.
// Implementations must be safe for concurrent use.
type Store interface {
	// Create generates a new Thread with a random UUID and stores it.
	Create() (*Thread, error)
	// Get retrieves a Thread by ID. The second return value is false
	// if the thread does not exist.
	Get(id string) (*Thread, bool)
	// GetBy retrieves a Thread by a metadata key-value pair.
	// The second return value is false if no thread matches.
	GetBy(key, value string) (*Thread, bool)
	// Save persists the given Thread, updating its UpdatedAt timestamp.
	Save(thread *Thread) error
	// Delete removes a Thread by ID and returns true if it existed.
	Delete(id string) bool
	// List returns all stored Threads.
	List() ([]*Thread, error)
}
