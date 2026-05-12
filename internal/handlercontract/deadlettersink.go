package handlercontract

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// DeadLetterSink is the append-only JSONL sink for events the bus could not
// deliver (consumer error, panic in observer, redaction-config-missing, etc.).
//
// Each call to Record appends one JSON object to the underlying file and
// fsyncs immediately — dead letters are rare and durability is more important
// than throughput.
//
// Default path: <project>/.harmonik/events/dead-letters.jsonl
// The path is supplied by the caller via OpenDeadLetterSink; the sink does NOT
// compute the project root itself.
//
// Spec ref: MVH_ROADMAP.md row #9.
// Bead ref: hk-qyue9.
type DeadLetterSink interface {
	// Record appends one dead-letter entry for env with the given reason.
	// Returns a non-nil error if the write or fsync fails.
	Record(ctx context.Context, env core.EventEnvelope, reason string) error

	// Close flushes any OS-buffered data and releases the file descriptor.
	// Must be called exactly once when the sink is no longer needed.
	Close() error
}

// deadLetterRecord is the JSON shape written per entry.
type deadLetterRecord struct {
	RecordedAt string             `json:"recorded_at"`
	Reason     string             `json:"reason"`
	Envelope   core.EventEnvelope `json:"envelope"`
}

// jsonlDeadLetterSink is the file-backed DeadLetterSink implementation.
type jsonlDeadLetterSink struct {
	mu   sync.Mutex
	file *os.File
}

// OpenDeadLetterSink opens (creates if needed) the dead-letter JSONL file at
// path and returns a DeadLetterSink bound to it.
//
// The parent directory must already exist; OpenDeadLetterSink does NOT create
// it. The caller is responsible for calling Close when the sink is no longer
// needed.
//
// Default path convention (enforced by the caller):
//
//	<project>/.harmonik/events/dead-letters.jsonl
//
// Bead ref: hk-qyue9.
//
//nolint:gosec // G304: path is daemon-startup-resolved and validated by the caller; not user input.
func OpenDeadLetterSink(path string) (DeadLetterSink, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("dead letter sink open %q: %w", path, err)
	}
	return &jsonlDeadLetterSink{file: f}, nil
}

// Record appends one dead-letter JSON line for env and fsyncs.
//
// The JSON shape is:
//
//	{"recorded_at":"<RFC3339Nano>","reason":"<short>","envelope":{...full Envelope...}}
//
// Concurrency: writes are serialised by an internal mutex.
func (s *jsonlDeadLetterSink) Record(_ context.Context, env core.EventEnvelope, reason string) error {
	rec := deadLetterRecord{
		RecordedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Reason:     reason,
		Envelope:   env,
	}

	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("dead letter sink marshal: %w", err)
	}

	// Append line + newline; fsync for durability (dead letters are rare).
	buf := make([]byte, len(line)+1)
	copy(buf, line)
	buf[len(line)] = '\n'

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.file.Write(buf); err != nil {
		return fmt.Errorf("dead letter sink write: %w", err)
	}
	if err := s.file.Sync(); err != nil {
		return fmt.Errorf("dead letter sink fsync: %w", err)
	}
	return nil
}

// Close flushes and releases the underlying file descriptor.
func (s *jsonlDeadLetterSink) Close() error {
	return s.file.Close()
}
