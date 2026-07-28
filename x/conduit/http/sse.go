package http

import (
	"fmt"
	stdhttp "net/http"
)

// sseWriter formats and flushes Server-Sent Events over an HTTP response.
type sseWriter struct {
	w       stdhttp.ResponseWriter
	flusher stdhttp.Flusher
}

// newSSEWriter creates an sseWriter for the given response writer.
// It returns an error if the writer does not support http.Flusher.
func newSSEWriter(w stdhttp.ResponseWriter) (*sseWriter, error) {
	flusher, ok := w.(stdhttp.Flusher)
	if !ok {
		return nil, fmt.Errorf("response writer does not support flushing")
	}
	return &sseWriter{
		w:       w,
		flusher: flusher,
	}, nil
}

// WriteEvent writes a single SSE event with the given kind and JSON data,
// followed by a blank line, and flushes the response buffer.
func (sw *sseWriter) WriteEvent(kind string, data []byte) error {
	if _, err := fmt.Fprintf(sw.w, "event: %s\n", kind); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(sw.w, "data: %s\n\n", string(data)); err != nil {
		return err
	}
	sw.flusher.Flush()
	return nil
}

// WriteComment writes a single SSE comment line (a line beginning
// with ':' which clients ignore) and flushes the response buffer.
// It is used to flush response headers before any events arrive,
// which lets the client's Do() return promptly rather than blocking
// on the first byte. It can also be used as a periodic keepalive.
func (sw *sseWriter) WriteComment(text string) error {
	if _, err := fmt.Fprintf(sw.w, ": %s\n\n", text); err != nil {
		return err
	}
	sw.flusher.Flush()
	return nil
}
