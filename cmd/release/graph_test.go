package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBuildDependencyGraph(t *testing.T) {
	root := t.TempDir()

	// Root module depends on x/tool
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(
		"module example.com/root\ngo 1.21\nrequire github.com/andrewhowdencom/ore/x/tool v0.1.0\n"),
		0644); err != nil {
		t.Fatal(err)
	}

	// Submodule with no ore deps
	if err := os.MkdirAll(filepath.Join(root, "x", "tool"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "x", "tool", "go.mod"),
		[]byte("module example.com/root/x/tool\ngo 1.21\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Another submodule depends on root and tool
	if err := os.MkdirAll(filepath.Join(root, "x", "provider", "openai"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "x", "provider", "openai", "go.mod"), []byte(
		"module example.com/root/x/provider/openai\ngo 1.21\nrequire (\n\tgithub.com/andrewhowdencom/ore v0.1.0\n\tgithub.com/andrewhowdencom/ore/x/tool v0.1.0\n)\n"),
		0644); err != nil {
		t.Fatal(err)
	}

	modules := []Module{
		{Path: "example.com/root", Dir: "."},
		{Path: "example.com/root/x/tool", Dir: "x/tool"},
		{Path: "example.com/root/x/provider/openai", Dir: "x/provider/openai"},
	}

	graph, err := buildDependencyGraph(root, modules)
	if err != nil {
		t.Fatal(err)
	}

	if len(graph["example.com/root"]) != 1 || graph["example.com/root"][0] != "github.com/andrewhowdencom/ore/x/tool" {
		t.Errorf("root deps = %v, want [github.com/andrewhowdencom/ore/x/tool]", graph["example.com/root"])
	}

	if len(graph["example.com/root/x/provider/openai"]) != 2 {
		t.Errorf("openai deps = %v, want 2 deps", graph["example.com/root/x/provider/openai"])
	}

	if len(graph["example.com/root/x/tool"]) != 0 {
		t.Errorf("tool deps = %v, want none", graph["example.com/root/x/tool"])
	}
}

func TestTopologicalSort(t *testing.T) {
	modules := []Module{
		{Path: "A", Dir: "a"},
		{Path: "B", Dir: "b"},
		{Path: "C", Dir: "c"},
	}
	graph := map[string][]string{
		"A": {"B"}, // A depends on B
		"B": {"C"}, // B depends on C
		"C": {},    // C has no dependencies
	}
	sorted, err := topologicalSort(modules, graph)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"C", "B", "A"}
	for i, m := range sorted {
		if m.Path != want[i] {
			t.Errorf("sorted[%d] = %q, want %q", i, m.Path, want[i])
		}
	}
}

func TestTopologicalSort_Cycle(t *testing.T) {
	modules := []Module{
		{Path: "A", Dir: "a"},
		{Path: "B", Dir: "b"},
	}
	graph := map[string][]string{
		"A": {"B"},
		"B": {"A"},
	}
	_, err := topologicalSort(modules, graph)
	if err == nil {
		t.Fatal("expected cycle error, got nil")
	}
}

func TestTopologicalSort_Independent(t *testing.T) {
	modules := []Module{
		{Path: "A", Dir: "a"},
		{Path: "B", Dir: "b"},
	}
	graph := map[string][]string{
		"A": {},
		"B": {},
	}
	sorted, err := topologicalSort(modules, graph)
	if err != nil {
		t.Fatal(err)
	}
	if len(sorted) != 2 {
		t.Fatalf("expected 2 modules, got %d", len(sorted))
	}
}
