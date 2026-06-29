package ledger

import (
	"time"

	"github.com/andrewhowdencom/ore/artifact"
)

// Clock provides the current time for Buffer to set turn timestamps.
// Implementations can be swapped for deterministic testing.
type Clock interface {
	Now() time.Time
}

// realClock is the default production clock that delegates to time.Now.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// Buffer is a simple in-memory implementation of State.
// It is not safe for concurrent use.
type Buffer struct {
	turns []Turn
	clock Clock
	meta  map[string]string
}

// WithClock configures Buffer to use a custom Clock for turn timestamps.
// If not used, Buffer defaults to the real wall-clock time.
func WithClock(c Clock) func(*Buffer) {
	return func(b *Buffer) {
		b.clock = c
	}
}

// NewBuffer creates a Buffer with optional functional options.
func NewBuffer(opts ...func(*Buffer)) *Buffer {
	b := &Buffer{clock: realClock{}}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

// Turns returns a defensive copy of the internal turn slice.
// Note: this is a shallow copy of the slice itself; the Artifacts slices
// within each Turn are not deep-copied.
func (m *Buffer) Turns() []Turn {
	result := make([]Turn, len(m.turns))
	copy(result, m.turns)
	return result
}

// Append adds a new turn to the in-memory state, recording the current
// time from its configured Clock.
func (m *Buffer) Append(role Role, artifacts ...artifact.Artifact) {
	m.turns = append(m.turns, Turn{
		Role:      role,
		Artifacts: artifacts,
		Timestamp: m.now(),
	})
}

// now returns the current time from the configured Clock, or time.Now()
// if no Clock has been set (preserving backward compatibility for &Buffer{}).
func (m *Buffer) now() time.Time {
	if m.clock == nil {
		return time.Now()
	}
	return m.clock.Now()
}

// LoadTurns replaces the internal turn slice with the provided turns.
// It is intended for deserialization paths that must preserve timestamps
// rather than re-Append them (which would overwrite timestamps).
func (m *Buffer) LoadTurns(turns []Turn) {
	m.turns = append([]Turn(nil), turns...)
}

// Meta returns the metadata handle for this buffer. The handle is live:
// reads and writes operate on the buffer's internal map. Multiple calls
// return handles that share the same backing storage, so writes through
// one are visible through any other.
//
// As with the rest of Buffer's API, the Meta handle is not safe for
// concurrent use; the same serial-call contract applies.
func (m *Buffer) Meta() Meta {
	if m.meta == nil {
		m.meta = make(map[string]string)
	}
	return &bufferMeta{b: m}
}

// bufferMeta is the concrete [Meta] implementation for [Buffer]. It holds
// a pointer to the owning buffer so writes flow directly into the
// buffer's metadata map.
type bufferMeta struct {
	b *Buffer
}

// Get implements [Meta].
func (m *bufferMeta) Get(key string) (string, bool) {
	if m.b.meta == nil {
		return "", false
	}
	v, ok := m.b.meta[key]
	return v, ok
}

// Set implements [Meta].
func (m *bufferMeta) Set(key, value string) {
	if m.b.meta == nil {
		m.b.meta = make(map[string]string)
	}
	m.b.meta[key] = value
}

// All implements [Meta].
func (m *bufferMeta) All() map[string]string {
	out := make(map[string]string, len(m.b.meta))
	for k, v := range m.b.meta {
		out[k] = v
	}
	return out
}
