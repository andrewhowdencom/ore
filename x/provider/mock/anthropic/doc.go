// Package anthropic implements a wire-compatible mock of the Anthropic
// Messages streaming API. It accepts the same POST /v1/messages request
// shape as the real API and emits the same `event: <name>\ndata: <json>`
// SSE frame format consumed by the official anthropic-sdk-go client.
//
// The mock is hand-rolled with [encoding/json] (no SDK imports in
// production code), so it cannot drift from the real SDK version and
// never pulls transitive dependencies into the root module. Tests
// import the real Anthropic SDK to verify the mock's bytes round-trip
// through the official client.
//
// # Usage
//
// Application code wires the mock by pointing the real provider at the
// mock's URL:
//
//	srv, _ := anthropicmock.New(anthropicmock.WithResponses(mock.Response{
//	    Text: "Hello, world!",
//	    Usage: &mock.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
//	}))
//	ts := httptest.NewServer(srv.Handler())
//	defer ts.Close()
//
//	p, _ := anthropic.New(anthropic.WithBaseURL(ts.URL), anthropic.WithAPIKey("test"))
//	// ... invoke p ...
package anthropic