package sentinel_test

// layera_hkl087e_test.go — unit tests for the Layer A stall detectors.
//
// Tests are table-driven and operate against synthetic Snapshots, so they
// run without filesystem I/O and complete in milliseconds.
//
// Coverage:
//   - heartbeat_gap fires when LastEventAge > RunSilenceStall
//   - review_stall fires when Phase==VerdictFired && time-since-verdict > ReviewFinalizeStall
//   - run_age fires when time-since-start > RunMaxAge
//   - terminal runs are never flagged
//   - a run can produce multiple hits in one pass
//   - zero-value config returns ErrLayerAConfigInvalid (fail-loud)
//
// Spec: .kerf/works/stall-sentinel/02-analysis.md §Layer A, DESIGN.md §2.
// Bead: hk-l087e.

import (
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/sentinel"
)

// --- helpers ---

func makeSnapshot(now time.Time, runs ...sentinel.RunSignal) sentinel.Snapshot {
	m := make(map[string]sentinel.RunSignal, len(runs))
	for _, r := range runs {
		m[r.RunID] = r
	}
	return sentinel.Snapshot{Now: now, Runs: m, Lanes: map[string]sentinel.LaneSignal{}}
}

func baseRun(id, beadID string, startedAt time.Time) sentinel.RunSignal {
	return sentinel.RunSignal{
		RunID:        id,
		BeadID:       beadID,
		LaneName:     "default",
		StartedAt:    startedAt,
		LastEventAt:  startedAt,
		LastEventAge: 0,
		Phase:        sentinel.RunPhaseStarted,
	}
}

// withLastEvent returns rs with LastEventAt set to t and LastEventAge recomputed
// against snap.Now. Call after makeSnapshot so Now is known.
func withLastEvent(rs sentinel.RunSignal, now, t time.Time) sentinel.RunSignal {
	rs.LastEventAt = t
	rs.LastEventAge = now.Sub(t)
	return rs
}

func withPhase(rs sentinel.RunSignal, p sentinel.RunPhase) sentinel.RunSignal {
	rs.Phase = p
	return rs
}

func withVerdict(rs sentinel.RunSignal, verdictAt time.Time) sentinel.RunSignal {
	rs.Phase = sentinel.RunPhaseVerdictFired
	rs.VerdictAt = verdictAt
	return rs
}

// defaultCfg returns a valid LayerAConfig.
func defaultCfg() sentinel.LayerAConfig {
	return sentinel.LayerAConfig{
		RunSilenceStall:     20 * time.Minute,
		ReviewFinalizeStall: 10 * time.Minute,
		RunMaxAge:           60 * time.Minute,
	}
}

// --- tests ---

func TestDetectLayerA_HeartbeatGap(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_000_000, 0)
	started := now.Add(-30 * time.Minute)

	run1 := baseRun("run-1", "bead-1", started)
	// Last event was 25 min ago — exceeds RunSilenceStall (20m).
	run1 = withLastEvent(run1, now, now.Add(-25*time.Minute))

	snap := makeSnapshot(now, run1)
	hits, err := sentinel.DetectLayerA(snap, defaultCfg())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var hbHits []sentinel.StallHit
	for _, h := range hits {
		if h.Signature == core.StallSignatureHeartbeatGap {
			hbHits = append(hbHits, h)
		}
	}
	if len(hbHits) != 1 {
		t.Fatalf("want 1 heartbeat_gap hit, got %d", len(hbHits))
	}
	h := hbHits[0]
	if h.RunID != "run-1" {
		t.Errorf("wrong RunID: %q", h.RunID)
	}
	if h.BeadID != "bead-1" {
		t.Errorf("wrong BeadID: %q", h.BeadID)
	}
	if h.Elapsed < 25*time.Minute {
		t.Errorf("elapsed too short: %v", h.Elapsed)
	}
}

func TestDetectLayerA_HeartbeatGap_BelowThreshold(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_000_000, 0)
	started := now.Add(-30 * time.Minute)

	run1 := baseRun("run-1", "bead-1", started)
	// Last event was 5 min ago — below RunSilenceStall (20m). Should NOT fire.
	run1 = withLastEvent(run1, now, now.Add(-5*time.Minute))

	snap := makeSnapshot(now, run1)
	hits, err := sentinel.DetectLayerA(snap, defaultCfg())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, h := range hits {
		if h.Signature == core.StallSignatureHeartbeatGap {
			t.Errorf("heartbeat_gap fired but should not have: RunID=%q elapsed=%v", h.RunID, h.Elapsed)
		}
	}
}

