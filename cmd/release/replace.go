package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/mod/modfile"
)

// tidyWithLocalSource runs the equivalent of `updateModuleDeps` followed by
// `go mod tidy` for module m, but first adds temporary `replace` directives
// in m's go.mod pointing each in-this-run dependency at its local source
// directory. The replaces are required because the new target versions
// (e.g. v0.5.0) have not yet been published to the module proxy: without
// them, tidy would either (a) fail to resolve packages whose old path was
// renamed in the new source, or (b) refuse to validate the graph because
// the pinned version is unknown to the proxy.
//
// Any pre-existing replace directives for these paths are saved before
// being overwritten and restored on success. go.mod and go.sum are
// snapshotted up front: on any error, both files are restored to the
// state they were in before tidyWithLocalSource was called.
//
// On success, m's go.mod carries the target-version requires (set by
// updateModuleDeps), with the local replaces dropped and any user
// replaces restored. go.sum reflects what tidy wrote during this call
// (the hashes for in-this-run modules are absent because they resolved
// locally; the proxy fills them in once the target versions are pushed).
func tidyWithLocalSource(root string, m Module, inThisRunDeps []Module, targetVersions map[string]string) error {
	gomodPath := filepath.Join(root, m.Dir, "go.mod")
	gosumPath := filepath.Join(root, m.Dir, "go.sum")

	gomodBackup, err := os.ReadFile(gomodPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", gomodPath, err)
	}
	gosumBackup, gosumErr := os.ReadFile(gosumPath)
	hasGosum := gosumErr == nil
	if gosumErr != nil && !os.IsNotExist(gosumErr) {
		return fmt.Errorf("read %s: %w", gosumPath, gosumErr)
	}

	success := false
	defer func() {
		if success {
			return
		}
		_ = os.WriteFile(gomodPath, gomodBackup, 0o644)
		if hasGosum {
			_ = os.WriteFile(gosumPath, gosumBackup, 0o644)
		} else {
			// tidy may have created go.sum; remove it so we leave the
			// tree exactly as we found it.
			_ = os.Remove(gosumPath)
		}
	}()

	// Save any user-authored replaces for the paths we're about to
	// overwrite, so we can restore them on the success path.
	fBackup, err := modfile.Parse(gomodPath, gomodBackup, nil)
	if err != nil {
		return fmt.Errorf("parse %s: %w", gomodPath, err)
	}
	originals := make(map[string]*modfile.Replace, len(inThisRunDeps))
	for _, dep := range inThisRunDeps {
		for _, r := range fBackup.Replace {
			if r.Old.Path == dep.Path {
				originals[dep.Path] = r
				break
			}
		}
	}

	if err := addLocalReplaces(root, m, inThisRunDeps); err != nil {
		return fmt.Errorf("add local replaces in %s: %w", m.Path, err)
	}

	if err := updateModuleDeps(root, m, targetVersions); err != nil {
		return fmt.Errorf("update deps in %s: %w", m.Path, err)
	}

	if err := runGoModTidy(root, m); err != nil {
		return fmt.Errorf("tidy in %s: %w", m.Path, err)
	}

	if err := restoreReplaces(root, m, inThisRunDeps, originals); err != nil {
		return fmt.Errorf("restore replaces in %s: %w", m.Path, err)
	}

	success = true
	return nil
}

