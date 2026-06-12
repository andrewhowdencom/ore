package bash

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/tool"
	"github.com/andrewhowdencom/ore/x/tool/truncate"
)

// Compile-time type check.
var _ tool.ToolFunc = Bash

// Bash executes a shell command and returns its stdout, stderr, and
// exit code. Output is captured by a streaming, bounded accumulator
// (BoundedBuffer) that retains a rolling 2*frameworkDefaultTailCap
// tail in memory and spills the full byte stream to a temp file
// when the cap is exceeded. The temp file path is included in the
// result so the LLM can read the full output via read_file.
//
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

	// Delegate to ExecSandbox if available. The exec sandbox
	// runs the command in its own environment; it returns strings
	// (not bounded buffers) and may not support streaming. We
	// wrap the result in a Result with no spilling.
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

	stdout, stderr, stdoutPath, stderrPath, err := runCommand(cmd, ctx, timeout)
	result := &Result{
		Stdout:     stdout,
		Stderr:     stderr,
		ExitCode:   0,
		StdoutPath: stdoutPath,
		StderrPath: stderrPath,
	}

	// Apply framework-default truncation to the result. The
	// tool's Format declaration controls the cap; we honor it
	// here so the Result's Stdout/Stderr are already bounded
	// (and MarshalLLM has the recovery hint to point to the
	// temp file). The framework handler additionally consults
	// the same Format, but the LLMRenderer opt-out in Result
	// means handler-level truncation is bypassed for this tool.
	result.applyTruncation()

	if err != nil {
		if ctx.Err() != nil {
			return result, fmt.Errorf("command timed out after %d seconds: %w", timeout, ctx.Err())
		}

		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			result.ExitCode = exitErr.ExitCode()
			return result, fmt.Errorf("command exited with code %d", exitErr.ExitCode())
		}

		return result, fmt.Errorf("command execution failed: %w", err)
	}

	return result, nil
}

// applyTruncation runs the framework's truncator over the
// Result's stdout and stderr, replacing each with the bounded
// tail and recording the Truncation metadata on the result. The
// StdoutPath / StderrPath fields are preserved (they were set by
// runCommand) so the recovery hint in MarshalLLM can reference
// them.
func (r *Result) applyTruncation() {
	cfg := BashTool.Format.ResolvedTruncateConfig()
	style := BashTool.Format.Style
	if style == 0 {
		style = tool.StyleTail
	}

	stdoutOut, stdoutMeta := truncate.Truncate(r.Stdout, cfg, style)
	r.Stdout = stdoutOut
	if stdoutMeta.Truncated() {
		if r.Truncation == nil {
			r.Truncation = &artifact.Truncation{}
		}
		// Track the largest (most truncated) metadata so the
		// LLM sees the worst case. In practice stdout and
		// stderr are usually not both truncated.
		if stdoutMeta.OriginalBytes > r.Truncation.OriginalBytes {
			r.Truncation.OriginalBytes = stdoutMeta.OriginalBytes
			r.Truncation.OriginalLines = stdoutMeta.OriginalLines
			r.Truncation.ShownBytes = stdoutMeta.ShownBytes
			r.Truncation.ShownLines = stdoutMeta.ShownLines
			r.Truncation.Style = stdoutMeta.Style
		}
	}

	stderrOut, stderrMeta := truncate.Truncate(r.Stderr, cfg, style)
	r.Stderr = stderrOut
	if stderrMeta.Truncated() {
		if r.Truncation == nil {
			r.Truncation = &artifact.Truncation{}
		}
		if stderrMeta.OriginalBytes > r.Truncation.OriginalBytes {
			r.Truncation.OriginalBytes = stderrMeta.OriginalBytes
			r.Truncation.OriginalLines = stderrMeta.OriginalLines
			r.Truncation.ShownBytes = stderrMeta.ShownBytes
			r.Truncation.ShownLines = stderrMeta.ShownLines
			r.Truncation.Style = stderrMeta.Style
		}
	}
}

// BashTool is the tool.Tool descriptor for Bash.
var BashTool = tool.Tool{
	Name: "bash",
	Description: "Execute a shell command. Returns stdout, stderr, and exit code.\n\n" +
		"Output limits: stdout and stderr are each captured by a streaming, " +
		"bounded-memory accumulator. When a stream exceeds the cap, the full " +
		"output is written to a temp file (path included in the result) and " +
		"only the tail is returned.\n\n" +
		"Recovery: when truncation occurs, the result includes the temp file " +
		"path. Use read_file on the path to read the full output, or use " +
		"grep/tail/head on it to extract the relevant lines.",
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
	DisplayHint: BashDisplayHint,
	Format: tool.Format{
		// Apply framework default truncation to the result. The
		// BoundedBuffer already bounds in-memory retention; this
		// truncator bounds the LLM-facing string so that
		// the per-turn cost from a multi-MB tool result is
		// predictable. The temp file path is the recovery
		// channel; the LLM can use read_file to read more.
		Truncate: tool.TruncateConfig{
			MaxBytes: 50_000,
			MaxLines: 2_000,
		},
	},
}

// bashDisplay renders a bash tool call as a Markdown code block.
type bashDisplay struct {
	Command string
}

func (b bashDisplay) MarshalMarkdown() string {
	return "```bash\n$ " + b.Command + "\n```"
}

// BashDisplayHint is the display-hint formatter for the bash tool.
func BashDisplayHint(args map[string]any) any {
	cmd := toString(args["command"])
	if cmd == "" {
		return nil
	}
	return bashDisplay{Command: cmd}
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


