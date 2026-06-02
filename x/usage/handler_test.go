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

func TestHandler_AccumulatesAcrossMultipleUsages(t *testing.T) {
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
		{"sent": "10", "received": "5", "total": "15"},
		{"sent": "30", "received": "15", "total": "45"},
		{"sent": "60", "received": "30", "total": "90"},
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

func TestHandler_ConcurrentAccumulation(t *testing.T) {
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

	// Read final accumulated totals via a zero-usage call.
	e := &mockEmitter{}
	err := h.Handle(context.Background(), artifact.Usage{
		PromptTokens:     0,
		CompletionTokens: 0,
		TotalTokens:      0,
	}, e)
	require.NoError(t, err)
	require.Len(t, e.events, 1)

	props := e.events[0].(loop.PropertiesEvent)
	assert.Equal(t, "100", props.Properties["sent"])
	assert.Equal(t, "100", props.Properties["received"])
	assert.Equal(t, "200", props.Properties["total"])
}
