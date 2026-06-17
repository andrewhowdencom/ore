// Package minimax exposes third-party providers that speak the
// Anthropic and OpenAI wire protocols but are hosted at api.minimax.io.
//
// The constructors in this package point the framework wires at the
// upstream host with the appropriate path prefix and apply an
// identity-resolver so that canonical spec names (as emitted by the
// ore catalog) are forwarded verbatim. When a future upstream adopts
// aliases, callers may override [WithNameResolver] — the same option
// exposed by the underlying wires — to translate canonical names
// into the wire names understood by the host.
//
// The package does not introduce a new wire protocol; it is a thin
// first-party wrapper over [github.com/andrewhowdencom/ore/x/wire/anthropic]
// and [github.com/andrewhowdencom/ore/x/wire/openai].
//
// # Constructors
//
//   - [NewAnthropic] returns a provider speaking the Anthropic
//     Messages API on api.minimax.io/anthropic.
//   - [NewOpenAI] returns a provider speaking the OpenAI Chat
//     Completions API on api.minimax.io/openai/v1.
//
// Both constructors require an API key. The key is the same shape as
// the upstream service's key.
package minimax

// anthropicBaseURL is the api.minimax.io path that mirrors the
// Anthropic Messages API. The wire does not impose an isOpenRouter
// branch because the auth header is selected by base URL inspection;
// hosts other than api.anthropic.com or openrouter.ai fall into the
// bearer-auth branch. minimax uses bearer auth.
const anthropicBaseURL = "https://api.minimax.io/anthropic"

// openaiBaseURL is the api.minimax.io path that mirrors the OpenAI
// Chat Completions API.
const openaiBaseURL = "https://api.minimax.io/openai/v1"