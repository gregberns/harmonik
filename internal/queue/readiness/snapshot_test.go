package readiness

// snapshot_test.go — the claims the pure record makes.
//
// Every refusal test below was watched failing before it was kept: the
// "refuses" cases were first run against a request that satisfied the clause,
// and each one went green, which is what proves the assertion is reading the
// field it names.

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
)

// capturedAt is a fixed instant. The record takes its time as an argument, so
// no test here needs a clock.
var capturedAt = time.Date(2026, 8, 4, 17, 30, 0, 0, time.UTC)

// wholeRequest is a request that satisfies every clause of BI-013e. Each
// refusal test starts from it and breaks exactly one thing, so a failure names
// the clause and not the fixture.
func wholeRequest() request {
	return request{
		CapturedAt: capturedAt,
		Selection: Selection{
			Candidate: Candidate{
				BeadID: "hk-canary",
				Title:  "correct one comment in the scratch clone",
				Status: core.CoarseStatusOpen,
				Labels: []string{"codename:queue-dogfood-readiness"},
			},
			RepeatSafeReason: "single-file comment edit; re-running it reaches the same tree",
			Posture:          Posture{Local: true, ItemCount: 1, Concurrency: 1},
		},
		Excluded: []Exclusion{
			{
				Candidate: Candidate{BeadID: "hk-remote", Title: "remote worker probe", Status: core.CoarseStatusOpen},
				Reason:    "needs a remote worker; pass one is local only",
			},
		},
		Commands: []Command{
			{Argv: []string{"br", "show", "hk-canary", "--format", "json"}, OutputPath: "evidence/hk-canary.json"},
			{Argv: []string{"git", "rev-parse", "HEAD"}},
		},
		EventLogPaths:  []string{".harmonik/events/events.jsonl"},
		TerminalIntent: TerminalIntent{Dir: ".harmonik/beads-intents"},
		StaleFindings: []StaleFinding{{
			FindingID:     "hk-v4wer",
			CheckedSource: "internal/daemon/dot_cascade_helpers.go",
			FixingCommit:  "27c7a5fb8",
			FocusedProof:  "TestDotNode_FailureSignalAfterACommitFailsTheNode",
			Disposition:   "stale; current source classifies the outcome before a node can succeed",
		}},
		CurrentFindings: []CurrentFinding{{
			RecordID: "hk-still-open",
			Evidence: "the sweep still globs the wrong archive layout",
			Source:   "internal/lifecycle/queuearchivesweep.go",
		}},
	}
}

func TestNewSnapshot_RecordsEveryFieldTheReadinessRequirementNames(t *testing.T) {
	got, err := newSnapshot(wholeRequest())
	if err != nil {
		t.Fatalf("newSnapshot on a whole request: %v", err)
	}

	if got.SchemaVersion != SchemaVersion {
		t.Errorf("schema version = %d, want %d", got.SchemaVersion, SchemaVersion)
	}
	if !got.CapturedAt.Equal(capturedAt) {
		t.Errorf("capture time = %s, want %s", got.CapturedAt, capturedAt)
	}
	if got.Selection.Candidate.BeadID != "hk-canary" {
		t.Errorf("selected item = %q, want hk-canary", got.Selection.Candidate.BeadID)
	}
	if got.Selection.RepeatSafeReason == "" {
		t.Error("selected item kept no repeat-safe reason")
	}
	if len(got.Excluded) != 1 || got.Excluded[0].Reason == "" {
		t.Errorf("exclusions = %+v, want one with a reason", got.Excluded)
	}
	if len(got.Commands) != 2 {
		t.Errorf("commands = %d, want 2", len(got.Commands))
	}
	if len(got.Events.LogPaths) != 1 || got.Events.LogPaths[0] != ".harmonik/events/events.jsonl" {
		t.Errorf("event log paths = %q", got.Events.LogPaths)
	}
	if got.TerminalIntent.Dir == "" {
		t.Error("terminal-intent inspection recorded no directory")
	}
	if len(got.StaleFindings) != 1 {
		t.Errorf("stale findings = %d, want 1", len(got.StaleFindings))
	}
	if len(got.CurrentFindings) != 1 {
		t.Errorf("current findings = %d, want 1", len(got.CurrentFindings))
	}
}

