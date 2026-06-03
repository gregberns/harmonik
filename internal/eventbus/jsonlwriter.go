package eventbus

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"iter"
	"log"
	"os"
	"sync"

	"github.com/gregberns/harmonik/internal/core"
)

// writeRequest is a single unit of work sent to the drainer goroutine.
type writeRequest struct {
	buf    []byte
	doSync bool
	result chan<- error
}

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
// # Latency-concentration fix (hk-5zode)
//
// Prior to this revision the writer held a sync.Mutex across the entire
// Write+Sync call. Any goroutine calling Append(sync=true) blocked all other
// Append callers for the duration of the fsync(2) syscall — typically 1–10 ms
// on an SSD. Under N≥5 concurrent runs each emitting F-class events this
// caused P99 Emit latency to grow linearly with N (O(N × fsync_latency)).
//
// The fix uses a batching drainer goroutine:
//   - Append enqueues a writeRequest (buf + doSync + result channel) on an
//     internal buffered channel, then blocks waiting on the per-call result channel.
//   - The drainer goroutine dequeues the first available request, then
//     immediately drains any additional requests that have already arrived
//     (non-blocking select loop). All queued writes are written together.
//     If any queued request has doSync=true, a single fsync is issued for the
//     whole batch — fsync(2) is a barrier over all preceding writes.
//   - Each batched caller receives the same error result (nil or the write/sync
//     error).
//
// The improvement: at N=10 concurrent runs each emitting F-class events, bursts
// of simultaneous Appends are coalesced into one write+fsync per batch instead
// of N sequential fsyncs. P99 latency drops from O(N × fsync_latency) to
// O(1 × fsync_latency) for burst-concurrent callers.
//
// Correctness: batching does not violate EV-020 (no reordering, no truncation)
// because all writes in a batch are written to the file before the single fsync
// that covers them all. All line buffers are concatenated into one Write call
// to preserve POSIX O_APPEND atomicity within PIPE_BUF.
//
// The drainer is started by OpenJSONLWriter and stopped by Close. Append after
// Close returns [ErrWriterClosed].
//
// Spec ref: event-model.md §6.2 EV-020; §4.4 EV-015, EV-016; §6.1.
// Bead ref: hk-hqwn.29; hk-5zode (latency-concentration fix).
type JSONLWriter struct {
	queue chan writeRequest
	stop  chan struct{} // closed by Close to signal the drainer
	done  chan struct{} // closed by drainer when it exits

	// mu + isClosed guard the closed state so Append never sends on queue
	// after Close has been called. The invariant: once isClosed is true,
	// no new items enter queue, so the drainer can safely drain and exit.
	mu       sync.Mutex
	isClosed bool

	// closeOnce ensures Close is idempotent: a second call returns nil without
	// re-closing the stop channel (which would panic).
	closeOnce sync.Once
}

// ErrWriterClosed is returned by Append when called after Close.
var ErrWriterClosed = fmt.Errorf("jsonlwriter: writer is closed")

// OpenJSONLWriter opens (or creates) the JSONL file at path with
// O_CREATE|O_WRONLY|O_APPEND semantics and returns a JSONLWriter bound to it.
//
// The returned writer starts an internal batching drainer goroutine that
// coalesces concurrent writes and minimises fsync calls under load. The caller
// is responsible for calling Close when the writer is no longer needed (e.g.,
// on graceful daemon shutdown after Drain completes). Close signals the drainer
// to finish pending requests and exit.
//
// Spec ref: event-model.md §6.2 EV-020.
//
//nolint:gosec // G304: path is daemon-startup-resolved and validated by the caller; not user input.
func OpenJSONLWriter(path string) (*JSONLWriter, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}

	w := &JSONLWriter{
		// Buffer of 128 allows short bursts to enqueue without blocking the
		// drainer. At N=10 runs × 10 F-class/sec = 100 events/sec the channel
		// is consumed faster than it fills outside of startup bursts.
		queue: make(chan writeRequest, 128),
		stop:  make(chan struct{}),
		done:  make(chan struct{}),
	}
	go w.drain(f)
	return w, nil
}

