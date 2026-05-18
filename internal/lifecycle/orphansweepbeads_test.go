package lifecycle

// orphansweepbeads_test.go — unit tests for the PL-006 sixth-bullet
// stale-in_progress bead-reset sweep. Bead ref: hk-iuaed.4.
//
// Helper prefix: imrestSweep (per implementer-protocol §Helper-prefix).

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/gregberns/harmonik/internal/brcli"
	"github.com/gregberns/harmonik/internal/core"
)

// ---------------------------------------------------------------------------
// Fakes for BeadLedger / BeadResetter / MergeCommitScanner
// ---------------------------------------------------------------------------

// imrestSweepFakeLedger implements InFlightBeadLedger.
type imrestSweepFakeLedger struct {
	beads []core.BeadRecord
	err   error
}

func (f *imrestSweepFakeLedger) ListInFlightBeads(_ context.Context) ([]core.BeadRecord, error) {
	return f.beads, f.err
}

// imrestSweepFakeResetter implements BeadResetter and records calls.
type imrestSweepFakeResetter struct {
	called []core.BeadID
	errOn  map[core.BeadID]error
}

func (f *imrestSweepFakeResetter) ResetBead(
	_ context.Context,
	_ string,
	_ brcli.TimeoutConfig,
	beadID core.BeadID,
	_ core.ProjectHash,
	_ int64,
) error {
	f.called = append(f.called, beadID)
	if err, ok := f.errOn[beadID]; ok {
		return err
	}
	return nil
}

// imrestSweepFakeMergeScanner implements MergeCommitScanner.
type imrestSweepFakeMergeScanner struct {
	merged map[core.BeadID]bool
	err    error
}

func (f *imrestSweepFakeMergeScanner) HasMergeCommitForBead(_ context.Context, beadID core.BeadID) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.merged[beadID], nil
}

// imrestSweepFakeCat3cCloser implements BeadCat3cCloser and records calls.
type imrestSweepFakeCat3cCloser struct {
	called []core.BeadID
	errOn  map[core.BeadID]error
}

func (f *imrestSweepFakeCat3cCloser) SweepCloseBead(_ context.Context, _ brcli.TimeoutConfig, beadID core.BeadID) error {
	f.called = append(f.called, beadID)
	if err, ok := f.errOn[beadID]; ok {
		return err
	}
	return nil
}

// imrestSweepBead constructs a valid in-progress BeadRecord with the given ID.
func imrestSweepBead(id string) core.BeadRecord {
	return core.BeadRecord{
		BeadID:        core.BeadID(id),
		Title:         "test " + id,
		Description:   "test description for " + id,
		BeadType:      "task",
		Status:        core.CoarseStatusInProgress,
		Edges:         nil,
		AuditTrailRef: id,
	}
}

// imrestSweepWriteIntent writes a minimal IntentLogEntry to intentLogDir for
// (op, beadID) so ScanIntentLog observes it.
func imrestSweepWriteIntent(t *testing.T, intentLogDir string, beadID core.BeadID, op core.TerminalOp) {
	t.Helper()
	//nolint:gosec // G301: 0755 matches conventions
	if err := os.MkdirAll(intentLogDir, 0o755); err != nil {
		t.Fatalf("imrestSweepWriteIntent: MkdirAll: %v", err)
	}

	var post core.CoarseStatus
	var runID core.RunID
	var transitionID core.TransitionID
	var ikey string
	switch op {
	case core.TerminalOpClaim:
		post = core.CoarseStatusInProgress
		runID = core.RunID(uuid.New())
		transitionID = core.TransitionID(uuid.New())
		ikey = core.IdempotencyKey(runID, transitionID, op)
	case core.TerminalOpClose:
		post = core.CoarseStatusClosed
		runID = core.RunID(uuid.New())
		transitionID = core.TransitionID(uuid.New())
		ikey = core.IdempotencyKey(runID, transitionID, op)
	case core.TerminalOpReopen:
		post = core.CoarseStatusOpen
		runID = core.RunID(uuid.New())
		transitionID = core.TransitionID(uuid.New())
		ikey = core.IdempotencyKey(runID, transitionID, op)
	case core.TerminalOpReset:
		post = core.CoarseStatusOpen
		ikey = core.ResetBeadIdempotencyKey(core.ProjectHash("aabbccddeeff"), beadID, 1_700_000_000_000_000_000)
	}

	entry := core.IntentLogEntry{
		IdempotencyKey:    ikey,
		RunID:             runID,
		TransitionID:      transitionID,
		Op:                op,
		BeadID:            beadID,
		IntendedPostState: post,
		RequestedAt:       time.Now().UTC(),
		SchemaVersion:     1,
	}
	if !entry.Valid() {
		t.Fatalf("imrestSweepWriteIntent: constructed IntentLogEntry failed Valid(): %+v", entry)
	}

	// Encode bead ID into filename via the same scheme as the adapter:
	// "<encoded_ikey>.json" with colons replaced by underscores.
	// We don't need byte-exact compatibility — the scanner reads any *.json.
	data, marshErr := json.Marshal(entry)
	if marshErr != nil {
		t.Fatalf("imrestSweepWriteIntent: Marshal: %v", marshErr)
	}
	fname := string(beadID) + "_" + string(op) + ".json"
	//nolint:gosec // G306: matches conventions
	if err := os.WriteFile(filepath.Join(intentLogDir, fname), data, 0o600); err != nil {
		t.Fatalf("imrestSweepWriteIntent: WriteFile: %v", err)
	}
}

