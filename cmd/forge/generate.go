package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"go/parser"
	"go/token"
	"text/template"
)

//go:embed templates/main.go.tmpl
var mainGoTmpl string

//go:embed templates/go.mod.tmpl
var goModTmpl string

// GenerateMainGo produces a compilable main.go for the conduit specified
// in manifest.
func GenerateMainGo(manifest *Manifest) ([]byte, error) {
	tmpl, err := template.New("main").Parse(mainGoTmpl)
	if err != nil {
		return nil, fmt.Errorf("parse main.go template: %w", err)
	}

	var buf bytes.Buffer
	data := struct {
		ConduitType string
	}{
		ConduitType: manifest.Conduit.Type,
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

// GenerateGoMod produces a go.mod that depends on the local ore module via
// a replace directive.
func GenerateGoMod(manifest *Manifest, oreModulePath string) ([]byte, error) {
	tmpl, err := template.New("gomod").Parse(goModTmpl)
	if err != nil {
		return nil, fmt.Errorf("parse go.mod template: %w", err)
	}

	var buf bytes.Buffer
	data := struct {
		ModuleName    string
		OreModulePath string
	}{
		ModuleName:    manifest.Dist.Name,
		OreModulePath: oreModulePath,
	}

	if err := tmpl.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("execute go.mod template: %w", err)
	}

	return buf.Bytes(), nil
}
