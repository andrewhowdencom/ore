package session

import (
	"testing"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/stretchr/testify/assert"
)

func TestUserMessageEvent_Kind(t *testing.T) {
	e := UserMessageEvent{Content: "hello"}
	assert.Equal(t, "user_message", e.Kind())
}

func TestUserMessageEvent_Context(t *testing.T) {
	e := UserMessageEvent{Content: "hello", Ctx: loop.EventContext{Provenance: "test-id"}}
	assert.Equal(t, "test-id", e.Context().Provenance)
}

func TestInterruptEvent_Kind(t *testing.T) {
	e := InterruptEvent{}
	assert.Equal(t, "interrupt", e.Kind())
}

func TestInterruptEvent_Context(t *testing.T) {
	e := InterruptEvent{Ctx: loop.EventContext{Provenance: "test-id"}}
	assert.Equal(t, "test-id", e.Context().Provenance)
}

func TestEventInterface(t *testing.T) {
	// Verify both types satisfy the Event interface.
	var _ Event = UserMessageEvent{}
	var _ Event = InterruptEvent{}
}
