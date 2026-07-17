package export

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/ledger"
)

func TestJSON(t *testing.T) {
	buf := ledger.NewThread()
	buf.Append(ledger.RoleUser, artifact.Text{Content: "Hello!"})
	thread := Thread{
		ID:       "thread-json-1",
		Turns:    buf.Turns(),
		Metadata: map[string]string{"foo": "bar"},
	}

	var w bytes.Buffer
	if err := JSON(&w, thread); err != nil {
		t.Fatalf("JSON() error = %v", err)
	}

	got := w.String()

	// Should be valid JSON.
	var check map[string]any
	if err := json.Unmarshal([]byte(got), &check); err != nil {
		t.Fatalf("JSON() output is not valid JSON: %v\noutput:\n%s", err, got)
	}

	// Should be indented (pretty-printed).
	if !strings.Contains(got, "\n") {
		t.Error("JSON() output is not indented")
	}

	// Should contain known fields.
	wantSubstrs := []string{
		`"id": "thread-json-1"`,
		// CreatedAt/UpdatedAt are no longer in the wire format.
		`"foo": "bar"`,
		`"role": "user"`,
		`"Hello!"`,
	}
	for _, want := range wantSubstrs {
		if !strings.Contains(got, want) {
			t.Errorf("JSON() output missing substring %q\ngot:\n%s", want, got)
		}
	}
}

// TestJSON_WireFormatShape asserts the user-facing wire format
// shape — {id, current_tip, metadata, turns} with no
// created_at/updated_at — so external tooling that consumes the
// JSON export keeps working unchanged across versions.
func TestJSON_WireFormatShape(t *testing.T) {
	buf := ledger.NewThread()
	buf.Append(ledger.RoleUser, artifact.Text{Content: "Hello!"})
	thread := Thread{
		ID:       "wire-shape",
		Turns:    buf.Turns(),
		Metadata: map[string]string{"k": "v"},
	}

	var w bytes.Buffer
	if err := JSON(&w, thread); err != nil {
		t.Fatalf("JSON() error = %v", err)
	}
	got := w.String()

	// Top-level shape: must contain exactly id, current_tip, metadata,
	// turns. Use a struct round-trip to assert the field set.
	var decoded map[string]any
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatalf("unmarshal: %v\noutput:\n%s", err, got)
	}
	wantKeys := []string{"id", "current_tip", "metadata", "turns"}
	for _, k := range wantKeys {
		if _, ok := decoded[k]; !ok {
			t.Errorf("missing top-level key %q in output:\n%s", k, got)
		}
	}
	// Banned keys (legacy junk fields that must never appear).
	bannedKeys := []string{"created_at", "updated_at", "createdAt", "updatedAt"}
	for _, k := range bannedKeys {
		if _, ok := decoded[k]; ok {
			t.Errorf("unexpected legacy key %q in output:\n%s", k, got)
		}
	}

	// current_tip should be the last turn's ID (or omitted if empty).
	last := buf.Turns()
	if lastID, ok := decoded["current_tip"].(string); !ok || lastID != last[len(last)-1].ID {
		t.Errorf("current_tip: got %v, want %q", decoded["current_tip"], last[len(last)-1].ID)
	}
}

// TestJSON_EmptyTurns confirms that an empty turn list omits
// current_tip (per the `omitempty` tag) and produces a valid
// minimal document.
func TestJSON_EmptyTurns(t *testing.T) {
	thread := Thread{ID: "empty"}

	var w bytes.Buffer
	if err := JSON(&w, thread); err != nil {
		t.Fatalf("JSON() error = %v", err)
	}
	got := w.String()

	var decoded map[string]any
	if err := json.Unmarshal([]byte(got), &decoded); err != nil {
		t.Fatalf("unmarshal: %v\noutput:\n%s", err, got)
	}
	if _, ok := decoded["current_tip"]; ok {
		t.Errorf("current_tip should be omitted for empty turns, got: %v", decoded["current_tip"])
	}
	if decoded["id"] != "empty" {
		t.Errorf("id: got %v, want %q", decoded["id"], "empty")
	}
}