package http

import (
	"testing"
	"time"
)

// TestPaginateAndSortThreads_Empty verifies that an empty input
// produces an empty page with no next-cursor and no error.
func TestPaginateAndSortThreads_Empty(t *testing.T) {


	page, next, err := paginateAndSortThreads(nil, 20, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page) != 0 {
		t.Fatalf("expected empty page, got %d items", len(page))
	}
	if next != "" {
		t.Fatalf("expected no next cursor, got %q", next)
	}
}

// TestPaginateAndSortThreads_OrderByLastActivity verifies that
// threads are sorted by LastAt descending and that ties on
// timestamp are broken by ID ascending.
func TestPaginateAndSortThreads_OrderByLastActivity(t *testing.T) {


	threads := []ThreadSummary{
		{ID: "zebra", LastAt: time.Date(2024, 1, 1, 0, 0, 1, 0, time.UTC)},
		{ID: "apple", LastAt: time.Date(2024, 1, 1, 0, 0, 3, 0, time.UTC)},
		{ID: "mango", LastAt: time.Date(2024, 1, 1, 0, 0, 2, 0, time.UTC)},
	}

	page, _, err := paginateAndSortThreads(threads, 10, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page) != 3 {
		t.Fatalf("expected 3 items, got %d", len(page))
	}
	if page[0].ID != "apple" {
		t.Errorf("expected apple first (latest activity), got %q", page[0].ID)
	}
	if page[1].ID != "mango" {
		t.Errorf("expected mango second, got %q", page[1].ID)
	}
	if page[2].ID != "zebra" {
		t.Errorf("expected zebra third, got %q", page[2].ID)
	}
}

// TestPaginateAndSortThreads_TieOnTimestampBreaksByID verifies the
// ID-ascending tiebreaker for threads that share a timestamp.
func TestPaginateAndSortThreads_TieOnTimestampBreaksByID(t *testing.T) {


	at := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	threads := []ThreadSummary{
		{ID: "z", LastAt: at},
		{ID: "a", LastAt: at},
		{ID: "m", LastAt: at},
	}

	page, _, err := paginateAndSortThreads(threads, 10, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got, want := page[0].ID, "a"; got != want {
		t.Errorf("first = %q, want %q", got, want)
	}
	if got, want := page[1].ID, "m"; got != want {
		t.Errorf("second = %q, want %q", got, want)
	}
	if got, want := page[2].ID, "z"; got != want {
		t.Errorf("third = %q, want %q", got, want)
	}
}

// TestPaginateAndSortThreads_EmptyThreadsSortLast verifies that
// threads with zero LastAt sort to the end.
func TestPaginateAndSortThreads_EmptyThreadsSortLast(t *testing.T) {


	threads := []ThreadSummary{
		{ID: "empty1", LastAt: time.Time{}},
		{ID: "real", LastAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
		{ID: "empty2", LastAt: time.Time{}},
	}

	page, _, err := paginateAndSortThreads(threads, 10, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if page[0].ID != "real" {
		t.Errorf("expected real first, got %q", page[0].ID)
	}
	if page[1].ID != "empty1" {
		t.Errorf("expected empty1 before empty2 (ID order), got %q", page[1].ID)
	}
	if page[2].ID != "empty2" {
		t.Errorf("expected empty2 last, got %q", page[2].ID)
	}
}

// TestPaginateAndSortThreads_LimitAppliesAndYieldsNextCursor
// verifies pagination returns a next cursor when there are more
// items than the limit, and that following the cursor yields the
// remaining items.
func TestPaginateAndSortThreads_LimitAppliesAndYieldsNextCursor(t *testing.T) {


	threads := []ThreadSummary{
		{ID: "t1", LastAt: time.Date(2024, 1, 1, 0, 0, 3, 0, time.UTC)},
		{ID: "t2", LastAt: time.Date(2024, 1, 1, 0, 0, 2, 0, time.UTC)},
		{ID: "t3", LastAt: time.Date(2024, 1, 1, 0, 0, 1, 0, time.UTC)},
		{ID: "t4", LastAt: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
	}

	page, next, err := paginateAndSortThreads(threads, 2, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page) != 2 {
		t.Fatalf("expected 2 items, got %d", len(page))
	}
	if page[0].ID != "t1" || page[1].ID != "t2" {
		t.Errorf("expected first two items, got %+v", page)
	}
	if next == "" {
		t.Fatal("expected a next cursor, got empty")
	}

	// Follow the cursor.
	page2, next2, err := paginateAndSortThreads(threads, 2, next)
	if err != nil {
		t.Fatalf("unexpected error on follow: %v", err)
	}
	if len(page2) != 2 || page2[0].ID != "t3" || page2[1].ID != "t4" {
		t.Errorf("expected remaining items, got %+v", page2)
	}
	if next2 != "" {
		t.Errorf("expected no next cursor on final page, got %q", next2)
	}
}

// TestPaginateAndSortThreads_InvalidCursorReturnsErrInvalidCursor
// verifies that a malformed cursor yields the sentinel error.
func TestPaginateAndSortThreads_InvalidCursorReturnsErrInvalidCursor(t *testing.T) {


	_, _, err := paginateAndSortThreads(nil, 20, "not-a-real-cursor!!!")
	if err == nil {
		t.Fatal("expected error for malformed cursor")
	}
}

// TestParseLimit_ClampingAndDefaults verifies the limit parsing
// behaviour.
func TestParseLimit_ClampingAndDefaults(t *testing.T) {


	cases := []struct {
		in   string
		want int
	}{
		{"", defaultThreadPageSize},
		{"abc", defaultThreadPageSize},
		{"0", 1},
		{"-5", 1},
		{"5", 5},
		{"5000", maxThreadPageSize},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := parseLimit(c.in); got != c.want {
				t.Errorf("parseLimit(%q) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}
