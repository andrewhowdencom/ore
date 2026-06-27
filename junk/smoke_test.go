package junk

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSmoke_AffectedThreadFile points a JSONStore at the actual
// thread file that motivated issue #453 (the one whose kind
// JSON unmarshal failed under the old artifactRegistry). The path
// is supplied via the WORKSHOP_SMOKE_THREAD environment variable
// so the test does not depend on a particular user's filesystem.
//
// Set the env var to the absolute path of a thread .json file
// to enable this test. Without it, the test is skipped. This makes
// the test useful as a developer smoke check without making it a
// CI requirement.
func TestSmoke_AffectedThreadFile(t *testing.T) {
	path := os.Getenv("WORKSHOP_SMOKE_THREAD")
	if path == "" {
		t.Skip("WORKSHOP_SMOKE_THREAD not set; skipping smoke test")
	}

	// The env var points at a thread file; JSONStore wants a
	// directory, so use the parent.
	dir := filepath.Dir(path)
	store, err := NewJSONStore(dir)
	require.NoError(t, err)

	id := filepath.Base(path)
	id = id[:len(id)-len(filepath.Ext(id))]

	thr, err := store.Get(id)
	require.NoError(t, err, "Get should succeed for a file whose kinds are now registered")
	assert.Equal(t, id, thr.ID)
	assert.NotEmpty(t, thr.State.Turns(), "thread should have at least one turn after loading")
}