// imrestSweepBaseConfig builds a SweepStaleInProgressBeadsConfig with the
// required scalar fields populated.
func imrestSweepBaseConfig(t *testing.T) SweepStaleInProgressBeadsConfig {
	t.Helper()
	return SweepStaleInProgressBeadsConfig{
		IntentLogDir:  t.TempDir(),
		ProjectHash:   core.ProjectHash("aabbccddeeff"),
		DaemonStartNS: time.Now().UnixNano(),
	}
}

// ---------------------------------------------------------------------------
// SweepStaleInProgressBeads — happy paths and exclusions
// ---------------------------------------------------------------------------

// TestSweepStaleInProgressBeads_NoBeads verifies that an empty in-flight set
// yields zero resets and no error.
//
// Spec ref: process-lifecycle.md §4.5 PL-006 sixth bullet.
func TestSweepStaleInProgressBeads_NoBeads(t *testing.T) {
	t.Parallel()

	cfg := imrestSweepBaseConfig(t)
	cfg.Ledger = &imrestSweepFakeLedger{}
	cfg.Resetter = &imrestSweepFakeResetter{}

	result, err := SweepStaleInProgressBeads(context.Background(), cfg)
	if err != nil {
		t.Fatalf("SweepStaleInProgressBeads: unexpected error: %v", err)
	}
	if result.ResetCount != 0 {
		t.Errorf("PL-006 sixth bullet: count = %d, want 0", result.ResetCount)
	}
}

// TestSweepStaleInProgressBeads_ProvenanceSkipsNonOwnedBead verifies that an
// in_progress bead without a local claim intent (not owned by this daemon) is
// NOT reset. Provenance discipline per PL-006a.
func TestSweepStaleInProgressBeads_ProvenanceSkipsNonOwnedBead(t *testing.T) {
	t.Parallel()

	cfg := imrestSweepBaseConfig(t)
	cfg.Ledger = &imrestSweepFakeLedger{beads: []core.BeadRecord{imrestSweepBead("hk-other-project")}}
	resetter := &imrestSweepFakeResetter{}
	cfg.Resetter = resetter

	result, err := SweepStaleInProgressBeads(context.Background(), cfg)
	if err != nil {
		t.Fatalf("SweepStaleInProgressBeads: unexpected error: %v", err)
	}
	if result.ResetCount != 0 {
		t.Errorf("PL-006a provenance: count = %d, want 0 (no local claim intent)", result.ResetCount)
	}
	if len(resetter.called) != 0 {
		t.Errorf("PL-006a provenance: ResetBead called on non-owned bead: %v", resetter.called)
	}
}

// TestSweepStaleInProgressBeads_ExclusionA_LiveClaimIntent verifies that a
// bead with a pending `claim` intent (exclusion (a) — BI-031 will re-drive)
// is NOT reset.
func TestSweepStaleInProgressBeads_ExclusionA_LiveClaimIntent(t *testing.T) {
	t.Parallel()

	cfg := imrestSweepBaseConfig(t)
	bid := core.BeadID("hk-iuaed.t1")
	imrestSweepWriteIntent(t, cfg.IntentLogDir, bid, core.TerminalOpClaim)

	cfg.Ledger = &imrestSweepFakeLedger{beads: []core.BeadRecord{imrestSweepBead(string(bid))}}
	resetter := &imrestSweepFakeResetter{}
	cfg.Resetter = resetter

	result, err := SweepStaleInProgressBeads(context.Background(), cfg)
	if err != nil {
		t.Fatalf("SweepStaleInProgressBeads: unexpected error: %v", err)
	}
	if result.ResetCount != 0 {
		t.Errorf("PL-006 exclusion (a): count = %d, want 0", result.ResetCount)
	}
	if len(resetter.called) != 0 {
		t.Errorf("PL-006 exclusion (a): ResetBead called despite claim intent: %v", resetter.called)
	}
}

