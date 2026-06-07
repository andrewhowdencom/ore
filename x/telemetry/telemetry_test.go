package telemetry

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/state"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testArtifact is a simple artifact type used for testing the unknown-type
// JSON fallback in countChars.
type testArtifact struct {
	KindVal string `json:"kind"`
	Content string `json:"content"`
}

func (t testArtifact) Kind() string { return t.KindVal }

// setupTelemetry creates a Telemetry backed by a real SDK meter provider
// with a ManualReader, so tests can collect and inspect recorded metrics.
func setupTelemetry(t *testing.T) (*Telemetry, sdkmetric.Reader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	meter := mp.Meter("test")
	return New(meter), reader
}

// collectMetrics gathers all metrics from the reader into a ResourceMetrics.
func collectMetrics(t *testing.T, reader sdkmetric.Reader) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	err := reader.Collect(context.Background(), &rm)
	require.NoError(t, err)
	return rm
}

// findMetric searches ResourceMetrics for a counter named name and returns
// its Sum[int64] data. If the metric is not found, ok is false.
func findMetric(t *testing.T, rm metricdata.ResourceMetrics, name string) (metricdata.Sum[int64], bool) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				sum, ok := m.Data.(metricdata.Sum[int64])
				return sum, ok
			}
		}
	}
	return metricdata.Sum[int64]{}, false
}

// attrsMap converts an attribute.Set into a map[string]string for easy
// assertion. It assumes all attributes are string-valued.
func attrsMap(set attribute.Set) map[string]string {
	m := make(map[string]string)
	for _, attr := range set.ToSlice() {
		m[string(attr.Key)] = attr.Value.AsString()
	}
	return m
}

// findDataPoint searches a slice of DataPoint for one whose attributes match
// the expected map. Returns the point and true if found.
func findDataPoint(t *testing.T, points []metricdata.DataPoint[int64], expected map[string]string) (metricdata.DataPoint[int64], bool) {
	t.Helper()
	for _, p := range points {
		ptAttrs := attrsMap(p.Attributes)
		if len(ptAttrs) != len(expected) {
			continue
		}
		match := true
		for k, v := range expected {
			if ptAttrs[k] != v {
				match = false
				break
			}
		}
		if match {
			return p, true
		}
	}
	return metricdata.DataPoint[int64]{}, false
}

func TestCountChars_Text(t *testing.T) {
	assert.Equal(t, int64(5), countChars(artifact.Text{Content: "hello"}))
}

func TestCountChars_Reasoning(t *testing.T) {
	assert.Equal(t, int64(5), countChars(artifact.Reasoning{Content: "think"}))
}

func TestCountChars_ToolCall(t *testing.T) {
	tc := artifact.ToolCall{ID: "1", Name: "test", Arguments: `{"x":1}`}
	assert.Equal(t, int64(len(tc.LLMString())), countChars(tc))
}

func TestCountChars_ToolResult(t *testing.T) {
	tr := artifact.ToolResult{ToolCallID: "1", Content: "result"}
	assert.Equal(t, int64(len(tr.LLMString())), countChars(tr))
}

func TestCountChars_Image(t *testing.T) {
	assert.Equal(t, int64(12), countChars(artifact.Image{URL: "http://a.b/c"}))
}

func TestCountChars_Usage(t *testing.T) {
	assert.Equal(t, int64(0), countChars(artifact.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}))
}

func TestCountChars_UnknownType(t *testing.T) {
	ta := testArtifact{KindVal: "custom", Content: "hello"}
	expected, _ := json.Marshal(ta)
	assert.Equal(t, int64(len(expected)), countChars(ta))
}

func TestCountChars_ToolCallWithValue(t *testing.T) {
	tc := artifact.ToolCall{
		ID:        "1",
		Name:      "test",
		Arguments: `{"x":1}`,
		Value:     map[string]any{"x": 1},
	}
	assert.Equal(t, int64(len(`{"x":1}`)), countChars(tc))
}

