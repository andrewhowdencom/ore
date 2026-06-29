package analytics_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/junk"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/x/analytics"
)

// errTest is a sentinel error for mocking store failures.
var errTest = &sentinelError{}

type sentinelError struct{}

func (e *sentinelError) Error() string { return "test error" }

// mockStore is a test double for junk.Store.
type mockStore struct {
	threads []*junk.Thread
	err     error
}

func (m *mockStore) Create() (*junk.Thread, error) { return nil, nil }
func (m *mockStore) Get(id string) (*junk.Thread, bool) { return nil, false }
func (m *mockStore) GetBy(key, value string) (*junk.Thread, bool) { return nil, false }
func (m *mockStore) Save(thread *junk.Thread) error { return nil }
func (m *mockStore) Delete(id string) bool { return false }
func (m *mockStore) List() ([]*junk.Thread, error) {
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

	got = analytics.AnalyzeTurns([]ledger.Turn{})
	if len(got) != 0 {
		t.Fatalf("expected empty slice for empty turns, got %d entries", len(got))
	}
}

func TestAnalyzeTurns_MixedArtifacts(t *testing.T) {
	turns := []ledger.Turn{
		{
			Role:      ledger.RoleUser,
			Artifacts: []artifact.Artifact{
				artifact.Text{Content: "hello"},
				artifact.Text{Content: "world"},
				artifact.Reasoning{Content: "thinking"},
			},
		},
		{
			// Assistant turn: the model emits the call, plus other
			// non-tool artifacts (image, usage). The framework never
			// co-locates tool_result with tool_call, but the analytics
			// resolution is whole-scope so it doesn't matter.
			Role: ledger.RoleAssistant,
			Artifacts: []artifact.Artifact{
				artifact.ToolCall{ID: "1", Name: "bash", Arguments: `{"cmd":"ls"}`},
				artifact.Image{URL: "http://example.com/img.png"},
				artifact.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
			},
		},
		{
			// Tool turn: the handler emits the result separately.
			Role: ledger.RoleTool,
			Artifacts: []artifact.Artifact{
				artifact.ToolResult{ToolCallID: "1", Content: "ok"},
			},
		},
	}

	got := analytics.AnalyzeTurns(turns)
	if len(got) != 6 {
		t.Fatalf("expected 6 kind entries, got %d", len(got))
	}

	byKind := make(map[string]*analytics.Stats, len(got))
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

	// tool_call: 1 artifact with Name="bash". LLMString() returns
	// t.Arguments = `{"cmd":"ls"}` of length 12.
	if s, ok := byKind["tool_call"]; !ok {
		t.Fatal("missing 'tool_call' kind")
	} else if s.Source != "bash" {
		t.Errorf("tool_call source: got %q, want %q", s.Source, "bash")
	} else if s.Count != 1 {
		t.Errorf("tool_call count: got %d, want 1", s.Count)
	} else if s.Bytes != 12 {
		t.Errorf("tool_call bytes: got %d, want 12", s.Bytes)
	}

	// tool_result: 1 artifact, len(LLMString()) = len("ok") = 2.
	// Source is resolved by joining ToolCallID="1" against the
	// tool_call{Name="bash", ID="1"} in the assistant turn above
	// via whole-scope resolution (the call and result are in
	// separate ledger.Turn values; the framework always emits them
	// that way).
	if s, ok := byKind["tool_result"]; !ok {
		t.Fatal("missing 'tool_result' kind")
	} else if s.Source != "bash" {
		t.Errorf("tool_result source: got %q, want %q", s.Source, "bash")
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

// TestAnalyzeTurns_ToolCallBucketedByName exercises the second axis
// of the per-(Kind, Source) breakdown: multiple tool_call artifacts
// targeting different tools must produce distinct rows, and two
// tool_calls targeting the same tool must aggregate into one row.
func TestAnalyzeTurns_ToolCallBucketedByName(t *testing.T) {
	turns := []ledger.Turn{
		{
			Role: ledger.RoleAssistant,
			Artifacts: []artifact.Artifact{
				// Two calls target "bash" (different IDs, different args).
				// The (tool_call, bash) row should aggregate both.
				artifact.ToolCall{ID: "1", Name: "bash", Arguments: `{"cmd":"ls"}`},
				artifact.ToolCall{ID: "2", Name: "file_read", Arguments: `{"path":"/tmp/x"}`},
				artifact.ToolCall{ID: "3", Name: "bash", Arguments: `{"cmd":"pwd"}`},
			},
		},
	}

	got := analytics.AnalyzeTurns(turns)
	if len(got) != 2 {
		t.Fatalf("expected 2 (kind, source) buckets, got %d: %+v", len(got), got)
	}

	// Sorted lexicographically: "bash" < "file_read".
	if got[0].Kind != "tool_call" || got[0].Source != "bash" {
		t.Errorf("got[0]: got (%q, %q), want (tool_call, bash)", got[0].Kind, got[0].Source)
	}
	if got[0].Count != 2 {
		t.Errorf("bash count: got %d, want 2", got[0].Count)
	}
	// Bytes: len(`{"cmd":"ls"}`) + len(`{"cmd":"pwd"}`) = 12 + 13 = 25.
	if got[0].Bytes != 25 {
		t.Errorf("bash bytes: got %d, want 25", got[0].Bytes)
	}

	if got[1].Kind != "tool_call" || got[1].Source != "file_read" {
		t.Errorf("got[1]: got (%q, %q), want (tool_call, file_read)", got[1].Kind, got[1].Source)
	}
	if got[1].Count != 1 {
		t.Errorf("file_read count: got %d, want 1", got[1].Count)
	}
	// Bytes: len(`{"path":"/tmp/x"}`) = 17.
	if got[1].Bytes != 17 {
		t.Errorf("file_read bytes: got %d, want 17", got[1].Bytes)
	}
}

// TestAnalyzeTurns_ToolResultResolvedByToolCall exercises the
// whole-scope join: a tool_result's Source is the originating
// tool_call's Name, looked up by ToolCallID across the slice —
// specifically across the assistant/role-tool turn boundary that
// the framework always produces.
func TestAnalyzeTurns_ToolResultResolvedByToolCall(t *testing.T) {
	turns := []ledger.Turn{
		{
			Role: ledger.RoleAssistant,
			Artifacts: []artifact.Artifact{
				artifact.ToolCall{ID: "1", Name: "bash", Arguments: `{"cmd":"ls"}`},
			},
		},
		{
			Role: ledger.RoleTool,
			Artifacts: []artifact.Artifact{
				artifact.ToolResult{ToolCallID: "1", Content: "ok"},
			},
		},
	}

	got := analytics.AnalyzeTurns(turns)
	if len(got) != 2 {
		t.Fatalf("expected 2 (kind, source) buckets, got %d: %+v", len(got), got)
	}

	// Sort: "bash" sorts before "" only if it's strictly less.
	// In ASCII, '(' (0x28) sorts before 'b' (0x62), so
	// (tool_call, bash) sorts after (tool_result, bash). Wait:
	// within the same Kind, the sort is purely on Source, so for
	// tool_call we have ("bash") and for tool_result we have
	// ("bash") — they're separate kinds. Cross-kind, the sort is
	// by Kind: "tool_call" < "tool_result".
	if got[0].Kind != "tool_call" || got[0].Source != "bash" {
		t.Errorf("got[0]: got (%q, %q), want (tool_call, bash)", got[0].Kind, got[0].Source)
	}
	if got[0].Count != 1 || got[0].Bytes != 12 {
		t.Errorf("tool_call: got (count=%d, bytes=%d), want (1, 12)", got[0].Count, got[0].Bytes)
	}
	if got[1].Kind != "tool_result" || got[1].Source != "bash" {
		t.Errorf("got[1]: got (%q, %q), want (tool_result, bash)", got[1].Kind, got[1].Source)
	}
	if got[1].Count != 1 || got[1].Bytes != 2 {
		t.Errorf("tool_result: got (count=%d, bytes=%d), want (1, 2)", got[1].Count, got[1].Bytes)
	}
}

// TestAnalyzeTurns_ToolResultOrphan covers the case where a
// tool_result's ToolCallID has no matching tool_call anywhere in the
// scope. The result should bucket under the Source "(unknown)" so
// the gap is visible in the report.
func TestAnalyzeTurns_ToolResultOrphan(t *testing.T) {
	turns := []ledger.Turn{
		{
			// An unrelated tool_call, deliberately with a
			// different ID than the orphan's ToolCallID.
			Role: ledger.RoleAssistant,
			Artifacts: []artifact.Artifact{
				artifact.ToolCall{ID: "1", Name: "file_read", Arguments: `{"path":"/tmp/x"}`},
			},
		},
		{
			// Orphan: no tool_call in this scope has ID "missing".
			Role: ledger.RoleTool,
			Artifacts: []artifact.Artifact{
				artifact.ToolResult{ToolCallID: "missing", Content: "ok"},
			},
		},
	}

	got := analytics.AnalyzeTurns(turns)
	if len(got) != 2 {
		t.Fatalf("expected 2 (kind, source) buckets, got %d: %+v", len(got), got)
	}

	// Sort order: "tool_call" < "tool_result".
	if got[0].Kind != "tool_call" || got[0].Source != "file_read" {
		t.Errorf("got[0]: got (%q, %q), want (tool_call, file_read)", got[0].Kind, got[0].Source)
	}

	if got[1].Kind != "tool_result" || got[1].Source != "(unknown)" {
		t.Errorf("got[1]: got (%q, %q), want (tool_result, \"(unknown)\")", got[1].Kind, got[1].Source)
	}
	if got[1].Count != 1 || got[1].Bytes != 2 {
		t.Errorf("orphan tool_result: got (count=%d, bytes=%d), want (1, 2)", got[1].Count, got[1].Bytes)
	}
}

func TestAnalyzeTurns_UnknownArtifact(t *testing.T) {
	turns := []ledger.Turn{
		{
			Role:      ledger.RoleUser,
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
	th := &junk.Thread{State: &ledger.Buffer{}}
	got := analytics.AnalyzeThread(th)
	if len(got) != 0 {
		t.Fatalf("expected empty slice, got %d entries", len(got))
	}
}

func TestAnalyzeThread_WithTurns(t *testing.T) {
	buf := &ledger.Buffer{}
	buf.Append(ledger.RoleUser, artifact.Text{Content: "hi"})
	th := &junk.Thread{State: buf}

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
	buf1 := &ledger.Buffer{}
	buf1.Append(ledger.RoleUser, artifact.Text{Content: "hi"})
	buf2 := &ledger.Buffer{}
	buf2.Append(ledger.RoleAssistant, artifact.Reasoning{Content: "think"})

	store := &mockStore{
		threads: []*junk.Thread{
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
// production read path for any junk.Store implementation is "load
// from disk", and the byte counts observed after a round-trip must
// match the in-memory baseline. Before the fix, junk/serialize.go
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
	// Tool_call and tool_result are placed in the separate turns the
	// framework actually produces: the call in RoleAssistant, the
	// result in a subsequent RoleTool turn.
	turns := []ledger.Turn{
		{
			Role: ledger.RoleUser,
			Artifacts: []artifact.Artifact{
				artifact.Text{Content: "hi"},
				artifact.Reasoning{Content: "think"},
				artifact.ToolCall{ID: "1", Name: "bash", Arguments: `{"cmd":"ls"}`},
				artifact.Image{URL: "http://example.com/img.png"},
				artifact.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
			},
		},
		{
			Role: ledger.RoleTool,
			Artifacts: []artifact.Artifact{
				artifact.ToolResult{ToolCallID: "1", Content: "ok"},
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
	store, err := junk.NewJSONStore(dir)
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
	reopened, err := junk.NewJSONStore(dir)
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
	// junk/serialize.go.
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
		// Source must also round-trip identically. For tool_call and
		// tool_result, this means the originating tool's Name must
		// survive JSON round-trip AND the whole-scope join must still
		// find the originating call after reload (the call is in
		// turn 0, the result is in turn 1; resolution crosses the
		// assistant/role-tool boundary).
		if got[i].Source != baseline[i].Source {
			t.Errorf("%s source: got %q, want %q (whole-scope join did not "+
				"survive the JSON round-trip)",
				got[i].Kind, got[i].Source, baseline[i].Source)
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
	store, err := junk.NewJSONStore(dir)
	if err != nil {
		t.Fatalf("new store: %v", err)
	}

	thr, err := store.Create()
	if err != nil {
		t.Fatalf("create thread: %v", err)
	}
	thr.State.Append(ledger.RoleUser,
		artifact.Text{Content: "hi"},
		artifact.Reasoning{Content: "think"},
		artifact.ToolCall{ID: "1", Name: "bash", Arguments: `{"cmd":"ls"}`},
		artifact.Image{URL: "http://example.com/img.png"},
		artifact.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	)
	thr.State.Append(ledger.RoleTool,
		artifact.ToolResult{ToolCallID: "1", Content: "ok"},
	)
	if err := store.Save(thr); err != nil {
		t.Fatalf("save: %v", err)
	}

	reopened, err := junk.NewJSONStore(dir)
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
	bySource := make(map[string]string, len(got))
	for _, s := range got {
		byKind[s.Kind] = s.Bytes
		bySource[s.Kind] = s.Source
	}
	for kind, expected := range want {
		if byKind[kind] != expected {
			t.Errorf("%s bytes: got %d, want %d", kind, byKind[kind], expected)
		}
	}
	// tool_call and tool_result are bucketed by the originating
	// tool's Name after JSON round-trip.
	if bySource["tool_call"] != "bash" {
		t.Errorf("tool_call source: got %q, want %q", bySource["tool_call"], "bash")
	}
	if bySource["tool_result"] != "bash" {
		t.Errorf("tool_result source: got %q, want %q", bySource["tool_result"], "bash")
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
	store, _ := junk.NewJSONStore(dir)
	thr, _ := store.Create()
	thr.State.Append(ledger.RoleUser, art)
	_ = store.Save(thr)
	reopened, _ := junk.NewJSONStore(dir)
	loaded, ok := reopened.Get(thr.ID)
	if !ok {
		t.Fatal("expected to find the persisted thread")
	}

	got := analytics.AnalyzeThread(loaded)
	if len(got) != 1 {
		t.Fatalf("expected 1 kind entry, got %d", len(got))
	}
	// Text has no notion of a tool source, so Source must be empty.
	if got[0].Source != "" {
		t.Errorf("source: got %q, want \"\" (text has no source)", got[0].Source)
	}
	if got[0].Bytes >= int64(len(envelope)) {
		t.Errorf("bytes (%d) match or exceed JSON envelope (%d) — counting "+
			"the on-disk JSON, not the LLM payload", got[0].Bytes, len(envelope))
	}
	if got[0].Bytes != int64(len(art.Content)) {
		t.Errorf("bytes: got %d, want %d (raw payload)", got[0].Bytes, len(art.Content))
	}
}

// TestAnalyzeStore_ToolResultOrphanPerThread confirms that the
// whole-scope ToolCallID→Name join is scoped to a single thread: a
// tool_result in thread B whose ToolCallID matches a tool_call in
// thread A is still an orphan, because cross-thread resolution is
// out of scope. This guards the per-thread invariant while also
// covering the (unknown) bucket at the store-wide aggregate level.
func TestAnalyzeStore_ToolResultOrphanPerThread(t *testing.T) {
	// Thread A: a tool_call in the assistant turn and a matching
	// tool_result in a separate role-tool turn. The result must
	// resolve to "bash" via the whole-scope join within thread A.
	bufA := &ledger.Buffer{}
	bufA.Append(ledger.RoleAssistant,
		artifact.ToolCall{ID: "1", Name: "bash", Arguments: `{"cmd":"ls"}`},
	)
	bufA.Append(ledger.RoleTool,
		artifact.ToolResult{ToolCallID: "1", Content: "ok"},
	)

	// Thread B: a tool_call in the assistant turn and a tool_result
	// in a separate role-tool turn whose ToolCallID does NOT match
	// the local call ("2" vs "1"). Even though thread A has a call
	// with the matching ID, the per-thread scope prevents it from
	// rescuing thread B's result.
	bufB := &ledger.Buffer{}
	bufB.Append(ledger.RoleAssistant,
		artifact.ToolCall{ID: "2", Name: "file_read", Arguments: `{"path":"/tmp/x"}`},
	)
	bufB.Append(ledger.RoleTool,
		artifact.ToolResult{ToolCallID: "1", Content: "ok"},
	)

	store := &mockStore{
		threads: []*junk.Thread{
			{State: bufA},
			{State: bufB},
		},
	}

	got, err := analytics.AnalyzeStore(store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expected rows, sorted lexicographically by (Kind, Source).
	// Note: '(' (0x28) sorts before 'b' (0x62), so "(unknown)" sorts
	// before "bash" within the tool_result kind.
	//   (tool_call, bash, 1, 12)         — from thread A
	//   (tool_call, file_read, 1, 17)    — from thread B
	//   (tool_result, (unknown), 1, 2)   — from thread B (orphan)
	//   (tool_result, bash, 1, 2)        — from thread A
	want := []analytics.Stats{
		{Kind: "tool_call", Source: "bash", Count: 1, Bytes: 12},
		{Kind: "tool_call", Source: "file_read", Count: 1, Bytes: 17},
		{Kind: "tool_result", Source: "(unknown)", Count: 1, Bytes: 2},
		{Kind: "tool_result", Source: "bash", Count: 1, Bytes: 2},
	}

	if len(got) != len(want) {
		t.Fatalf("expected %d rows, got %d: %+v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d]: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestAnalyzeTurns_ToolResultResolvedAcrossTurns is the regression
// test for the cross-turn attribution bug. The framework always
// emits tool_call in a RoleAssistant turn and tool_result in a
// separate RoleTool turn. Under the previous same-turn resolution
// these would never match; under whole-scope resolution they do.
//
// This test deliberately mirrors the architecture the framework
// produces. If a future change makes tool_call and tool_result
// share a turn again, this test must be updated to reflect that
// new architecture — but it must never be re-shaped to silently
// pass under a same-turn resolution that hides the original bug.
func TestAnalyzeTurns_ToolResultResolvedAcrossTurns(t *testing.T) {
	turns := []ledger.Turn{
		{
			Role: ledger.RoleUser,
			Artifacts: []artifact.Artifact{
				artifact.Text{Content: "list /tmp"},
			},
		},
		{
			Role: ledger.RoleAssistant,
			Artifacts: []artifact.Artifact{
				artifact.ToolCall{ID: "A", Name: "bash", Arguments: `{"cmd":"ls"}`},
			},
		},
		{
			Role: ledger.RoleTool,
			Artifacts: []artifact.Artifact{
				artifact.ToolResult{ToolCallID: "A", Content: "a.txt b.txt"},
			},
		},
		{
			// Subsequent assistant turn, with no new tool calls.
			// The previous result is still in state and would be
			// re-sent on the next API call.
			Role: ledger.RoleAssistant,
			Artifacts: []artifact.Artifact{
				artifact.Text{Content: "you have a.txt and b.txt"},
			},
		},
	}

	got := analytics.AnalyzeTurns(turns)

	// tool_result must resolve to "bash" via whole-scope join.
	// Under same-turn resolution, this row would bucket as
	// (tool_result, "(unknown)") and the test would fail.
	var toolResult *analytics.Stats
	for i := range got {
		if got[i].Kind == "tool_result" {
			toolResult = &got[i]
		}
	}
	if toolResult == nil {
		t.Fatalf("expected a tool_result row, got: %+v", got)
	}
	if toolResult.Source != "bash" {
		t.Errorf("tool_result source: got %q, want %q (cross-turn "+
			"resolution failed; this is the regression test for the "+
			"analytics attribution bug)", toolResult.Source, "bash")
	}
	if toolResult.Count != 1 {
		t.Errorf("tool_result count: got %d, want 1", toolResult.Count)
	}
	// Bytes: len("a.txt b.txt") = 11.
	if toolResult.Bytes != 11 {
		t.Errorf("tool_result bytes: got %d, want 11", toolResult.Bytes)
	}
}

// TestAnalyzeTurns_ParallelToolCalls covers the case where the model
// emits multiple tool_calls in one assistant turn (e.g. parallel
// tool execution) and the framework emits multiple tool_results
// in a single role-tool turn. Each result must resolve to the
// originating call's Name.
func TestAnalyzeTurns_ParallelToolCalls(t *testing.T) {
	turns := []ledger.Turn{
		{
			Role: ledger.RoleAssistant,
			Artifacts: []artifact.Artifact{
				artifact.ToolCall{ID: "A", Name: "bash", Arguments: `{"cmd":"ls /tmp"}`},
				artifact.ToolCall{ID: "B", Name: "bash", Arguments: `{"cmd":"ls /etc"}`},
				artifact.ToolCall{ID: "C", Name: "file_read", Arguments: `{"path":"/etc/hosts"}`},
			},
		},
		{
			Role: ledger.RoleTool,
			Artifacts: []artifact.Artifact{
				artifact.ToolResult{ToolCallID: "A", Content: "tmp-a"},
				artifact.ToolResult{ToolCallID: "B", Content: "etc-b"},
				artifact.ToolResult{ToolCallID: "C", Content: "127.0.0.1 localhost"},
			},
		},
	}

	got := analytics.AnalyzeTurns(turns)

	// tool_call: 3 artifacts, two for "bash" (different IDs), one
	// for "file_read". Aggregated: 2 rows.
	// tool_result: 3 artifacts, two for "bash" (different IDs but
	// same source "bash"), one for "file_read". Aggregated: 2 rows.
	// Total: 4 rows.
	want := []analytics.Stats{
		{Kind: "tool_call", Source: "bash", Count: 2, Bytes: 0},       // bytes asserted below
		{Kind: "tool_call", Source: "file_read", Count: 1, Bytes: 0},  // bytes asserted below
		{Kind: "tool_result", Source: "bash", Count: 2, Bytes: 0},    // bytes asserted below
		{Kind: "tool_result", Source: "file_read", Count: 1, Bytes: 0}, // bytes asserted below
	}
	// Compute the expected bytes:
	//   tool_call bash:    len(`{"cmd":"ls /tmp"}`) + len(`{"cmd":"ls /etc"}`) = 17 + 17 = 34
	//   tool_call file_read: len(`{"path":"/etc/hosts"}`) = 21
	//   tool_result bash:    len("tmp-a") + len("etc-b") = 5 + 5 = 10
	//   tool_result file_read: len("127.0.0.1 localhost") = 19
	want[0].Bytes = 34
	want[1].Bytes = 21
	want[2].Bytes = 10
	want[3].Bytes = 19

	if len(got) != len(want) {
		t.Fatalf("expected %d rows, got %d: %+v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d]: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

// TestAnalyzeStore_PerThreadIsolation_ToolCallInThreadAResolvesOnlyInThreadA
// confirms that a tool_call in thread A does not rescue a tool_result
// in thread B that happens to share a ToolCallID. Each thread's
// whole-scope join is independent.
func TestAnalyzeStore_PerThreadIsolation_ToolCallInThreadAResolvesOnlyInThreadA(t *testing.T) {
	// Thread A: a tool_call with ID "1" and a matching tool_result.
	// Whole-scope join within thread A resolves the result to "bash".
	bufA := &ledger.Buffer{}
	bufA.Append(ledger.RoleAssistant,
		artifact.ToolCall{ID: "1", Name: "bash", Arguments: `{"cmd":"ls"}`},
	)
	bufA.Append(ledger.RoleTool,
		artifact.ToolResult{ToolCallID: "1", Content: "ok"},
	)

	// Thread B: a tool_result whose ToolCallID is "1" — the same ID
	// as thread A's call. Per-thread scope prevents thread A's call
	// from resolving thread B's result. Thread B has no local call
	// with ID "1" → orphan.
	bufB := &ledger.Buffer{}
	bufB.Append(ledger.RoleTool,
		artifact.ToolResult{ToolCallID: "1", Content: "ok"},
	)

	store := &mockStore{
		threads: []*junk.Thread{
			{State: bufA},
			{State: bufB},
		},
	}

	got, err := analytics.AnalyzeStore(store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Expected rows: thread A contributes (tool_call, bash, 1, 12)
	// and (tool_result, bash, 1, 2). Thread B contributes
	// (tool_result, "(unknown)", 1, 2).
	want := []analytics.Stats{
		{Kind: "tool_call", Source: "bash", Count: 1, Bytes: 12},
		{Kind: "tool_result", Source: "(unknown)", Count: 1, Bytes: 2},
		{Kind: "tool_result", Source: "bash", Count: 1, Bytes: 2},
	}

	if len(got) != len(want) {
		t.Fatalf("expected %d rows, got %d: %+v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d]: got %+v, want %+v", i, got[i], want[i])
		}
	}
}
