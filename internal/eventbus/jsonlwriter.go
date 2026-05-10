package eventbus

import (
	"os"
	"sync"
)

// JSONLWriter is the append-only JSONL event log writer for harmonik.
//
// Every call to Append writes exactly one JSON line (a JSON object followed by
// a single newline character '\n') to the underlying file. The writer MUST NOT
// rewrite, truncate, or reorder existing lines (EV-020). Corruption in the
// form of a partial-line write on crash is detected by readers per the §6.2
// torn-tail read-recovery rule: a final line without a terminating newline is
// discarded silently in post-crash startup context.
//
// The file is opened with O_CREATE|O_WRONLY|O_APPEND so that every write is
// positioned at the file's end by the OS kernel, providing the atomicity
// guarantee described in the §6.2 "Concurrent tailing" note.
//
// Fsync behaviour is caller-driven: the caller passes a sync flag to Append.
// When sync is true, Append calls File.Sync() after the write; this is the
// mechanism for fsync-boundary (F-class) events per EV-015 / EV-016. Callers
// for O-class (ordinary) events pass sync=false.
//
// Spec ref: event-model.md §6.2 EV-020; §4.4 EV-015, EV-016; §6.1.
// Bead ref: hk-hqwn.29.
type JSONLWriter struct {
	mu   sync.Mutex
	file *os.File
}

// OpenJSONLWriter opens (or creates) the JSONL file at path with
// O_CREATE|O_WRONLY|O_APPEND semantics and returns a JSONLWriter bound to it.
//
// The caller is responsible for calling Close when the writer is no longer
// needed (e.g., on graceful daemon shutdown after Drain completes).
//
// Spec ref: event-model.md §6.2 EV-020.
//
//nolint:gosec // G304: path is daemon-startup-resolved and validated by the caller; not user input.
func OpenJSONLWriter(path string) (*JSONLWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &JSONLWriter{file: f}, nil
}

// Append writes a single JSONL line to the file.
//
// line MUST be a complete, valid JSON object serialised to a byte slice WITHOUT
// a trailing newline; Append appends exactly one '\n' terminator itself.
// Passing an empty line or a slice that already ends with '\n' violates the
// contract and will produce a malformed JSONL entry.
//
// When sync is true, Append calls [os.File.Sync] after the write to provide
// fsync-boundary (F-class) durability semantics per EV-016. When sync is
// false the write is best-effort (O-class / L-class events).
//
// Append is safe for concurrent use: all writes are serialised by an internal
// mutex. The underlying O_APPEND flag ensures that after the mutex releases,
// concurrent writers operating on different JSONLWriter instances bound to the
// same path do not interleave at the file-descriptor level for writes within
// PIPE_BUF; for lines exceeding PIPE_BUF, callers MUST NOT share the same
// file path across concurrent JSONLWriter instances without external coordination.
//
// Returns a non-nil error if the write or (when sync=true) the sync fails.
// The caller MUST treat a write error as a fatal condition and escalate per the
// on-disk durability contract (EV-015).
//
// Spec ref: event-model.md §6.2 EV-020; §4.4 EV-015, EV-016.
func (w *JSONLWriter) Append(line []byte, sync bool) error {
	// Allocate a single buffer: line + newline. This ensures one write call
	// per event, minimising torn-write window under POSIX O_APPEND semantics.
	buf := make([]byte, len(line)+1)
	copy(buf, line)
	buf[len(line)] = '\n'

	w.mu.Lock()
	defer w.mu.Unlock()

	if _, err := w.file.Write(buf); err != nil {
		return err
	}
	if sync {
		return w.file.Sync()
	}
	return nil
}

// Close flushes any OS-buffered data and releases the file descriptor.
//
// Close should be called exactly once, after all Append calls complete and
// the daemon's Drain phase has finished. Calling Close concurrently with
// Append produces undefined behaviour.
func (w *JSONLWriter) Close() error {
	return w.file.Close()
}
