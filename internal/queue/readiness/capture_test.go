package readiness

// capture_test.go — the claims the shell makes, and the read-only ratchet.
//
// The headline claim of this package is a negative one: assembling readiness
// evidence never changes fleet ledger state. A negative claim is satisfied for
// free by any environment where nothing happens, so each test below pairs it
// with positive evidence that the machinery actually ran, and the guard that
// checks for a write is itself put through a failing case.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/core"
)

// ledgerRecorder is a fake ledger that logs the verb of every call it receives,
// in the shape those calls take when they reach `br`.
//
// It deliberately implements MORE than [BeadReader]. CloseBead and CreateBead
// are here even though the port declares no such methods, and that is the whole
// point of the read-only test below. Go lets a function widen a narrow
// interface at runtime —
//
//	if w, ok := ledger.(interface{ CloseBead(context.Context, core.BeadID) error }); ok {
//		_ = w.CloseBead(ctx, id)
//	}
//
// — and that dodge compiles, reads as local and harmless, and defeats the whole
// read-only guarantee with nothing else going red. A recorder that could only
// record reads would make "no write happened" unfalsifiable. This one can
// record a write, so only the production code's restraint keeps the log clean.
type ledgerRecorder struct {
	beads map[core.BeadID]core.BeadRecord
	calls []string
	fail  map[core.BeadID]error
}

func (r *ledgerRecorder) ShowBead(_ context.Context, id core.BeadID) (core.BeadRecord, error) {
	r.calls = append(r.calls, "show "+string(id))
	if err, bad := r.fail[id]; bad {
		return core.BeadRecord{}, err
	}
	rec, ok := r.beads[id]
	if !ok {
		return core.BeadRecord{}, errors.New("ISSUE_NOT_FOUND")
	}
	return rec, nil
}

func (r *ledgerRecorder) CloseBead(_ context.Context, id core.BeadID) error {
	r.calls = append(r.calls, "close "+string(id))
	return nil
}

func (r *ledgerRecorder) CreateBead(_ context.Context, title string) (core.BeadID, error) {
	r.calls = append(r.calls, "create "+title)
	return "hk-new", nil
}

// mutatingCalls returns every logged call whose verb can change a bead. It is a
// plain function so the guard itself can be tested; see the sub-test in
// TestCapture_MakesNoLedgerCallThatCouldChangeABead.
func mutatingCalls(calls []string) []string {
	mutating := []string{"close ", "reopen ", "claim ", "create ", "update ", "delete ", "tombstone "}
	var found []string
	for _, c := range calls {
		for _, verb := range mutating {
			if len(c) >= len(verb) && c[:len(verb)] == verb {
				found = append(found, c)
			}
		}
	}
	return found
}

func recorderWithCandidates() *ledgerRecorder {
	return &ledgerRecorder{beads: map[core.BeadID]core.BeadRecord{
		"hk-canary": {
			BeadID: "hk-canary",
			Title:  "correct one comment in the scratch clone",
			Status: core.CoarseStatusOpen,
			Labels: []string{"codename:queue-dogfood-readiness"},
		},
		"hk-remote": {BeadID: "hk-remote", Title: "remote worker probe", Status: core.CoarseStatusOpen},
		"hk-wave":   {BeadID: "hk-wave", Title: "wave queue probe", Status: core.CoarseStatusOpen},
	}}
}

func captureRequest() CaptureRequest {
	return CaptureRequest{
		CapturedAt: capturedAt,
		Posture:    Posture{Local: true, ItemCount: 1, Concurrency: 1},
		Selected: []SelectedID{
			{BeadID: "hk-canary", RepeatSafeReason: "single-file comment edit; re-running it reaches the same tree"},
		},
		ExcludedIDs: []ExcludedID{
			{BeadID: "hk-remote", Reason: "needs a remote worker; pass one is local only"},
			{BeadID: "hk-wave", Reason: "wave queue; pass one is one stream item"},
		},
		Commands:       []Command{{Argv: []string{"br", "show", "hk-canary", "--format", "json"}}},
		EventLogPaths:  []string{".harmonik/events/events.jsonl"},
		TerminalIntent: TerminalIntent{Dir: ".harmonik/beads-intents"},
	}
}

