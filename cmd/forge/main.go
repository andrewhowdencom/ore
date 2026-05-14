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

	f, err := os.Open(*configPath)
	if err != nil {
		return fmt.Errorf("open manifest: %w", err)
	}
	defer f.Close()

	return fmt.Errorf("manifest parsing not yet implemented")
}