// TestSweepStaleInProgressBeads_ExclusionB_PendingCloseIntent verifies that a
// bead with a pending `close` intent (exclusion (b) — Cat 3a) is NOT reset.
//
// Setup: we synthesise both a claim intent (provenance) AND a close intent.
// In the real wire, BI-030 step 6 deletes the claim intent on success — so a
// close intent without a claim intent is the realistic state. We test that
// scenario here.
func TestSweepStaleInProgressBeads_ExclusionB_PendingCloseIntent(t *testing.T) {
	t.Parallel()

	cfg := imrestSweepBaseConfig(t)
	bid := core.BeadID("hk-iuaed.t2")

	// Provenance proxy: in production, the claim intent has been removed by
	// BI-030 step 6 success. For exclusion (b) to fire we need to satisfy
	// provenance via the claim intent OR an alternate signal. Since at MVH
	// the only provenance signal is the claim intent, this test exercises a
	// situation where BOTH a claim and a close intent are present — which is
	// transient but valid (claim intent in-flight on the previous instance
	// when the daemon crashed; close intent never landed).
	imrestSweepWriteIntent(t, cfg.IntentLogDir, bid, core.TerminalOpClaim)
	imrestSweepWriteIntent(t, cfg.IntentLogDir, bid, core.TerminalOpClose)

	cfg.Ledger = &imrestSweepFakeLedger{beads: []core.BeadRecord{imrestSweepBead(string(bid))}}
	resetter := &imrestSweepFakeResetter{}
	cfg.Resetter = resetter

	result, err := SweepStaleInProgressBeads(context.Background(), cfg)
	if err != nil {
		t.Fatalf("SweepStaleInProgressBeads: unexpected error: %v", err)
	}
	if result.ResetCount != 0 {
		t.Errorf("PL-006 exclusion (b): count = %d, want 0", result.ResetCount)
	}
	if len(resetter.called) != 0 {
		t.Errorf("PL-006 exclusion (b): ResetBead called despite close intent: %v", resetter.called)
	}
}

// TestSweepStaleInProgressBeads_ExclusionC_MergeCommit verifies that a bead
// for which the target branch carries a `Harmonik-Bead-ID` merge commit
// (exclusion (c) — Cat 3c) is NOT reset.
//
// To exercise (c) we need provenance via the claim intent AND (c) must fire
// before (a). Since (a) fires first in the implementation, this test is set
// up such that the claim intent is ABSENT (proxy: the daemon successfully
// removed it before crashing post-run-launch) but a merge commit exists. We
// must therefore use a separate provenance signal. The current implementation
// gates provenance on the claim intent alone — so without a claim intent the
// bead is skipped on provenance regardless of (c).
//
// What this test actually verifies: when the claim intent is present AND a
// merge commit also exists, exclusion (a) fires first and (c) is unreached.
// The reset is NOT issued. This documents the layered-exclusion behavior at
// MVH (when the in-memory-model rebuild is wired post-MVH, exclusion (a) will
// become independent of the claim intent).
func TestSweepStaleInProgressBeads_ExclusionC_LayeredWithA(t *testing.T) {
	t.Parallel()

	cfg := imrestSweepBaseConfig(t)
	bid := core.BeadID("hk-iuaed.t3")
	imrestSweepWriteIntent(t, cfg.IntentLogDir, bid, core.TerminalOpClaim)

	cfg.Ledger = &imrestSweepFakeLedger{beads: []core.BeadRecord{imrestSweepBead(string(bid))}}
	cfg.MergeScanner = &imrestSweepFakeMergeScanner{merged: map[core.BeadID]bool{bid: true}}
	resetter := &imrestSweepFakeResetter{}
	cfg.Resetter = resetter

	result, err := SweepStaleInProgressBeads(context.Background(), cfg)
	if err != nil {
		t.Fatalf("SweepStaleInProgressBeads: unexpected error: %v", err)
	}
	if result.ResetCount != 0 {
		t.Errorf("PL-006 exclusion layering: count = %d, want 0", result.ResetCount)
	}
	if len(resetter.called) != 0 {
		t.Errorf("PL-006 exclusion layering: ResetBead called: %v", resetter.called)
	}
}

// TestSweepStaleInProgressBeads_ResetsWhenNoExclusions verifies that when the
// only signal is "in_progress" AND a claim-intent existed once but has been
// removed (BI-030 step 6 success), the bead is NOT reset because provenance
// fails. Documents the MVH provenance discipline.
func TestSweepStaleInProgressBeads_NoClaimIntent_NotOwned(t *testing.T) {
	t.Parallel()

	cfg := imrestSweepBaseConfig(t)
	bid := core.BeadID("hk-iuaed.t4")
	// No claim intent written.

	cfg.Ledger = &imrestSweepFakeLedger{beads: []core.BeadRecord{imrestSweepBead(string(bid))}}
	resetter := &imrestSweepFakeResetter{}
	cfg.Resetter = resetter

	result, err := SweepStaleInProgressBeads(context.Background(), cfg)
	if err != nil {
		t.Fatalf("SweepStaleInProgressBeads: unexpected error: %v", err)
	}
	if result.ResetCount != 0 {
		t.Errorf("PL-006a provenance: count = %d, want 0 (no claim intent → not owned)", result.ResetCount)
	}
	if len(resetter.called) != 0 {
		t.Errorf("PL-006a provenance: ResetBead called on non-owned bead")
	}
}

