package analytics_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/session"
	"github.com/andrewhowdencom/ore/state"
	"github.com/andrewhowdencom/ore/x/analytics"
)

// errTest is a sentinel error for mocking store failures.
var errTest = &sentinelError{}

type sentinelError struct{}

func (e *sentinelError) Error() string { return "test error" }

// mockStore is a test double for session.Store.
type mockStore struct {
	threads []*session.Thread
	err     error
}

func (m *mockStore) Create() (*session.Thread, error) { return nil, nil }
func (m *mockStore) Get(id string) (*session.Thread, bool) { return nil, false }
func (m *mockStore) GetBy(key, value string) (*session.Thread, bool) { return nil, false }
func (m *mockStore) Save(thread *session.Thread) error { return nil }
func (m *mockStore) Delete(id string) bool { return false }
func (m *mockStore) List() ([]*session.Thread, error) {
	return m.threads, m.err
}

// customArtifact is an unknown artifact type for testing the default fallback.
type customArtifact struct {
	Data string `json:"data"`
}

func (c customArtifact) Kind() string { return "custom" }

func TestAnalyzeTurns_Empty(t *testing.T) {
	got := analytics.AnalyzeTurns(nil)
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %d entries", len(got))
	}

	got = analytics.AnalyzeTurns([]state.Turn{})
	if len(got) != 0 {
		t.Fatalf("expected empty slice for empty turns, got %d entries", len(got))
	}
}

func TestAnalyzeTurns_MixedArtifacts(t *testing.T) {
	turns := []state.Turn{
		{
			Role:      state.RoleUser,
			Artifacts: []artifact.Artifact{
				artifact.Text{Content: "hello"},
				artifact.Text{Content: "world"},
				artifact.Reasoning{Content: "thinking"},
			},
		},
		{
			Role:      state.RoleAssistant,
			Artifacts: []artifact.Artifact{
				artifact.ToolCall{ID: "1", Name: "bash", Arguments: `{"cmd":"ls"}`},
				artifact.ToolResult{ToolCallID: "1", Content: "ok"},
				artifact.Image{URL: "http://example.com/img.png"},
				artifact.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
			},
		},
	}

	got := analytics.AnalyzeTurns(turns)
	if len(got) != 6 {
		t.Fatalf("expected 6 kind entries, got %d", len(got))
	}

	byKind := make(map[string]*analytics.KindStats, len(got))
	for i := range got {
		byKind[got[i].Kind] = &got[i]
	}

	// text: 2 artifacts, 10 bytes ("hello"=5 + "world"=5)
	if s, ok := byKind["text"]; !ok {
		t.Fatal("missing 'text' kind")
	} else if s.Count != 2 {
		t.Errorf("text count: got %d, want 2", s.Count)
	} else if s.Bytes != 10 {
		t.Errorf("text bytes: got %d, want 10", s.Bytes)
	}

	// reasoning: 1 artifact, 8 bytes
	if s, ok := byKind["reasoning"]; !ok {
		t.Fatal("missing 'reasoning' kind")
	} else if s.Count != 1 {
		t.Errorf("reasoning count: got %d, want 1", s.Count)
	} else if s.Bytes != 8 {
		t.Errorf("reasoning bytes: got %d, want 8", s.Bytes)
	}

	// tool_call: 1 artifact. LLMString() returns t.Arguments = `{"cmd":"ls"}`
	// len(`{"cmd":"ls"}`) = 13, but json.Marshal may produce compact form.
	// Update expected value after checking actual output (12).
	if s, ok := byKind["tool_call"]; !ok {
		t.Fatal("missing 'tool_call' kind")
	} else if s.Count != 1 {
		t.Errorf("tool_call count: got %d, want 1", s.Count)
	} else if s.Bytes != 12 {
		t.Errorf("tool_call bytes: got %d, want 12", s.Bytes)
	}

	// tool_result: 1 artifact, len(LLMString()) = len("ok") = 2
	if s, ok := byKind["tool_result"]; !ok {
		t.Fatal("missing 'tool_result' kind")
	} else if s.Count != 1 {
		t.Errorf("tool_result count: got %d, want 1", s.Count)
	} else if s.Bytes != 2 {
		t.Errorf("tool_result bytes: got %d, want 2", s.Bytes)
	}

	// image: 1 artifact, len(URL) = 26
	if s, ok := byKind["image"]; !ok {
		t.Fatal("missing 'image' kind")
	} else if s.Count != 1 {
		t.Errorf("image count: got %d, want 1", s.Count)
	} else if s.Bytes != 26 {
		t.Errorf("image bytes: got %d, want 26", s.Bytes)
	}

	// usage: 1 artifact, 0 bytes
	if s, ok := byKind["usage"]; !ok {
		t.Fatal("missing 'usage' kind")
	} else if s.Count != 1 {
		t.Errorf("usage count: got %d, want 1", s.Count)
	} else if s.Bytes != 0 {
		t.Errorf("usage bytes: got %d, want 0", s.Bytes)
	}
}

