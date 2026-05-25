package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/semver"
)

// updateModuleDeps scans a module's go.mod for ore dependencies and updates
// them to the latest tagged version if a newer one exists.  It writes the
// file only when at least one dependency is changed.
func updateModuleDeps(root string, m Module, latestTags map[string]string) error {
	gomodPath := filepath.Join(root, m.Dir, "go.mod")
	data, err := os.ReadFile(gomodPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", gomodPath, err)
	}

	f, err := modfile.Parse(gomodPath, data, nil)
	if err != nil {
		return fmt.Errorf("parse %s: %w", gomodPath, err)
	}

	updated := false
	for _, r := range f.Require {
		if strings.HasPrefix(r.Mod.Path, "github.com/andrewhowdencom/ore") {
			latest, ok := latestTags[r.Mod.Path]
			if !ok || latest == "" {
				continue
			}
			if semver.Compare(latest, r.Mod.Version) > 0 {
				if err := f.AddRequire(r.Mod.Path, latest); err != nil {
					return fmt.Errorf("add require %s@%s: %w", r.Mod.Path, latest, err)
				}
				updated = true
			}
		}
	}

	if updated {
		f.Cleanup()
		out := modfile.Format(f.Syntax)
		if err := os.WriteFile(gomodPath, out, 0644); err != nil {
			return fmt.Errorf("write %s: %w", gomodPath, err)
		}
	}

	return nil
}
