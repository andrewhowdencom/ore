package http

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/junk"
	"github.com/andrewhowdencom/ore/ledger"
)

// makeThread builds a *junk.Thread with the given ID and UpdatedAt
// for use in pagination tests. The State and Metadata are initialised
// to non-nil empty values so the helper works in isolation.
func makeThread(id string, updatedAt time.Time) *junk.Thread {
	return &junk.Thread{
		ID:        id,
		State:     &ledger.Buffer{},
		CreatedAt: updatedAt.Add(-time.Hour),
		UpdatedAt: updatedAt,
		Metadata:  map[string]string{},
	}
}

// idsOf extracts the IDs from a slice of threads in order, for assertions.
func idsOf(threads []*junk.Thread) []string {
	out := make([]string, len(threads))
	for i, t := range threads {
		out[i] = t.ID
	}
	return out
}

func TestPaginateAndSortThreads_EmptyInput(t *testing.T) {
	page, next, err := paginateAndSortThreads(nil, 20, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page) != 0 {
		t.Errorf("expected empty page, got %d items", len(page))
	}
	if next != "" {
		t.Errorf("expected empty next cursor, got %q", next)
	}
}

func TestPaginateAndSortThreads_SinglePageReturnsAll(t *testing.T) {
	now := time.Now()
	threads := []*junk.Thread{
		makeThread("a", now.Add(-3*time.Hour)),
		makeThread("b", now.Add(-2*time.Hour)),
		makeThread("c", now.Add(-1*time.Hour)),
	}

	page, next, err := paginateAndSortThreads(threads, 20, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page) != 3 {
		t.Fatalf("expected 3 items, got %d", len(page))
	}
	if next != "" {
		t.Errorf("expected empty next cursor, got %q", next)
	}

	// Verify order: most recent first.
	want := []string{"c", "b", "a"}
	if got := idsOf(page); !slices.Equal(got, want) {
		t.Errorf("order: got %v, want %v", got, want)
	}
}

func TestPaginateAndSortThreads_LimitRespected(t *testing.T) {
	now := time.Now()
	threads := []*junk.Thread{
		makeThread("a", now.Add(-3*time.Hour)),
		makeThread("b", now.Add(-2*time.Hour)),
		makeThread("c", now.Add(-1*time.Hour)),
	}

	page, next, err := paginateAndSortThreads(threads, 2, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page) != 2 {
		t.Fatalf("expected 2 items, got %d", len(page))
	}
	if next == "" {
		t.Error("expected non-empty next cursor when more items remain")
	}

	want := []string{"c", "b"}
	if got := idsOf(page); !slices.Equal(got, want) {
		t.Errorf("order: got %v, want %v", got, want)
	}
}

func TestPaginateAndSortThreads_CursorProgression(t *testing.T) {
	now := time.Now()
	threads := []*junk.Thread{
		makeThread("a", now.Add(-3*time.Hour)),
		makeThread("b", now.Add(-2*time.Hour)),
		makeThread("c", now.Add(-1*time.Hour)),
		makeThread("d", now),
	}

	// First page.
	page1, cursor1, err := paginateAndSortThreads(threads, 2, "")
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if got, want := idsOf(page1), []string{"d", "c"}; !slices.Equal(got, want) {
		t.Errorf("page 1: got %v, want %v", got, want)
	}
	if cursor1 == "" {
		t.Fatal("page 1 should have a next cursor")
	}

	// Second page.
	page2, cursor2, err := paginateAndSortThreads(threads, 2, cursor1)
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if got, want := idsOf(page2), []string{"b", "a"}; !slices.Equal(got, want) {
		t.Errorf("page 2: got %v, want %v", got, want)
	}
	if cursor2 != "" {
		t.Errorf("page 2 should be the last page; got cursor %q", cursor2)
	}
}

func TestPaginateAndSortThreads_CursorProgressionThreePages(t *testing.T) {
	now := time.Now()
	threads := []*junk.Thread{
		makeThread("a", now.Add(-5*time.Hour)),
		makeThread("b", now.Add(-4*time.Hour)),
		makeThread("c", now.Add(-3*time.Hour)),
		makeThread("d", now.Add(-2*time.Hour)),
		makeThread("e", now.Add(-1*time.Hour)),
	}

	// Walk three pages of 2, 2, 1 and verify we visit every thread exactly once.
	seen := []string{}
	cursor := ""
	for pageNum := 1; pageNum <= 3; pageNum++ {
		page, next, err := paginateAndSortThreads(threads, 2, cursor)
		if err != nil {
			t.Fatalf("page %d: %v", pageNum, err)
		}
		seen = append(seen, idsOf(page)...)
		if pageNum < 3 {
			if next == "" {
				t.Fatalf("page %d should have a next cursor", pageNum)
			}
			cursor = next
		} else if next != "" {
			t.Errorf("page %d should be the last; got cursor %q", pageNum, next)
		}
	}

	want := []string{"e", "d", "c", "b", "a"}
	if !slices.Equal(seen, want) {
		t.Errorf("progression order: got %v, want %v", seen, want)
	}
}