func TestCountChars_ToolResultWithValue(t *testing.T) {
	tr := artifact.ToolResult{
		ToolCallID: "1",
		Content:    "raw",
		Value:      map[string]any{"result": "ok"},
	}
	expected := `{"result":"ok"}`
	assert.Equal(t, int64(len(expected)), countChars(tr))
}

func TestNew_NilMeter_IsNoOp(t *testing.T) {
	telemetry := New(nil)
	require.NotNil(t, telemetry)

	cb := telemetry.OnEmit()
	require.NotNil(t, cb)

	assert.NotPanics(t, func() {
		cb(context.Background(), loop.TurnCompleteEvent{
			Turn: state.Turn{
				Role:      state.RoleAssistant,
				Artifacts: []artifact.Artifact{artifact.Text{Content: "hello"}},
				Timestamp: time.Now(),
			},
		})
	})
}

func TestOnEmit_SentCounter_UserRole(t *testing.T) {
	telemetry, reader := setupTelemetry(t)
	cb := telemetry.OnEmit()
	ctx := context.Background()

	cb(ctx, loop.TurnCompleteEvent{
		Turn: state.Turn{
			Role: state.RoleUser,
			Artifacts: []artifact.Artifact{
				artifact.Text{Content: "hello"},
			},
			Timestamp: time.Now(),
		},
	})

	rm := collectMetrics(t, reader)
	sum, ok := findMetric(t, rm, "ore.llm.characters.sent")
	require.True(t, ok)
	require.Len(t, sum.DataPoints, 1)
	assert.Equal(t, int64(5), sum.DataPoints[0].Value)
	assert.Equal(t, map[string]string{
		"artifact.kind": "text",
		"role":          "user",
	}, attrsMap(sum.DataPoints[0].Attributes))

	_, ok = findMetric(t, rm, "ore.llm.characters.received")
	assert.False(t, ok)
}

func TestOnEmit_SentCounter_SystemRole(t *testing.T) {
	telemetry, reader := setupTelemetry(t)
	cb := telemetry.OnEmit()
	ctx := context.Background()

	cb(ctx, loop.TurnCompleteEvent{
		Turn: state.Turn{
			Role: state.RoleSystem,
			Artifacts: []artifact.Artifact{
				artifact.Text{Content: "sys"},
			},
			Timestamp: time.Now(),
		},
	})

	rm := collectMetrics(t, reader)
	sum, ok := findMetric(t, rm, "ore.llm.characters.sent")
	require.True(t, ok)
	require.Len(t, sum.DataPoints, 1)
	assert.Equal(t, int64(3), sum.DataPoints[0].Value)
	assert.Equal(t, map[string]string{
		"artifact.kind": "text",
		"role":          "system",
	}, attrsMap(sum.DataPoints[0].Attributes))
}

func TestOnEmit_SentCounter_ToolRole(t *testing.T) {
	telemetry, reader := setupTelemetry(t)
	cb := telemetry.OnEmit()
	ctx := context.Background()

	cb(ctx, loop.TurnCompleteEvent{
		Turn: state.Turn{
			Role: state.RoleTool,
			Artifacts: []artifact.Artifact{
				artifact.ToolResult{ToolCallID: "1", Content: "res"},
			},
			Timestamp: time.Now(),
		},
	})

	rm := collectMetrics(t, reader)
	sum, ok := findMetric(t, rm, "ore.llm.characters.sent")
	require.True(t, ok)
	require.Len(t, sum.DataPoints, 1)
	assert.Equal(t, map[string]string{
		"artifact.kind": "tool_result",
		"role":          "tool",
	}, attrsMap(sum.DataPoints[0].Attributes))

	_, ok = findMetric(t, rm, "ore.llm.characters.received")
	assert.False(t, ok)
}

