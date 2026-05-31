package loop

import (
	"sync"

	"github.com/andrewhowdencom/ore/artifact"
)

// subscription tracks a single subscriber's channel and the event kinds it
// has requested.
type subscription struct {
	ch    chan OutputEvent
	kinds map[string]struct{}
}

// matches reports whether the subscription accepts the given event kind.
// A nil kinds map means the subscription accepts all kinds.
func (s subscription) matches(kind string) bool {
	if s.kinds == nil {
		return true
	}
	_, ok := s.kinds[kind]
	return ok
}

// FanOut distributes OutputEvent values from a source channel to multiple
// subscribers, filtered by event kind.
//
// Complete, self-contained events (TurnCompleteEvent, PropertiesEvent,
// LifecycleEvent, ErrorEvent, and non-delta ArtifactEvents) are retained in a
// bounded replay buffer (default capacity 50). When a new subscriber registers
// via Subscribe(), buffered events matching the subscriber's kind filter are
// replayed before live events. Delta events (text_delta, reasoning_delta,
// tool_call_delta) remain ephemeral and are not buffered — late subscribers do
// not receive them. This makes the FanOut lossy for streaming chunks but
// preserves complete event history for late-joining consumers.
type FanOut struct {
	src       <-chan outputEventEnvelope
	subs      []subscription
	mu        sync.Mutex
	done      chan struct{}
	once      sync.Once
	wg        sync.WaitGroup
	closed    bool
	buffer    []OutputEvent
	bufferCap int
}

// NewFanOut creates a FanOut that reads from src and distributes events.
// The FanOut starts a background goroutine that reads from src until it is
// closed or the FanOut is closed.
func NewFanOut(src <-chan outputEventEnvelope) *FanOut {
	f := &FanOut{
		src:       src,
		subs:      make([]subscription, 0),
		done:      make(chan struct{}),
		bufferCap: 50,
	}
	f.wg.Add(1)
	go f.run()
	return f
}

func (f *FanOut) run() {
	defer f.wg.Done()
	for {
		select {
		case env, ok := <-f.src:
			if !ok {
				// Source closed — close all subscribers and return.
				f.closeAll()
				return
			}
			f.send(env.event)
			close(env.done)
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
	if isReplayable(event) {
		if f.bufferCap > 0 {
			f.buffer = append(f.buffer, event)
			if len(f.buffer) > f.bufferCap {
				f.buffer = f.buffer[len(f.buffer)-f.bufferCap:]
			}
		}
	}
	subs := make([]subscription, len(f.subs))
	copy(subs, f.subs)
	f.mu.Unlock()
	for _, sub := range subs {
		if sub.matches(event.Kind()) {
			select {
			case sub.ch <- event:
			case <-f.done:
				return
			}
		}
	}
}

func (f *FanOut) drain() {
	for {
		select {
		case env, ok := <-f.src:
			if !ok {
				return
			}
			f.send(env.event)
			close(env.done)
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
// whose Kind() matches any of the given kinds. If no kinds are provided,
// the channel receives all events regardless of kind. The channel is closed
// when the FanOut is closed.
//
// Events are sent with a fixed buffer of 100000. If a subscriber falls
// behind and its buffer fills, send() applies backpressure to the entire
// FanOut rather than dropping events. The caller must read from the channel
// promptly to prevent head-of-line blocking for other subscribers.
//
// Subscribing to multiple kinds on one channel preserves ordering across
// those event types — events are delivered in the order they were received
// from the source.
//
// Replay buffer: complete events (TurnCompleteEvent, PropertiesEvent,
// LifecycleEvent, ErrorEvent, and non-delta ArtifactEvents) are buffered in
// a bounded ring and replayed to new subscribers before live events. Delta
// events (text_delta, reasoning_delta, tool_call_delta) remain ephemeral
// and lossy — late subscribers do not receive them.
func (f *FanOut) Subscribe(kinds ...string) <-chan OutputEvent {
	ch := make(chan OutputEvent, 100000)
	var kindSet map[string]struct{}
	if len(kinds) > 0 {
		kindSet = make(map[string]struct{}, len(kinds))
		for _, k := range kinds {
			kindSet[k] = struct{}{}
		}
	}
	sub := subscription{ch: ch, kinds: kindSet}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		close(ch)
		return ch
	}
	// Replay buffered complete events matching the subscriber's kinds.
	for _, event := range f.buffer {
		if sub.matches(event.Kind()) {
			ch <- event
		}
	}
	f.subs = append(f.subs, sub)
	return ch
}

// isReplayable reports whether an event should be retained in the bounded
// replay buffer for late subscribers. Delta events (text_delta,
// reasoning_delta, tool_call_delta) are not replayable because they are
// ephemeral streaming chunks; receiving a partial sequence would produce a
// broken artifact. All other event types are self-contained and safe to replay.
func isReplayable(event OutputEvent) bool {
	ae, ok := event.(ArtifactEvent)
	if !ok {
		return true
	}
	_, isDelta := ae.Artifact.(artifact.Delta)
	return !isDelta
}

// Close stops the FanOut and closes all subscriber channels.
func (f *FanOut) Close() error {
	f.once.Do(func() {
		close(f.done)
	})
	f.wg.Wait()
	return nil
}
