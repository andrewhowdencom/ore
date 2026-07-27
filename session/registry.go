package session

import (
	"errors"
	"sync"
)

// ErrSessionNotFound is returned by Registry.Get and Registry.Remove
// when no session matches the requested identifier. Callers can
// detect this case with errors.Is.
var ErrSessionNotFound = errors.New("session not found")

// ErrSessionAlreadyExists is returned by Registry.Register when a
// session with the same ID is already registered. The existing
// session is left untouched.
var ErrSessionAlreadyExists = errors.New("session already exists")

// Registry indexes active *Session values by their ID. It is the
// process-wide lookup that the engine and conduits share.
//
// Registry is intentionally minimal: the engine consumes it for
// per-session submission resolution, and applications compose it with
// ledger persistence to hydrate sessions on attach. Conduits
// historically read sessions through a Runner, but in the new
// architecture the registry is the single source of truth for active
// sessions.
//
// Implementations must be safe for concurrent use across goroutines.
// The InMemoryRegistry provided in this package satisfies that
// contract; alternative implementations (e.g. distributed) may
// impose different consistency guarantees.
type Registry interface {
	// Register adds the session to the registry. If a session with
	// the same ID is already registered, ErrSessionAlreadyExists is
	// returned and the existing session is left untouched.
	Register(*Session) error

	// Get retrieves a session by ID. Returns ErrSessionNotFound
	// wrapped with the ID when no session matches.
	Get(id string) (*Session, error)

	// Remove removes the session from the registry and returns it.
	// Returns ErrSessionNotFound when no session matches.
	//
	// Removal does not Close the session; the caller is responsible
	// for any resource cleanup the session holds. The registry
	// itself never invokes Close, because the engine may want to
	// deregister a session while preserving its in-memory state
	// (e.g. for a subsequent reattach).
	Remove(id string) (*Session, error)
}

// InMemoryRegistry is the reference Registry implementation. It holds
// active *Session values in a map keyed by session ID, guarded by a
// sync.RWMutex so concurrent readers do not serialise on each other.
type InMemoryRegistry struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

// NewInMemoryRegistry constructs an empty InMemoryRegistry.
func NewInMemoryRegistry() *InMemoryRegistry {
	return &InMemoryRegistry{
		sessions: make(map[string]*Session),
	}
}

// Register implements Registry.
func (r *InMemoryRegistry) Register(s *Session) error {
	if s == nil {
		return errors.New("session.InMemoryRegistry.Register: nil session")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.sessions[s.ID()]; ok {
		return errors.Join(ErrSessionAlreadyExists, errors.New(": "+s.ID()))
	}
	r.sessions[s.ID()] = s
	return nil
}

// Get implements Registry.
func (r *InMemoryRegistry) Get(id string) (*Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sessions[id]
	if !ok {
		return nil, errors.Join(ErrSessionNotFound, errors.New(": "+id))
	}
	return s, nil
}

// Remove implements Registry.
func (r *InMemoryRegistry) Remove(id string) (*Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[id]
	if !ok {
		return nil, errors.Join(ErrSessionNotFound, errors.New(": "+id))
	}
	delete(r.sessions, id)
	return s, nil
}

// Compile-time assertion that *InMemoryRegistry satisfies Registry.
var _ Registry = (*InMemoryRegistry)(nil)