func TestPaginateAndSortThreads_TiebreakByID(t *testing.T) {
	// Three threads share the exact same UpdatedAt.
	// They must paginate in id-ascending order, and a cursor pointing
	// at "b" must skip "b" on the next page.
	same := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	threads := []*junk.Thread{
		makeThread("c", same),
		makeThread("a", same),
		makeThread("b", same),
	}

	page1, cursor1, err := paginateAndSortThreads(threads, 2, "")
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if got, want := idsOf(page1), []string{"a", "b"}; !slices.Equal(got, want) {
		t.Errorf("page 1 (tied): got %v, want %v", got, want)
	}
	if cursor1 == "" {
		t.Fatal("page 1 should have a next cursor (c remains)")
	}

	page2, cursor2, err := paginateAndSortThreads(threads, 2, cursor1)
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if got, want := idsOf(page2), []string{"c"}; !slices.Equal(got, want) {
		t.Errorf("page 2 (tied): got %v, want %v", got, want)
	}
	if cursor2 != "" {
		t.Errorf("page 2 should be the last; got cursor %q", cursor2)
	}
}

func TestPaginateAndSortThreads_InvalidCursor(t *testing.T) {
	now := time.Now()
	threads := []*junk.Thread{makeThread("a", now)}

	_, _, err := paginateAndSortThreads(threads, 20, "!!!not-base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid cursor, got nil")
	}
	if !errors.Is(err, errInvalidCursor) {
		t.Errorf("expected errInvalidCursor, got %v", err)
	}
}

func TestPaginateAndSortThreads_CursorPastEndReturnsEmpty(t *testing.T) {
	now := time.Now()
	threads := []*junk.Thread{
		makeThread("a", now.Add(-2*time.Hour)),
		makeThread("b", now.Add(-1*time.Hour)),
	}

	// Build a cursor pointing to "a" (the OLDEST thread). The next page
	// should be empty.
	c, err := (threadCursor{
		Version:   threadCursorVersion,
		UpdatedAt: threads[0].UpdatedAt,
		ID:        threads[0].ID,
	}).encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	page, next, err := paginateAndSortThreads(threads, 20, c)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page) != 0 {
		t.Errorf("expected empty page, got %d items", len(page))
	}
	if next != "" {
		t.Errorf("expected empty next cursor, got %q", next)
	}
}

