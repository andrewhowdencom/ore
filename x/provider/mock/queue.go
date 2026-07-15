package mock

import "sync/atomic"

// Queue is a thread-safe, round-robin queue of canned [Response] values.
// Each call to [Queue.Next] advances an internal atomic counter and
// returns the response at the new position; once the counter exceeds the
// last index, it wraps back to zero. A queue of length 1 collapses to
// "return the same response forever".
//
// Queue instances are created by [NewQueue] and are safe for concurrent
// use. The slice of responses is captured by value at construction time
// and is not mutated thereafter, so no mutex is required around the
// slice itself — only the counter, which uses [sync/atomic].
type Queue struct {
	responses []Response
	counter   atomic.Uint64
}

// NewQueue constructs a Queue from a variadic list of canned responses.
// The slice is captured as-is; mutating the caller's slice after this
// call has no effect on the queue.
func NewQueue(responses ...Response) *Queue {
	q := &Queue{
		responses: make([]Response, len(responses)),
	}
	copy(q.responses, responses)
	return q
}

// Next returns the next canned response and advances the internal
// counter modulo the queue length. The zero-length queue returns the
// zero [Response] value (the call is still safe); callers that need to
// forbid an empty queue should validate at construction.
//
// Next is safe for concurrent calls: two goroutines calling Next
// simultaneously receive distinct slots (the atomic counter guarantees
// unique increments, modulo the wrap).
func (q *Queue) Next() Response {
	if len(q.responses) == 0 {
		return Response{}
	}
	idx := q.counter.Add(1) - 1
	return q.responses[idx%uint64(len(q.responses))]
}

// Len returns the number of canned responses in the queue.
func (q *Queue) Len() int {
	return len(q.responses)
}
