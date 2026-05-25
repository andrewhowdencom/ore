package main

import (
	"fmt"
	"os/exec"
	"strings"

	"golang.org/x/mod/semver"
)

// latestTag returns the highest valid semver tag for a module.
// For the root module (dir == ".") it searches tags matching "v*".
// For submodules it searches tags matching "<dir>/v*".
// Returns an empty string if no matching tag exists.
func latestTag(root, moduleDir string) (string, error) {
	var prefix string
	if moduleDir != "." && moduleDir != "" {
		prefix = moduleDir + "/"
	}
	cmd := exec.Command("git", "-C", root, "tag", "--list", prefix+"v*", "--sort=-v:refname")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("list tags: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for _, line := range lines {
		tag := strings.TrimSpace(line)
		if tag == "" {
			continue
		}
		version := strings.TrimPrefix(tag, prefix)
		if semver.IsValid(version) {
			return tag, nil
		}
	}
	return "", nil
}