// drain is the single writer goroutine. It owns f exclusively; no other
// goroutine touches f after drain starts.
//
// Batching algorithm:
//  1. Block on the first request from the queue, or exit when stop is closed
//     and the queue is empty.
//  2. Non-blocking drain: collect any additional requests already in the queue.
//  3. Concatenate all line buffers into one Write call.
//  4. If any request in the batch has doSync=true, issue one Sync.
//  5. Send the result (nil or error) to every request's result channel.
//
// This coalesces concurrent Append calls into a single Write+Sync per batch,
// reducing fsync calls from O(N) to O(1) for burst-concurrent callers.
func (w *JSONLWriter) drain(f *os.File) {
	defer func() {
		_ = f.Close()
		close(w.done)
	}()

	for {
		// Wait for the first request or a stop signal.
		var first writeRequest
		select {
		case req, ok := <-w.queue:
			if !ok {
				return
			}
			first = req
		case <-w.stop:
			// Drain any remaining items in the queue before exiting.
			for {
				select {
				case req, ok := <-w.queue:
					if !ok {
						return
					}
					w.processBatch(f, []writeRequest{req})
				default:
					return
				}
			}
		}

		// Collect any additional requests already waiting.
		batch := []writeRequest{first}
	drainLoop:
		for {
			select {
			case req, ok := <-w.queue:
				if !ok {
					// queue closed; process what we have
					break drainLoop
				}
				batch = append(batch, req)
			default:
				break drainLoop
			}
		}

		w.processBatch(f, batch)
	}
}

