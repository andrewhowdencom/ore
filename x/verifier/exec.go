package verifier

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"time"

	"github.com/andrewhowdencom/ore/state"
)

// ExecVerifier runs a shell command and maps the exit code to a
// VerificationResult. Exit code 0 is Pass, non-zero is Fail, and
// execution failure (e.g., command not found) is Error.
type ExecVerifier struct {
	Name    string
	Command string
	Args    []string
	Dir     string
	Timeout time.Duration
}

var _ Verifier = (*ExecVerifier)(nil)

// Verify runs the configured command and returns a VerificationResult
// based on the exit code.
func (e *ExecVerifier) Verify(ctx context.Context, st state.State) (VerificationResult, error) {
	if e.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, e.Command, e.Args...)
	if e.Dir != "" {
		cmd.Dir = e.Dir
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		// If the context was cancelled (timeout or explicit cancellation),
		// treat as Error rather than Fail.
		if ctx.Err() != nil {
			return VerificationResult{
				Name:   e.Name,
				Status: VerificationError,
				Report: string(out),
			}, fmt.Errorf("exec verifier %q cancelled: %w", e.Name, ctx.Err())
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// Non-zero exit code = Fail.
			return VerificationResult{
				Name:   e.Name,
				Status: VerificationFail,
				Report: string(out),
			}, nil
		}
		// Other error (command not found, etc.) = Error.
		return VerificationResult{
			Name:   e.Name,
			Status: VerificationError,
			Report: string(out),
		}, fmt.Errorf("exec verifier %q failed: %w", e.Name, err)
	}

	return VerificationResult{
		Name:   e.Name,
		Status: VerificationPass,
		Report: string(out),
	}, nil
}
