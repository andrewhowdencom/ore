// Package openai is the first-party OpenAI Chat Completions API provider.
//
// Use [WithAPIKey] for a static credential or [WithBearerTokenSource] for an
// application-managed OAuth or workload identity token. The package does not
// implement authorization grants, refresh, or credential persistence.
package openai

// This file is intentionally minimal. The first-party wrapper is a
// thin shim over the wire at github.com/andrewhowdencom/ore/x/wire/openai;
// see openai.go for the implementation.
