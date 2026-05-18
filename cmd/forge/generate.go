package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed templates/main.go.tmpl
var mainGoTmpl string

//go:embed templates/go.mod.tmpl
var goModTmpl string

// GenerateMainGo produces a compilable main.go for the conduits specified
// in blueprint.
func GenerateMainGo(blueprint *Blueprint) ([]byte, error) {
	tmpl, err := template.New("main").Parse(mainGoTmpl)
	if err != nil {
		return nil, fmt.Errorf("parse main.go template: %w", err)
	}

	var buf bytes.Buffer
	data := struct {
		ConduitType string
	}{
		ConduitType: deriveConduitType(blueprint),
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute main.go template: %w", err)
	}

	// Verify generated code is valid Go syntax.
	fset := token.NewFileSet()
	if _, err := parser.ParseFile(fset, "main.go", buf.Bytes(), parser.AllErrors); err != nil {
		return nil, fmt.Errorf("generated main.go is invalid Go: %w", err)
	}

	return buf.Bytes(), nil
}

// Generate writes main.go and go.mod into targetDir.
func Generate(blueprint *Blueprint, oreModulePath string, targetDir string) error {
	mainGo, err := GenerateMainGo(blueprint)
	if err != nil {
		return fmt.Errorf("generate main.go: %w", err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "main.go"), mainGo, 0644); err != nil {
		return fmt.Errorf("write main.go: %w", err)
	}

	goMod, err := GenerateGoMod(blueprint, oreModulePath)
	if err != nil {
		return fmt.Errorf("generate go.mod: %w", err)
	}
	if err := os.WriteFile(filepath.Join(targetDir, "go.mod"), goMod, 0644); err != nil {
		return fmt.Errorf("write go.mod: %w", err)
	}

	return nil
}

// GenerateGoMod produces a go.mod that depends on the local ore module via
// a replace directive.
func GenerateGoMod(blueprint *Blueprint, oreModulePath string) ([]byte, error) {
	tmpl, err := template.New("gomod").Parse(goModTmpl)
	if err != nil {
		return nil, fmt.Errorf("parse go.mod template: %w", err)
	}

	var buf bytes.Buffer
	data := struct {
		ModuleName    string
		OreModulePath string
	}{
		ModuleName:    blueprint.Dist.Name,
		OreModulePath: oreModulePath,
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute go.mod template: %w", err)
	}

	return buf.Bytes(), nil
}

// deriveConduitType returns the legacy conduit type ("http" or "tui") from
// the first conduit module path, for backward compatibility with the current
// template. This will be removed when the template is rewritten for multi-
// conduit support.
func deriveConduitType(blueprint *Blueprint) string {
	if len(blueprint.Conduits) == 0 {
		return ""
	}
	module := blueprint.Conduits[0].Module
	switch {
	case strings.HasSuffix(module, "/http"):
		return "http"
	case strings.HasSuffix(module, "/tui"):
		return "tui"
	default:
		return ""
	}
}
