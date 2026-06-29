package systemprompt

import (
	"context"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTransform_PrependSystemPrompt(t *testing.T) {
	tr, err := New(WithContentFunc(func() string { return "You are a helpful assistant." }))
	require.NoError(t, err)
	base := &ledger.Buffer{}
	base.Append(ledger.RoleUser, artifact.Text{Content: "hello"})

	result, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)

	turns := result.Turns()
	require.Len(t, turns, 2)
	assert.Equal(t, ledger.RoleSystem, turns[0].Role)
	assert.Equal(t, ledger.RoleUser, turns[1].Role)

	text, ok := turns[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "You are a helpful assistant.", text.Content)
}

func TestTransform_EmptyContent(t *testing.T) {
	tr, err := New()
	require.NoError(t, err)
	base := &ledger.Buffer{}
	base.Append(ledger.RoleUser, artifact.Text{Content: "hello"})

	result, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)

	turns := result.Turns()
	require.Len(t, turns, 2)
	assert.Equal(t, ledger.RoleSystem, turns[0].Role)

	text, ok := turns[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Empty(t, text.Content)
}

func TestTransform_NilContentFunc(t *testing.T) {
	tr, err := New(WithContentFunc(nil))
	require.NoError(t, err)
	base := &ledger.Buffer{}
	base.Append(ledger.RoleUser, artifact.Text{Content: "hello"})

	result, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)

	turns := result.Turns()
	require.Len(t, turns, 2)
	assert.Equal(t, ledger.RoleSystem, turns[0].Role)

	text, ok := turns[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Empty(t, text.Content)
}

func TestTransform_DelegatesAppend(t *testing.T) {
	tr, err := New(WithContentFunc(func() string { return "system" }))
	require.NoError(t, err)
	base := &ledger.Buffer{}
	base.Append(ledger.RoleUser, artifact.Text{Content: "user"})

	result, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)

	result.Append(ledger.RoleAssistant, artifact.Text{Content: "assistant"})

	// Base state should have the appended turn
	baseTurns := base.Turns()
	require.Len(t, baseTurns, 2)
	assert.Equal(t, ledger.RoleAssistant, baseTurns[1].Role)

	// Wrapped view should have virtual + base + appended
	turns := result.Turns()
	require.Len(t, turns, 3)
	assert.Equal(t, ledger.RoleSystem, turns[0].Role)
	assert.Equal(t, ledger.RoleUser, turns[1].Role)
	assert.Equal(t, ledger.RoleAssistant, turns[2].Role)
}

func TestTransform_DynamicContent(t *testing.T) {
	var content string
	tr, err := New(WithContentFunc(func() string { return content }))
	require.NoError(t, err)
	base := &ledger.Buffer{}
	base.Append(ledger.RoleUser, artifact.Text{Content: "hello"})

	content = "first prompt"
	result, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)
	turns := result.Turns()
	require.Len(t, turns, 2)
	text, ok := turns[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "first prompt", text.Content)

	content = "second prompt"
	result, err = tr.Transform(context.Background(), base)
	require.NoError(t, err)
	turns = result.Turns()
	require.Len(t, turns, 2)
	text, ok = turns[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "second prompt", text.Content)
}

func TestTransform_MultipleContentFuncs(t *testing.T) {
	tr, err := New(WithContentFuncs(
		func() string { return "First fragment." },
		func() string { return "Second fragment." },
	))
	require.NoError(t, err)
	base := &ledger.Buffer{}
	base.Append(ledger.RoleUser, artifact.Text{Content: "hello"})

	result, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)

	turns := result.Turns()
	require.Len(t, turns, 2)
	assert.Equal(t, ledger.RoleSystem, turns[0].Role)

	text, ok := turns[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "First fragment.\n\nSecond fragment.", text.Content)
}

func TestTransform_MultipleWithContentFuncCalls(t *testing.T) {
	tr, err := New(
		WithContentFunc(func() string { return "First." }),
		WithContentFunc(func() string { return "Second." }),
	)
	require.NoError(t, err)
	base := &ledger.Buffer{}
	base.Append(ledger.RoleUser, artifact.Text{Content: "hello"})

	result, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)

	turns := result.Turns()
	require.Len(t, turns, 2)

	text, ok := turns[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "First.\n\nSecond.", text.Content)
}

func TestTransform_EmptyFragmentSkipped(t *testing.T) {
	tr, err := New(WithContentFuncs(
		func() string { return "" },
		func() string { return "Middle." },
		func() string { return "" },
	))
	require.NoError(t, err)
	base := &ledger.Buffer{}
	base.Append(ledger.RoleUser, artifact.Text{Content: "hello"})

	result, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)

	turns := result.Turns()
	require.Len(t, turns, 2)

	text, ok := turns[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "Middle.", text.Content)
}

