package request

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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
// repeatedly and to call after a capture has completed.
//
// Close forces any open capture file to flush and close. This is the
// cleanup path for the rare case where the wire leaks a response body
// (and therefore never triggers our close hook).
func (d *Dumper) Close() error {
	var firstErr error
	d.arms.Range(func(_, v any) bool {
		s := v.(*slot)
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.capture == nil {
			return true
		}
		if s.capture.file == nil {
			return true
		}
		if err := s.capture.flushAndClose(); err != nil && firstErr == nil {
			firstErr = err
		}
		return true
	})
	return firstErr
}

// slot is the per-thread state. It owns its one-shot armed flag and
// the in-progress capture for its one round trip. A per-slot mutex
// guards file open/close so that Close() and the response body
// close hook cannot race.
type slot struct {
	armed atomic.Bool

	mu      sync.Mutex
	capture *capture
}

// capture is the in-progress per-round-trip capture. The request
// fields are filled in before the real RoundTrip; the response fields
// and the streaming body are filled in after. All formatted output
// goes to bw (a buffered writer over the capture file); the file is
// closed when the wrapped response body's Close() fires, or when
// the dumper's Close() is called as a cleanup fallback.
type capture struct {
	requestHeaders http.Header
	requestBody    bytes.Buffer // held in memory so the body can be restored

	responseStatus string
	responseHeader http.Header

	// File sink. Lazily opened on the first capture-init event (the
	// armed RoundTrip that wins the CAS). Closed when the wrapped
	// response body fires its Close, or when the dumper is
	// torn down.
	file *os.File
	bw   *bufio.Writer
	path string
}

// Path returns the on-disk path of this capture's file, or "" if no
// file has been opened yet. Exposed for tests and diagnostics; not
// intended as part of the stable API.
func (c *capture) Path() string {
	if c == nil {
		return ""
	}
	return c.path
}

// open opens the capture file and initializes the buffered writer.
// Safe to call multiple times; subsequent calls are noops.
func (c *capture) open(d *Dumper) error {
	if c.file != nil {
		return nil
	}
	// RFC3339 uses ':' which is not portable across all
	// filesystems (notably Windows). Substitute '-' to keep the
	// path valid everywhere.
	stamp := time.Now().UTC().Format(time.RFC3339)
	stamp = strings.ReplaceAll(stamp, ":", "-")
	name := fmt.Sprintf("%s.request.%s.log", d.appName, stamp)
	c.path = filepath.Join(d.outputDir, name)
	f, err := os.Create(c.path)
	if err != nil {
		return fmt.Errorf("open capture file: %w", err)
	}
	c.file = f
	c.bw = bufio.NewWriter(f)
	return nil
}

// flushAndClose flushes the buffered writer and closes the file. It
// is a noop if no file is open.
func (c *capture) flushAndClose() error {
	if c.file == nil {
		return nil
	}
	var firstErr error
	if c.bw != nil {
		if err := c.bw.Flush(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("flush capture file: %w", err)
		}
	}
	if err := c.file.Close(); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("close capture file: %w", err)
	}
	c.file = nil
	c.bw = nil
	return firstErr
}

// writeRequest formats and writes the === REQUEST === section to the
// capture file.
func (c *capture) writeRequest(method string, u *url.URL, body []byte, header http.Header) error {
	if _, err := fmt.Fprintf(c.bw, "=== REQUEST ===\n%s %s\n", method, requestURI(u)); err != nil {
		return err
	}
	if err := writeHeaders(c.bw, header); err != nil {
		return err
	}
	if _, err := c.bw.Write(body); err != nil {
		return err
	}
	return nil
}

// writeResponseHeaders formats and writes the === RESPONSE ===
// section header (status + headers, no body yet — the body is
// streamed via the tee).
func (c *capture) writeResponseHeaders(status string, header http.Header) error {
	if _, err := fmt.Fprintf(c.bw, "\n=== RESPONSE ===\n%s\n", status); err != nil {
		return err
	}
	return writeHeaders(c.bw, header)
}

