module github.com/andrewhowdencom/ore/examples

go 1.26.2

require (
	github.com/andrewhowdencom/ore v1.2.0
	github.com/andrewhowdencom/ore/x/analytics v0.2.5
	github.com/andrewhowdencom/ore/x/compaction v0.5.1
	github.com/andrewhowdencom/ore/x/conduit/http v0.8.1
	github.com/andrewhowdencom/ore/x/conduit/tui v0.12.7
	github.com/andrewhowdencom/ore/x/provider/openai v0.6.4
	github.com/andrewhowdencom/ore/x/telemetry v0.1.4
	github.com/andrewhowdencom/ore/x/tool v0.6.0
	github.com/andrewhowdencom/ore/x/tool/calculator v0.4.1
	github.com/andrewhowdencom/ore/x/tool/filesystem v0.5.2
	github.com/andrewhowdencom/ore/x/tool/set_model v0.1.5
	github.com/andrewhowdencom/ore/x/tool/set_title v0.3.2
	github.com/andrewhowdencom/ore/x/usage v0.2.2
	go.opentelemetry.io/otel v1.44.0
	go.opentelemetry.io/otel/sdk v1.44.0
	go.opentelemetry.io/otel/sdk/metric v1.44.0
	go.opentelemetry.io/otel/trace v1.44.0
)

require (
	charm.land/bubbles/v2 v2.1.0 // indirect
	charm.land/bubbletea/v2 v2.0.6 // indirect
	charm.land/lipgloss/v2 v2.0.3 // indirect
	github.com/alecthomas/chroma/v2 v2.20.0 // indirect
	github.com/andrewhowdencom/ore/x/conduit v0.1.5 // indirect
	github.com/andrewhowdencom/ore/x/llmbytes v0.1.2 // indirect
	github.com/andrewhowdencom/ore/x/provider/retry v0.0.3 // indirect
	github.com/andrewhowdencom/ore/x/tool/truncate v0.1.1 // indirect
	github.com/andrewhowdencom/ore/x/wire/openai v0.5.0 // indirect
	github.com/atotto/clipboard v0.1.4 // indirect
	github.com/aymanbagabas/go-osc52/v2 v2.0.1 // indirect
	github.com/aymerick/douceur v0.2.0 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/charmbracelet/colorprofile v0.4.3 // indirect
	github.com/charmbracelet/glamour v1.0.0 // indirect
	github.com/charmbracelet/lipgloss v1.1.1-0.20250404203927-76690c660834 // indirect
	github.com/charmbracelet/ultraviolet v0.0.0-20260416155717-489999b90468 // indirect
	github.com/charmbracelet/x/ansi v0.11.7 // indirect
	github.com/charmbracelet/x/cellbuf v0.0.15 // indirect
	github.com/charmbracelet/x/exp/slice v0.0.0-20250327172914-2fdc97757edf // indirect
	github.com/charmbracelet/x/term v0.2.2 // indirect
	github.com/charmbracelet/x/termios v0.1.1 // indirect
	github.com/charmbracelet/x/windows v0.2.2 // indirect
	github.com/clipperhouse/displaywidth v0.11.0 // indirect
	github.com/clipperhouse/uax29/v2 v2.7.0 // indirect
	github.com/dlclark/regexp2 v1.11.5 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/css v1.0.1 // indirect
	github.com/lucasb-eyer/go-colorful v1.4.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-runewidth v0.0.23 // indirect
	github.com/microcosm-cc/bluemonday v1.0.27 // indirect
	github.com/muesli/cancelreader v0.2.2 // indirect
	github.com/muesli/reflow v0.3.0 // indirect
	github.com/muesli/termenv v0.16.0 // indirect
	github.com/openai/openai-go v1.12.0 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	github.com/tidwall/gjson v1.18.0 // indirect
	github.com/tidwall/match v1.1.1 // indirect
	github.com/tidwall/pretty v1.2.1 // indirect
	github.com/tidwall/sjson v1.2.5 // indirect
	github.com/xo/terminfo v0.0.0-20220910002029-abceb7e1c41e // indirect
	github.com/yuin/goldmark v1.7.13 // indirect
	github.com/yuin/goldmark-emoji v1.0.6 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/httptrace/otelhttptrace v0.69.0 // indirect
	go.opentelemetry.io/otel/metric v1.44.0 // indirect
	golang.org/x/net v0.53.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
	golang.org/x/term v0.42.0 // indirect
	golang.org/x/text v0.36.0 // indirect
)

replace (
	github.com/andrewhowdencom/ore/x/conduit => ../x/conduit
	github.com/andrewhowdencom/ore/x/llmbytes => ../x/llmbytes
)

replace github.com/andrewhowdencom/ore/x/tool => ../x/tool

replace github.com/andrewhowdencom/ore/x/tool/filesystem => ../x/tool/filesystem

replace github.com/andrewhowdencom/ore/x/tool/truncate => ../x/tool/truncate

replace github.com/andrewhowdencom/ore/x/verifier => ../x/verifier

replace github.com/andrewhowdencom/ore => ..

replace github.com/andrewhowdencom/ore/x/compaction => ../x/compaction

replace github.com/andrewhowdencom/ore/x/conduit/http => ../x/conduit/http

replace github.com/andrewhowdencom/ore/x/conduit/tui => ../x/conduit/tui

replace github.com/andrewhowdencom/ore/x/provider/openai => ../x/provider/openai

replace github.com/andrewhowdencom/ore/x/telemetry => ../x/telemetry

replace github.com/andrewhowdencom/ore/x/tool/calculator => ../x/tool/calculator

replace github.com/andrewhowdencom/ore/x/tool/set_model => ../x/tool/set_model

replace github.com/andrewhowdencom/ore/x/tool/set_title => ../x/tool/set_title

replace github.com/andrewhowdencom/ore/x/usage => ../x/usage

replace github.com/andrewhowdencom/ore/x/wire/openai => ../x/wire/openai
