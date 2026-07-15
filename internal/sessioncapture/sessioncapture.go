// Package sessioncapture is the CONSUMER side of the structured-driver capture
// tee (agent-input-substrate T7). The tee itself (internal/apptap) owns no file
// format and opens no file (RS-016 / AIS-013): it splices caller-owned pipes
// and tees a byte-identical copy to a caller-supplied writer, best-effort. This
// package is that writer — everything the tee deliberately does NOT do lives
// here:
//
//   - Persistence: the captured stream is written to disk at
//     ${workspace_path}/.harmonik/sessions/${session_id}/ (WM §4.7), one file
//     per direction (wire-in.jsonl / wire-out.jsonl).
//   - Redaction: the in-memory tee is verbatim; the persisting writer for the
//     OUTPUT direction applies an HC-032-style value-pattern scrub before the
//     bytes land on disk. INPUT is structurally secret-free (HC-028) and is
//     persisted verbatim.
//   - Retention: a keep-N / age-prune rule over the sibling session directories
//     (the brhistoryrotate / orphansweep precedents) so the corpus never grows
//     unbounded — it MUST NOT inherit the unrotated large-events.jsonl defect.
//   - A mechanical CAPTURE-LOG ledger, appended per capture session (the
//     keeper EXTRACT-LOG.md is the executed model; a declared-but-never-written
//     ledger is the anti-pattern AIS-014 calls out).
//
// AIS-INV-002 (capture never aborts the run) is enforced at the tee (apptap's
// best-effort splice), not here: a Write returning an error from this package
// is swallowed by the apptap degrading writer and degrades capture to
// uncaptured. This package therefore MAY return write errors honestly (e.g. a
// full disk) without endangering the live agent stream.
//
// Spec refs: specs/agent-input.md AIS-013/AIS-014, AIS-INV-002;
// specs/handler-contract.md HC-028/HC-032.
package sessioncapture

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gregberns/harmonik/internal/substrate"
)

const (
	// wireInFile / wireOutFile are the per-direction corpus files (WM §4.7).
	wireInFile  = "wire-in.jsonl"
	wireOutFile = "wire-out.jsonl"

	// captureLogFile is the mechanical ledger, appended once per capture session
	// at the sessions-root level (sibling to every ${session_id}/ dir).
	captureLogFile = "CAPTURE-LOG.md"

	// dirPerm / filePerm mirror the .harmonik dir conventions (brhistoryrotate).
	dirPerm  = 0o755
	filePerm = 0o644

	// defaultKeepN is the number of most-recent session dirs to retain when
	// Config.KeepN is unset. Bounds the corpus without discarding recent runs.
	defaultKeepN = 20
)

// Config configures a capture Session. WorkspacePath and SessionID are required;
// the rest have safe defaults.
type Config struct {
	// WorkspacePath is the run's workspace root; the corpus lands under
	// ${WorkspacePath}/.harmonik/sessions/.
	WorkspacePath string
	// SessionID names this session's corpus directory.
	SessionID string
	// Clock supplies time for the ledger timestamp and the age-prune cutoff
	// (RS-015); no wall-clock. Default: substrate.SystemClock.
	Clock substrate.ClockPort
	// KeepN is the retention keep-count: the N most-recent session dirs survive.
	// Default: defaultKeepN. A non-positive value after defaulting disables the
	// keep-count arm (age-prune still applies).
	KeepN int
	// MaxAge, when > 0, additionally prunes any session dir older than MaxAge
	// (by mtime, measured against Clock.Now). Zero disables the age arm.
	MaxAge time.Duration
}

// Session is one capture session's consumer state: the two per-direction
// persisting writers plus the corpus directory. Construct with Open; release
// with Close. Not intended for concurrent Open/Close, but the two direction
// writers it hands out are each single-goroutine-owned by their apptap tee.
type Session struct {
	dir string

	inW  *persistWriter
	outW *persistWriter
}

