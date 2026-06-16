// Package catalogmodels exports well-known [models.Spec] values
// for the framework's primary model families. The catalog is
// generated from https://models.dev by [cmd/modelsdev-gen]; the
// per-family files are committed and re-generated when upstream
// ships new model revisions or when the allowlist grows.
//
// The Name field of every Spec is the canonical wire name
// understood by the upstream primary's API (e.g.
// "claude-opus-4-5" for Anthropic, "gpt-4o" for OpenAI). Adapters
// translate the canonical name to a wire value via
// [x/wire/anthropic.WithNameResolver] or the equivalent on the
// target wire; the catalog itself is wire-agnostic.
//
// Recommended import pattern (alias the package to avoid
// clashing with the root [github.com/andrewhowdencom/ore/models]):
//
//	import (
//	    "github.com/andrewhowdencom/ore/models"
//	    catalogmodels "github.com/andrewhowdencom/ore/x/catalog/models"
//	)
//
//	opts := []provider.InvokeOption{catalogmodels.ClaudeOpus45}
//
// To add a new family, edit [cmd/modelsdev-gen]'s primaryProviders
// allowlist and re-run `task generate`. The per-family files are
// committed; do not hand-edit them.
package catalogmodels
