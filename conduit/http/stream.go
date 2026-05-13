package http

import (
	"fmt"
	stdhttp "net/http"
)

// ndjsonWriter streams newline-delimited JSON objects over an HTTP response.
type ndjsonWriter struct {
	w       stdhttp.ResponseWriter
	flusher stdhttp.Flusher
}

// newNDJSONWriter creates an ndjsonWriter for the given response writer.
// It returns an error if the writer does not support http.Flusher.
func newNDJSONWriter(w stdhttp.ResponseWriter) (*ndjsonWriter, error) {
	flusher, ok := w.(stdhttp.Flusher)
	if !ok {
		return nil, fmt.Errorf("response writer does not support flushing")
	}
	return &ndjsonWriter{
		w:       w,
		flusher: flusher,
	}, nil
}

// WriteEvent writes a single JSON object followed by a newline and flushes
// the response buffer.
func (nw *ndjsonWriter) WriteEvent(data []byte) error {
	if _, err := nw.w.Write(data); err != nil {
		return err
	}
	if _, err := nw.w.Write([]byte("\n")); err != nil {
		return err
	}
	nw.flusher.Flush()
	return nil
}
