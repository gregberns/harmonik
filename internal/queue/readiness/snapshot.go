package readiness

// snapshot.go — the pure half of the readiness evidence record.
//
// Nothing here touches the world. Every function takes values and returns
// values, the capture time arrives as an argument rather than from the clock,
// and the only failure mode is a refusal carried in the return type.
//
// Spec ref: specs/beads-integration.md §4.5b BI-013e.

import (
	"errors"
	"fmt"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// SchemaVersion is the schema version stamped into every snapshot.
//
// It is modelled on the N-1 readability contract of specs/operator-nfr.md §4.5
// ON-018 — a reader at version N-1 parses a record written at version N and
// treats fields it does not know as unknown but not fatal. ON-018 does not name
// this artifact, so the contract here is by construction rather than by
// citation. Adding a field is non-breaking. Renaming or removing one is
// breaking and must raise this number.
const SchemaVersion = 1

// EventEvidenceNote is written into every snapshot by [Capture]. A caller
// cannot supply, edit, or suppress it.
//
// specs/beads-integration.md §4.7 BI-023 makes the JSONL event log
// observational only. It is evidence about what happened, and it never drives a
// write back to the ledger. The note exists because the snapshot outlives the
// session that captured it, and the next reader is the assessor, who has no
// other way to know which of the three stores this file is allowed to speak
// for.
const EventEvidenceNote = "JSONL is observational only; it never drives a ledger write (BI-023)."

// Refusals returned by [Capture]. Each one names a clause of BI-013e that the
// request did not satisfy. They are sentinels so a caller can branch on the
// reason rather than matching on message text.
var (
	ErrNoCaptureTime            = errors.New("readiness: snapshot has no capture time")
	ErrNoCommands               = errors.New("readiness: snapshot records no commands")
	ErrNoSelectedItem           = errors.New("readiness: snapshot names no selected item")
	ErrSelectedItemNotOpen      = errors.New("readiness: selected item is not open")
	ErrNoRepeatSafeReason       = errors.New("readiness: selected item has no repeat-safe reason")
	ErrPostureNotOneLocalRun    = errors.New("readiness: posture is not one local stream run")
	ErrExclusionWithoutBead     = errors.New("readiness: excluded entry names no bead")
	ErrExclusionWithoutReason   = errors.New("readiness: excluded candidate has no exclusion reason")
	ErrDuplicateCandidate       = errors.New("readiness: candidate appears twice in the candidate set")
	ErrNoEventLogPath           = errors.New("readiness: snapshot names no event-log file")
	ErrNoTerminalIntentDir      = errors.New("readiness: snapshot names no terminal-intent directory")
	ErrStaleFindingIncomplete   = errors.New("readiness: stale finding does not name its ID, checked source, fixing commit, focused proof, and disposition")
	ErrCurrentFindingIncomplete = errors.New("readiness: current finding does not name its scoped record, current source evidence, and source path")

	// Refusals raised when a snapshot is read back from a file rather than built.
	ErrSnapshotSchemaUnreadable = errors.New("readiness: snapshot on disk states a schema version this build cannot read")
	ErrSnapshotNoteAltered      = errors.New("readiness: snapshot on disk does not carry the observational note it was written with")
)

// Candidate is the ledger data for one bead that was considered for the canary
// run.
//
// Every field is copied from a live `br show` read by [Capture]. BI-013e
// forbids a caller supplying a status, which is why the only constructor for
// this type is unexported and takes a [core.BeadRecord]: there is no exported
// path that reaches these fields with a value nobody read.
type Candidate struct {
	BeadID core.BeadID       `json:"bead_id"`
	Title  string            `json:"title"`
	Status core.CoarseStatus `json:"status"`
	Labels []string          `json:"labels,omitempty"`
}

// candidateFrom is the only way a Candidate is built from outside a decoder.
func candidateFrom(rec core.BeadRecord) Candidate {
	return Candidate{
		BeadID: rec.BeadID,
		Title:  rec.Title,
		Status: rec.Status,
		Labels: rec.Labels,
	}
}

// Posture is the run shape the selected item was judged against.
//
// BI-013e requires the selected item to be "suitable for one local stream run",
// which is the three fields below. The wider rejection matrix — Pi, remote
// worker, cross-repository target, wave queue, feedback use — belongs to the
// validator that reads this record, not to the record itself.
type Posture struct {
	Local       bool `json:"local"`
	ItemCount   int  `json:"item_count"`
	Concurrency int  `json:"concurrency"`
}

// oneLocalStreamRun reports whether p is the single local run BI-013e allows.
func (p Posture) oneLocalStreamRun() bool {
	return p.Local && p.ItemCount == 1 && p.Concurrency == 1
}

// Selection is the one candidate chosen for the canary, with the reason it is
// safe to run more than once and the posture it was judged against.
//
// There is exactly one selection per snapshot because it is a field and not a
// list. A record that selected two items, or none, cannot be built.
type Selection struct {
	Candidate        Candidate `json:"candidate"`
	RepeatSafeReason string    `json:"repeat_safe_reason"`
	Posture          Posture   `json:"posture"`
}

// Exclusion is a candidate that was considered and set aside, with the reason.
// Both halves are required: a reason that names no bead, and a bead with no
// reason, are each half of a record and neither is evidence.
type Exclusion struct {
	Candidate Candidate `json:"candidate"`
	Reason    string    `json:"reason"`
}

// Command is one command that produced part of this evidence, with the file its
// output was retained in. A command whose output was not retained leaves
// OutputPath empty and is still recorded, because knowing a command was run is
// worth more than the silence.
type Command struct {
	Argv       []string `json:"argv"`
	OutputPath string   `json:"output_path,omitempty"`
}

// EventEvidence names the event logs this capture read and restates what those
// logs are allowed to prove.
//
// LogPaths is a list because events.jsonl rotates: a capture that spans a
// rotation reads the live file and its predecessor, and a record that could
// name only one of them would have to drop the other silently.
type EventEvidence struct {
	LogPaths []string `json:"log_paths"`
	Note     string   `json:"note"`
}

// PendingIntent is one surviving entry in the terminal-intent log: a Beads
// terminal write whose completion is ambiguous.
type PendingIntent struct {
	IdempotencyKey string          `json:"idempotency_key"`
	BeadID         core.BeadID     `json:"bead_id"`
	Op             core.TerminalOp `json:"op"`
	RequestedAt    time.Time       `json:"requested_at"`
}

// TerminalIntent is the result of the terminal-intent inspection BI-013e
// requires.
//
// Dir is required and Pending may be empty, and that asymmetry is the point: an
// empty Pending list with a named Dir says "we looked here and found nothing",
// which is a finding. An absent Dir says nothing at all.
type TerminalIntent struct {
	Dir     string          `json:"dir"`
	Pending []PendingIntent `json:"pending"`
}

// StaleFinding is an old graph finding that current source has already fixed.
//
// Closing one needs a commit and a test, not a judgement, so all five fields are
// required. The shape mirrors the triage table in
// .kerf/works/queue-dogfood-readiness/08-stale-graph-triage.md.
type StaleFinding struct {
	FindingID     string `json:"finding_id"`
	CheckedSource string `json:"checked_source"`
	FixingCommit  string `json:"fixing_commit"`
	FocusedProof  string `json:"focused_proof"`
	Disposition   string `json:"disposition"`
}

// CurrentFinding is a condition that current source still has. BI-013e requires
// it to become a new scoped open record with current source evidence, so the
// snapshot records which record, which evidence, and where in the source.
//
// Source is required for the same reason [StaleFinding.CheckedSource] is: a
// finding that names no source path cannot be re-checked by the next reader.
//
// This is a separate type from [StaleFinding] and lives in a separate list, so
// the two cannot be conflated by forgetting to set a flag.
type CurrentFinding struct {
	RecordID string `json:"record_id"`
	Evidence string `json:"evidence"`
	Source   string `json:"source"`
}

// Snapshot is the retained readiness evidence record.
//
// A value of this type came out of [Capture], so every clause of BI-013e it can
// express is already satisfied. Readers do not re-check it.
type Snapshot struct {
	SchemaVersion   int              `json:"schema_version"`
	CapturedAt      time.Time        `json:"captured_at"`
	Selection       Selection        `json:"selection"`
	Excluded        []Exclusion      `json:"excluded"`
	Commands        []Command        `json:"commands"`
	Events          EventEvidence    `json:"events"`
	TerminalIntent  TerminalIntent   `json:"terminal_intent"`
	StaleFindings   []StaleFinding   `json:"stale_findings"`
	CurrentFindings []CurrentFinding `json:"current_findings"`
}

// CandidateSet returns every bead this capture considered: the selected one
// first, then each excluded one in the order the request gave them.
func (s Snapshot) CandidateSet() []Candidate {
	set := make([]Candidate, 0, len(s.Excluded)+1)
	set = append(set, s.Selection.Candidate)
	for _, e := range s.Excluded {
		set = append(set, e.Candidate)
	}
	return set
}

// OutputPaths returns the retained output file of every recorded command, in
// command order, skipping commands whose output was not retained.
func (s Snapshot) OutputPaths() []string {
	paths := make([]string, 0, len(s.Commands))
	for _, c := range s.Commands {
		if c.OutputPath != "" {
			paths = append(paths, c.OutputPath)
		}
	}
	return paths
}

// request is the unchecked input to [newSnapshot].
//
// It is deliberately a separate type from [Snapshot]. The request is what a
// caller assembled and may be wrong in any of a dozen ways; the snapshot is
// what survived the check. Keeping them apart is what stops a half-filled
// record from being passed around as if it had been verified.
//
// Both this type and [newSnapshot] are unexported on purpose. BI-013e says the
// caller must not supply a candidate status, and an exported constructor taking
// a caller-built [Candidate] would be exactly that hole: it would produce a
// record indistinguishable from one [Capture] read live. The only way in from
// outside this package is [Capture], which reads every status itself.
type request struct {
	CapturedAt      time.Time
	Selection       Selection
	Excluded        []Exclusion
	Commands        []Command
	EventLogPaths   []string
	TerminalIntent  TerminalIntent
	StaleFindings   []StaleFinding
	CurrentFindings []CurrentFinding
}

// newSnapshot checks req against BI-013e and returns the retained record.
//
// It refuses rather than repairing: a request that does not say why the
// selected item is safe to re-run is not a request with a missing field, it is
// a selection nobody justified.
//
// The returned snapshot's event note is always [EventEvidenceNote].
func newSnapshot(req request) (Snapshot, error) {
	if req.CapturedAt.IsZero() {
		return Snapshot{}, ErrNoCaptureTime
	}
	if len(req.Commands) == 0 {
		return Snapshot{}, ErrNoCommands
	}
	if err := checkSelection(req.Selection); err != nil {
		return Snapshot{}, err
	}
	if err := checkCandidateSet(req.Selection.Candidate, req.Excluded); err != nil {
		return Snapshot{}, err
	}
	if len(req.EventLogPaths) == 0 {
		return Snapshot{}, ErrNoEventLogPath
	}
	if req.TerminalIntent.Dir == "" {
		return Snapshot{}, ErrNoTerminalIntentDir
	}
	if err := checkFindings(req.StaleFindings, req.CurrentFindings); err != nil {
		return Snapshot{}, err
	}

	return Snapshot{
		SchemaVersion: SchemaVersion,
		CapturedAt:    req.CapturedAt.UTC(),
		Selection:     req.Selection,
		Excluded:      req.Excluded,
		Commands:      req.Commands,
		Events: EventEvidence{
			LogPaths: req.EventLogPaths,
			Note:     EventEvidenceNote,
		},
		TerminalIntent:  req.TerminalIntent,
		StaleFindings:   req.StaleFindings,
		CurrentFindings: req.CurrentFindings,
	}, nil
}

// checkSelection enforces the three things BI-013e says of the selected item:
// it is open, it is repeat-safe with a stated reason, and it is suitable for
// one local stream run.
func checkSelection(sel Selection) error {
	if sel.Candidate.BeadID == "" {
		return ErrNoSelectedItem
	}
	if sel.Candidate.Status != core.CoarseStatusOpen {
		return fmt.Errorf("%w: %s is %s", ErrSelectedItemNotOpen, sel.Candidate.BeadID, sel.Candidate.Status)
	}
	if sel.RepeatSafeReason == "" {
		return fmt.Errorf("%w: %s", ErrNoRepeatSafeReason, sel.Candidate.BeadID)
	}
	if !sel.Posture.oneLocalStreamRun() {
		return fmt.Errorf("%w: local=%t items=%d concurrency=%d",
			ErrPostureNotOneLocalRun, sel.Posture.Local, sel.Posture.ItemCount, sel.Posture.Concurrency)
	}
	return nil
}

// checkCandidateSet enforces that every excluded entry names a bead and a
// reason, and that no bead appears twice across the whole set. A bead listed as
// both selected and excluded is a capture that contradicts itself.
func checkCandidateSet(selected Candidate, excluded []Exclusion) error {
	seen := map[core.BeadID]struct{}{selected.BeadID: {}}
	for _, e := range excluded {
		if e.Candidate.BeadID == "" {
			return fmt.Errorf("%w: reason %q", ErrExclusionWithoutBead, e.Reason)
		}
		if e.Reason == "" {
			return fmt.Errorf("%w: %s", ErrExclusionWithoutReason, e.Candidate.BeadID)
		}
		if _, dup := seen[e.Candidate.BeadID]; dup {
			return fmt.Errorf("%w: %s", ErrDuplicateCandidate, e.Candidate.BeadID)
		}
		seen[e.Candidate.BeadID] = struct{}{}
	}
	return nil
}

// checkFindings enforces the evidence standard on both finding lists. The two
// lists are checked separately because they carry different obligations: a
// stale finding must name what fixed it, a current one must name where it now
// lives.
func checkFindings(stale []StaleFinding, current []CurrentFinding) error {
	for _, f := range stale {
		if f.FindingID == "" || f.CheckedSource == "" || f.FixingCommit == "" ||
			f.FocusedProof == "" || f.Disposition == "" {
			return fmt.Errorf("%w: %q", ErrStaleFindingIncomplete, f.FindingID)
		}
	}
	for _, f := range current {
		if f.RecordID == "" || f.Evidence == "" || f.Source == "" {
			return fmt.Errorf("%w: %q", ErrCurrentFindingIncomplete, f.RecordID)
		}
	}
	return nil
}
