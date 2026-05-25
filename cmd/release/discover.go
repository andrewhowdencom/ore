package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Module represents a Go module discovered in the repository.
type Module struct {
	Path string // Module import path (e.g., github.com/andrewhowdencom/ore/x/provider/openai)
	Dir  string // Relative directory from repo root (e.g., x/provider/openai, or . for root)
}

// repoRoot returns the absolute path of the repository root using git.
func repoRoot() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("find repo root: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// discoverModules scans the repository for all go.mod files and returns the
// corresponding Module values. Hidden directories and vendor trees are skipped.
func discoverModules(root string) ([]Module, error) {
	var modules []Module
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			// Skip hidden directories (.git, .pi, .worktrees, etc.)
			if strings.HasPrefix(info.Name(), ".") && path != root {
				return filepath.SkipDir
			}
			// Skip vendor trees
			if info.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Base(path) != "go.mod" {
			return nil
		}
		dir := filepath.Dir(path)
		relDir, err := filepath.Rel(root, dir)
		if err != nil {
			return err
		}
		if relDir == "." {
			relDir = "."
		}
		modulePath, err := parseModulePath(path)
		if err != nil {
			return err
		}
		modules = append(modules, Module{
			Path: modulePath,
			Dir:  relDir,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Deterministic ordering for tests and stable output.
	sort.Slice(modules, func(i, j int) bool {
		return modules[i].Dir < modules[j].Dir
	})
	return modules, nil
}

// parseModulePath reads the module directive from a go.mod file.
func parseModulePath(gomod string) (string, error) {
	f, err := os.Open(gomod)
	if err != nil {
		return "", err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "module ") {
			path := strings.TrimSpace(strings.TrimPrefix(line, "module "))
			// Strip trailing comments.
			if idx := strings.Index(path, "//"); idx >= 0 {
				path = strings.TrimSpace(path[:idx])
			}
			return path, nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("no module directive found in %s", gomod)
}
