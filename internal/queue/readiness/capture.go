package readiness

// capture.go — the thin shell around the pure record in snapshot.go.
//
// Two effects live here and nothing else: reading the live bead ledger through
// a read-only port, and scanning the terminal-intent directory. Both hand plain
// values to the pure constructor in snapshot.go, which does the judging.
//
// Spec ref: specs/beads-integration.md §4.5b BI-013e; §4.5a BI-013b (a
// submit-time bead read is `br show` and never mutates).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// BeadReader is the ledger surface a readiness capture needs, and all of it.
//
// It is a consumer-owned port: this package declares the smallest interface it
// can use, and the caller supplies something that happens to satisfy it — in
// production a *brcli.Adapter, in tests a recorder. The package never learns
// that `br` exists.
//
// It carries the read-only guarantee of BI-013e. There is no write method here,
// so no ordinary call through this port can close, create, or reopen a bead.
//
// That promise is strong but not total, and the difference matters. Go lets a
// caller recover a wider interface from a narrower one with a type assertion,
// and that compiles — so the compiler stops the easy mistake, not a determined
// one. Two tests cover what it cannot:
// TestBeadReaderExposesNoWriteMethod fails if this port grows a write method,
// and TestCapture_MakesNoLedgerCallThatCouldChangeABead fails if a capture
// widens the port it was handed and writes through it.
type BeadReader interface {
	ShowBead(ctx context.Context, id core.BeadID) (core.BeadRecord, error)
}

// CaptureRequest is the caller's side of a capture: which beads to read, and
// every piece of evidence that does not come from the ledger.
//
// The ledger data is deliberately absent. A caller cannot hand in a bead record
// it assembled itself — [Capture] reads every one of them live, which is what
// makes the snapshot's status field evidence rather than a claim.
type CaptureRequest struct {
	CapturedAt time.Time

	// SelectedID is the canary item. ExcludedIDs are every other bead that was
	// considered, each with the reason it was set aside.
	SelectedID       core.BeadID
	RepeatSafeReason string
	Posture          Posture
	ExcludedIDs      []ExcludedID

	Commands        []Command
	EventLogPaths   []string
	TerminalIntent  TerminalIntent
	StaleFindings   []StaleFinding
	CurrentFindings []CurrentFinding
}

// ExcludedID names a considered-and-rejected bead and why it was rejected.
type ExcludedID struct {
	BeadID core.BeadID
	Reason string
}

// Capture reads every candidate from the live ledger and builds the snapshot.
//
// Reads happen in a fixed order — the selected bead first, then the excluded
// ones as given — so two captures of the same request produce the same record
// and the same sequence of ledger calls.
//
// A read failure fails the whole capture. A candidate whose status could not be
// read is not evidence, and a snapshot that quietly dropped it would report a
// smaller candidate set than the one that was actually considered.
func Capture(ctx context.Context, ledger BeadReader, req CaptureRequest) (Snapshot, error) {
	if req.SelectedID == "" {
		return Snapshot{}, ErrNoSelectedItem
	}

	selected, err := readCandidate(ctx, ledger, req.SelectedID)
	if err != nil {
		return Snapshot{}, err
	}

	excluded := make([]Exclusion, 0, len(req.ExcludedIDs))
	for _, ex := range req.ExcludedIDs {
		cand, readErr := readCandidate(ctx, ledger, ex.BeadID)
		if readErr != nil {
			return Snapshot{}, readErr
		}
		excluded = append(excluded, Exclusion{Candidate: cand, Reason: ex.Reason})
	}

	return newSnapshot(request{
		CapturedAt: req.CapturedAt,
		Selection: Selection{
			Candidate:        selected,
			RepeatSafeReason: req.RepeatSafeReason,
			Posture:          req.Posture,
		},
		Excluded:        excluded,
		Commands:        req.Commands,
		EventLogPaths:   req.EventLogPaths,
		TerminalIntent:  req.TerminalIntent,
		StaleFindings:   req.StaleFindings,
		CurrentFindings: req.CurrentFindings,
	})
}

// readCandidate turns one live ledger record into a [Candidate].
func readCandidate(ctx context.Context, ledger BeadReader, id core.BeadID) (Candidate, error) {
	rec, err := ledger.ShowBead(ctx, id)
	if err != nil {
		return Candidate{}, fmt.Errorf("readiness: read candidate %s: %w", id, err)
	}
	return candidateFrom(rec), nil
}

// TerminalIntentDirName is the directory the Beads adapter writes a pending
// terminal-write record into, relative to the project's .harmonik directory.
// See specs/beads-integration.md §6.2 and core.IntentLogEntry.
const TerminalIntentDirName = "beads-intents"

// ReadTerminalIntents inspects the terminal-intent log under harmonikDir and
// reports what is pending.
//
// An absent directory is not an error. It means the adapter has never had an
// ambiguous write here, which is the ordinary state and is itself the finding.
// The returned Dir is always set, so the snapshot records where the inspection
// looked even when it found nothing.
//
// Entries come back sorted by idempotency key, so the record is stable across
// captures and a diff of two snapshots shows a real change.
func ReadTerminalIntents(harmonikDir string) (TerminalIntent, error) {
	dir := filepath.Join(harmonikDir, TerminalIntentDirName)
	result := TerminalIntent{Dir: dir}

	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return result, nil
	}
	if err != nil {
		return TerminalIntent{}, fmt.Errorf("readiness: read terminal-intent dir %q: %w", dir, err)
	}

	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		entry, readErr := core.ReadIntentLogEntry(filepath.Join(dir, e.Name()))
		if readErr != nil {
			return TerminalIntent{}, fmt.Errorf("readiness: read terminal intent %q: %w", e.Name(), readErr)
		}
		result.Pending = append(result.Pending, PendingIntent{
			IdempotencyKey: entry.IdempotencyKey,
			BeadID:         entry.BeadID,
			Op:             entry.Op,
			RequestedAt:    entry.RequestedAt,
		})
	}
	sort.Slice(result.Pending, func(i, j int) bool {
		return result.Pending[i].IdempotencyKey < result.Pending[j].IdempotencyKey
	})
	return result, nil
}

// WriteSnapshot writes s to path as indented JSON.
//
// The file is the artifact the assessor reads, so it is written whole and
// pretty-printed rather than as one line: a person has to be able to diff two
// of these and see which clause changed.
func WriteSnapshot(path string, s Snapshot) error {
	body, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("readiness: marshal snapshot: %w", err)
	}
	return writeJSONFile(path, append(body, '\n'))
}

// writeJSONFile writes body to path, creating the parent directory. Shared by
// the snapshot and the validation record so the two artifacts land the same way.
func writeJSONFile(path string, body []byte) (err error) {
	if mkErr := os.MkdirAll(filepath.Dir(path), core.HarmonikDirMode); mkErr != nil {
		return fmt.Errorf("readiness: mkdir for %q: %w", path, mkErr)
	}
	f, openErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644) //nolint:gosec // G304: path is an operator-supplied evidence destination, so it is a runtime value by construction and G304 fires however it is validated
	if openErr != nil {
		return fmt.Errorf("readiness: open %q: %w", path, openErr)
	}
	// A failed Close can mean the record never reached the disk, so it joins the
	// result rather than being dropped in favour of the write error.
	defer func() { err = errors.Join(err, f.Close()) }()

	if _, writeErr := f.Write(body); writeErr != nil {
		return fmt.Errorf("readiness: write %q: %w", path, writeErr)
	}
	return nil
}
