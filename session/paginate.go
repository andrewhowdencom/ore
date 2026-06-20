// Package session provides thread persistence and the paginate helper.
//
// The paginate helper is HTTP-agnostic: it operates on slices of
// *Thread. The HTTP conduit, the workshop CLI, and any other consumer
// of the threads listing can share one implementation.
//
// The API is small:
//
//	const DefaultPageSize = 20
//	const MaxPageSize = 100
//	var ErrInvalidCursor
//	type Cursor struct { ... }
//	func (c *Cursor) Encode() (string, error)
//	func DecodeCursor(s string) (Cursor, error)
//	func Paginate(threads []*Thread, limit int, cursor string) (page []*Thread, nextCursor string, err error)
//	func ClampLimit(n int) int
//
// The wire format for cursors is opaque base64-encoded JSON with a
// version field for forward compatibility. Clients MUST treat the
// returned cursor as a string.
//
// Sort order is updated_at descending, with id ascending as a
// deterministic tiebreaker. The tiebreaker is required because two
// threads can share a timestamp; without it, pagination across
// identical timestamps is unstable.
package session

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Thread listing pagination parameters.
const (
	// DefaultPageSize is the page size used when the caller does not
	// specify a limit, or supplies a non-positive limit that should
	// fall back to the default.
	DefaultPageSize = 20

	// MaxPageSize is the upper bound on a single page. Callers that
	// request a larger limit are silently clamped to this value.
	MaxPageSize = 100
)

// ErrInvalidCursor is the sentinel returned by DecodeCursor and Paginate
// when a cursor string cannot be parsed, refers to an unknown version,
// or is empty. Callers should test with errors.Is.
var ErrInvalidCursor = errors.New("invalid pagination cursor")

// cursorVersion is the current wire format version. It is bumped when
// the cursor encoding changes in a way that is not backward-compatible.
const cursorVersion = 1

// Cursor is the opaque pagination cursor format. The Version field
// allows the encoding to evolve without breaking existing clients. The
// cursor identifies the LAST item of the previous page; the next page
// returns items that sort strictly after this position in
// (updated_at desc, id asc) order.
type Cursor struct {
	Version   int       `json:"v"`
	UpdatedAt time.Time `json:"u"`
	ID        string    `json:"i"`
}

// Encode returns the opaque base64-encoded JSON form of the cursor.
// The output uses URL-safe base64 with no padding, so it survives in
// query strings and shell pipelines without further escaping.
func (c *Cursor) Encode() (string, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("marshal cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(data), nil
}

// DecodeCursor parses a base64-encoded JSON cursor string. Returns an
// error wrapping ErrInvalidCursor for any parse failure, unknown
// version, or empty input. The error is suitable for translation to
// HTTP 400 by API callers.
func DecodeCursor(s string) (Cursor, error) {
	if s == "" {
		return Cursor{}, fmt.Errorf("%w: empty cursor", ErrInvalidCursor)
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return Cursor{}, fmt.Errorf("%w: %v", ErrInvalidCursor, err)
	}
	var c Cursor
	if err := json.Unmarshal(raw, &c); err != nil {
		return Cursor{}, fmt.Errorf("%w: %v", ErrInvalidCursor, err)
	}
	if c.Version != cursorVersion {
		return Cursor{}, fmt.Errorf("%w: unsupported version %d", ErrInvalidCursor, c.Version)
	}
	return c, nil
}

// Paginate sorts the given slice of threads by (updated_at desc, id asc)
// and returns a single page of at most limit items, starting strictly
// after the position identified by cursor. An empty cursor means "start
// from the beginning". Returns an error wrapping ErrInvalidCursor if
// the cursor cannot be decoded.
//
// The input slice is sorted in place; the returned page is a sub-slice
// of the input (i.e. the *Thread pointers are not copied). The
// nextCursor return is empty when the returned page is the last page.
//
// limit is clamped: n < 1 is treated as 1, n > MaxPageSize is treated
// as MaxPageSize. Use ClampLimit directly when you need the same
// semantics for a parsed string value.
func Paginate(threads []*Thread, limit int, cursor string) (page []*Thread, nextCursor string, err error) {
	if limit < 1 {
		limit = 1
	}
	if limit > MaxPageSize {
		limit = MaxPageSize
	}

	slices.SortFunc(threads, compareThreads)

	start := 0
	if cursor != "" {
		c, err := DecodeCursor(cursor)
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
		next, encErr := (&Cursor{
			Version:   cursorVersion,
			UpdatedAt: threads[end-1].UpdatedAt,
			ID:        threads[end-1].ID,
		}).Encode()
		if encErr != nil {
			return nil, "", encErr
		}
		nextCursor = next
	}

	return page, nextCursor, nil
}

// ClampLimit returns n clamped to the [1, MaxPageSize] range. Values
// less than 1 are clamped to 1; values greater than MaxPageSize are
// clamped to MaxPageSize. Callers that need a fallback to the default
// page size for non-positive input should branch on the input value
// before calling this function (or use DefaultPageSize explicitly).
func ClampLimit(n int) int {
	if n < 1 {
		return 1
	}
	if n > MaxPageSize {
		return MaxPageSize
	}
	return n
}

// compareThreads orders threads by (updated_at desc, id asc). The
// tiebreaker on id is required for deterministic pagination across
// threads that share a timestamp.
func compareThreads(a, b *Thread) int {
	if a.UpdatedAt.Equal(b.UpdatedAt) {
		return strings.Compare(a.ID, b.ID)
	}
	if a.UpdatedAt.After(b.UpdatedAt) {
		return -1 // a comes first (later updated_at)
	}
	return 1
}

// threadIsAfterCursor reports whether t sorts strictly after the
// cursor position in (updated_at desc, id asc) order. Items equal to
// the cursor are NOT considered "after"; the cursor is exclusive.
func threadIsAfterCursor(t *Thread, c Cursor) bool {
	if t.UpdatedAt.Before(c.UpdatedAt) {
		return true
	}
	if t.UpdatedAt.Equal(c.UpdatedAt) && t.ID > c.ID {
		return true
	}
	return false
}
