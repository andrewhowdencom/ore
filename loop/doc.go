// Package loop implements the single-turn execution primitive for tack.
// It provides a Step type that invokes a provider, optionally routes
// streaming deltas to a surface, and runs registered artifact handlers
// on the complete response.
//
// Step is the single canonical single-turn execution primitive with
// optional, opt-in capabilities via functional options. A Step with no
// options is valid for non-streaming, non-handler use cases.
package loop
