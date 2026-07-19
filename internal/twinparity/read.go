package twinparity

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// LoadStream reads a JSONL event log from path and canonicalizes it into a
// Stream. Blank lines are skipped. A non-existent file is an error; use
// LoadStreamLines with a nil slice for the empty-stream case.
func LoadStream(path string) (Stream, error) {
	//nolint:gosec // G304: fixture paths are test-controlled, not user input.
	f, err := os.Open(path)
	if err != nil {
		return Stream{}, fmt.Errorf("twinparity: open %s: %w", path, err)
	}
	defer func() { _ = f.Close() }() //nolint:errcheck // read-only fixture handle; close error is irrelevant

	var lines []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return Stream{}, fmt.Errorf("twinparity: scan %s: %w", path, err)
	}
	return LoadStreamLines(lines)
}

// LoadStreamLines canonicalizes a slice of JSONL lines into a Stream. Blank
// lines are skipped; a line that fails to parse as a JSON object is an error.
func LoadStreamLines(lines []string) (Stream, error) {
	var stream Stream

	// First pass: find the earliest parseable envelope timestamp so elapsed
	// is measured relative to the stream's first record.
	var firstTS time.Time
	decoded := make([]map[string]json.RawMessage, 0, len(lines))
	for i, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(line), &obj); err != nil {
			return Stream{}, fmt.Errorf("twinparity: line %d not a JSON object: %w", i, err)
		}
		decoded = append(decoded, obj)
		if firstTS.IsZero() {
			if ts, ok := recordTimestamp(obj); ok {
				firstTS = ts
			}
		}
	}

	stream.Events = make([]CanonEvent, 0, len(decoded))
	for seq, obj := range decoded {
		stream.Events = append(stream.Events, canonRecord(obj, seq, firstTS))
	}
	return stream, nil
}