// manyItemCaptureRequest selects three items instead of one and states a run
// shape to match. It is the request the old one-selection field could not hold.
func manyItemCaptureRequest() CaptureRequest {
	req := captureRequest()
	req.Posture = Posture{Local: true, ItemCount: 3, Concurrency: 3}
	req.Selected = []SelectedID{
		{BeadID: "hk-canary", RepeatSafeReason: "single-file comment edit; re-running it reaches the same tree"},
		{BeadID: "hk-remote", RepeatSafeReason: "the probe is idempotent against a local endpoint"},
		{BeadID: "hk-wave", RepeatSafeReason: "writes nothing outside its own scratch directory"},
	}
	req.ExcludedIDs = nil
	return req
}

func TestCapture_ReadsTheLiveLedgerOnceForEveryCandidate(t *testing.T) {
	rec := recorderWithCandidates()
	snap, err := Capture(context.Background(), rec, captureRequest())
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}

	want := []string{"show hk-canary", "show hk-remote", "show hk-wave"}
	if !reflect.DeepEqual(rec.calls, want) {
		t.Errorf("ledger calls = %v, want %v", rec.calls, want)
	}
	if got := len(snap.CandidateSet()); got != 3 {
		t.Errorf("candidate set = %d, want 3", got)
	}
}

// The read-only claim, against a ledger that CAN take a write.
//
// The recorder implements CloseBead and CreateBead, so the only reason this
// test stays green is that Capture never reaches for them — not by type-
// asserting the port to something wider, and not by any other route. Add such
// an assertion to Capture and this goes red. That is what makes the assertion
// below a claim rather than a restatement of the port's signature; the port's
// own shape is covered separately by TestBeadReaderExposesNoWriteMethod.
func TestCapture_MakesNoLedgerCallThatCouldChangeABead(t *testing.T) {
	rec := recorderWithCandidates()
	if _, err := Capture(context.Background(), rec, captureRequest()); err != nil {
		t.Fatalf("Capture: %v", err)
	}

	if len(rec.calls) == 0 {
		t.Fatal("no ledger call was made at all; a capture that reads nothing proves nothing")
	}
	if got := mutatingCalls(rec.calls); len(got) != 0 {
		t.Errorf("capture made ledger calls that can change a bead: %v", got)
	}

	// Positive evidence that the recorder really would have logged a write, so
	// the empty result above is restraint and not a blind spot.
	t.Run("the recorder logs a write when it takes one", func(t *testing.T) {
		probe := recorderWithCandidates()
		if err := probe.CloseBead(context.Background(), "hk-canary"); err != nil {
			t.Fatalf("CloseBead: %v", err)
		}
		if got := mutatingCalls(probe.calls); len(got) != 1 || got[0] != "close hk-canary" {
			t.Errorf("guard over a written-to recorder = %v, want the close", got)
		}
	})
}

// The port is the read-only guarantee. This test is the ratchet on it: adding a
// write method to BeadReader turns the guarantee off with no other signal.
func TestBeadReaderExposesNoWriteMethod(t *testing.T) {
	readOnly := map[string]struct{}{"ShowBead": {}}

	typ := reflect.TypeOf((*BeadReader)(nil)).Elem()
	if typ.NumMethod() == 0 {
		t.Fatal("BeadReader declares no method at all, so this test would pass against an empty port")
	}
	for i := range typ.NumMethod() {
		name := typ.Method(i).Name
		if _, ok := readOnly[name]; !ok {
			t.Errorf("BeadReader has method %q, which is not a read. "+
				"The read-only guarantee of BI-013e rests on this port having no way to write.", name)
		}
	}
}

