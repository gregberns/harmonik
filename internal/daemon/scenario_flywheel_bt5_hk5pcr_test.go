//go:build scenario

package daemon

// scenario_flywheel_bt5_hk5pcr_test.go — BT5 flywheel scenario tests.
//
// Three end-to-end behaviours of the flywheel-motion v1 slice are exercised
// here against REAL production code (no stubs for the logic under test):
//
//	BT5-1  G-liveness halt        — flywheel-motion.md §6.1
//	BT5-2  work-gen-once          — flywheel-motion.md §5.4 (B)
//	BT5-3  ledger-survives-restart — flywheel-motion.md §5.4 B guardrail 4 (AC1)
//
// # BT5-1 — G-liveness halt (the inverted-metric self-kill)
//
// The governor's G-liveness gate (sentinel.Evaluate) counts consecutive
// evaluation cycles with zero terminal progress (MovementScore == 0). After N
// such cycles (Config.LivenessNoProgressN) it returns ActivationHalt with
// LivenessViolated=true; the workloop ACT-mode path then emits a liveness_halt
// page event and halts dispatch (workloop.go:1559-1571). This test drives N
// consecutive zero-progress Evaluate calls against a real empty events.jsonl
// and asserts:
//   - cycles 1..N-1 do NOT halt (Level != ActivationHalt, LivenessViolated=false),
//   - cycle N DOES halt (ActivationHalt + LivenessViolated=true),
//   - a single terminal-progress event (bead_closed) RESETS the counter, so the
//     gate does not fire on the cycle that observes movement (the doom-loop is
//     genuinely about SUSTAINED no-progress, not a momentary lull).
//
// # BT5-2 — work-gen-once (deploy-class completion stages exactly ONE bead)
//
// stagedBeadGeneratorEval is the real §5.4 (B) staged-bead generator. On a
// Phase-1 completion of a deploy-relevant class it calls `br create --status
// open ... --label needs-greenlight` EXACTLY ONCE — the bead lands OPEN
// (guardrail 2: never auto-dispatched the same tick) and carries the
// needs-greenlight label (AC2 captain-greenlight gate). This test drives a real
// deploy-class completion through the generator with a fake `br` binary on disk
// (so `br create` is REAL exec, observed via its argv recording) and asserts
// exactly one create, with --status open and needs-greenlight.
//
// # BT5-3 — ledger-survives-restart (durable at-most-once across daemon restart)
//
// The AC1 durable ledger (followup_ledger_ac1.go, hk-3ndb) persists each
// (target_bead_id, follow_up_class) key to .harmonik/follow-up-ledger.jsonl on
// a successful create. On daemon restart the in-memory followUpLedger is
// re-made and re-seeded via loadFollowUpLedger. This test runs the generator
// once (→ exactly one staged bead + one on-disk ledger entry), then SIMULATES A
// DAEMON RESTART (fresh workLoopDeps whose followUpLedger is re-seeded from the
// persisted file exactly as daemon boot does) and REPLAYS the same completion:
// the generator must be a no-op — STILL exactly one staged bead, no duplicate
// tail. Because AC1 is landed on main, this case PASSES (it is not skipped).
//
// # Why //go:build scenario
//
// These tests perform full durable round-trips: real events.jsonl I/O, a real
// fake-br exec for `br create`, and a real load-from-disk ledger replay. They
// exceed the daemon's 30-min commit-gate budget on a loaded box, so they are
// tagged scenario and run only on the explicit scenario gate.
//
// # Helper reuse
//
// Reuses bt4WarmState / bt4WriteMoveEvent from the BT4 sentinel scenario test
// and writePhase2Config / writeFakeBrArgScript / stagedBeadFixtureDeps /
// writeTestFile from eagerfill_em063_test.go. New helpers carry the "bt5"
// prefix per the helper-prefix discipline.
//
// Run independently (the daemon gate skips //go:build scenario):
//
//	go test -tags=scenario -run BT5 ./internal/daemon/...
//
// Spec ref: flywheel-motion.md §§5.4, 6.1, 9.1. Bead: hk-5pcr. Epic: hk-0oca.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/sentinel"
)

// ---------------------------------------------------------------------------
// BT5 helpers
// ---------------------------------------------------------------------------

// bt5LivenessConfig returns a governor Config whose G-liveness gate fires after
// n consecutive zero-progress cycles. Warmup is satisfied by bt4WarmState
// (DaemonStartedAt = now-1h), so the §1.4 warmup suppression of the halt gate
// is already past.
func bt5LivenessConfig(n int) sentinel.Config {
	return sentinel.Config{
		Window:              30 * time.Minute,
		WarmupWindow:        30 * time.Minute, // satisfied by state.DaemonStartedAt = now-1h
		SustainedWindows:    2,
		LivenessNoProgressN: n,
	}
}

