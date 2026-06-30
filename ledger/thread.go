package ledger

import (
	"time"

	"github.com/andrewhowdencom/ore/artifact"
)

// Thread is the tree-backed implementation of [State].
//
// A Thread owns a set of turns (the tree) plus a current-tip pointer
// identifying which branch is "active". The active path is computed
// on demand by [Thread.ResolveActivePath], which walks back from the
// current tip via [Turn.ParentID], applying each turn's
// [Metadata.Control] directive.
//
// Thread is not safe for concurrent use. The framework's serial
// pipeline — the loop worker goroutine, the Transform chain, the
// session's worker — is the only contract.
type Thread struct {
	// ID is the unique identifier of this thread. Empty for in-memory
	// ephemeral threads; populated when hydrated from the journal.
	ID string

	// turns is the addressable node pool. The walk traverses this
	// map via [Turn.ParentID] links.
	turns map[string]*Turn

	// CurrentTip selects the active branch. Walking back from this
	// turn's [Turn.ParentID] chain produces [Thread.Turns].
	CurrentTip string

	// meta holds per-conversation key-value state. Accessed via
	// [Thread.Meta]. Lazily allocated.
	meta map[string]string

	// clock is the source of turn timestamps. Defaults to wall-clock.
	clock Clock
}

// NewThread constructs an empty Thread with the default clock. Pass
// [WithThreadClock] to substitute.
func NewThread(opts ...func(*Thread)) *Thread {
	t := &Thread{
		turns: make(map[string]*Turn),
		clock: realClock{},
	}
	for _, opt := range opts {
		opt(t)
	}
	return t
}

// Turns returns the active-path projection of the tree: the turns
// reachable by walking back from [Thread.CurrentTip] via [Turn.ParentID],
// applying each turn's [Metadata.Control] directive.
//
// The result is in chronological order (root-to-tip). A defensive
// copy is returned so callers can iterate without affecting the
// underlying tree.
func (t *Thread) Turns() []Turn {
	return t.ResolveActivePath()
}

// ResolveActivePath walks the tree from [Thread.CurrentTip] backwards
// via [Turn.ParentID] and returns the turns in chronological order,
// honoring each turn's [Metadata.Control] directive:
//
//   - ControlContinue (or empty): include the turn, continue the walk.
//   - ControlStop: include the turn, terminate the walk.
//   - ControlSkip: exclude the turn, continue the walk.
//
// If CurrentTip is "" (empty thread), the result is empty.
//
// If a turn's ParentID references a non-existent turn (broken tree),
// the walk terminates at that gap. If a turn in the chain is missing
// from the thread, the walk terminates at the gap.
func (t *Thread) ResolveActivePath() []Turn {
	var path []Turn
	current := t.CurrentTip
	for current != "" {
		node, ok := t.turns[current]
		if !ok {
			break
		}

		control := node.Metadata.Control
		if control == ControlSkip {
			// Exclude, continue.
			current = node.ParentID
			continue
		}

		// Prepend for chronological (root-to-tip) order.
		path = append([]Turn{*node}, path...)

		if control == ControlStop {
			// Include, terminate.
			break
		}

		current = node.ParentID
	}
	return path
}

// Append adds a new turn to the thread, advancing the current tip.
// The new turn's ParentID is set to the previous current tip (or ""
// if the thread was empty), and the current tip advances to the new
// turn's ID. The new turn's ID is generated from a cryptographically
// random source.
func (t *Thread) Append(role Role, artifacts ...artifact.Artifact) {
	if t.turns == nil {
		t.turns = make(map[string]*Turn)
	}
	if t.clock == nil {
		t.clock = realClock{}
	}

	parentID := t.CurrentTip
	id := generateTurnID()
	t.turns[id] = &Turn{
		ID:        id,
		ParentID:  parentID,
		Role:      role,
		Artifacts: artifacts,
		Timestamp: t.clock.Now(),
	}
	t.CurrentTip = id
}

// SaveTurn inserts (or replaces) a turn in the thread without
// advancing the current tip. Use this when hydrating from a journal
// or restoring from persisted state. The caller is responsible for
// ensuring the turn's ID is unique within the thread.
func (t *Thread) SaveTurn(turn *Turn) {
	if t.turns == nil {
		t.turns = make(map[string]*Turn)
	}
	t.turns[turn.ID] = turn
}

// SetCurrentTip sets the active branch pointer. Use this after
// hydrating, when forking to switch branches, or when re-anchoring
// the walk after a re-parent.
func (t *Thread) SetCurrentTip(turnID string) {
	t.CurrentTip = turnID
}

// SetParent re-parents a turn, updating the in-memory tree structure.
// This is the in-memory equivalent of Repository.UpdateTurnParent;
// the repository persists it as a journal event.
//
// If turnID does not exist in the thread, SetParent is a no-op.
// The new parent ID is stored verbatim; the caller is responsible
// for ensuring the target exists if chain consistency matters.
func (t *Thread) SetParent(turnID, parentID string) {
	if node, ok := t.turns[turnID]; ok {
		node.ParentID = parentID
	}
}

// SetControl updates a turn's [Metadata.Control] directive. This is
// the in-memory equivalent of Repository.UpdateTurnControl.
//
// If turnID does not exist in the thread, SetControl is a no-op.
func (t *Thread) SetControl(turnID string, control TraversalControl) {
	if node, ok := t.turns[turnID]; ok {
		node.Metadata.Control = control
	}
}

// Meta returns the metadata handle for this thread. The handle is
// live: reads and writes operate on the thread's internal map.
// Multiple calls return handles that share the same backing storage,
// so writes through one are visible through any other.
//
// As with the rest of Thread's API, the Meta handle is not safe for
// concurrent use; the same serial-call contract applies.
func (t *Thread) Meta() Meta {
	if t.meta == nil {
		t.meta = make(map[string]string)
	}
	return &threadMeta{t: t}
}

// threadMeta is the concrete [Meta] implementation for [Thread].
type threadMeta struct {
	t *Thread
}

// Get implements [Meta].
func (m *threadMeta) Get(key string) (string, bool) {
	if m.t.meta == nil {
		return "", false
	}
	v, ok := m.t.meta[key]
	return v, ok
}

// Set implements [Meta].
func (m *threadMeta) Set(key, value string) {
	if m.t.meta == nil {
		m.t.meta = make(map[string]string)
	}
	m.t.meta[key] = value
}

// All implements [Meta].
func (m *threadMeta) All() map[string]string {
	out := make(map[string]string, len(m.t.meta))
	for k, v := range m.t.meta {
		out[k] = v
	}
	return out
}

// WithThreadClock configures Thread to use a custom Clock for turn
// timestamps. If not used, Thread defaults to the real wall-clock time.
func WithThreadClock(c Clock) func(*Thread) {
	return func(t *Thread) {
		t.clock = c
	}
}

// Compile-time check that [Thread] satisfies [State].
var _ State = (*Thread)(nil)

// Ensure unused-import lint doesn't trip on time. Some toolchains
// flag unused imports when the only consumer is via reflection.
var _ = time.Now