func TestDetectLayerA_ReviewStall(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_000_000, 0)
	started := now.Add(-30 * time.Minute)
	// reviewer_verdict fired 15 min ago; ReviewFinalizeStall is 10m — exceeds threshold.
	verdictAt := now.Add(-15 * time.Minute)

	run1 := baseRun("run-1", "bead-1", started)
	run1 = withLastEvent(run1, now, verdictAt) // last event is the verdict
	run1 = withVerdict(run1, verdictAt)

	snap := makeSnapshot(now, run1)
	hits, err := sentinel.DetectLayerA(snap, defaultCfg())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var rvHits []sentinel.StallHit
	for _, h := range hits {
		if h.Signature == core.StallSignatureReviewStall {
			rvHits = append(rvHits, h)
		}
	}
	if len(rvHits) != 1 {
		t.Fatalf("want 1 review_stall hit, got %d", len(rvHits))
	}
	h := rvHits[0]
	if h.RunID != "run-1" {
		t.Errorf("wrong RunID: %q", h.RunID)
	}
	if h.Elapsed < 15*time.Minute {
		t.Errorf("elapsed too short: %v", h.Elapsed)
	}
}

func TestDetectLayerA_ReviewStall_NotVerdictPhase(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_000_000, 0)
	started := now.Add(-30 * time.Minute)

	// A run in RunPhaseInImplementation with a long gap — review_stall must NOT fire.
	run1 := baseRun("run-1", "bead-1", started)
	run1 = withLastEvent(run1, now, now.Add(-25*time.Minute))
	run1 = withPhase(run1, sentinel.RunPhaseInImplementation)

	snap := makeSnapshot(now, run1)
	hits, err := sentinel.DetectLayerA(snap, defaultCfg())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, h := range hits {
		if h.Signature == core.StallSignatureReviewStall {
			t.Errorf("review_stall fired on non-verdict phase: RunID=%q elapsed=%v", h.RunID, h.Elapsed)
		}
	}
}

func TestDetectLayerA_RunAge(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_000_000, 0)
	// Run started 90 min ago; RunMaxAge is 60m — exceeds threshold.
	started := now.Add(-90 * time.Minute)

	run1 := baseRun("run-1", "bead-1", started)
	run1 = withLastEvent(run1, now, now.Add(-5*time.Minute)) // recent heartbeat, but too old overall

	snap := makeSnapshot(now, run1)
	hits, err := sentinel.DetectLayerA(snap, defaultCfg())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var ageHits []sentinel.StallHit
	for _, h := range hits {
		if h.Signature == core.StallSignatureRunAge {
			ageHits = append(ageHits, h)
		}
	}
	if len(ageHits) != 1 {
		t.Fatalf("want 1 run_age hit, got %d", len(ageHits))
	}
	h := ageHits[0]
	if h.RunID != "run-1" {
		t.Errorf("wrong RunID: %q", h.RunID)
	}
	if h.Elapsed < 90*time.Minute {
		t.Errorf("elapsed too short: %v", h.Elapsed)
	}
}

func TestDetectLayerA_TerminalRunSkipped(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_000_000, 0)
	// A terminal run that is very old — no signature should fire.
	started := now.Add(-120 * time.Minute)

	run1 := baseRun("run-1", "bead-1", started)
	run1 = withLastEvent(run1, now, now.Add(-60*time.Minute))
	run1 = withPhase(run1, sentinel.RunPhaseTerminal)

	snap := makeSnapshot(now, run1)
	hits, err := sentinel.DetectLayerA(snap, defaultCfg())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("expected no hits for terminal run, got %d", len(hits))
	}
}