func TestOnEmit_ReceivedCounter_AssistantRole(t *testing.T) {
	telemetry, reader := setupTelemetry(t)
	cb := telemetry.OnEmit()
	ctx := context.Background()

	cb(ctx, loop.TurnCompleteEvent{
		Turn: state.Turn{
			Role: state.RoleAssistant,
			Artifacts: []artifact.Artifact{
				artifact.Text{Content: "world"},
				artifact.Reasoning{Content: "think"},
			},
			Timestamp: time.Now(),
		},
	})

	rm := collectMetrics(t, reader)

	// Sent should not be present
	_, ok := findMetric(t, rm, "ore.llm.characters.sent")
	assert.False(t, ok)

	// Received should have two data points (text + reasoning)
	sum, ok := findMetric(t, rm, "ore.llm.characters.received")
	require.True(t, ok)
	require.Len(t, sum.DataPoints, 2)

	textPt, ok := findDataPoint(t, sum.DataPoints, map[string]string{"artifact.kind": "text", "role": "assistant"})
	require.True(t, ok)
	assert.Equal(t, int64(5), textPt.Value)

	reasoningPt, ok := findDataPoint(t, sum.DataPoints, map[string]string{"artifact.kind": "reasoning", "role": "assistant"})
	require.True(t, ok)
	assert.Equal(t, int64(5), reasoningPt.Value)
}

func TestOnEmit_NonTurnCompleteEvent_Ignored(t *testing.T) {
	telemetry, reader := setupTelemetry(t)
	cb := telemetry.OnEmit()
	ctx := context.Background()

	cb(ctx, loop.PropertiesEvent{Properties: map[string]string{"key": "val"}})

	rm := collectMetrics(t, reader)
	_, ok := findMetric(t, rm, "ore.llm.characters.sent")
	assert.False(t, ok)
	_, ok = findMetric(t, rm, "ore.llm.characters.received")
	assert.False(t, ok)
}

func TestOnEmit_ZeroChars_Skipped(t *testing.T) {
	telemetry, reader := setupTelemetry(t)
	cb := telemetry.OnEmit()
	ctx := context.Background()

	cb(ctx, loop.TurnCompleteEvent{
		Turn: state.Turn{
			Role: state.RoleAssistant,
			Artifacts: []artifact.Artifact{
				artifact.Usage{PromptTokens: 10},
			},
			Timestamp: time.Now(),
		},
	})

	rm := collectMetrics(t, reader)
	_, ok := findMetric(t, rm, "ore.llm.characters.received")
	assert.False(t, ok)
}

func TestOnEmit_MultipleArtifacts_MultipleDataPoints(t *testing.T) {
	telemetry, reader := setupTelemetry(t)
	cb := telemetry.OnEmit()
	ctx := context.Background()

	cb(ctx, loop.TurnCompleteEvent{
		Turn: state.Turn{
			Role: state.RoleUser,
			Artifacts: []artifact.Artifact{
				artifact.Text{Content: "hello"},
				artifact.Image{URL: "http://x.y"},
			},
			Timestamp: time.Now(),
		},
	})

	rm := collectMetrics(t, reader)
	sum, ok := findMetric(t, rm, "ore.llm.characters.sent")
	require.True(t, ok)
	require.Len(t, sum.DataPoints, 2)

	textPt, ok := findDataPoint(t, sum.DataPoints, map[string]string{"artifact.kind": "text", "role": "user"})
	require.True(t, ok)
	assert.Equal(t, int64(5), textPt.Value)

	imagePt, ok := findDataPoint(t, sum.DataPoints, map[string]string{"artifact.kind": "image", "role": "user"})
	require.True(t, ok)
	assert.Equal(t, int64(10), imagePt.Value)
}