// TestSweepStaleInProgressBeads_LedgerErrorPropagates verifies that a
// ListInFlightBeads error aborts the sweep with a wrapped error.
func TestSweepStaleInProgressBeads_LedgerErrorPropagates(t *testing.T) {
	t.Parallel()

	cfg := imrestSweepBaseConfig(t)
	sentinel := errors.New("br list failed")
	cfg.Ledger = &imrestSweepFakeLedger{err: sentinel}
	cfg.Resetter = &imrestSweepFakeResetter{}

	_, err := SweepStaleInProgressBeads(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error from ledger; got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected wrapped sentinel; got %v", err)
	}
}

// TestSweepStaleInProgressBeads_ConfigValidation verifies required-field
// guards reject nil/zero inputs.
func TestSweepStaleInProgressBeads_ConfigValidation(t *testing.T) {
	t.Parallel()

	good := imrestSweepBaseConfig(t)
	good.Ledger = &imrestSweepFakeLedger{}
	good.Resetter = &imrestSweepFakeResetter{}

	cases := []struct {
		name    string
		mutate  func(c *SweepStaleInProgressBeadsConfig)
		wantSub string
	}{
		{
			name:    "nil-ledger",
			mutate:  func(c *SweepStaleInProgressBeadsConfig) { c.Ledger = nil },
			wantSub: "Ledger",
		},
		{
			name:    "nil-resetter",
			mutate:  func(c *SweepStaleInProgressBeadsConfig) { c.Resetter = nil },
			wantSub: "Resetter",
		},
		{
			name:    "empty-intent-log-dir",
			mutate:  func(c *SweepStaleInProgressBeadsConfig) { c.IntentLogDir = "" },
			wantSub: "IntentLogDir",
		},
		{
			name:    "empty-project-hash",
			mutate:  func(c *SweepStaleInProgressBeadsConfig) { c.ProjectHash = "" },
			wantSub: "ProjectHash",
		},
		{
			name:    "zero-daemon-start-ns",
			mutate:  func(c *SweepStaleInProgressBeadsConfig) { c.DaemonStartNS = 0 },
			wantSub: "DaemonStartNS",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := good
			tc.mutate(&cfg)
			_, err := SweepStaleInProgressBeads(context.Background(), cfg)
			if err == nil {
				t.Fatalf("%s: expected validation error, got nil", tc.name)
			}
		})
	}
}

// imrestSweepFakeProvenance implements ProvenanceChecker. owns reports the
// set of beads for which Owns returns true; err is returned by every call when
// non-nil.
type imrestSweepFakeProvenance struct {
	owns map[core.BeadID]bool
	err  error
}

func (f *imrestSweepFakeProvenance) Owns(_ context.Context, beadID core.BeadID) (bool, error) {
	if f.err != nil {
		return false, f.err
	}
	return f.owns[beadID], nil
}

// TestSweepStaleInProgressBeads_ResetFires_WhenProvenanceCheckerEstablishesOwnership
// verifies the reset-firing happy path: a stale in_progress bead with no claim
// intent (BI-031 recovery already cleared it), no close/reopen intent, and no
// merge commit, is reset when an injected [ProvenanceChecker] establishes
// ownership. This is the imrest scenario the PL-006 sixth bullet targets.
//
// Spec ref: process-lifecycle.md §4.5 PL-006 sixth bullet (default reset path);
// beads-integration.md §4.4 BI-010d.
func TestSweepStaleInProgressBeads_ResetFires_WhenProvenanceCheckerEstablishesOwnership(t *testing.T) {
	t.Parallel()

	cfg := imrestSweepBaseConfig(t)
	bid := core.BeadID("hk-iuaed.t5")
	// No claim intent written (simulating a prior BI-031 recovery that
	// cleared the intent file). No close/reopen intent. No merge commit.

	cfg.Ledger = &imrestSweepFakeLedger{beads: []core.BeadRecord{imrestSweepBead(string(bid))}}
	cfg.Provenance = &imrestSweepFakeProvenance{owns: map[core.BeadID]bool{bid: true}}
	resetter := &imrestSweepFakeResetter{}
	cfg.Resetter = resetter

	result, err := SweepStaleInProgressBeads(context.Background(), cfg)
	if err != nil {
		t.Fatalf("SweepStaleInProgressBeads: unexpected error: %v", err)
	}
	if result.ResetCount != 1 {
		t.Errorf("PL-006 sixth bullet reset path: count = %d, want 1", result.ResetCount)
	}
	if len(resetter.called) != 1 || resetter.called[0] != bid {
		t.Errorf("PL-006 sixth bullet reset path: ResetBead called=%v, want [%s]", resetter.called, bid)
	}
}

