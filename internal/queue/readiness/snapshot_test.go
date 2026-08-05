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
		Posture:    Posture{Local: true, ItemCount: 1, Concurrency: 1},
		Selected: []SelectedItem{{
			Candidate: Candidate{
				BeadID: "hk-canary",
				Title:  "correct one comment in the scratch clone",
				Status: core.CoarseStatusOpen,
				Labels: []string{"codename:queue-dogfood-readiness"},
			},
			RepeatSafeReason: "single-file comment edit; re-running it reaches the same tree",
		}},
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

// manyItemRequest is the run the operator asked for: several items at once,
// each with its own reason for being safe to re-run.
func manyItemRequest() request {
	req := wholeRequest()
	req.Posture = Posture{Local: true, ItemCount: 3, Concurrency: 3}
	req.Selected = []SelectedItem{
		{
			Candidate:        Candidate{BeadID: "hk-canary", Title: "correct one comment", Status: core.CoarseStatusOpen},
			RepeatSafeReason: "single-file comment edit; re-running it reaches the same tree",
		},
		{
			Candidate:        Candidate{BeadID: "hk-second", Title: "correct a second comment", Status: core.CoarseStatusOpen},
			RepeatSafeReason: "touches one other file that nothing tests",
		},
		{
			Candidate:        Candidate{BeadID: "hk-third", Title: "correct a third comment", Status: core.CoarseStatusOpen},
			RepeatSafeReason: "adds a doc line; the tree is the same whether it runs once or twice",
		},
	}
	return req
}

