package http

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestCursor_RoundTrip(t *testing.T) {
	original := threadCursor{
		Version:   threadCursorVersion,
		UpdatedAt: time.Date(2026, 6, 15, 12, 30, 45, 0, time.UTC),
		ID:        "abc-123",
	}

	encoded, err := original.encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if encoded == "" {
		t.Fatal("encoded cursor is empty")
	}

	decoded, err := decodeThreadCursor(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if decoded.Version != original.Version {
		t.Errorf("Version: got %d, want %d", decoded.Version, original.Version)
	}
	if !decoded.UpdatedAt.Equal(original.UpdatedAt) {
		t.Errorf("UpdatedAt: got %v, want %v", decoded.UpdatedAt, original.UpdatedAt)
	}
	if decoded.ID != original.ID {
		t.Errorf("ID: got %q, want %q", decoded.ID, original.ID)
	}
}

func TestCursor_StableEncoding(t *testing.T) {
	c := threadCursor{
		Version:   threadCursorVersion,
		UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		ID:        "x",
	}
	first, err := c.encode()
	if err != nil {
		t.Fatalf("encode #1: %v", err)
	}
	second, err := c.encode()
	if err != nil {
		t.Fatalf("encode #2: %v", err)
	}
	if first != second {
		t.Errorf("cursor encoding is not stable: %q vs %q", first, second)
	}
}

func TestCursor_MalformedBase64(t *testing.T) {
	// Use a clearly invalid base64 string (contains '!' which isn't in the alphabet).
	_, err := decodeThreadCursor("!!!not-base64!!!")
	if err == nil {
		t.Fatal("expected error for malformed base64, got nil")
	}
	if !errors.Is(err, errInvalidCursor) {
		t.Errorf("expected errInvalidCursor, got %v", err)
	}
}

func TestCursor_MalformedJSON(t *testing.T) {
	// Valid base64, but the decoded bytes are not valid JSON.
	notJSON := base64.RawURLEncoding.EncodeToString([]byte("not json"))
	_, err := decodeThreadCursor(notJSON)
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
	if !errors.Is(err, errInvalidCursor) {
		t.Errorf("expected errInvalidCursor, got %v", err)
	}
}

func TestCursor_UnknownVersion(t *testing.T) {
	unknown := threadCursor{Version: 999, UpdatedAt: time.Now(), ID: "x"}
	encoded, err := unknown.encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	_, err = decodeThreadCursor(encoded)
	if err == nil {
		t.Fatal("expected error for unknown version, got nil")
	}
	if !errors.Is(err, errInvalidCursor) {
		t.Errorf("expected errInvalidCursor, got %v", err)
	}
	if !strings.Contains(err.Error(), "999") {
		t.Errorf("error should mention the bad version (999): %v", err)
	}
}

func TestCursor_EmptyString(t *testing.T) {
	_, err := decodeThreadCursor("")
	if err == nil {
		t.Fatal("expected error for empty cursor, got nil")
	}
	if !errors.Is(err, errInvalidCursor) {
		t.Errorf("expected errInvalidCursor, got %v", err)
	}
}

func TestThreadsListResponseJSON_MarshalsToEnvelope(t *testing.T) {
	resp := threadsListResponseJSON{
		Threads: []threadSummaryJSON{
			{ID: "a", CreatedAt: time.Unix(1, 0).UTC(), UpdatedAt: time.Unix(2, 0).UTC()},
		},
		NextCursor: "opaque",
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(data)
	for _, want := range []string{`"threads"`, `"next_cursor"`, `"a"`, `"opaque"`} {
		if !strings.Contains(got, want) {
			t.Errorf("marshalled response missing %q: %s", want, got)
		}
	}
}

func TestThreadsListResponseJSON_OmitsEmptyCursor(t *testing.T) {
	resp := threadsListResponseJSON{
		Threads: []threadSummaryJSON{},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "next_cursor") {
		t.Errorf("expected next_cursor to be omitted when empty, got: %s", data)
	}
}

func TestThreadsListResponseJSON_EmptyThreadsArray(t *testing.T) {
	resp := threadsListResponseJSON{}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(data)
	if !strings.Contains(got, `"threads":[]`) {
		t.Errorf("expected empty threads array, got: %s", got)
	}
}
