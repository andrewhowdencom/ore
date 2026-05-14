package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// FindOreModuleRoot walks up from startDir looking for a go.mod whose
// module declaration is github.com/andrewhowdencom/ore. It returns the
// absolute path of the directory containing that go.mod.
func FindOreModuleRoot(startDir string) (string, error) {
	dir, err := filepath.Abs(startDir)
	if err != nil {
		return "", fmt.Errorf("resolve start dir: %w", err)
	}

	for {
		modPath := filepath.Join(dir, "go.mod")
		data, err := os.ReadFile(modPath)
		if err == nil {
			lines := strings.Split(string(data), "\n")
			for _, line := range lines {
				trimmed := strings.TrimSpace(line)
				if trimmed == "" || strings.HasPrefix(trimmed, "//") {
					continue
				}
				if strings.HasPrefix(trimmed, "module github.com/andrewhowdencom/ore") {
					return dir, nil
				}
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("cannot find ore module root: no go.mod with module github.com/andrewhowdencom/ore found in %s or any parent directory", startDir)
}
