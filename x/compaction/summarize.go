package compaction

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/state"
)

// SummarizeStrategy uses an LLM provider to summarize conversation history,
// replacing dropped turns with a single synthetic system summary turn.
//
// It is token-aware: it walks backwards from the last turn, accumulating
// token estimates, and preserves the suffix that fits within MaxTokens.
// Everything before the suffix is summarized. If the entire history fits,
// it returns a defensive copy without calling the provider.
//
// The provider is called with the turns to be summarized loaded into a
// temporary state.Buffer, followed by a user prompt asking for a concise
// summary. Only artifact.Text responses from the provider are collected;
// other artifact types (Usage, Reasoning, ToolCall, etc.) are ignored.
// This is an MVP limitation.
type SummarizeStrategy struct {
	Provider  provider.Provider
	MaxTokens int
}

// estimateTokens returns a rough token estimate for a slice of turns by
// summing len(artifact.Text.Content) / 4 for all text artifacts in each turn.
// This is a heuristic approximation (~4 characters per token) and requires no
// external dependencies. Non-text artifacts are ignored for estimation.
func estimateTokens(turns []state.Turn) int {
	total := 0
	for _, turn := range turns {
		for _, art := range turn.Artifacts {
			if t, ok := art.(artifact.Text); ok {
				total += len(t.Content) / 4
			}
		}
	}
	return total
}

// Compact walks backwards from the last turn, accumulating token estimates,
// to find the split point where the suffix fits within MaxTokens. It
// summarizes the prefix via the provider and returns a new slice containing
// the summary turn followed by the preserved turns.
//
// If the entire history fits within MaxTokens, it returns a defensive copy
// of the original slice without calling the provider (no-op).
//
// The summary turn uses RoleSystem because it is injected context about prior
// conversation, not a real assistant response.
func (s SummarizeStrategy) Compact(ctx context.Context, turns []state.Turn) ([]state.Turn, error) {
	if s.MaxTokens <= 0 {
		return nil, fmt.Errorf("SummarizeStrategy.MaxTokens must be > 0, got %d", s.MaxTokens)
	}
	if len(turns) == 0 {
		return []state.Turn{}, nil
	}
	if estimateTokens(turns) <= s.MaxTokens {
		result := make([]state.Turn, len(turns))
		copy(result, turns)
		return result, nil
	}

	// Walk backwards to find the split point.
	// Always preserve at least the last turn (best effort), even if it
	// alone exceeds the budget. The strategy is a reducer, not a strict enforcer.
	k := len(turns) - 1
	accumulated := estimateTokens(turns[k:])
	for i := len(turns) - 2; i >= 0; i-- {
		turnTokens := estimateTokens(turns[i : i+1])
		if accumulated+turnTokens > s.MaxTokens {
			k = i + 1
			break
		}
		accumulated += turnTokens
		if i == 0 {
			k = 0
		}
	}

	toSummarize := turns[:k]
	toPreserve := turns[k:]

	// If there is nothing to summarize, return the preserved turns directly.
	if len(toSummarize) == 0 {
		result := make([]state.Turn, len(toPreserve))
		copy(result, toPreserve)
		return result, nil
	}

	buf := &state.Buffer{}
	buf.LoadTurns(toSummarize)
	buf.Append(state.RoleUser, artifact.Text{
		Content: "Summarize the above conversation concisely, preserving all key facts, decisions, and context.",
	})

	ch := make(chan artifact.Artifact, 100)
	var texts []string

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for art := range ch {
			if t, ok := art.(artifact.Text); ok {
				texts = append(texts, t.Content)
			}
		}
	}()

	if err := s.Provider.Invoke(ctx, buf, ch); err != nil {
		close(ch)
		wg.Wait()
		return nil, fmt.Errorf("summarization provider call failed: %w", err)
	}
	close(ch)
	wg.Wait()

	summary := strings.Join(texts, "")
	summaryTurn := state.Turn{
		Role:      state.RoleSystem,
		Artifacts: []artifact.Artifact{artifact.Text{Content: summary}},
	}

	result := make([]state.Turn, 0, 1+len(toPreserve))
	result = append(result, summaryTurn)
	result = append(result, toPreserve...)
	return result, nil
}
