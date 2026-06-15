package http

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
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
type threadSummaryJSON struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
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
// returns items that sort strictly after this position in (updated_at desc,
// id asc) order.
type threadCursor struct {
	Version   int       `json:"v"`
	UpdatedAt time.Time `json:"u"`
	ID        string    `json:"i"`
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
