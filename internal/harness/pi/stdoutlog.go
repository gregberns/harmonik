package pi

// stdoutlog.go — the persisted form of Pi's NDJSON stdout (hk-k4jrh).
//
// The daemon tees the Pi child's stdout into
// <worktree>/.harmonik/pi-agent/pi-stdout.log so a failed run can be read after
// the fact (hk-j6wm7). Tee'd verbatim, that file grows with the SQUARE of how
// much the model says, because every `message_update` line carries the WHOLE
// accumulated assistant message so far — twice:
//
//	{"type":"message_update",
//	 "assistantMessageEvent":{"type":"thinking_delta","contentIndex":0,
//	                          "delta":"<the new text>",
//	                          "partial":{<the whole message so far>}},
//	 "message":{<the whole message so far>}}
//
// Line N therefore repeats everything in lines 1..N-1. One 8.5-minute run on
// 2026-08-15 wrote 197,243,057 bytes for 45,063 characters of model output.
// That matters beyond the disk it eats: below 10 GiB free the daemon pauses
// dispatch and says nothing, so a long run can wedge the fleet well before its
// own 90-minute ceiling fires.
//
// StdoutLogWriter drops those two accumulated snapshots on their way to disk.
// Nothing is lost that a person reading the log needs:
//
//   - `delta` on each *_delta event still carries the new text. Replayed over
//     the same run above, the deltas reconstruct every completed thinking and
//     text block CHARACTER FOR CHARACTER against the `content` field that
//     `thinking_end` / `text_end` carries (6 of 6 blocks, measured).
//   - `content` on `thinking_end` / `text_end`, `toolCall` on `toolcall_end`,
//     `args` on `tool_execution_start` and `result` on `tool_execution_end` are
//     separate fields and are untouched.
//   - `message_start`, `message_end`, `agent_end` and `turn_end` are not
//     rewritten at all, so the final assistant message survives in full.
//
// The same run shrinks from 197,243,057 bytes to 830,717 — a factor of 237 —
// and the curve goes from quadratic to linear in the length of the turn. Both
// figures are the shipped code measured on 2026-08-15 against the retained
// capture of run 01a0061c-6601-7528-9544-2ed98c23b967, which is kept at
// .harmonik/worktrees/<run-id>/.harmonik/pi-agent/pi-stdout.log — a failed pi
// run keeps its worktree, so the file survives to be re-measured. Name the path
// rather than the run: a reviewer looked for this capture in ~/.harmonik, /tmp
// and /var/folders, concluded it was gone, and flagged the figures as
// underivable.
//
// WHAT CHANGES ON DISK, EXACTLY. A line that is not a rewritten message_update
// is copied byte for byte. A rewritten one is decoded and re-encoded, so its
// keys come back in Go's map order (alphabetical) rather than Pi's emission
// order — but every surviving VALUE keeps its bytes, including `<`, `>` and `&`,
// which Go's default encoder would have turned into <, > and &.
// That escaping is not cosmetic: it hit 122 of the 4,139 delta lines on the run
// above, and it would leave the file disagreeing with itself, so a person
// grepping it for the model's own text would find the pass-through lines and
// miss the rewritten ones.
//
// Only the DISK copy changes. The daemon tees, so the bytes the session-id
// interceptor and the spawn watcher read are the child's original bytes; this
// writer sits on the tee's sink side and can see none of them.
//
// Bead: hk-k4jrh. Capture path: internal/daemon/agentlaunch.go (hk-j6wm7).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// piMessageUpdateType is the "type" discriminator of the only Pi NDJSON event
// that carries an accumulated snapshot of the assistant message.
const piMessageUpdateType = "message_update"

// The three keys the rewrite navigates by. piAccumulatedSnapshotKey and
// piTopLevelAccumulationKey are the two the writer REMOVES: both hold the whole
// assistant message accumulated so far, and on the run measured for hk-k4jrh
// they held the same value on 4,156 of 4,157 lines (the odd one out differed
// only in `usage`, which the harness reads from message_start/message_end
// instead).
//
// Dropping one alone still leaves the file quadratic — it only halves the
// constant — so both go.
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

// piStdoutLogLine returns the form of one NDJSON line to persist, including its
// trailing newline if the input had one.
//
// It is total: a line that is not a JSON object, or is a JSON object whose
// "type" is not message_update, comes back byte for byte. Only a well-formed
// message_update line is rewritten, and only by removing the two accumulated
// snapshots. Key order inside a rewritten line is Go's map order (alphabetical),
// not Pi's emission order; every surviving value keeps its exact bytes.
func piStdoutLogLine(line []byte) []byte {
	// Fast path, and the reason this function is cheap on the lines that are not
	// the problem: only a line that mentions the discriminator at all is worth
	// decoding. A tool result that quotes the string reaches the decode below and
	// is returned unchanged there, so this is a filter and not the decision.
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

// piEncodeJSONLine encodes v as one line of compact JSON, leaving `<`, `>` and
// `&` as the model wrote them.
//
// json.Marshal cannot do this. It runs with escapeHTML on and no way to turn it
// off, which rewrites those three characters to <, > and & —
// inside json.RawMessage values as well, so even a field this code never
// touches comes back altered. json.Encoder is the only encoder in the standard
// library that exposes the switch. It appends a newline of its own, which the
// caller supplies from the original line instead, so it is trimmed here.
func piEncodeJSONLine(v any) ([]byte, error) {
	var out bytes.Buffer
	enc := json.NewEncoder(&out)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("pi stdout log: encode line: %w", err)
	}
	return bytes.TrimRight(out.Bytes(), "\n"), nil
}
