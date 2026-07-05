package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Test helpers used across the package. The git-related helpers
// (setupTestRepo, commitFile) live in tags_test.go.

// removeFile stages a deletion and commits it with the given message.
func removeFile(t *testing.T, dir, path, msg string) {
	t.Helper()
	if err := exec.Command("git", "-C", dir, "rm", path).Run(); err != nil {
		t.Fatalf("git rm %s: %v", path, err)
	}
	if err := exec.Command("git", "-C", dir, "commit", "-m", msg).Run(); err != nil {
		t.Fatalf("git commit (rm %s): %v", path, err)
	}
}

// renameFile moves a tracked file and commits. The destination's parent
// directory is created if needed.
func renameFile(t *testing.T, dir, oldPath, newPath, msg string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, filepath.Dir(newPath)), 0755); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "-C", dir, "mv", oldPath, newPath).Run(); err != nil {
		t.Fatalf("git mv %s %s: %v", oldPath, newPath, err)
	}
	if err := exec.Command("git", "-C", dir, "commit", "-m", msg).Run(); err != nil {
		t.Fatalf("git commit (mv): %v", err)
	}
}

// tagAt marks the current HEAD with `name`.
func tagAt(t *testing.T, dir, name string) {
	t.Helper()
	if err := exec.Command("git", "-C", dir, "tag", name).Run(); err != nil {
		t.Fatalf("git tag %s: %v", name, err)
	}
}

// makeSubmodule creates dir/<subdir>/go.mod so the subdir is recognised as a
// distinct module for pathspec tests.
func makeSubmodule(t *testing.T, dir, subdir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, subdir), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, subdir, "go.mod"), []byte("module "+subdir+"\n\ngo 1.26.2\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "-C", dir, "add", "-A").Run(); err != nil {
		t.Fatal(err)
	}
}

func TestBumpFromDiff_NoChangesSinceTag(t *testing.T) {
	dir := setupTestRepo(t)
	commitFile(t, dir, "init.go", "init")
	tagAt(t, dir, "v0.1.0")

	got, err := bumpFromDiff(dir, ".", "v0.1.0", nil)
	if err != nil {
		t.Fatalf("bumpFromDiff: %v", err)
	}
	if got != None {
		t.Errorf("bumpFromDiff() = %v, want None", got)
	}
}

func TestBumpFromDiff_NoTag(t *testing.T) {
	// Untagged module: nothing to compare against.
	dir := setupTestRepo(t)
	commitFile(t, dir, "init.go", "init")

	got, err := bumpFromDiff(dir, ".", "", nil)
	if err != nil {
		t.Fatalf("bumpFromDiff: %v", err)
	}
	if got != None {
		t.Errorf("bumpFromDiff() with no tag = %v, want None", got)
	}
}

func TestBumpFromDiff_DeletedGoFile(t *testing.T) {
	dir := setupTestRepo(t)
	commitFile(t, dir, "init.go", "init")
	commitFile(t, dir, "public.go", "initial public surface")
	tagAt(t, dir, "v0.1.0")

	// Post-tag: delete public.go.
	removeFile(t, dir, "public.go", "refactor: drop public.go")

	got, err := bumpFromDiff(dir, ".", "v0.1.0", nil)
	if err != nil {
		t.Fatalf("bumpFromDiff: %v", err)
	}
	if got != Major {
		t.Errorf("bumpFromDiff() = %v, want Major", got)
	}
}

// Deleting a binary under cmd/ (the Go convention for executable
// entry points) must not trigger a major bump. The bumpFromDiff check
// is meant to surface library surface-area changes (package renames,
// removed exported files), not binary removals — a binary's removal
// cannot break consumers that import the module as a library, and
// promoting the version to vN+1 would force every downstream go.mod to
// adopt a /vN path under Go's module rules.
func TestBumpFromDiff_DeletedCmdBinary(t *testing.T) {
	dir := setupTestRepo(t)
	commitFile(t, dir, "init.go", "init")
	// Pretend cmd/foo was an experimental CLI shipped with the
	// library. It has its own main.go and a helper; both are package
	// main because that's the cmd/ convention.
	commitFile(t, dir, "cmd/foo/main.go", "package main\nfunc main(){}")
	commitFile(t, dir, "cmd/foo/helper.go", "package main\n// helper")
	commitFile(t, dir, "library.go", "library surface")
	tagAt(t, dir, "v1.0.0")

	removeFile(t, dir, "cmd/foo/main.go", "chore: retire experimental cli")
	removeFile(t, dir, "cmd/foo/helper.go", "chore: retire experimental cli")

	got, err := bumpFromDiff(dir, ".", "v1.0.0", nil)
	if err != nil {
		t.Fatalf("bumpFromDiff: %v", err)
	}
	// Library surface unchanged; removal of the cmd/ binary alone is
	// not a breaking change to library consumers.
	if got != None {
		t.Errorf("bumpFromDiff() = %v, want None (cmd/ binary deletion is not a library breaking change)", got)
	}
}

