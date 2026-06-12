//go:build !windows

package bash

import (
	"context"
	"fmt"
	"os/exec"
	"syscall"
)

// runCommand starts cmd and waits for it to finish, or kills the
// entire process group when ctx is cancelled. This ensures child
// processes (e.g. spawned by sh -c) are also terminated on
// timeout.
//
// Output is captured by BoundedBuffer, which retains a rolling
// 2*frameworkDefaultTailCap tail in memory and spills the full
// byte stream to a temp file when the cap is exceeded. This bounds
// the host process's heap regardless of the subprocess's output
// size; a multi-GB `dd if=/dev/zero` no longer allocates the full
// output in memory.
//
// The returned strings are the bounded tail. The temp file paths
// are returned separately so the caller can include them in the
// tool result's recovery hint.
func runCommand(cmd *exec.Cmd, ctx context.Context, timeout int) (stdout, stderr, stdoutPath, stderrPath string, err error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdoutBuf := NewBoundedBuffer(frameworkDefaultTailCap)
	stderrBuf := NewBoundedBuffer(frameworkDefaultTailCap)
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf
	// Close temp files when the function returns, so the
	// processes holding the file descriptors release them. The
	// files themselves are kept on disk (the bash tool uses the
	// path in the LLM-facing message).
	defer func() {
		_ = stdoutBuf.Close()
		_ = stderrBuf.Close()
	}()

	if startErr := cmd.Start(); startErr != nil {
		return "", "", "", "", fmt.Errorf("failed to start command: %w", startErr)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case waitErr := <-done:
		if ctx.Err() != nil && waitErr != nil {
			return stdoutBuf.String(), stderrBuf.String(), stdoutBuf.Path(), stderrBuf.Path(),
				fmt.Errorf("command timed out after %d seconds: %w", timeout, ctx.Err())
		}
		return stdoutBuf.String(), stderrBuf.String(), stdoutBuf.Path(), stderrBuf.Path(), waitErr
	case <-ctx.Done():
		// Kill the process group (negative PID) so all children
		// are terminated.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-done
		return "", "", "", "",
			fmt.Errorf("command timed out after %d seconds: %w", timeout, ctx.Err())
	}
}
