package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// execCommand is a testable wrapper around exec.Command.
var execCommand = exec.Command

// Build generates a temporary Go module from blueprint, runs go mod tidy,
// and compiles a binary at outputPath using the local ore module.
// If outputPath is relative it is resolved against the current working
// directory before compilation.
func Build(blueprint *Blueprint, oreModulePath string, outputPath string) error {
	tmpDir, err := os.MkdirTemp("", "forge-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	if err := Generate(blueprint, oreModulePath, tmpDir); err != nil {
		return fmt.Errorf("generate: %w", err)
	}

	// Resolve external modules (not under github.com/andrewhowdencom/ore/).
	orePrefix := "github.com/andrewhowdencom/ore"
	seen := make(map[string]struct{})
	for _, c := range blueprint.Conduits {
		if strings.HasPrefix(c.Module, orePrefix+"/") {
			continue
		}
		if _, ok := seen[c.Module]; ok {
			continue
		}
		seen[c.Module] = struct{}{}
		get := execCommand("go", "get", c.Module)
		get.Dir = tmpDir
		if out, err := get.CombinedOutput(); err != nil {
			return fmt.Errorf("go get %s: %w\n%s", c.Module, err, out)
		}
	}
	for _, h := range blueprint.Handlers {
		if strings.HasPrefix(h.Module, orePrefix+"/") {
			continue
		}
		if _, ok := seen[h.Module]; ok {
			continue
		}
		seen[h.Module] = struct{}{}
		get := execCommand("go", "get", h.Module)
		get.Dir = tmpDir
		if out, err := get.CombinedOutput(); err != nil {
			return fmt.Errorf("go get %s: %w\n%s", h.Module, err, out)
		}
	}

	tidy := execCommand("go", "mod", "tidy")
	tidy.Dir = tmpDir
	if out, err := tidy.CombinedOutput(); err != nil {
		return fmt.Errorf("go mod tidy: %w\n%s", err, out)
	}

	if !filepath.IsAbs(outputPath) {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}
		outputPath = filepath.Join(cwd, outputPath)
	}

	build := execCommand("go", "build", "-o", outputPath, ".")
	build.Dir = tmpDir
	if out, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("go build: %w\n%s", err, out)
	}

	return nil
}


