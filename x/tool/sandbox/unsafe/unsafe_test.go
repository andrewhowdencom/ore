package unsafe

import (
	"testing"

	"github.com/andrewhowdencom/ore/tool"
	"github.com/stretchr/testify/assert"
)

// Compile-time assertion that Sandbox implements tool.Sandbox.
var _ tool.Sandbox = (*Sandbox)(nil)

func TestNew(t *testing.T) {
	tests := []struct {
		name        string
		sandboxName string
		wantName    string
	}{
		{"basic name", "foo", "foo"},
		{"empty name", "", ""},
		{"with spaces", "my sandbox", "my sandbox"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sb := New(tt.sandboxName)
			assert.Equal(t, tt.wantName, sb.Name())
		})
	}
}
