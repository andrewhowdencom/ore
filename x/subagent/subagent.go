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

// AsTool wraps an application-supplied factory as a tool.Tool. Each
// invocation of the tool calls the factory to obtain a fresh
// *agent.Agent, runs the agent against a one-shot ledger seeded
// with the prompt, captures the produced turn from the agent's
// "turn_complete" event stream, and closes the agent. The factory
// is the sole extension point: callers compose the agent's pattern,
// model, and tool subset, then hand the factory to AsTool.
//
// The tool's input is a JSON object with a single "prompt" string
// field; the agent runs once (its configured pattern decides
// whether that is one turn or many), and the produced assistant
// turn is returned as the tool's string result.
//
// Fresh-per-call contract: the factory MUST return a new
// *agent.Agent per invocation. The closure calls defer Close()
// on the returned agent; *agent.Agent.Close is idempotent (via
// sync.Once) but a closed agent's step will reject subsequent
// runs. A factory that returns the same agent twice will fail on
// the second call.
//
// State isolation: the sub-agent runs against a fresh
// ledger.Buffer seeded with the prompt as a RoleUser turn. The
// sub-agent's configured transforms, handlers, and pattern apply
// to that fresh buffer. The agent MUST NOT be constructed with
// agent.WithState (which would auto-append to the bound state on
// every Emit); the factory's responsibility is to omit WithState
// from the agent's options.
//
// AsTool returns both the tool.Tool descriptor and its callable
// ToolFunc. The caller is expected to register both with a
// tool.Registry, e.g.:
//
//	subT, subFn := subagent.AsTool(func() (*agent.Agent, error) {
//	    sp, _ := subagent.ResultSystemPrompt()
//	    return agent.New("researcher",
//	        agent.WithProvider(prov),
//	        agent.WithSpec(spec),
//	        agent.WithPattern(&cognitive.SingleShot{}),
//	        agent.WithTransforms(sp),
//	    ), nil
//	}, "researcher", "Search the codebase and answer the prompt.")
//	_ = registry.Register(subT, subFn)
func AsTool(build func() (*agent.Agent, error), name, description string) (tool.Tool, tool.ToolFunc) {
	fn := func(ctx context.Context, _ tool.Sandbox, args map[string]any) (any, error) {
		prompt, _ := args["prompt"].(string)
		if prompt == "" {
			return nil, fmt.Errorf("subagent %s: prompt is required", name)
		}

		a, err := build()
		if err != nil {
			return nil, fmt.Errorf("subagent %s: %w", name, err)
		}
		defer func() { _ = a.Close() }()

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
