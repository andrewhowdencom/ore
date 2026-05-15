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
