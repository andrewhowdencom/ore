package bash

import (
	"os"
	"strings"
	"testing"
)

func TestBoundedBuffer_UnderCap(t *testing.T) {
	t.Parallel()

	bb := NewBoundedBuffer(100)
	n, err := bb.Write([]byte("hello world"))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 11 {
		t.Errorf("n = %d, want 11", n)
	}
	if got := bb.String(); got != "hello world" {
		t.Errorf("String() = %q, want %q", got, "hello world")
	}
	if bb.Path() != "" {
		t.Errorf("Path() = %q, want empty (no spill)", bb.Path())
	}
	if bb.Spilled() {
		t.Errorf("Spilled() = true, want false")
	}
}

func TestBoundedBuffer_OverCap(t *testing.T) {
	t.Parallel()

	bb := NewBoundedBuffer(50)

	// Write 200 bytes; should spill to a temp file.
	input := strings.Repeat("a", 200)
	n, err := bb.Write([]byte(input))
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if n != 200 {
		t.Errorf("n = %d, want 200", n)
	}

	if !bb.Spilled() {
		t.Errorf("Spilled() = false, want true")
	}
	path := bb.Path()
	if path == "" {
		t.Fatal("Path() returned empty after spill")
	}
	t.Cleanup(func() { os.Remove(path) })

	// In-memory tail should be the last 100 bytes (2*cap).
	tail := bb.String()
	if len(tail) != 100 {
		t.Errorf("len(tail) = %d, want 100", len(tail))
	}
	if tail != input[100:] {
		t.Errorf("tail is not the last 100 bytes")
	}

	// Temp file should contain the full 200 bytes.
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(contents) != input {
		t.Errorf("temp file contents = %d bytes, want %d", len(contents), len(input))
	}
}

func TestBoundedBuffer_MultipleWrites(t *testing.T) {
	t.Parallel()

	bb := NewBoundedBuffer(50)

	// Five 30-byte writes = 150 bytes total. Spill triggers
	// after the second or third write (when tail > cap).
	for i := 0; i < 5; i++ {
		if _, err := bb.Write([]byte(strings.Repeat(string(rune('a'+i)), 30))); err != nil {
			t.Fatalf("Write %d: %v", i, err)
		}
	}

	if !bb.Spilled() {
		t.Errorf("Spilled() = false after 150 bytes, want true")
	}

	path := bb.Path()
	t.Cleanup(func() { os.Remove(path) })

	// Temp file should contain the full stream.
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if len(contents) != 150 {
		t.Errorf("temp file size = %d, want 150", len(contents))
	}

	// Tail should be the last 100 bytes.
	tail := bb.String()
	if len(tail) != 100 {
		t.Errorf("len(tail) = %d, want 100", len(tail))
	}
}

func TestBoundedBuffer_Empty(t *testing.T) {
	t.Parallel()

	bb := NewBoundedBuffer(100)
	if got := bb.String(); got != "" {
		t.Errorf("String() = %q, want empty", got)
	}
	if bb.Path() != "" {
		t.Errorf("Path() = %q, want empty", bb.Path())
	}
	if bb.Spilled() {
		t.Errorf("Spilled() = true, want false")
	}
}

func TestBoundedBuffer_Close_NoSpill(t *testing.T) {
	t.Parallel()

	bb := NewBoundedBuffer(100)
	_, _ = bb.Write([]byte("hi"))
	if err := bb.Close(); err != nil {
		t.Errorf("Close on non-spilled buffer: %v", err)
	}
}

func TestBoundedBuffer_Close_AfterSpill(t *testing.T) {
	t.Parallel()

	bb := NewBoundedBuffer(10)
	_, _ = bb.Write([]byte(strings.Repeat("a", 100)))
	path := bb.Path()
	t.Cleanup(func() { os.Remove(path) })

	if err := bb.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	// After close, Path still returns the path (caller uses it
	// for the LLM-facing message), but the file is closed.
	if bb.Path() != path {
		t.Errorf("Path() = %q, want %q", bb.Path(), path)
	}
}

func TestBoundedBuffer_DefaultCapWhenZero(t *testing.T) {
	t.Parallel()

	bb := NewBoundedBuffer(0)
	// Write a small amount; should not spill.
	_, _ = bb.Write([]byte("hello"))
	if bb.Spilled() {
		t.Errorf("Spilled() = true, want false for small input")
	}
	// The default cap (50_000) should be applied.
	if bb.cap != frameworkDefaultTailCap {
		t.Errorf("cap = %d, want %d", bb.cap, frameworkDefaultTailCap)
	}
}

func TestBoundedBuffer_Bytes(t *testing.T) {
	t.Parallel()

	bb := NewBoundedBuffer(100)
	_, _ = bb.Write([]byte("hello"))
	got := bb.Bytes()
	if string(got) != "hello" {
		t.Errorf("Bytes() = %q, want %q", got, "hello")
	}
	// Modifying the returned slice should not affect the buffer.
	got[0] = 'X'
	if bb.String() != "hello" {
		t.Errorf("buffer mutated through Bytes() return value")
	}
}