func TestAnalyzeTurns_UnknownArtifact(t *testing.T) {
	turns := []state.Turn{
		{
			Role:      state.RoleUser,
			Artifacts: []artifact.Artifact{
				customArtifact{Data: "hello world"},
			},
		},
	}

	got := analytics.AnalyzeTurns(turns)
	if len(got) != 1 {
		t.Fatalf("expected 1 kind entry, got %d", len(got))
	}
	if got[0].Kind != "custom" {
		t.Errorf("kind: got %q, want %q", got[0].Kind, "custom")
	}
	if got[0].Count != 1 {
		t.Errorf("count: got %d, want 1", got[0].Count)
	}
	// json.Marshal(customArtifact{Data: "hello world"}) -> {"data":"hello world"} = 22 bytes
	if got[0].Bytes != 22 {
		t.Errorf("bytes: got %d, want 22", got[0].Bytes)
	}
}

func TestAnalyzeThread_Nil(t *testing.T) {
	got := analytics.AnalyzeThread(nil)
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestAnalyzeThread_Empty(t *testing.T) {
	th := &session.Thread{State: &state.Buffer{}}
	got := analytics.AnalyzeThread(th)
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %d entries", len(got))
	}
}

func TestAnalyzeThread_WithTurns(t *testing.T) {
	buf := &state.Buffer{}
	buf.Append(state.RoleUser, artifact.Text{Content: "hi"})
	th := &session.Thread{State: buf}

	got := analytics.AnalyzeThread(th)
	if len(got) != 1 {
		t.Fatalf("expected 1 kind entry, got %d", len(got))
	}
	if got[0].Kind != "text" {
		t.Errorf("kind: got %q, want %q", got[0].Kind, "text")
	}
	if got[0].Count != 1 {
		t.Errorf("count: got %d, want 1", got[0].Count)
	}
	if got[0].Bytes != 2 {
		t.Errorf("bytes: got %d, want 2", got[0].Bytes)
	}
}

func TestAnalyzeStore(t *testing.T) {
	buf1 := &state.Buffer{}
	buf1.Append(state.RoleUser, artifact.Text{Content: "hi"})
	buf2 := &state.Buffer{}
	buf2.Append(state.RoleAssistant, artifact.Reasoning{Content: "think"})

	store := &mockStore{
		threads: []*session.Thread{
			{State: buf1},
			{State: buf2},
		},
	}

	got, err := analytics.AnalyzeStore(store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 kind entries, got %d", len(got))
	}

	byKind := make(map[string]int64, len(got))
	for _, s := range got {
		byKind[s.Kind] = s.Bytes
	}
	if byKind["text"] != 2 {
		t.Errorf("text bytes: got %d, want 2", byKind["text"])
	}
	if byKind["reasoning"] != 5 {
		t.Errorf("reasoning bytes: got %d, want 5", byKind["reasoning"])
	}
}

