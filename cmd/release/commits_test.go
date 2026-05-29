package main

import (
	"os/exec"
	"testing"
)

func TestCommitsSinceTag(t *testing.T) {
	dir := setupTestRepo(t)

	// Commit 1
	commitFile(t, dir, "main.go", "initial commit")

	// Tag v0.1.0
	if err := exec.Command("git", "-C", dir, "tag", "v0.1.0").Run(); err != nil {
		t.Fatal(err)
	}

	// Commit 2
	commitFile(t, dir, "feature.go", "feat: add feature")

	// Commit 3
	commitFile(t, dir, "fix.go", "fix: bugfix")

	commits, err := commitsSinceTag(dir, "v0.1.0", newCommitCache())
	if err != nil {
		t.Fatalf("commitsSinceTag: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(commits))
	}

	// git rev-list returns newest first (HEAD first)
	if commits[0].Message != "fix: bugfix" {
		t.Errorf("first commit message = %q, want %q", commits[0].Message, "fix: bugfix")
	}
	if commits[1].Message != "feat: add feature" {
		t.Errorf("second commit message = %q, want %q", commits[1].Message, "feat: add feature")
	}

	// Verify files
	if len(commits[0].Files) != 1 || commits[0].Files[0] != "fix.go" {
		t.Errorf("first commit files = %v, want [fix.go]", commits[0].Files)
	}
	if len(commits[1].Files) != 1 || commits[1].Files[0] != "feature.go" {
		t.Errorf("second commit files = %v, want [feature.go]", commits[1].Files)
	}
}

func TestCommitsSinceTag_Empty(t *testing.T) {
	dir := setupTestRepo(t)
	commitFile(t, dir, "main.go", "init")
	if err := exec.Command("git", "-C", dir, "tag", "v1.0.0").Run(); err != nil {
		t.Fatal(err)
	}

	commits, err := commitsSinceTag(dir, "v1.0.0", newCommitCache())
	if err != nil {
		t.Fatalf("commitsSinceTag: %v", err)
	}
	if len(commits) != 0 {
		t.Fatalf("expected 0 commits, got %d", len(commits))
	}
}

func TestCommitsSinceTag_NoTag(t *testing.T) {
	dir := setupTestRepo(t)
	commitFile(t, dir, "a.go", "first")
	commitFile(t, dir, "b.go", "second")

	commits, err := commitsSinceTag(dir, "", newCommitCache())
	if err != nil {
		t.Fatalf("commitsSinceTag: %v", err)
	}
	if len(commits) != 2 {
		t.Fatalf("expected 2 commits, got %d", len(commits))
	}
}

func TestMapCommitsToModules(t *testing.T) {
	modules := []Module{
		{Path: "github.com/example/root", Dir: "."},
		{Path: "github.com/example/root/x/tool", Dir: "x/tool"},
		{Path: "github.com/example/root/x/tool/bash", Dir: "x/tool/bash"},
		{Path: "github.com/example/root/x/provider/openai", Dir: "x/provider/openai"},
	}

	commits := []Commit{
		{
			SHA:     "abc123",
			Message: "feat: update tool",
			Files:   []string{"x/tool/tool.go"},
		},
		{
			SHA:     "def456",
			Message: "feat: update bash",
			Files:   []string{"x/tool/bash/main.go"},
		},
		{
			SHA:     "ghi789",
			Message: "fix: update both",
			Files:   []string{"x/tool/tool.go", "x/tool/bash/main.go"},
		},
		{
			SHA:     "jkl012",
			Message: "docs: update readme",
			Files:   []string{"README.md"},
		},
		{
			SHA:     "mno345",
			Message: "feat: update provider",
			Files:   []string{"x/provider/openai/client.go"},
		},
		{
			SHA:     "pqr678",
			Message: "chore: cross-module change",
			Files:   []string{"x/tool/registry.go", "x/provider/openai/client.go"},
		},
	}

	result := mapCommitsToModules(commits, modules)

	// Root should only get commits that don't match any submodule
	rootCommits := result["github.com/example/root"]
	if len(rootCommits) != 1 {
		t.Errorf("root commits = %d, want 1", len(rootCommits))
	}
	if len(rootCommits) > 0 && rootCommits[0].SHA != "jkl012" {
		t.Errorf("root commit SHA = %q, want jkl012", rootCommits[0].SHA)
	}

	// x/tool should get commits that affect files directly in x/tool/.
	// Note: pqr678 changes x/tool/registry.go which also matches x/tool/.
	toolCommits := result["github.com/example/root/x/tool"]
	if len(toolCommits) != 3 {
		t.Errorf("x/tool commits = %d, want 3", len(toolCommits))
	}

	// x/tool/bash should get commits that affect files in x/tool/bash/
	bashCommits := result["github.com/example/root/x/tool/bash"]
	if len(bashCommits) != 2 {
		t.Errorf("x/tool/bash commits = %d, want 2", len(bashCommits))
	}
	// def456 and ghi789 (ghi789 has x/tool/bash/main.go which matches x/tool/bash/)

	// x/provider/openai should get its own commits and cross-module ones
	providerCommits := result["github.com/example/root/x/provider/openai"]
	if len(providerCommits) != 2 {
		t.Errorf("x/provider/openai commits = %d, want 2", len(providerCommits))
	}
	// mno345 and pqr678

	// Verify ghi789 affects BOTH x/tool and x/tool/bash
	foundTool := false
	foundBash := false
	for _, c := range toolCommits {
		if c.SHA == "ghi789" {
			foundTool = true
		}
	}
	for _, c := range bashCommits {
		if c.SHA == "ghi789" {
			foundBash = true
		}
	}
	if !foundTool {
		t.Error("ghi789 should affect x/tool")
	}
	if !foundBash {
		t.Error("ghi789 should affect x/tool/bash")
	}
}

func TestMapCommitsToModules_NestedPrecedence(t *testing.T) {
	// Ensure x/tool/bash takes precedence over x/tool for files inside x/tool/bash/
	modules := []Module{
		{Path: "github.com/example/root/x/tool", Dir: "x/tool"},
		{Path: "github.com/example/root/x/tool/bash", Dir: "x/tool/bash"},
	}

	commits := []Commit{
		{
			SHA:     "nested",
			Message: "feat: nested change",
			Files:   []string{"x/tool/bash/main.go"},
		},
	}

	result := mapCommitsToModules(commits, modules)

	bashCommits := result["github.com/example/root/x/tool/bash"]
	if len(bashCommits) != 1 {
		t.Fatalf("x/tool/bash commits = %d, want 1", len(bashCommits))
	}

	toolCommits := result["github.com/example/root/x/tool"]
	if len(toolCommits) != 0 {
		t.Errorf("x/tool commits = %d, want 0 (nested should win)", len(toolCommits))
	}
}
