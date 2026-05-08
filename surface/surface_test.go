package surface

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUserMessageEvent_Kind(t *testing.T) {
	e := UserMessageEvent{Content: "hello"}
	assert.Equal(t, "user_message", e.Kind())
}

func TestInterruptEvent_Kind(t *testing.T) {
	e := InterruptEvent{}
	assert.Equal(t, "interrupt", e.Kind())
}

func TestEventInterface(t *testing.T) {
	// Verify both types satisfy the Event interface.
	var _ Event = UserMessageEvent{}
	var _ Event = InterruptEvent{}
}
