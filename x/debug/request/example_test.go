package request_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/x/debug/request"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExample_EndToEndCapture walks a single armed round trip from
// the dumper's perspective, asserting that the resulting capture
// file contains the request and response sections in the agreed
// format. This is the example shown in the package's doc.go.
func TestExample_EndToEndCapture(t *testing.T) {
	t.Parallel()

	// Set up a server that records what the wire sent and replies
	// with a fixed body.
	var serverSaw atomic.Value // string
	serverSaw.Store("")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		serverSaw.Store(string(b))
		w.Header().Set("X-Echo", "ok")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("hello back"))
	}))
	t.Cleanup(server.Close)

	// Construct the dumper with a temp output dir so the test
	// leaves no files behind. t.TempDir() returns a fresh
	// subdirectory on each call, so we capture the result and
	// reuse it.
	outputDir := t.TempDir()
	d := request.New("example", request.WithOutputDir(outputDir))
	t.Cleanup(func() { _ = d.Close() })

	// Wrap a single *http.Client with the dumper. The same
	// client could be passed to anthropic.New and openai.New;
	// the capture is vendor-agnostic.
	client := &http.Client{Transport: d.Wrap(http.DefaultTransport)}

	// Arm the dumper for a thread.
	const threadID = "example-thread"
	d.Enable(threadID)

	// Send a request through the wrapped client. The context
	// carries the thread ID (the loop does this in production).
	req, err := http.NewRequestWithContext(
		loop.WithThreadID(context.Background(), threadID),
		http.MethodPost, server.URL+"/v1/echo",
		strings.NewReader("hello forward"),
	)
	require.NoError(t, err)

	resp, err := client.Do(req)
	require.NoError(t, err)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	_ = resp.Body.Close()

	// The wire still sees the original request and the original
	// response.
	assert.Equal(t, "hello forward", serverSaw.Load().(string))
	assert.Equal(t, "hello back", string(body))

	// The dumper disarmed itself.
	assert.False(t, d.Armed(threadID), "dumper should auto-disarm after the capture")

	// The capture file exists in the output dir we configured.
	// We don't know the exact timestamp in the filename, so we
	// list the directory and pick the one matching the convention.
	entries, err := os.ReadDir(outputDir)
	require.NoError(t, err)
	var found string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "example.request.") && strings.HasSuffix(e.Name(), ".log") {
			found = e.Name()
			break
		}
	}
	require.NotEmpty(t, found, "expected a capture file matching the convention")

	contents, err := os.ReadFile(outputDir + "/" + found)
	require.NoError(t, err)
	got := string(contents)
	assert.Contains(t, got, "=== REQUEST ===")
	assert.Contains(t, got, "POST /v1/echo")
	assert.Contains(t, got, "hello forward")
	assert.Contains(t, got, "=== RESPONSE ===")
	assert.Contains(t, got, "200 OK")
	assert.Contains(t, got, "X-Echo: ok")
	assert.Contains(t, got, "hello back")
}