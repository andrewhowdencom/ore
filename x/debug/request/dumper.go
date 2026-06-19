package request

import (
	"bytes"
	"fmt"
	"io"
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

	// arms maps a thread ID (as set via loop.WithThreadID) to a *slot.
	// Each slot owns its armed flag and the in-progress capture for
	// its one-shot round trip. Concurrent Enable calls on the same
	// thread ID land on the same slot.
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
// internal map. Memory cost is one *slot per armed thread ID; cleanup
// is the caller's responsibility.
func (d *Dumper) Enable(threadID string) {
	s, _ := d.arms.LoadOrStore(threadID, &slot{})
	s.(*slot).armed.Store(true)
}

// Armed reports whether the dumper is currently armed for the given
// thread ID. Returns false for unknown thread IDs.
func (d *Dumper) Armed(threadID string) bool {
	v, ok := d.arms.Load(threadID)
	if !ok {
		return false
	}
	return v.(*slot).armed.Load()
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

// slot is the per-thread state. It owns its one-shot armed flag and
// the in-progress capture buffer that the winning RoundTrip fills in.
type slot struct {
	armed atomic.Bool

	// capture is allocated lazily by the RoundTrip that wins the
	// CAS. Until then it is nil; Enable alone does not allocate it.
	// Only one RoundTrip ever writes to it (the one that wins the
	// CAS), so no internal locking is needed.
	capture *capture
}

// capture is the in-progress per-round-trip capture buffer. The
// request fields are filled in before the real RoundTrip; the
// response fields are filled in after (Task 4).
type capture struct {
	requestHeaders http.Header
	requestBody    bytes.Buffer

	responseStatus string
	responseHeader http.Header
	responseBody   bytes.Buffer
}

// dumperTransport is the per-instance wrapper installed via Wrap.
type dumperTransport struct {
	dumper *Dumper
	base   http.RoundTripper
}

// RoundTrip satisfies [http.RoundTripper]. When the dumper is armed for
// the request's thread ID, it atomically disarms and captures the
// request and response. When disarmed, or when the request has no
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

	s := v.(*slot)
	if !s.armed.CompareAndSwap(true, false) {
		// Already disarmed (or never armed for this slot). The hot
		// path passes through unchanged.
		return t.base.RoundTrip(req)
	}

	// Disarmed. We now own this slot's capture. Allocate lazily so
	// never-armed slots don't carry a buffer.
	c := &capture{}
	s.capture = c

	// Capture the request: copy headers, drain body to the buffer,
	// then restore the body for the real transport.
	c.requestHeaders = req.Header.Clone()
	if req.Body != nil {
		if err := drainBody(req.Body, &c.requestBody); err != nil {
			// We have already disarmed the slot. We could not capture
			// the request body; fall back to a noop for this round
			// trip and leave the capture half-filled. Re-arming will
			// start a fresh capture on the next request.
			s.capture = nil
			return t.base.RoundTrip(req)
		}
		req.Body = io.NopCloser(bytes.NewReader(c.requestBody.Bytes()))
	}

	// Hand off to the real transport.
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		return resp, err
	}

	// Capture the response: copy status and headers, then wrap
	// resp.Body so the wire still sees a stream while we tee a
	// copy into the capture buffer.
	c.responseStatus = resp.Status
	c.responseHeader = resp.Header.Clone()
	if resp.Body != nil {
		resp.Body = &teeReadCloser{
			rc: resp.Body,
			dst: &c.responseBody,
		}
	}

	return resp, nil
}

// teeReadCloser is an io.ReadCloser that copies every Read into dst
// before returning the bytes to the caller. It is used to capture a
// streaming response body without consuming it from the wire's point
// of view. Close closes the underlying reader.
type teeReadCloser struct {
	rc  io.ReadCloser
	dst io.Writer
}

func (t *teeReadCloser) Read(p []byte) (int, error) {
	n, err := t.rc.Read(p)
	if n > 0 {
		if _, werr := t.dst.Write(p[:n]); werr != nil {
			// Best-effort: a write failure to the capture
			// buffer does not affect the wire. We swallow it
			// and continue returning the bytes the
			// underlying read produced; the wire sees the
			// correct response.
			_ = werr
		}
	}
	return n, err
}

func (t *teeReadCloser) Close() error {
	return t.rc.Close()
}

// drainBody reads the entire body from r into buf. It is intentionally
// tolerant of nil readers and returns nil.
func drainBody(r io.ReadCloser, buf *bytes.Buffer) error {
	if r == nil {
		return nil
	}
	defer r.Close()
	if _, err := io.Copy(buf, r); err != nil {
		return fmt.Errorf("drain request body: %w", err)
	}
	return nil
}
