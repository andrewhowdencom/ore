package main

import (
	"fmt"
	"strings"
)

func runAll(dryRun bool, args []string) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}

	if !dryRun {
		branch, err := currentBranch(root)
		if err != nil {
			return err
		}
		if branch != "main" {
			return fmt.Errorf("not on main branch (currently on %q); checkout main before releasing", branch)
		}
	}

	modules, err := discoverModules(root)
	if err != nil {
		return err
	}

	// Build maps of existing tags and versions.
	allVersions := make(map[string]string)
	latestTagMap := make(map[string]string)
	for _, m := range modules {
		tag, err := latestTag(root, m.Dir)
		if err != nil {
			return err
		}
		latestTagMap[m.Path] = tag
		version := ""
		if tag != "" {
			if m.Dir == "." {
				version = tag
			} else {
				version = strings.TrimPrefix(tag, m.Dir+"/")
			}
		}
		allVersions[m.Path] = version
	}

	graph, err := buildDependencyGraph(root, modules)
	if err != nil {
		return err
	}

	// Determine changed modules and target versions.
	type target struct {
		module  Module
		current string
		version string
		bump    Bump
		count   int
	}
	var targets []target

	for _, m := range modules {
		tag := latestTagMap[m.Path]
		commits, err := commitsSinceTag(root, tag)
		if err != nil {
			return err
		}
		mapped := mapCommitsToModules(commits, modules)
		moduleCommits := mapped[m.Path]
		if len(moduleCommits) == 0 {
			continue
		}
		var msgs []string
		for _, c := range moduleCommits {
			msgs = append(msgs, c.Message)
		}
		bump := bumpType(msgs)
		version, err := nextVersion(allVersions[m.Path], bump)
		if err != nil {
			return err
		}
		targets = append(targets, target{
			module:  m,
			current: allVersions[m.Path],
			version: version,
			bump:    bump,
			count:   len(moduleCommits),
		})
	}

	if len(targets) == 0 {
		fmt.Println("No modules with unreleased changes.")
		return nil
	}

	// Print release plan summary.
	fmt.Printf("Releasing %d module(s):\n", len(targets))
	for _, t := range targets {
		if t.current == "" {
			fmt.Printf("  %s  (none) → %s  (%s, %d commits)\n", t.module.Path, t.version, t.bump, t.count)
		} else {
			fmt.Printf("  %s  %s → %s  (%s, %d commits)\n", t.module.Path, t.current, t.version, t.bump, t.count)
		}
	}
	fmt.Println()

	changedModules := make([]Module, len(targets))
	for i, t := range targets {
		changedModules[i] = t.module
	}
	sorted, err := topologicalSort(changedModules, graph)
	if err != nil {
		return err
	}

	// Snapshot current versions before we override with targets.
	currentVersions := make(map[string]string)
	for k, v := range allVersions {
		currentVersions[k] = v
	}

	// Override allVersions with target versions for the release phase.
	for _, t := range targets {
		allVersions[t.module.Path] = t.version
	}

	// Pre-flight: tidy go.mod with current (already-resolvable) versions.
	if !dryRun {
		fmt.Println("Pre-flight: validating go mod tidy...")
		for _, m := range sorted {
			fmt.Printf("  %s: updating dependencies to current versions...\n", m.Path)
			if err := updateModuleDeps(root, m, currentVersions); err != nil {
				return fmt.Errorf("pre-flight update deps for %s: %w", m.Path, err)
			}
			fmt.Printf("  %s: go mod tidy...\n", m.Path)
			if err := runGoModTidy(root, m); err != nil {
				return fmt.Errorf("pre-flight go mod tidy for %s: %w\nRun 'git checkout -- .' to revert changes.", m.Path, err)
			}
		}
		fmt.Println("Pre-flight passed.")
		fmt.Println()
	} else {
		fmt.Println("[dry-run] Skipping pre-flight go mod tidy (no file changes).")
		fmt.Println()
	}

	// Release: bump to target versions, commit and tag each module.
	for _, m := range sorted {
		var version string
		for _, t := range targets {
			if t.module.Path == m.Path {
				version = t.version
				break
			}
		}
		if !dryRun {
			fmt.Printf("  %s: updating dependencies to target versions...\n", m.Path)
			if err := updateModuleDeps(root, m, allVersions); err != nil {
				return err
			}
		}
		fmt.Printf("Releasing %s %s...\n", m.Path, version)
		if err := stageAndCommit(root, m, version, dryRun); err != nil {
			return err
		}
		if err := createTag(root, m, version, dryRun); err != nil {
			return err
		}
	}

	fmt.Println("Pushing to origin/main...")
	if err := push(root, dryRun); err != nil {
		return err
	}

	return nil
}
