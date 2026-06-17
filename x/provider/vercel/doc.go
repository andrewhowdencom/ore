// Package vercel exposes the Vercel AI Gateway as a first-party provider.
// The gateway mirrors the OpenAI Chat Completions API at
// https://ai-gateway.vercel.sh/v1 and authenticates via the standard
// bearer-token header, which the openai wire selects automatically when
// the base URL targets ai-gateway.vercel.sh.
//
// The package composes the existing openai wire with a name-resolver
// backed by a generated lookup table (lookup.go). The table is a
// best-effort join between canonical ore catalog names and the
// gateway's wire identifiers; on miss the resolver falls back to
// identity so unknown models can still be requested by their
// gateway id verbatim.
package vercel

// openaiBaseURL is the Vercel AI Gateway base URL for the OpenAI
// Chat Completions API. The wire's base-URL inspection selects
// bearer-token auth for any host other than api.openai.com.
const openaiBaseURL = "https://ai-gateway.vercel.sh/v1"