// The note is the snapshot's own statement of what its event evidence is
// allowed to prove, so a caller must not be able to soften it.
func TestNewSnapshot_StampsTheObservationalNoteOverAnythingTheCallerSupplied(t *testing.T) {
	got, err := newSnapshot(wholeRequest())
	if err != nil {
		t.Fatalf("newSnapshot: %v", err)
	}
	if got.Events.Note != EventEvidenceNote {
		t.Errorf("event note = %q, want the fixed observational note", got.Events.Note)
	}
	if got.Events.Note == "" {
		t.Error("event note is empty; the record does not say what its evidence proves")
	}
}

func TestNewSnapshot_RefusesASelectedItemThatIsNotOpen(t *testing.T) {
	for _, status := range []core.CoarseStatus{
		core.CoarseStatusInProgress,
		core.CoarseStatusClosed,
		core.CoarseStatusBlocked,
		core.CoarseStatusTombstone,
	} {
		req := wholeRequest()
		req.Selection.Candidate.Status = status
		if _, err := newSnapshot(req); !errors.Is(err, ErrSelectedItemNotOpen) {
			t.Errorf("status %s: err = %v, want ErrSelectedItemNotOpen", status, err)
		}
	}
}

func TestNewSnapshot_RefusesASelectionWithNoStatedRepeatSafeReason(t *testing.T) {
	req := wholeRequest()
	req.Selection.RepeatSafeReason = ""
	if _, err := newSnapshot(req); !errors.Is(err, ErrNoRepeatSafeReason) {
		t.Errorf("err = %v, want ErrNoRepeatSafeReason", err)
	}
}

func TestNewSnapshot_RefusesAPostureThatIsNotOneLocalStreamRun(t *testing.T) {
	for name, posture := range map[string]Posture{
		"not local":       {Local: false, ItemCount: 1, Concurrency: 1},
		"two items":       {Local: true, ItemCount: 2, Concurrency: 1},
		"no items":        {Local: true, ItemCount: 0, Concurrency: 1},
		"concurrency two": {Local: true, ItemCount: 1, Concurrency: 2},
	} {
		req := wholeRequest()
		req.Selection.Posture = posture
		if _, err := newSnapshot(req); !errors.Is(err, ErrPostureNotOneLocalRun) {
			t.Errorf("%s: err = %v, want ErrPostureNotOneLocalRun", name, err)
		}
	}
}

// An exclusion is two halves and neither alone is evidence: a reason that names
// no bead says nothing, and a bead with no reason is just an absence.
func TestNewSnapshot_RefusesAnExclusionMissingEitherHalf(t *testing.T) {
	for name, tc := range map[string]struct {
		damage func(*Exclusion)
		want   error
	}{
		"no reason": {func(e *Exclusion) { e.Reason = "" }, ErrExclusionWithoutReason},
		"no bead":   {func(e *Exclusion) { e.Candidate.BeadID = "" }, ErrExclusionWithoutBead},
	} {
		req := wholeRequest()
		tc.damage(&req.Excluded[0])
		if _, err := newSnapshot(req); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", name, err, tc.want)
		}
	}
}

// A bead that is both the canary and a rejected candidate is a capture that
// contradicts itself, and the contradiction is invisible in the rendered file.
func TestNewSnapshot_RefusesABeadThatIsBothSelectedAndExcluded(t *testing.T) {
	req := wholeRequest()
	req.Excluded[0].Candidate.BeadID = req.Selection.Candidate.BeadID
	if _, err := newSnapshot(req); !errors.Is(err, ErrDuplicateCandidate) {
		t.Errorf("err = %v, want ErrDuplicateCandidate", err)
	}
}

// Closing an old finding needs a commit and a test. Each field below is one
// half of that standard, and dropping any one of them must refuse the record.
func TestNewSnapshot_RefusesAStaleFindingThatDoesNotMeetTheEvidenceStandard(t *testing.T) {
	for name, damage := range map[string]func(*StaleFinding){
		"no ID":             func(f *StaleFinding) { f.FindingID = "" },
		"no checked source": func(f *StaleFinding) { f.CheckedSource = "" },
		"no fixing commit":  func(f *StaleFinding) { f.FixingCommit = "" },
		"no focused proof":  func(f *StaleFinding) { f.FocusedProof = "" },
		"no disposition":    func(f *StaleFinding) { f.Disposition = "" },
	} {
		req := wholeRequest()
		damage(&req.StaleFindings[0])
		if _, err := newSnapshot(req); !errors.Is(err, ErrStaleFindingIncomplete) {
			t.Errorf("%s: err = %v, want ErrStaleFindingIncomplete", name, err)
		}
	}
}

