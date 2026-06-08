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
// replacing all turns with a single synthetic system summary turn.
//
// The provider is called with the full history loaded into a temporary
// state.Buffer, followed by a user prompt asking for a concise summary.
// Only artifact.Text responses from the provider are collected; other artifact
// types (Usage, Reasoning, ToolCall, etc.) are ignored. This is an MVP
// limitation.
type SummarizeStrategy struct {
	Provider provider.Provider
}

// Compact loads all turns into a temporary buffer, calls the provider to
// generate a summary, and returns a single synthetic RoleSystem turn containing
// that summary.
//
// If there are no turns, it returns an empty slice without calling the provider.
//
// The summary turn uses RoleSystem because it is injected context about prior
// conversation, not a real assistant response.
func (s SummarizeStrategy) Compact(ctx context.Context, turns []state.Turn) ([]state.Turn, error) {
	if s.Provider == nil {
		return nil, fmt.Errorf("SummarizeStrategy.Provider must not be nil")
	}
	if len(turns) == 0 {
		return []state.Turn{}, nil
	}

	buf := &state.Buffer{}
	buf.LoadTurns(turns)
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

	return []state.Turn{summaryTurn}, nil
}
