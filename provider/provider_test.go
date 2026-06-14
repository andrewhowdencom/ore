package provider_test

import (
	"testing"

	"github.com/andrewhowdencom/ore/provider"
	"github.com/stretchr/testify/assert"
)

// Compile-time interface satisfaction checks. If a future refactor breaks
// the ModelOption / WithModel contract, the package will fail to build
// before tests even run.
var (
	_ provider.InvokeOption = provider.ModelOption{}
	_ provider.InvokeOption = provider.WithModel("")
)

func TestModelOption_Interface(t *testing.T) {
	t.Run("ModelOption struct satisfies InvokeOption", func(t *testing.T) {
		var opt provider.InvokeOption = provider.ModelOption{Model: "gpt-4o-mini"}
		assert.NotNil(t, opt)
	})

	t.Run("WithModel constructor returns InvokeOption with the given name", func(t *testing.T) {
		opt := provider.WithModel("gpt-4o-mini")
		mo, ok := opt.(provider.ModelOption)
		assert.True(t, ok, "WithModel should return a ModelOption value")
		assert.Equal(t, "gpt-4o-mini", mo.Model)
	})

	t.Run("WithModel empty string produces an empty Model field", func(t *testing.T) {
		opt := provider.WithModel("")
		mo, ok := opt.(provider.ModelOption)
		assert.True(t, ok)
		assert.Equal(t, "", mo.Model, "empty input must produce empty Model, not a default")
	})
}
