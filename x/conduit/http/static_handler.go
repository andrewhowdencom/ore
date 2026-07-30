package http

import (
	"html/template"
	stdhttp "net/http"
	"time"
)

// serveUI serves the embedded static files (index.html and chat.js)
// for the web chat client. It reads the requested file from staticFS
// and returns 404 for unknown paths. It is registered at GET /chat
// and GET /chat.js when WithUI() is enabled.
func (h *Handler) serveUI(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	switch r.URL.Path {
	case "/chat":
		data, err := staticFS.ReadFile("static/index.html")
		if err != nil {
			writeJSONError(w, stdhttp.StatusInternalServerError, err.Error())
			return
		}
		tmpl, err := template.New("index").Parse(string(data))
		if err != nil {
			writeJSONError(w, stdhttp.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = tmpl.Execute(w, struct{ Name string }{Name: h.name})
	case "/chat.js":
		data, err := staticFS.ReadFile("static/chat.js")
		if err != nil {
			writeJSONError(w, stdhttp.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		_, _ = w.Write(data)
	default:
		writeJSONError(w, stdhttp.StatusNotFound, "not found")
	}
}

// serveLanding renders the thread-list landing page when the UI is
// enabled. The thread list is sourced from the Backend.
func (h *Handler) serveLanding(w stdhttp.ResponseWriter, r *stdhttp.Request) {
	if r.URL.Path != "/" {
		writeJSONError(w, stdhttp.StatusNotFound, "not found")
		return
	}

	threads, err := h.backend.ListThreads(r.Context())
	if err != nil {
		writeJSONError(w, stdhttp.StatusInternalServerError, err.Error())
		return
	}

	page, next, err := paginateAndSortThreads(threads, defaultThreadPageSize, "")
	if err != nil {
		writeJSONError(w, stdhttp.StatusInternalServerError, err.Error())
		return
	}

	type landingItem struct {
		ID      string
		Preview string
		LastAt  string
	}

	items := make([]landingItem, len(page))
	for i, t := range page {
		lastAt := ""
		if !t.LastAt.IsZero() {
			lastAt = t.LastAt.UTC().Format(time.RFC3339)
		}
		items[i] = landingItem{
			ID:      t.ID,
			Preview: t.Preview,
			LastAt:  lastAt,
		}
	}

	data, err := staticFS.ReadFile("static/landing.html")
	if err != nil {
		writeJSONError(w, stdhttp.StatusInternalServerError, err.Error())
		return
	}

	tmpl, err := template.New("landing").Parse(string(data))
	if err != nil {
		writeJSONError(w, stdhttp.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = tmpl.Execute(w, struct {
		Name       string
		Threads    []landingItem
		NextCursor string
	}{Name: h.name, Threads: items, NextCursor: next})
}