// addLocalReplaces writes a `replace` directive in m's go.mod for each
// module in deps, pointing it at the local source on disk. Existing
// replaces for the same path are overwritten; the caller is responsible
// for capturing and restoring them.
//
// go.mod's parser accepts a local path on the right of a replace only
// when it is absolute, starts with "./", or starts with "../". Plain
// relative segments like "dep" are interpreted as module paths and
// rejected. filepath.Rel yields "dep" for a same-parent sibling; we
// rewrite that to "./dep" so the modfile round-trips.
func addLocalReplaces(root string, m Module, deps []Module) error {
	if len(deps) == 0 {
		return nil
	}
	gomodPath := filepath.Join(root, m.Dir, "go.mod")
	data, err := os.ReadFile(gomodPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", gomodPath, err)
	}
	f, err := modfile.Parse(gomodPath, data, nil)
	if err != nil {
		return fmt.Errorf("parse %s: %w", gomodPath, err)
	}
	for _, dep := range deps {
		rel, err := filepath.Rel(filepath.Join(root, m.Dir), filepath.Join(root, dep.Dir))
		if err != nil {
			return fmt.Errorf("rel path from %s to %s: %w", m.Dir, dep.Dir, err)
		}
		if rel == "." {
			// m and dep share the same directory; nothing to replace.
			continue
		}
		if !strings.HasPrefix(rel, "./") && !strings.HasPrefix(rel, "../") && !strings.HasPrefix(rel, "/") {
			rel = "./" + rel
		}
		if err := f.AddReplace(dep.Path, "", rel, ""); err != nil {
			return fmt.Errorf("add replace %s => %s: %w", dep.Path, rel, err)
		}
	}
	f.Cleanup()
	return os.WriteFile(gomodPath, modfile.Format(f.Syntax), 0o644)
}

// restoreReplaces removes the replaces added by addLocalReplaces (for
// each path in deps) and reinstates the originals supplied by the
// caller. A path with no original is dropped entirely.
func restoreReplaces(root string, m Module, deps []Module, originals map[string]*modfile.Replace) error {
	if len(deps) == 0 {
		return nil
	}
	gomodPath := filepath.Join(root, m.Dir, "go.mod")
	data, err := os.ReadFile(gomodPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", gomodPath, err)
	}
	f, err := modfile.Parse(gomodPath, data, nil)
	if err != nil {
		return fmt.Errorf("parse %s: %w", gomodPath, err)
	}
	for _, dep := range deps {
		if err := f.DropReplace(dep.Path, ""); err != nil {
			return fmt.Errorf("drop replace %s: %w", dep.Path, err)
		}
		if original, ok := originals[dep.Path]; ok {
			if err := f.AddReplace(dep.Path, original.Old.Version, original.New.Path, original.New.Version); err != nil {
				return fmt.Errorf("restore replace %s => %s: %w", dep.Path, original.New, err)
			}
		}
	}
	f.Cleanup()
	return os.WriteFile(gomodPath, modfile.Format(f.Syntax), 0o644)
}

// inThisRunDeps returns, for module m, the set of ore-internal modules
// being released in this run that are reachable from m via the
// dependency graph (i.e. the in-this-run portion of m's transitive
// closure).
//
// The release tool's `go mod tidy` runs with GOWORK=off, which means
// only the main module's `replace` directives take effect; replace
// directives in dependency modules are silently ignored. So when
// processing m, the tool must add a local `replace` for every
// in-this-run module that m might pull in — direct deps as well as
// transitive ones — otherwise tidy would attempt to download the
// (unpublished) new versions from the module proxy.
//
// seen should be a set updated as the caller iterates through the
// topological order; inThisRunDeps is invoked with the set as it stands
// at the start of m's processing. The topological sort guarantees that
// every reachable in-this-run module has already been seen.
func inThisRunDeps(
	m Module,
	targetPaths map[string]bool,
	pathToMod map[string]Module,
	seen map[string]bool,
	graph map[string][]string,
) []Module {
	visited := make(map[string]bool)
	var walk func(path string)
	walk = func(path string) {
		if visited[path] {
			return
		}
		visited[path] = true
		for _, dep := range graph[path] {
			walk(dep)
		}
	}
	walk(m.Path)

	// Stable order: process the closure in lexicographic path order so
	// that the `replace` directives written into go.mod have a
	// deterministic shape (helpful for tests and for diffs).
	paths := make([]string, 0, len(visited))
	for p := range visited {
		if p == m.Path {
			continue
		}
		if !targetPaths[p] {
			continue
		}
		if !seen[p] {
			continue
		}
		paths = append(paths, p)
	}
	sort.Strings(paths)

	deps := make([]Module, 0, len(paths))
	for _, p := range paths {
		if dep, ok := pathToMod[p]; ok {
			deps = append(deps, dep)
		}
	}
	return deps
}