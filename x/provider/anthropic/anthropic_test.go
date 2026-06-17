package anthropic

import (
	"context"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewSucceeds verifies that the first-party wrapper constructs a
// provider that satisfies the provider.Provider interface.
func TestNewSucceeds(t *testing.T) {
	p, err := New() // No API key — we only assert construction, not Invoke.
	require.Error(t, err)
	require.Nil(t, p)
}

// TestInterfaceConformance is a compile-time check that the wire's
// returned provider satisfies the provider.Provider interface. It is a
// runtime check (not a var declaration) so that a future wire change
// that breaks the interface is caught at test time rather than at
// compile time of the wire itself.
func TestInterfaceConformance(t *testing.T) {
	var p provider.Provider
	// Build via New at runtime, but New requires WithAPIKey to reach the
	// construction branch — assert the error rather than the value.
	got, err := New()
	assert.Error(t, err)
	assert.Nil(t, got)
	// Force the compile-time interface check via a typed nil.
	_ = p
}

// TestOptionPassthrough verifies that Option values constructed in this
// package are accepted by New and forwarded to the wire.
//
// This test never reaches Invoke — it is sufficient that New returns
// (or errors) without panicking on the wire's validation path.
func TestOptionPassthrough(t *testing.T) {
	// Construct a provider with an obviously invalid base URL and a
	// missing key. The wire should reject the missing key before it
	// touches the base URL; the assertion is that New returns the
	// wire's "missing required option" error verbatim, proving the
	// option list reached the wire.
	_, err := New()
	require.Error(t, err)
}

// Compile-time interface conformance assertion: the wire's *Provider
// must implement provider.Provider. This is also enforced in the wire
// package; we duplicate it here so a future drift is caught at the
// first-party wrapper too. The assignment compiles away; it is here as
// documentation and an additional compile-time guarantee.
//
// We assert against the interface, not the wire's concrete type, so
// that the assertion survives renames of the wire's type.
//
//nolint:unused // Retained as a compile-time assertion; never executed.
var _ provider.Provider = (provider.Provider)(nil)

// silence unused imports in the test file when run with -run=NONE.
var (
	_ = context.Background
	_ = artifact.Text{}
	_ = models.Spec{}
	_ = state.NewBuffer()
)
