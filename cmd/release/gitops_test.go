package main

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// setupTestRepo and commitFile are test helpers defined in tags_test.go.
// setupTestRepo creates a temporary Git repo with an initial commit.
// commitFile writes a file, stages it, and commits with the given message.

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w

	outC := make(chan string)
	go func() {
		var buf bytes.Buffer
		io.Copy(&buf, r)
		outC <- buf.String()
	}()

	fn()
	w.Close()
	os.Stdout = old
	return <-outC
}

func TestStageAndCommit_NoChanges_SkipsCommit(t *testing.T) {
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

func TestStageAndCommit_WithChanges_CreatesCommit(t *testing.T) {
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

func TestStageAndCommit_WithGoSum_CreatesCommit(t *testing.T) {
	dir := setupTestRepo(t)
	commitFile(t, dir, "go.mod", "init")

	// Create a go.sum file and commit it.
	if err := os.WriteFile(filepath.Join(dir, "go.sum"), []byte("example.com/test h1:abc\n"), 0644); err != nil {
		t.Fatal(err)
	}
	exec.Command("git", "-C", dir, "add", "go.sum").Run()
	exec.Command("git", "-C", dir, "commit", "-m", "add go.sum").Run()

	// Modify only go.mod.
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/test\ngo 1.26\n\nrequire foo v1.0.0\n"), 0644); err != nil {
		t.Fatal(err)
	}

	m := Module{Path: "example.com/test", Dir: "."}
	if err := stageAndCommit(dir, m, "v0.1.0", false); err != nil {
		t.Fatalf("stageAndCommit: %v", err)
	}

	// Verify a new commit was created.
	out, err := exec.Command("git", "-C", dir, "log", "-1", "--pretty=format:%s").Output()
	if err != nil {
		t.Fatal(err)
	}
	want := "chore(release): bump example.com/test to v0.1.0"
	if string(out) != want {
		t.Errorf("commit message = %q, want %q", string(out), want)
	}
}

func TestStageAndCommit_DryRun(t *testing.T) {
	dir := setupTestRepo(t)
	commitFile(t, dir, "go.mod", "init")

	m := Module{Path: "example.com/test", Dir: "."}
	got := captureStdout(t, func() {
		if err := stageAndCommit(dir, m, "v0.1.0", true); err != nil {
			t.Fatalf("stageAndCommit: %v", err)
		}
	})
	want := "[dry-run] git add go.mod\n[dry-run] git commit -m \"chore(release): bump example.com/test to v0.1.0\"\n[dry-run] (commit skipped if no staged changes)\n"
	if got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}

	// Verify no commit was created.
	out, err := exec.Command("git", "-C", dir, "log", "-1", "--pretty=format:%s").Output()
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "init" {
		t.Errorf("dry-run should not create a commit, got %q", string(out))
	}
}

func TestStageAndCommit_NoChanges_CapturesStdout(t *testing.T) {
	dir := setupTestRepo(t)
	commitFile(t, dir, "go.mod", "init")

	m := Module{Path: "example.com/test", Dir: "."}
	got := captureStdout(t, func() {
		if err := stageAndCommit(dir, m, "v0.1.0", false); err != nil {
			t.Fatalf("stageAndCommit: %v", err)
		}
	})
	want := "No changes to commit for example.com/test v0.1.0; skipping commit.\n"
	if got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

func TestCreateTag_Root(t *testing.T) {
	dir := setupTestRepo(t)
	commitFile(t, dir, "README.md", "init")

	m := Module{Path: "example.com/test", Dir: "."}
	if err := createTag(dir, m, "v0.1.0", false); err != nil {
		t.Fatalf("createTag: %v", err)
	}

	out, err := exec.Command("git", "-C", dir, "tag", "-l").Output()
	if err != nil {
		t.Fatal(err)
	}
	tags := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(tags) != 1 || tags[0] != "v0.1.0" {
		t.Errorf("tags = %v, want [v0.1.0]", tags)
	}
}

func TestCreateTag_Submodule(t *testing.T) {
	dir := setupTestRepo(t)
	commitFile(t, dir, "README.md", "init")

	m := Module{Path: "example.com/test/x/sub", Dir: "x/sub"}
	if err := createTag(dir, m, "v0.1.0", false); err != nil {
		t.Fatalf("createTag: %v", err)
	}

	out, err := exec.Command("git", "-C", dir, "tag", "-l").Output()
	if err != nil {
		t.Fatal(err)
	}
	tags := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(tags) != 1 || tags[0] != "x/sub/v0.1.0" {
		t.Errorf("tags = %v, want [x/sub/v0.1.0]", tags)
	}
}

func TestCreateTag_DryRun(t *testing.T) {
	dir := setupTestRepo(t)
	commitFile(t, dir, "README.md", "init")

	m := Module{Path: "example.com/test", Dir: "."}
	got := captureStdout(t, func() {
		if err := createTag(dir, m, "v0.1.0", true); err != nil {
			t.Fatalf("createTag: %v", err)
		}
	})
	want := "[dry-run] git tag -a v0.1.0 -m \"Release example.com/test v0.1.0\"\n"
	if got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}

	// Verify no tag was actually created.
	out, err := exec.Command("git", "-C", dir, "tag", "-l").Output()
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(out)) != "" {
		t.Errorf("dry-run created a tag: %q", string(out))
	}
}

func TestPush_DryRun(t *testing.T) {
	dir := setupTestRepo(t)
	commitFile(t, dir, "README.md", "init")

	got := captureStdout(t, func() {
		if err := push(dir, true); err != nil {
			t.Fatalf("push: %v", err)
		}
	})
	want := "[dry-run] git push origin main\n[dry-run] git push origin --tags\n"
	if got != want {
		t.Errorf("stdout = %q, want %q", got, want)
	}
}

// TestCurrentBranch verifies branch detection. Git init may create 'master'
// as the default branch on older installations; accept both.
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
