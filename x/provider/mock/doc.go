// Package mock provides wire-compatible mock LLM servers for testing and
// performance validation in the ore framework.
//
// The package defines a high-level, wire-format-agnostic [Response] value
// type and a thread-safe [Queue] that rotates canned responses per HTTP
// request. Per-vendor sub-packages translate [Response] into the exact
// SSE frame format expected by real provider SDKs:
//
//   - [github.com/andrewhowdencom/ore/x/provider/mock/openai] — OpenAI chat
//     completions streaming.
//   - [github.com/andrewhowdencom/ore/x/provider/mock/anthropic] — Anthropic
//     messages streaming.
//
// All wire-format translation is hand-rolled with [encoding/json] (no SDK
// imports in the library), so the mock cannot drift from the real
// provider's SDK version and never pulls transitive dependencies into the
// root module.
//
// # Usage
//
// Application code instantiates a vendor sub-package's [New] with
// [WithResponses] (or [openai.WithResponses] / [anthropic.WithResponses]):
//
//	srv, err := openaimock.New(openaimock.WithResponses(mock.Response{
//	    Text: "Hello, world!",
//	    Usage: &mock.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
//	}))
//	// For httptest embedding:
//	ts := httptest.NewServer(srv.Handler())
//	defer ts.Close()
//	// For long-running (e.g. workshop dev mode):
//	go srv.Start(ctx, ":0")
//
// Real adapters point at the mock via [openai.WithBaseURL] (or the
// Anthropic equivalent). The mock emits the canonical `data: [DONE]` or
// `message_stop` terminator so the real adapter's streaming loop ends
// cleanly.
package mock