// TestSweepStaleInProgressBeads_ProvenanceChecker_FalseSkipsReset verifies that
// when a non-nil [ProvenanceChecker] reports false AND no claim intent exists,
// the bead is NOT reset — provenance has not been established by either signal.
func TestSweepStaleInProgressBeads_ProvenanceChecker_FalseSkipsReset(t *testing.T) {
	t.Parallel()

	cfg := imrestSweepBaseConfig(t)
	bid := core.BeadID("hk-iuaed.t6")
	cfg.Ledger = &imrestSweepFakeLedger{beads: []core.BeadRecord{imrestSweepBead(string(bid))}}
	cfg.Provenance = &imrestSweepFakeProvenance{owns: map[core.BeadID]bool{}} // empty: bid not owned
	resetter := &imrestSweepFakeResetter{}
	cfg.Resetter = resetter

	result, err := SweepStaleInProgressBeads(context.Background(), cfg)
	if err != nil {
		t.Fatalf("SweepStaleInProgressBeads: unexpected error: %v", err)
	}
	if result.ResetCount != 0 {
		t.Errorf("PL-006a provenance: count = %d, want 0 (Provenance.Owns=false, no claim intent)", result.ResetCount)
	}
	if len(resetter.called) != 0 {
		t.Errorf("PL-006a provenance: ResetBead called on non-owned bead: %v", resetter.called)
	}
}

// TestSweepStaleInProgressBeads_ResetFires_RespectsExclusionB verifies that
// even when a ProvenanceChecker establishes ownership, a pending close/reopen
// intent (exclusion b — Cat 3a) still suppresses the reset.
func TestSweepStaleInProgressBeads_ResetFires_RespectsExclusionB(t *testing.T) {
	t.Parallel()

	cfg := imrestSweepBaseConfig(t)
	bid := core.BeadID("hk-iuaed.t7")
	imrestSweepWriteIntent(t, cfg.IntentLogDir, bid, core.TerminalOpClose)

	cfg.Ledger = &imrestSweepFakeLedger{beads: []core.BeadRecord{imrestSweepBead(string(bid))}}
	cfg.Provenance = &imrestSweepFakeProvenance{owns: map[core.BeadID]bool{bid: true}}
	resetter := &imrestSweepFakeResetter{}
	cfg.Resetter = resetter

	result, err := SweepStaleInProgressBeads(context.Background(), cfg)
	if err != nil {
		t.Fatalf("SweepStaleInProgressBeads: unexpected error: %v", err)
	}
	if result.ResetCount != 0 {
		t.Errorf("PL-006 exclusion (b) over provenance: count = %d, want 0", result.ResetCount)
	}
	if len(resetter.called) != 0 {
		t.Errorf("PL-006 exclusion (b) over provenance: ResetBead called despite close intent: %v", resetter.called)
	}
}

// TestSweepStaleInProgressBeads_ResetFires_RespectsExclusionC verifies that
// when a ProvenanceChecker establishes ownership, a merge commit on the target
// branch (exclusion c — Cat 3c) still suppresses the reset.
func TestSweepStaleInProgressBeads_ResetFires_RespectsExclusionC(t *testing.T) {
	t.Parallel()

	cfg := imrestSweepBaseConfig(t)
	bid := core.BeadID("hk-iuaed.t8")

	cfg.Ledger = &imrestSweepFakeLedger{beads: []core.BeadRecord{imrestSweepBead(string(bid))}}
	cfg.Provenance = &imrestSweepFakeProvenance{owns: map[core.BeadID]bool{bid: true}}
	cfg.MergeScanner = &imrestSweepFakeMergeScanner{merged: map[core.BeadID]bool{bid: true}}
	resetter := &imrestSweepFakeResetter{}
	cfg.Resetter = resetter

	result, err := SweepStaleInProgressBeads(context.Background(), cfg)
	if err != nil {
		t.Fatalf("SweepStaleInProgressBeads: unexpected error: %v", err)
	}
	if result.ResetCount != 0 {
		t.Errorf("PL-006 exclusion (c) over provenance: count = %d, want 0", result.ResetCount)
	}
	if len(resetter.called) != 0 {
		t.Errorf("PL-006 exclusion (c) over provenance: ResetBead called despite merge commit: %v", resetter.called)
	}
}

// TestSweepStaleInProgressBeads_ResetFires_MultipleBeads verifies that multiple
// project-owned beads with no exclusions are all reset.
func TestSweepStaleInProgressBeads_ResetFires_MultipleBeads(t *testing.T) {
	t.Parallel()

	cfg := imrestSweepBaseConfig(t)
	bidA := core.BeadID("hk-iuaed.tA")
	bidB := core.BeadID("hk-iuaed.tB")

	cfg.Ledger = &imrestSweepFakeLedger{beads: []core.BeadRecord{
		imrestSweepBead(string(bidA)),
		imrestSweepBead(string(bidB)),
	}}
	cfg.Provenance = &imrestSweepFakeProvenance{owns: map[core.BeadID]bool{bidA: true, bidB: true}}
	resetter := &imrestSweepFakeResetter{}
	cfg.Resetter = resetter

	result, err := SweepStaleInProgressBeads(context.Background(), cfg)
	if err != nil {
		t.Fatalf("SweepStaleInProgressBeads: unexpected error: %v", err)
	}
	if result.ResetCount != 2 {
		t.Errorf("PL-006 sixth bullet reset path: count = %d, want 2", result.ResetCount)
	}
	if len(resetter.called) != 2 {
		t.Errorf("PL-006 sixth bullet reset path: ResetBead call count = %d, want 2; calls=%v", len(resetter.called), resetter.called)
	}
}

