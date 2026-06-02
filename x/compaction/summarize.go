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
// The provider is called with the turns to be summarized loaded into a
// temporary state.Buffer, followed by a user prompt asking for a concise
// summary. Only artifact.Text responses from the provider are collected;
// other artifact types (Usage, Reasoning, ToolCall, etc.) are ignored.
// This is an MVP limitation.
type SummarizeStrategy struct {
	Provider      provider.Provider
	PreserveLastN int
}

// Compact splits turns into summarizable and preserved slices, calls the
// provider to generate a summary, and returns a new slice containing the
// summary turn followed by the preserved turns.
//
// If len(turns) <= PreserveLastN, it returns a defensive copy of the
// original slice (no-op).
//
// The summary turn uses RoleSystem because it is injected context about prior
// conversation, not a real assistant response.
func (s SummarizeStrategy) Compact(ctx context.Context, turns []state.Turn) ([]state.Turn, error) {
	if s.PreserveLastN < 0 {
		return nil, fmt.Errorf("SummarizeStrategy.PreserveLastN must be >= 0, got %d", s.PreserveLastN)
	}
	if len(turns) <= s.PreserveLastN {
		result := make([]state.Turn, len(turns))
		copy(result, turns)
		return result, nil
	}

	toSummarize := turns[:len(turns)-s.PreserveLastN]
	toPreserve := turns[len(turns)-s.PreserveLastN:]

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
