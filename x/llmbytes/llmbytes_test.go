package llmbytes_test

import (
	"encoding/json"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/x/llmbytes"
)

// customArtifact is an unknown kind used to exercise the JSON-envelope
// fallback. It deliberately has a payload smaller than its envelope so
// the test can distinguish "fell through" from "matched a real case".
type customArtifact struct {
	Data string `json:"data"`
}

func (c customArtifact) Kind() string { return "custom" }

func TestOf_Text(t *testing.T) {
	t.Run("value", func(t *testing.T) {
		if got, want := llmbytes.Of(artifact.Text{Content: "hello"}), int64(5); got != want {
			t.Errorf("Of(Text): got %d, want %d", got, want)
		}
	})
	t.Run("pointer", func(t *testing.T) {
		if got, want := llmbytes.Of(&artifact.Text{Content: "hello"}), int64(5); got != want {
			t.Errorf("Of(*Text): got %d, want %d", got, want)
		}
	})
}

func TestOf_Reasoning(t *testing.T) {
	t.Run("value", func(t *testing.T) {
		if got, want := llmbytes.Of(artifact.Reasoning{Content: "think"}), int64(5); got != want {
			t.Errorf("Of(Reasoning): got %d, want %d", got, want)
		}
	})
	t.Run("pointer", func(t *testing.T) {
		if got, want := llmbytes.Of(&artifact.Reasoning{Content: "think"}), int64(5); got != want {
			t.Errorf("Of(*Reasoning): got %d, want %d", got, want)
		}
	})
}

func TestOf_ToolCall(t *testing.T) {
	t.Run("value", func(t *testing.T) {
		tc := artifact.ToolCall{ID: "1", Name: "test", Arguments: `{"x":1}`}
		if got, want := llmbytes.Of(tc), int64(len(tc.LLMString())); got != want {
			t.Errorf("Of(ToolCall): got %d, want %d", got, want)
		}
	})
	t.Run("pointer", func(t *testing.T) {
		tc := &artifact.ToolCall{ID: "1", Name: "test", Arguments: `{"x":1}`}
		if got, want := llmbytes.Of(tc), int64(len(tc.LLMString())); got != want {
			t.Errorf("Of(*ToolCall): got %d, want %d", got, want)
		}
	})
	// ToolCall has a Value variant that, when set, makes LLMString()
	// return the JSON of Value rather than Arguments. Both code paths
	// (value and pointer dispatch) must agree.
	t.Run("value_with_value", func(t *testing.T) {
		tc := artifact.ToolCall{
			ID: "1", Name: "test", Arguments: `{"x":1}`,
			Value: map[string]any{"x": 1},
		}
		if got, want := llmbytes.Of(tc), int64(len(`{"x":1}`)); got != want {
			t.Errorf("Of(ToolCall w/ Value): got %d, want %d", got, want)
		}
	})
	t.Run("pointer_with_value", func(t *testing.T) {
		tc := &artifact.ToolCall{
			ID: "1", Name: "test", Arguments: `{"x":1}`,
			Value: map[string]any{"x": 1},
		}
		if got, want := llmbytes.Of(tc), int64(len(`{"x":1}`)); got != want {
			t.Errorf("Of(*ToolCall w/ Value): got %d, want %d", got, want)
		}
	})
}

func TestOf_ToolResult(t *testing.T) {
	t.Run("value", func(t *testing.T) {
		tr := artifact.ToolResult{ToolCallID: "1", Content: "result"}
		if got, want := llmbytes.Of(tr), int64(len(tr.LLMString())); got != want {
			t.Errorf("Of(ToolResult): got %d, want %d", got, want)
		}
	})
	t.Run("pointer", func(t *testing.T) {
		tr := &artifact.ToolResult{ToolCallID: "1", Content: "result"}
		if got, want := llmbytes.Of(tr), int64(len(tr.LLMString())); got != want {
			t.Errorf("Of(*ToolResult): got %d, want %d", got, want)
		}
	})
	t.Run("value_with_value", func(t *testing.T) {
		tr := artifact.ToolResult{
			ToolCallID: "1", Content: "raw",
			Value: map[string]any{"result": "ok"},
		}
		if got, want := llmbytes.Of(tr), int64(len(`{"result":"ok"}`)); got != want {
			t.Errorf("Of(ToolResult w/ Value): got %d, want %d", got, want)
		}
	})
	t.Run("pointer_with_value", func(t *testing.T) {
		tr := &artifact.ToolResult{
			ToolCallID: "1", Content: "raw",
			Value: map[string]any{"result": "ok"},
		}
		if got, want := llmbytes.Of(tr), int64(len(`{"result":"ok"}`)); got != want {
			t.Errorf("Of(*ToolResult w/ Value): got %d, want %d", got, want)
		}
	})
}

func TestOf_Image(t *testing.T) {
	t.Run("value", func(t *testing.T) {
		if got, want := llmbytes.Of(artifact.Image{URL: "http://a.b/c"}), int64(12); got != want {
			t.Errorf("Of(Image): got %d, want %d", got, want)
		}
	})
	t.Run("pointer", func(t *testing.T) {
		if got, want := llmbytes.Of(&artifact.Image{URL: "http://a.b/c"}), int64(12); got != want {
			t.Errorf("Of(*Image): got %d, want %d", got, want)
		}
	})
}

func TestOf_Usage(t *testing.T) {
	t.Run("value", func(t *testing.T) {
		if got := llmbytes.Of(artifact.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}); got != 0 {
			t.Errorf("Of(Usage): got %d, want 0", got)
		}
	})
	t.Run("pointer", func(t *testing.T) {
		if got := llmbytes.Of(&artifact.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}); got != 0 {
			t.Errorf("Of(*Usage): got %d, want 0", got)
		}
	})
}

func TestOf_Unknown_FallsBackToJSONEnvelope(t *testing.T) {
	// Custom artifact: payload "hi" is 2 bytes, envelope is
	// {"data":"hi"} = 11 bytes. The fallback must report the envelope,
	// which is the worst-case the LLM ever sees and never smaller than
	// the payload.
	c := customArtifact{Data: "hi"}

	envelope, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got, want := llmbytes.Of(c), int64(len(envelope)); got != want {
		t.Errorf("Of(customArtifact): got %d, want %d (JSON envelope)", got, want)
	}
	// Sanity: confirm the test is meaningful (envelope > payload).
	if len(envelope) <= len(c.Data) {
		t.Fatalf("test is not meaningful: envelope (%d) <= payload (%d)",
			len(envelope), len(c.Data))
	}
}

// TestOf_Nil documents that Of returns the JSON envelope length for a
// nil artifact interface (json.Marshal(nil) is "null", 4 bytes). This
// matches the previous countBytes behavior in x/analytics and
// x/telemetry, so no caller is affected. The assertion is here to lock
// the contract down — if a future change adds a nil-guard that returns
// 0, all downstream byte totals shift.
func TestOf_Nil(t *testing.T) {
	if got := llmbytes.Of(nil); got != 4 {
		t.Errorf("Of(nil): got %d, want 4 (\"null\" envelope)", got)
	}
}
