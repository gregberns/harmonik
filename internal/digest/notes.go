package digest

import (
	"bufio"
	"encoding/json"
	"os"
	"time"
)

// noteEntry is the per-line JSON shape in notes.jsonl per CL-040.
type noteEntry struct {
	SchemaVersion int       `json:"schema_version"`
	Ts            time.Time `json:"ts"`
	ToolCallID    string    `json:"tool_call_id"`
	SessionID     string    `json:"session_id"`
	Kind          string    `json:"kind"`
	Refs          []string  `json:"refs"`
	Text          string    `json:"text"`
	ResolvedAt    *string   `json:"resolved_at,omitempty"`
	ResolvedBy    *string   `json:"resolved_by,omitempty"`
}

// readOpenNotes reads notes.jsonl and returns all unresolved entries.
// A missing file returns (nil, nil) — no notes is valid.
//
//nolint:gosec // G304: path is caller-supplied project dir, not user input.
func readOpenNotes(path string) ([]noteEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var open []noteEntry
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		var entry noteEntry
		if jsonErr := json.Unmarshal(scanner.Bytes(), &entry); jsonErr != nil {
			continue // skip malformed lines
		}
		if entry.ResolvedAt == nil {
			open = append(open, entry)
		}
	}
	return open, scanner.Err()
}
