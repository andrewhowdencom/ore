package http

import (
	"context"
	"time"

	"github.com/andrewhowdencom/ore/session"
)

// ThreadSummary is the application-facing shape of a thread entry
// returned by Backend.ListThreads. The HTTP conduit translates this
// to its wire JSON form; the Backend itself does not need to know
// the wire shape.
type ThreadSummary struct {
	// ID is the canonical thread identifier (the ledger key).
	ID string

	// Preview is a short user-message excerpt taken from the first
	// user turn of the thread. Empty when the thread has no user
	// turns yet.
	Preview string

	// LastAt is the timestamp of the most recent turn in the thread.
	// Zero when the thread has no turns.
	LastAt time.Time
}

// Backend is the application-facing capability that the HTTP conduit
// consumes. It is intentionally narrow: the conduit translates HTTP
// requests into one of these operations and formats the response.
// Anything beyond — provider, agent, factory, queue — is the
// application's concern, not the conduit's.
//
// The HTTP conduit does NOT import engine, agent, or ledger directly.
// Each method maps to a single, well-defined contract:
//
//   - CreateSession makes a new session (or attaches to an existing
//     thread when threadID is non-empty) and returns it.
//   - GetSession looks up an active session by ID.
//   - Submit enqueues an event against a session. The conduit does
//     not block on inference; the application enforces its own
//     admission policy (e.g. the engine's bounded queue).
//   - ListThreads enumerates persisted thread IDs.
//   - DeleteSession removes a session from the active registry. The
//     underlying thread is not deleted from durable storage.
//
// All operations are safe for concurrent use across goroutines.
type Backend interface {
	// CreateSession creates a new session. When threadID is non-empty,
	// the session attaches to that existing thread (hydrating from
	// durable storage when necessary); when threadID is empty, a
	// fresh ephemeral session is created. The returned session is
	// registered for active use.
	//
	// Implementations return a non-nil error when the thread does
	// not exist (for attach) or any persistence operation fails.
	CreateSession(ctx context.Context, threadID string) (*session.Session, error)

	// GetSession looks up an active session by ID. Returns an error
	// when no session is registered under the given ID.
	GetSession(ctx context.Context, id string) (*session.Session, error)

	// Submit enqueues an event for the given session. The session
	// must be registered (i.e. reachable via GetSession). The
	// application is responsible for whatever admission policy
	// governs event acceptance (e.g. the engine's bounded queue).
	Submit(ctx context.Context, id string, event session.Event) error

	// ListThreads enumerates every persisted thread. The empty
	// result is returned (not an error) when the application's
	// durable store is empty.
	ListThreads(ctx context.Context) ([]ThreadSummary, error)

	// DeleteSession removes the session from the active registry
	// and closes its underlying resources. The thread itself is
	// NOT deleted from durable storage; it can be re-attached
	// later via CreateSession with a non-empty threadID.
	DeleteSession(ctx context.Context, id string) error
}
