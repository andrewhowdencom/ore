package bash

import (
	"os"
	"sync"
)

// BoundedBuffer is an io.Writer that retains only a rolling 2*cap
// tail in memory and optionally spills the full byte stream to a
// temp file (created lazily on first overflow). The Write method
// never blocks and never returns an error (other than from the
// underlying temp file write).
//
// Mechanics:
//
//   - Internally, BoundedBuffer keeps a single byte slice "tail" of
//     length at most 2*cap. On each Write, the bytes are appended
//     to the slice; if the result exceeds 2*cap, the front is
//     dropped to maintain the cap.
//   - The first time tail exceeds cap, a temp file is opened
//     (os.CreateTemp with prefix "ore-bash-") and the cumulative
//     tail content is written to it. Subsequent writes are also
//     appended to the file, so the file always contains the full
//     byte stream from the first overflow onwards.
//   - Path() returns the temp file path, or "" if no spill has
//     occurred.
//   - Close() closes the temp file. It is safe to call Close on a
//     BoundedBuffer that has not spilled.
//   - The struct is safe for concurrent use. The mutex serializes
//     access to the tail and the temp file. The bash tool only
//     writes from a single goroutine (cmd.Stdout), so contention
//     is not a concern in practice; the lock is defensive.
type BoundedBuffer struct {
	mu    sync.Mutex
	cap   int
	tail  []byte
	file  *os.File
	path  string
	spill bool
}

// NewBoundedBuffer creates a BoundedBuffer that retains at most
// 2*cap bytes in memory. A non-positive cap is treated as
// frameworkDefaultTailCap.
func NewBoundedBuffer(cap int) *BoundedBuffer {
	if cap <= 0 {
		cap = frameworkDefaultTailCap
	}
	return &BoundedBuffer{cap: cap}
}

// frameworkDefaultTailCap is the default per-stream tail cap
// applied by the bash tool. The choice mirrors the framework's
// default byte cap of 50 KB / 2000 lines, but is applied to each
// stream independently. BoundedBuffer keeps a 2*cap rolling tail
// so the cap is hit by a Write that brings tail to length cap+1;
// the dropped head ensures the tail is the most recent cap bytes.
const frameworkDefaultTailCap = 50_000

// Write appends p to the buffer. It never returns an error except
// in the pathological case where the temp file write fails; in
// that case, Write returns the error and the bytes are still
// retained in the in-memory tail so the rest of the stream is not
// lost.
func (b *BoundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// Append to the in-memory tail.
	b.tail = append(b.tail, p...)

	// Spill to a temp file the first time we cross the cap.
	if !b.spill && len(b.tail) > b.cap {
		f, err := os.CreateTemp("", "ore-bash-*.log")
		if err != nil {
			// Could not create the temp file; fall back to
			// in-memory only. The caller still has the tail.
			return len(p), err
		}
		if _, err := f.Write(b.tail); err != nil {
			f.Close()
			os.Remove(f.Name())
			return len(p), err
		}
		b.file = f
		b.path = f.Name()
		b.spill = true
	} else if b.spill {
		// Continue spilling to the file.
		if _, err := b.file.Write(p); err != nil {
			return len(p), err
		}
	}

	// Trim the in-memory tail to 2*cap. We keep a 2* window so
	// line-based truncation at a later stage has enough context.
	if len(b.tail) > 2*b.cap {
		// Drop the front; copy the back half.
		newTail := make([]byte, 2*b.cap)
		copy(newTail, b.tail[len(b.tail)-2*b.cap:])
		b.tail = newTail
	}

	return len(p), nil
}

// String returns the current in-memory tail as a string. The
// returned string is a copy; callers may modify it freely.
func (b *BoundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.tail)
}

// Bytes returns the current in-memory tail as a byte slice. The
// returned slice is a copy; callers may modify it freely.
func (b *BoundedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]byte, len(b.tail))
	copy(out, b.tail)
	return out
}

// Path returns the temp file path, or "" if no spill has occurred.
func (b *BoundedBuffer) Path() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.path
}

// Spilled reports whether the buffer has spilled to a temp file.
func (b *BoundedBuffer) Spilled() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.spill
}

// Close closes the temp file. It is safe to call Close on a
// BoundedBuffer that has not spilled. The temp file is NOT
// removed; the caller (typically the bash tool) keeps the path so
// the LLM can read the full output via read_file.
func (b *BoundedBuffer) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.file == nil {
		return nil
	}
	err := b.file.Close()
	b.file = nil
	return err
}
