package junk

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUserMessageEvent_Kind(t *testing.T) {
	e := UserMessageEvent{Content: "hello"}
	assert.Equal(t, "user_message", e.Kind())
}

func TestUserMessageEvent_Context(t *testing.T) {
	e := UserMessageEvent{Content: "hello", Ctx: loop.WithProvenance(context.Background(), "test-id")}
	prov, _ := loop.ProvenanceFrom(e.Context())
	assert.Equal(t, "test-id", prov)
}

func TestInterruptEvent_Kind(t *testing.T) {
	e := InterruptEvent{}
	assert.Equal(t, "interrupt", e.Kind())
}

func TestInterruptEvent_Context(t *testing.T) {
	e := InterruptEvent{Ctx: loop.WithProvenance(context.Background(), "test-id")}
	prov, _ := loop.ProvenanceFrom(e.Context())
	assert.Equal(t, "test-id", prov)
}

func TestEventInterface(t *testing.T) {
	// Verify both types satisfy the Event interface.
	var _ Event = UserMessageEvent{}
	var _ Event = InterruptEvent{}
}

func TestSessionSwitchEvent_Kind(t *testing.T) {
	e := SessionSwitchEvent{SessionID: "test-id"}
	assert.Equal(t, "session_switch", e.Kind())
}

func TestSessionSwitchEvent_Context(t *testing.T) {
	e := SessionSwitchEvent{SessionID: "test-id", Ctx: loop.WithProvenance(context.Background(), "test-provenance")}
	prov, _ := loop.ProvenanceFrom(e.Context())
	assert.Equal(t, "test-provenance", prov)
}

func TestSessionSwitchEvent_MarshalJSON(t *testing.T) {
	e := SessionSwitchEvent{SessionID: "test-id"}
	data, err := e.MarshalJSON()
	require.NoError(t, err)
	assert.JSONEq(t, `{"kind":"session_switch","session_id":"test-id"}`, string(data))
}

func TestSessionSwitchEvent_MarshalJSON_WithContext(t *testing.T) {
	ctx := loop.WithProvenance(context.Background(), "http")
	e := SessionSwitchEvent{SessionID: "test-id", Ctx: ctx}
	data, err := e.MarshalJSON()
	require.NoError(t, err)
	assert.JSONEq(t, `{"kind":"session_switch","session_id":"test-id","context":{"provenance":"http"}}`, string(data))
}

func TestSessionSwitchEvent_MarshalJSON_RoundTrip(t *testing.T) {
	ctx := loop.WithProvenance(context.Background(), "http")
	e := SessionSwitchEvent{SessionID: "test-id", Ctx: ctx}
	data, err := e.MarshalJSON()
	require.NoError(t, err)

	var raw map[string]interface{}
	err = json.Unmarshal(data, &raw)
	require.NoError(t, err)
	assert.Equal(t, "session_switch", raw["kind"])
	assert.Equal(t, "test-id", raw["session_id"])
	require.NotNil(t, raw["context"])
	contextMap := raw["context"].(map[string]interface{})
	assert.Equal(t, "http", contextMap["provenance"])
}
