package main

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

// Commit represents a single Git commit with its message and changed files.
type Commit struct {
	SHA     string
	Message string
	Files   []string
}

// commitsSinceTag returns all commits after the given tag up to HEAD.
// If tag is empty, all commits reachable from HEAD are returned.
func commitsSinceTag(root, tag string) ([]Commit, error) {
	var revRange string
	if tag == "" {
		revRange = "HEAD"
	} else {
		revRange = tag + "..HEAD"
	}
	cmd := exec.Command("git", "-C", root, "rev-list", revRange)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("rev-list %s: %w", revRange, err)
	}
	shas := strings.Split(strings.TrimSpace(string(out)), "\n")
	var commits []Commit
	for _, sha := range shas {
		sha = strings.TrimSpace(sha)
		if sha == "" {
			continue
		}
		msg, err := commitMessage(root, sha)
		if err != nil {
			return nil, err
		}
		files, err := commitFiles(root, sha)
		if err != nil {
			return nil, err
		}
		commits = append(commits, Commit{
			SHA:     sha,
			Message: msg,
			Files:   files,
		})
	}
	return commits, nil
}

func commitMessage(root, sha string) (string, error) {
	cmd := exec.Command("git", "-C", root, "log", "-1", "--format=%s", sha)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("log %s: %w", sha, err)
	}
	return strings.TrimSpace(string(out)), nil
}

func commitFiles(root, sha string) ([]string, error) {
	cmd := exec.Command("git", "-C", root, "diff-tree", "--no-commit-id", "--name-only", "-r", sha)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("diff-tree %s: %w", sha, err)
	}
	var files []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			files = append(files, line)
		}
	}
	return files, nil
}

// mapCommitsToModules uses a longest-prefix file-path heuristic to determine
// which module(s) each commit affects. A commit can affect multiple modules if
// it changes files in more than one module directory. Files that do not match
// any submodule directory are assigned to the root module.
func mapCommitsToModules(commits []Commit, modules []Module) map[string][]Commit {
	// Sort modules by directory length descending so nested modules (e.g.
	// x/tool/bash) take precedence over their parents (e.g. x/tool).
	sorted := make([]Module, len(modules))
	copy(sorted, modules)
	sort.Slice(sorted, func(i, j int) bool {
		return len(sorted[i].Dir) > len(sorted[j].Dir)
	})

	result := make(map[string][]Commit)

	for _, c := range commits {
		affected := make(map[string]bool)
		for _, f := range c.Files {
			assigned := false
			for _, m := range sorted {
				if m.Dir == "." {
					continue
				}
				if f == m.Dir || strings.HasPrefix(f, m.Dir+"/") {
					affected[m.Path] = true
					assigned = true
					break
				}
			}
			if !assigned {
				for _, m := range modules {
					if m.Dir == "." {
						affected[m.Path] = true
						break
					}
				}
			}
		}
		for modPath := range affected {
			result[modPath] = append(result[modPath], c)
		}
	}
	return result
}
