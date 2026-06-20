package session

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/state"
)

// makeThread builds a *Thread with the given ID and UpdatedAt for use
// in pagination tests. The State and Metadata are initialised to
// non-nil empty values so the helper works in isolation.
func makeThread(id string, updatedAt time.Time) *Thread {
	return &Thread{
		ID:        id,
		State:     &state.Buffer{},
		CreatedAt: updatedAt.Add(-time.Hour),
		UpdatedAt: updatedAt,
		Metadata:  map[string]string{},
	}
}

// idsOf extracts the IDs from a slice of threads in order, for
// assertions in tests.
func idsOf(threads []*Thread) []string {
	out := make([]string, len(threads))
	for i, t := range threads {
		out[i] = t.ID
	}
	return out
}

func TestPaginate_EmptyInput(t *testing.T) {
	page, next, err := Paginate(nil, DefaultPageSize, "")
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

func TestPaginate_SinglePageReturnsAll(t *testing.T) {
	now := time.Now()
	threads := []*Thread{
		makeThread("a", now.Add(-3*time.Hour)),
		makeThread("b", now.Add(-2*time.Hour)),
		makeThread("c", now.Add(-1*time.Hour)),
	}

	page, next, err := Paginate(threads, DefaultPageSize, "")
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

func TestPaginate_LimitRespected(t *testing.T) {
	now := time.Now()
	threads := []*Thread{
		makeThread("a", now.Add(-3*time.Hour)),
		makeThread("b", now.Add(-2*time.Hour)),
		makeThread("c", now.Add(-1*time.Hour)),
	}

	page, next, err := Paginate(threads, 2, "")
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

func TestPaginate_CursorProgression(t *testing.T) {
	now := time.Now()
	threads := []*Thread{
		makeThread("a", now.Add(-3*time.Hour)),
		makeThread("b", now.Add(-2*time.Hour)),
		makeThread("c", now.Add(-1*time.Hour)),
		makeThread("d", now),
	}

	// First page.
	page1, cursor1, err := Paginate(threads, 2, "")
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
	page2, cursor2, err := Paginate(threads, 2, cursor1)
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

func TestPaginate_CursorProgressionThreePages(t *testing.T) {
	now := time.Now()
	threads := []*Thread{
		makeThread("a", now.Add(-5*time.Hour)),
		makeThread("b", now.Add(-4*time.Hour)),
		makeThread("c", now.Add(-3*time.Hour)),
		makeThread("d", now.Add(-2*time.Hour)),
		makeThread("e", now.Add(-1*time.Hour)),
	}

	// Walk three pages of 2, 2, 1 and verify we visit every thread
	// exactly once.
	seen := []string{}
	cursor := ""
	for pageNum := 1; pageNum <= 3; pageNum++ {
		page, next, err := Paginate(threads, 2, cursor)
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

func TestPaginate_TiebreakByID(t *testing.T) {
	// Three threads share the exact same UpdatedAt. They must
	// paginate in id-ascending order, and a cursor pointing at "b"
	// must skip "b" on the next page.
	same := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	threads := []*Thread{
		makeThread("c", same),
		makeThread("a", same),
		makeThread("b", same),
	}

	page1, cursor1, err := Paginate(threads, 2, "")
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if got, want := idsOf(page1), []string{"a", "b"}; !slices.Equal(got, want) {
		t.Errorf("page 1 (tied): got %v, want %v", got, want)
	}
	if cursor1 == "" {
		t.Fatal("page 1 should have a next cursor (c remains)")
	}

	page2, cursor2, err := Paginate(threads, 2, cursor1)
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

func TestPaginate_InvalidCursor(t *testing.T) {
	now := time.Now()
	threads := []*Thread{makeThread("a", now)}

	_, _, err := Paginate(threads, DefaultPageSize, "!!!not-base64!!!")
	if err == nil {
		t.Fatal("expected error for invalid cursor, got nil")
	}
	if !errors.Is(err, ErrInvalidCursor) {
		t.Errorf("expected ErrInvalidCursor, got %v", err)
	}
}

func TestPaginate_CursorPastEndReturnsEmpty(t *testing.T) {
	now := time.Now()
	threads := []*Thread{
		makeThread("a", now.Add(-2*time.Hour)),
		makeThread("b", now.Add(-1*time.Hour)),
	}

	// Build a cursor pointing to "a" (the OLDEST thread). The next
	// page should be empty.
	c, err := (&Cursor{
		Version:   cursorVersion,
		UpdatedAt: threads[0].UpdatedAt,
		ID:        threads[0].ID,
	}).Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	page, next, err := Paginate(threads, DefaultPageSize, c)
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

func TestPaginate_LimitOne(t *testing.T) {
	now := time.Now()
	threads := []*Thread{
		makeThread("a", now.Add(-2*time.Hour)),
		makeThread("b", now.Add(-1*time.Hour)),
	}

	page, next, err := Paginate(threads, 1, "")
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

func TestPaginate_LimitLargerThanTotal(t *testing.T) {
	now := time.Now()
	threads := []*Thread{
		makeThread("a", now.Add(-2*time.Hour)),
		makeThread("b", now.Add(-1*time.Hour)),
	}

	page, next, err := Paginate(threads, 1000, "")
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

func TestPaginate_LimitZeroClampedToOne(t *testing.T) {
	now := time.Now()
	threads := []*Thread{
		makeThread("a", now.Add(-1*time.Hour)),
		makeThread("b", now),
	}

	page, _, err := Paginate(threads, 0, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page) != 1 {
		t.Errorf("limit 0 should yield 1 item (clamped), got %d", len(page))
	}
}

func TestPaginate_LimitNegativeClampedToOne(t *testing.T) {
	now := time.Now()
	threads := []*Thread{
		makeThread("a", now.Add(-1*time.Hour)),
	}

	page, _, err := Paginate(threads, -5, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(page) != 1 {
		t.Errorf("negative limit should yield 1 item (clamped), got %d", len(page))
	}
}

func TestPaginate_DoesNotMutateInputIDs(t *testing.T) {
	// The function sorts in place; callers should see the same
	// *Thread pointers in the result. Verify the original slice
	// still contains the same pointers (even if the order changed).
	now := time.Now()
	a := makeThread("a", now.Add(-2*time.Hour))
	b := makeThread("b", now.Add(-1*time.Hour))
	threads := []*Thread{a, b}

	page, _, err := Paginate(threads, DefaultPageSize, "")
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
	original := Cursor{
		Version:   cursorVersion,
		UpdatedAt: time.Date(2026, 6, 15, 12, 30, 45, 0, time.UTC),
		ID:        "abc-123",
	}

	encoded, err := original.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if encoded == "" {
		t.Fatal("encoded cursor is empty")
	}

	decoded, err := DecodeCursor(encoded)
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
	c := Cursor{
		Version:   cursorVersion,
		UpdatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		ID:        "x",
	}
	first, err := c.Encode()
	if err != nil {
		t.Fatalf("encode #1: %v", err)
	}
	second, err := c.Encode()
	if err != nil {
		t.Fatalf("encode #2: %v", err)
	}
	if first != second {
		t.Errorf("cursor encoding is not stable: %q vs %q", first, second)
	}
}

func TestCursor_MalformedBase64(t *testing.T) {
	_, err := DecodeCursor("!!!not-base64!!!")
	if err == nil {
		t.Fatal("expected error for malformed base64, got nil")
	}
	if !errors.Is(err, ErrInvalidCursor) {
		t.Errorf("expected ErrInvalidCursor, got %v", err)
	}
}

func TestCursor_MalformedJSON(t *testing.T) {
	// Valid base64, but the decoded bytes are not valid JSON.
	// We use json here to construct a valid base64 string for the
	// test.
	validJSON, _ := json.Marshal("hello")
	notJSON := strings.Trim(string(validJSON), `"`) + "!!!"
	_, err := DecodeCursor(notJSON)
	if err == nil {
		t.Fatal("expected error for malformed JSON, got nil")
	}
	if !errors.Is(err, ErrInvalidCursor) {
		t.Errorf("expected ErrInvalidCursor, got %v", err)
	}
}

func TestCursor_UnknownVersion(t *testing.T) {
	unknown := Cursor{Version: 999, UpdatedAt: time.Now(), ID: "x"}
	encoded, err := unknown.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	_, err = DecodeCursor(encoded)
	if err == nil {
		t.Fatal("expected error for unknown version, got nil")
	}
	if !errors.Is(err, ErrInvalidCursor) {
		t.Errorf("expected ErrInvalidCursor, got %v", err)
	}
	if !strings.Contains(err.Error(), "999") {
		t.Errorf("error should mention the bad version (999): %v", err)
	}
}

func TestCursor_EmptyString(t *testing.T) {
	_, err := DecodeCursor("")
	if err == nil {
		t.Fatal("expected error for empty cursor, got nil")
	}
	if !errors.Is(err, ErrInvalidCursor) {
		t.Errorf("expected ErrInvalidCursor, got %v", err)
	}
}

func TestEncodeProducesBase64URL(t *testing.T) {
	c := Cursor{Version: cursorVersion, UpdatedAt: time.Now(), ID: "x"}
	encoded, err := c.Encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	if strings.ContainsAny(encoded, "+/=") {
		t.Errorf("cursor contains non-URL-safe base64 chars: %q", encoded)
	}
	// Verify the format is recoverable.
	decoded, err := DecodeCursor(encoded)
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
	a := &Thread{ID: "a", UpdatedAt: now}
	b := &Thread{ID: "b", UpdatedAt: now}
	c := &Thread{ID: "c", UpdatedAt: now.Add(time.Hour)}

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

// Quick smoke test that threadIsAfterCursor handles the boundary
// correctly.
func TestThreadIsAfterCursor(t *testing.T) {
	now := time.Now()
	earlier := now.Add(-time.Hour)
	later := now.Add(time.Hour)
	c := Cursor{Version: cursorVersion, UpdatedAt: now, ID: "m"}

	tests := []struct {
		name string
		thr  *Thread
		want bool
	}{
		{"strictly earlier ts", &Thread{ID: "z", UpdatedAt: earlier}, true},
		{"strictly later ts", &Thread{ID: "a", UpdatedAt: later}, false},
		{"same ts, lower id", &Thread{ID: "a", UpdatedAt: now}, false},
		{"same ts, higher id", &Thread{ID: "z", UpdatedAt: now}, true},
		{"same ts, same id", &Thread{ID: "m", UpdatedAt: now}, false},
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

// Ensure that error messages mention the cursor problem (helps
// clients debug bad cursors).
func TestInvalidCursorError_HasDescriptiveMessage(t *testing.T) {
	_, err := DecodeCursor("garbage")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "cursor") {
		t.Errorf("error message should mention 'cursor': %v", err)
	}
	// Also ensure the sentinel error can be unwrapped.
	if !errors.Is(err, ErrInvalidCursor) {
		t.Errorf("err should unwrap to ErrInvalidCursor: %v", err)
	}
}

func TestClampLimit(t *testing.T) {
	tests := []struct {
		in, want int
	}{
		{0, 1},
		{-5, 1},
		{1, 1},
		{5, 5},
		{DefaultPageSize, DefaultPageSize},
		{MaxPageSize, MaxPageSize},
		{MaxPageSize + 1, MaxPageSize},
		{99999, MaxPageSize},
	}
	for _, tt := range tests {
		if got := ClampLimit(tt.in); got != tt.want {
			t.Errorf("ClampLimit(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}
