//go:build scenario

package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/sentinel"
)

func bt5LivenessConfig(n int) sentinel.Config {
	return sentinel.Config{
		Window:              30 * time.Minute,
		WarmupWindow:        30 * time.Minute, // satisfied by state.DaemonStartedAt = now-1h
		SustainedWindows:    2,
		LivenessNoProgressN: n,
	}
}

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

	input := sentinel.GovernorInput{
		ProjectDir:    projectDir,
		Now:           now,
		HasReadyBeads: true,
	}

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

	state2 := bt4WarmState(now)
	for i := 1; i < n; i++ {
		sig := sentinel.Evaluate(ctx, state2, input, cfg)
		if sig.Level == sentinel.ActivationHalt {
			t.Fatalf("BT5-1 reset-arm: cycle %d halted before movement injected", i)
		}
	}
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

	deps, eagerRefill := stagedBeadFixtureDeps(t, projectDir, scriptPath)
	ledgerPath := filepath.Join(projectDir, ".harmonik", followUpLedgerFileName)
	eagerRefill.followUpLedgerPath = ledgerPath

	stagedBeadGeneratorEvalForTest(context.Background(), deps, eagerRefill, "hk-bt5-deploybead", []string{"deploy"})

	if n := bt5CountBrCreateCalls(t, argsFile); n != 1 {
		t.Fatalf("BT5-2: expected exactly 1 br create (work-gen-ONCE); got %d", n)
	}

	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("BT5-2: read br args: %v", err)
	}
	line := strings.TrimSpace(string(data))

	if !strings.Contains(line, "--status") || !strings.Contains(line, "open") {
		t.Errorf("BT5-2: staged bead must be created --status open (land-open guardrail); argv=%q", line)
	}
	if !strings.Contains(line, labelNeedsGreenlight) {
		t.Errorf("BT5-2: staged bead must carry %q label (AC2 greenlight gate); argv=%q", labelNeedsGreenlight, line)
	}
	if !strings.Contains(line, "hk-bt5-deploybead") {
		t.Errorf("BT5-2: staged bead must reference the completed bead; argv=%q", line)
	}

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

// TestScenario_Flywheel_BT5_3_LedgerSurvivesRestart exercises §5.4 B guardrail 4
// (AC1, hk-3ndb): the durable ledger prevents the staged-bead generator from
// double-emitting across a daemon restart.
//
// Round-trip:
//  1. Run the generator once with a fresh (empty) in-memory ledger + a real
//     on-disk follow-up-ledger.jsonl → exactly one br create + one disk entry.
//  2. SIMULATE A DAEMON RESTART: build a new EagerRefillPort and load the
//     persisted file through the boot helper.
//  3. REPLAY the same deploy-class completion through the post-restart port.
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

	depsBefore, eagerBefore := stagedBeadFixtureDeps(t, projectDir, scriptPath)
	eagerBefore.followUpLedgerPath = ledgerPath

	stagedBeadGeneratorEvalForTest(ctx, depsBefore, eagerBefore, completed, []string{class})

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

	depsAfter, _ := stagedBeadFixtureDeps(t, projectDir, scriptPath)
	eagerAfter := newEagerRefillPort(Config{ProjectDir: projectDir})
	loadEagerRefillLedger(&eagerAfter)
	if _, ok := eagerAfter.followUpLedger[string(completed)+":"+class]; !ok {
		t.Fatalf("BT5-3 restart: re-seeded ledger lost the prior key %q — durability broken; got %v",
			string(completed)+":"+class, eagerAfter.followUpLedger)
	}

	stagedBeadGeneratorEvalForTest(ctx, depsAfter, eagerAfter, completed, []string{class})

	if n := bt5CountBrCreateCalls(t, argsFile); n != 1 {
		t.Fatalf("BT5-3 phase 3: replay after restart double-emitted — expected STILL exactly 1 br create, got %d (durable at-most-once broken)", n)
	}

	ledger2, err := loadFollowUpLedger(ledgerPath)
	if err != nil {
		t.Fatalf("BT5-3 phase 3: loadFollowUpLedger: %v", err)
	}
	if len(ledger2) != 1 {
		t.Errorf("BT5-3 phase 3: disk ledger has %d entries after replay; want 1 (%v)", len(ledger2), ledger2)
	}

	t.Log("BT5-3 PASS: ledger persisted across simulated restart; replay was a no-op — STILL exactly one staged bead")
}
