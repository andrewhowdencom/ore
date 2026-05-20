package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
)

//go:embed templates/main.go.tmpl
var mainGoTmpl string

//go:embed templates/go.mod.tmpl
var goModTmpl string

// ConduitTemplateData holds per-conduit information for main.go.tmpl.
type ConduitTemplateData struct {
	Index          int
	Name           string
	ImportAlias    string
	ModulePath     string
	HasOptions     bool
	OptionsLiteral string
}

// HandlerTemplateData holds per-handler information for main.go.tmpl.
type HandlerTemplateData struct {
	Index          int
	Name           string
	ImportAlias    string
	ModulePath     string
	HasOptions     bool
	OptionsLiteral string
}

// MainGoTemplateData holds the top-level data for main.go.tmpl.
type MainGoTemplateData struct {
	Name     string
	Conduits []ConduitTemplateData
	Handlers []HandlerTemplateData
}

// replaceDirective holds a single replace entry for go.mod.tmpl.
type replaceDirective struct {
	ModulePath string
	LocalPath  string
}

// GenerateMainGo produces a compilable main.go for the conduits and
// handlers specified in the blueprint.
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
// a replace directive. For conduits and handlers that are submodules of ore
// (i.e. their module path starts with github.com/andrewhowdencom/ore/), an
// additional replace directive points from the module path to its local path
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
	for _, h := range blueprint.Handlers {
		if strings.HasPrefix(h.Module, orePrefix+"/") {
			rel := strings.TrimPrefix(h.Module, orePrefix)
			rel = strings.TrimPrefix(rel, "/")
			localPath := filepath.Join(oreModulePath, filepath.FromSlash(rel))
			replaces = append(replaces, replaceDirective{
				ModulePath: h.Module,
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

// formatGoMapStringAny formats a map[string]any into a Go composite literal string.
func formatGoMapStringAny(m map[string]any) string {
	if len(m) == 0 {
		return "map[string]any{}"
	}
	var pairs []string
	for k, v := range m {
		pairs = append(pairs, fmt.Sprintf("%q: %s", k, goValue(v)))
	}
	sort.Strings(pairs)
	return fmt.Sprintf("map[string]any{%s}", strings.Join(pairs, ", "))
}

// goValue recursively formats a value into a valid Go literal string.
func goValue(v any) string {
	switch val := v.(type) {
	case string:
		return fmt.Sprintf("%q", val)
	case nil:
		return "nil"
	case bool, int, int64, float64:
		return fmt.Sprintf("%v", val)
	case []any:
		var parts []string
		for _, item := range val {
			parts = append(parts, goValue(item))
		}
		return fmt.Sprintf("[]any{%s}", strings.Join(parts, ", "))
	case map[string]any:
		var parts []string
		for k, v := range val {
			parts = append(parts, fmt.Sprintf("%q: %s", k, goValue(v)))
		}
		sort.Strings(parts)
		return fmt.Sprintf("map[string]any{%s}", strings.Join(parts, ", "))
	default:
		return fmt.Sprintf("%v", val)
	}
}

// buildTemplateData converts a Blueprint into the template data structure
// used by main.go.tmpl.
func buildTemplateData(blueprint *Blueprint) (*MainGoTemplateData, error) {
	data := &MainGoTemplateData{
		Name: blueprint.Dist.Name,
	}
	usedAliases := make(map[string]struct{})

	for i, c := range blueprint.Conduits {
		alias := deriveImportAlias(c.Module, usedAliases)
		ctd := ConduitTemplateData{
			Index:       i,
			Name:        c.Name,
			ImportAlias: alias,
			ModulePath:  c.Module,
		}
		if len(c.Options) > 0 {
			ctd.HasOptions = true
			ctd.OptionsLiteral = formatGoMapStringAny(c.Options)
		}
		data.Conduits = append(data.Conduits, ctd)
		usedAliases[alias] = struct{}{}
	}

	for i, h := range blueprint.Handlers {
		alias := deriveImportAlias(h.Module, usedAliases)
		htd := HandlerTemplateData{
			Index:       i,
			Name:        h.Name,
			ImportAlias: alias,
			ModulePath:  h.Module,
		}
		if len(h.Options) > 0 {
			htd.HasOptions = true
			htd.OptionsLiteral = formatGoMapStringAny(h.Options)
		}
		data.Handlers = append(data.Handlers, htd)
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
