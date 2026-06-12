//go:build windows

package bash

import (
	"context"
	"fmt"
	"os/exec"
)

// runCommand executes cmd and captures stdout and stderr via
// BoundedBuffer. On Windows, exec.CommandContext already handles
// process termination through job objects, so no additional
// process-group management is needed.
//
// The returned strings are the bounded tail. The temp file paths
// are returned separately so the caller can include them in the
// tool result's recovery hint.
func runCommand(cmd *exec.Cmd, ctx context.Context, timeout int) (stdout, stderr, stdoutPath, stderrPath string, err error) {
	stdoutBuf := NewBoundedBuffer(frameworkDefaultTailCap)
	stderrBuf := NewBoundedBuffer(frameworkDefaultTailCap)
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf
	defer func() {
		_ = stdoutBuf.Close()
		_ = stderrBuf.Close()
	}()

	if runErr := cmd.Run(); runErr != nil {
		if ctx.Err() != nil {
			return stdoutBuf.String(), stderrBuf.String(), stdoutBuf.Path(), stderrBuf.Path(),
				fmt.Errorf("command timed out after %d seconds: %w", timeout, ctx.Err())
		}
		return stdoutBuf.String(), stderrBuf.String(), stdoutBuf.Path(), stderrBuf.Path(), runErr
	}

	return stdoutBuf.String(), stderrBuf.String(), stdoutBuf.Path(), stderrBuf.Path(), nil
}