// bt5CountBrCreateCalls reads a fake-br argv-recording file (one line per `br`
// invocation, joined args) and returns the number of lines that contain
// "create". Used to assert work-gen-once (exactly one create) and
// ledger-survives-restart (still exactly one create after replay).
func bt5CountBrCreateCalls(t *testing.T, argsFile string) int {
	t.Helper()
	data, err := os.ReadFile(argsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return 0 // br never called
		}
		t.Fatalf("bt5CountBrCreateCalls: read %s: %v", argsFile, err)
	}
	count := 0
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, "create") {
			count++
		}
	}
	return count
}

// ---------------------------------------------------------------------------
// BT5-1 — G-liveness halt
// ---------------------------------------------------------------------------

// TestScenario_Flywheel_BT5_1_GLivenessHalt exercises the §6.1 G-liveness gate:
// N consecutive zero-progress cycles → ActivationHalt; N-1 cycles do NOT halt;
// and a single terminal-progress event resets the doom-loop counter.
//
// Drives the REAL sentinel.Evaluate against a real empty events.jsonl (zero
// movement each cycle). This is exactly the function the workloop ACT-mode path
// calls each tick (workloop.go:1548); ActivationHalt there triggers the
// liveness_halt page + dispatch halt (workloop.go:1559-1571).
func TestScenario_Flywheel_BT5_1_GLivenessHalt(t *testing.T) {
	ctx := context.Background()
	projectDir := bt4ProjectDir(t) // .harmonik/events/events.jsonl exists, empty
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

	const n = 4
	state := bt4WarmState(now) // DaemonStartedAt = now-1h → warmup satisfied
	cfg := bt5LivenessConfig(n)

	// Opportunity present (ready beads) so the halt is the only escalation in play,
	// not a "no_opportunity" suppression — though the halt gate runs BEFORE the
	// opportunity gate, this keeps the input realistic for the ACT-mode tick.
	input := sentinel.GovernorInput{
		ProjectDir:    projectDir,
		Now:           now,
		HasReadyBeads: true,
	}

	// ── Cycles 1..N-1 must NOT halt ─────────────────────────────────────────
	for i := 1; i < n; i++ {
		sig := sentinel.Evaluate(ctx, state, input, cfg)
		if sig.Level == sentinel.ActivationHalt {
			t.Fatalf("BT5-1: cycle %d/%d halted early (LivenessViolated=%v, consecutiveZero=%d) — N-1 cycles MUST NOT halt",
				i, n, sig.LivenessViolated, sig.ConsecutiveZeroCycles)
		}
		if sig.LivenessViolated {
			t.Fatalf("BT5-1: cycle %d/%d set LivenessViolated before reaching N=%d", i, n, n)
		}
		if sig.ConsecutiveZeroCycles != i {
			t.Errorf("BT5-1: cycle %d: ConsecutiveZeroCycles = %d, want %d", i, sig.ConsecutiveZeroCycles, i)
		}
	}

	// ── Cycle N MUST halt ───────────────────────────────────────────────────
	sigN := sentinel.Evaluate(ctx, state, input, cfg)
	if sigN.Level != sentinel.ActivationHalt {
		t.Fatalf("BT5-1: cycle %d: expected ActivationHalt, got %s (consecutiveZero=%d)",
			n, sigN.Level, sigN.ConsecutiveZeroCycles)
	}
	if !sigN.LivenessViolated {
		t.Errorf("BT5-1: cycle %d: LivenessViolated must be true at halt", n)
	}
	if sigN.ConsecutiveZeroCycles != n {
		t.Errorf("BT5-1: cycle %d: ConsecutiveZeroCycles = %d, want %d", n, sigN.ConsecutiveZeroCycles, n)
	}

	// ── Movement resets the doom-loop counter (sustained, not momentary) ─────
	// Fresh state; accumulate N-1 zero cycles, then inject one terminal-progress
	// event (bead_closed) in-window. The next Evaluate must see score>0, reset
	// ConsecutiveZeroCycles to 0, and NOT halt.
	state2 := bt4WarmState(now)
	for i := 1; i < n; i++ {
		sig := sentinel.Evaluate(ctx, state2, input, cfg)
		if sig.Level == sentinel.ActivationHalt {
			t.Fatalf("BT5-1 reset-arm: cycle %d halted before movement injected", i)
		}
	}
	// Inject real movement within the 30m window.
	bt4WriteMoveEvent(t, projectDir, core.EventTypeBeadClosed, now.Add(-1*time.Minute))
	sigMove := sentinel.Evaluate(ctx, state2, input, cfg)
	if sigMove.Level == sentinel.ActivationHalt {
		t.Errorf("BT5-1 reset: a terminal-progress event must reset the doom-loop counter; got HALT (score=%d, consecutiveZero=%d)",
			sigMove.Sample.MovementScore, sigMove.ConsecutiveZeroCycles)
	}
	if sigMove.ConsecutiveZeroCycles != 0 {
		t.Errorf("BT5-1 reset: ConsecutiveZeroCycles = %d after movement, want 0", sigMove.ConsecutiveZeroCycles)
	}
	if sigMove.Sample.MovementScore == 0 {
		t.Errorf("BT5-1 reset: expected movement score > 0 after bead_closed in window, got 0")
	}

	t.Logf("BT5-1 PASS: %d-1 cycles no halt; cycle %d → ActivationHalt+LivenessViolated; one bead_closed resets the counter",
		n, n)
}

