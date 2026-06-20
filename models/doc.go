// Package models defines the ModelSpec value type — a typed object
// combining a model identity with its inference configuration — and
// the supporting ThinkingLevel type used to express reasoning effort.
//
// ModelSpec is the canonical argument to provider invocations. It
// supersedes the bare-string model identity and the per-call option
// type that previously lived in the provider package. The split is
// deliberate: a Spec describes a model (identity + how to run it),
// while a provider translates a Spec to a vendor's wire format.
//
// The Spec type is intentionally permissive: pointer-typed fields
// distinguish "set" from "use the framework / model default", and
// unknown fields are silently ignored by adapters. This lets the
// framework grow new Spec fields (and lets vendors grow new catalog
// entries) without breaking every existing call site.
//
// Per-vendor model catalogs live in the unified catalog module
// (x/catalog/models), exported as named Spec values for the p80 case
// (models.Catalog().OpenAI().GPT4o, models.Catalog().Anthropic().ClaudeOpus45, …).
// Per-vendor provider adapters in x/provider/<vendor>/ expose the same
// names to applications; ad-hoc construction covers the long tail:
//
//	models.Spec{
//	    Name: "ft:gpt-3.5-turbo:my-org:custom",
//	    Temperature: ptr(0.3),
//	}
package models
