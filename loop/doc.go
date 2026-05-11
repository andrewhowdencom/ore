// Package loop implements the single-turn execution primitive for ore.
// It provides a Step type that invokes a provider, distributes streaming
// artifacts to subscribers via an embedded FanOut, and runs registered
// artifact handlers on the complete response.
//
// Options include:
//
//   - WithHandlers — run artifact handlers on the complete response.
//   - WithBeforeTurn — transform state before the provider call.
//
// Step is the single canonical single-turn execution primitive with
// optional, opt-in capabilities via functional options. A Step with no
// options is valid for non-streaming, non-handler use cases.
//
// Surfaces subscribe to specific artifact kinds via Step.Subscribe(),
// receiving artifacts directly (which satisfy OutputEvent via Kind()) as
// they are emitted by the provider.
package loop
