package main

import (
	"os"
	"testing"

	"github.com/andrewhowdencom/ore/agent"
	"github.com/andrewhowdencom/ore/cognitive"
	"github.com/andrewhowdencom/ore/provider"
)

// newTestAgent builds an ephemeral agent for tests, wired to the
// given provider and pattern. The agent is closed via t.Cleanup.
func newTestAgent(t *testing.T, p provider.Provider, pat cognitive.Pattern) *agent.Agent {
	t.Helper()
	a := agent.New("test",
		agent.WithProvider(p),
		agent.WithPattern(pat),
	)
	t.Cleanup(func() { _ = a.Close() })
	return a
}

// writeFile is a small helper used by tests that need to drop a
// fixture on disk.
func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o600)
}
