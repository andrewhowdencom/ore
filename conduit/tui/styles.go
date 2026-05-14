package tui

import _ "embed"

// These embedded style files are tweaked copies of glamour v1.0.0's
// built-in dark.json and light.json. The only modification is that
// "document.margin" has been changed from 2 to 0 to remove the
// document-level margin padding that wastes vertical viewport space.
// See: https://github.com/charmbracelet/glamour/tree/v1.0.0/styles

//go:embed styles/dark.json
var darkStyle []byte

//go:embed styles/light.json
var lightStyle []byte