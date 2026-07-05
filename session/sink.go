package session

import (
	"log/slog"
	"sync"

	"github.com/andrewhowdencom/ore/loop"
)

// SinkFunc receives output events from sessions, together with the
// originating session ID. It may be invoked concurrently for multiple
// sessions. Implementations should be non-blocking or short-lived.
type SinkFunc func(sessionID string, event loop.OutputEvent)

type sink struct {
	id    int64
	kinds map[string]struct{}
	fn    SinkFunc
}

func makeKindsSet(kinds []string) map[string]struct{} {
	if len(kinds) == 0 {
		return nil
	}
	out := make(map[string]struct{}, len(kinds))
	for _, k := range kinds {
		out[k] = struct{}{}
	}
	return out
}

// SinkRouter is a process-wide registry of external subscribers
// (typically conduits) that want to receive session events. It is
// concurrency-safe; subscribers may be added or removed at any time.
//
// Each registered sink receives every event from a session whose
// Session.Subscribe channel yields the event, filtered by the kinds
// the sink registered for. An empty kinds list means all events.
//
// Sinks are invoked on the session's emit goroutine; implementations
// must be non-blocking or short-lived. A panicking sink is recovered
// and logged.
type SinkRouter struct {
	mu      sync.RWMutex
	sinks   []sink
	nextID  int64
}

func newSinkRouter() *SinkRouter {
	return &SinkRouter{}
}

// Add registers a sink. It returns a function that removes the sink
// when called. Calling the returned function more than once is safe
// and idempotent.
func (r *SinkRouter) Add(kinds []string, fn SinkFunc) func() {
	r.mu.Lock()
	id := r.nextID
	r.nextID++
	s := sink{
		id:    id,
		kinds: makeKindsSet(kinds),
		fn:    fn,
	}
	r.sinks = append(r.sinks, s)
	r.mu.Unlock()

	return func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		for i, existing := range r.sinks {
			if existing.id == id {
				r.sinks = append(r.sinks[:i], r.sinks[i+1:]...)
				return
			}
		}
	}
}

// Deliver invokes all registered sinks for the given sessionID and
// event, applying per-sink kind filters. Sinks that panic are
// recovered and logged.
func (r *SinkRouter) Deliver(sessionID string, event loop.OutputEvent) {
	r.mu.RLock()
	sinks := make([]sink, len(r.sinks))
	copy(sinks, r.sinks)
	r.mu.RUnlock()

	kind := event.Kind()
	for _, s := range sinks {
		if s.kinds != nil {
			if _, ok := s.kinds[kind]; !ok {
				continue
			}
		}
		callSink(s.fn, sessionID, event)
	}
}

func callSink(fn SinkFunc, sessionID string, event loop.OutputEvent) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("sink callback panicked",
				"recover", r,
				"session_id", sessionID,
				"event_kind", event.Kind())
		}
	}()
	fn(sessionID, event)
}