func TestOnEmit_NilCounter_NoCrash(t *testing.T) {
	// Telemetry with only sent counter, no received counter
	sent, _ := sdkmetric.NewMeterProvider().Meter("test").Int64Counter("test.sent")
	telemetry := &Telemetry{sent: sent}

	cb := telemetry.OnEmit()
	ctx := context.Background()

	// Assistant role with no received counter should not crash
	assert.NotPanics(t, func() {
		cb(ctx, loop.TurnCompleteEvent{
			Turn: state.Turn{
				Role:      state.RoleAssistant,
				Artifacts: []artifact.Artifact{artifact.Text{Content: "test"}},
				Timestamp: time.Now(),
			},
		})
	})
}

func TestOnEmit_IntegrationWithMockMeter(t *testing.T) {
	// This test verifies the full flow through New() -> OnEmit() -> metric recording
	telemetry, reader := setupTelemetry(t)
	cb := telemetry.OnEmit()
	ctx := context.Background()

	cb(ctx, loop.TurnCompleteEvent{
		Turn: state.Turn{
			Role: state.RoleUser,
			Artifacts: []artifact.Artifact{
				artifact.Text{Content: "hi"},
			},
			Timestamp: time.Now(),
		},
	})

	rm := collectMetrics(t, reader)
	sum, ok := findMetric(t, rm, "ore.llm.characters.sent")
	require.True(t, ok)
	require.Len(t, sum.DataPoints, 1)
	assert.Equal(t, int64(2), sum.DataPoints[0].Value)
	assert.Equal(t, map[string]string{
		"artifact.kind": "text",
		"role":          "user",
	}, attrsMap(sum.DataPoints[0].Attributes))
}

func TestOnEmit_DifferentTurns_SameCounter(t *testing.T) {
	telemetry, reader := setupTelemetry(t)
	cb := telemetry.OnEmit()
	ctx := context.Background()

	// Two user turns
	cb(ctx, loop.TurnCompleteEvent{
		Turn: state.Turn{
			Role:      state.RoleUser,
			Artifacts: []artifact.Artifact{artifact.Text{Content: "a"}},
			Timestamp: time.Now(),
		},
	})
	cb(ctx, loop.TurnCompleteEvent{
		Turn: state.Turn{
			Role:      state.RoleUser,
			Artifacts: []artifact.Artifact{artifact.Text{Content: "bb"}},
			Timestamp: time.Now(),
		},
	})

	rm := collectMetrics(t, reader)
	sum, ok := findMetric(t, rm, "ore.llm.characters.sent")
	require.True(t, ok)
	require.Len(t, sum.DataPoints, 1)
	assert.Equal(t, int64(3), sum.DataPoints[0].Value)
}

func TestOnEmit_AssistantTurnWithToolCall(t *testing.T) {
	telemetry, reader := setupTelemetry(t)
	cb := telemetry.OnEmit()
	ctx := context.Background()

	cb(ctx, loop.TurnCompleteEvent{
		Turn: state.Turn{
			Role: state.RoleAssistant,
			Artifacts: []artifact.Artifact{
				artifact.Text{Content: "answer"},
				artifact.ToolCall{ID: "1", Name: "calc", Arguments: `{"a":1}`},
			},
			Timestamp: time.Now(),
		},
	})

	rm := collectMetrics(t, reader)
	sum, ok := findMetric(t, rm, "ore.llm.characters.received")
	require.True(t, ok)
	require.Len(t, sum.DataPoints, 2)

	textPt, ok := findDataPoint(t, sum.DataPoints, map[string]string{"artifact.kind": "text", "role": "assistant"})
	require.True(t, ok)
	assert.Equal(t, int64(6), textPt.Value)
	assert.Equal(t, map[string]string{
		"artifact.kind": "text",
		"role":          "assistant",
	}, attrsMap(textPt.Attributes))

	toolCallPt, ok := findDataPoint(t, sum.DataPoints, map[string]string{"artifact.kind": "tool_call", "role": "assistant"})
	require.True(t, ok)
	assert.Equal(t, int64(7), toolCallPt.Value)
	assert.Equal(t, map[string]string{
		"artifact.kind": "tool_call",
		"role":          "assistant",
	}, attrsMap(toolCallPt.Attributes))
}