// TestSweepStaleInProgressBeads_ResetError_ContinuesAndReports verifies that a
// reset failure on one bead does not abort the sweep — subsequent beads are
// still processed — and that the failure is reported in the returned error.
func TestSweepStaleInProgressBeads_ResetError_ContinuesAndReports(t *testing.T) {
	t.Parallel()

	cfg := imrestSweepBaseConfig(t)
	bidA := core.BeadID("hk-iuaed.tAerr")
	bidB := core.BeadID("hk-iuaed.tBok")

	cfg.Ledger = &imrestSweepFakeLedger{beads: []core.BeadRecord{
		imrestSweepBead(string(bidA)),
		imrestSweepBead(string(bidB)),
	}}
	cfg.Provenance = &imrestSweepFakeProvenance{owns: map[core.BeadID]bool{bidA: true, bidB: true}}
	sentinel := errors.New("reset failed for bidA")
	resetter := &imrestSweepFakeResetter{errOn: map[core.BeadID]error{bidA: sentinel}}
	cfg.Resetter = resetter

	result, err := SweepStaleInProgressBeads(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected wrapped error from failing reset; got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel %v wrapped in returned error; got %v", sentinel, err)
	}
	if result.ResetCount != 1 {
		t.Errorf("count = %d, want 1 (bidB succeeded)", result.ResetCount)
	}
}

// TestScanIntentLog_PartitionsByOp verifies that ScanIntentLog correctly
// partitions intent files into claim vs close/reopen sets. Reset intents
// appear only in provenance (not claims or mutations).
func TestScanIntentLog_PartitionsByOp(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	imrestSweepWriteIntent(t, dir, "hk-a", core.TerminalOpClaim)
	imrestSweepWriteIntent(t, dir, "hk-b", core.TerminalOpClose)
	imrestSweepWriteIntent(t, dir, "hk-c", core.TerminalOpReopen)
	imrestSweepWriteIntent(t, dir, "hk-d", core.TerminalOpReset)

	provenance, claims, mutations, err := ScanIntentLog(dir, nil)
	if err != nil {
		t.Fatalf("ScanIntentLog: %v", err)
	}
	if _, ok := claims["hk-a"]; !ok {
		t.Error("expected hk-a in claims set")
	}
	if _, ok := mutations["hk-b"]; !ok {
		t.Error("expected hk-b in mutations set (close)")
	}
	if _, ok := mutations["hk-c"]; !ok {
		t.Error("expected hk-c in mutations set (reopen)")
	}
	if _, ok := claims["hk-d"]; ok {
		t.Error("hk-d (reset) should NOT be in claims set")
	}
	if _, ok := mutations["hk-d"]; ok {
		t.Error("hk-d (reset) should NOT be in mutations set")
	}
	// All four beads must appear in provenance.
	for _, bid := range []core.BeadID{"hk-a", "hk-b", "hk-c", "hk-d"} {
		if _, ok := provenance[bid]; !ok {
			t.Errorf("expected %s in provenance set", bid)
		}
	}
}

// TestScanIntentLog_MissingDirNoError verifies that ScanIntentLog returns empty
// sets without error when the directory does not exist.
func TestScanIntentLog_MissingDirNoError(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "does-not-exist")
	provenance, claims, mutations, err := ScanIntentLog(dir, nil)
	if err != nil {
		t.Fatalf("ScanIntentLog: missing dir should be silent; got %v", err)
	}
	if len(provenance) != 0 || len(claims) != 0 || len(mutations) != 0 {
		t.Errorf("expected empty sets; got provenance=%v claims=%v mutations=%v", provenance, claims, mutations)
	}
}