// writeHeaders writes a sorted "Key: Value\n" block for each header.
func writeHeaders(w *bufio.Writer, header http.Header) error {
	keys := make([]string, 0, len(header))
	for k := range header {
		keys = append(keys, k)
	}
	// Sort for deterministic output across platforms (header
	// iteration order is undefined in Go).
	sortStrings(keys)
	for _, k := range keys {
		for _, v := range header[k] {
			if _, err := fmt.Fprintf(w, "%s: %s\n", k, v); err != nil {
				return err
			}
		}
	}
	// Blank line separating headers from body.
	_, err := w.WriteString("\n")
	return err
}

// requestURI returns the path-and-query form of u, the same form
// http.Request.RequestURI carries. It exists so the request line in
// the capture file is canonical regardless of whether the URL has
// a host component.
func requestURI(u *url.URL) string {
	if u == nil {
		return ""
	}
	if u.Opaque != "" {
		return u.Opaque
	}
	uri := u.Path
	if u.RawQuery != "" {
		uri += "?" + u.RawQuery
	}
	if u.Fragment != "" {
		uri += "#" + u.Fragment
	}
	return uri
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

	// Disarmed. We now own this slot's capture. Open the capture
	// file lazily, then fill the request section.
	s.mu.Lock()
	c := &capture{}
	s.capture = c
	if err := c.open(t.dumper); err != nil {
		// File-open failure must not break the wire. Clear the
		// capture and fall back to a noop pass-through.
		s.mu.Unlock()
		s.capture = nil
		return t.base.RoundTrip(req)
	}

	// Capture the request: copy headers, drain body to the
	// in-memory buffer (so we can restore req.Body for the real
	// transport), then write the request section to the file.
	c.requestHeaders = req.Header.Clone()
	if req.Body != nil {
		if err := drainBody(req.Body, &c.requestBody); err != nil {
			s.mu.Unlock()
			s.capture = nil
			_ = c.flushAndClose()
			return t.base.RoundTrip(req)
		}
		req.Body = io.NopCloser(bytes.NewReader(c.requestBody.Bytes()))
	}

	if err := c.writeRequest(req.Method, req.URL, c.requestBody.Bytes(), c.requestHeaders); err != nil {
		// Best-effort: capture failed, but the wire still has a
		// working request. Continue.
		_ = err
	}
	s.mu.Unlock()

	// Hand off to the real transport.
	resp, err := t.base.RoundTrip(req)
	if err != nil {
		// RoundTrip errored. Close the capture file and return.
		s.mu.Lock()
		_ = c.flushAndClose()
		s.capture = nil
		s.mu.Unlock()
		return resp, err
	}

	// Capture the response: status, headers, then a tee on the
	// body that streams into the file as the wire reads.
	s.mu.Lock()
	c.responseStatus = resp.Status
	c.responseHeader = resp.Header.Clone()
	if err := c.writeResponseHeaders(resp.Status, resp.Header); err != nil {
		_ = err
	}
	// Flush now so the response section header is on disk before
	// the body starts streaming. Subsequent writes go through the
	// same buffered writer; the next flush is at Close.
	_ = c.bw.Flush()
	if resp.Body != nil {
		resp.Body = &captureBody{
			rc: resp.Body,
			c:  c,
		}
	}
	s.mu.Unlock()

	return resp, nil
}

// captureBody is the response body wrapper. It tees every Read into
// the capture's file writer and, on Close, flushes and closes the
// capture file. It is the only way the capture file gets closed
// during normal operation.
type captureBody struct {
	rc io.ReadCloser
	c  *capture
}

func (b *captureBody) Read(p []byte) (int, error) {
	n, err := b.rc.Read(p)
	if n > 0 {
		// Best-effort write: a capture failure must not affect
		// the wire's read.
		_, _ = b.c.bw.Write(p[:n])
	}
	return n, err
}

func (b *captureBody) Close() error {
	// Flush and close the capture file. Errors are joined with
	// the underlying body's Close error so the caller sees both.
	fileErr := b.c.flushAndClose()
	bodyErr := b.rc.Close()
	switch {
	case fileErr != nil && bodyErr != nil:
		return fmt.Errorf("close capture file: %v; close response body: %w", fileErr, bodyErr)
	case fileErr != nil:
		return fileErr
	default:
		return bodyErr
	}
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

// sortStrings is a tiny shim around sort.Strings that avoids the
// extra import in this file's import block.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
