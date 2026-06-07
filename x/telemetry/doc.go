// Package telemetry provides an OpenTelemetry metrics callback for ore
// conversation turns. It records per-artifact, per-role character counts
// via loop.OnEmit, attributing cost to artifact kinds (text, tool_call,
// tool_result, reasoning, image) and turn roles (user, assistant, tool, system).
//
// Use New(meter) to create a Telemetry instance, then wire its OnEmit()
// callback into a loop.Step via loop.WithOnEmit:
//
//	telemetry := telemetry.New(meter)
//	step := loop.New(
//	    loop.WithOnEmit(telemetry.OnEmit()),
//	)
//
// If meter is nil, all recording operations are no-ops.
//
// Character counts are computed per artifact:
//   - Text: len(Content)
//   - Reasoning: len(Content)
//   - ToolCall: len(LLMString())
//   - ToolResult: len(LLMString())
//   - Image: len(URL)
//   - Usage: 0 (metadata, not content)
//   - Unknown: len(JSON.Marshal(art))
//
// Two counters are recorded:
//   - "ore.llm.characters.sent" for user, system, and tool turns
//   - "ore.llm.characters.received" for assistant turns
//
// Both counters carry attributes:
//   - "artifact.kind" — the artifact's Kind() string
//   - "role" — the turn role (user, assistant, tool, system)
package telemetry