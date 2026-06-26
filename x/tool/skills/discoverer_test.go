package skills

import (
	"context"
	"embed"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed testdata/embed
var testEmbedFS embed.FS

func TestFSDiscoverer_HappyPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "conduit")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: conduit
description: Implements a new ore I/O conduit package.
---

# Ore Conduit

Test content.
`), 0o644))

	d := NewFSDiscoverer(filepath.Join(dir, "skills"))
	meta, err := d.Discover(context.Background())
	require.NoError(t, err)
	assert.Len(t, meta, 1)
	assert.Equal(t, "conduit", meta[0].Name)
	assert.Equal(t, "Implements a new ore I/O conduit package.", meta[0].Description)

	content, err := d.Read(context.Background(), "conduit", "")
	require.NoError(t, err)
	assert.Contains(t, content, "# Ore Conduit")
}

func TestFSDiscoverer_MissingSkillMD(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "not-a-skill.txt"), []byte("hello"), 0o644))

	d := NewFSDiscoverer(dir)
	meta, err := d.Discover(context.Background())
	require.NoError(t, err)
	assert.Empty(t, meta)
}

func TestFSDiscoverer_InvalidFrontmatter(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "bad")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
not-name: missing
---

# Bad
`), 0o644))

	d := NewFSDiscoverer(filepath.Join(dir, "skills"))
	meta, err := d.Discover(context.Background())
	require.NoError(t, err)
	assert.Empty(t, meta)
}

func TestFSDiscoverer_MissingNameField(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "noname")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
description: no name here
---

# No Name
`), 0o644))

	d := NewFSDiscoverer(filepath.Join(dir, "skills"))
	meta, err := d.Discover(context.Background())
	require.NoError(t, err)
	assert.Empty(t, meta)
}

func TestFSDiscoverer_MissingDescriptionField(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "nodescription")
	require.NoError(t, os.MkdirAll(skillDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(`---
name: some-name
---

# No Description
`), 0o644))

	d := NewFSDiscoverer(filepath.Join(dir, "skills"))
	meta, err := d.Discover(context.Background())
	require.NoError(t, err)
	assert.Empty(t, meta)
}

func TestFSDiscoverer_ReadNonexistent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	d := NewFSDiscoverer(dir)
	_, err := d.Read(context.Background(), "missing", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestFSDiscoverer_DuplicateNamesFirstWins(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	skillDir1 := filepath.Join(dir, "skills", "a")
	skillDir2 := filepath.Join(dir, "skills", "b")
	require.NoError(t, os.MkdirAll(skillDir1, 0o755))
	require.NoError(t, os.MkdirAll(skillDir2, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir1, "SKILL.md"), []byte(`---
name: duplicate
description: first one
---
`), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(skillDir2, "SKILL.md"), []byte(`---
name: duplicate
description: second one
---
`), 0o644))

	d := NewFSDiscoverer(filepath.Join(dir, "skills"))
	meta, err := d.Discover(context.Background())
	require.NoError(t, err)
	assert.Len(t, meta, 1)
	assert.Equal(t, "first one", meta[0].Description)
}

func TestEmbeddedDiscoverer_HappyPath(t *testing.T) {
	t.Parallel()
	d := NewEmbeddedDiscoverer(testEmbedFS, "testdata/embed/skills")
	meta, err := d.Discover(context.Background())
	require.NoError(t, err)
	assert.Len(t, meta, 1)
	assert.Equal(t, "go", meta[0].Name)
	assert.Equal(t, "Guidelines for Go development, testing, and tooling.", meta[0].Description)

	content, err := d.Read(context.Background(), "go", "")
	require.NoError(t, err)
	assert.Contains(t, content, "# Go")
}

func TestEmbeddedDiscoverer_ReadNonexistent(t *testing.T) {
	t.Parallel()
	d := NewEmbeddedDiscoverer(testEmbedFS, "testdata/embed/skills")
	_, err := d.Read(context.Background(), "missing", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}
