package loop

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSeverity_String(t *testing.T) {
	tests := []struct {
		name string
		sev  Severity
		want string
	}{
		{"success", SeveritySuccess, "Success"},
		{"info", SeverityInfo, "Info"},
		{"warn", SeverityWarn, "Warn"},
		{"error", SeverityError, "Error"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.sev.String())
		})
	}
}

func TestSeverity_String_Unknown(t *testing.T) {
	// Out-of-range values render as a numeric fallback so debuggers can
	// see what was actually stored rather than rendering as an empty string.
	sev := Severity(99)
	assert.Equal(t, "Severity(99)", sev.String())
}

func TestSeverity_ZeroValue(t *testing.T) {
	// The zero value of Severity is SeveritySuccess. This is intentional
	// so a zero-value Notice renders optimistically rather than as an
	// error.
	var s Severity
	assert.Equal(t, SeveritySuccess, s)
	assert.Equal(t, "Success", s.String())
}

func TestNoticeEvent_Kind(t *testing.T) {
	e := NoticeEvent{Notice: Notice{Content: "hi", Severity: SeverityInfo}}
	assert.Equal(t, "notice", e.Kind())
}

func TestNoticeEvent_Context(t *testing.T) {
	ctx := WithProvenance(context.Background(), "test")
	e := NoticeEvent{Notice: Notice{Content: "hi"}, Ctx: ctx}
	assert.Equal(t, ctx, e.Context())
}

func TestNoticeEvent_Context_Nil(t *testing.T) {
	// A nil Ctx must round-trip as nil rather than panicking.
	e := NoticeEvent{Notice: Notice{Content: "hi"}}
	assert.Nil(t, e.Context())
}

func TestNoticeEvent_MarshalJSON_Structure(t *testing.T) {
	e := NoticeEvent{
		Notice: Notice{Content: "hello", Severity: SeverityWarn},
		Ctx:    WithProvenance(context.Background(), "test"),
	}
	data, err := json.Marshal(e)
	require.NoError(t, err)

	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &got))

	assert.Equal(t, "notice", got["kind"])
	assert.Equal(t, "hello", got["content"])
	assert.Equal(t, "Warn", got["severity"])
	// context is present because we attached a provenance.
	require.NotNil(t, got["context"])
	ctxMap, ok := got["context"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "test", ctxMap["provenance"])
}

func TestNoticeEvent_MarshalJSON_OmitsEmptyContext(t *testing.T) {
	// When no provenance or traceparent is on the context, the context
	// field is omitted entirely so the on-the-wire shape is minimal.
	e := NoticeEvent{Notice: Notice{Content: "no-context", Severity: SeverityError}}
	data, err := json.Marshal(e)
	require.NoError(t, err)

	var got map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &got))
	_, present := got["context"]
	assert.False(t, present, "context should be omitted when empty")
}

func TestNoticeEvent_MarshalJSON_RoundTrip(t *testing.T) {
	// The serialised shape must contain every field needed by conduits to
	// reconstruct the original NoticeEvent for assertions or re-emission.
	for _, sev := range []Severity{SeveritySuccess, SeverityInfo, SeverityWarn, SeverityError} {
		t.Run(sev.String(), func(t *testing.T) {
			original := NoticeEvent{
				Notice: Notice{Content: "round-trip", Severity: sev},
				Ctx:    WithProvenance(context.Background(), "rt"),
			}
			data, err := json.Marshal(original)
			require.NoError(t, err)

			var decoded struct {
				Kind     string `json:"kind"`
				Content  string `json:"content"`
				Severity string `json:"severity"`
			}
			require.NoError(t, json.Unmarshal(data, &decoded))

			assert.Equal(t, "notice", decoded.Kind)
			assert.Equal(t, "round-trip", decoded.Content)
			assert.Equal(t, sev.String(), decoded.Severity)
		})
	}
}

func TestNoticeEvent_ImplementsOutputEvent(t *testing.T) {
	// Compile-time guarantee that NoticeEvent satisfies the OutputEvent
	// interface so the EventBus can route it.
	var _ OutputEvent = NoticeEvent{}
}