package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/cognitive"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/provider"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubProvider is a test double implementing provider.Provider.
// It writes canned artifacts to the result channel and returns
// the configured error.
type stubProvider struct {
	artifacts []artifact.Artifact
	err       error
}

var _ provider.Provider = (*stubProvider)(nil)

func (s *stubProvider) Invoke(_ context.Context, _ ledger.State, _ models.Spec, ch chan<- artifact.Artifact, _ ...provider.InvokeOption) error {
	for _, a := range s.artifacts {
		ch <- a
	}
	return s.err
}

func TestRun_EmitsOneResultPerCase(t *testing.T) {
	stub := &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "Hello, world."},
		},
	}
	cases := []Case{
		{ID: "case-1", Input: "Hi"},
		{ID: "case-2", Input: "Hello"},
	}

	var buf bytes.Buffer
	require.NoError(t, run(&buf, stub, "test-model", "single_shot", cases))

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	require.Len(t, lines, 2)

	for i, line := range lines {
		var r Result
		require.NoError(t, json.Unmarshal([]byte(line), &r), "line %d: %s", i, line)
		assert.Equal(t, cases[i].ID, r.ID)
		assert.Equal(t, cases[i].Input, r.Input)
		assert.Equal(t, "Hello, world.", r.Output)
		assert.Empty(t, r.Error)
		assert.GreaterOrEqual(t, r.DurationMS, int64(0))
	}
}

func TestRun_PerCaseSpecOverride(t *testing.T) {
	stub := &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "OK"},
		},
	}
	cases := []Case{
		{ID: "no-override", Input: "a"},
		{
			ID:    "with-override",
			Input: "b",
			Spec:  &CaseSpec{MaxOutputTokens: 4096},
		},
	}

	var buf bytes.Buffer
	require.NoError(t, run(&buf, stub, "test-model", "single_shot", cases))

	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	require.Len(t, lines, 2)

	var r1, r2 Result
	require.NoError(t, json.Unmarshal([]byte(lines[0]), &r1))
	require.NoError(t, json.Unmarshal([]byte(lines[1]), &r2))

	assert.Empty(t, r1.Error)
	assert.Empty(t, r2.Error)
}

func TestRun_ProviderError(t *testing.T) {
	want := errors.New("provider down")
	stub := &stubProvider{err: want}
	cases := []Case{{ID: "err", Input: "x"}}

	var buf bytes.Buffer
	require.NoError(t, run(&buf, stub, "test-model", "single_shot", cases))

	var r Result
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &r))
	assert.Equal(t, "err", r.ID)
	assert.Contains(t, r.Error, "provider down")
	assert.Empty(t, r.Output)
}

func TestRun_UnknownPatternDefaultsToSingleShot(t *testing.T) {
	stub := &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "ok"},
		},
	}
	cases := []Case{{ID: "x", Input: "y"}}

	var buf bytes.Buffer
	require.NoError(t, run(&buf, stub, "test-model", "unknown", cases))

	var r Result
	require.NoError(t, json.Unmarshal([]byte(strings.TrimSpace(buf.String())), &r))
	assert.Empty(t, r.Error)
	assert.Equal(t, "ok", r.Output)
}

func TestBuildPattern(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{name: "single_shot", want: "single_shot"},
		{name: "react", want: "react"},
		{name: "unknown defaults to single_shot", want: "single_shot"},
		{name: "", want: "single_shot"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := buildPattern(tt.name)
			assert.Equal(t, tt.want, p.Name())
		})
	}
}

func TestLoadCases(t *testing.T) {
	cases := []Case{
		{ID: "a", Input: "1"},
		{ID: "b", Input: "2", Spec: &CaseSpec{MaxOutputTokens: 100}},
	}
	data, err := json.Marshal(cases)
	require.NoError(t, err)

	path := t.TempDir() + "/cases.json"
	require.NoError(t, writeFile(path, data))

	got, err := loadCases(path)
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "a", got[0].ID)
	assert.Equal(t, "2", got[1].Input)
	require.NotNil(t, got[1].Spec)
	assert.Equal(t, int64(100), got[1].Spec.MaxOutputTokens)
}

func TestAssistantText(t *testing.T) {
	turn := ledger.Turn{
		Artifacts: []artifact.Artifact{
			artifact.TextDelta{Content: "a"},
			artifact.TextDelta{Content: "b"},
			artifact.Usage{TotalTokens: 1},
			artifact.Text{Content: "c"},
		},
	}
	assert.Equal(t, "abc", assistantText(turn))
}

func TestRunCase_ContextCancellation(t *testing.T) {
	stub := &stubProvider{
		artifacts: []artifact.Artifact{
			artifact.Text{Content: "ignored"},
		},
	}
	a := newTestAgent(t, stub, &cognitive.SingleShot{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before RunCase

	res := runCase(ctx, a, Case{ID: "x", Input: "y"})
	require.Error(t, errors.New(res.Error))
	assert.Equal(t, "x", res.ID)
}
