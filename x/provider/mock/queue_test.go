package mock

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestQueue_Rotation exercises the [Queue.Next] round-robin behavior
// across the three shapes the package contract documents: single
// response (always the same), multi-response (sequential), and
// wrap-around (back to zero after the last index).
func TestQueue_Rotation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		responses []Response
		want      []string // expected Text fields from successive Next calls
	}{
		{
			name:      "single response collapses to same",
			responses: []Response{{Text: "always"}},
			want:      []string{"always", "always", "always", "always"},
		},
		{
			name:      "two responses alternate",
			responses: []Response{{Text: "a"}, {Text: "b"}},
			want:      []string{"a", "b", "a", "b"},
		},
		{
			name:      "wrap-around after last index",
			responses: []Response{{Text: "x"}, {Text: "y"}, {Text: "z"}},
			want:      []string{"x", "y", "z", "x", "y", "z"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			q := NewQueue(tt.responses...)
			for i, want := range tt.want {
				got := q.Next()
				assert.Equal(t, want, got.Text, "call %d", i)
			}
		})
	}
}

// TestQueue_Len reports the configured length without consuming any
// elements.
func TestQueue_Len(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		responses []Response
		want      int
	}{
		{name: "zero", responses: nil, want: 0},
		{name: "one", responses: []Response{{Text: "x"}}, want: 1},
		{name: "many", responses: []Response{{Text: "a"}, {Text: "b"}, {Text: "c"}}, want: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			q := NewQueue(tt.responses...)
			assert.Equal(t, tt.want, q.Len())
		})
	}
}

// TestQueue_EmptyReturnsZero verifies that calling Next on an empty
// queue returns the zero Response rather than panicking. Empty queues
// are a programming error at the call site, but the queue itself
// remains safe.
func TestQueue_EmptyReturnsZero(t *testing.T) {
	t.Parallel()

	q := NewQueue()
	got := q.Next()
	assert.Equal(t, Response{}, got)
}

// TestQueue_ConcurrentNext spawns N goroutines that all call Next. The
// atomic counter guarantees each call receives a unique slot modulo the
// queue length, so the union of all observations equals the queue's
// full cycle, repeated N/len(times). Race-detector clean.
func TestQueue_ConcurrentNext(t *testing.T) {
	t.Parallel()

	responses := []Response{
		{Text: "a"}, {Text: "b"}, {Text: "c"},
	}
	q := NewQueue(responses...)

	const goroutines = 32
	const calls = 100

	var wg sync.WaitGroup
	counts := make(map[string]*atomic.Int64, len(responses))
	for _, r := range responses {
		counts[r.Text] = &atomic.Int64{}
	}

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < calls; j++ {
				r := q.Next()
				counts[r.Text].Add(1)
			}
		}()
	}
	wg.Wait()

	total := int64(goroutines * calls)
	var sum int64
	for _, c := range counts {
		sum += c.Load()
	}
	assert.Equal(t, total, sum, "every Next call must return exactly one response")
}