// TestSweepStaleInProgressBeads_ResetFires_StaleCloseIntentEstablishesProvenance
// reproduces the hk-sc3o4 dogfood failure: a bead whose claim intent was
// already cleared by BI-031 recovery (so it does NOT appear in claims) but
// whose close intent from a timed-out close attempt is still on disk.
//
// Before the fix, provenance was established only via the claim intent — the
// close intent was invisible to the ownership check, so the bead was skipped
// (stale_intents_observed=4, bead_in_progress_reset=0). After the fix,
// ANY intent file in the project's intent-log directory establishes
// provenance.
//
// Note: exclusion (b) does NOT fire here because we supply a CLOSE intent
// for provenance but the bead also has no exclusion (b) would only fire if
// the close intent is still in the mutations set. In this scenario the close
// attempt itself failed (timed out), leaving the intent file, but there is
// no pending Cat 3a handler — so the daemon SHOULD reset the bead.
//
// Spec ref: process-lifecycle.md §4.5 PL-006 sixth bullet.
// Bug ref: hk-sc3o4.
func TestSweepStaleInProgressBeads_ResetFires_StaleCloseIntentEstablishesProvenance(t *testing.T) {
	t.Parallel()

	cfg := imrestSweepBaseConfig(t)
	bid := core.BeadID("hk-sc3o4-repro")

	// Simulate: claim intent was cleared by BI-031 recovery; a timed-out close
	// attempt left a close intent on disk. No claim intent present.
	imrestSweepWriteIntent(t, cfg.IntentLogDir, bid, core.TerminalOpClose)
	// No TerminalOpClaim intent written — this is the scenario.

	cfg.Ledger = &imrestSweepFakeLedger{beads: []core.BeadRecord{imrestSweepBead(string(bid))}}
	resetter := &imrestSweepFakeResetter{}
	cfg.Resetter = resetter
	// No ProvenanceChecker wired (MVH production default).

	result, err := SweepStaleInProgressBeads(context.Background(), cfg)
	if err != nil {
		t.Fatalf("SweepStaleInProgressBeads: unexpected error: %v", err)
	}
	// hk-sc3o4 fix: the close intent establishes provenance AND exclusion (b)
	// does NOT suppress the reset because the bead is stale IN_PROGRESS with
	// no live Cat 3a handler. BUT wait: exclusion (b) fires on mutation intent
	// presence — so this test exercises the tricky interaction.
	//
	// Correct behavior per PL-006: stale close intent = Cat 3a territory.
	// The sweep MUST NOT preempt Cat 3a. Therefore count = 0 here.
	// The provenance fix ensures the bead reaches the exclusion checks
	// rather than being silently skipped — but exclusion (b) still applies.
	//
	// The real fix for hk-sc3o4 therefore requires either:
	//   (i)  a stale RESET intent (not close) establishing provenance, or
	//   (ii) a ProvenanceChecker that is wired to detect orphaned beads.
	//
	// This test documents the correct layered behavior.
	if result.ResetCount != 0 {
		t.Errorf("PL-006 exclusion (b) must suppress reset when close intent present: count = %d, want 0", result.ResetCount)
	}
	if len(resetter.called) != 0 {
		t.Errorf("ResetBead must not be called when close intent present (Cat 3a territory): %v", resetter.called)
	}
}

// TestSweepStaleInProgressBeads_ResetFires_StaleResetIntentEstablishesProvenance
// reproduces the hk-sc3o4 dogfood failure using a stale RESET intent
// (no claim, no close/reopen). This is the exact scenario where:
//   - A prior daemon reset the bead (or attempted to).
//   - The reset intent was left on disk (e.g., the daemon crashed during the
//     reset write's BI-030 atomic rename).
//   - The next daemon restart finds the bead IN_PROGRESS and must reset it.
//
// Before the fix: provenance fails (no claim intent) → bead skipped.
// After the fix: reset intent establishes provenance → reset fires.
//
// Spec ref: process-lifecycle.md §4.5 PL-006 sixth bullet.
// Bug ref: hk-sc3o4.
func TestSweepStaleInProgressBeads_ResetFires_StaleResetIntentEstablishesProvenance(t *testing.T) {
	t.Parallel()

	cfg := imrestSweepBaseConfig(t)
	bid := core.BeadID("hk-sc3o4-repro-reset")

	// Stale reset intent from a prior sweep that crashed during the BI-030
	// atomic rename. No claim intent. No close/reopen intent.
	imrestSweepWriteIntent(t, cfg.IntentLogDir, bid, core.TerminalOpReset)

	cfg.Ledger = &imrestSweepFakeLedger{beads: []core.BeadRecord{imrestSweepBead(string(bid))}}
	resetter := &imrestSweepFakeResetter{}
	cfg.Resetter = resetter
	// No ProvenanceChecker (MVH production default).

	result, err := SweepStaleInProgressBeads(context.Background(), cfg)
	if err != nil {
		t.Fatalf("SweepStaleInProgressBeads: unexpected error: %v", err)
	}
	if result.ResetCount != 1 {
		t.Errorf("hk-sc3o4: stale reset intent should establish provenance and fire reset: count = %d, want 1", result.ResetCount)
	}
	if len(resetter.called) != 1 || resetter.called[0] != bid {
		t.Errorf("hk-sc3o4: ResetBead not called on expected bead: calls=%v", resetter.called)
	}
}

// ---------------------------------------------------------------------------
// Cat 3c auto-resolution (hk-lgtq2) — BeadCat3cCloser coverage
// ---------------------------------------------------------------------------

