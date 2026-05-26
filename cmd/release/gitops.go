package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func currentBranch(root string) (string, error) {
	out, err := exec.Command("git", "-C", root, "rev-parse", "--abbrev-ref", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("get branch: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func runGoModTidy(root string, m Module) error {
	dir := filepath.Join(root, m.Dir)
	cmd := exec.Command("go", "mod", "tidy")
	cmd.Dir = dir

	// Ensure GOWORK is off so the module is validated independently.
	env := os.Environ()
	var filtered []string
	for _, e := range env {
		if !strings.HasPrefix(e, "GOWORK=") {
			filtered = append(filtered, e)
		}
	}
	cmd.Env = append(filtered, "GOWORK=off")

	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("go mod tidy in %s: %w\n%s", m.Dir, err, out)
	}
	return nil
}

// hasStagedChanges reports whether the Git index in root contains changes
// staged relative to HEAD.
func hasStagedChanges(root string) (bool, error) {
	if err := exec.Command("git", "-C", root, "diff", "--cached", "--quiet").Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return true, nil
		}
		code := -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		}
		return false, fmt.Errorf("git diff --cached (exit %d): %w", code, err)
	}
	return false, nil
}

// stageAndCommit stages go.mod and go.sum (if present) and creates a Git commit
// with the supplied release message. If no files change after staging, the commit
// is silently skipped so the caller can proceed to tag creation on the current HEAD.
// This handles modules (e.g. the root module) that have no ore-internal dependency
// bumps and therefore no go.mod/go.sum changes. See issue #205.
func stageAndCommit(root string, m Module, version string, dryRun bool) error {
	relGomod := filepath.Join(m.Dir, "go.mod")
	if m.Dir == "." {
		relGomod = "go.mod"
	}
	files := []string{relGomod}

	relGosum := filepath.Join(m.Dir, "go.sum")
	if m.Dir == "." {
		relGosum = "go.sum"
	}
	if _, err := os.Stat(filepath.Join(root, relGosum)); err == nil {
		files = append(files, relGosum)
	}

	msg := fmt.Sprintf("chore(release): bump %s to %s", m.Path, version)

	if dryRun {
		fmt.Printf("[dry-run] git add %s\n", strings.Join(files, " "))
		fmt.Printf("[dry-run] git commit -m %q\n", msg)
		fmt.Printf("[dry-run] (commit skipped if no staged changes)\n")
		return nil
	}

	args := append([]string{"-C", root, "add"}, files...)
	cmd := exec.Command("git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %w\n%s", err, out)
	}

	// Issue #205: git commit would fail when no files changed after git add.
	// Check whether there are actually staged changes before committing.
	hasChanges, err := hasStagedChanges(root)
	if err != nil {
		return err
	}
	if !hasChanges {
		fmt.Printf("No changes to commit for %s %s; skipping commit.\n", m.Path, version)
		return nil
	}

	cmd = exec.Command("git", "-C", root, "commit", "-m", msg)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit: %w\n%s", err, out)
	}

	return nil
}

func createTag(root string, m Module, version string, dryRun bool) error {
	var tag string
	if m.Dir == "." {
		tag = version
	} else {
		tag = m.Dir + "/" + version
	}

	msg := fmt.Sprintf("Release %s %s", m.Path, version)

	if dryRun {
		fmt.Printf("[dry-run] git tag -a %s -m %q\n", tag, msg)
		return nil
	}

	cmd := exec.Command("git", "-C", root, "tag", "-a", tag, "-m", msg)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git tag: %w\n%s", err, out)
	}

	return nil
}

func push(root string, dryRun bool) error {
	if dryRun {
		fmt.Printf("[dry-run] git push origin main\n")
		fmt.Printf("[dry-run] git push origin --tags\n")
		return nil
	}

	cmd := exec.Command("git", "-C", root, "push", "origin", "main")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git push main: %w\n%s", err, out)
	}

	cmd = exec.Command("git", "-C", root, "push", "origin", "--tags")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git push tags: %w\n%s", err, out)
	}

	return nil
}
