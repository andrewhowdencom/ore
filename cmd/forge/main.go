// Package main provides the forge CLI, a tool that reads a YAML
// manifest and generates a compilable Go agent application.
//
// Usage:
//
//	forge build --config forge.yaml
//	forge generate --config forge.yaml
//	forge version
package main

import (
	"fmt"
	"log/slog"
	"os"
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))

	if err := run(); err != nil {
		slog.Error("forge failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cmd := newForgeCmd()
	cmd.SetArgs(normalizeArgs(os.Args[1:]))
	return cmd.Execute()
}

// normalizeArgs converts single-dash long flags (e.g. -config) to the
// double-dash form that Cobra's pflag package expects. This preserves
// backward compatibility with the original flat flag-based CLI.
func normalizeArgs(args []string) []string {
	result := make([]string, len(args))
	copy(result, args)
	for i, arg := range result {
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && len(arg) > 2 {
			name := strings.TrimPrefix(arg, "-")
			if idx := strings.Index(name, "="); idx != -1 {
				name = name[:idx]
			}
			if name == "config" {
				result[i] = "--" + strings.TrimPrefix(arg, "-")
			}
		}
	}
	return result
}

func newForgeCmd() *cobra.Command {
	var logLevel string

	rootCmd := &cobra.Command{
		Use:           "forge",
		Short:         "Generate and build ore agent binaries from YAML manifests",
		SilenceErrors: true,
		SilenceUsage:  true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			var level slog.Level
			if err := level.UnmarshalText([]byte(logLevel)); err != nil {
				return fmt.Errorf("invalid log level %q: %w", logLevel, err)
			}
			handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
			slog.SetDefault(slog.New(handler))
			return nil
		},
	}

	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", "info", "log level (debug, info, warn, error)")

	var configPath string
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "forge.yaml", "path to manifest file")

	buildCmd := &cobra.Command{
		Use:     "build",
		Short:   "Generate and compile an agent binary",
		Example: "forge build --config forge.yaml",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWithPath(configPath)
		},
	}

	var outputDir string
	generateCmd := &cobra.Command{
		Use:   "generate",
		Short: "Generate main.go and go.mod without compiling",
		Example: "forge generate --config forge.yaml\n" +
			"forge generate --config forge.yaml -o ./my-agent/",
		RunE: func(cmd *cobra.Command, args []string) error {
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

			if outputDir != "" {
				if err := os.MkdirAll(outputDir, 0755); err != nil {
					return fmt.Errorf("create output directory: %w", err)
				}
				if err := Generate(manifest, oreModulePath, outputDir); err != nil {
					return fmt.Errorf("generate: %w", err)
				}
				slog.Info("generate complete", "output", outputDir)
				return nil
			}

			mainGo, err := GenerateMainGo(manifest)
			if err != nil {
				return fmt.Errorf("generate main.go: %w", err)
			}
			if _, err := cmd.OutOrStdout().Write(mainGo); err != nil {
				return fmt.Errorf("write main.go to stdout: %w", err)
			}

			separator := []byte("\n// --- FILE: go.mod ---\n")
			if _, err := cmd.OutOrStdout().Write(separator); err != nil {
				return fmt.Errorf("write separator to stdout: %w", err)
			}

			goMod, err := GenerateGoMod(manifest, oreModulePath)
			if err != nil {
				return fmt.Errorf("generate go.mod: %w", err)
			}
			if _, err := cmd.OutOrStdout().Write(goMod); err != nil {
				return fmt.Errorf("write go.mod to stdout: %w", err)
			}

			return nil
		},
	}
	generateCmd.Flags().StringVarP(&outputDir, "output", "o", "", "output directory (default: stdout)")

	versionCmd := &cobra.Command{
		Use:     "version",
		Short:   "Print version information",
		Example: "forge version",
		RunE: func(cmd *cobra.Command, args []string) error {
			info, ok := debug.ReadBuildInfo()
			if !ok || info.Main.Version == "" {
				fmt.Fprintln(cmd.OutOrStdout(), "dev")
				return nil
			}
			fmt.Fprintln(cmd.OutOrStdout(), info.Main.Version)
			return nil
		},
	}

	rootCmd.AddCommand(buildCmd, generateCmd, versionCmd)
	rootCmd.RunE = buildCmd.RunE

	return rootCmd
}

// runWithPath executes the forge build pipeline for the manifest at configPath.
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
