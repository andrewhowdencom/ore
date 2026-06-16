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
)

// ErrTruncatedSummary is the sentinel returned by SummarizeStrategy.Compact
// when the provider produced a response whose final artifact.StopReason
// was StopReasonLength. The error indicates the model hit its
// output-token cap mid-summary; the returned turns slice (the caller's
// original turns) is unchanged so callers can decide to retry, fall
// back to a different strategy, or refuse to compact.
//
// Use errors.Is to detect:
//
//	if errors.Is(err, compaction.ErrTruncatedSummary) { ... }
//
// This is intentionally a non-wrapped sentinel (exposed at the package
// level) so callers can switch on it without parsing error strings.
var ErrTruncatedSummary = errors.New("summarization produced truncated result")

// SummarizeStrategy uses an LLM provider to summarize conversation history,
// replacing all turns with a single synthetic system summary turn.
//
// The provider is called with the full history loaded into a temporary
// state.Buffer, followed by a user prompt asking for a concise summary.
// Only artifact.Text responses from the provider are collected; other artifact
// types (Usage, Reasoning, ToolCall, etc.) are ignored. This is an MVP
// limitation.
type SummarizeStrategy struct {
	Provider provider.Provider
	// Spec is the compactor's own [models.Spec]. The strategy
	// forwards the spec to the provider's Invoke method, including
	// Spec.MaxOutputTokens as the per-invocation output-token budget.
	//
	// The compactor is conceptually a sub-agent: it runs a different
	// (simpler) task than the main conversation, and may use a
	// different (typically cheaper) model. The application is
	// responsible for selecting the right Spec; the strategy
	// consumes it transparently.
	Spec models.Spec
	// Prompt is an optional custom summarization prompt. When empty, a default
	// structured handoff prompt is used.
	Prompt string
}

// defaultSummarizeMaxTokens is the per-invocation output budget the
// strategy requests when MaxTokens is unset (zero). 8192 tokens is
// large enough to produce the full five-section structured handoff
// prompt (Primary Goal, Key Decisions, Completed Work, Current State,
// Pending Tasks) for a typical conversation while staying well
// within the output caps of the long-tail of supported models
// (Sonnet 4 / 4.5, GPT-4o, etc.). Applications with unusual
// workloads (very long histories, or summaries intended for very
// large downstream contexts) can override via the MaxTokens field.
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

// Compact loads all turns into a temporary buffer, calls the provider to
// generate a summary, and returns a single synthetic RoleSystem turn containing
// that summary.
//
// If there are no turns, it returns an empty slice without calling the provider.
//
// The summary turn uses RoleSystem because it is injected context about prior
// conversation, not a real assistant response.
//
// The strategy passes a per-invocation provider.WithMaxTokens so the model
// has room to produce a complete summary. If the provider's response
// carries a final artifact.StopReason of StopReasonLength, Compact
// returns the original turns unchanged wrapped with ErrTruncatedSummary
// so callers can retry, fall back, or refuse.
func (s SummarizeStrategy) Compact(ctx context.Context, turns []state.Turn) ([]state.Turn, error) {
	if s.Provider == nil {
		return nil, fmt.Errorf("SummarizeStrategy.Provider must not be nil")
	}
	originalTurns := turns
	if len(turns) == 0 {
		return []state.Turn{}, nil
	}

	buf := &state.Buffer{}
	buf.LoadTurns(turns)

	prompt := s.Prompt
	if prompt == "" {
		prompt = defaultPrompt
	}
	buf.Append(state.RoleUser, artifact.Text{Content: prompt})

	// Resolve the effective max-tokens budget. Zero or negative
	// values fall back to the framework default. The provider
	// receives this as a single, explicit option so the model has
	// room to produce a complete summary regardless of the
	// adapter's per-model default (which on the Anthropic adapter
	// is now "no default" — see anthropic.go WithMaxTokens).
	maxTokens := s.Spec.MaxOutputTokens
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
				// defensive: if a future adapter ever emits
				// more than one, we want the last meaningful
				// one (matching the openai wire format's
				// behavior of carrying the real value on
				// the final delta and nulls elsewhere).
				if a.Reason != "" {
					lastStopReason = a.Reason
				}
			}
		}
	}()

	if err := s.Provider.Invoke(ctx, buf, s.Spec, ch, opts...); err != nil {
		close(ch)
		wg.Wait()
		return nil, fmt.Errorf("summarization provider call failed: %w", err)
	}
	close(ch)
	wg.Wait()

	// Truncation check. The provider is contractually obligated
	// to surface a final StopReason on the channel for every
	// successful stream; the stop_reason / finish_reason → canonical
	// translation is implemented per-adapter. A Length reason
	// means the model hit its output cap; the partial text we
	// collected above is not a valid summary (e.g. the compaction
	// bug produced exactly one or two tokens, which is the
	// "##" symptom). Returning the original turns unchanged
	// preserves the caller's history so they can decide policy:
	// retry with a larger budget, fall back to a different
	// strategy, or refuse to compact and let the history grow.
	if lastStopReason == artifact.StopReasonLength {
		return originalTurns, fmt.Errorf("summarization truncated: %w", ErrTruncatedSummary)
	}

	summary := strings.Join(texts, "")
	summaryTurn := state.Turn{
		Role:      state.RoleSystem,
		Artifacts: []artifact.Artifact{artifact.Text{Content: summary}},
		Timestamp: time.Now(),
	}

	return []state.Turn{summaryTurn}, nil
}
