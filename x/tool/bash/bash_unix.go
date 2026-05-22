//go:build !windows

package bash

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"syscall"
)

// runCommand starts cmd and waits for it to finish, or kills the entire
// process group when ctx is cancelled. This ensures child processes (e.g.
// spawned by sh -c) are also terminated on timeout.
func runCommand(cmd *exec.Cmd, ctx context.Context, timeout int) (stdout, stderr string, err error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return "", "", fmt.Errorf("failed to start command: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		if ctx.Err() != nil && err != nil {
			return stdoutBuf.String(), stderrBuf.String(), fmt.Errorf("command timed out after %d seconds: %w", timeout, ctx.Err())
		}
		return stdoutBuf.String(), stderrBuf.String(), err
	case <-ctx.Done():
		// Kill the process group (negative PID) so all children are terminated.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		return "", "", fmt.Errorf("command timed out after %d seconds: %w", timeout, ctx.Err())
	}
}