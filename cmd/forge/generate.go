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

// ConduitTemplateData holds per-conduit information for main.go.tmpl.
type ConduitTemplateData struct {
	Index       int
	ImportAlias string
	ModulePath  string
	Options     []string
}

// MainGoTemplateData holds the top-level data for main.go.tmpl.
type MainGoTemplateData struct {
	HasFlag  bool
	HasHTTP  bool
	Conduits []ConduitTemplateData
}

// GenerateMainGo produces a compilable main.go for the conduits specified
// in blueprint.
func GenerateMainGo(blueprint *Blueprint) ([]byte, error) {
	tmpl, err := template.New("main").Parse(mainGoTmpl)
	if err != nil {
		return nil, fmt.Errorf("parse main.go template: %w", err)
	}

	data, err := buildTemplateData(blueprint)
	if err != nil {
		return nil, fmt.Errorf("build template data: %w", err)
	}

	var buf bytes.Buffer
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

// Generate writes generated main.go and go.mod files into targetDir.
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

// buildTemplateData converts a Blueprint into the template data structure
// used by main.go.tmpl, populating conduit-specific option strings for
// built-in conduits.
func buildTemplateData(blueprint *Blueprint) (*MainGoTemplateData, error) {
	data := &MainGoTemplateData{}
	usedAliases := make(map[string]struct{})

	for i, c := range blueprint.Conduits {
		alias := deriveImportAlias(c.Module, usedAliases)
		cond := ConduitTemplateData{
			Index:       i,
			ImportAlias: alias,
			ModulePath:  c.Module,
		}

		switch {
		case isBuiltInHTTP(c.Module):
			data.HasHTTP = true
			cond.Options = []string{
				alias + ".WithUI()",
				alias + `.WithAddr(":" + port)`,
			}
		case isBuiltInTUI(c.Module):
			data.HasFlag = true
			cond.Options = []string{
				alias + ".WithThreadID(threadID)",
			}
		}
		// External conduits have no options for the first iteration.

		data.Conduits = append(data.Conduits, cond)
		usedAliases[alias] = struct{}{}
	}

	return data, nil
}

// isBuiltInHTTP reports whether the given module path is the built-in HTTP
// conduit.
func isBuiltInHTTP(module string) bool {
	return module == "github.com/andrewhowdencom/ore/x/conduit/http"
}

// isBuiltInTUI reports whether the given module path is the built-in TUI
// conduit.
func isBuiltInTUI(module string) bool {
	return module == "github.com/andrewhowdencom/ore/x/conduit/tui"
}

// deriveImportAlias returns a Go import alias for module.
//
// Built-in conduits use hardcoded aliases (httpc, tui) to avoid stdlib
// conflicts. External conduits derive the alias from the last path element
// and disambiguate collisions with numeric suffixes.
func deriveImportAlias(module string, used map[string]struct{}) string {
	switch module {
	case "github.com/andrewhowdencom/ore/x/conduit/http":
		return "httpc"
	case "github.com/andrewhowdencom/ore/x/conduit/tui":
		return "tui"
	}

	parts := strings.Split(module, "/")
	alias := parts[len(parts)-1]

	base := alias
	for i := 1; ; i++ {
		if _, ok := used[alias]; !ok {
			return alias
		}
		alias = fmt.Sprintf("%s%d", base, i)
	}
}
