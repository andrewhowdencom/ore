package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

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

	tidy := exec.Command("go", "mod", "tidy")
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

	build := exec.Command("go", "build", "-o", outputPath, ".")
	build.Dir = tmpDir
	if out, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("go build: %w\n%s", err, out)
	}

	return nil
}
