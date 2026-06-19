package request

import (
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/andrewhowdencom/ore/loop"
)

// Dumper is a thread-scoped, one-shot capture facility for the HTTP
// request and response of the next provider invocation. It is always
// installed but noop when disarmed: the hot path cost of an unarmed
// RoundTrip is one sync.Map.Load.
//
// Dumper instances are safe for concurrent use.
type Dumper struct {
	appName   string
	outputDir string

	// arms maps a thread ID (as set via loop.WithThreadID) to an
	// *atomic.Bool flag. Enable creates the slot if absent; the flag
	// is set to true. The next RoundTrip whose request context
	// carries that thread ID performs a CompareAndSwap(true, false)
	// to disarm exactly once. Subsequent requests pass through
	// unchanged.
	arms sync.Map
}

// New returns a Dumper configured for the given application name. The
// appName is used in the capture file name:
//
//	<outputDir>/<appName>.request.<RFC3339>.log
//
// By default, outputDir is the current working directory. Use
// [WithOutputDir] to change it.
func New(appName string, opts ...Option) *Dumper {
	d := &Dumper{
		appName:   appName,
		outputDir: ".",
	}
	for _, opt := range opts {
		opt(d)
	}
	return d
}

// Option configures a Dumper at construction time.
type Option func(*Dumper)

// WithOutputDir sets the directory where capture files are written.
// Relative paths are interpreted against the process's current working
// directory at the time the file is opened.
func WithOutputDir(dir string) Option {
	return func(d *Dumper) {
		d.outputDir = dir
	}
}

// Enable arms the dumper to capture the next RoundTrip whose request
// context carries the given thread ID. The capture is one-shot: the
// first armed RoundTrip atomically disarms itself and writes the file.
// Re-arming while already armed is a no-op.
//
// If the dumper is disarmed or the thread ID never matches a real
// request (e.g. the user navigates away), the slot remains in the
// internal map. Memory cost is one *atomic.Bool per armed thread ID;
// cleanup is the caller's responsibility.
func (d *Dumper) Enable(threadID string) {
	v, _ := d.arms.LoadOrStore(threadID, &atomic.Bool{})
	v.(*atomic.Bool).Store(true)
}

// Armed reports whether the dumper is currently armed for the given
// thread ID. Returns false for unknown thread IDs.
func (d *Dumper) Armed(threadID string) bool {
	v, ok := d.arms.Load(threadID)
	if !ok {
		return false
	}
	return v.(*atomic.Bool).Load()
}

// Wrap returns an [http.RoundTripper] that intercepts the next armed
// request from each thread and captures it. When disarmed (or when the
// request context has no thread ID), it passes through to the supplied
// base transport unchanged. A nil base falls back to
// [http.DefaultTransport].
func (d *Dumper) Wrap(rt http.RoundTripper) http.RoundTripper {
	if rt == nil {
		rt = http.DefaultTransport
	}
	return &dumperTransport{dumper: d, base: rt}
}

// Close releases any resources held by the dumper. It is safe to call
// repeatedly and to call after a capture has completed. In this
// implementation the dumper holds no persistent resources between
// captures, so Close is currently a no-op; it exists for forward
// compatibility with later file-handle lifecycle changes.
func (d *Dumper) Close() error {
	return nil
}

// dumperTransport is the per-instance wrapper installed via Wrap.
type dumperTransport struct {
	dumper *Dumper
	base   http.RoundTripper
}

// RoundTrip satisfies [http.RoundTripper]. When the dumper is armed for
// the request's thread ID, it atomically disarms and (in Task 3+) captures
// the request and response. When disarmed, or when the request has no
// thread ID on its context, it forwards to the base transport
// unchanged.
func (t *dumperTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	threadID, ok := loop.ThreadIDFrom(req.Context())
	if !ok {
		// Skip-silently: requests outside the loop (e.g. scripts, tests)
		// never carry a thread ID. The dumper never grabs traffic for
		// the wrong thread.
		return t.base.RoundTrip(req)
	}

	v, ok := t.dumper.arms.Load(threadID)
	if !ok {
		return t.base.RoundTrip(req)
	}

	slot := v.(*atomic.Bool)
	if !slot.CompareAndSwap(true, false) {
		// Already disarmed (or never armed for this slot). The hot
		// path passes through unchanged.
		return t.base.RoundTrip(req)
	}

	// Disarmed. Capture will be added in Task 3.
	return t.base.RoundTrip(req)
}
