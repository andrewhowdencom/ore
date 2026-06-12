package truncate

import (
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
)

func TestRenderHint_EmptyTemplate(t *testing.T) {
	t.Parallel()

	if got := RenderHint("", artifact.Truncation{}); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestRenderHint_NoPlaceholders(t *testing.T) {
	t.Parallel()

	tmpl := "Use offset=N to continue."
	got := RenderHint(tmpl, artifact.Truncation{})
	if got != tmpl {
		t.Errorf("got %q, want %q", got, tmpl)
	}
}

func TestRenderHint_KnownPlaceholders(t *testing.T) {
	t.Parallel()

	meta := artifact.Truncation{
		OriginalBytes: 1000,
		OriginalLines: 100,
		ShownBytes:    50,
		ShownLines:    5,
		Style:         "tail",
	}

	tmpl := "Truncated {shown_lines}/{original_lines} lines. Style: {style}. Use offset={next_offset}."
	got := RenderHint(tmpl, meta)

	want := "Truncated 5/100 lines. Style: tail. Use offset=6."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderHint_UnknownPlaceholderLeftAsIs(t *testing.T) {
	t.Parallel()

	meta := artifact.Truncation{}
	tmpl := "Path: {path}. Unknown: {foo}."
	got := RenderHint(tmpl, meta)

	want := "Path: {path}. Unknown: {foo}."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderHint_ExtrasOverride(t *testing.T) {
	t.Parallel()

	meta := artifact.Truncation{}
	extras := map[string]string{"path": "/tmp/output.log", "offset": "100"}
	got := RenderHint("Full output at {path}. Use offset={offset}.", meta, extras)

	want := "Full output at /tmp/output.log. Use offset=100."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderHint_MultipleOccurrences(t *testing.T) {
	t.Parallel()

	meta := artifact.Truncation{OriginalBytes: 100}
	got := RenderHint("{original_bytes} bytes, {original_bytes} total.", meta)

	want := "100 bytes, 100 total."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestRenderHint_ExtrasAddNewKeys(t *testing.T) {
	t.Parallel()

	meta := artifact.Truncation{}
	extras := map[string]string{"limit": "2000", "next_offset": "500"}
	got := RenderHint("Use limit={limit} or offset={next_offset}.", meta, extras)

	want := "Use limit=2000 or offset=500."
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
