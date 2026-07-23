package junk

import (
	"context"
	"testing"

	"github.com/andrewhowdencom/ore/loop"
	"github.com/stretchr/testify/assert"
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
