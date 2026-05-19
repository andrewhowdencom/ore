package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/andrewhowdencom/ore/x/conduit"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRun(t *testing.T) {
	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "out.md")

	descriptors := []conduit.Descriptor{
		{
			Name:        "TestA",
			Description: "Test conduit A",
			Capabilities: []conduit.Capability{
				conduit.CapEventSource,
				conduit.CapRenderMarkdown,
			},
		},
		{
			Name:        "TestB",
			Description: "Test conduit B",
			Capabilities: []conduit.Capability{
				conduit.CapEventSource,
				conduit.CapShowStatus,
			},
		},
	}

	err := run(outPath, "Test Matrix", descriptors)
	require.NoError(t, err)

	data, err := os.ReadFile(outPath)
	require.NoError(t, err)
	content := string(data)

	assert.Contains(t, content, "# Test Matrix")
	assert.Contains(t, content, "| Capability | TestA | TestB |")
	assert.Contains(t, content, "|------------|----------|----------|")
	assert.Contains(t, content, "| event-source | ✅ | ✅ |")
	assert.Contains(t, content, "| render-markdown | ✅ | ❌ |")
	assert.Contains(t, content, "| show-status | ❌ | ✅ |")
}
