package ledger

import "time"

// mockClock is a test double that returns a fixed instant. It
// implements the Clock interface and is used to make Thread.Append
// timestamps deterministic in tests that exercise clock injection.
type mockClock struct {
	now time.Time
}

func (m *mockClock) Now() time.Time { return m.now }