package export

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/andrewhowdencom/ore/ledger"
)

// wireThread is the on-disk wire format for a [Thread]. The shape is
// intentionally compatible with the previous junk.Thread.MarshalJSON
// output so external tooling that consumes the JSON export keeps
// working unchanged:
//
//	{
//	  "id": "...",
//	   "current_tip": "...",
//	   "metadata": {...},
//	   "turns": [...]
//	}
//
// created_at/updated_at are intentionally absent — conversations
// are temporal via their turn history.
//
// ledger.Turn is used directly because its JSON tags already match
// the fields above (Role, Timestamp, Artifacts, etc.), so json.Marshal
// produces the same per-turn shape junk.Thread's wire format did.
type wireThread struct {
	ID         string            `json:"id"`
	CurrentTip string            `json:"current_tip,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Turns      []ledger.Turn     `json:"turns"`
}

// JSON writes a pretty-printed JSON representation of the thread to
// w. The output preserves the wire format documented in the package
// comment — {id, current_tip, metadata, turns} with no
// created_at/updated_at — so external tooling continues to work.
func JSON(w io.Writer, t Thread) error {
	wt := wireThread{
		ID:       t.ID,
		Metadata: t.Metadata,
		Turns:    t.Turns,
	}
	// CurrentTip is reconstructed from the last turn's ID. An empty
	// turn list yields an empty tip, which is omitted by the
	// `omitempty` JSON tag — matching the previous junk.Thread
	// wire-format behavior.
	if n := len(t.Turns); n > 0 {
		wt.CurrentTip = t.Turns[n-1].ID
	}
	data, err := json.MarshalIndent(wt, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal thread: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write json: %w", err)
	}
	return nil
}