package loop

import (
	"sync"
)

// subscription tracks a single subscriber's channel and the event kinds it
// has requested.
type subscription struct {
	ch    chan OutputEvent
	kinds map[string]struct{}
}

// FanOut distributes OutputEvent values from a source channel to multiple
// subscribers, filtered by event kind.
type FanOut struct {
	src    <-chan OutputEvent
	subs   []subscription
	mu     sync.Mutex
	done   chan struct{}
	once   sync.Once
	wg     sync.WaitGroup
	closed bool
}

// NewFanOut creates a FanOut that reads from src and distributes events.
// The FanOut starts a background goroutine that reads from src until it is
// closed or the FanOut is closed.
func NewFanOut(src <-chan OutputEvent) *FanOut {
	f := &FanOut{
		src:  src,
		subs: make([]subscription, 0),
		done: make(chan struct{}),
	}
	f.wg.Add(1)
	go f.run()
	return f
}

func (f *FanOut) run() {
	defer f.wg.Done()
	for {
		select {
		case event, ok := <-f.src:
			if !ok {
				// Source closed — close all subscribers and return.
				f.closeAll()
				return
			}
			f.send(event)
		case <-f.done:
			// FanOut was explicitly closed — drain remaining events from src
			// without blocking, close all subscribers, and return.
			f.drain()
			f.closeAll()
			return
		}
	}
}

func (f *FanOut) send(event OutputEvent) {
	f.mu.Lock()
	subs := make([]subscription, len(f.subs))
	copy(subs, f.subs)
	f.mu.Unlock()
	for _, sub := range subs {
		if _, ok := sub.kinds[event.Kind()]; ok {
			select {
			case sub.ch <- event:
			case <-f.done:
				return
			default:
				// Subscriber's buffer is full — drop the event to prevent
				// blocking the entire FanOut and stalling other subscribers.
			}
		}
	}
}

func (f *FanOut) drain() {
	for {
		select {
		case event, ok := <-f.src:
			if !ok {
				return
			}
			f.send(event)
		default:
			return
		}
	}
}

func (f *FanOut) closeAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, sub := range f.subs {
		close(sub.ch)
	}
	f.closed = true
}

// Subscribe returns a receive-only channel that receives all OutputEvents
// whose Kind() matches any of the given kinds. The channel is closed when
// the FanOut is closed.
//
// Events are sent non-blocking with a fixed buffer of 100. If a subscriber
// falls behind and its buffer fills, subsequent matching events are dropped
// for that subscriber. The caller must read from the channel promptly to
// avoid missing events.
//
// Subscribing to multiple kinds on one channel preserves ordering across
// those event types — events are delivered in the order they were received
// from the source.
func (f *FanOut) Subscribe(kinds ...string) <-chan OutputEvent {
	ch := make(chan OutputEvent, 100)
	kindSet := make(map[string]struct{}, len(kinds))
	for _, k := range kinds {
		kindSet[k] = struct{}{}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		close(ch)
		return ch
	}
	f.subs = append(f.subs, subscription{ch: ch, kinds: kindSet})
	return ch
}

// Close stops the FanOut and closes all subscriber channels.
func (f *FanOut) Close() error {
	f.once.Do(func() {
		close(f.done)
	})
	f.wg.Wait()
	return nil
}
