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
		{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15, ThinkingTokens: ptr(0)},
		{PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30, ThinkingTokens: ptr(0)},
		{PromptTokens: 30, CompletionTokens: 15, TotalTokens: 45, ThinkingTokens: ptr(0)},
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
		{ThinkingTokens: ptr(10)},
		{ThinkingTokens: ptr(20)},
		{ThinkingTokens: ptr(30)},
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

// TestHandler_EmitsZeroThinking asserts that an explicitly-zero ThinkingTokens
// count (a non-nil pointer to 0) is emitted as the string "0" (not omitted,
// not "?"). The TUI's "show when zero" requirement depends on the key being
// present even when there was no extended-thinking activity this turn. The
// nil-pointer case is covered separately by TestHandler_EmitsUnknownWhenNil.
func TestHandler_EmitsZeroThinking(t *testing.T) {
	h := New()
	var e mockEmitter

	err := h.Handle(context.Background(), artifact.Usage{ThinkingTokens: ptr(0)}, &e)
	require.NoError(t, err)

	require.Len(t, e.events, 1)
	pe, ok := e.events[0].(loop.PropertiesEvent)
	require.True(t, ok)
	v, present := pe.Properties["thinking"]
	assert.True(t, present, `"thinking" key must be present even when zero`)
	assert.Equal(t, "0", v)
}

// TestHandler_EmitsUnknownWhenNil asserts that a nil ThinkingTokens pointer
// is rendered as "?" rather than "0". The "?" sentinel is what the TUI turns
// into Ψ ? so the user can distinguish "provider did not report thinking"
// from "provider reported zero thinking". This is the only behavior change
// introduced by the *int conversion; all prior "zero" expectations relied on
// the SDK's silent zero-default for absent fields.
func TestHandler_EmitsUnknownWhenNil(t *testing.T) {
	h := New()
	var e mockEmitter

	err := h.Handle(context.Background(), artifact.Usage{}, &e)
	require.NoError(t, err)

	require.Len(t, e.events, 1)
	pe, ok := e.events[0].(loop.PropertiesEvent)
	require.True(t, ok)
	v, present := pe.Properties["thinking"]
	assert.True(t, present, `"thinking" key must be present even when unknown`)
	assert.Equal(t, "?", v)
}

// TestHandler_EmitsCacheReadAndCacheWrite asserts that non-zero
// CacheReadTokens and CacheWriteTokens flow through to the
// PropertiesEvent as per-turn "cache_read" and "cache_write" keys. Both
// fields are sourced from Anthropic-style prompt caching
// (cache_read_input_tokens and cache_creation_input_tokens) and are
// emitted verbatim — the framework does not sum them, leaving the
// per-provider window arithmetic to the user.
func TestHandler_EmitsCacheReadAndCacheWrite(t *testing.T) {
	h := New()
	var e mockEmitter

	err := h.Handle(context.Background(), artifact.Usage{
		PromptTokens:     2000,
		CompletionTokens: 800,
		TotalTokens:      2800,
		CacheReadTokens:  148000,
		CacheWriteTokens: 5000,
	}, &e)
	require.NoError(t, err)

	require.Len(t, e.events, 1)
	pe, ok := e.events[0].(loop.PropertiesEvent)
	require.True(t, ok)
	assert.Equal(t, "2000", pe.Properties["sent"])
	assert.Equal(t, "800", pe.Properties["received"])
	assert.Equal(t, "2800", pe.Properties["total"])
	assert.Equal(t, "148000", pe.Properties["cache_read"])
	assert.Equal(t, "5000", pe.Properties["cache_write"])
}

// TestHandler_OmitsCacheKeysWhenZero asserts that the producer-side
// hide-when-zero rule is enforced here: when CacheReadTokens and
// CacheWriteTokens are both zero, neither key is emitted, so the
// renderer's empty-string filter drops the segment and the status
// bar stays at its previous four-segment layout. This is the
// behaviour observed on providers that do not report caching
// (cache_read, cache_write omitted entirely) and on turns where the
// cache buckets are naturally empty.
func TestHandler_OmitsCacheKeysWhenZero(t *testing.T) {
	h := New()
	var e mockEmitter

	err := h.Handle(context.Background(), artifact.Usage{
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		// CacheReadTokens and CacheWriteTokens left at zero value.
	}, &e)
	require.NoError(t, err)

	require.Len(t, e.events, 1)
	pe, ok := e.events[0].(loop.PropertiesEvent)
	require.True(t, ok)
	_, hasRead := pe.Properties["cache_read"]
	_, hasWrite := pe.Properties["cache_write"]
	assert.False(t, hasRead, "cache_read must be omitted when CacheReadTokens is zero")
	assert.False(t, hasWrite, "cache_write must be omitted when CacheWriteTokens is zero")
}

// TestHandler_CacheKeysArePerTurn asserts that cache_read and
// cache_write follow the same per-turn overwrite convention as
// prompt and completion: a later call's values replace the
// earlier values; no accumulation. This matches the documented
// contract that cache counts are a property of the most recent
// request, not the running total.
func TestHandler_CacheKeysArePerTurn(t *testing.T) {
	h := New()
	var e mockEmitter

	usages := []artifact.Usage{
		{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15,
			CacheReadTokens: 100, CacheWriteTokens: 50},
		{PromptTokens: 20, CompletionTokens: 10, TotalTokens: 30,
			CacheReadTokens: 200, CacheWriteTokens: 0},
	}

	for _, u := range usages {
		err := h.Handle(context.Background(), u, &e)
		require.NoError(t, err)
	}

	require.Len(t, e.events, 2)

	first := e.events[0].(loop.PropertiesEvent).Properties
	assert.Equal(t, "100", first["cache_read"], "first call's cache_read must persist")
	assert.Equal(t, "50", first["cache_write"], "first call's cache_write must persist")

	second := e.events[1].(loop.PropertiesEvent).Properties
	assert.Equal(t, "200", second["cache_read"], "second call's cache_read must overwrite")
	_, hasWrite := second["cache_write"]
	assert.False(t, hasWrite, "second call's zero cache_write must be omitted (not preserved as 0)")
}

// ptr returns a pointer to v. Test-only helper for building pointer-typed
// literal values for Usage.ThinkingTokens, whose semantic distinguishes nil
// (provider did not report) from a pointer to zero (provider reported zero).
func ptr[T any](v T) *T { return &v }
