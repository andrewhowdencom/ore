package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCurrentBranch(t *testing.T) {
	dir := setupTestRepo(t)
	commitFile(t, dir, "a.go", "init")

	branch, err := currentBranch(dir)
	if err != nil {
		t.Fatal(err)
	}
	if branch != "main" && branch != "master" {
		t.Errorf("branch = %q, want main or master", branch)
	}
}

func TestRunGoModTidy(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\ngo 1.26\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc main() {}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	m := Module{Path: "example.com/test", Dir: "."}
	if err := runGoModTidy(dir, m); err != nil {
		t.Fatal(err)
	}
}
