package bash

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/tool"
)

// Compile-time type check.
var _ tool.ToolFunc = Bash

// Bash executes a shell command and returns its stdout, stderr, and exit code.
// Parameters:
//   - command (string, required): the shell command to execute.
//   - working_directory (string, optional): the directory to execute the command in.
//   - timeout_seconds (number, optional, default 30): maximum execution time in seconds.
func Bash(ctx context.Context, sb tool.Sandbox, args map[string]any) (any, error) {
	if sb == nil {
		return nil, fmt.Errorf("sandbox required for bash tool")
	}

	command := toString(args["command"])
	if command == "" {
		return nil, fmt.Errorf("command is required")
	}

	workingDir := toString(args["working_directory"])

	timeout := toInt(args["timeout_seconds"], 30)
	if timeout <= 0 {
		timeout = 30
	}

	// Delegate to ExecSandbox if available.
	if execSb, ok := sb.(tool.ExecSandbox); ok {
		dir := workingDir
		if dir == "" {
			if fsb, ok := sb.(tool.FileSandbox); ok {
				dir = fsb.WorkingDirectory()
			}
		}
		stdout, stderr, exitCode, err := execSb.Run(ctx, command, dir, secondsToDuration(timeout))
		if err != nil {
			if exitCode != 0 {
				return &Result{
					Stdout:   stdout,
					Stderr:   stderr,
					ExitCode: exitCode,
				}, fmt.Errorf("command exited with code %d", exitCode)
			}
			return nil, fmt.Errorf("command execution failed: %w", err)
		}
		return &Result{
			Stdout:   stdout,
			Stderr:   stderr,
			ExitCode: 0,
		}, nil
	}

	// Fallback: use FileSandbox.WorkingDirectory() as default cwd.
	if fsb, ok := sb.(tool.FileSandbox); ok {
		if workingDir == "" {
			workingDir = fsb.WorkingDirectory()
		}
	}

	ctx, cancel := context.WithTimeout(ctx, secondsToDuration(timeout))
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(ctx, "cmd", "/C", command)
	} else {
		cmd = exec.CommandContext(ctx, "sh", "-c", command)
	}

	if workingDir != "" {
		cmd.Dir = workingDir
	}

	stdout, stderr, err := runCommand(cmd, ctx, timeout)

	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("command timed out after %d seconds: %w", timeout, ctx.Err())
		}

		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return &Result{
				Stdout:   stdout,
				Stderr:   stderr,
				ExitCode: exitErr.ExitCode(),
			}, fmt.Errorf("command exited with code %d", exitErr.ExitCode())
		}

		return nil, fmt.Errorf("command execution failed: %w", err)
	}

	return &Result{
		Stdout:   stdout,
		Stderr:   stderr,
		ExitCode: 0,
	}, nil
}

// BashTool is the provider.Tool descriptor for Bash.
var BashTool = provider.Tool{
	Name: "bash",
	Description: "Execute a shell command. Returns stdout, stderr, and exit code. " +
		"Use this to run builds, tests, package managers, git operations, and other shell tasks.",
	Schema: map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The shell command to execute.",
			},
			"working_directory": map[string]any{
				"type":        "string",
				"description": "The directory to execute the command in. Defaults to the current working directory.",
			},
			"timeout_seconds": map[string]any{
				"type":        "integer",
				"description": "Maximum execution time in seconds. Defaults to 30.",
				"default":     30,
			},
		},
		"required": []string{"command"},
	},
}

func secondsToDuration(s int) time.Duration {
	return time.Duration(s) * time.Second
}

// toString safely extracts a string value from a JSON-decoded argument.
func toString(v any) string {
	s, _ := v.(string)
	return s
}

// toInt safely extracts an integer from a JSON-decoded number (float64 or int)
// with a default value.
func toInt(v any, def int) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case float32:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	case uint:
		return int(n)
	case string:
		var i int
		_, err := fmt.Sscanf(n, "%d", &i)
		if err != nil {
			return def
		}
		return i
	}
	return def
}