func TestTransform_AllEmptyFragments(t *testing.T) {
	tr, err := New(WithContentFuncs(
		func() string { return "" },
		func() string { return "" },
	))
	require.NoError(t, err)
	base := &ledger.Buffer{}
	base.Append(ledger.RoleUser, artifact.Text{Content: "hello"})

	result, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)

	turns := result.Turns()
	require.Len(t, turns, 2)

	text, ok := turns[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Empty(t, text.Content)
}

func TestTransform_NilFuncSkipped(t *testing.T) {
	tr, err := New(WithContentFuncs(
		func() string { return "Before." },
		nil,
		func() string { return "After." },
	))
	require.NoError(t, err)
	base := &ledger.Buffer{}
	base.Append(ledger.RoleUser, artifact.Text{Content: "hello"})

	result, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)

	turns := result.Turns()
	require.Len(t, turns, 2)

	text, ok := turns[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "Before.\n\nAfter.", text.Content)
}

func TestTransform_MixedOptionOrder(t *testing.T) {
	tr, err := New(
		WithContentFunc(func() string { return "A" }),
		WithContentFuncs(
			func() string { return "B" },
			func() string { return "C" },
		),
		WithContentFunc(func() string { return "D" }),
	)
	require.NoError(t, err)
	base := &ledger.Buffer{}
	base.Append(ledger.RoleUser, artifact.Text{Content: "hello"})

	result, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)

	turns := result.Turns()
	require.Len(t, turns, 2)

	text, ok := turns[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "A\n\nB\n\nC\n\nD", text.Content)
}

func TestTransform_ZeroArgWithContentFuncs(t *testing.T) {
	tr, err := New(WithContentFuncs())
	require.NoError(t, err)
	base := &ledger.Buffer{}
	base.Append(ledger.RoleUser, artifact.Text{Content: "hello"})

	result, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)

	turns := result.Turns()
	require.Len(t, turns, 2)

	text, ok := turns[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Empty(t, text.Content)
}

func TestTransform_ExistingSystemTurn(t *testing.T) {
	tr, err := New(WithContentFunc(func() string { return "Injected prompt." }))
	require.NoError(t, err)
	base := &ledger.Buffer{}
	base.Append(ledger.RoleSystem, artifact.Text{Content: "Existing system turn."})
	base.Append(ledger.RoleUser, artifact.Text{Content: "hello"})

	result, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)

	turns := result.Turns()
	require.Len(t, turns, 3)
	assert.Equal(t, ledger.RoleSystem, turns[0].Role)
	assert.Equal(t, ledger.RoleSystem, turns[1].Role)
	assert.Equal(t, ledger.RoleUser, turns[2].Role)

	text0, ok := turns[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "Injected prompt.", text0.Content)

	text1, ok := turns[1].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "Existing system turn.", text1.Content)
}

func TestTransform_InternalNewlines(t *testing.T) {
	tr, err := New(WithContentFuncs(
		func() string { return "First line.\nSecond line." },
		func() string { return "Third line." },
	))
	require.NoError(t, err)
	base := &ledger.Buffer{}
	base.Append(ledger.RoleUser, artifact.Text{Content: "hello"})

	result, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)

	turns := result.Turns()
	require.Len(t, turns, 2)

	text, ok := turns[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "First line.\nSecond line.\n\nThird line.", text.Content)
}

func TestTransform_ContextContentFunc(t *testing.T) {
	tr, err := New(WithContextContentFunc(func(ctx context.Context) string { return "Context-aware." }))
	require.NoError(t, err)
	base := &ledger.Buffer{}
	base.Append(ledger.RoleUser, artifact.Text{Content: "hello"})

	result, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)

	turns := result.Turns()
	require.Len(t, turns, 2)
	assert.Equal(t, ledger.RoleSystem, turns[0].Role)

	text, ok := turns[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "Context-aware.", text.Content)
}

func TestTransform_MixedRegularAndContextContentFuncs(t *testing.T) {
	tr, err := New(
		WithContentFunc(func() string { return "Regular." }),
		WithContextContentFunc(func(ctx context.Context) string { return "Context." }),
		WithContentFunc(func() string { return "Another regular." }),
	)
	require.NoError(t, err)
	base := &ledger.Buffer{}
	base.Append(ledger.RoleUser, artifact.Text{Content: "hello"})

	result, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)

	turns := result.Turns()
	require.Len(t, turns, 2)

	text, ok := turns[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "Regular.\n\nAnother regular.\n\nContext.", text.Content)
}

func TestTransform_NilContextContentFunc(t *testing.T) {
	tr, err := New(WithContextContentFunc(nil))
	require.NoError(t, err)
	base := &ledger.Buffer{}
	base.Append(ledger.RoleUser, artifact.Text{Content: "hello"})

	result, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)

	turns := result.Turns()
	require.Len(t, turns, 2)

	text, ok := turns[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Empty(t, text.Content)
}

