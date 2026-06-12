package bash

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// skipIfSmallCommandSkips is true when the test platform does not
// support generating large outputs cheaply. Today this is false on
// all supported platforms, but the seam is here for future
// portability concerns.
func integrationSkip() bool {
	if os.Getenv("ORE_BASH_SKIP_LARGE_OUTPUT_TEST") != "" {
		return true
	}
	return false
}

// TestBash_LargeOutput_BoundedResult exercises the streaming
// accumulator and the truncator on a command that produces
// multi-megabyte output. The test asserts:
//
//  1. The function returns within a short timeout.
//  2. Result.Truncation is non-nil and reports the original
//     size was at least 50 MB.
//  3. Result.Stdout (the LLM-facing string) is bounded to ≤ 50 KB.
//  4. Result.StdoutPath is set and points to a file containing
//     the full output.
//
// The test uses `head -c <bytes> /dev/zero | base64` to produce a
// deterministic multi-megabyte byte stream without depending on
// the host filesystem layout. On platforms where head / base64
// are not available, the test is skipped.
func TestBash_LargeOutput_BoundedResult(t *testing.T) {
	t.Parallel()

	if integrationSkip() {
		t.Skip("ORE_BASH_SKIP_LARGE_OUTPUT_TEST is set")
	}

	if _, err := exec.LookPath("head"); err != nil {
		t.Skip("head command not available")
	}
	if _, err := exec.LookPath("base64"); err != nil {
		t.Skip("base64 command not available")
	}

	// 60 MB of zeros, base64-encoded. base64 inflates by 4/3,
	// so we get ~80 MB of output.
	const sizeBytes = 60 * 1024 * 1024
	command := fmt.Sprintf("head -c %d /dev/zero | base64", sizeBytes)

	dir := t.TempDir()
	sb := &testFileSandbox{dir: dir}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := Bash(ctx, sb, map[string]any{
		"command":         command,
		"timeout_seconds": 30,
	})
	require.NoError(t, err, "Bash should succeed on a normal command")
	r, ok := result.(*Result)
	require.True(t, ok, "result should be *Result")

	// Truncation metadata: original was at least 50 MB.
	require.NotNil(t, r.Truncation, "Truncation should be non-nil for 60 MB output")
	assert.GreaterOrEqual(t, r.Truncation.OriginalBytes, 50*1024*1024,
		"OriginalBytes should reflect the multi-MB input")
	assert.LessOrEqual(t, len(r.Stdout), 50_000,
		"Stdout (the LLM-facing string) should be bounded to ≤ 50 KB")

	// Temp file should be set and contain the full output.
	require.NotEmpty(t, r.StdoutPath, "StdoutPath should be set on truncation")
	t.Cleanup(func() { os.Remove(r.StdoutPath) })
	contents, err := os.ReadFile(r.StdoutPath)
	require.NoError(t, err, "temp file should be readable")
	assert.GreaterOrEqual(t, len(contents), 50*1024*1024,
		"temp file should contain the full output")
}

// TestBash_LargeOutput_BoundedHeap exercises the same scenario
// but also asserts that the host process's heap growth is
// bounded. This catches the original bug class (unbounded
// bytes.Buffer) directly.
//
// The test is opt-in because heap measurement is inherently
// noisy in shared CI environments: the Go runtime's allocator
// can have residual allocation from prior tests in the same
// `go test` run, and the growth metric reflects that noise.
// Set ORE_BASH_RUN_HEAP_TEST=1 to enable. The unbounded
// bytes.Buffer case (the bug we are guarding against) would
// grow by the full 60 MB subprocess output, far above the
// 50 MB threshold even with measurement noise.
func TestBash_LargeOutput_BoundedHeap(t *testing.T) {
	t.Parallel()

	if testing.Short() {
		t.Skip("heap-growth check is skipped in -short mode")
	}
	if os.Getenv("ORE_BASH_RUN_HEAP_TEST") == "" {
		t.Skip("ORE_BASH_RUN_HEAP_TEST is not set; heap-growth check is opt-in")
	}
	if _, err := exec.LookPath("head"); err != nil {
		t.Skip("head command not available")
	}
	if _, err := exec.LookPath("base64"); err != nil {
		t.Skip("base64 command not available")
	}

	const sizeBytes = 60 * 1024 * 1024
	command := fmt.Sprintf("head -c %d /dev/zero | base64", sizeBytes)

	dir := t.TempDir()
	sb := &testFileSandbox{dir: dir}

	// Force a GC cycle before measuring so the baseline heap is
	// the live-set, not including finalized garbage. We do two
	// cycles to give finalizers a chance to run.
	runtime.GC()
	runtime.GC()
	runtime.Gosched()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	result, err := Bash(ctx, sb, map[string]any{
		"command":         command,
		"timeout_seconds": 30,
	})
	require.NoError(t, err)
	r := result.(*Result)
	require.NotNil(t, r.Truncation)
	t.Cleanup(func() {
		if r.StdoutPath != "" {
			os.Remove(r.StdoutPath)
		}
	})

	runtime.GC()
	runtime.GC()
	runtime.Gosched()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)

	// Heap growth should be bounded by the BoundedBuffer's
	// 2*frameworkDefaultTailCap (100 KB) plus a small margin
	// for the rest of the test's allocations. 50 MB is a
	// generous upper bound that is still orders of magnitude
	// less than the unbounded-bytes.Buffer case (which would
	// grow by the full 60 MB subprocess output). The wide
	// margin accommodates heap measurement noise from the Go
	// runtime allocator under shared test conditions.
	heapGrowth := int64(after.HeapInuse) - int64(before.HeapInuse)
	assert.Less(t, heapGrowth, int64(50*1024*1024),
		"heap growth should be bounded (<50 MB) for a 60 MB subprocess output; got %d bytes", heapGrowth)
}

// TestBash_LargeOutput_MarshalLLM_IncludesHint ensures the
// recovery hint in MarshalLLM names the temp file path and
// tells the model how to read more.
func TestBash_LargeOutput_MarshalLLM_IncludesHint(t *testing.T) {
	t.Parallel()

	if _, err := exec.LookPath("head"); err != nil {
		t.Skip("head command not available")
	}
	if _, err := exec.LookPath("base64"); err != nil {
		t.Skip("base64 command not available")
	}

	const sizeBytes = 60 * 1024 * 1024
	command := fmt.Sprintf("head -c %d /dev/zero | base64", sizeBytes)

	dir := t.TempDir()
	sb := &testFileSandbox{dir: dir}

	result, err := Bash(context.Background(), sb, map[string]any{
		"command":         command,
		"timeout_seconds": 30,
	})
	require.NoError(t, err)
	r := result.(*Result)
	t.Cleanup(func() {
		if r.StdoutPath != "" {
			os.Remove(r.StdoutPath)
		}
	})

	got := r.MarshalLLM()
	assert.True(t,
		strings.Contains(got, "truncated"),
		"MarshalLLM should mention truncation",
	)
	assert.True(t,
		strings.Contains(got, r.StdoutPath),
		"MarshalLLM should reference the temp file path",
	)
	assert.True(t,
		strings.Contains(got, "read_file"),
		"MarshalLLM should recommend read_file",
	)

	// Compile-time assertion: *Result implements LLMRenderer.
	var _ artifact.LLMRenderer = (*Result)(nil)
}
