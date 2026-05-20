package calculator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdd(t *testing.T) {
	result, err := Add(context.Background(), map[string]any{"a": 3.0, "b": 5.0})
	require.NoError(t, err)
	assert.InDelta(t, 8.0, result, 0.0001)
}

func TestAdd_WithIntegers(t *testing.T) {
	result, err := Add(context.Background(), map[string]any{"a": 3, "b": 5})
	require.NoError(t, err)
	assert.InDelta(t, 8.0, result, 0.0001)
}

func TestAdd_WithStrings(t *testing.T) {
	result, err := Add(context.Background(), map[string]any{"a": "3", "b": "5"})
	require.NoError(t, err)
	assert.InDelta(t, 8.0, result, 0.0001)
}

func TestMultiply(t *testing.T) {
	result, err := Multiply(context.Background(), map[string]any{"a": 3.0, "b": 5.0})
	require.NoError(t, err)
	assert.InDelta(t, 15.0, result, 0.0001)
}

func TestMultiply_WithIntegers(t *testing.T) {
	result, err := Multiply(context.Background(), map[string]any{"a": 3, "b": 5})
	require.NoError(t, err)
	assert.InDelta(t, 15.0, result, 0.0001)
}

func TestMultiply_WithStrings(t *testing.T) {
	result, err := Multiply(context.Background(), map[string]any{"a": "3", "b": "5"})
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
	result, err := Add(context.Background(), map[string]any{})
	require.NoError(t, err)
	assert.InDelta(t, 0.0, result, 0.0001)
}

func TestMultiply_MissingArgs(t *testing.T) {
	result, err := Multiply(context.Background(), map[string]any{})
	require.NoError(t, err)
	assert.InDelta(t, 0.0, result, 0.0001)
}
