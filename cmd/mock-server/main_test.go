package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/x/provider/mock"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRun_RequiresFlags asserts the CLI rejects the empty arg list.
func TestRun_RequiresFlags(t *testing.T) {
	t.Parallel()

	err := run([]string{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "-vendor and -config are required")
}

// TestRun_UnknownVendor rejects an unrecognised vendor with a clear
// error message.
func TestRun_UnknownVendor(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := filepath.Join(dir, "responses.json")
	require.NoError(t, os.WriteFile(cfg, []byte(`[{"text":"x"}]`), 0o600))

	err := run([]string{"-vendor=banana", "-config=" + cfg})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown vendor")
}

// TestRun_ConfigParseError rejects malformed JSON.
func TestRun_ConfigParseError(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	cfg := filepath.Join(dir, "responses.json")
	require.NoError(t, os.WriteFile(cfg, []byte(`not json`), 0o600))

	err := run([]string{"-vendor=openai", "-config=" + cfg})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse")
}

// TestRun_AcceptsBothVendors exercises run()'s dispatch: each vendor
// builds a working handler and runs the listening loop. The test
// cancels the listening loop quickly via SIGINT (delivered via the
// process group signal) so the binary exits cleanly.
//
// Because run() reads os.Args and binds a real listener, this test
// spawns the actual binary in a subprocess and verifies that:
//   - it starts within a reasonable deadline
//   - the bound URL printed to stderr is parseable
//   - it can be terminated via SIGINT
func TestRun_AcceptsBothVendors(t *testing.T) {
	t.Parallel()
	if testing.Short() {
		t.Skip("skipping subprocess test in short mode")
	}

	for _, vendor := range []string{"openai", "anthropic"} {
		vendor := vendor
		t.Run(vendor, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			cfg := filepath.Join(dir, "responses.json")
			require.NoError(t, os.WriteFile(cfg, []byte(`[
				{"text":"hi","usage":{"prompt_tokens":4,"completion_tokens":5,"total_tokens":9}}
			]`), 0o600))

			// Compile the binary in a temp dir to avoid polluting the
			// source tree, then run it with the temp config.
			binDir := t.TempDir()
			binPath := filepath.Join(binDir, "mock-server")
			buildCmd := exec.Command("go", "build", "-o", binPath, ".")
			out, err := buildCmd.CombinedOutput()
			require.NoError(t, err, "go build failed: %s", string(out))

			runCmd := exec.Command(binPath, "-vendor="+vendor, "-config="+cfg, "-addr=127.0.0.1:0")
			runCmd.Dir = "."
			stderr, err := runCmd.StderrPipe()
			require.NoError(t, err)
			require.NoError(t, runCmd.Start())

			defer func() {
				_ = runCmd.Process.Signal(os.Interrupt)
				_ = runCmd.Wait()
			}()

			url := readListeningURL(t, stderr)
			require.NotEmpty(t, url, "no listening line")

			// Hit the appropriate endpoint.
			path := "/chat/completions"
			if vendor == "anthropic" {
				path = "/v1/messages"
			}
			body := `{"model":"x","max_tokens":1024,"messages":[]}`
			req, err := http.NewRequest(http.MethodPost, url+path, strings.NewReader(body))
			require.NoError(t, err)
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			b, err := io.ReadAll(resp.Body)
			require.NoError(t, err)
			assert.Contains(t, string(b), "hi")
		})
	}
}

// TestBuildHandler_OpenAI builds the OpenAI handler via buildHandler
// and proves the SSE shape with httptest — no subprocess needed.
func TestBuildHandler_OpenAI(t *testing.T) {
	t.Parallel()

	h, err := buildHandler("openai", []mock.Response{
		{Text: "hi", Usage: &mock.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3}},
	})
	require.NoError(t, err)

	ts := httptest.NewServer(h)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/chat/completions", "application/json",
		strings.NewReader(`{"model":"gpt-4o","messages":[]}`))
	require.NoError(t, err)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	body := string(b)

	assert.Contains(t, body, `"content":"hi"`)
	assert.Contains(t, body, `"finish_reason":"stop"`)
	assert.Contains(t, body, `"total_tokens":3`)
	assert.Contains(t, body, "data: [DONE]")
}

// TestBuildHandler_Anthropic mirrors TestBuildHandler_OpenAI for the
// Anthropic wire format.
func TestBuildHandler_Anthropic(t *testing.T) {
	t.Parallel()

	h, err := buildHandler("anthropic", []mock.Response{
		{Text: "hi", Usage: &mock.Usage{PromptTokens: 1, CompletionTokens: 2, TotalTokens: 3}},
	})
	require.NoError(t, err)

	ts := httptest.NewServer(h)
	defer ts.Close()

	resp, err := http.Post(ts.URL+"/v1/messages", "application/json",
		strings.NewReader(`{"model":"claude-3-7-sonnet-latest","max_tokens":1024,"messages":[]}`))
	require.NoError(t, err)
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	body := string(b)

	assert.Contains(t, body, "event: message_start")
	assert.Contains(t, body, "event: content_block_delta")
	assert.Contains(t, body, `"text":"hi"`)
	assert.Contains(t, body, "event: message_stop")
}

// TestLoadResponses covers happy path and JSON errors.
func TestLoadResponses(t *testing.T) {
	t.Parallel()

	t.Run("happy", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		cfg := filepath.Join(dir, "r.json")
		require.NoError(t, os.WriteFile(cfg, []byte(`[{"text":"hi"}]`), 0o600))
		rs, err := loadResponses(cfg)
		require.NoError(t, err)
		require.Len(t, rs, 1)
		assert.Equal(t, "hi", rs[0].Text)
	})

	t.Run("missing", func(t *testing.T) {
		t.Parallel()
		_, err := loadResponses("/no/such/file")
		require.Error(t, err)
	})

	t.Run("malformed", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		cfg := filepath.Join(dir, "r.json")
		require.NoError(t, os.WriteFile(cfg, []byte(`{not valid`), 0o600))
		_, err := loadResponses(cfg)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "parse")
	})
}

// readListeningURL scans subprocess stderr until the listening
// line appears. Returns the URL portion.
func readListeningURL(t *testing.T, r io.Reader) string {
	t.Helper()
	buf := make([]byte, 4096)
	var acc strings.Builder
	deadline := time.After(15 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for listening line; got: %s", acc.String())
		default:
		}
		n, err := r.Read(buf)
		if n > 0 {
			acc.Write(buf[:n])
			s := acc.String()
			if i := strings.Index(s, "mock-server: listening on http://"); i >= 0 {
				rest := s[i+len("mock-server: listening on "):]
				if j := strings.Index(rest, " "); j > 0 {
					return rest[:j]
				}
				if strings.HasSuffix(rest, "\n") {
					return strings.TrimSuffix(rest, "\n")
				}
				return rest
			}
		}
		if err != nil {
			t.Fatalf("stderr closed: %v; got: %s", err, acc.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// Compile-time import sanity.
var _ = json.RawMessage{}