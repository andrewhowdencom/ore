package compaction

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/andrewhowdencom/ore/agent"
	"github.com/andrewhowdencom/ore/artifact"
	"github.com/andrewhowdencom/ore/loop"
	"github.com/andrewhowdencom/ore/models"
	"github.com/andrewhowdencom/ore/ledger"
	"github.com/andrewhowdencom/ore/x/llmbytes"
)

// MetaKeyBoundaryIndex is the ledger.Meta key under which the compaction
// boundary's turn index is stored. The Transform reads this key on every
// LLM call to project the buffer; the value is the string form of an
// int. This is the hot-path key — reads happen on every turn.
//
// The chosen namespace `ore.compaction.boundary.*` reserves the
// `ore.compaction.*` prefix for future compaction-related facts (e.g.
// strategy-specific metadata) without colliding with other session-level
// metadata that lives in junk.Thread.Metadata.
const MetaKeyBoundaryIndex = "ore.compaction.boundary.index"

// MetaKeyBoundaryInfo is the ledger.Meta key under which the BoundaryInfo
// struct is stored as a JSON-encoded string. The TUI reads this key to
// render the compaction collapse block (turn count, bytes saved,
// strategy, model, timestamp). This is the cold-path key — reads
// happen at most once per compaction.
const MetaKeyBoundaryInfo = "ore.compaction.boundary.info"

// BoundaryInfo captures the metadata of a single compaction event. It
// is the structured counterpart to the LLM-facing summary text that
// the compactor appends to the buffer: the Text artifact carries what
// the next model call sees; BoundaryInfo carries what the rest of the
// framework (the TUI, future analytics) needs to reason about what
// was folded.
//
// BoundaryInfo is JSON-serialized for storage in ledger.Meta under
// [MetaKeyBoundaryInfo]. The fields are stable; new fields may be
// added without breaking older readers.
type BoundaryInfo struct {
	CompactedThrough     int       `json:"compacted_through"`
	DroppedTurnCount     int       `json:"dropped_turn_count"`
	DroppedTokenEstimate int64     `json:"dropped_token_estimate"`
	Strategy             string    `json:"strategy"`
	Model                string    `json:"model,omitempty"`
	CreatedAt            time.Time `json:"created_at"`
}

// encodeBoundaryInfo serializes a BoundaryInfo for storage in
// ledger.Meta. The returned string is the raw JSON encoding; callers
// that want error-tolerant storage can ignore the error path (it
// cannot fail for valid BoundaryInfo values).
func encodeBoundaryInfo(info BoundaryInfo) (string, error) {
	b, err := json.Marshal(info)
	if err != nil {
		return "", fmt.Errorf("encode boundary info: %w", err)
	}
	return string(b), nil
}

// EncodeBoundaryInfo is the public form of [encodeBoundaryInfo],
// exposed for callers outside the package (notably
// junk.Stream.MarkBoundary) that need to write a BoundaryInfo
// into ledger.Meta.
func EncodeBoundaryInfo(info BoundaryInfo) (string, error) {
	return encodeBoundaryInfo(info)
}

// decodeBoundaryInfo parses a BoundaryInfo previously produced by
// [encodeBoundaryInfo]. A missing key (encoded as empty string) is
// not an error: the returned BoundaryInfo is the zero value, which is
// what callers should treat as "no info recorded".
//
// Exposed as [DecodeBoundaryInfo] for callers outside the package
// (notably the TUI) that need to read a boundary recorded by
// [Summarize] from ledger.Meta.
func decodeBoundaryInfo(encoded string) (BoundaryInfo, error) {
	if encoded == "" {
		return BoundaryInfo{}, nil
	}
	var info BoundaryInfo
	if err := json.Unmarshal([]byte(encoded), &info); err != nil {
		return BoundaryInfo{}, fmt.Errorf("decode boundary info: %w", err)
	}
	return info, nil
}

// DecodeBoundaryInfo is the public form of [decodeBoundaryInfo]. It
// returns the zero BoundaryInfo (and no error) when the encoded value
// is empty, which is the "no boundary recorded" signal.
func DecodeBoundaryInfo(encoded string) (BoundaryInfo, error) {
	return decodeBoundaryInfo(encoded)
}

// ErrTruncatedSummary is the sentinel returned by Summarize when the
// produced turn carries an artifact.StopReason with StopReasonLength.
// The error indicates the model hit its output-token cap mid-summary;
// the returned ledger.Turn is the zero value, so callers can detect
// the failure and refuse to append anything to the buffer.
//
// Use errors.Is to detect:
//
//	if errors.Is(err, compaction.ErrTruncatedSummary) { ... }
//
// This is intentionally a non-wrapped sentinel (exposed at the package
// level) so callers can switch on it without parsing error strings.
var ErrTruncatedSummary = errors.New("summarization produced truncated result")

// StrategyNameSummarize is the canonical value placed in
// BoundaryInfo.Strategy when Summarize is the producer. Future
// strategies (e.g. extractive, lossy-truncate) will introduce their
// own constants alongside this one.
const StrategyNameSummarize = "summarize"

