package unsafe

import (
	"testing"

	"github.com/andrewhowdencom/ore/tool"
	"github.com/stretchr/testify/assert"
)

// Compile-time interface check.
var _ tool.Sandbox = (*Sandbox)(nil)

func TestNew_Name(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		wantName string
	}{
		{"foo", "foo"},
		{"bar", "bar"},
		{"", ""},
		{"test-sandbox", "test-sandbox"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			sb := New(tt.name)
			assert.Equal(t, tt.wantName, sb.Name())
		})
	}
}

func TestSandbox_Interface(t *testing.T) {
	t.Parallel()

	// Verify the returned type is the concrete type.
	sb := New("interface-check")
	assert.Implements(t, (*tool.Sandbox)(nil), sb)

	// Verify we can type-assert to the concrete type.
	_, ok := sb.(*Sandbox)
	assert.True(t, ok, "expected sb to be *Sandbox")
}
