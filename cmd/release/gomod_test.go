package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateModuleDeps(t *testing.T) {
	dir := t.TempDir()

	content := `module example.com/mod

go 1.21

require (
	github.com/andrewhowdencom/ore v0.0.0
	github.com/andrewhowdencom/ore/x/tool v0.1.0
	github.com/other/lib v1.0.0
)
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	latestTags := map[string]string{
		"github.com/andrewhowdencom/ore":        "v0.2.0",
		"github.com/andrewhowdencom/ore/x/tool": "v0.2.0",
	}

	m := Module{Path: "example.com/mod", Dir: "."}
	if err := updateModuleDeps(dir, m, latestTags); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	updated := string(data)

	if !strings.Contains(updated, "github.com/andrewhowdencom/ore v0.2.0") {
		t.Errorf("ore dependency not updated\n%s", updated)
	}
	if !strings.Contains(updated, "github.com/andrewhowdencom/ore/x/tool v0.2.0") {
		t.Errorf("x/tool dependency not updated\n%s", updated)
	}
	if !strings.Contains(updated, "github.com/other/lib v1.0.0") {
		t.Errorf("other/lib dependency should not be modified\n%s", updated)
	}
}

func TestUpdateModuleDeps_NoChange(t *testing.T) {
	dir := t.TempDir()
	content := `module example.com/mod
go 1.21
require github.com/andrewhowdencom/ore v0.2.0
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	latestTags := map[string]string{
		"github.com/andrewhowdencom/ore": "v0.2.0",
	}

	m := Module{Path: "example.com/mod", Dir: "."}
	if err := updateModuleDeps(dir, m, latestTags); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != content {
		t.Errorf("go.mod should not have been modified\nwant:\n%s\ngot:\n%s", content, string(data))
	}
}

func TestUpdateModuleDeps_Placeholder(t *testing.T) {
	dir := t.TempDir()
	content := `module example.com/mod
go 1.21
require github.com/andrewhowdencom/ore v0.0.0-00010101000000-000000000000
`
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	latestTags := map[string]string{
		"github.com/andrewhowdencom/ore": "v0.1.0",
	}

	m := Module{Path: "example.com/mod", Dir: "."}
	if err := updateModuleDeps(dir, m, latestTags); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "github.com/andrewhowdencom/ore v0.1.0") {
		t.Errorf("placeholder version not updated\n%s", string(data))
	}
}