// Open creates the session corpus directory, runs retention over the sibling
// session dirs, appends the mechanical CAPTURE-LOG entry, and opens the two
// per-direction persisting writers. A non-nil error means the corpus could not
// be established; the caller (composition root) treats capture as unavailable
// and proceeds uncaptured (AIS-INV-002 — capture is never load-bearing).
func Open(ctx context.Context, cfg Config) (*Session, error) {
	if cfg.WorkspacePath == "" || cfg.SessionID == "" {
		return nil, fmt.Errorf("sessioncapture: WorkspacePath and SessionID are required")
	}
	clk := cfg.Clock
	if clk == nil {
		clk = substrate.SystemClock{}
	}
	keepN := cfg.KeepN
	if keepN == 0 {
		keepN = defaultKeepN
	}

	root := filepath.Join(cfg.WorkspacePath, ".harmonik", "sessions")
	dir := filepath.Join(root, cfg.SessionID)
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return nil, fmt.Errorf("sessioncapture: mkdir corpus: %w", err)
	}

	// Retention: prune the sibling session dirs down to the keep-N most recent
	// and drop any older than MaxAge. Best-effort — a prune error must not fail
	// the capture (mirrors brhistoryrotate's always-non-fatal discipline).
	pruneSessions(ctx, root, keepN, cfg.MaxAge, clk)

	// Mechanical CAPTURE-LOG: append one ledger line per capture session.
	appendCaptureLog(ctx, root, cfg.SessionID, clk.Now())

	inW, err := newPersistWriter(filepath.Join(dir, wireInFile), directionInput)
	if err != nil {
		return nil, fmt.Errorf("sessioncapture: open input corpus: %w", err)
	}
	outW, err := newPersistWriter(filepath.Join(dir, wireOutFile), directionOutput)
	if err != nil {
		return nil, errors.Join(fmt.Errorf("sessioncapture: open output corpus: %w", err), inW.Close())
	}

	return &Session{dir: dir, inW: inW, outW: outW}, nil
}

// Dir returns the corpus directory for this session.
func (s *Session) Dir() string { return s.dir }

// Input returns the persisting writer for the INPUT (caller→child) direction.
// INPUT is structurally secret-free (HC-028): it is persisted verbatim.
func (s *Session) Input() *persistWriter { return s.inW }

// Output returns the persisting writer for the OUTPUT (child→caller) direction.
// OUTPUT is the secret risk (HC-032): the writer applies a value-pattern scrub
// before bytes land on disk.
func (s *Session) Output() *persistWriter { return s.outW }

// Close flushes and closes both direction writers. Errors are joined.
func (s *Session) Close() error {
	var errs []error
	if err := s.inW.Close(); err != nil {
		errs = append(errs, err)
	}
	if err := s.outW.Close(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("sessioncapture: close: %v", errs)
	}
	return nil
}

// direction distinguishes the verbatim INPUT sink from the scrubbing OUTPUT
// sink (AIS-014 / HC-028 / HC-032).
type direction int

const (
	directionInput direction = iota
	directionOutput
)

// persistWriter persists one direction's captured bytes to a corpus file. For
// the OUTPUT direction it applies the HC-032 value-pattern scrub on a
// per-line boundary (the wire is NDJSON) before writing; a partial trailing
// line is held until its terminating newline arrives or Close flushes it. For
// the INPUT direction bytes pass straight through, verbatim (HC-028).
//
// A Write error (e.g. disk full) is returned honestly to the caller — the
// apptap best-effort tee upstream swallows it and degrades to uncaptured
// (AIS-INV-002). This writer therefore never has to fake success itself.
type persistWriter struct {
	mu   sync.Mutex
	f    *os.File
	dir  direction
	tail []byte // buffered partial trailing line (OUTPUT only)
}

func newPersistWriter(path string, dir direction) (*persistWriter, error) {
	//nolint:gosec // G304: path derived from operator-supplied workspace + session id, not remote input.
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, filePerm)
	if err != nil {
		return nil, err
	}
	return &persistWriter{f: f, dir: dir}, nil
}

// Write persists p. INPUT is verbatim; OUTPUT is scrubbed line-by-line.
//
// It always reports len(p) written on success so the upstream tee sees a full
// write; a scrubbed OUTPUT line may differ in byte length from the input, but
// the contract with the tee is "these bytes were consumed", not "these exact
// bytes hit disk" (redaction is the whole point of the OUTPUT sink).
func (p *persistWriter) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.f == nil {
		return len(b), nil // closed; drop silently (degraded).
	}
	if p.dir == directionInput {
		if _, err := p.f.Write(b); err != nil {
			return 0, err
		}
		return len(b), nil
	}
	// OUTPUT: scrub on line boundaries so a value-pattern match is never split
	// across two Writes. Accumulate into tail; emit each complete (newline-
	// terminated) line scrubbed.
	p.tail = append(p.tail, b...)
	for {
		i := indexNewline(p.tail)
		if i < 0 {
			break
		}
		line := p.tail[:i+1] // include the newline
		if _, err := p.f.Write(scrubLine(line)); err != nil {
			return 0, err
		}
		p.tail = p.tail[i+1:]
	}
	return len(b), nil
}

// Close flushes any buffered partial OUTPUT line (scrubbed) and closes the file.
func (p *persistWriter) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.f == nil {
		return nil
	}
	var flushErr error
	if p.dir == directionOutput && len(p.tail) > 0 {
		if _, err := p.f.Write(scrubLine(p.tail)); err != nil {
			flushErr = err
		}
		p.tail = nil
	}
	closeErr := p.f.Close()
	p.f = nil
	if flushErr != nil {
		return flushErr
	}
	return closeErr
}

func indexNewline(b []byte) int {
	for i, c := range b {
		if c == '\n' {
			return i
		}
	}
	return -1
}