func TestTransform_EmptyContextContentFunc(t *testing.T) {
	tr, err := New(WithContextContentFunc(func(ctx context.Context) string { return "" }))
	require.NoError(t, err)
	base := &ledger.Buffer{}
	base.Append(ledger.RoleUser, artifact.Text{Content: "hello"})

	result, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)

	turns := result.Turns()
	require.Len(t, turns, 2)

	text, ok := turns[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Empty(t, text.Content)
}

func TestTransform_ContextContentFuncReceivesContext(t *testing.T) {
	type ctxKey struct{}
	tr, err := New(WithContextContentFunc(func(ctx context.Context) string {
		val, ok := ctx.Value(ctxKey{}).(string)
		if !ok {
			return "no-value"
		}
		return val
	}))
	require.NoError(t, err)
	base := &ledger.Buffer{}
	base.Append(ledger.RoleUser, artifact.Text{Content: "hello"})

	ctx := context.WithValue(context.Background(), ctxKey{}, "test-value")
	result, err := tr.Transform(ctx, base)
	require.NoError(t, err)

	turns := result.Turns()
	require.Len(t, turns, 2)

	text, ok := turns[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "test-value", text.Content)
}

func TestTransform_MultipleContextContentFuncs_OrderAndSkipping(t *testing.T) {
	tr, err := New(
		WithContentFunc(func() string { return "Regular." }),
		WithContextContentFunc(func(ctx context.Context) string { return "Ctx A." }),
		WithContextContentFunc(nil),
		WithContextContentFunc(func(ctx context.Context) string { return "" }),
		WithContextContentFunc(func(ctx context.Context) string { return "Ctx B." }),
		WithContentFunc(func() string { return "Another regular." }),
	)
	require.NoError(t, err)
	base := &ledger.Buffer{}
	base.Append(ledger.RoleUser, artifact.Text{Content: "hello"})

	result, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)

	turns := result.Turns()
	require.Len(t, turns, 2)

	text, ok := turns[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Equal(t, "Regular.\n\nAnother regular.\n\nCtx A.\n\nCtx B.", text.Content)
}

func TestTransform_WithToolExamples(t *testing.T) {
	tr, err := New(WithToolExamples([]tool.Tool{
		{
			Name:        "add",
			Description: "Add two numbers",
			Examples: []tool.Example{
				{
					Input:       map[string]any{"a": 1, "b": 2},
					Output:      3,
					Explanation: "Adding two integers",
				},
			},
		},
		{
			Name:        "read_file",
			Description: "Read a file",
			Examples: []tool.Example{
				{
					Input:       map[string]any{"path": "hello.go"},
					Output:      "package main\n",
					Explanation: "Reading a Go source file",
				},
			},
		},
		{
			Name:        "no_examples",
			Description: "Has no examples",
		},
	}))
	require.NoError(t, err)
	base := &ledger.Buffer{}
	base.Append(ledger.RoleUser, artifact.Text{Content: "hello"})

	result, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)

	turns := result.Turns()
	require.Len(t, turns, 2)
	assert.Equal(t, ledger.RoleSystem, turns[0].Role)

	text, ok := turns[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)

	assert.Contains(t, text.Content, "Tool Examples:")
	assert.Contains(t, text.Content, "## add")
	assert.Contains(t, text.Content, "Add two numbers")
	assert.Contains(t, text.Content, "### Example 1")
	assert.Contains(t, text.Content, `"a": 1`)
	assert.Contains(t, text.Content, `"b": 2`)
	assert.Contains(t, text.Content, "3")
	assert.Contains(t, text.Content, "Adding two integers")
	assert.Contains(t, text.Content, "## read_file")
	assert.Contains(t, text.Content, "Read a file")
	assert.Contains(t, text.Content, "package main")
	assert.Contains(t, text.Content, "Reading a Go source file")
	assert.NotContains(t, text.Content, "no_examples")
}

func TestTransform_WithToolExamples_Empty(t *testing.T) {
	tr, err := New(WithToolExamples([]tool.Tool{}))
	require.NoError(t, err)
	base := &ledger.Buffer{}
	base.Append(ledger.RoleUser, artifact.Text{Content: "hello"})

	result, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)

	turns := result.Turns()
	require.Len(t, turns, 2)

	text, ok := turns[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Empty(t, text.Content)
}

func TestTransform_WithToolExamples_NoExamples(t *testing.T) {
	tr, err := New(WithToolExamples([]tool.Tool{
		{Name: "no_examples", Description: "Has no examples"},
	}))
	require.NoError(t, err)
	base := &ledger.Buffer{}
	base.Append(ledger.RoleUser, artifact.Text{Content: "hello"})

	result, err := tr.Transform(context.Background(), base)
	require.NoError(t, err)

	turns := result.Turns()
	require.Len(t, turns, 2)

	text, ok := turns[0].Artifacts[0].(artifact.Text)
	require.True(t, ok)
	assert.Empty(t, text.Content)
}