// processBatch writes and optionally syncs a batch of requests, then fans out
// the result to all callers in the batch.
func (w *JSONLWriter) processBatch(f *os.File, batch []writeRequest) {
	// Concatenate all payloads into a single buffer for one Write call.
	// This preserves O_APPEND atomicity: the combined buffer is submitted
	// as a single kernel write, keeping all lines for this batch contiguous.
	totalLen := 0
	needsSync := false
	for i := range batch {
		totalLen += len(batch[i].buf)
		if batch[i].doSync {
			needsSync = true
		}
	}

	combined := make([]byte, 0, totalLen)
	for i := range batch {
		combined = append(combined, batch[i].buf...)
	}

	var batchErr error
	if _, writeErr := f.Write(combined); writeErr != nil {
		batchErr = writeErr
	} else if needsSync {
		// One fsync covers all writes in the batch (fsync is a barrier
		// over all preceding writes to the fd, per POSIX).
		batchErr = f.Sync()
	}

	// Fan out the result to all callers in this batch.
	for i := range batch {
		batch[i].result <- batchErr
	}
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
// Append is safe for concurrent use. Multiple goroutines may call Append
// simultaneously; writes are serialised by the internal drainer goroutine.
// Concurrent F-class Appends are coalesced into a single Write+Sync per batch,
// reducing fsync overhead from O(N) to O(1) for burst-concurrent callers.
// See the type-level comment for the full latency analysis (hk-5zode).
//
// Returns [ErrWriterClosed] if called after Close. Returns a non-nil error if
// the write or (when sync=true) the sync fails. The caller MUST treat a write
// error as a fatal condition and escalate per the on-disk durability contract
// (EV-015).
//
// Spec ref: event-model.md §6.2 EV-020; §4.4 EV-015, EV-016.
func (w *JSONLWriter) Append(line []byte, sync bool) error {
	// Allocate a single buffer: line + newline. This ensures one write call
	// per event line, minimising torn-write window under POSIX O_APPEND semantics.
	buf := make([]byte, len(line)+1)
	copy(buf, line)
	buf[len(line)] = '\n'

	result := make(chan error, 1)
	req := writeRequest{
		buf:    buf,
		doSync: sync,
		result: result,
	}

	// Check isClosed under lock before enqueuing. This prevents sending on
	// queue after Close has been called (which would race with the drainer
	// exiting). The lock window is minimal — just the closed check and enqueue.
	w.mu.Lock()
	if w.isClosed {
		w.mu.Unlock()
		return ErrWriterClosed
	}
	w.queue <- req
	w.mu.Unlock()

	return <-result
}

// Close signals the drainer goroutine to stop accepting new requests, waits
// for it to finish processing any already-enqueued requests, then returns.
//
// Close is idempotent: a second (or subsequent) call is a no-op and returns
// nil without panicking. This protects callers that combine an explicit close
// with a deferred close (e.g. bus.Seal() + defer w.Close()).
//
// Calling Close concurrently with Append is safe: Append checks isClosed
// under mu and returns [ErrWriterClosed] rather than sending on the queue.
// The typical shutdown sequence (Drain all subscribers, then Close writer)
// is safe.
func (w *JSONLWriter) Close() error {
	w.mu.Lock()
	w.isClosed = true
	w.mu.Unlock()

	w.closeOnce.Do(func() {
		close(w.stop)
	})
	<-w.done
	return nil
}

// ScanAfter returns an iterator over all events in the JSONL file at path
// where event_id strictly follows sinceID (i.e. the event_id byte string is
// lexicographically greater than sinceID's byte string). Because EventID is a
// UUIDv7, lexicographic byte order is chronological order (EV-002).
//
// Events are yielded in file order. Malformed lines are skipped with a
// log.Printf warning. A missing file is treated as an empty log (no events
// yielded, no error) so callers do not need to special-case a fresh daemon.
//
// ScanAfter is a pure read-side function; it does NOT modify the file (EV-020).
//
// Spec ref: event-model.md §6.2 EV-020; EV-002 (UUIDv7 ordering).
// Bead ref: hk-a5sil (since_event_id replay).
//
//nolint:gosec // G304: path is daemon-startup-resolved; not user input.
func ScanAfter(path string, sinceID core.EventID) iter.Seq[core.Event] {
	return func(yield func(core.Event) bool) {
		f, err := os.Open(path)
		if err != nil {
			if !os.IsNotExist(err) {
				log.Printf("eventbus.ScanAfter: open %s: %v", path, err)
			}
			return
		}
		defer func() { _ = f.Close() }()

		since := [16]byte(sinceID)
		reader := bufio.NewReader(f)
		for {
			lineBytes, err := reader.ReadBytes('\n')
			if len(lineBytes) > 0 {
				// Trim the trailing newline before unmarshalling.
				lineBytes = bytes.TrimRight(lineBytes, "\n")
				var ev core.Event
				if decodeErr := json.Unmarshal(lineBytes, &ev); decodeErr != nil {
					log.Printf("eventbus.ScanAfter: malformed line in %s (skipping): %v", path, decodeErr)
				} else {
					// bytes.Compare on raw UUID bytes: lexicographic order matches
					// chronological order for UUIDv7 (EV-002). Skip events ≤ sinceID.
					evUID := [16]byte(ev.EventID)
					if bytes.Compare(evUID[:], since[:]) > 0 {
						if !yield(ev) {
							return
						}
					}
				}
			}
			if err == io.EOF {
				return
			}
			if err != nil {
				log.Printf("eventbus.ScanAfter: read %s: %v", path, err)
				return
			}
		}
	}
}

// Filter returns an iterator over the events in the JSONL file at path whose
// envelope run_id matches runID. Events are yielded in file order (append
// order). The file is read from the beginning each time Filter is called;
// Filter is a pure read-side operation and does NOT shard or modify the file
// (EV-020).
//
// Each line is decoded as a [core.Event] envelope. Lines whose run_id field
// matches runID are yielded to the caller. Lines that do not match are
// skipped silently. Lines that are malformed JSON (cannot be decoded as a
// core.Event envelope) are also skipped, with a log.Printf warning — the
// caller is not interrupted.
//
// The torn-tail rule (event-model.md §6.2): a final line that lacks a
// terminating newline is treated as malformed by the JSON decoder and is
// therefore skipped per the above malformed-line policy.
//
// Filter is a free function bound to a path rather than a method on
// JSONLWriter so that callers can read any JSONL file (including files written
// by a closed writer, or files under a different path) without holding a live
// writer reference.
//
// Spec ref: event-model.md §6.2 EV-020; POST_MVH_PARALLELISM_ROADMAP.md row 10.
// Bead ref: hk-e61c3.5.
//
//nolint:gosec // G304: path is caller-supplied and scoped to the harmonik project dir; not user input.
func Filter(path string, runID core.RunID) iter.Seq[core.Event] {
	return func(yield func(core.Event) bool) {
		f, err := os.Open(path)
		if err != nil {
			log.Printf("eventbus.Filter: open %s: %v", path, err)
			return
		}
		defer func() { _ = f.Close() }()

		reader := bufio.NewReader(f)
		for {
			lineBytes, err := reader.ReadBytes('\n')
			if len(lineBytes) > 0 {
				lineBytes = bytes.TrimRight(lineBytes, "\n")
				// Decode just the envelope fields needed for matching.
				var ev core.Event
				if decodeErr := json.Unmarshal(lineBytes, &ev); decodeErr != nil {
					log.Printf("eventbus.Filter: malformed line in %s (skipping): %v", path, decodeErr)
				} else if ev.RunID != nil && *ev.RunID == runID {
					if !yield(ev) {
						return
					}
				}
			}
			if err == io.EOF {
				return
			}
			if err != nil {
				log.Printf("eventbus.Filter: read %s: %v", path, err)
				return
			}
		}
	}
}