func TestOnEmit_ToolResultWithLLMStringValue(t *testing.T) {
	telemetry, reader := setupTelemetry(t)
	cb := telemetry.OnEmit()
	ctx := context.Background()

	cb(ctx, loop.TurnCompleteEvent{
		Turn: state.Turn{
			Role: state.RoleTool,
			Artifacts: []artifact.Artifact{
				artifact.ToolResult{
					ToolCallID: "1",
					Content:    "raw",
					Value:      map[string]any{"result": "ok"},
				},
			},
			Timestamp: time.Now(),
		},
	})

	rm := collectMetrics(t, reader)
	sum, ok := findMetric(t, rm, "ore.llm.characters.sent")
	require.True(t, ok)
	require.Len(t, sum.DataPoints, 1)
	assert.Equal(t, int64(len(`{"result":"ok"}`)), sum.DataPoints[0].Value)
	assert.Equal(t, map[string]string{
		"artifact.kind": "tool_result",
		"role":          "tool",
	}, attrsMap(sum.DataPoints[0].Attributes))
}

func TestOnEmit_UnknownArtifactType(t *testing.T) {
	telemetry, reader := setupTelemetry(t)
	cb := telemetry.OnEmit()
	ctx := context.Background()

	cb(ctx, loop.TurnCompleteEvent{
		Turn: state.Turn{
			Role:      state.RoleUser,
			Artifacts: []artifact.Artifact{testArtifact{KindVal: "custom", Content: "hello"}},
			Timestamp: time.Now(),
		},
	})

	rm := collectMetrics(t, reader)
	sum, ok := findMetric(t, rm, "ore.llm.characters.sent")
	require.True(t, ok)
	require.Len(t, sum.DataPoints, 1)
	expectedJSON, _ := json.Marshal(testArtifact{KindVal: "custom", Content: "hello"})
	assert.Equal(t, int64(len(expectedJSON)), sum.DataPoints[0].Value)
	assert.Equal(t, map[string]string{
		"artifact.kind": "custom",
		"role":          "user",
	}, attrsMap(sum.DataPoints[0].Attributes))
}

func TestOnEmit_EmptyArtifacts_NoMetrics(t *testing.T) {
	telemetry, reader := setupTelemetry(t)
	cb := telemetry.OnEmit()
	ctx := context.Background()

	cb(ctx, loop.TurnCompleteEvent{
		Turn: state.Turn{
			Role:      state.RoleUser,
			Artifacts: []artifact.Artifact{},
			Timestamp: time.Now(),
		},
	})

	rm := collectMetrics(t, reader)
	_, ok := findMetric(t, rm, "ore.llm.characters.sent")
	assert.False(t, ok)
}

func TestOnEmit_MixedArtifactsWithZeroAndNonZero(t *testing.T) {
	telemetry, reader := setupTelemetry(t)
	cb := telemetry.OnEmit()
	ctx := context.Background()

	cb(ctx, loop.TurnCompleteEvent{
		Turn: state.Turn{
			Role: state.RoleUser,
			Artifacts: []artifact.Artifact{
				artifact.Usage{PromptTokens: 10},
				artifact.Text{Content: "hi"},
			},
			Timestamp: time.Now(),
		},
	})

	rm := collectMetrics(t, reader)
	sum, ok := findMetric(t, rm, "ore.llm.characters.sent")
	require.True(t, ok)
	require.Len(t, sum.DataPoints, 1)
	assert.Equal(t, int64(2), sum.DataPoints[0].Value)
	assert.Equal(t, map[string]string{
		"artifact.kind": "text",
		"role":          "user",
	}, attrsMap(sum.DataPoints[0].Attributes))
}