func TestPaginateAndSortThreads_LimitOne(t *testing.T) {
	now := time.Now()
	threads := []*junk.Thread{
		makeThread("a", now.Add(-2*time.Hour)),
		makeThread("b", now.Add(-1*time.Hour)),
	}

	page, next, err := paginateAndSortThreads(threads, 1, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page) != 1 {
		t.Fatalf("expected 1 item, got %d", len(page))
	}
	if got, want := idsOf(page), []string{"b"}; !slices.Equal(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	if next == "" {
		t.Error("expected next cursor when items remain")
	}
}

func TestPaginateAndSortThreads_LimitLargerThanTotal(t *testing.T) {
	now := time.Now()
	threads := []*junk.Thread{
		makeThread("a", now.Add(-2*time.Hour)),
		makeThread("b", now.Add(-1*time.Hour)),
	}

	page, next, err := paginateAndSortThreads(threads, 1000, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page) != 2 {
		t.Errorf("expected 2 items, got %d", len(page))
	}
	if next != "" {
		t.Errorf("expected empty next cursor, got %q", next)
	}
}

func TestPaginateAndSortThreads_LimitZeroTreatedAsOne(t *testing.T) {
	now := time.Now()
	threads := []*junk.Thread{
		makeThread("a", now.Add(-1*time.Hour)),
		makeThread("b", now),
	}

	page, _, err := paginateAndSortThreads(threads, 0, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page) != 1 {
		t.Errorf("limit 0 should yield 1 item (clamped), got %d", len(page))
	}
}

func TestPaginateAndSortThreads_LimitNegativeTreatedAsOne(t *testing.T) {
	now := time.Now()
	threads := []*junk.Thread{
		makeThread("a", now.Add(-1*time.Hour)),
	}

	page, _, err := paginateAndSortThreads(threads, -5, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page) != 1 {
		t.Errorf("negative limit should yield 1 item (clamped), got %d", len(page))
	}
}

func TestPaginateAndSortThreads_DoesNotMutateInputIDs(t *testing.T) {
	// The function sorts in place; callers should see the same *Thread
	// pointers in the result. Verify the original slice still contains
	// the same pointers (even if the order changed).
	now := time.Now()
	a := makeThread("a", now.Add(-2*time.Hour))
	b := makeThread("b", now.Add(-1*time.Hour))
	threads := []*junk.Thread{a, b}

	page, _, err := paginateAndSortThreads(threads, 20, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page) != 2 {
		t.Fatalf("expected 2 items, got %d", len(page))
	}
	// The most recent should now be first.
	if page[0] != b || page[1] != a {
		t.Errorf("sort mutated incorrectly: got [%p, %p], want [%p, %p]",
			page[0], page[1], b, a)
	}
	// The original slice should now be in sorted order too.
	if threads[0] != b || threads[1] != a {
		t.Errorf("original slice not sorted in place: got [%p, %p]", threads[0], threads[1])
	}
}

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
	// We use json here to construct a valid base64 string for the test.
	validJSON, _ := json.Marshal("hello")
	notJSON := strings.Trim(string(validJSON), `"`) + "!!!"
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

// TestEncodeProducesBase64URL is a sanity test that the cursor encoding
// uses URL-safe base64 with no padding, so cursors survive in query
// strings without further escaping.
func TestEncodeProducesBase64URL(t *testing.T) {
	c := threadCursor{Version: threadCursorVersion, UpdatedAt: time.Now(), ID: "x"}
	encoded, err := c.encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if strings.ContainsAny(encoded, "+/=") {
		t.Errorf("cursor contains non-URL-safe base64 chars: %q", encoded)
	}
	// Verify the format is recoverable.
	decoded, err := decodeThreadCursor(encoded)
	if err != nil {
		t.Fatalf("decode round-trip failed: %v", err)
	}
	if decoded.ID != "x" {
		t.Errorf("round-trip changed ID: got %q", decoded.ID)
	}
}

// Ensure that the comparator used by slices.SortFunc is well-behaved
// across edge cases (equal timestamps, equal IDs, etc.).
func TestCompareThreads(t *testing.T) {
	now := time.Now()
	a := &junk.Thread{ID: "a", UpdatedAt: now}
	b := &junk.Thread{ID: "b", UpdatedAt: now}
	c := &junk.Thread{ID: "c", UpdatedAt: now.Add(time.Hour)}

	if compareThreads(a, b) >= 0 {
		t.Error("a should sort before b (same ts, lower id)")
	}
	if compareThreads(b, a) <= 0 {
		t.Error("b should sort after a (same ts, higher id)")
	}
	if compareThreads(c, a) >= 0 {
		t.Error("c (later ts) should sort before a")
	}
	if compareThreads(a, c) <= 0 {
		t.Error("a (earlier ts) should sort after c")
	}
	// Stability check: compareThreads(a, a) == 0.
	if compareThreads(a, a) != 0 {
		t.Errorf("compareThreads(a, a) = %d, want 0", compareThreads(a, a))
	}
}

// Quick smoke test that threadIsAfterCursor handles the boundary correctly.
func TestThreadIsAfterCursor(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-time.Hour)
	later := now.Add(time.Hour)
	c := threadCursor{Version: 1, UpdatedAt: now, ID: "m"}

	tests := []struct {
		name string
		thr  *junk.Thread
		want bool
	}{
		{"strictly earlier ts", &junk.Thread{ID: "z", UpdatedAt: earlier}, true},
		{"strictly later ts", &junk.Thread{ID: "a", UpdatedAt: later}, false},
		{"same ts, lower id", &junk.Thread{ID: "a", UpdatedAt: now}, false},
		{"same ts, higher id", &junk.Thread{ID: "z", UpdatedAt: now}, true},
		{"same ts, same id", &junk.Thread{ID: "m", UpdatedAt: now}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := threadIsAfterCursor(tt.thr, c)
			if got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}

// Ensure that error messages mention the cursor problem (helps clients
// debug bad cursors).
func TestInvalidCursorError_HasDescriptiveMessage(t *testing.T) {
	_, err := decodeThreadCursor("garbage")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "cursor") {
		t.Errorf("error message should mention 'cursor': %v", err)
	}
	// Also ensure the sentinel error can be unwrapped.
	if !errors.Is(err, errInvalidCursor) {
		t.Errorf("err should unwrap to errInvalidCursor: %v", err)
	}
	_ = fmt.Sprintf
}
