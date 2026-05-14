// Package main provides the forge CLI, a minimal tool that reads a YAML
// manifest and generates a compilable Go agent application.
//
// Usage:
//
//	go run ./cmd/forge -config forge.yaml
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(logger)

	if err := run(); err != nil {
		slog.Error("forge failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := flag.String("config", "forge.yaml", "path to manifest file")
	flag.Parse()
	return runWithPath(*configPath)
}

// runWithPath executes the forge pipeline for the manifest at configPath.
func runWithPath(configPath string) error {
	f, err := os.Open(configPath)
	if err != nil {
		return fmt.Errorf("open manifest: %w", err)
	}
	defer f.Close()

	manifest, err := ParseManifest(f)
	if err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}

	oreModulePath, err := FindOreModuleRoot(".")
	if err != nil {
		return fmt.Errorf("find ore module root: %w", err)
	}

	if err := Build(manifest, oreModulePath, manifest.Dist.OutputPath); err != nil {
		return fmt.Errorf("build: %w", err)
	}

	slog.Info("build complete", "output", manifest.Dist.OutputPath)
	return nil
}
