package minimax

import (
	"testing"

	"github.com/andrewhowdencom/ore/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIdentityResolver verifies that the resolver is identity (no
// rewrite). This guards against accidental table rewrites in future
// refactors.
func TestIdentityResolver(t *testing.T) {
	cases := []string{
		"claude-opus-4-5",
		"gpt-4o",
		"minimax/text-01",
		"some-canonical-name",
		"",
	}
	for _, c := range cases {
		t.Run(c, func(t *testing.T) {
			assert.Equal(t, c, identityResolver(c))
		})
	}
}

// TestNewAnthropicSucceeds verifies that NewAnthropic constructs a
// provider that satisfies provider.Provider. The provider is fully
// constructed because we supply the required API key.
func TestNewAnthropicSucceeds(t *testing.T) {
	p, err := NewAnthropic("test-key")
	require.NoError(t, err)
	require.NotNil(t, p)
	// Compile-time check that the returned value satisfies the
	// interface.
	var _ provider.Provider = p
}

// TestNewOpenAISucceeds verifies that NewOpenAI constructs a provider
// that satisfies provider.Provider. The provider is fully constructed
// because we supply the required API key.
func TestNewOpenAISucceeds(t *testing.T) {
	p, err := NewOpenAI("test-key")
	require.NoError(t, err)
	require.NotNil(t, p)
	var _ provider.Provider = p
}

// TestBaseURLConstants pins the base URL values so a future
// accidental change to the upstream host surfaces as a test failure
// rather than as silent misrouted traffic.
func TestBaseURLConstants(t *testing.T) {
	assert.Equal(t, "https://api.minimax.io/anthropic", anthropicBaseURL)
	assert.Equal(t, "https://api.minimax.io/openai/v1", openaiBaseURL)
}