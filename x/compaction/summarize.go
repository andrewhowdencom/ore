package compaction

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/andrewhowdencom/ore/agent"
	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/x/llmbytes"
)

// ErrTruncatedSummary is the sentinel returned by Summarize when the
// produced turn carries an artifact.StopReason with StopReasonLength.
// The error indicates the model hit its output-token cap mid-summary;
// the returned state.Turn is the zero value, so callers can detect
// the failure and refuse to append anything to the buffer.
//
// Use errors.Is to detect:
//
//	if errors.Is(err, compaction.ErrTruncatedSummary) { ... }
//
// This is intentionally a non-wrapped sentinel (exposed at the package
// level) so callers can switch on it without parsing error strings.
var ErrTruncatedSummary = errors.New("summarization produced truncated result")

// StrategyNameSummarize is the canonical value placed in
// artifact.Compaction.Strategy when Summarize is the producer. Future
// strategies (e.g. extractive, lossy-truncate) will introduce their
// own constants alongside this one.
const StrategyNameSummarize = "summarize"

// Summarize runs the configured agent once to summarize the given
// turns, then wraps the produced turn in an artifact.Compaction
// (carrying artifact.Text alongside the metadata).
//
// The agent is expected to be configured with a cognitive.SingleShot
// pattern (constructed by the caller). The caller owns the agent's
// lifecycle; Summarize does not close it. Any transforms and handlers
// registered on the agent apply to the summarization call (the same
// configuration surface as the main conversation, minus persona /
// system-prompt and tools — the caller decides what the compactor's
// agent carries).
//
// On success the returned turn is RoleSystem, carrying both
// artifact.Compaction (metadata) and artifact.Text (the LLM-facing
// summary). On ErrTruncatedSummary the returned turn is the zero value
// and the caller MUST NOT append anything to the buffer.
//
// Summarize subscribes to the agent's "turn_complete" event before
// running the agent, so the produced turn is captured from the agent's
// event stream. The agent's state binding (if any) is independent of
// the ephemeral buffer Summarize builds internally; the captured turn
// is the canonical result.
//
// The previous per-invocation 8192-token default is gone. Callers that
// want an explicit output budget set spec.MaxOutputTokens on the
// compactor agent (or pass it through agent.WithSpec).
func Summarize(ctx context.Context, a *agent.Agent, turns []state.Turn) (state.Turn, error) {
	// Compute dropped bytes from input turns. The estimate is best-effort;
	// it is what the TUI marker renders and what analytics attributes to
	// the compaction, not what the provider reports.
	var droppedBytes int64
	for _, t := range turns {
		for _, art := range t.Artifacts {
			droppedBytes += llmbytes.Of(art)
		}
	}
	if len(turns) == 0 {
		return state.Turn{}, nil
	}

	// Build an ephemeral buffer with the input turns and the user prompt.
	buf := &state.Buffer{}
	buf.LoadTurns(turns)
	buf.Append(state.RoleUser, artifact.Text{Content: defaultPrompt})

	// Subscribe to the agent's turn_complete event before running the
	// agent. The subscriber goroutine reads the first such event, copies
	// the produced turn into capturedCh, and exits. The EventBus's Emit
	// blocks until every subscriber has received the event, so by the
	// time agent.Run returns, the event is already queued on this
	// subscriber's channel — the main goroutine will receive it from
	// capturedCh without a race.
	type captured struct{ turn state.Turn }
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
		return state.Turn{}, fmt.Errorf("summarization agent run failed: %w", err)
	}

	var produced state.Turn
	select {
	case c := <-capturedCh:
		produced = c.turn
	case <-ctx.Done():
		return state.Turn{}, ctx.Err()
	}

	// Truncation check: the produced turn carries an artifact.StopReason
	// if the model finished. A Length reason means the model hit its
	// output cap; the partial text we collected is not a valid summary.
	// Returning the zero turn preserves the caller's history so they
	// can decide policy.
	for _, art := range produced.Artifacts {
		if sr, ok := art.(artifact.StopReason); ok && sr.Reason == artifact.StopReasonLength {
			return state.Turn{}, ErrTruncatedSummary
		}
	}

	return wrapSummaryTurn(produced, a.Spec(), droppedBytes, len(turns)), nil
}

// wrapSummaryTurn concatenates the produced turn's Text / TextDelta
// content into a single Text artifact and pairs it with a Compaction
// metadata artifact. The result is the RoleSystem turn that the
// compactor returns to its caller.
func wrapSummaryTurn(turn state.Turn, spec models.Spec, droppedBytes int64, droppedTurnCount int) state.Turn {
	var content string
	for _, art := range turn.Artifacts {
		switch a := art.(type) {
		case artifact.Text:
			content += a.Content
		case artifact.TextDelta:
			content += a.Content
		}
	}
	now := time.Now()
	return state.Turn{
		Role: state.RoleSystem,
		Artifacts: []artifact.Artifact{
			artifact.Compaction{
				CompactedThrough:     droppedTurnCount,
				DroppedTurnCount:     droppedTurnCount,
				DroppedTokenEstimate: droppedBytes,
				Strategy:             StrategyNameSummarize,
				Model:                spec.Name,
				CreatedAt:            now,
			},
			artifact.Text{Content: content},
		},
		Timestamp: now,
	}
}
