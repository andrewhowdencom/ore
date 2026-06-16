package usage

import (
	"context"
	"sync"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockEmitter captures emitted OutputEvents for test inspection.
type mockEmitter struct {
	events []loop.OutputEvent
}

func (m *mockEmitter) Emit(ctx context.Context, event loop.OutputEvent) {
	m.events = append(m.events, event)
}

func TestHandler_IgnoresNonUsageArtifacts(t *testing.T) {
	h := New()
	var e mockEmitter

	artifacts := []artifact.Artifact{
		artifact.Text{Content: "hello"},
		artifact.TextDelta{Content: "world"},
		artifact.ToolCall{ID: "call_1", Name: "test"},
	}

	for _, art := range artifacts {
		err := h.Handle(context.Background(), art, &e)
		require.NoError(t, err)
	}

	assert.Empty(t, e.events)
}

func TestHandler_AggregatesUsageAndEmitsProperties(t *testing.T) {
	h := New()
	var e mockEmitter

	err := h.Handle(context.Background(), artifact.Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
	}, &e)
	require.NoError(t, err)

	require.Len(t, e.events, 1)
	pe, ok := e.events[0].(loop.PropertiesEvent)
	require.True(t, ok)
	assert.Equal(t, "100", pe.Properties["sent"])
	assert.Equal(t, "50", pe.Properties["received"])
	assert.Equal(t, "150", pe.Properties["total"])
}

func TestHandler_TracksLastTurnValuesAndAccumulatesTotal(t *testing.T) {
	h := New()
	var e mockEmitter

	usages := []artifact.Usage{
		{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		{PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30},
		{PromptTokens: 30, CompletionTokens: 15, TotalTokens: 45},
	}

	for _, u := range usages {
		err := h.Handle(context.Background(), u, &e)
		require.NoError(t, err)
	}

	require.Len(t, e.events, 3)

	expected := []map[string]string{
		{"sent": "10", "received": "5", "thinking": "0", "total": "15"},
		{"sent": "20", "received": "10", "thinking": "0", "total": "45"},
		{"sent": "30", "received": "15", "thinking": "0", "total": "90"},
	}

	for i, exp := range expected {
		pe, ok := e.events[i].(loop.PropertiesEvent)
		require.True(t, ok)
		assert.Equal(t, exp, pe.Properties)
	}
}

func TestHandler_ZeroUsage(t *testing.T) {
	h := New()
	var e mockEmitter

	err := h.Handle(context.Background(), artifact.Usage{}, &e)
	require.NoError(t, err)

	require.Len(t, e.events, 1)
	pe, ok := e.events[0].(loop.PropertiesEvent)
	require.True(t, ok)
	assert.Equal(t, "0", pe.Properties["sent"])
	assert.Equal(t, "0", pe.Properties["received"])
	assert.Equal(t, "0", pe.Properties["total"])
}

func TestHandler_ConcurrentUpdates(t *testing.T) {
	h := New()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = h.Handle(context.Background(), artifact.Usage{
				PromptTokens:     1,
				CompletionTokens: 1,
				TotalTokens:      2,
			}, &mockEmitter{})
		}()
	}
	wg.Wait()

	// Verify total accumulated correctly and overwrite semantics work
	// by doing a final handle with known values.
	e := &mockEmitter{}
	err := h.Handle(context.Background(), artifact.Usage{
		PromptTokens:     10,
		CompletionTokens: 5,
		TotalTokens:      15,
	}, e)
	require.NoError(t, err)
	require.Len(t, e.events, 1)

	props := e.events[0].(loop.PropertiesEvent)
	assert.Equal(t, "10", props.Properties["sent"])
	assert.Equal(t, "5", props.Properties["received"])
	// total accumulates: 100 * 2 + 15 = 215.
	assert.Equal(t, "215", props.Properties["total"])
}

// TestHandler_EmitsThinkingTokensPerTurn asserts that ThinkingTokens follows
// per-turn (overwrite) semantics: each emitted PropertiesEvent carries the
// latest value, not a running sum. This mirrors the documented contract for
// "sent" and "received" and is the contract the TUI's Ψ indicator depends on.
func TestHandler_EmitsThinkingTokensPerTurn(t *testing.T) {
	h := New()
	var e mockEmitter

	usages := []artifact.Usage{
		{ThinkingTokens: 10},
		{ThinkingTokens: 20},
		{ThinkingTokens: 30},
	}

	for _, u := range usages {
		err := h.Handle(context.Background(), u, &e)
		require.NoError(t, err)
	}

	require.Len(t, e.events, 3)

	for i, want := range []string{"10", "20", "30"} {
		pe, ok := e.events[i].(loop.PropertiesEvent)
		require.True(t, ok)
		assert.Equal(t, want, pe.Properties["thinking"],
			"turn %d: thinking should be overwritten with the latest value", i)
	}
}

// TestHandler_EmitsZeroThinking asserts that a zero ThinkingTokens count is
// emitted as the string "0" (not omitted). The TUI's "show when zero"
// requirement depends on the key being present even when there was no
// extended-thinking activity this turn.
func TestHandler_EmitsZeroThinking(t *testing.T) {
	h := New()
	var e mockEmitter

	err := h.Handle(context.Background(), artifact.Usage{}, &e)
	require.NoError(t, err)

	require.Len(t, e.events, 1)
	pe, ok := e.events[0].(loop.PropertiesEvent)
	require.True(t, ok)
	v, present := pe.Properties["thinking"]
	assert.True(t, present, `"thinking" key must be present even when zero`)
	assert.Equal(t, "0", v)
}
