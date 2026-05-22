package skills

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"sync"
)

// EmbeddedDiscoverer scans an embed.FS for SKILL.md files.
type EmbeddedDiscoverer struct {
	FS    embed.FS
	Root  string
	paths map[string]string
	mu    sync.RWMutex
}

// NewEmbeddedDiscoverer creates an embed.FS-backed Discoverer rooted at root.
func NewEmbeddedDiscoverer(fs embed.FS, root string) *EmbeddedDiscoverer {
	return &EmbeddedDiscoverer{
		FS:    fs,
		Root:  root,
		paths: make(map[string]string),
	}
}

// Discover walks the embedded directory tree and returns metadata for every
// valid SKILL.md found. Malformed files are skipped with a logged warning.
func (d *EmbeddedDiscoverer) Discover(ctx context.Context) ([]SkillMeta, error) {
	paths := make(map[string]string)
	var result []SkillMeta

	root := d.Root
	if root == "" {
		root = "."
	}

	err := fs.WalkDir(d.FS, root, func(filePath string, entry fs.DirEntry, err error) error {
		if err != nil {
			slog.Warn("skipping unreadable path during embedded skill discovery", "path", filePath, "error", err)
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if path.Base(filePath) != "SKILL.md" {
			return nil
		}

		meta, err := parseSkillFile(filePath, func(p string) ([]byte, error) {
			return d.FS.ReadFile(p)
		})
		if err != nil {
			slog.Warn("skipping malformed embedded SKILL.md", "path", filePath, "error", err)
			return nil
		}

		if _, exists := paths[meta.Name]; exists {
			slog.Warn("duplicate skill name in embedded discoverer, skipping", "name", meta.Name, "path", filePath)
			return nil
		}

		paths[meta.Name] = filePath
		result = append(result, meta)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk embedded directory %q: %w", root, err)
	}

	d.mu.Lock()
	d.paths = paths
	d.mu.Unlock()

	return result, nil
}

// Read returns the full SKILL.md content for the named skill.
func (d *EmbeddedDiscoverer) Read(ctx context.Context, name string) (string, error) {
	d.mu.RLock()
	filePath, ok := d.paths[name]
	d.mu.RUnlock()
	if !ok {
		return "", fmt.Errorf("skill %q not found in embedded discoverer", name)
	}

	data, err := d.FS.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read embedded skill file %q: %w", filePath, err)
	}
	return string(data), nil
}
