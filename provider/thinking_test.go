package provider_test

import (
	"testing"

	"github.com/andrewhowdencom/ore/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestThinkingLevel_Constants(t *testing.T) {
	t.Parallel()

	cases := []struct {
		level provider.ThinkingLevel
		want  string
	}{
		{provider.ThinkingLevelOff, "off"},
		{provider.ThinkingLevelMinimal, "minimal"},
		{provider.ThinkingLevelLow, "low"},
		{provider.ThinkingLevelMedium, "medium"},
		{provider.ThinkingLevelHigh, "high"},
		{provider.ThinkingLevelMax, "max"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, string(tc.level), "constant string value")
		assert.True(t, tc.level.Valid(), "%q should be valid", tc.level)
	}
}

func TestThinkingLevel_Valid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		level provider.ThinkingLevel
		want  bool
	}{
		{provider.ThinkingLevelOff, true},
		{provider.ThinkingLevelMinimal, true},
		{provider.ThinkingLevelLow, true},
		{provider.ThinkingLevelMedium, true},
		{provider.ThinkingLevelHigh, true},
		{provider.ThinkingLevelMax, true},
		{provider.ThinkingLevel(""), false},
		{provider.ThinkingLevel("MEDIUM"), false}, // case-sensitive
		{provider.ThinkingLevel("foo"), false},
		{provider.ThinkingLevel(" off"), false}, // whitespace-sensitive
		{provider.ThinkingLevel("off "), false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, tc.level.Valid(), "%q.Valid()", tc.level)
	}
}

func TestParseThinkingLevel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in      string
		want    provider.ThinkingLevel
		wantErr bool
	}{
		{"off", provider.ThinkingLevelOff, false},
		{"minimal", provider.ThinkingLevelMinimal, false},
		{"low", provider.ThinkingLevelLow, false},
		{"medium", provider.ThinkingLevelMedium, false},
		{"high", provider.ThinkingLevelHigh, false},
		{"max", provider.ThinkingLevelMax, false},
		{"", "", true},
		{"MEDIUM", "", true},
		{"foo", "", true},
		{" off", "", true},
	}
	for _, tc := range cases {
		got, err := provider.ParseThinkingLevel(tc.in)
		if tc.wantErr {
			assert.Error(t, err, "input %q should produce an error", tc.in)
			continue
		}
		require.NoError(t, err, "input %q should not error", tc.in)
		assert.Equal(t, tc.want, got, "input %q", tc.in)
	}
}
