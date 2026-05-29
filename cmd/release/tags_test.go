package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func setupTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := exec.Command("git", "-C", dir, "init").Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if err := exec.Command("git", "-C", dir, "config", "user.email", "test@example.com").Run(); err != nil {
		t.Fatalf("git config: %v", err)
	}
	if err := exec.Command("git", "-C", dir, "config", "user.name", "Test").Run(); err != nil {
		t.Fatalf("git config: %v", err)
	}
	return dir
}

func commitFile(t *testing.T, dir, path, msg string) {
	t.Helper()
	fullPath := filepath.Join(dir, path)
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fullPath, []byte(msg), 0644); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "-C", dir, "add", path).Run(); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if err := exec.Command("git", "-C", dir, "commit", "-m", msg).Run(); err != nil {
		t.Fatalf("git commit: %v", err)
	}
}

func TestLatestTag(t *testing.T) {
	dir := setupTestRepo(t)

	commitFile(t, dir, "README.md", "init")

	// Root tags
	if err := exec.Command("git", "-C", dir, "tag", "v0.1.0").Run(); err != nil {
		t.Fatalf("git tag: %v", err)
	}
	if err := exec.Command("git", "-C", dir, "tag", "v0.2.0").Run(); err != nil {
		t.Fatalf("git tag: %v", err)
	}
	if err := exec.Command("git", "-C", dir, "tag", "v0.1.1").Run(); err != nil {
		t.Fatalf("git tag: %v", err)
	}
	if err := exec.Command("git", "-C", dir, "tag", "0.0.1").Run(); err != nil { // invalid (no v prefix) should be ignored
		t.Fatalf("git tag: %v", err)
	}

	// Submodule tags
	if err := exec.Command("git", "-C", dir, "tag", "x/provider/openai/v0.3.0").Run(); err != nil {
		t.Fatalf("git tag: %v", err)
	}
	if err := exec.Command("git", "-C", dir, "tag", "x/provider/openai/v0.2.0").Run(); err != nil {
		t.Fatalf("git tag: %v", err)
	}
	if err := exec.Command("git", "-C", dir, "tag", "x/tool/v0.1.0").Run(); err != nil {
		t.Fatalf("git tag: %v", err)
	}
	if err := exec.Command("git", "-C", dir, "tag", "x/tool/bash/v0.5.0").Run(); err != nil {
		t.Fatalf("git tag: %v", err)
	}

	tests := []struct {
		moduleDir string
		want      string
	}{
		{moduleDir: ".", want: "v0.2.0"},
		{moduleDir: "x/provider/openai", want: "x/provider/openai/v0.3.0"},
		{moduleDir: "x/tool", want: "x/tool/v0.1.0"},
		{moduleDir: "x/tool/bash", want: "x/tool/bash/v0.5.0"},
		{moduleDir: "x/nonexistent", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.moduleDir, func(t *testing.T) {
			got, err := latestTag(dir, tt.moduleDir)
			if err != nil {
				t.Fatalf("latestTag(%q): %v", tt.moduleDir, err)
			}
			if got != tt.want {
				t.Errorf("latestTag(%q) = %q, want %q", tt.moduleDir, got, tt.want)
			}
		})
	}
}