// multiItemSnapshot is the many-item record the validator tests judge against.
func multiItemSnapshot(t *testing.T) Snapshot {
	t.Helper()
	snap, err := newSnapshot(manyItemRequest())
	if err != nil {
		t.Fatalf("newSnapshot on a many-item request: %v", err)
	}
	return snap
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
	if len(got.Selected) != 1 || got.Selected[0].Candidate.BeadID != "hk-canary" {
		t.Errorf("selected items = %+v, want hk-canary", got.Selected)
	}
	if got.Selected[0].RepeatSafeReason == "" {
		t.Error("selected item kept no repeat-safe reason")
	}
	if got.Posture != (Posture{Local: true, ItemCount: 1, Concurrency: 1}) {
		t.Errorf("posture = %+v, want the one the request stated", got.Posture)
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

// The record the operator asked for: several items, each named, each with its
// own reason. The old shape held one selection in a field and could not express
// this at all.
func TestNewSnapshot_RecordsSeveralSelectedItemsEachWithItsOwnReason(t *testing.T) {
	got := multiItemSnapshot(t)

	if len(got.Selected) != 3 {
		t.Fatalf("selected items = %d, want 3", len(got.Selected))
	}
	if got.Posture.ItemCount != 3 || got.Posture.Concurrency != 3 {
		t.Errorf("posture = %+v, want three items at concurrency three", got.Posture)
	}
	reasons := map[string]bool{}
	for _, sel := range got.Selected {
		if sel.RepeatSafeReason == "" {
			t.Errorf("%s carries no repeat-safe reason", sel.Candidate.BeadID)
		}
		reasons[sel.RepeatSafeReason] = true
	}
	if len(reasons) != 3 {
		t.Errorf("three items share %d reasons; each item is its own judgement", len(reasons))
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
		req.Selected[0].Candidate.Status = status
		if _, err := newSnapshot(req); !errors.Is(err, ErrSelectedItemNotOpen) {
			t.Errorf("status %s: err = %v, want ErrSelectedItemNotOpen", status, err)
		}
	}
}

// A record that proved its first item and took the rest on trust is the failure
// the list shape exists to make impossible, so the check runs to the end.
func TestNewSnapshot_RefusesANotOpenItemAnywhereInTheSelection(t *testing.T) {
	for _, at := range []int{0, 1, 2} {
		req := manyItemRequest()
		req.Selected[at].Candidate.Status = core.CoarseStatusClosed
		if _, err := newSnapshot(req); !errors.Is(err, ErrSelectedItemNotOpen) {
			t.Errorf("closed item at position %d: err = %v, want ErrSelectedItemNotOpen", at, err)
		}
	}
}

func TestNewSnapshot_RefusesASelectionWithNoStatedRepeatSafeReason(t *testing.T) {
	req := wholeRequest()
	req.Selected[0].RepeatSafeReason = ""
	if _, err := newSnapshot(req); !errors.Is(err, ErrNoRepeatSafeReason) {
		t.Errorf("err = %v, want ErrNoRepeatSafeReason", err)
	}
}

// Every item needs its own reason. One sentence covering three items is two of
// them taken on trust.
func TestNewSnapshot_RefusesAnyOneItemWithNoStatedRepeatSafeReason(t *testing.T) {
	for _, at := range []int{0, 1, 2} {
		req := manyItemRequest()
		req.Selected[at].RepeatSafeReason = ""
		if _, err := newSnapshot(req); !errors.Is(err, ErrNoRepeatSafeReason) {
			t.Errorf("no reason at position %d: err = %v, want ErrNoRepeatSafeReason", at, err)
		}
	}
}

// The load-bearing one. Widening the posture without this check gives a record
// that claims three items and can name only one, and the assessor reading the
// file six weeks later cannot tell which number is true.
func TestNewSnapshot_RefusesAPostureWhoseItemCountDoesNotMatchTheItemsNamed(t *testing.T) {
	for name, tc := range map[string]struct {
		itemCount int
		selected  int
	}{
		"claims three, names one":  {itemCount: 3, selected: 1},
		"claims one, names three":  {itemCount: 1, selected: 3},
		"claims none, names one":   {itemCount: 0, selected: 1},
		"claims four, names three": {itemCount: 4, selected: 3},
	} {
		req := manyItemRequest()
		req.Selected = req.Selected[:tc.selected]
		req.Posture.ItemCount = tc.itemCount

		if _, err := newSnapshot(req); !errors.Is(err, ErrPostureItemCountMismatch) {
			t.Errorf("%s: err = %v, want ErrPostureItemCountMismatch", name, err)
		}
	}
}

// A remote run is still out of scope for the first pass. Only the one-item and
// concurrency-one rules were withdrawn.
func TestNewSnapshot_RefusesARunThatIsNotLocal(t *testing.T) {
	req := wholeRequest()
	req.Posture.Local = false
	if _, err := newSnapshot(req); !errors.Is(err, ErrPostureNotLocal) {
		t.Errorf("err = %v, want ErrPostureNotLocal", err)
	}
}

// The record must state how many items run at the same time. A zero states
// nothing, and a record that accepted one would be evidence about a run shape
// nobody wrote down.
func TestNewSnapshot_RefusesAPostureThatStatesNoConcurrency(t *testing.T) {
	for _, concurrency := range []int{0, -1} {
		req := wholeRequest()
		req.Posture.Concurrency = concurrency
		if _, err := newSnapshot(req); !errors.Is(err, ErrPostureConcurrencyUnset) {
			t.Errorf("concurrency %d: err = %v, want ErrPostureConcurrencyUnset", concurrency, err)
		}
	}
}

// The rule the operator withdrew. This test is the guard against it coming
// back: more than one item at more than concurrency one is a record the package
// must be able to build.
func TestNewSnapshot_AcceptsSeveralItemsAtConcurrencyAboveOne(t *testing.T) {
	for name, posture := range map[string]Posture{
		"three items, three at once": {Local: true, ItemCount: 3, Concurrency: 3},
		"three items, two at once":   {Local: true, ItemCount: 3, Concurrency: 2},
		"three items, ten at once":   {Local: true, ItemCount: 3, Concurrency: 10},
	} {
		req := manyItemRequest()
		req.Posture = posture
		if _, err := newSnapshot(req); err != nil {
			t.Errorf("%s: newSnapshot refused a many-item run: %v", name, err)
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

// A bead that is both a canary and a rejected candidate is a capture that
// contradicts itself, and the contradiction is invisible in the rendered file.
func TestNewSnapshot_RefusesABeadThatIsBothSelectedAndExcluded(t *testing.T) {
	req := wholeRequest()
	req.Excluded[0].Candidate.BeadID = req.Selected[0].Candidate.BeadID
	if _, err := newSnapshot(req); !errors.Is(err, ErrDuplicateCandidate) {
		t.Errorf("err = %v, want ErrDuplicateCandidate", err)
	}
}

// One bead selected twice inflates the item count against a run that would
// dispatch it once. The old one-selection shape could not express this; the
// list can, so the check has to.
func TestNewSnapshot_RefusesTheSameBeadSelectedTwice(t *testing.T) {
	req := manyItemRequest()
	req.Selected[2].Candidate.BeadID = req.Selected[0].Candidate.BeadID
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
		"no selected item at all":    {func(r *request) { r.Selected = nil }, ErrNoSelectedItem},
		"a selected item with no ID": {func(r *request) { r.Selected[0].Candidate.BeadID = "" }, ErrNoSelectedItem},
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

// The posture is one key at the top of the file and not a copy inside each
// selected item. Two copies could disagree about how many items there are.
func TestSnapshot_WritesThePostureOnceAtTheTopOfTheFile(t *testing.T) {
	body, err := json.Marshal(multiItemSnapshot(t))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if got := strings.Count(string(body), `"posture"`); got != 1 {
		t.Errorf("encoded snapshot names the posture %d times, want once", got)
	}
	if !strings.Contains(string(body), `"item_count":3`) {
		t.Errorf("encoded snapshot does not record the item count: %s", body)
	}
	if !strings.Contains(string(body), `"concurrency":3`) {
		t.Errorf("encoded snapshot does not record the concurrency: %s", body)
	}
}

func TestSnapshot_CandidateSetIsEverySelectedItemPlusEveryExcludedOne(t *testing.T) {
	set := multiItemSnapshot(t).CandidateSet()
	if len(set) != 4 {
		t.Fatalf("candidate set = %d beads, want 4", len(set))
	}
	want := []core.BeadID{"hk-canary", "hk-second", "hk-third", "hk-remote"}
	for i, id := range want {
		if set[i].BeadID != id {
			t.Errorf("candidate %d = %s, want %s; selected items come first, in order", i, set[i].BeadID, id)
		}
	}
}

func TestSnapshot_SelectedBeadIDsNamesEveryChosenItemInOrder(t *testing.T) {
	ids := multiItemSnapshot(t).SelectedBeadIDs()
	want := []string{"hk-canary", "hk-second", "hk-third"}
	if len(ids) != len(want) {
		t.Fatalf("selected bead IDs = %q, want %q", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Errorf("selected bead %d = %q, want %q", i, ids[i], want[i])
		}
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
