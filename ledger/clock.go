package ledger

import "time"

// Clock provides the current time for a [Thread] (or any [State]
// implementation) to set turn timestamps. Implementations can be
// swapped for deterministic testing.
type Clock interface {
	Now() time.Time
}

// realClock is the default production clock that delegates to time.Now.
type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// ClockFunc adapts a plain function to the [Clock] interface. Useful in
// tests that need to stamp turns with a fixed or deterministic time.
type ClockFunc func() time.Time

// Now implements Clock.
func (f ClockFunc) Now() time.Time { return f() }

// clock is the unexported concrete clock used by [NewThread] when no
// [WithThreadClock] option is supplied. Kept as a package-private alias
// of [realClock] for symmetry with the buffer/thread internal fields
// that pre-date the split.
var clock = realClock{}
