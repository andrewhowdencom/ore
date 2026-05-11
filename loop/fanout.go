package loop

import (
	"sync"
)

// FanOut distributes OutputEvent values from a source channel to multiple
// subscribers, filtered by event kind.
type FanOut struct {
	src    <-chan OutputEvent
	subs   map[string][]chan OutputEvent
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
		subs: make(map[string][]chan OutputEvent),
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
	chans := f.subs[event.Kind()]
	f.mu.Unlock()
	for _, ch := range chans {
		select {
		case ch <- event:
		case <-f.done:
			return
		}
	}
}

func (f *FanOut) drain() {
	for {
		select {
		case _, ok := <-f.src:
			if !ok {
				return
			}
		default:
			return
		}
	}
}

func (f *FanOut) closeAll() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, chans := range f.subs {
		for _, ch := range chans {
			close(ch)
		}
	}
	f.closed = true
}

// Subscribe returns a receive-only channel that receives all OutputEvents
// whose Kind() matches the given kind. The channel is closed when the
// FanOut is closed. The caller must read from the channel or provide
// sufficient buffer capacity to avoid blocking the FanOut.
func (f *FanOut) Subscribe(kind string) <-chan OutputEvent {
	ch := make(chan OutputEvent, 100)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		close(ch)
		return ch
	}
	f.subs[kind] = append(f.subs[kind], ch)
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
