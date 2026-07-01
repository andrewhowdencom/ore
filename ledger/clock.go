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

// clock is the unexported concrete clock used by [NewThread] when no
// [WithThreadClock] option is supplied. Kept as a package-private alias
// of [realClock] for symmetry with the buffer/thread internal fields
// that pre-date the split.
var clock = realClock{}