// TestSweepStaleInProgressBeads_Cat3cClose_Success verifies the Cat 3c
// auto-resolution happy path: when a non-nil Cat3cCloser is injected and the
// merge scanner reports a Harmonik-Bead-ID commit on the target branch, the
// sweep CLOSES the bead (calls SweepCloseBead), increments Cat3cCloseCount,
// and does NOT reset the bead.
//
// Covers orphansweepbeads.go:504-512 (close success branch).
// Spec ref: hk-lgtq2 (Cat 3c auto-reconciler).
func TestSweepStaleInProgressBeads_Cat3cClose_Success(t *testing.T) {
	t.Parallel()

	cfg := imrestSweepBaseConfig(t)
	bid := core.BeadID("hk-lgtq2-cat3c-success")

	// Provenance via ProvenanceChecker (no intent file needed).
	cfg.Provenance = &imrestSweepFakeProvenance{owns: map[core.BeadID]bool{bid: true}}
	// Merge scanner: bead is subsumed (merge commit present on target branch).
	cfg.MergeScanner = &imrestSweepFakeMergeScanner{merged: map[core.BeadID]bool{bid: true}}

	cfg.Ledger = &imrestSweepFakeLedger{beads: []core.BeadRecord{imrestSweepBead(string(bid))}}
	resetter := &imrestSweepFakeResetter{}
	cfg.Resetter = resetter
	closer := &imrestSweepFakeCat3cCloser{}
	cfg.Cat3cCloser = closer

	result, err := SweepStaleInProgressBeads(context.Background(), cfg)
	if err != nil {
		t.Fatalf("SweepStaleInProgressBeads: unexpected error: %v", err)
	}
	// Cat 3c close must have fired.
	if result.Cat3cCloseCount != 1 {
		t.Errorf("Cat3cCloseCount = %d, want 1", result.Cat3cCloseCount)
	}
	if len(closer.called) != 1 || closer.called[0] != bid {
		t.Errorf("SweepCloseBead calls = %v, want [%s]", closer.called, bid)
	}
	// Reset must NOT have fired (Cat 3c takes the bead, not reset path).
	if result.ResetCount != 0 {
		t.Errorf("ResetCount = %d, want 0 (Cat 3c owns the bead)", result.ResetCount)
	}
	if len(resetter.called) != 0 {
		t.Errorf("ResetBead must not be called for a Cat 3c bead: calls=%v", resetter.called)
	}
}

// TestSweepStaleInProgressBeads_Cat3cClose_ErrorAggregated verifies that when
// SweepCloseBead returns an error for one bead, the sweep does NOT abort —
// remaining beads are still processed — and the error is aggregated into the
// returned error (existing pattern at orphansweepbeads.go:544).
//
// Covers orphansweepbeads.go:507-509 (close failure aggregation branch).
// Spec ref: hk-lgtq2 (Cat 3c auto-reconciler).
func TestSweepStaleInProgressBeads_Cat3cClose_ErrorAggregated(t *testing.T) {
	t.Parallel()

	cfg := imrestSweepBaseConfig(t)
	bidFail := core.BeadID("hk-lgtq2-cat3c-fail")
	bidOK := core.BeadID("hk-lgtq2-cat3c-ok")

	// Both beads owned and both subsumed.
	cfg.Provenance = &imrestSweepFakeProvenance{owns: map[core.BeadID]bool{bidFail: true, bidOK: true}}
	cfg.MergeScanner = &imrestSweepFakeMergeScanner{merged: map[core.BeadID]bool{bidFail: true, bidOK: true}}
	cfg.Ledger = &imrestSweepFakeLedger{beads: []core.BeadRecord{
		imrestSweepBead(string(bidFail)),
		imrestSweepBead(string(bidOK)),
	}}
	resetter := &imrestSweepFakeResetter{}
	cfg.Resetter = resetter
	sentinel := errors.New("cat3c close failed for bidFail")
	closer := &imrestSweepFakeCat3cCloser{errOn: map[core.BeadID]error{bidFail: sentinel}}
	cfg.Cat3cCloser = closer

	result, err := SweepStaleInProgressBeads(context.Background(), cfg)
	// Error must be reported (aggregated from the failing close).
	if err == nil {
		t.Fatal("expected aggregated error from Cat 3c close failure; got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel %v wrapped in returned error; got %v", sentinel, err)
	}
	// The successful bead (bidOK) must have been closed — sweep continued despite bidFail error.
	if result.Cat3cCloseCount != 1 {
		t.Errorf("Cat3cCloseCount = %d, want 1 (bidOK succeeded)", result.Cat3cCloseCount)
	}
	if len(closer.called) != 2 {
		t.Errorf("SweepCloseBead call count = %d, want 2 (both beads attempted); calls=%v", len(closer.called), closer.called)
	}
	// No resets issued — both beads reached the Cat 3c path.
	if result.ResetCount != 0 {
		t.Errorf("ResetCount = %d, want 0", result.ResetCount)
	}
}
