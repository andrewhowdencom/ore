// Package subagent exposes an *agent.Agent as a tool.Tool, so a parent
// agent can invoke a sub-agent's inference through the same tool-use
// machinery as any other capability. The pattern of the wrapped agent
// is opaque to the parent: a SingleShot agent is a one-shot helper; a
// ReAct agent runs a tool-use loop; a Verified agent retries on quality
// failures. The parent sees only the final assistant turn.
//
// The skeleton is deliberately minimal. Streaming, parallel fan-out,
// and shared state are out of scope and tracked as follow-ups.
package subagent

import (
	"context"
	"fmt"

	"github.com/andrewhowdencom/ore/agent"
	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/tool"
)

// promptSchema is the JSON Schema for the sub-agent tool's input. The
// tool takes a single required "prompt" string field.
var promptSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"prompt": map[string]any{
			"type":        "string",
			"description": "The prompt to send to the sub-agent.",
		},
	},
	"required": []string{"prompt"},
}

// AsTool wraps an *agent.Agent as a tool.Tool. The tool's input is a
// JSON object with a single "prompt" string field; the agent runs
// once (its configured pattern decides whether that is one turn or
// many), and the produced assistant turn is returned as the tool's
// string result.
//
// The sub-agent runs against a fresh ledger.Buffer seeded with the
// prompt as a RoleUser turn. The sub-agent's configured transforms,
// handlers, and pattern apply to that fresh buffer. State does not
// persist between sub-agent invocations.
//
// The tool captures the produced turn from the agent's
// "turn_complete" event stream (the same mechanism the compactor
// uses). The agent need not have a state binding; the ephemeral
// buffer built here is used for inference context only.
//
// AsTool returns both the tool.Tool descriptor and its callable
// ToolFunc. The caller is expected to register both with a
// tool.Registry, e.g.:
//
//	subT, subFn := subagent.AsTool(sub, "researcher",
//	    "Search the codebase and answer the prompt.")
//	_ = registry.Register(subT, subFn)
func AsTool(a *agent.Agent, name, description string) (tool.Tool, tool.ToolFunc) {
	fn := func(ctx context.Context, _ tool.Sandbox, args map[string]any) (any, error) {
		prompt, _ := args["prompt"].(string)
		if prompt == "" {
			return nil, fmt.Errorf("subagent %s: prompt is required", name)
		}

		buf := &ledger.Buffer{}
		buf.Append(ledger.RoleUser, artifact.Text{Content: prompt})

		// Subscribe to the agent's turn_complete event before
		// running the agent. The subscriber goroutine reads the
		// first matching event, copies the produced turn into a
		// local channel, and exits. The EventBus's Emit blocks
		// until delivery, so by the time agent.Run returns, the
		// event is already queued on this subscriber's channel.
		type captured struct{ turn ledger.Turn }
		capturedCh := make(chan captured, 1)
		events := a.Subscribe("turn_complete")
		go func() {
			for ev := range events {
				if tc, ok := ev.(loop.TurnCompleteEvent); ok {
					select {
					case capturedCh <- captured{turn: tc.Turn}:
					default:
					}
					return
				}
			}
		}()

		if _, err := a.Run(ctx, buf); err != nil {
			return nil, fmt.Errorf("subagent %s: %w", name, err)
		}

		var produced ledger.Turn
		select {
		case c := <-capturedCh:
			produced = c.turn
		case <-ctx.Done():
			return nil, ctx.Err()
		}

		return assistantText(produced), nil
	}

	return tool.Tool{
		Name:        name,
		Description: description,
		Schema:      promptSchema,
	}, fn
}

// assistantText concatenates the Text and TextDelta content of an
// assistant turn into a single string. Other artifact kinds
// (Reasoning, ToolCall, Usage, etc.) are ignored; the tool result is
// the model's user-facing text.
func assistantText(turn ledger.Turn) string {
	var s string
	for _, a := range turn.Artifacts {
		switch v := a.(type) {
		case artifact.Text:
			s += v.Content
		case artifact.TextDelta:
			s += v.Content
		}
	}
	return s
}
