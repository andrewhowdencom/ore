package tui

import (
	"bytes"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogBuffer_SingleWriteAndFlush(t *testing.T) {
	buf := NewLogBuffer()
	_, err := buf.Write([]byte("hello world\n"))
	require.NoError(t, err)

	var out bytes.Buffer
	err = buf.FlushTo(&out)
	require.NoError(t, err)
	assert.Equal(t, "hello world\n", out.String())
}

func TestLogBuffer_MultipleWritesThenFlush(t *testing.T) {
	buf := NewLogBuffer()
	_, _ = buf.Write([]byte("line1\n"))
	_, _ = buf.Write([]byte("line2\n"))
	_, _ = buf.Write([]byte("line3\n"))

	var out bytes.Buffer
	require.NoError(t, buf.FlushTo(&out))
	assert.Equal(t, "line1\nline2\nline3\n", out.String())
}

func TestLogBuffer_FlushClearsBuffer(t *testing.T) {
	buf := NewLogBuffer()
	_, _ = buf.Write([]byte("data"))

	var out bytes.Buffer
	require.NoError(t, buf.FlushTo(&out))
	assert.Equal(t, "data", out.String())

	// Second flush should be a no-op.
	out.Reset()
	require.NoError(t, buf.FlushTo(&out))
	assert.Empty(t, out.String())
}

func TestLogBuffer_ConcurrentWrites(t *testing.T) {
	buf := NewLogBuffer()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = buf.Write([]byte("x"))
		}()
	}
	wg.Wait()

	var out bytes.Buffer
	require.NoError(t, buf.FlushTo(&out))
	assert.Equal(t, 100, len(out.String()))
	assert.True(t, strings.Repeat("x", 100) == out.String())
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) {
	return 0, io.ErrShortWrite
}

func TestLogBuffer_FlushToError(t *testing.T) {
	buf := NewLogBuffer()
	_, _ = buf.Write([]byte("will fail"))

	err := buf.FlushTo(failWriter{})
	require.Error(t, err)
	// Buffer is reset even on error.
	assert.Equal(t, 0, buf.buf.Len())
}
