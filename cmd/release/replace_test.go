package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

// writeFile is a tiny helper that writes content to dir/name and fails
// the test on failure.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// readFile reads dir/name and fails the test on failure.
func readFile(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// scaffoldModule writes a minimal go.mod and a small main.go that
// imports the given import paths so tidy has something to resolve.
//
// We use module paths under "github.com/andrewhowdencom/ore/x/" so the
// existing release-tool filters (which update only paths matching that
// prefix) treat them naturally during tests.
func scaffoldModule(t *testing.T, dir, path string, imports ...string) {
	t.Helper()
	writeFile(t, dir, "go.mod", "module "+path+"\n\ngo 1.26\n")

	var src strings.Builder
	src.WriteString("package " + lastSegment(path) + "\n\n")
	for _, imp := range imports {
		src.WriteString("import _ \"" + imp + "\"\n")
	}
	writeFile(t, dir, "main.go", src.String())
}

func lastSegment(p string) string {
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}

// moduleVer is a tiny helper for tests to construct a module.Version
// literal concisely.
func moduleVer(p string) module.Version {
	return module.Version{Path: p}
}

func TestAddAndRestoreLocalReplaces_NoOriginal(t *testing.T) {
	root := t.TempDir()

	const depPath = "github.com/andrewhowdencom/ore/x/example/dep"
	const myPath = "github.com/andrewhowdencom/ore/x/example/me"
	const depDir = "dep"
	const myDir = "."

	depAbs := filepath.Join(root, depDir)
	if err := os.MkdirAll(depAbs, 0o755); err != nil {
		t.Fatal(err)
	}
	scaffoldModule(t, depAbs, depPath)
	scaffoldModule(t, root, myPath)

	if err := addLocalReplaces(root, Module{Path: myPath, Dir: myDir}, []Module{{Path: depPath, Dir: depDir}}); err != nil {
		t.Fatal(err)
	}

	got := readFile(t, root, "go.mod")
	if !strings.Contains(got, "replace "+depPath+" => ./"+depDir) {
		t.Errorf("expected replace directive with ./ prefix, got:\n%s", got)
	}

	// Restore should drop the local replace entirely (no original to reinstate).
	if err := restoreReplaces(root, Module{Path: myPath, Dir: myDir}, []Module{{Path: depPath, Dir: depDir}}, nil); err != nil {
		t.Fatal(err)
	}

	got = readFile(t, root, "go.mod")
	if strings.Contains(got, "replace") {
		t.Errorf("expected no replace after restore, got:\n%s", got)
	}
}

func TestAddAndRestoreLocalReplaces_PreservesOriginal(t *testing.T) {
	root := t.TempDir()

	const depPath = "github.com/andrewhowdencom/ore/x/example/dep"
	const myPath = "github.com/andrewhowdencom/ore/x/example/me"

	depAbs := filepath.Join(root, "dep")
	if err := os.MkdirAll(depAbs, 0o755); err != nil {
		t.Fatal(err)
	}
	scaffoldModule(t, depAbs, depPath)

	// Pre-existing replace pointing at /some/old/path.
	writeFile(t, root, "go.mod",
		"module "+myPath+"\n\ngo 1.26\n\nreplace "+depPath+" => /some/old/path\n")

	// Add the local replace; pre-existing one is overwritten (we don't
	// preserve the originals at this layer — tidyWithLocalSource captures
	// them). Verify our replace took effect.
	if err := addLocalReplaces(root, Module{Path: myPath, Dir: "."}, []Module{{Path: depPath, Dir: "dep"}}); err != nil {
		t.Fatal(err)
	}
	got := readFile(t, root, "go.mod")
	if !strings.Contains(got, "replace "+depPath+" => ./dep") {
		t.Errorf("expected local replace with ./ prefix, got:\n%s", got)
	}
	if strings.Contains(got, "/some/old/path") {
		t.Errorf("expected pre-existing replace to be overwritten, got:\n%s", got)
	}

	// Now restore using a snapshot of the original Replace directive.
	if err := restoreReplaces(root, Module{Path: myPath, Dir: "."}, []Module{{Path: depPath, Dir: "dep"}}, map[string]*modfile.Replace{
		depPath: {Old: moduleVer(depPath), New: moduleVer("/some/old/path")},
	}); err != nil {
		t.Fatal(err)
	}

	got = readFile(t, root, "go.mod")
	if !strings.Contains(got, "replace "+depPath+" => /some/old/path") {
		t.Errorf("expected pre-existing replace restored, got:\n%s", got)
	}
	if strings.Contains(got, "replace "+depPath+" => dep") {
		t.Errorf("expected local replace dropped, got:\n%s", got)
	}
}

// TestTidyWithLocalSource_HappyPath exercises the full wrapper using a
// two-module fixture where the consumer imports the producer. After the
// call, the consumer's require is bumped to the target version, the
// consumer's main.go still resolves via the local replace, and no
// replaces remain in go.mod.
func TestTidyWithLocalSource_HappyPath(t *testing.T) {
	root := t.TempDir()

	const depPath = "github.com/andrewhowdencom/ore/x/example/dep"
	const myPath = "github.com/andrewhowdencom/ore/x/example/me"

	if err := os.MkdirAll(filepath.Join(root, "dep"), 0o755); err != nil {
		t.Fatal(err)
	}
	scaffoldModule(t, filepath.Join(root, "dep"), depPath)
	scaffoldModule(t, root, myPath, depPath)

	writeFile(t, root, "go.mod",
		"module "+myPath+"\n\ngo 1.26\n\nrequire "+depPath+" v0.0.1\n")

	targetVersions := map[string]string{
		depPath: "v0.1.0",
	}

	if err := tidyWithLocalSource(root,
		Module{Path: myPath, Dir: "."},
		[]Module{{Path: depPath, Dir: "dep"}},
		targetVersions,
	); err != nil {
		t.Fatalf("tidyWithLocalSource: %v", err)
	}

	got := readFile(t, root, "go.mod")
	if !strings.Contains(got, "require "+depPath+" v0.1.0") {
		t.Errorf("expected target require, got:\n%s", got)
	}
	if strings.Contains(got, "replace ") {
		t.Errorf("expected no replace in committed go.mod, got:\n%s", got)
	}
}

// TestTidyWithLocalSource_RollsBackOnTidyFailure confirms that when
// tidy fails, the consumer's go.mod and go.sum are restored to their
// pre-call state.
//
// We trigger the failure by having the consumer's source import a
// package that does not exist in the producer module. After the
// consumer's require is bumped to the producer's new version and tidy
// runs (with the local replace in place), tidy must resolve every
// package the consumer imports — and the missing one cannot be found
// in the local source.
func TestTidyWithLocalSource_RollsBackOnTidyFailure(t *testing.T) {
	root := t.TempDir()

	const depPath = "github.com/andrewhowdencom/ore/x/example/dep"
	const myPath = "github.com/andrewhowdencom/ore/x/example/me"

	if err := os.MkdirAll(filepath.Join(root, "dep"), 0o755); err != nil {
		t.Fatal(err)
	}
	scaffoldModule(t, filepath.Join(root, "dep"), depPath)
	scaffoldModule(t, root, myPath, depPath+"/no-such-package")

	writeFile(t, root, "go.mod",
		"module "+myPath+"\n\ngo 1.26\n\nrequire "+depPath+" v0.0.1\n")
	writeFile(t, root, "go.sum", "placeholder\n")

	goModBefore := readFile(t, root, "go.mod")
	goSumBefore := readFile(t, root, "go.sum")

	err := tidyWithLocalSource(root,
		Module{Path: myPath, Dir: "."},
		[]Module{{Path: depPath, Dir: "dep"}},
		map[string]string{depPath: "v0.1.0"},
	)
	if err == nil {
		t.Fatal("expected tidy to fail (consumer imports a non-existent package), got nil")
	}

	if got := readFile(t, root, "go.mod"); got != goModBefore {
		t.Errorf("go.mod was not restored after failure\nwant:\n%s\n\ngot:\n%s", goModBefore, got)
	}
	if got := readFile(t, root, "go.sum"); got != goSumBefore {
		t.Errorf("go.sum was not restored after failure\nwant:\n%s\n\ngot:\n%s", goSumBefore, got)
	}
}

func TestInThisRunDeps(t *testing.T) {
	const a, b, c = "github.com/andrewhowdencom/ore/x/a", "github.com/andrewhowdencom/ore/x/b", "github.com/andrewhowdencom/ore/x/c"

	targetPaths := map[string]bool{a: true, b: true, c: true}
	pathToMod := map[string]Module{
		a: {Path: a, Dir: "a"},
		b: {Path: b, Dir: "b"},
		c: {Path: c, Dir: "c"},
	}
	graph := map[string][]string{
		a: nil,
		b: {a},
		c: {a, b},
	}

	seen := map[string]bool{a: true}

	got := inThisRunDeps(Module{Path: b, Dir: "b"}, targetPaths, pathToMod, seen, graph)
	if len(got) != 1 || got[0].Path != a {
		t.Errorf("b should depend on a only; got %+v", got)
	}

	seen[b] = true
	got = inThisRunDeps(Module{Path: c, Dir: "c"}, targetPaths, pathToMod, seen, graph)
	if len(got) != 2 || got[0].Path != a || got[1].Path != b {
		t.Errorf("c should depend on a and b in order; got %+v", got)
	}

	// A non-target dep should not appear.
	targetPaths = map[string]bool{a: true, b: true}
	seen = map[string]bool{a: true}
	got = inThisRunDeps(Module{Path: b, Dir: "b"}, targetPaths, pathToMod, seen, graph)
	if len(got) != 1 || got[0].Path != a {
		t.Errorf("b should still see a (target); got %+v", got)
	}
}

// TestInThisRunDeps_TransitiveClosure verifies that inThisRunDeps
// returns the full transitive closure restricted to in-this-run
// modules, not just direct deps. This is required because tidy
// ignores replace directives in non-main modules, so for any
// transitive target dep we have to add the local replace to m's
// go.mod directly.
func TestInThisRunDeps_TransitiveClosure(t *testing.T) {
	const a, b, c, d = "github.com/andrewhowdencom/ore/x/a", "github.com/andrewhowdencom/ore/x/b", "github.com/andrewhowdencom/ore/x/c", "github.com/andrewhowdencom/ore/x/d"

	targetPaths := map[string]bool{a: true, b: true, c: true, d: true}
	pathToMod := map[string]Module{
		a: {Path: a, Dir: "a"},
		b: {Path: b, Dir: "b"},
		c: {Path: c, Dir: "c"},
		d: {Path: d, Dir: "d"},
	}
	graph := map[string][]string{
		// d -> c -> b -> a; a is a leaf.
		a: nil,
		b: {a},
		c: {b},
		d: {c},
	}

	seen := map[string]bool{a: true, b: true, c: true}

	got := inThisRunDeps(Module{Path: d, Dir: "d"}, targetPaths, pathToMod, seen, graph)
	if len(got) != 3 {
		t.Fatalf("d should see transitive closure {a, b, c}; got %+v", got)
	}
	wantPaths := map[string]bool{a: true, b: true, c: true}
	for _, dep := range got {
		if !wantPaths[dep.Path] {
			t.Errorf("unexpected dep in closure: %s", dep.Path)
		}
	}

	// A non-target transitive dep should be excluded from the result.
	const e = "github.com/andrewhowdencom/ore/x/e"
	targetPaths = map[string]bool{a: true, b: true, c: true, d: true}
	pathToMod[e] = Module{Path: e, Dir: "e"}
	graph[c] = []string{b, e}
	seen = map[string]bool{a: true, b: true, c: true}
	got = inThisRunDeps(Module{Path: d, Dir: "d"}, targetPaths, pathToMod, seen, graph)
	for _, dep := range got {
		if dep.Path == e {
			t.Errorf("non-target e should not appear in closure; got %+v", got)
		}
	}
}