// Same rule for a nested module: deletions under that module's own
// cmd/ must not register as library breaking changes either.
func TestBumpFromDiff_DeletedCmdBinaryInSubmodule(t *testing.T) {
	dir := setupTestRepo(t)
	commitFile(t, dir, "init.go", "init")
	makeSubmodule(t, dir, "x/foo")
	commitFile(t, dir, "x/foo/library.go", "library surface")
	commitFile(t, dir, "x/foo/cmd/xfoo/main.go", "package main\nfunc main(){}")
	tagAt(t, dir, "v0.1.0")

	removeFile(t, dir, "x/foo/cmd/xfoo/main.go", "chore: retire cli")

	got, err := bumpFromDiff(dir, "x/foo", "v0.1.0", nil)
	if err != nil {
		t.Fatalf("bumpFromDiff: %v", err)
	}
	if got != None {
		t.Errorf("bumpFromDiff() = %v, want None (cmd/ binary deletion is not a library breaking change)", got)
	}
}

func TestBumpFromDiff_DeletedTestFileOnly(t *testing.T) {
	dir := setupTestRepo(t)
	commitFile(t, dir, "init.go", "init")
	commitFile(t, dir, "public.go", "initial public surface")
	commitFile(t, dir, "public_test.go", "initial tests")
	tagAt(t, dir, "v0.1.0")

	// Post-tag: delete only the test file.
	removeFile(t, dir, "public_test.go", "test: prune obsolete")

	got, err := bumpFromDiff(dir, ".", "v0.1.0", nil)
	if err != nil {
		t.Fatalf("bumpFromDiff: %v", err)
	}
	if got != None {
		t.Errorf("bumpFromDiff() = %v, want None (test-only deletions are not breaking)", got)
	}
}

func TestBumpFromDiff_RenamedGoFile(t *testing.T) {
	dir := setupTestRepo(t)
	commitFile(t, dir, "session/api.go", "initial api")
	makeSubmodule(t, dir, "session") // not strictly needed; makes the directory real
	tagAt(t, dir, "v0.1.0")

	// Simulate the bug that motivated this check: a package rename.
	renameFile(t, dir, "session/api.go", "junk/api.go", "refactor: rename session package to junk")

	got, err := bumpFromDiff(dir, ".", "v0.1.0", nil)
	if err != nil {
		t.Fatalf("bumpFromDiff: %v", err)
	}
	if got != Major {
		t.Errorf("bumpFromDiff() = %v, want Major (package rename is breaking)", got)
	}
}

func TestBumpFromDiff_RenamedNonGoFile(t *testing.T) {
	dir := setupTestRepo(t)
	commitFile(t, dir, "README.md", "readme")
	tagAt(t, dir, "v0.1.0")

	renameFile(t, dir, "README.md", "README2.md", "docs: rename readme")

	got, err := bumpFromDiff(dir, ".", "v0.1.0", nil)
	if err != nil {
		t.Fatalf("bumpFromDiff: %v", err)
	}
	if got != None {
		t.Errorf("bumpFromDiff() = %v, want None (non-.go renames are not breaking)", got)
	}
}

func TestBumpFromDiff_AddedGoFile(t *testing.T) {
	dir := setupTestRepo(t)
	commitFile(t, dir, "init.go", "init")
	tagAt(t, dir, "v0.1.0")

	commitFile(t, dir, "new.go", "feat: add new feature")

	got, err := bumpFromDiff(dir, ".", "v0.1.0", nil)
	if err != nil {
		t.Fatalf("bumpFromDiff: %v", err)
	}
	if got != None {
		t.Errorf("bumpFromDiff() = %v, want None (additions are not breaking)", got)
	}
}

func TestBumpFromDiff_PathspecIgnoresUnrelatedDeletion(t *testing.T) {
	// Deletion in a sibling submodule should not fire when querying the root.
	dir := setupTestRepo(t)
	commitFile(t, dir, "init.go", "init")
	makeSubmodule(t, dir, "x/conduit")
	commitFile(t, dir, "x/conduit/api.go", "submodule init")
	tagAt(t, dir, "v0.1.0")

	removeFile(t, dir, "x/conduit/api.go", "drop submodule api")

	got, err := bumpFromDiff(dir, ".", "v0.1.0", []string{"x/conduit"})
	if err != nil {
		t.Fatalf("bumpFromDiff: %v", err)
	}
	if got != None {
		t.Errorf("bumpFromDiff() = %v, want None (submodule deletion should not affect root)", got)
	}
}

