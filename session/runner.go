package session

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/andrewhowdencom/ore/loop"
)

// ErrSessionNotFound is the sentinel returned by Runner.Get when no
// session matches the requested identifier. Callers can detect this
// case with errors.Is.
var ErrSessionNotFound = errors.New("session not found")

// Runner is the process-wide wire that drives inference against
// Sessions. It owns:
//
//   - the AgentFactory that builds per-pattern agents from session
//     metadata
//   - a chain of Interceptors (slash commands, etc.) that run before
//     the agent
//   - a SinkRouter that forwards session events to external
//     subscribers (conduits)
//   - a registry of active sessions indexed by ID
//
// Conduits interact with the Runner, not the Session, for
// inference-driving operations.
type Runner struct {
	factory      AgentFactory
	interceptors []Interceptor
	sinks        *SinkRouter
	sessions     map[string]*Session
	mu           sync.RWMutex
}

// RunnerOption configures a Runner.
type RunnerOption func(*Runner)

// WithFactory sets the AgentFactory used to build per-pattern agents.
// Required; NewRunner returns a Runner that panics if no factory is
// configured before the first Run.
func WithFactory(f AgentFactory) RunnerOption {
	return func(r *Runner) { r.factory = f }
}

// WithInterceptor appends an Interceptor to the Runner's chain. The
// Runner invokes interceptors in registration order; the first to
// return a non-nil InterceptResult.Event replaces the original event
// for downstream processing.
func WithInterceptor(i Interceptor) RunnerOption {
	return func(r *Runner) { r.interceptors = append(r.interceptors, i) }
}

// WithSink registers a sink on the Runner's SinkRouter. The
// returned function unregisters the sink when called.
func (r *Runner) WithSink(kinds []string, fn SinkFunc) func() {
	return r.sinks.Add(kinds, fn)
}

// NewRunner constructs a Runner with the given options. The Runner's
// internal SinkRouter is always present; all other fields default to
// nil until configured.
func NewRunner(opts ...RunnerOption) *Runner {
	r := &Runner{
		sinks:    newSinkRouter(),
		sessions: make(map[string]*Session),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Create creates a new session with the given ID. The session is
// registered and returned. If a session with the same ID already
// exists, ErrSessionAlreadyExists is returned (the existing session
// is left untouched).
var ErrSessionAlreadyExists = errors.New("session already exists")

func (r *Runner) Create(ctx context.Context, id string, sess *Session) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.sessions[id]; ok {
		return fmt.Errorf("%w: %s", ErrSessionAlreadyExists, id)
	}
	r.sessions[id] = sess
	return nil
}

// Get retrieves an active session by ID.
func (r *Runner) Get(id string) (*Session, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sessions[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrSessionNotFound, id)
	}
	return s, nil
}

// Register inserts a session into the registry without checking for
// duplicates. It is the caller's responsibility to ensure uniqueness.
func (r *Runner) Register(sess *Session) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[sess.ID()] = sess
}

// Close closes all sessions and releases resources. The Runner is
// unusable after Close.
func (r *Runner) Close() error {
	r.mu.Lock()
	sessions := make([]*Session, 0, len(r.sessions))
	for _, s := range r.sessions {
		sessions = append(sessions, s)
	}
	r.sessions = nil
	r.mu.Unlock()
	for _, s := range sessions {
		_ = s.Close()
	}
	return nil
}

// Run is the canonical entry point for inference-driving events. It
// invokes the registered interceptors in order, builds an agent
// from the session's metadata via the configured AgentFactory, runs
// the agent, and broadcasts emitted events through the SinkRouter.
//
// Run is synchronous from the caller's perspective: it returns once
// the agent's Run has returned (or the event was consumed by an
// interceptor). Events are processed in the caller's goroutine; the
// session does not queue or buffer them. Callers that need async
// submission own the goroutine and channel that calls Run.
func (r *Runner) Run(ctx context.Context, sess *Session, evt Event) error {
	if r.factory == nil {
		return fmt.Errorf("session.Runner: no AgentFactory configured")
	}
	if sess == nil {
		return fmt.Errorf("session.Runner: nil session")
	}
	if evt == nil {
		return fmt.Errorf("session.Runner: nil event")
	}

	current := evt
	for _, intr := range r.interceptors {
		result, err := intr.Intercept(ctx, current, sess, sess.Emitter())
		if err != nil {
			return fmt.Errorf("interceptor: %w", err)
		}
		for _, n := range result.Notice {
			r.sinks.Deliver(sess.ID(), loop.NoticeEvent{Notice: n, Ctx: current.Context()})
		}
		if result.Event == nil {
			return nil
		}
		current = result.Event
	}

	ag, err := r.factory.Build(sess)
	if err != nil {
		return fmt.Errorf("agent factory: %w", err)
	}

	if _, err := ag.Run(ctx, sess.Thread()); err != nil {
		return fmt.Errorf("agent run: %w", err)
	}

	return nil
}