// Summarize runs the configured agent once to summarize the given
// turns. The produced turn carries only an artifact.Text (the
// LLM-facing summary). The companion BoundaryInfo — the structured
// provenance of the compaction — is returned alongside so the caller
// can write it to ledger.Meta via stream.MarkBoundary (or equivalent).
//
// The agent is expected to be configured with a cognitive.SingleShot
// pattern (constructed by the caller). The caller owns the agent's
// lifecycle; Summarize does not close it. Any transforms and handlers
// registered on the agent apply to the summarization call (the same
// configuration surface as the main conversation, minus persona /
// system-prompt and tools — the caller decides what the compactor's
// agent carries).
//
// On success the returned turn is RoleSystem with a single
// artifact.Text (the LLM-facing summary). On ErrTruncatedSummary the
// returned turn is the zero value and the caller MUST NOT append
// anything to the buffer.
//
// Summarize subscribes to the agent's "turn_complete" event before
// running the agent, so the produced turn is captured from the agent's
// event stream. The agent's state binding (if any) is independent of
// the ephemeral buffer Summarize builds internally; the captured turn
// is the canonical result.
//
// The previous per-invocation 8192-token default is gone. Callers that
// want an explicit output budget set spec.MaxOutputTokens on the
// compactor agent (or pass it through agent.WithSpec).
func Summarize(ctx context.Context, a *agent.Agent, turns []ledger.Turn) (ledger.Turn, BoundaryInfo, error) {
	// Compute dropped bytes from input turns. The estimate is best-effort;
	// it is what the TUI marker renders and what analytics attributes to
	// the compaction, not what the provider reports.
	var droppedBytes int64
	for _, t := range turns {
		for _, art := range t.Artifacts {
			droppedBytes += llmbytes.Of(art)
		}
	}
	if len(turns) == 0 {
		return ledger.Turn{}, BoundaryInfo{}, nil
	}

	// Build an ephemeral buffer with the input turns and the user prompt.
	buf := &ledger.Buffer{}
	buf.LoadTurns(turns)
	buf.Append(ledger.RoleUser, artifact.Text{Content: defaultPrompt})

	// Subscribe to the agent's turn_complete event before running the
	// agent. The subscriber goroutine reads the first such event, copies
	// the produced turn into capturedCh, and exits. The EventBus's Emit
	// blocks until every subscriber has received the event, so by the
	// time agent.Run returns, the event is already queued on this
	// subscriber's channel — the main goroutine will receive it from
	// capturedCh without a race.
	type captured struct{ turn ledger.Turn }
	capturedCh := make(chan captured, 1)
	events := a.Subscribe("turn_complete")
	go func() {
		for ev := range events {
			if tc, ok := ev.(loop.TurnCompleteEvent); ok {
				select {
				case capturedCh <- captured{turn: tc.Turn}:
				default:
				}
				return
			}
		}
	}()

	if _, err := a.Run(ctx, buf); err != nil {
		return ledger.Turn{}, BoundaryInfo{}, fmt.Errorf("summarization agent run failed: %w", err)
	}

	var produced ledger.Turn
	select {
	case c := <-capturedCh:
		produced = c.turn
	case <-ctx.Done():
		return ledger.Turn{}, BoundaryInfo{}, ctx.Err()
	}

	// Truncation check: the produced turn carries an artifact.StopReason
	// if the model finished. A Length reason means the model hit its
	// output cap; the partial text we collected is not a valid summary.
	// Returning the zero turn preserves the caller's history so they
	// can decide policy.
	for _, art := range produced.Artifacts {
		if sr, ok := art.(artifact.StopReason); ok && sr.Reason == artifact.StopReasonLength {
			return ledger.Turn{}, BoundaryInfo{}, ErrTruncatedSummary
		}
	}

	return wrapSummaryTurn(produced, a.Spec(), droppedBytes, len(turns))
}

// wrapSummaryTurn concatenates the produced turn's Text / TextDelta
// content into a single Text artifact. The result is the RoleSystem
// turn that the compactor returns to its caller, paired with a
// BoundaryInfo describing the compaction event for ledger.Meta.
//
// Note: this no longer embeds an artifact.Compaction alongside the
// summary. The boundary marker has moved to ledger.Meta — see
// [MetaKeyBoundaryIndex] and [MetaKeyBoundaryInfo]. The wire now sees
// only `[Text]` for the compaction turn, which is what the Anthropic
// `onlyText` predicate expects.
func wrapSummaryTurn(turn ledger.Turn, spec models.Spec, droppedBytes int64, droppedTurnCount int) (ledger.Turn, BoundaryInfo, error) {
	var content string
	for _, art := range turn.Artifacts {
		switch a := art.(type) {
		case artifact.Text:
			content += a.Content
		case artifact.TextDelta:
			content += a.Content
		}
	}
	now := time.Now()
	out := ledger.Turn{
		Role: ledger.RoleSystem,
		Artifacts: []artifact.Artifact{
			artifact.Text{Content: content},
		},
		Timestamp: now,
	}
	info := BoundaryInfo{
		CompactedThrough:     droppedTurnCount,
		DroppedTurnCount:     droppedTurnCount,
		DroppedTokenEstimate: droppedBytes,
		Strategy:             StrategyNameSummarize,
		Model:                spec.Name,
		CreatedAt:            now,
	}
	return out, info, nil
}
