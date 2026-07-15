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
	assertOpsContain(t, pe.Operations, "sent", "100")
	assertOpsContain(t, pe.Operations, "received", "50")
	assertOpsContain(t, pe.Operations, "total", "150")
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
		got := opsToMap(pe.Operations)
		assert.Equal(t, exp, got)
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
	assertOpsContain(t, pe.Operations, "sent", "0")
	assertOpsContain(t, pe.Operations, "received", "0")
	assertOpsContain(t, pe.Operations, "total", "0")
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
	got := opsToMap(props.Operations)
	assert.Equal(t, "10", got["sent"])
	assert.Equal(t, "5", got["received"])
	// total accumulates: 100 * 2 + 15 = 215.
	assert.Equal(t, "215", got["total"])
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
		assertOpsContain(t, pe.Operations, "thinking", want,
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
	got := opsToMap(pe.Operations)
	v, present := got["thinking"]
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
	got := opsToMap(pe.Operations)
	v, present := got["thinking"]
	assert.True(t, present, `"thinking" key must be present even when unknown`)
	assert.Equal(t, "?", v)
}

// opsToMap folds an Operations stream into a key/value map for tests
// that compare against the legacy map[string]string shape. Set ops
// overwrite earlier values for the same key; delete ops remove the
// key entirely.
func opsToMap(ops []loop.PropertyOperation) map[string]string {
	out := make(map[string]string, len(ops))
	for _, op := range ops {
		switch op.Op {
		case loop.PropertyOpSet:
			out[op.Key] = op.Value
		case loop.PropertyOpDelete:
			delete(out, op.Key)
		}
	}
	return out
}

// assertOpsContain asserts that the Operations stream contains a set
// op with the given key carrying the expected value. Fails the test
// if the key is absent or the value differs.
func assertOpsContain(t *testing.T, ops []loop.PropertyOperation, key, want string, msgAndArgs ...interface{}) {
	t.Helper()
	for _, op := range ops {
		if op.Op != loop.PropertyOpSet || op.Key != key {
			continue
		}
		assert.Equal(t, want, op.Value, msgAndArgs...)
		return
	}
	t.Fatalf("key %q not present in operations", key)
}

// ptr returns a pointer to v. Test-only helper for building pointer-typed
// literal values for Usage.ThinkingTokens, whose semantic distinguishes nil
// (provider did not report) from a pointer to zero (provider reported zero).
func ptr[T any](v T) *T { return &v }
