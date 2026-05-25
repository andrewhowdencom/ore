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
		return nil
	}

	args := append([]string{"-C", root, "add"}, files...)
	cmd := exec.Command("git", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %w\n%s", err, out)
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