func TestDetectLayerA_MultipleHitsPerRun(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_000_000, 0)
	// Run started 90 min ago (exceeds RunMaxAge 60m) AND has been silent
	// for 25 min (exceeds RunSilenceStall 20m).
	started := now.Add(-90 * time.Minute)
	lastEvent := now.Add(-25 * time.Minute)

	run1 := baseRun("run-1", "bead-1", started)
	run1 = withLastEvent(run1, now, lastEvent)

	snap := makeSnapshot(now, run1)
	hits, err := sentinel.DetectLayerA(snap, defaultCfg())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sigs := make(map[core.StallSignature]bool)
	for _, h := range hits {
		if h.RunID != "run-1" {
			t.Errorf("unexpected RunID: %q", h.RunID)
		}
		sigs[h.Signature] = true
	}
	if !sigs[core.StallSignatureHeartbeatGap] {
		t.Error("expected heartbeat_gap hit")
	}
	if !sigs[core.StallSignatureRunAge] {
		t.Error("expected run_age hit")
	}
}

func TestDetectLayerA_ConfigInvalid_ZeroRunSilenceStall(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_000_000, 0)
	snap := makeSnapshot(now)

	cfg := defaultCfg()
	cfg.RunSilenceStall = 0

	_, err := sentinel.DetectLayerA(snap, cfg)
	if err == nil {
		t.Fatal("expected ErrLayerAConfigInvalid, got nil")
	}
	var ce *sentinel.ErrLayerAConfigInvalid
	if !isLayerAConfigInvalid(err, &ce) {
		t.Errorf("wrong error type: %T: %v", err, err)
	}
}

func TestDetectLayerA_ConfigInvalid_ZeroReviewFinalizeStall(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_000_000, 0)
	snap := makeSnapshot(now)

	cfg := defaultCfg()
	cfg.ReviewFinalizeStall = 0

	_, err := sentinel.DetectLayerA(snap, cfg)
	if err == nil {
		t.Fatal("expected ErrLayerAConfigInvalid, got nil")
	}
}

func TestDetectLayerA_ConfigInvalid_ZeroRunMaxAge(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_000_000, 0)
	snap := makeSnapshot(now)

	cfg := defaultCfg()
	cfg.RunMaxAge = 0

	_, err := sentinel.DetectLayerA(snap, cfg)
	if err == nil {
		t.Fatal("expected ErrLayerAConfigInvalid, got nil")
	}
}

func TestDetectLayerA_EmptySnapshot_NoHits(t *testing.T) {
	t.Parallel()
	now := time.Unix(1_000_000, 0)
	snap := makeSnapshot(now)

	hits, err := sentinel.DetectLayerA(snap, defaultCfg())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(hits) != 0 {
		t.Errorf("expected no hits on empty snapshot, got %d", len(hits))
	}
}

func TestStallHit_StallDetectedPayload(t *testing.T) {
	t.Parallel()
	h := sentinel.StallHit{
		RunID:     "run-abc",
		BeadID:    "bead-xyz",
		LaneName:  "default",
		Signature: core.StallSignatureHeartbeatGap,
		Elapsed:   25*time.Minute + 30*time.Second,
	}
	p := h.StallDetectedPayload()
	if p.RunID != "run-abc" {
		t.Errorf("RunID: got %q want %q", p.RunID, "run-abc")
	}
	if p.BeadID != "bead-xyz" {
		t.Errorf("BeadID: got %q want %q", p.BeadID, "bead-xyz")
	}
	if p.Signature != core.StallSignatureHeartbeatGap {
		t.Errorf("Signature: got %q want %q", p.Signature, core.StallSignatureHeartbeatGap)
	}
	wantMs := h.Elapsed.Milliseconds()
	if p.ElapsedMs != wantMs {
		t.Errorf("ElapsedMs: got %d want %d", p.ElapsedMs, wantMs)
	}
	if !p.Valid() {
		t.Error("payload.Valid() returned false")
	}
}

// isLayerAConfigInvalid is a type-assertion helper to avoid importing errors.As
// in a test package that doesn't have access to the internal struct directly.
// The sentinel package exports ErrLayerAConfigInvalid so we can do a direct
// type assertion.
func isLayerAConfigInvalid(err error, out **sentinel.ErrLayerAConfigInvalid) bool {
	if e, ok := err.(*sentinel.ErrLayerAConfigInvalid); ok {
		*out = e
		return true
	}
	return false
}
