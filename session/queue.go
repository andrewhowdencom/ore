package session

import (
	"context"
	"sync"
)

// workQueue is an unbounded FIFO of events waiting to be processed by a
// single worker goroutine. It is the internal serialization primitive
// for a Session: events submitted via Session.Run are enqueued here and
// drained in order by the worker.
//
// The queue is not safe for concurrent submission from many goroutines —
// the Session's mutex serializes callers — but the worker's drain loop
// is concurrency-safe relative to the producer via the embedded cond.
type workQueue struct {
	mu       sync.Mutex
	cond     *sync.Cond
	items    []queuedEvent
	closed   bool
	workerWG sync.WaitGroup
}

// queuedEvent wraps an Event with the caller's context and an optional
// completion channel. If done is non-nil, the worker signals the final
// error on it after processing.
type queuedEvent struct {
	event Event
	ctx   context.Context
	done  chan error
}

func newWorkQueue() *workQueue {
	q := &workQueue{}
	q.cond = sync.NewCond(&q.mu)
	return q
}

// submit enqueues an event. The cond is signaled to wake the worker.
// If the queue is closed, submit returns an error and the caller is
// expected to surface it to the user.
func (q *workQueue) submit(ctx context.Context, evt Event) error {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return errSessionClosed
	}
	q.items = append(q.items, queuedEvent{event: evt, ctx: ctx})
	q.mu.Unlock()
	q.cond.Signal()
	return nil
}

// runWorker drains the queue, invoking fn for each event. It returns
// when the queue is closed and the work is drained. The worker's
// goroutine is joined via workerWG.
func (q *workQueue) runWorker(fn func(context.Context, Event) error) {
	q.workerWG.Add(1)
	defer q.workerWG.Done()
	for {
		q.mu.Lock()
		for len(q.items) == 0 && !q.closed {
			q.cond.Wait()
		}
		if q.closed && len(q.items) == 0 {
			q.mu.Unlock()
			return
		}
		item := q.items[0]
		q.items = q.items[1:]
		q.mu.Unlock()

		err := fn(item.ctx, item.event)
		if item.done != nil {
			select {
			case item.done <- err:
			default:
			}
		}
	}
}

// close signals the worker to exit. After close, the queue rejects
// further submits. close blocks until the worker has drained and
// returned.
func (q *workQueue) close() {
	q.mu.Lock()
	q.closed = true
	q.mu.Unlock()
	q.cond.Broadcast()
	q.workerWG.Wait()
}