// A candidate whose status could not be read is not evidence. Dropping it would
// report a smaller candidate set than the one that was really considered, which
// is the flattering-denominator failure in miniature.
func TestCapture_FailsWhenACandidateCannotBeRead(t *testing.T) {
	rec := recorderWithCandidates()
	rec.fail = map[core.BeadID]error{"hk-wave": errors.New("br: database is locked")}

	if _, err := Capture(context.Background(), rec, captureRequest()); err == nil {
		t.Fatal("Capture succeeded with an unreadable candidate")
	}
	// Positive evidence that it failed at the right point: the two readable
	// candidates ahead of it were still read.
	if len(rec.calls) != 3 {
		t.Errorf("ledger calls = %v, want the two good reads then the failing one", rec.calls)
	}
}

// The caller supplies bead IDs and reasons; it cannot supply a status. This is
// what makes the status in the record evidence rather than an assertion.
func TestCapture_TakesTheCandidateStatusFromTheLedgerAndNotTheCaller(t *testing.T) {
	rec := recorderWithCandidates()
	closed := rec.beads["hk-canary"]
	closed.Status = core.CoarseStatusClosed
	rec.beads["hk-canary"] = closed

	_, err := Capture(context.Background(), rec, captureRequest())
	if !errors.Is(err, ErrSelectedItemNotOpen) {
		t.Fatalf("err = %v, want ErrSelectedItemNotOpen from the live status", err)
	}
	if len(rec.calls) == 0 {
		t.Error("the refusal did not come from a ledger read")
	}
}

// Every selected item's status comes from its own live read. A capture that
// read the first one and took the rest from the request would let a closed item
// into a many-item run.
func TestCapture_ReadsTheStatusOfEverySelectedItemLive(t *testing.T) {
	rec := recorderWithCandidates()
	closed := rec.beads["hk-wave"]
	closed.Status = core.CoarseStatusClosed
	rec.beads["hk-wave"] = closed

	_, err := Capture(context.Background(), rec, manyItemCaptureRequest())
	if !errors.Is(err, ErrSelectedItemNotOpen) {
		t.Fatalf("err = %v, want ErrSelectedItemNotOpen for the third selected item", err)
	}
	want := []string{"show hk-canary", "show hk-remote", "show hk-wave"}
	if !reflect.DeepEqual(rec.calls, want) {
		t.Errorf("ledger calls = %v, want %v; each selected item is read in order", rec.calls, want)
	}
}

// The many-item record, end to end through the shell.
func TestCapture_KeepsEverySelectedItemAndItsOwnReason(t *testing.T) {
	rec := recorderWithCandidates()
	snap, err := Capture(context.Background(), rec, manyItemCaptureRequest())
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if len(snap.Selected) != 3 {
		t.Fatalf("selected items = %d, want 3", len(snap.Selected))
	}
	if snap.Posture.ItemCount != 3 || snap.Posture.Concurrency != 3 {
		t.Errorf("posture = %+v, want three items at concurrency three", snap.Posture)
	}
	for _, sel := range snap.Selected {
		if sel.RepeatSafeReason == "" {
			t.Errorf("%s lost its repeat-safe reason", sel.Candidate.BeadID)
		}
	}
}

func TestCapture_RefusesARequestThatNamesNoSelectedItem(t *testing.T) {
	rec := recorderWithCandidates()
	req := captureRequest()
	req.Selected = nil

	if _, err := Capture(context.Background(), rec, req); !errors.Is(err, ErrNoSelectedItem) {
		t.Errorf("err = %v, want ErrNoSelectedItem", err)
	}
	if len(rec.calls) != 0 {
		t.Errorf("a request with no selection still read the ledger: %v", rec.calls)
	}
}

// A request that states a run of three items but names one is refused before
// any of it reaches a file. This is the check the widened record exists for.
func TestCapture_RefusesAPostureThatDisagreesWithTheItemsNamed(t *testing.T) {
	rec := recorderWithCandidates()
	req := captureRequest()
	req.Posture.ItemCount = 3

	if _, err := Capture(context.Background(), rec, req); !errors.Is(err, ErrPostureItemCountMismatch) {
		t.Errorf("err = %v, want ErrPostureItemCountMismatch", err)
	}
}

