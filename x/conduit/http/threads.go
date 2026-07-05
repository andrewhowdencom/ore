package http

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/andrewhowdencom/ore/junk"
)

// Thread listing pagination parameters.
const (
	defaultThreadPageSize = 20
	maxThreadPageSize     = 100
)

// errInvalidCursor is returned when a pagination cursor cannot be decoded.
// The handler translates it to 400 Bad Request.
var errInvalidCursor = errors.New("invalid pagination cursor")

// threadSummaryJSON is the JSON representation of a Thread on the listing.
// The Preview field carries the first user turn's text excerpt, truncated,
// so the landing page's "Load more" JS can render the same card shape
// as the server-rendered first page.
//
// CreatedAt and UpdatedAt have been removed from the wire format. The
// conversation's temporal data lives in the turn history. The last
// turn's timestamp is used as the cursor sort key (see threadCursor).
type threadSummaryJSON struct {
	ID      string `json:"id"`
	Preview string `json:"preview,omitempty"`
	LastAt  string `json:"last_at,omitempty"`
}

// lastActivity returns the timestamp of the most recent turn in the
// thread. Empty threads return the zero time. The returned time is
// the conversation's "last activity" — used as the sort key for the
// thread listing.
func lastActivity(t *junk.Thread) time.Time {
	turns := t.State.AllTurns()
	if len(turns) == 0 {
		return time.Time{}
	}
	return turns[len(turns)-1].Timestamp
}

// threadsListResponseJSON is the envelope for the GET /threads response.
// MarshalJSON ensures the threads array is always serialized as a JSON
// array (never null), even when empty.
type threadsListResponseJSON struct {
	Threads    []threadSummaryJSON `json:"threads"`
	NextCursor string              `json:"next_cursor,omitempty"`
}

// MarshalJSON implements json.Marshaler, normalising the empty threads
// slice to an empty array.
func (r threadsListResponseJSON) MarshalJSON() ([]byte, error) {
	type alias threadsListResponseJSON
	if r.Threads == nil {
		r.Threads = []threadSummaryJSON{}
	}
	return json.Marshal(alias(r))
}

// threadCursor is the opaque pagination cursor format. The Version field
// allows the encoding to evolve without breaking existing clients. The
// cursor identifies the LAST item of the previous page; the next page
// returns items that sort strictly after this position in (last_at desc,
// id asc) order.
//
// The LastAt timestamp is derived from the most recent turn's timestamp
// (or the zero time for empty threads). Empty threads sort last
// regardless of their ID.
type threadCursor struct {
	Version int       `json:"v"`
	LastAt  time.Time `json:"l"`
	ID      string    `json:"i"`
}

const threadCursorVersion = 1

// encode returns the opaque base64-encoded JSON form of the cursor.
func (c threadCursor) encode() (string, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("marshal cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

// decodeThreadCursor parses a base64-encoded JSON cursor string. Returns
// errInvalidCursor (possibly wrapped) for any parse failure, unknown
// version, or empty input.
func decodeThreadCursor(s string) (threadCursor, error) {
	if s == "" {
		return threadCursor{}, fmt.Errorf("%w: empty cursor", errInvalidCursor)
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return threadCursor{}, fmt.Errorf("%w: %v", errInvalidCursor, err)
	}
	var c threadCursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return threadCursor{}, fmt.Errorf("%w: %v", errInvalidCursor, err)
	}
	if c.Version != threadCursorVersion {
		return threadCursor{}, fmt.Errorf("%w: unsupported version %d", errInvalidCursor, c.Version)
	}
	return c, nil
}

// paginateAndSortThreads sorts the given slice of threads by (last
// activity desc, id asc) and returns a single page of at most limit
// items, starting strictly after the position identified by cursor.
// An empty cursor means "start from the beginning". Empty threads sort
// last regardless of their ID. Returns errInvalidCursor if the cursor
// cannot be decoded. The input slice is sorted in place; the returned
// page is a sub-slice of the input.
func paginateAndSortThreads(threads []*junk.Thread, limit int, cursor string) (page []*junk.Thread, nextCursor string, err error) {
	if limit < 1 {
		limit = 1
	}

	slices.SortFunc(threads, compareThreads)

	start := 0
	if cursor != "" {
		c, err := decodeThreadCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		start = len(threads) // default: no items after cursor
		for i, t := range threads {
			if threadIsAfterCursor(t, c) {
				start = i
				break
			}
		}
	}

	end := start + limit
	if end > len(threads) {
		end = len(threads)
	}

	page = threads[start:end]

	if end < len(threads) {
		last := threads[end-1]
		next, encErr := (threadCursor{
			Version: threadCursorVersion,
			LastAt:  lastActivity(last),
			ID:      last.ID,
		}).encode()
		if encErr != nil {
			return nil, "", encErr
		}
		nextCursor = next
	}

	return page, nextCursor, nil
}

// compareThreads orders threads by (last activity desc, id asc). The
// tiebreaker on id is required for deterministic pagination across
// threads that share a timestamp. Empty threads (zero last activity)
// sort last.
func compareThreads(a, b *junk.Thread) int {
	aAt := lastActivity(a)
	bAt := lastActivity(b)
	if aAt.Equal(bAt) {
		return strings.Compare(a.ID, b.ID)
	}
	if aAt.IsZero() {
		return 1 // a is empty; b first
	}
	if bAt.IsZero() {
		return -1 // b is empty; a first
	}
	if aAt.After(bAt) {
		return -1 // a comes first (later activity)
	}
	return 1
}

// threadIsAfterCursor reports whether t sorts strictly after the cursor
// position in (last activity desc, id asc) order. Items equal to the
// cursor are NOT considered "after"; the cursor is exclusive.
func threadIsAfterCursor(t *junk.Thread, c threadCursor) bool {
	tAt := lastActivity(t)
	if tAt.IsZero() {
		// Empty threads never sort after a real cursor.
		return false
	}
	if c.LastAt.IsZero() {
		// Anything with activity sorts after an empty cursor.
		return true
	}
	if tAt.Before(c.LastAt) {
		return true
	}
	if tAt.Equal(c.LastAt) && t.ID > c.ID {
		return true
	}
	return false
}

// parseLimit parses the ?limit= query parameter and returns a value
// clamped to [1, maxThreadPageSize]. An empty or unparseable string
// returns defaultThreadPageSize. The handler treats all values the same;
// out-of-range values are silently clamped.
func parseLimit(s string) int {
	if s == "" {
		return defaultThreadPageSize
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return defaultThreadPageSize
	}
	if n < 1 {
		return 1
	}
	if n > maxThreadPageSize {
		return maxThreadPageSize
	}
	return n
}

// summariesFrom converts a slice of junk.Thread pointers into the
// JSON-ready summary form used by the listing response. Each summary
// includes a 120-character preview excerpt from the first user turn.
func summariesFrom(threads []*junk.Thread) []threadSummaryJSON {
	out := make([]threadSummaryJSON, 0, len(threads))
	for _, t := range threads {
		at := lastActivity(t)
		lastAt := ""
		if !at.IsZero() {
			lastAt = at.UTC().Format(time.RFC3339)
		}
		out = append(out, threadSummaryJSON{
			ID:      t.ID,
			Preview: previewSnippet(t, 120),
			LastAt:  lastAt,
		})
	}
	return out
}