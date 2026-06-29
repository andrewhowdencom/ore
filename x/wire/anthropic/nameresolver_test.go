package anthropic

import (
	"context"
	"net/http"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newNRTestParams builds a minimal anthropic.MessageNewParams
// for use with the recording transport in the WithNameResolver
// tests. The model field is the only one that varies between
// cases; the rest is the smallest valid request the SDK will
// marshal.
func newNRTestParams(model string) anthropic.MessageNewParams {
	return anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: 1,
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock("ping")),
		},
	}
}

// TestWithNameResolver_IdentityDefault verifies that a Provider
// constructed without WithNameResolver resolves the canonical spec
// name to itself. The captured wire request must carry the same
// name the caller put in the spec.
func TestWithNameResolver_IdentityDefault(t *testing.T) {
	t.Parallel()

	transport := &recordingTransport{}
	p, err := New(
		WithAPIKey("test-key"),
		WithHTTPClient(&http.Client{Transport: transport}),
	)
	require.NoError(t, err)
	// p.nameResolver is the identity function. The default must
	// not be nil.
	require.NotNil(t, p.nameResolver,
		"identity name resolver must be installed by default")
	assert.Equal(t, "claude-test", p.nameResolver("claude-test"),
		"identity resolver must forward unchanged")

	// Fire a non-streaming request so the SDK writes the body
	// to the transport. The captured body is the canonical
	// check; we don't care about the response.
	_, _ = p.client.Messages.New(
		context.Background(),
		newNRTestParams("claude-test"),
	)

	req := transport.Request()
	require.NotNil(t, req, "the SDK must have made an HTTP call")
	body := transport.Body()
	assert.Contains(t, string(body), `"model":"claude-test"`,
		"identity resolver must forward the spec name unchanged")
}

// TestWithNameResolver_AppliesMapping verifies that an explicit
// WithNameResolver translates the canonical spec name into the
// wire name understood by the configured host. The test uses a
// resolver that prefixes the input with "vendor/": a typical
// gateway pattern.
func TestWithNameResolver_AppliesMapping(t *testing.T) {
	t.Parallel()

	transport := &recordingTransport{}
	p, err := New(
		WithAPIKey("test-key"),
		WithNameResolver(func(canonical string) string {
			return "vendor/" + canonical
		}),
		WithHTTPClient(&http.Client{Transport: transport}),
	)
	require.NoError(t, err)

	mem := ledger.NewBuffer()
	mem.Append(ledger.RoleUser, artifact.Text{Content: "hi"})

	ch := make(chan artifact.Artifact, 1)
	spec := models.Spec{Name: "claude-test"}
	require.NoError(t, p.Invoke(t.Context(), mem, spec, ch))
	close(ch)
	for range ch {
	}

	req := transport.Request()
	require.NotNil(t, req, "the SDK must have made an HTTP call")
	body := transport.Body()
	assert.Contains(t, string(body), `"model":"vendor/claude-test"`,
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

	mem := ledger.NewBuffer()
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