// A current finding that names no source path cannot be re-checked by the next
// reader, which is the same reason a stale one must name its checked source.
func TestNewSnapshot_RefusesACurrentFindingWithNoScopedRecordEvidenceOrSource(t *testing.T) {
	for name, damage := range map[string]func(*CurrentFinding){
		"no scoped record": func(f *CurrentFinding) { f.RecordID = "" },
		"no evidence":      func(f *CurrentFinding) { f.Evidence = "" },
		"no source path":   func(f *CurrentFinding) { f.Source = "" },
	} {
		req := wholeRequest()
		damage(&req.CurrentFindings[0])
		if _, err := newSnapshot(req); !errors.Is(err, ErrCurrentFindingIncomplete) {
			t.Errorf("%s: err = %v, want ErrCurrentFindingIncomplete", name, err)
		}
	}
}

func TestNewSnapshot_RefusesACaptureMissingItsRequiredEvidencePaths(t *testing.T) {
	for name, tc := range map[string]struct {
		damage func(*request)
		want   error
	}{
		"no capture time":            {func(r *request) { r.CapturedAt = time.Time{} }, ErrNoCaptureTime},
		"no commands":                {func(r *request) { r.Commands = nil }, ErrNoCommands},
		"no selected item":           {func(r *request) { r.Selection.Candidate.BeadID = "" }, ErrNoSelectedItem},
		"no event log path":          {func(r *request) { r.EventLogPaths = nil }, ErrNoEventLogPath},
		"no terminal-intent inspect": {func(r *request) { r.TerminalIntent.Dir = "" }, ErrNoTerminalIntentDir},
	} {
		req := wholeRequest()
		tc.damage(&req)
		if _, err := newSnapshot(req); !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", name, err, tc.want)
		}
	}
}

// BI-013e requires stale findings to be named separately from current ones.
// The separation is structural — two typed lists, not one list with a flag — so
// this test proves it survives the round trip to the file the assessor reads.
func TestSnapshot_KeepsStaleFindingsOutOfTheCurrentFindingListThroughJSON(t *testing.T) {
	snap, err := newSnapshot(wholeRequest())
	if err != nil {
		t.Fatalf("newSnapshot: %v", err)
	}
	body, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Snapshot
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if len(got.StaleFindings) != 1 || got.StaleFindings[0].FindingID != "hk-v4wer" {
		t.Errorf("stale findings after round trip = %+v", got.StaleFindings)
	}
	if len(got.CurrentFindings) != 1 || got.CurrentFindings[0].RecordID != "hk-still-open" {
		t.Errorf("current findings after round trip = %+v", got.CurrentFindings)
	}
	for _, cur := range got.CurrentFindings {
		if cur.RecordID == "hk-v4wer" {
			t.Error("a stale finding arrived in the current-finding list")
		}
	}
	// Positive evidence that the two lists are distinct keys in the file, not
	// one key the reader splits: a decoder that ignored the distinction would
	// leave one of them empty above, but so would an encoder that wrote
	// neither, so check the encoded form names both.
	for _, key := range []string{`"stale_findings"`, `"current_findings"`} {
		if !strings.Contains(string(body), key) {
			t.Errorf("encoded snapshot has no %s key", key)
		}
	}
}

func TestSnapshot_CandidateSetIsTheSelectedItemPlusEveryExcludedOne(t *testing.T) {
	snap, err := newSnapshot(wholeRequest())
	if err != nil {
		t.Fatalf("newSnapshot: %v", err)
	}
	set := snap.CandidateSet()
	if len(set) != 2 {
		t.Fatalf("candidate set = %d beads, want 2", len(set))
	}
	if set[0].BeadID != "hk-canary" || set[1].BeadID != "hk-remote" {
		t.Errorf("candidate set = %s, %s; want the selected item first", set[0].BeadID, set[1].BeadID)
	}
}

func TestSnapshot_OutputPathsSkipsACommandWhoseOutputWasNotRetained(t *testing.T) {
	snap, err := newSnapshot(wholeRequest())
	if err != nil {
		t.Fatalf("newSnapshot: %v", err)
	}
	paths := snap.OutputPaths()
	if len(paths) != 1 || paths[0] != "evidence/hk-canary.json" {
		t.Errorf("output paths = %v, want only the retained one", paths)
	}
	// The fixture holds two commands, so a method that returned every command's
	// path would have returned two. That is what makes the count above a claim.
	if len(snap.Commands) != 2 {
		t.Fatalf("fixture no longer has one retained and one unretained command")
	}
}
