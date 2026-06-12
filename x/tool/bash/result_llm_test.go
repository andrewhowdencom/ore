package bash

import (
	"strings"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
)

func TestResult_MarshalLLM_NoTruncation(t *testing.T) {
	t.Parallel()

	r := &Result{
		Stdout:   "hello world\n",
		Stderr:   "",
		ExitCode: 0,
	}

	got := r.MarshalLLM()
	if !strings.Contains(got, "**stdout**") {
		t.Errorf("expected stdout section in:\n%s", got)
	}
	if !strings.Contains(got, "hello world") {
		t.Errorf("expected stdout content in:\n%s", got)
	}
	if !strings.Contains(got, "**exit code:** 0") {
		t.Errorf("expected exit code in:\n%s", got)
	}
	if strings.Contains(got, "truncated") {
		t.Errorf("did not expect truncation notice, got:\n%s", got)
	}
	if strings.Contains(got, "temp file") {
		t.Errorf("did not expect temp file path, got:\n%s", got)
	}
}

func TestResult_MarshalLLM_Truncated_WithStdoutPath(t *testing.T) {
	t.Parallel()

	r := &Result{
		Stdout:     "...truncated tail...",
		Stderr:     "",
		ExitCode:   0,
		StdoutPath: "/tmp/ore-bash-abc.log",
		Truncation: &artifact.Truncation{
			OriginalBytes: 10_000_000,
			OriginalLines: 100_000,
			ShownBytes:    50_000,
			ShownLines:    2_000,
			Style:         "tail",
		},
	}

	got := r.MarshalLLM()
	if !strings.Contains(got, "truncated") {
		t.Errorf("expected truncation notice, got:\n%s", got)
	}
	if !strings.Contains(got, "/tmp/ore-bash-abc.log") {
		t.Errorf("expected stdout temp file path, got:\n%s", got)
	}
	if !strings.Contains(got, "Recovery:") {
		t.Errorf("expected recovery hint, got:\n%s", got)
	}
	if !strings.Contains(got, "read_file") {
		t.Errorf("expected read_file hint, got:\n%s", got)
	}
}

func TestResult_MarshalLLM_Truncated_WithBothPaths(t *testing.T) {
	t.Parallel()

	r := &Result{
		Stdout:     "out tail",
		Stderr:     "err tail",
		ExitCode:   1,
		StdoutPath: "/tmp/ore-bash-stdout.log",
		StderrPath: "/tmp/ore-bash-stderr.log",
		Truncation: &artifact.Truncation{OriginalBytes: 1000, ShownBytes: 100},
	}

	got := r.MarshalLLM()
	if !strings.Contains(got, "/tmp/ore-bash-stdout.log") {
		t.Errorf("expected stdout path, got:\n%s", got)
	}
	if !strings.Contains(got, "/tmp/ore-bash-stderr.log") {
		t.Errorf("expected stderr path, got:\n%s", got)
	}
	if !strings.Contains(got, "stdout:") || !strings.Contains(got, "stderr:") {
		t.Errorf("expected both labels in recovery hint, got:\n%s", got)
	}
}

func TestResult_ImplementsLLMRenderer(t *testing.T) {
	t.Parallel()

	var _ artifact.LLMRenderer = (*Result)(nil)
}

func TestResult_ApplyTruncation_NoOp(t *testing.T) {
	t.Parallel()

	// Small output should pass through unchanged.
	r := &Result{Stdout: "hi", Stderr: "err", ExitCode: 0}
	r.applyTruncation()
	if r.Stdout != "hi" {
		t.Errorf("Stdout = %q, want %q", r.Stdout, "hi")
	}
	if r.Stderr != "err" {
		t.Errorf("Stderr = %q, want %q", r.Stderr, "err")
	}
	if r.Truncation != nil {
		t.Errorf("Truncation should be nil for small output, got %+v", r.Truncation)
	}
}

func TestResult_ApplyTruncation_TruncatesLargeStdout(t *testing.T) {
	t.Parallel()

	// 200 KB of 'a' characters; default cap is 50 KB.
	big := strings.Repeat("a", 200_000)
	r := &Result{Stdout: big, Stderr: "", ExitCode: 0}
	r.applyTruncation()
	if len(r.Stdout) > 50_000 {
		t.Errorf("Stdout should be ≤ 50 KB after truncation, got %d bytes", len(r.Stdout))
	}
	if r.Truncation == nil {
		t.Fatal("Truncation should be non-nil for large output")
	}
	if r.Truncation.OriginalBytes < 200_000 {
		t.Errorf("OriginalBytes = %d, want ≥ 200000", r.Truncation.OriginalBytes)
	}
}

func TestResult_ApplyTruncation_TracksLargest(t *testing.T) {
	t.Parallel()

	// Both stdout and stderr are large; the metadata should
	// reflect the more-truncated one.
	r := &Result{
		Stdout: strings.Repeat("a", 100_000),
		Stderr: strings.Repeat("b", 500_000),
		ExitCode: 0,
	}
	r.applyTruncation()
	if r.Truncation == nil {
		t.Fatal("Truncation should be non-nil")
	}
	// The largest OriginalBytes wins.
	if r.Truncation.OriginalBytes < 500_000 {
		t.Errorf("OriginalBytes = %d, want ≥ 500000 (stderr was larger)", r.Truncation.OriginalBytes)
	}
}