// ---------------------------------------------------------------------------
// BT5-2 — work-gen-once (deploy-class completion stages exactly ONE bead)
// ---------------------------------------------------------------------------

// TestScenario_Flywheel_BT5_2_WorkGenOnce exercises §5.4 (B): a deploy-class
// Phase-1 completion stages EXACTLY ONE bead via the REAL stagedBeadGeneratorEval.
// The created bead must be --status open (guardrail 2: land-open, NOT
// same-tick-dispatched) and carry the needs-greenlight label (AC2 greenlight
// gate). A real fake-br binary records the create argv so the assertions read
// the actual `br create` that production code emitted.
func TestScenario_Flywheel_BT5_2_WorkGenOnce(t *testing.T) {
	projectDir := t.TempDir()
	writePhase2Config(t, projectDir, "deploy", "harmonik queue status")

	tmp := t.TempDir()
	argsFile := filepath.Join(tmp, "br-args.txt")
	scriptPath := filepath.Join(tmp, "br")
	writeFakeBrArgScript(t, scriptPath, argsFile)

	deps := stagedBeadFixtureDeps(t, projectDir, scriptPath)
	ledgerPath := filepath.Join(projectDir, ".harmonik", followUpLedgerFileName)
	deps.followUpLedgerPath = ledgerPath

	// One deploy-class completion.
	stagedBeadGeneratorEval(context.Background(), deps, "hk-bt5-deploybead", []string{"deploy"})

	// ── Assert: EXACTLY ONE br create ───────────────────────────────────────
	if n := bt5CountBrCreateCalls(t, argsFile); n != 1 {
		t.Fatalf("BT5-2: expected exactly 1 br create (work-gen-ONCE); got %d", n)
	}

	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("BT5-2: read br args: %v", err)
	}
	line := strings.TrimSpace(string(data))

	// ── Assert: land-open (guardrail 2 — not same-tick-dispatched) ──────────
	if !strings.Contains(line, "--status") || !strings.Contains(line, "open") {
		t.Errorf("BT5-2: staged bead must be created --status open (land-open guardrail); argv=%q", line)
	}
	// ── Assert: needs-greenlight (AC2 captain-greenlight gate) ──────────────
	if !strings.Contains(line, labelNeedsGreenlight) {
		t.Errorf("BT5-2: staged bead must carry %q label (AC2 greenlight gate); argv=%q", labelNeedsGreenlight, line)
	}
	// ── Assert: names the target bead + class (rule-only provenance) ────────
	if !strings.Contains(line, "hk-bt5-deploybead") {
		t.Errorf("BT5-2: staged bead must reference the completed bead; argv=%q", line)
	}

	// ── Assert: exactly one durable ledger entry, keyed (bead:class) ────────
	ledger, lerr := loadFollowUpLedger(ledgerPath)
	if lerr != nil {
		t.Fatalf("BT5-2: loadFollowUpLedger: %v", lerr)
	}
	if len(ledger) != 1 {
		t.Errorf("BT5-2: expected exactly 1 ledger entry; got %d (%v)", len(ledger), ledger)
	}
	if _, ok := ledger["hk-bt5-deploybead:deploy"]; !ok {
		t.Errorf("BT5-2: ledger missing key 'hk-bt5-deploybead:deploy'; got %v", ledger)
	}

	t.Log("BT5-2 PASS: deploy-class completion staged EXACTLY ONE open+needs-greenlight bead; one ledger entry")
}

// ---------------------------------------------------------------------------
// BT5-3 — ledger-survives-restart (durable at-most-once across daemon restart)
// ---------------------------------------------------------------------------

