package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// bumpFromDiff inspects the changes in `dir` between `tag` and HEAD and returns
// Major if any non-test .go file was deleted or renamed within the module.
//
// An empty `tag` means the module has never been tagged, so there is no
// prior API surface to break; this returns None.
//
// `excludeDirs` lists sibling module directories that should be ignored when
// `dir == "."` (i.e., when querying the root module's diff). The root's diff
// spans the whole repository, but submodules own the files under their own
// directories — those changes belong to the submodule's release, not the
// root's. Without this filter, a deletion in `x/conduit/foo.go` would falsely
// register as a root breaking change.
//
// This is a deliberately conservative mechanical check: any .go file that
// disappears from the module's directory is treated as a potential breaking
// change. The dominant case this catches is a package rename — for example,
// `session/foo.go` renamed to `junk/foo.go` — which would otherwise look
// additive from a commit-message perspective (e.g., a `refactor:` commit that
// just happens to delete every file under `session/` and re-create them
// under `junk/`).
//
// This check does NOT catch changes to exported declarations that remain on
// disk under the same path (e.g., a function signature change). For those the
// author must continue to use the Conventional Commits `!` or BREAKING CHANGE
// indicators, or a `feat:` (Minor) commit.
func bumpFromDiff(root, dir, tag string, excludeDirs []string) (Bump, error) {
	if tag == "" {
		return None, nil
	}
	revRange := tag + "..HEAD"

	// We query the whole repo rather than passing `dir` as a git pathspec,
	// because pathspec semantics for renames are subtle and a misbehaving
	// pathspec could miss a file that moved across the pathspec boundary.
	// Filtering by `dir` ourselves afterwards is straightforward and robust.
	args := []string{"-C", root, "diff", "--name-status", "--diff-filter=DR", revRange}
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		return None, fmt.Errorf("git diff deletions/renames for %s since %s: %w", dir, revRange, err)
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// Format: "D<TAB><path>" or "R<NN><TAB><oldpath><TAB><newpath>".
		parts := strings.Split(line, "\t")
		if len(parts) < 2 {
			continue
		}
		var oldPath string
		switch {
		case strings.HasPrefix(parts[0], "D"):
			oldPath = parts[1]
		case strings.HasPrefix(parts[0], "R"):
			if len(parts) < 3 {
				continue
			}
			oldPath = parts[1]
		default:
			continue
		}
		if !inModuleDir(oldPath, dir, excludeDirs) {
			continue
		}
		if strings.HasSuffix(oldPath, ".go") && !strings.HasSuffix(oldPath, "_test.go") {
			return Major, nil
		}
	}
	return None, nil
}

// inModuleDir reports whether `path` belongs to the module rooted at `dir`.
// For nested modules (`dir != "."`) the path must equal `dir` or start with
// `dir + "/"`. For the root module (`dir == "."`) the path is accepted unless
// it falls under any of the `excludeDirs` (sibling submodules).
func inModuleDir(path, dir string, excludeDirs []string) bool {
	if path == "" {
		return false
	}
	if dir != "." {
		return path == dir || strings.HasPrefix(path, dir+"/")
	}
	for _, ex := range excludeDirs {
		if path == ex || strings.HasPrefix(path, ex+"/") {
			return false
		}
	}
	return true
}
