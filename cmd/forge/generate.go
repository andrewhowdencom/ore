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
}

// MainGoTemplateData holds the top-level data for main.go.tmpl.
type MainGoTemplateData struct {
	Conduits []ConduitTemplateData
}

// replaceDirective holds a single replace entry for go.mod.tmpl.
type replaceDirective struct {
	ModulePath string
	LocalPath  string
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
// a replace directive. For conduits that are submodules of ore (i.e. their
// module path starts with github.com/andrewhowdencom/ore/), an additional
// replace directive points from the conduit module path to its local path
// derived from oreModulePath.
func GenerateGoMod(blueprint *Blueprint, oreModulePath string) ([]byte, error) {
	tmpl, err := template.New("gomod").Parse(goModTmpl)
	if err != nil {
		return nil, fmt.Errorf("parse go.mod template: %w", err)
	}

	var replaces []replaceDirective
	orePrefix := "github.com/andrewhowdencom/ore"
	for _, c := range blueprint.Conduits {
		if strings.HasPrefix(c.Module, orePrefix+"/") {
			rel := strings.TrimPrefix(c.Module, orePrefix)
			rel = strings.TrimPrefix(rel, "/")
			localPath := filepath.Join(oreModulePath, filepath.FromSlash(rel))
			replaces = append(replaces, replaceDirective{
				ModulePath: c.Module,
				LocalPath:  localPath,
			})
		}
	}

	var buf bytes.Buffer
	data := struct {
		ModuleName    string
		OreModulePath string
		Replaces      []replaceDirective
	}{
		ModuleName:    blueprint.Dist.Name,
		OreModulePath: oreModulePath,
		Replaces:      replaces,
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute go.mod template: %w", err)
	}

	return buf.Bytes(), nil
}

// buildTemplateData converts a Blueprint into the template data structure
// used by main.go.tmpl.
func buildTemplateData(blueprint *Blueprint) (*MainGoTemplateData, error) {
	data := &MainGoTemplateData{}
	usedAliases := make(map[string]struct{})

	for i, c := range blueprint.Conduits {
		alias := deriveImportAlias(c.Module, usedAliases)
		data.Conduits = append(data.Conduits, ConduitTemplateData{
			Index:       i,
			ImportAlias: alias,
			ModulePath:  c.Module,
		})
		usedAliases[alias] = struct{}{}
	}

	return data, nil
}

// deriveImportAlias returns a Go import alias for module.
//
// The alias is derived from the last path element. Collisions with the
// standard library (e.g. "http" vs net/http) are avoided by using a
// well-known alternative. Numeric suffixes disambiguate duplicate aliases.
func deriveImportAlias(module string, used map[string]struct{}) string {
	parts := strings.Split(module, "/")
	alias := parts[len(parts)-1]

	// Avoid stdlib collisions.
	if alias == "http" {
		alias = "httpc"
	}

	base := alias
	for i := 1; ; i++ {
		if _, ok := used[alias]; !ok {
			return alias
		}
		alias = fmt.Sprintf("%s%d", base, i)
	}
}
