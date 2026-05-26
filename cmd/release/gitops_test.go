package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestStageAndCommitSkipsEmptyCommit(t *testing.T) {
	dir := setupTestRepo(t)
	commitFile(t, dir, "go.mod", "init")

	m := Module{Path: "example.com/test", Dir: "."}
	if err := stageAndCommit(dir, m, "v0.1.0", false); err != nil {
		t.Fatalf("stageAndCommit: %v", err)
	}

	// Verify no new commit was created.
	out, err := exec.Command("git", "-C", dir, "log", "-1", "--pretty=format:%s").Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "init" {
		t.Errorf("expected no new commit, got %q", string(out))
	}
}

func TestStageAndCommitCreatesCommitWhenChanged(t *testing.T) {
	dir := setupTestRepo(t)
	commitFile(t, dir, "go.mod", "init")

	// Modify go.mod after the initial commit.
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\ngo 1.26\n\nrequire foo v1.0.0\n"), 0644); err != nil {
		t.Fatal(err)
	}

	m := Module{Path: "example.com/test", Dir: "."}
	if err := stageAndCommit(dir, m, "v0.1.0", false); err != nil {
		t.Fatalf("stageAndCommit: %v", err)
	}

	// Verify a new commit was created with the release message.
	out, err := exec.Command("git", "-C", dir, "log", "-1", "--pretty=format:%s").Output()
	if err != nil {
		t.Fatal(err)
	}
	want := "chore(release): bump example.com/test to v0.1.0"
	if string(out) != want {
		t.Errorf("commit message = %q, want %q", string(out), want)
	}
}

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
