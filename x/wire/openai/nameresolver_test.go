package openai

import (
	"net/http"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWithNameResolver_IdentityDefault verifies that a Provider
// constructed without WithNameResolver resolves the canonical
// spec name to itself.
func TestWithNameResolver_IdentityDefault(t *testing.T) {
	t.Parallel()

	transport := &recordingMockTransport{
		responseBody: simpleSSE("ok"),
	}
	p, err := New(
		WithAPIKey("test-key"),
		WithHTTPClient(&http.Client{Transport: transport}),
	)
	require.NoError(t, err)
	require.NotNil(t, p.nameResolver,
		"identity name resolver must be installed by default")
	assert.Equal(t, "gpt-test", p.nameResolver("gpt-test"),
		"identity resolver must forward unchanged")

	mem := ledger.NewThread()
	mem.Append(ledger.RoleUser, artifact.Text{Content: "hi"})
	ch := make(chan artifact.Artifact, 1)
	require.NoError(t, p.Invoke(t.Context(), mem, models.Spec{Name: "gpt-test"}, ch))
	close(ch)
	for range ch {
	}

	requests := transport.Requests()
	require.Len(t, requests, 1, "the SDK must have made one HTTP call")
	assert.Contains(t, string(requests[0].body), `"model":"gpt-test"`,
		"identity resolver must forward the spec name unchanged")
}

// TestWithNameResolver_AppliesMapping verifies that an explicit
// WithNameResolver translates the canonical spec name into the
// wire name understood by the configured host. The test uses a
// resolver that prefixes the input with "vendor/": a typical
// gateway pattern.
func TestWithNameResolver_AppliesMapping(t *testing.T) {
	t.Parallel()

	transport := &recordingMockTransport{
		responseBody: simpleSSE("ok"),
	}
	p, err := New(
		WithAPIKey("test-key"),
		WithNameResolver(func(canonical string) string {
			return "vendor/" + canonical
		}),
		WithHTTPClient(&http.Client{Transport: transport}),
	)
	require.NoError(t, err)

	mem := ledger.NewThread()
	mem.Append(ledger.RoleUser, artifact.Text{Content: "hi"})
	ch := make(chan artifact.Artifact, 1)
	require.NoError(t, p.Invoke(t.Context(), mem, models.Spec{Name: "gpt-test"}, ch))
	close(ch)
	for range ch {
	}

	requests := transport.Requests()
	require.Len(t, requests, 1, "the SDK must have made one HTTP call")
	assert.Contains(t, string(requests[0].body), `"model":"vendor/gpt-test"`,
		"resolver must rewrite the canonical name on the wire")
}

// TestWithNameResolver_NotCalledForEmptyName verifies that the
// resolver is bypassed for an empty spec.Name. The hard error
// from the empty-name check is the contract; the resolver should
// not see a phantom empty-string call.
func TestWithNameResolver_NotCalledForEmptyName(t *testing.T) {
	t.Parallel()

	called := false
	p, err := New(
		WithAPIKey("test-key"),
		WithNameResolver(func(canonical string) string {
			called = true
			return "rewritten"
		}),
	)
	require.NoError(t, err)

	mem := ledger.NewThread()
	mem.Append(ledger.RoleUser, artifact.Text{Content: "hi"})
	ch := make(chan artifact.Artifact, 1)
	err = p.Invoke(t.Context(), mem, models.Spec{Name: ""}, ch)
	require.Error(t, err, "empty spec.Name must produce a hard error")
	assert.False(t, called, "resolver must not be invoked for an empty spec.Name")
}

// TestWithNameResolver_NilDefaultsToIdentity verifies that a nil
// resolver passed to WithNameResolver is treated as identity
// rather than a nil dereference.
func TestWithNameResolver_NilDefaultsToIdentity(t *testing.T) {
	t.Parallel()

	p, err := New(
		WithAPIKey("test-key"),
		WithNameResolver(nil),
	)
	require.NoError(t, err)
	require.NotNil(t, p.nameResolver,
		"nil resolver must be replaced with the identity function")
	assert.Equal(t, "x", p.nameResolver("x"))
}
