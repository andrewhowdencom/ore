package openrouter

import (
	"testing"

	"github.com/andrewhowdencom/ore/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNameResolver_Hit verifies that a canonical name present in the
// lookup table is translated to the OpenRouter wire identifier.
// The canonical names are primary-provider ids from
// x/catalog/models/<family>.go; the test cases below cover
// one entry per primary family that OpenRouter mirrors.
func TestNameResolver_Hit(t *testing.T) {
	cases := []struct {
		canonical string
		want      string
	}{
		{"claude-opus-4-5", "anthropic/claude-opus-4.5"},
		{"gpt-4o", "openai/gpt-4o"},
		{"o1", "openai/o1"},
		{"gemini-2.5-pro", "google/gemini-2.5-pro"},
		{"grok-4.3", "x-ai/grok-4.3"},
		{"mistral-nemo", "mistralai/mistral-nemo"},
		{"command-r-plus-08-2024", "cohere/command-r-plus-08-2024"},
	}
	for _, c := range cases {
		t.Run(c.canonical, func(t *testing.T) {
			assert.Equal(t, c.want, nameResolver(c.canonical))
		})
	}
}

// TestNameResolver_MissFallsBackToIdentity verifies that a canonical
// name absent from the lookup table is forwarded verbatim. This is the
// "request by OpenRouter id" path: callers can pass an OpenRouter-only
// wire identifier and the resolver will not rewrite it.
func TestNameResolver_MissFallsBackToIdentity(t *testing.T) {
	cases := []string{
		"anthropic/claude-3.5-sonnet", // already an openrouter id
		"some-unknown-model",          // not in catalog at all
		"",                            // empty input is forwarded as-is
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			assert.Equal(t, c, nameResolver(c))
		})
	}
}

// TestBaseURLConstant pins the OpenRouter base URL. A future change
// to the upstream host surfaces as a test failure rather than as
// silent misrouted traffic.
func TestBaseURLConstant(t *testing.T) {
	assert.Equal(t, "https://openrouter.ai/api/v1", anthropicBaseURL)
}

// TestNewSucceeds verifies that the constructor returns a non-nil
// provider.Provider with a valid API key. The provider is not
// exercised against the network; only the construction path is.
func TestNewSucceeds(t *testing.T) {
	p, err := New("test-key")
	require.NoError(t, err)
	require.NotNil(t, p)
	// Compile-time check that the returned value satisfies the
	// provider.Provider interface.
	var _ provider.Provider = p
}

// TestNew_RequiresAPIKey verifies that omitting the API key surfaces
// the wire's required-option error verbatim, proving the option list
// reaches the wire.
func TestNew_RequiresAPIKey(t *testing.T) {
	_, err := New("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "apiKey",
		"error should name the missing option")
}

// TestLookupNonEmpty verifies that the generated lookup map is not
// empty — a misconfigured generator that emits no entries would
// silently fall back to identity for every model.
func TestLookupNonEmpty(t *testing.T) {
	assert.NotEmpty(t, nameLookup,
		"nameLookup must contain at least one entry; check cmd/modelsdev-gen")
}