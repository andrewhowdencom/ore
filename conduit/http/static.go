package http

import (
	"embed"
	"io/fs"
)

// static.go embeds the UI assets for the HTTP conduit.

//go:embed static/*
var staticEmbedFS embed.FS

// staticFS holds the embedded static files (HTML/JS) used by the optional web UI.
// It is declared as fs.ReadFileFS so tests can substitute a mock filesystem.
var staticFS fs.ReadFileFS = staticEmbedFS