func TestBumpFromDiff_PathspecCatchesOwnDeletion(t *testing.T) {
	// Deletion in the queried submodule should fire even though root has
	// other files too.
	dir := setupTestRepo(t)
	commitFile(t, dir, "init.go", "init")
	makeSubmodule(t, dir, "x/conduit")
	commitFile(t, dir, "x/conduit/api.go", "submodule init")
	tagAt(t, dir, "v0.1.0")

	removeFile(t, dir, "x/conduit/api.go", "drop submodule api")

	got, err := bumpFromDiff(dir, "x/conduit", "v0.1.0", nil)
	if err != nil {
		t.Fatalf("bumpFromDiff: %v", err)
	}
	if got != Major {
		t.Errorf("bumpFromDiff() = %v, want Major (deletion in own dir must fire)", got)
	}
}

// Regression test for the exact bug that bit the junk rename: a `refactor:`
// commit that deletes every .go file under one package and re-creates them
// under another should be detected as a Major bump even though the commit
// message is non-breaking.
func TestBumpFromDiff_RegressionSessionToJunk(t *testing.T) {
	dir := setupTestRepo(t)
	// Initial state: everything under session/.
	commitFile(t, dir, "session/manager.go", "old manager")
	commitFile(t, dir, "session/store.go", "old store")
	commitFile(t, dir, "session/manager_test.go", "old manager test")
	tagAt(t, dir, "v0.12.5")

	// Refactor: rename session -> junk (rename detection picks this up as R).
	renameFile(t, dir, "session/manager.go", "junk/manager.go", "refactor: rename session package to junk")
	renameFile(t, dir, "session/store.go", "junk/store.go", "refactor: rename session package to junk")
	renameFile(t, dir, "session/manager_test.go", "junk/manager_test.go", "refactor: rename session package to junk")

	got, err := bumpFromDiff(dir, ".", "v0.12.5", nil)
	if err != nil {
		t.Fatalf("bumpFromDiff: %v", err)
	}
	if got != Major {
		t.Errorf("bumpFromDiff() = %v, want Major (this is the exact bug that motivated the check)", got)
	}
}

func TestBumpForModule_OnlyCommitMessage(t *testing.T) {
	// No structural change, so the bump is whatever bumpType infers.
	dir := setupTestRepo(t)
	commitFile(t, dir, "init.go", "init")
	tagAt(t, dir, "v0.1.0")
	commitFile(t, dir, "feature.go", "refactor: internal cleanup")

	got, err := bumpForModule(dir, ".", "v0.1.0", []string{"refactor: internal cleanup"}, nil)
	if err != nil {
		t.Fatalf("bumpForModule: %v", err)
	}
	// "refactor:" maps to Patch per Conventional Commits.
	if got != Patch {
		t.Errorf("bumpForModule() = %v, want Patch", got)
	}
}

func TestBumpForModule_StructuralOverridesMessage(t *testing.T) {
	// Structural change must upgrade a Patch-bumping message to Major.
	dir := setupTestRepo(t)
	commitFile(t, dir, "init.go", "init")
	commitFile(t, dir, "public.go", "initial public surface")
	tagAt(t, dir, "v0.1.0")
	removeFile(t, dir, "public.go", "refactor: drop public.go")

	got, err := bumpForModule(dir, ".", "v0.1.0", []string{"refactor: drop public.go"}, nil)
	if err != nil {
		t.Fatalf("bumpForModule: %v", err)
	}
	if got != Major {
		t.Errorf("bumpForModule() = %v, want Major (deletion upgrades even a patch-typed message)", got)
	}
}

func TestBumpForModule_MessageMajorWinsEvenIfNoStructure(t *testing.T) {
	// Major from the message still counts when there's no structural change.
	dir := setupTestRepo(t)
	commitFile(t, dir, "init.go", "init")
	tagAt(t, dir, "v0.1.0")
	commitFile(t, dir, "feature.go", "feat!: breaking api")

	got, err := bumpForModule(dir, ".", "v0.1.0", []string{"feat!: breaking api"}, nil)
	if err != nil {
		t.Fatalf("bumpForModule: %v", err)
	}
	if got != Major {
		t.Errorf("bumpForModule() = %v, want Major", got)
	}
}
