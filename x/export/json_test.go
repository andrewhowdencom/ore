package export

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/junk"
	"github.com/andrewhowdencom/ore/ledger"
)

func TestJSON(t *testing.T) {
	buf := ledger.NewThread()
	buf.Append(ledger.RoleUser, artifact.Text{Content: "Hello!"})
	thread := &junk.Thread{
		ID:        "thread-json-1",
		State:     buf,
		Metadata:  map[string]string{"foo": "bar"},
	}

	var w bytes.Buffer
	if err := JSON(&w, thread); err != nil {
		t.Fatalf("JSON() error = %v", err)
	}

	got := w.String()

	// Should be valid JSON.
	var check map[string]any
	if err := json.Unmarshal([]byte(got), &check); err != nil {
		t.Fatalf("JSON() output is not valid JSON: %v\noutput:\n%s", err, got)
	}

	// Should be indented (pretty-printed).
	if !strings.Contains(got, "\n") {
		t.Error("JSON() output is not indented")
	}

	// Should contain known fields.
	wantSubstrs := []string{
		`"id": "thread-json-1"`,
		`"created_at": "2024-01-01T00:00:00Z"`,
		`"foo": "bar"`,
		`"role": "user"`,
		`"Hello!"`,
	}
	for _, want := range wantSubstrs {
		if !strings.Contains(got, want) {
			t.Errorf("JSON() output missing substring %q\ngot:\n%s", want, got)
		}
	}
}
