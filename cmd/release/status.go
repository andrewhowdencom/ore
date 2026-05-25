package main

import (
	"fmt"
	"strings"
)

// runStatus discovers all modules, determines their latest tags, maps
// unreleased commits to each module, and prints a release preview.
func runStatus(dryRun bool, args []string) error {
	root, err := repoRoot()
	if err != nil {
		return err
	}

	modules, err := discoverModules(root)
	if err != nil {
		return err
	}

	type line struct {
		path    string
		current string
		next    string
		bump    Bump
		count   int
	}
	var lines []line

	for _, m := range modules {
		tag, err := latestTag(root, m.Dir)
		if err != nil {
			return fmt.Errorf("latest tag for %s: %w", m.Path, err)
		}

		current := ""
		if tag != "" {
			if m.Dir == "." {
				current = tag
			} else {
				current = strings.TrimPrefix(tag, m.Dir+"/")
			}
		}

		commits, err := commitsSinceTag(root, tag, newCommitCache())
		if err != nil {
			return fmt.Errorf("commits since %q for %s: %w", tag, m.Path, err)
		}

		mapped := mapCommitsToModules(commits, modules)
		moduleCommits := mapped[m.Path]

		var msgs []string
		for _, c := range moduleCommits {
			msgs = append(msgs, c.Message)
		}
		bump := bumpType(msgs)

		var next string
		if len(moduleCommits) > 0 {
			next, err = nextVersion(current, bump)
			if err != nil {
				return fmt.Errorf("next version for %s: %w", m.Path, err)
			}
		}

		lines = append(lines, line{
			path:    m.Path,
			current: current,
			next:    next,
			bump:    bump,
			count:   len(moduleCommits),
		})
	}

	// Align paths for readability.
	maxPath := 0
	for _, l := range lines {
		if n := len(l.path); n > maxPath {
			maxPath = n
		}
	}

	for _, l := range lines {
		if l.count == 0 {
			fmt.Printf("%-*s  %s  (no unreleased changes)\n", maxPath, l.path, l.current)
		} else if l.current == "" {
			fmt.Printf("%-*s  (none) → %s  (%s, %d commits)\n", maxPath, l.path, l.next, l.bump, l.count)
		} else {
			fmt.Printf("%-*s  %s → %s  (%s, %d commits)\n", maxPath, l.path, l.current, l.next, l.bump, l.count)
		}
	}

	return nil
}
