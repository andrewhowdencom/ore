package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindOreModuleRoot(t *testing.T) {
	t.Run("finds go.mod in start dir", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/andrewhowdencom/ore\n\ngo 1.26.2\n"), 0644))

		got, err := FindOreModuleRoot(dir)
		require.NoError(t, err)
		assert.Equal(t, dir, got)
	})

	t.Run("finds go.mod in parent dir", func(t *testing.T) {
		root := t.TempDir()
		sub := filepath.Join(root, "sub", "nested")
		require.NoError(t, os.MkdirAll(sub, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/andrewhowdencom/ore\n"), 0644))

		got, err := FindOreModuleRoot(sub)
		require.NoError(t, err)
		assert.Equal(t, root, got)
	})

	t.Run("wrong module name", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/other\n"), 0644))

		_, err := FindOreModuleRoot(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot find ore module root")
	})

	t.Run("no go.mod", func(t *testing.T) {
		dir := t.TempDir()

		_, err := FindOreModuleRoot(dir)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot find ore module root")
	})
}
