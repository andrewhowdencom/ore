package tui

import (
	"bytes"
	"io"
	"sync"
)

// LogBuffer captures slog output in memory so it can be replayed after the
// Bubble Tea TUI exits. The TUI owns the terminal's alternate screen buffer;
// any writes to stderr during Start() corrupt the display. Applications that
// use the TUI conduit should direct slog to a LogBuffer while the TUI runs,
// then flush captured output to a real writer after Start() returns.
//
//	buf := tui.NewLogBuffer()
//	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
//	// ... run the TUI ...
//	buf.FlushTo(os.Stderr)
type LogBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

// NewLogBuffer creates a thread-safe buffered io.Writer suitable for use as
// a slog.Handler output sink.
func NewLogBuffer() *LogBuffer {
	return &LogBuffer{}
}

// Write implements io.Writer. It is safe for concurrent use.
func (w *LogBuffer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.Write(p)
}

// FlushTo copies all captured bytes to dst and clears the buffer. It is safe
// for concurrent use. If no bytes were captured, FlushTo is a no-op.
func (w *LogBuffer) FlushTo(dst io.Writer) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.buf.Len() == 0 {
		return nil
	}
	_, err := io.Copy(dst, &w.buf)
	w.buf.Reset()
	return err
}