// TestScenario_Flywheel_BT5_3_LedgerSurvivesRestart exercises §5.4 B guardrail 4
// (AC1, hk-3ndb): the durable ledger prevents the staged-bead generator from
// double-emitting across a daemon restart.
//
// Round-trip:
//  1. Run the generator once with a fresh (empty) in-memory ledger + a real
//     on-disk follow-up-ledger.jsonl → exactly one br create + one disk entry.
//  2. SIMULATE A DAEMON RESTART: build a brand-new workLoopDeps (fresh
//     in-memory followUpLedger) and re-seed it from the persisted file via
//     loadFollowUpLedger — exactly what daemon boot does (workloop.go re-makes
//     followUpLedger and the durable ledger re-seeds it from
//     followUpLedgerPath).
//  3. REPLAY the same deploy-class completion through the post-restart deps.
//     The generator must be a NO-OP — STILL exactly one br create total, no
//     duplicate deploy+verify tail.
//
// AC1 is LANDED on main, so this case PASSES (it is not skipped).
func TestScenario_Flywheel_BT5_3_LedgerSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	projectDir := t.TempDir()
	writePhase2Config(t, projectDir, "deploy", "harmonik queue status")

	tmp := t.TempDir()
	argsFile := filepath.Join(tmp, "br-args.txt")
	scriptPath := filepath.Join(tmp, "br")
	writeFakeBrArgScript(t, scriptPath, argsFile)

	ledgerPath := filepath.Join(projectDir, ".harmonik", followUpLedgerFileName)
	const completed = core.BeadID("hk-bt5-restartbead")
	const class = "deploy"

	// ── Phase 1: pre-restart daemon stages the follow-up once ───────────────
	depsBefore := stagedBeadFixtureDeps(t, projectDir, scriptPath)
	depsBefore.followUpLedgerPath = ledgerPath

	stagedBeadGeneratorEval(ctx, depsBefore, completed, []string{class})

	if n := bt5CountBrCreateCalls(t, argsFile); n != 1 {
		t.Fatalf("BT5-3 phase 1: expected exactly 1 br create before restart; got %d", n)
	}
	ledger1, err := loadFollowUpLedger(ledgerPath)
	if err != nil {
		t.Fatalf("BT5-3 phase 1: loadFollowUpLedger: %v", err)
	}
	if _, ok := ledger1[string(completed)+":"+class]; !ok {
		t.Fatalf("BT5-3 phase 1: key not persisted to disk ledger; got %v", ledger1)
	}

	// ── Phase 2: simulate daemon RESTART — fresh deps, re-seed from disk ─────
	// A restart re-makes the in-memory followUpLedger (empty) then re-seeds it
	// from the durable file. We replicate the boot re-seed here, then assert the
	// new deps carry the prior key — i.e. the ledger genuinely survived.
	depsAfter := stagedBeadFixtureDeps(t, projectDir, scriptPath) // fresh empty ledger map
	depsAfter.followUpLedgerPath = ledgerPath
	depsAfter.followUpLedger = make(map[string]struct{})
	depsAfter.followUpLedgerMu = new(sync.Mutex)

	seeded, err := loadFollowUpLedger(ledgerPath)
	if err != nil {
		t.Fatalf("BT5-3 restart: loadFollowUpLedger re-seed: %v", err)
	}
	for k := range seeded {
		depsAfter.followUpLedger[k] = struct{}{}
	}
	if _, ok := depsAfter.followUpLedger[string(completed)+":"+class]; !ok {
		t.Fatalf("BT5-3 restart: re-seeded ledger lost the prior key %q — durability broken; got %v",
			string(completed)+":"+class, depsAfter.followUpLedger)
	}

	// ── Phase 3: REPLAY the same completion → must be a NO-OP ────────────────
	stagedBeadGeneratorEval(ctx, depsAfter, completed, []string{class})

	if n := bt5CountBrCreateCalls(t, argsFile); n != 1 {
		t.Fatalf("BT5-3 phase 3: replay after restart double-emitted — expected STILL exactly 1 br create, got %d (durable at-most-once broken)", n)
	}

	// Disk ledger must still hold exactly one entry (no duplicate append).
	ledger2, err := loadFollowUpLedger(ledgerPath)
	if err != nil {
		t.Fatalf("BT5-3 phase 3: loadFollowUpLedger: %v", err)
	}
	if len(ledger2) != 1 {
		t.Errorf("BT5-3 phase 3: disk ledger has %d entries after replay; want 1 (%v)", len(ledger2), ledger2)
	}

	t.Log("BT5-3 PASS: ledger persisted across simulated restart; replay was a no-op — STILL exactly one staged bead")
}
