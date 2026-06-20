package http

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/andrewhowdencom/ore/session"
)

// threadSummaryJSON is the JSON representation of a Thread on the
// listing. The Preview field carries the first user turn's text
// excerpt, truncated, so the landing page's "Load more" JS can render
// the same card shape as the server-rendered first page.
type threadSummaryJSON struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Preview   string    `json:"preview,omitempty"`
}

// threadsListResponseJSON is the envelope for the GET /threads
// response. MarshalJSON ensures the threads array is always serialized
// as a JSON array (never null), even when empty.
type threadsListResponseJSON struct {
	Threads    []threadSummaryJSON `json:"threads"`
	NextCursor string              `json:"next_cursor,omitempty"`
}

// MarshalJSON implements json.Marshaler, normalising the empty
// threads slice to an empty array.
func (r threadsListResponseJSON) MarshalJSON() ([]byte, error) {
	type alias threadsListResponseJSON
	if r.Threads == nil {
		r.Threads = []threadSummaryJSON{}
	}
	return json.Marshal(alias(r))
}

// parseLimit parses the ?limit= query parameter and returns a value
// clamped to [1, session.MaxPageSize]. An empty or unparseable string
// returns session.DefaultPageSize. The handler treats all values the
// same; out-of-range values are silently clamped.
func parseLimit(s string) int {
	if s == "" {
		return session.DefaultPageSize
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return session.DefaultPageSize
	}
	return session.ClampLimit(n)
}

// summariesFrom converts a slice of session.Thread pointers into the
// JSON-ready summary form used by the listing response. Each summary
// includes a 120-character preview excerpt from the first user turn.
func summariesFrom(threads []*session.Thread) []threadSummaryJSON {
	out := make([]threadSummaryJSON, 0, len(threads))
	for _, t := range threads {
		out = append(out, threadSummaryJSON{
			ID:        t.ID,
			CreatedAt: t.CreatedAt,
			UpdatedAt: t.UpdatedAt,
			Preview:   previewSnippet(t, 120),
		})
	}
	return out
}