package models_test

import (
	"testing"

	"github.com/andrewhowdencom/ore/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpec_ZeroValue(t *testing.T) {
	t.Parallel()

	var s models.Spec

	// All fields are zero / nil. This is the "no opinion" Spec;
	// an adapter that receives it falls back to the model's
	// defaults for every field.
	assert.Equal(t, "", s.Name, "Name is empty string")
	assert.Equal(t, 0, s.Window, "Window is 0")
	assert.Equal(t, int64(0), s.MaxOutputTokens, "MaxOutputTokens is 0")
	assert.Nil(t, s.Temperature, "Temperature is nil")
	assert.Equal(t, models.ThinkingLevel(""), s.ThinkingLevel, "ThinkingLevel is empty")
	assert.Nil(t, s.TopP, "TopP is nil")
	assert.Nil(t, s.TopK, "TopK is nil")
	assert.Nil(t, s.Seed, "Seed is nil")
	assert.Nil(t, s.StopSequences, "StopSequences is nil")
	assert.Nil(t, s.FrequencyPenalty, "FrequencyPenalty is nil")
	assert.Nil(t, s.PresencePenalty, "PresencePenalty is nil")
}

func TestSpec_PointerFieldIdentity(t *testing.T) {
	t.Parallel()

	// Pointer fields preserve identity through assignment, so
	// adapter code can read them without copying.
	temp := 0.7
	topP := 0.9
	topK := 40
	seed := int64(42)
	fp := 0.1
	pp := 0.2

	s := models.Spec{
		Name:             "gpt-4o",
		Window:           128_000,
		MaxOutputTokens:  16_384,
		Temperature:      &temp,
		ThinkingLevel:    models.ThinkingLevelMedium,
		TopP:             &topP,
		TopK:             &topK,
		Seed:             &seed,
		FrequencyPenalty: &fp,
		PresencePenalty:  &pp,
		StopSequences:    []string{"\n\nUser:"},
	}

	// Each pointer should be the same identity that was
	// originally assigned; we didn't copy the value.
	assert.Same(t, &temp, s.Temperature)
	assert.Same(t, &topP, s.TopP)
	assert.Same(t, &topK, s.TopK)
	assert.Same(t, &seed, s.Seed)
	assert.Same(t, &fp, s.FrequencyPenalty)
	assert.Same(t, &pp, s.PresencePenalty)
}

func TestThinkingLevel_Valid(t *testing.T) {
	t.Parallel()

	cases := []struct {
		level models.ThinkingLevel
		want  bool
	}{
		{models.ThinkingLevelOff, true},
		{models.ThinkingLevelMinimal, true},
		{models.ThinkingLevelLow, true},
		{models.ThinkingLevelMedium, true},
		{models.ThinkingLevelHigh, true},
		{models.ThinkingLevelMax, true},
		{models.ThinkingLevel(""), false},
		{models.ThinkingLevel("MEDIUM"), false}, // case-sensitive
		{models.ThinkingLevel("foo"), false},
		{models.ThinkingLevel(" off"), false}, // whitespace-sensitive
		{models.ThinkingLevel("off "), false},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, tc.level.Valid(), "%q.Valid()", tc.level)
	}
}

func TestParseThinkingLevel(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in      string
		want    models.ThinkingLevel
		wantErr bool
	}{
		{"off", models.ThinkingLevelOff, false},
		{"minimal", models.ThinkingLevelMinimal, false},
		{"low", models.ThinkingLevelLow, false},
		{"medium", models.ThinkingLevelMedium, false},
		{"high", models.ThinkingLevelHigh, false},
		{"max", models.ThinkingLevelMax, false},
		{"", "", true},
		{"MEDIUM", "", true},
		{"foo", "", true},
		{" off", "", true},
	}
	for _, tc := range cases {
		got, err := models.ParseThinkingLevel(tc.in)
		if tc.wantErr {
			assert.Error(t, err, "input %q should produce an error", tc.in)
			continue
		}
		require.NoError(t, err, "input %q should not error", tc.in)
		assert.Equal(t, tc.want, got, "input %q", tc.in)
	}
}

func TestThinkingLevel_Constants(t *testing.T) {
	t.Parallel()

	cases := []struct {
		level models.ThinkingLevel
		want  string
	}{
		{models.ThinkingLevelOff, "off"},
		{models.ThinkingLevelMinimal, "minimal"},
		{models.ThinkingLevelLow, "low"},
		{models.ThinkingLevelMedium, "medium"},
		{models.ThinkingLevelHigh, "high"},
		{models.ThinkingLevelMax, "max"},
	}
	for _, tc := range cases {
		assert.Equal(t, tc.want, string(tc.level), "constant string value")
		assert.True(t, tc.level.Valid(), "%q should be valid", tc.level)
	}
}