func TestReadTerminalIntents_NamesTheDirectoryEvenWhenNothingIsPending(t *testing.T) {
	harmonikDir := t.TempDir()

	got, err := ReadTerminalIntents(harmonikDir)
	if err != nil {
		t.Fatalf("ReadTerminalIntents on an absent dir: %v", err)
	}
	if got.Dir != filepath.Join(harmonikDir, TerminalIntentDirName) {
		t.Errorf("dir = %q, want the intent dir under %q", got.Dir, harmonikDir)
	}
	if len(got.Pending) != 0 {
		t.Errorf("pending = %+v, want none", got.Pending)
	}
}

func TestReadTerminalIntents_ListsEveryPendingEntrySortedByKey(t *testing.T) {
	harmonikDir := t.TempDir()
	intentDir := filepath.Join(harmonikDir, TerminalIntentDirName)
	if err := os.MkdirAll(intentDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// The file names are deliberately in the OPPOSITE order to the keys.
	// os.ReadDir already returns entries sorted by file name, so a fixture whose
	// file order matched its key order would pass with the sort deleted — the
	// first draft of this test did exactly that and proved nothing.
	writeIntent(t, intentDir, "01-first-on-disk.json", "zulu-key", "hk-two", core.TerminalOpClose)
	writeIntent(t, intentDir, "02-second-on-disk.json", "alpha-key", "hk-one", core.TerminalOpClaim)
	// A non-JSON file in the same directory must not become a pending entry.
	if err := os.WriteFile(filepath.Join(intentDir, "notes.txt"), []byte("scratch"), 0o600); err != nil {
		t.Fatalf("write notes: %v", err)
	}

	got, err := ReadTerminalIntents(harmonikDir)
	if err != nil {
		t.Fatalf("ReadTerminalIntents: %v", err)
	}
	if len(got.Pending) != 2 {
		t.Fatalf("pending = %d entries, want 2 (the .txt must be skipped)", len(got.Pending))
	}
	if got.Pending[0].IdempotencyKey != "alpha-key" || got.Pending[1].IdempotencyKey != "zulu-key" {
		t.Errorf("pending order = %q, %q; want key order",
			got.Pending[0].IdempotencyKey, got.Pending[1].IdempotencyKey)
	}
	if got.Pending[0].BeadID != "hk-one" || got.Pending[0].Op != core.TerminalOpClaim {
		t.Errorf("first pending entry = %+v, want the claim on hk-one", got.Pending[0])
	}
}

func TestWriteSnapshot_LeavesAFileTheAssessorCanDecode(t *testing.T) {
	snap, err := newSnapshot(wholeRequest())
	if err != nil {
		t.Fatalf("newSnapshot: %v", err)
	}
	path := filepath.Join(t.TempDir(), "evidence", "readiness.json")

	if err := WriteSnapshot(path, snap); err != nil {
		t.Fatalf("WriteSnapshot: %v", err)
	}
	body, err := os.ReadFile(path) //nolint:gosec // G304: path is this test's own temp dir
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	var got Snapshot
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Selected) != 1 || got.Selected[0].Candidate.BeadID != snap.Selected[0].Candidate.BeadID {
		t.Errorf("selected items after round trip = %+v", got.Selected)
	}
	if got.Events.Note != EventEvidenceNote {
		t.Errorf("observational note did not survive the write: %q", got.Events.Note)
	}
}

func writeIntent(t *testing.T, dir, file, key string, bead core.BeadID, op core.TerminalOp) {
	t.Helper()
	// A claim or close entry must carry a run and a transition, so the fixture
	// derives both from the key: fixed input, fixed IDs, no clock.
	id := uuid.NewSHA1(uuid.Nil, []byte(key))
	entry := core.IntentLogEntry{
		IdempotencyKey:    key,
		RunID:             core.RunID(id),
		TransitionID:      core.TransitionID(id),
		Op:                op,
		BeadID:            bead,
		IntendedPostState: core.CoarseStatusClosed,
		RequestedAt:       time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC),
		SchemaVersion:     1,
	}
	body, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal intent: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, file), body, 0o600); err != nil {
		t.Fatalf("write intent: %v", err)
	}
}
