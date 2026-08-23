package pi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

const piMessageUpdateType = "message_update"

const (
	piMessageUpdateEventKey   = "assistantMessageEvent"
	piAccumulatedSnapshotKey  = "partial" // inside assistantMessageEvent
	piTopLevelAccumulationKey = "message" // top level of the message_update line
)

// StdoutLogWriter is the sink side of the Pi stdout tee. It splits the byte
// stream into NDJSON lines and writes each line's persisted form (see
// piStdoutLogLine) to the underlying writer.
//
// A Write never writes fewer bytes than it accepts as far as its CALLER is
// concerned: it reports len(p) on success, because a filtering writer that
// reported the smaller on-disk count would make io.TeeReader report a short
// write and fail the read that produced the bytes. What reaches the sink is
// smaller on purpose.
//
// At most one incomplete line is buffered. Close writes any trailing fragment
// so a stream that ends without a newline still lands on disk.
//
// WRITE AND CLOSE RUN ON DIFFERENT GOROUTINES, so both take the mutex. The tee
// writes from the SpawnWatcher's read goroutine; Close runs from
// runAgentLaunch's defer on the dispatch goroutine, and three documented paths
// return with the watcher still draining and say so in their own comments — the
// kill-watcher reap grace in internal/runloop/waitsocketgrace.go and both
// "watcher.Done() reap timed out after Kill — continuing" sites in
// internal/daemon/agentlaunch.go. Callers read launch.Watcher.Err() after the
// return, so the watcher provably outlives the function. The *os.File this
// writer sits in front of synchronises Close against Write internally; a
// bytes.Buffer does not.
//
// The lock covers the sink write too, not only the buffer. Releasing it earlier
// would let two goroutines interleave halves of two lines into the file, which
// is the same evidence loss by another route.
type StdoutLogWriter struct {
	mu   sync.Mutex
	sink io.Writer
	buf  bytes.Buffer
}

// NewStdoutLogWriter wraps sink so Pi NDJSON lines are persisted without their
// accumulated message snapshots.
func NewStdoutLogWriter(sink io.Writer) *StdoutLogWriter {
	return &StdoutLogWriter{sink: sink}
}

// Write implements io.Writer over the NDJSON stream.
func (w *StdoutLogWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf.Write(p)
	for {
		b := w.buf.Bytes()
		idx := bytes.IndexByte(b, '\n')
		if idx < 0 {
			break
		}
		line := make([]byte, idx+1)
		copy(line, b[:idx+1])
		w.buf.Next(idx + 1)
		if _, err := w.sink.Write(piStdoutLogLine(line)); err != nil {
			return 0, fmt.Errorf("pi stdout log: write line: %w", err)
		}
	}
	return len(p), nil
}

// Close flushes a trailing line that arrived without its newline. It does not
// close the underlying sink — the caller owns the file it opened.
//
// Safe to call while a Write is in flight, and safe to call more than once: the
// second call finds an empty buffer and does nothing.
func (w *StdoutLogWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.buf.Len() == 0 {
		return nil
	}
	line := w.buf.Bytes()
	w.buf.Reset()
	if _, err := w.sink.Write(piStdoutLogLine(line)); err != nil {
		return fmt.Errorf("pi stdout log: flush trailing line: %w", err)
	}
	return nil
}

func piStdoutLogLine(line []byte) []byte {
	if !bytes.Contains(line, []byte(`"`+piMessageUpdateType+`"`)) {
		return line
	}

	trimmed := bytes.TrimRight(line, "\r\n")
	newline := line[len(trimmed):]

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(trimmed, &fields); err != nil {
		return line
	}
	var kind string
	if err := json.Unmarshal(fields["type"], &kind); err != nil || kind != piMessageUpdateType {
		return line
	}

	event, ok := fields[piMessageUpdateEventKey]
	if !ok {
		return line
	}
	var eventFields map[string]json.RawMessage
	if err := json.Unmarshal(event, &eventFields); err != nil {
		return line
	}
	if _, hasPartial := eventFields[piAccumulatedSnapshotKey]; !hasPartial {
		if _, hasTopLevel := fields[piTopLevelAccumulationKey]; !hasTopLevel {
			return line
		}
	}
	delete(eventFields, piAccumulatedSnapshotKey)
	delete(fields, piTopLevelAccumulationKey)

	rewrittenEvent, err := piEncodeJSONLine(eventFields)
	if err != nil {
		return line
	}
	fields[piMessageUpdateEventKey] = rewrittenEvent
	rewritten, err := piEncodeJSONLine(fields)
	if err != nil {
		return line
	}
	return append(rewritten, newline...)
}

func piEncodeJSONLine(v any) ([]byte, error) {
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("pi stdout log: encode line: %w", err)
	}
	return bytes.TrimRight(out.Bytes(), "\n"), nil
}
