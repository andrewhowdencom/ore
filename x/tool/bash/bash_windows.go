//go:build windows

package bash

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
)

// runCommand executes cmd and captures stdout and stderr. On Windows,
// exec.CommandContext already handles process termination through job
// objects, so no additional process-group management is needed.
func runCommand(cmd *exec.Cmd, ctx context.Context, timeout int) (stdout, stderr string, err error) {
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return stdoutBuf.String(), stderrBuf.String(), fmt.Errorf("command timed out after %d seconds: %w", timeout, ctx.Err())
		}
		return stdoutBuf.String(), stderrBuf.String(), err
	}

	return stdoutBuf.String(), stderrBuf.String(), nil
}