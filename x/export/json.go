package export

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/andrewhowdencom/ore/junk"
)

// JSON writes a pretty-printed JSON representation of the thread to w.
// It delegates to the thread's existing MarshalJSON implementation with
// indentation for readability.
func JSON(w io.Writer, thread *junk.Thread) error {
	data, err := json.MarshalIndent(thread, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal thread: %w", err)
	}
	_, err = w.Write(data)
	if err != nil {
		return fmt.Errorf("write json: %w", err)
	}
	return nil
}
