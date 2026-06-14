package anthropic

import (
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNew_RequiresAPIKey verifies that New returns an error when
// WithAPIKey is omitted. The skeleton implements the required-option
// contract from day one so callers cannot accidentally ship a provider
// that authenticates as the empty string.
func TestNew_RequiresAPIKey(t *testing.T) {
	t.Parallel()

	_, err := New(WithModel("claude-3-7-sonnet-latest"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "apiKey")
}

// TestNew_RequiresModel verifies that New returns an error when
// WithModel is omitted. Symmetric to TestNew_RequiresAPIKey: callers
// must explicitly name the model so the SDK does not silently default
// to anything.
func TestNew_RequiresModel(t *testing.T) {
	t.Parallel()

	_, err := New(WithAPIKey("test-key"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model")
}

// TestNew_SucceedsWithRequiredOptions verifies the happy path: an
// API key and a model are sufficient to construct a Provider. The
// skeleton exposes this so the rest of the test suite can build
// against a Provider without a live server.
func TestNew_SucceedsWithRequiredOptions(t *testing.T) {
	t.Parallel()

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-3-7-sonnet-latest"),
	)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "claude-3-7-sonnet-latest", p.model)
	assert.False(t, p.isOpenRouter, "empty base URL is Anthropic native, not OpenRouter")
}

// TestNew_OpenRouterBaseURL verifies that configuring an OpenRouter
// base URL flips the resolved isOpenRouter flag, which drives the
// auth header dispatch in Task 8. The skeleton just stores the
// flag; the full auth-header resolution lands in Task 8.
func TestNew_OpenRouterBaseURL(t *testing.T) {
	t.Parallel()

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("anthropic/claude-3.7-sonnet:thinking"),
		WithBaseURL("https://openrouter.ai/api/v1"),
	)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.True(t, p.isOpenRouter)
}

// TestNew_AnthropicNativeBaseURL verifies that a non-OpenRouter base
// URL is treated as Anthropic native. The isOpenRouter flag is the
// only resolved state checked here; the full auth-header
// resolution lands in Task 8.
func TestNew_AnthropicNativeBaseURL(t *testing.T) {
	t.Parallel()

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-3-7-sonnet-latest"),
		WithBaseURL("https://api.anthropic.com"),
	)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.False(t, p.isOpenRouter)
}

// TestInvoke_StubReturnsNil verifies the skeleton Invoke is callable
// and returns nil. Streaming behavior lands in Task 7; this is a
// compile-and-wire smoke test only.
func TestInvoke_StubReturnsNil(t *testing.T) {
	t.Parallel()

	p, err := New(
		WithAPIKey("test-key"),
		WithModel("claude-3-7-sonnet-latest"),
	)
	require.NoError(t, err)

	mem := &state.Buffer{}
	mem.Append(state.RoleUser, artifact.Text{Content: "hi"})

	ch := make(chan artifact.Artifact, 1)
	require.NoError(t, p.Invoke(t.Context(), mem, ch))
	close(ch)
	for range ch {
		// drain
	}
}

// TestIsOpenRouter_TableDriven exercises the host-detection helper
// with a small table of base URLs. The substring-match heuristic
// mirrors the openai module's; the table documents which inputs
// trigger which branch.
func TestIsOpenRouter_TableDriven(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		baseURL string
		want    bool
	}{
		{"empty defaults to Anthropic native", "", false},
		{"anthropic native host", "https://api.anthropic.com", false},
		{"openrouter canonical", "https://openrouter.ai/api/v1", true},
		{"openrouter subdomain", "https://beta.openrouter.ai/api/v1", true},
		{"anthropic native path with v1", "https://api.anthropic.com/v1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isOpenRouter(tt.baseURL))
		})
	}
}
