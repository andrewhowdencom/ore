package main

import (
	"testing"
)

func TestRun_NoMessage(t *testing.T) {
	t.Setenv("ORE_API_KEY", "test-key")
	if err := run(); err == nil {
		t.Fatal("expected error when no message is provided")
	}
}