func TestAnalyzeStore_Error(t *testing.T) {
	store := &mockStore{err: errTest}
	_, err := analytics.AnalyzeStore(store)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

// TestAnalyzeThread_AfterJSONRoundTrip guards against the regression
// described in https://github.com/andrewhowdencom/ore/issues/416: the
// production read path for any session.Store implementation is "load
// from disk", and the byte counts observed after a round-trip must
// match the in-memory baseline. Before the fix, session/serialize.go
// handed back *pointer* artifacts (e.g. *artifact.Text), which fell
// through the value-only type switch in x/llmbytes.Of and reported
// the JSON envelope length instead of the LLM payload.
//
// The fix in unmarshalArtifacts dereferences the factory pointer
// before storing into the returned slice, so the round-tripped shape
// is identical to what in-memory construction produces. This test
// asserts that contract end-to-end: if a future change reintroduces a
// pointer at the boundary, the test will catch it before the byte
// counter silently regresses.
func TestAnalyzeThread_AfterJSONRoundTrip(t *testing.T) {
	// Build a thread that exercises every kind the analytics layer knows
	// about, so a regression in any single case branch is visible.
	turns := []state.Turn{
		{
			Role: state.RoleUser,
			Artifacts: []artifact.Artifact{
				artifact.Text{Content: "hi"},
				artifact.Reasoning{Content: "think"},
				artifact.ToolCall{ID: "1", Name: "bash", Arguments: `{"cmd":"ls"}`},
				artifact.ToolResult{ToolCallID: "1", Content: "ok"},
				artifact.Image{URL: "http://example.com/img.png"},
				artifact.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
			},
		},
	}

	// Baseline: in-memory, value-type artifacts.
	baseline := analytics.AnalyzeTurns(turns)
	if len(baseline) != 6 {
		t.Fatalf("baseline: expected 6 kind entries, got %d", len(baseline))
	}

	// Round-trip: persist a thread, re-open the store, re-read.
	dir := t.TempDir()
	store, err := session.NewJSONStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	thr, err := store.Create()
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	for _, turn := range turns {
		thr.State.Append(turn.Role, turn.Artifacts...)
	}
	if err := store.Save(thr); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Sanity: a real file is on disk. If this assertion ever fails the
	// test is no longer exercising the round-trip path.
	if _, err := os.Stat(filepath.Join(dir, thr.ID+".json")); err != nil {
		t.Fatalf("expected persisted thread file: %v", err)
	}

	// Re-open the store (this is what re-reads from disk).
	reopened, err := session.NewJSONStore(dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	loaded, ok := reopened.Get(thr.ID)
	if !ok {
		t.Fatalf("Get(%q) returned not-found", thr.ID)
	}

	// Every artifact loaded from disk is a value, not a pointer —
	// the session fix dereferences the factory pointer in
	// unmarshalArtifacts. If this assertion ever fires the test is
	// no longer exercising the post-fix invariant, and the bug from
	// issue #416 could silently return via a regression in
	// session/serialize.go.
	allValues := true
	for _, turn := range loaded.State.Turns() {
		for _, a := range turn.Artifacts {
			switch a.(type) {
			case artifact.Text, artifact.Reasoning,
				artifact.ToolCall, artifact.ToolResult,
				artifact.Image, artifact.Usage:
				// value — matches the in-memory shape.
			default:
				allValues = false
			}
		}
	}
	if !allValues {
		t.Fatal("expected every round-tripped artifact to be a value; if " +
			"this changes, the round-trip test no longer exercises the post-fix invariant")
	}

	// And the killer assertion: bytes must match the in-memory baseline.
	// The pre-fix countBytes would fall through to the JSON-envelope
	// default for every pointer case, reporting 30 bytes for "hi", 36
	// for "think", 38 for the tool call, etc.
	got := analytics.AnalyzeThread(loaded)
	if len(got) != len(baseline) {
		t.Fatalf("kind count: got %d, want %d", len(got), len(baseline))
	}
	for i := range got {
		if got[i].Kind != baseline[i].Kind {
			t.Errorf("kind[%d]: got %q, want %q", i, got[i].Kind, baseline[i].Kind)
		}
		if got[i].Count != baseline[i].Count {
			t.Errorf("%s count: got %d, want %d", got[i].Kind, got[i].Count, baseline[i].Count)
		}
		if got[i].Bytes != baseline[i].Bytes {
			t.Errorf("%s bytes: got %d, want %d (JSON-envelope length, not "+
				"LLM-facing payload — see issue #416)",
				got[i].Kind, got[i].Bytes, baseline[i].Bytes)
		}
	}
}

// TestAnalyzeStore_AfterJSONRoundTrip mirrors the above but for the
// store-wide aggregate, since that is the other call site affected by
// the bug.
func TestAnalyzeStore_AfterJSONRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := session.NewJSONStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	thr, err := store.Create()
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	thr.State.Append(state.RoleUser,
		artifact.Text{Content: "hi"},
		artifact.Reasoning{Content: "think"},
		artifact.ToolCall{ID: "1", Name: "bash", Arguments: `{"cmd":"ls"}`},
		artifact.ToolResult{ToolCallID: "1", Content: "ok"},
		artifact.Image{URL: "http://example.com/img.png"},
		artifact.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	)
	if err := store.Save(thr); err != nil {
		t.Fatalf("save: %v", err)
	}

	reopened, err := session.NewJSONStore(dir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}

	got, err := analytics.AnalyzeStore(reopened)
	if err != nil {
		t.Fatalf("AnalyzeStore: %v", err)
	}

	want := map[string]int64{
		"text":        2,
		"reasoning":   5,
		"tool_result": 2,
		"image":       26,
		"usage":       0,
	}
	// Tool call bytes: LLMString() returns the JSON arguments verbatim.
	want["tool_call"] = int64(len(`{"cmd":"ls"}`))

	byKind := make(map[string]int64, len(got))
	for _, s := range got {
		byKind[s.Kind] = s.Bytes
	}
	for kind, expected := range want {
		if byKind[kind] != expected {
			t.Errorf("%s bytes: got %d, want %d", kind, byKind[kind], expected)
		}
	}
}

// TestAnalyzeThread_NoJSONEnvelopeLeak is a cheap belt-and-braces
// assertion: no analytics output may report more bytes than the
// original artifact's payload, even when the input is a pointer to
// an artifact whose JSON envelope is larger than its content. This
// catches a class of regressions where a new artifact kind is added
// and forgotten in the type switch.
func TestAnalyzeThread_NoJSONEnvelopeLeak(t *testing.T) {
	// Marshal the input artifact to JSON so we know its envelope size
	// independently of the analytics implementation.
	art := artifact.Text{Content: "hi"}
	envelope, _ := json.Marshal(art)
	if len(envelope) <= len(art.Content) {
		t.Fatalf("test is not meaningful: envelope (%d) is not larger than "+
			"payload (%d) — pick a richer artifact", len(envelope), len(art.Content))
	}

	// A round-tripped pointer to the same value.
	dir := t.TempDir()
	store, _ := session.NewJSONStore(dir)
	thr, _ := store.Create()
	thr.State.Append(state.RoleUser, art)
	_ = store.Save(thr)
	reopened, _ := session.NewJSONStore(dir)
	loaded, ok := reopened.Get(thr.ID)
	if !ok {
		t.Fatal("expected to find the persisted thread")
	}

	got := analytics.AnalyzeThread(loaded)
	if len(got) != 1 {
		t.Fatalf("expected 1 kind entry, got %d", len(got))
	}
	if got[0].Bytes >= int64(len(envelope)) {
		t.Errorf("bytes (%d) match or exceed JSON envelope (%d) — counting "+
			"the on-disk JSON, not the LLM payload", got[0].Bytes, len(envelope))
	}
	if got[0].Bytes != int64(len(art.Content)) {
		t.Errorf("bytes: got %d, want %d (raw payload)", got[0].Bytes, len(art.Content))
	}
}
