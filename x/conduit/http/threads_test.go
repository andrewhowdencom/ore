package http

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// HTTP-side tests for the threads listing response envelope. The
// underlying sort/paginate/cursor logic is tested in
// github.com/andrewhowdencom/ore/session (session/paginate_test.go);
// the HTTP layer only needs to verify the wire-format shape.

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