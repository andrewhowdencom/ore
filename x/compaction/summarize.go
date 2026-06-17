package compaction

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/x/llmbytes"
)

// ErrTruncatedSummary is the sentinel returned by Summarize when the
// provider produced a response whose final artifact.StopReason was
// StopReasonLength. The error indicates the model hit its output-token
// cap mid-summary; the returned state.Turn is the zero value, so callers
// can detect the failure and refuse to append anything to the buffer.
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

// Summarize calls the provider to summarize the given turns and returns
// a single RoleSystem turn carrying both artifact.Text (the LLM-facing
// summary) and artifact.Compaction (structured metadata describing the
// compaction event).
//
// The returned turn is intended to be appended to the buffer by the
// caller (e.g. a slash handler invoking /compact). Summarize is
// non-destructive: the original turns slice is never modified.
//
// On success the returned turn carries:
//
//   - artifact.Text with the summary content (matches the LLM-facing
//     contract established by the previous SummarizeStrategy design).
//   - artifact.Compaction with CompactedThrough=len(turns),
//     DroppedTurnCount=len(turns), DroppedTokenEstimate computed by
//     summing llmbytes.Of over every artifact in the input slice,
//     Strategy="summarize", Model=spec.Name, CreatedAt=time.Now().
//
// On ErrTruncatedSummary the returned turn is the zero value and the
// caller MUST NOT append anything to the buffer; the buffer should be
// left unchanged. This is the same defensive contract as the previous
// SummarizeStrategy — truncation no longer silently writes a one-token
// "##" fragment into the conversation.
//
// The provider receives the full history loaded into a temporary
// state.Buffer, followed by a user prompt asking for a concise summary.
// The summary uses RoleSystem because it is injected context about
// prior conversation, not a real assistant response.
//
// Spec.MaxOutputTokens is forwarded to the provider as the per-
// invocation output-token budget (with a fallback of 8192 when unset).
// This is the same self-sizing behavior that fixes the prior
// 'compaction returns ##' bug: without an explicit budget the
// Anthropic adapter would default to 1 token.
//
// Summarize only collects artifact.Text and artifact.TextDelta
// responses from the provider. Other artifact types (Usage, Reasoning,
// ToolCall, etc.) are silently ignored. This is an MVP limitation;
// future work may add custom formatters or multi-modal support.
func Summarize(ctx context.Context, p provider.Provider, spec models.Spec, turns []state.Turn) (state.Turn, error) {
	if p == nil {
		return state.Turn{}, fmt.Errorf("Summarize: provider must not be nil")
	}
	if len(turns) == 0 {
		return state.Turn{}, nil
	}

	// Dropped metadata: count turns and sum llmbytes over every
	// artifact in the input slice. The estimate is best-effort;
	// it is what the TUI marker renders and what analytics
	// attributes to the compaction, not what the provider reports.
	var droppedBytes int64
	for _, t := range turns {
		for _, a := range t.Artifacts {
			droppedBytes += llmbytes.Of(a)
		}
	}

	buf := &state.Buffer{}
	buf.LoadTurns(turns)

	prompt := defaultPrompt
	buf.Append(state.RoleUser, artifact.Text{Content: prompt})

	// Resolve the effective max-tokens budget. Zero or negative
	// values fall back to the framework default. The provider
	// receives this as a single, explicit option so the model has
	// room to produce a complete summary regardless of the
	// adapter's per-model default.
	maxTokens := spec.MaxOutputTokens
	if maxTokens <= 0 {
		maxTokens = defaultSummarizeMaxTokens
	}
	opts := []provider.InvokeOption{provider.WithMaxTokens(maxTokens)}

	ch := make(chan artifact.Artifact, 100)
	var texts []string
	var lastStopReason artifact.StopReasonKind

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for art := range ch {
			switch a := art.(type) {
			case artifact.Text:
				texts = append(texts, a.Content)
			case artifact.TextDelta:
				texts = append(texts, a.Content)
			case artifact.StopReason:
				// The latest non-empty StopReason wins. In
				// practice adapters emit a single StopReason
				// at the end of the stream, but the loop is
				// defensive.
				if a.Reason != "" {
					lastStopReason = a.Reason
				}
			}
		}
	}()

	if err := p.Invoke(ctx, buf, spec, ch, opts...); err != nil {
		close(ch)
		wg.Wait()
		return state.Turn{}, fmt.Errorf("summarization provider call failed: %w", err)
	}
	close(ch)
	wg.Wait()

	// Truncation check: the provider is contractually obligated
	// to surface a final StopReason on the channel for every
	// successful stream. A Length reason means the model hit
	// its output cap; the partial text we collected is not a
	// valid summary. Returning the zero turn preserves the
	// caller's history so they can decide policy.
	if lastStopReason == artifact.StopReasonLength {
		return state.Turn{}, fmt.Errorf("summarization truncated: %w", ErrTruncatedSummary)
	}

	summary := strings.Join(texts, "")
	now := time.Now()
	summaryTurn := state.Turn{
		Role: state.RoleSystem,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: summary},
			artifact.Compaction{
				CompactedThrough:     len(turns),
				DroppedTurnCount:     len(turns),
				DroppedTokenEstimate: droppedBytes,
				Strategy:             StrategyNameSummarize,
				Model:                spec.Name,
				CreatedAt:            now,
			},
		},
		Timestamp: now,
	}
	return summaryTurn, nil
}

// defaultSummarizeMaxTokens is the per-invocation output budget the
// strategy requests when Spec.MaxOutputTokens is unset (zero). 8192
// tokens is large enough to produce the full five-section structured
// handoff prompt (Primary Goal, Key Decisions, Completed Work, Current
// State, Pending Tasks) for a typical conversation while staying well
// within the output caps of the long-tail of supported models (Sonnet
// 4 / 4.5, GPT-4o, etc.). Applications with unusual workloads can
// override via Spec.MaxOutputTokens.
const defaultSummarizeMaxTokens int64 = 8192

const defaultPrompt = `You are creating a context handoff summary for another agent that will resume this conversation.

First, analyze the conversation history to identify:
- The primary goal or task the user is trying to accomplish
- Key decisions, constraints, preferences, and rules established
- Completed work and successful outcomes
- Current state and work in progress
- Pending tasks and next steps required

Do not include system prompt information (identity, guardrails, or application configuration) in the summary. These are added separately by the receiving agent and do not need to be preserved.

Then, output ONLY a structured summary using the following markdown sections. Do not include any analysis, reasoning, introductory text, or commentary outside these sections. The summary must be concise but complete — another agent will resume work from this summary.

## Primary Goal
[What the user is trying to accomplish]

## Key Decisions & Constraints
[Important rules, preferences, architectural choices, or constraints established]

## Completed Work
[What has already been successfully done]

## Current State / Work in Progress
[What is actively being worked on, including any in-progress decisions or partial results]

## Pending Tasks & Next Steps
[What still needs to be done to complete the goal]`
