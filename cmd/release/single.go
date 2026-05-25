package main

import (
	"fmt"
	"strings"
)

func runRelease(path string, dryRun bool, args []string) error {
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

	// Find the module matching the input path (import path or directory).
	var targetModule *Module
	for i := range modules {
		if modules[i].Path == path || modules[i].Dir == path {
			targetModule = &modules[i]
			break
		}
	}
	if targetModule == nil {
		return fmt.Errorf("no module matches %q", path)
	}
	m := *targetModule

	// Build allVersions with existing tags.
	allVersions := make(map[string]string)
	for _, mod := range modules {
		tag, err := latestTag(root, mod.Dir)
		if err != nil {
			return err
		}
		version := ""
		if tag != "" {
			if mod.Dir == "." {
				version = tag
			} else {
				version = strings.TrimPrefix(tag, mod.Dir+"/")
			}
		}
		allVersions[mod.Path] = version
	}

	// Determine target version for this module.
	tag, err := latestTag(root, m.Dir)
	if err != nil {
		return err
	}
	commits, err := commitsSinceTag(root, tag)
	if err != nil {
		return err
	}
	mapped := mapCommitsToModules(commits, modules)
	moduleCommits := mapped[m.Path]

	var msgs []string
	for _, c := range moduleCommits {
		msgs = append(msgs, c.Message)
	}
	bump := bumpType(msgs)
	version, err := nextVersion(allVersions[m.Path], bump)
	if err != nil {
		return err
	}

	fmt.Printf("Releasing %s %s (%s, %d commits)...\n", m.Path, version, bump, len(moduleCommits))

	// Update deps and tidy.
	allVersions[m.Path] = version
	if err := updateModuleDeps(root, m, allVersions); err != nil {
		return err
	}
	if err := runGoModTidy(root, m); err != nil {
		return err
	}

	// Commit and tag.
	if err := stageAndCommit(root, m, version, dryRun); err != nil {
		return err
	}
	if err := createTag(root, m, version, dryRun); err != nil {
		return err
	}

	// Push.
	fmt.Println("Pushing to origin/main...")
	if err := push(root, dryRun); err != nil {
		return err
	}

	return nil
}
