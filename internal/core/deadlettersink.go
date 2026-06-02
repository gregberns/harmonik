package core

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// DeadLetterSink is the append-only JSONL sink for events the bus could not
// deliver (consumer error, panic in observer, redaction-config-missing, etc.).
//
// Default path: <project>/.harmonik/events/dead-letters.jsonl
//
// Spec ref: MVH_ROADMAP.md row #9.
// Bead ref: hk-qyue9.
type DeadLetterSink interface {
	// Record appends one dead-letter entry for env with the given reason.
	Record(ctx context.Context, env EventEnvelope, reason string) error

	// Close flushes any OS-buffered data and releases the file descriptor.
	Close() error
}

// deadLetterRecord is the JSON shape written per entry.
type deadLetterRecord struct {
	RecordedAt string        `json:"recorded_at"`
	Reason     string        `json:"reason"`
	Envelope   EventEnvelope `json:"envelope"`
}

// jsonlDeadLetterSink is the file-backed DeadLetterSink implementation.
type jsonlDeadLetterSink struct {
	mu   sync.Mutex
	file *os.File
}

// OpenDeadLetterSink opens (creates if needed) the dead-letter JSONL file at
// path and returns a DeadLetterSink bound to it.
//
// The parent directory must already exist. The caller is responsible for
// calling Close when the sink is no longer needed.
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

func (s *jsonlDeadLetterSink) Record(_ context.Context, env EventEnvelope, reason string) error {
	rec := deadLetterRecord{
		RecordedAt: time.Now().UTC().Format(time.RFC3339Nano),
		Reason:     reason,
		Envelope:   env,
	}

	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("dead letter sink marshal: %w", err)
	}

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

func (s *jsonlDeadLetterSink) Close() error {
	return s.file.Close()
}

// NoopDeadLetterSink is a DeadLetterSink that silently discards all
// dead-letter records. Use as the required-argument default when the caller
// does not need to persist undeliverable events.
//
// Constructors that accept a DeadLetterSink argument MUST substitute nil with
// NoopDeadLetterSink{} rather than storing a nil interface value; the eventbus
// implementation relies on unconditional calls to Record (no nil-guard).
//
// Bead ref: hk-2m3bq.
type NoopDeadLetterSink struct{}

func (NoopDeadLetterSink) Record(_ context.Context, _ EventEnvelope, _ string) error {
	return nil
}

func (NoopDeadLetterSink) Close() error {
	return nil
}

// Compile-time check.
var _ DeadLetterSink = NoopDeadLetterSink{}
