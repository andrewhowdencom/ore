package main

import (
	"fmt"
	"strings"

	"golang.org/x/mod/semver"
)

// Bump represents the semver impact of a set of commits.
type Bump int

const (
	None Bump = iota
	Patch
	Minor
	Major
)

func (b Bump) String() string {
	switch b {
	case None:
		return "none"
	case Patch:
		return "patch"
	case Minor:
		return "minor"
	case Major:
		return "major"
	default:
		return "unknown"
	}
}

// bumpType analyses a slice of commit messages and returns the highest semver
// bump required according to the Conventional Commits specification.
//
// Rules:
//   - Any commit containing "BREAKING CHANGE:" or "BREAKING-CHANGE:" in its
//     body, or a "!" immediately before the ":" in the subject line → Major.
//   - A commit whose type is exactly "feat" (with optional scope) → Minor.
//   - All other commits → Patch (so nothing is silently dropped).
func bumpType(msgs []string) Bump {
	var b Bump = None
	for _, msg := range msgs {
		switch {
		case isBreaking(msg):
			b = maxBump(b, Major)
		case isFeat(msg):
			b = maxBump(b, Minor)
		default:
			b = maxBump(b, Patch)
		}
	}
	return b
}

func maxBump(a, b Bump) Bump {
	if b > a {
		return b
	}
	return a
}

func isBreaking(msg string) bool {
	// Footer / body indicators (case-sensitive per the spec).
	if strings.Contains(msg, "BREAKING CHANGE:") || strings.Contains(msg, "BREAKING-CHANGE:") {
		return true
	}
	// Subject-line indicator: "!" must be the last character before ":".
	idx := strings.Index(msg, ":")
	if idx > 0 && msg[idx-1] == '!' {
		return true
	}
	return false
}

func isFeat(msg string) bool {
	idx := strings.Index(msg, ":")
	if idx <= 0 {
		return false
	}
	prefix := msg[:idx]

	// Strip optional breaking indicator (handled by isBreaking).
	prefix = strings.TrimSuffix(prefix, "!")

	// Strip optional scope.
	if open := strings.Index(prefix, "("); open >= 0 {
		if close := strings.Index(prefix[open:], ")"); close >= 0 {
			prefix = prefix[:open] + prefix[open+close+1:]
		}
	}

	return prefix == "feat"
}

// nextVersion returns the incremented version for a given current version and
// bump type.  An empty current version means the module has never been tagged;
// in that case v0.1.0 is returned.
func nextVersion(current string, bump Bump) (string, error) {
	if bump == None {
		return current, nil
	}
	if current == "" {
		return "v0.1.0", nil
	}
	if !semver.IsValid(current) {
		return "", fmt.Errorf("invalid current version %q", current)
	}

	var major, minor, patch int
	n, err := fmt.Sscanf(current, "v%d.%d.%d", &major, &minor, &patch)
	if err != nil || n != 3 {
		return "", fmt.Errorf("parse version %q: got %d fields, err=%v", current, n, err)
	}

	switch bump {
	case Patch:
		patch++
	case Minor:
		minor++
		patch = 0
	case Major:
		major++
		minor = 0
		patch = 0
	}

	return fmt.Sprintf("v%d.%d.%d", major, minor, patch), nil
}
