package main

import (
	"fmt"
	"os"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	dryRun := false
	var remaining []string
	for _, a := range args {
		if a == "-dry-run" || a == "--dry-run" {
			dryRun = true
			continue
		}
		remaining = append(remaining, a)
	}

	if len(remaining) == 0 || remaining[0] == "--help" || remaining[0] == "-h" {
		usage()
		if len(remaining) == 0 {
			return fmt.Errorf("no command provided")
		}
		return nil
	}

	cmd := remaining[0]
	cmdArgs := remaining[1:]

	switch cmd {
	case "status":
		return runStatus(dryRun, cmdArgs)
	case "all":
		return runAll(dryRun, cmdArgs)
	default:
		return runRelease(cmd, dryRun, cmdArgs)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage: release [flags] <command>")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  status          Show pending module releases")
	fmt.Fprintln(os.Stderr, "  all             Release all modules with changes")
	fmt.Fprintln(os.Stderr, "  <module-path>   Release a specific module")
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Flags:")
	fmt.Fprintln(os.Stderr, "  -dry-run        Print actions without executing them")
}

