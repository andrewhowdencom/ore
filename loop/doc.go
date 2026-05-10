// Package loop implements the single-turn execution primitive for tack.
// It provides a Step type that invokes a provider, optionally emits
// streaming deltas as OutputEvents to a provided channel, and runs
// registered artifact handlers on the complete response.
//
// Options include:
//
//   - WithOutput — emit DeltaEvent and TurnCompleteEvent to a channel.
//   - WithHandlers — run artifact handlers on the complete response.
//   - WithBeforeTurn — transform state before the provider call.
//
// Step is the single canonical single-turn execution primitive with
// optional, opt-in capabilities via functional options. A Step with no
// options is valid for non-streaming, non-handler use cases.
package loop
