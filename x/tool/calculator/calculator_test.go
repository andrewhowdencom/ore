package calculator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdd(t *testing.T) {
	result, err := Add(context.Background(), nil, map[string]any{"a": 3.0, "b": 5.0})
	require.NoError(t, err)
	assert.InDelta(t, 8.0, result, 0.0001)
}

func TestAdd_WithIntegers(t *testing.T) {
	result, err := Add(context.Background(), nil, map[string]any{"a": 3, "b": 5})
	require.NoError(t, err)
	assert.InDelta(t, 8.0, result, 0.0001)
}

func TestAdd_WithStrings(t *testing.T) {
	result, err := Add(context.Background(), nil, map[string]any{"a": "3", "b": "5"})
	require.NoError(t, err)
	assert.InDelta(t, 8.0, result, 0.0001)
}

func TestMultiply(t *testing.T) {
	result, err := Multiply(context.Background(), nil, map[string]any{"a": 3.0, "b": 5.0})
	require.NoError(t, err)
	assert.InDelta(t, 15.0, result, 0.0001)
}

func TestMultiply_WithIntegers(t *testing.T) {
	result, err := Multiply(context.Background(), nil, map[string]any{"a": 3, "b": 5})
	require.NoError(t, err)
	assert.InDelta(t, 15.0, result, 0.0001)
}

func TestMultiply_WithStrings(t *testing.T) {
	result, err := Multiply(context.Background(), nil, map[string]any{"a": "3", "b": "5"})
	require.NoError(t, err)
	assert.InDelta(t, 15.0, result, 0.0001)
}

func TestToFloat64_Float64(t *testing.T) {
	assert.InDelta(t, 3.14, ToFloat64(3.14), 0.0001)
}

func TestToFloat64_Int(t *testing.T) {
	assert.InDelta(t, 42.0, ToFloat64(42), 0.0001)
}

func TestToFloat64_String(t *testing.T) {
	assert.InDelta(t, 2.5, ToFloat64("2.5"), 0.0001)
}

func TestToFloat64_InvalidString(t *testing.T) {
	assert.InDelta(t, 0.0, ToFloat64("not-a-number"), 0.0001)
}

func TestToFloat64_UnknownType(t *testing.T) {
	assert.InDelta(t, 0.0, ToFloat64(true), 0.0001)
}

func TestAdd_MissingArgs(t *testing.T) {
	result, err := Add(context.Background(), nil, map[string]any{})
	require.NoError(t, err)
	assert.InDelta(t, 0.0, result, 0.0001)
}

func TestMultiply_MissingArgs(t *testing.T) {
	result, err := Multiply(context.Background(), nil, map[string]any{})
	require.NoError(t, err)
	assert.InDelta(t, 0.0, result, 0.0001)
}

func TestToFloat64_Nil(t *testing.T) {
	assert.InDelta(t, 0.0, ToFloat64(nil), 0.0001)
}

func TestToFloat64_Int64(t *testing.T) {
	assert.InDelta(t, 42.0, ToFloat64(int64(42)), 0.0001)
}

func TestToFloat64_Uint(t *testing.T) {
	assert.InDelta(t, 42.0, ToFloat64(uint(42)), 0.0001)
}

func TestToFloat64_Float32(t *testing.T) {
	assert.InDelta(t, 3.14, ToFloat64(float32(3.14)), 0.0001)
}

func TestToFloat64_WhitespaceString(t *testing.T) {
	assert.InDelta(t, 3.5, ToFloat64(" 3.5 "), 0.0001)
}

func TestToFloat64_ScientificNotation(t *testing.T) {
	assert.InDelta(t, 100.0, ToFloat64("1e2"), 0.0001)
}

func TestAdd_NegativeNumbers(t *testing.T) {
	result, err := Add(context.Background(), nil, map[string]any{"a": -3.0, "b": 5.0})
	require.NoError(t, err)
	assert.InDelta(t, 2.0, result, 0.0001)
}

func TestMultiply_NegativeNumbers(t *testing.T) {
	result, err := Multiply(context.Background(), nil, map[string]any{"a": -3.0, "b": 5.0})
	require.NoError(t, err)
	assert.InDelta(t, -15.0, result, 0.0001)
}

func TestAdd_ExtraFields(t *testing.T) {
	result, err := Add(context.Background(), nil, map[string]any{"a": 3.0, "b": 5.0, "c": 99.0})
	require.NoError(t, err)
	assert.InDelta(t, 8.0, result, 0.0001)
}

func TestMultiply_ExtraFields(t *testing.T) {
	result, err := Multiply(context.Background(), nil, map[string]any{"a": 3.0, "b": 5.0, "c": 99.0})
	require.NoError(t, err)
	assert.InDelta(t, 15.0, result, 0.0001)
}
