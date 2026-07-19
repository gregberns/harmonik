package keeper_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

// hk-vpnp / Bug 3 — the automatic ACT-when-idle cycle LOOPS and truncates the
// handoff file to 0 lines between cycles.
//
// Observed live (nonces -000001 / -000002): the cycle injects /session-handoff,
// times out before the agent confirms (so /clear is NEVER issued), aborts, and
// in doing so destroys the on-disk handoff because step-2 truncated it BEFORE
// confirmation. Then it re-arms and re-fires with a new nonce, truncating again.
//
// These two tests assert the fixed contract:
//   - 3b: a NON-EMPTY handoff that fails to confirm is NOT truncated to 0.
//   - 3a: after a handoff timeout the cycle does NOT re-fire a second nonce on
//     the same un-cleared session in a tight loop.

// realHandoffCycler builds a Cycler that uses REAL on-disk handoff file I/O
// (default truncate/read/append) so the test can assert what survives on disk.
// The injector is a spy; the handoff is never actually written by an "agent",
// simulating a /session-handoff whose /clear never completes (nonce never lands).
func realHandoffCycler(t *testing.T, agent, projectDir, cycleID string, spy *cycleSpyInjector, jc *journalCapture) *keeper.Cycler {
	t.Helper()
	handoffPath := filepath.Join(projectDir, "HANDOFF-"+agent+".md")
	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          projectDir,
		TmuxTarget:          "fake-pane",
		ActPct:              90.0,
		WarnPct:             80.0,
		ForceActPct:         200.0, // disable the force-clear path for this test
		HandoffTimeout:      60 * time.Millisecond,
		ClearSettle:         30 * time.Millisecond,
		PollInterval:        10 * time.Millisecond,
		CycleIDGen:          func() string { return cycleID },
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath: func(_, _ string) string {
			return handoffPath
		},
		// Use the package defaults for ReadHandoff / TruncateHandoffFn /
		InjectFn: spy.inject,
		ReadGaugeFn: func(_, _ string) (*keeper.CtxFile, time.Time, error) {
			return &keeper.CtxFile{Pct: 95.0, SessionID: "sess-act"}, time.Now(), nil
		},
		CrispIdleFn:       func(_, _ string) bool { return true },
		HoldingDispatchFn: func(_, _ string) bool { return false },
		WriteJournalFn:    jc.write,
		SetTmuxEnvFn:      func(_ context.Context, _, _, _ string) error { return nil },
	}
	return keeper.NewCycler(cfg, &keeper.RecordingEmitter{})
}

// TestActLoop_HKVPNP_DoesNotTruncateNonEmptyHandoffOnTimeout reproduces Bug 3b:
// a non-empty handoff that fails to confirm must NOT be wiped to 0 lines.
func TestActLoop_HKVPNP_DoesNotTruncateNonEmptyHandoffOnTimeout(t *testing.T) {
	t.Parallel()

	const (
		agent   = "act-loop-agent"
		cycleID = "cyc-act-000001"
	)
	dir := t.TempDir()
	handoffPath := filepath.Join(dir, "HANDOFF-"+agent+".md")

	// Seed a real, non-empty prior handoff on disk.
	prior := "# Prior handoff\n\nLane: keeper-redesign. Important fleet intent here.\n"
	if err := os.WriteFile(handoffPath, []byte(prior), 0o600); err != nil {
		t.Fatalf("seed handoff: %v", err)
	}

	spy := &cycleSpyInjector{}
	jc := &journalCapture{}
	cycler := realHandoffCycler(t, agent, dir, cycleID, spy, jc)

	// The agent never writes the nonce → pollForNonce times out → abort.
	cf := &keeper.CtxFile{Pct: 95.0, SessionID: "sess-act"}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	// /clear must NOT have been issued (handoff unconfirmed).
	for _, txt := range spy.texts() {
		if txt == "/clear" {
			t.Fatalf("/clear issued on an unconfirmed handoff: %v", spy.texts())
		}
	}

	// THE BUG: the prior non-empty handoff must survive the aborted cycle.
	got, err := os.ReadFile(handoffPath) //nolint:gosec // test path
	if err != nil {
		t.Fatalf("read handoff after cycle: %v", err)
	}
	if len(strings.TrimSpace(string(got))) == 0 {
		t.Fatalf("Bug 3b: handoff truncated to 0 lines on aborted cycle; prior content lost")
	}
}

// TestActLoop_HKVPNP_DoesNotRefireSecondNonceAfterTimeout reproduces Bug 3a:
// after a handoff timeout on an un-cleared session, the cycle must NOT re-fire a
// second /session-handoff with a fresh nonce on the immediately following ticks.
//
// The live signature was nonces -000001 then -000002 firing back-to-back while
// /clear never completed. We simulate repeated watcher ticks (MaybeRun calls)
// against the SAME high-context session whose handoff never confirms, and assert
// only ONE /session-handoff is ever injected.
func TestActLoop_HKVPNP_DoesNotRefireSecondNonceAfterTimeout(t *testing.T) {
	t.Parallel()

	const agent = "act-loop-refire-agent"
	dir := t.TempDir()
	handoffPath := filepath.Join(dir, "HANDOFF-"+agent+".md")
	if err := os.WriteFile(handoffPath, []byte("# prior\n"), 0o600); err != nil {
		t.Fatalf("seed handoff: %v", err)
	}

	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	// Distinct nonce per cycle, mirroring the live -000001 / -000002 signature.
	var idMu sync.Mutex
	var idSeq int
	idGen := func() string {
		idMu.Lock()
		defer idMu.Unlock()
		idSeq++
		return "cyc-refire-00000" + string(rune('0'+idSeq))
	}

	cfg := keeper.CyclerConfig{
		IdleMarkerModTimeFn: idleMarkerFreshNow, // Stop hook wired: model-done on first AwaitModelDone poll (T8)
		AgentName:           agent,
		ProjectDir:          dir,
		TmuxTarget:          "fake-pane",
		ActPct:              90.0,
		WarnPct:             80.0,
		ForceActPct:         200.0,
		HandoffTimeout:      40 * time.Millisecond,
		ClearSettle:         20 * time.Millisecond,
		PollInterval:        10 * time.Millisecond,
		CycleIDGen:          idGen,
		IsManagedFn:         func(_, _ string) bool { return true },
		HandoffFilePath: func(_, _ string) string {
			return handoffPath
		},
		InjectFn: spy.inject,
		ReadGaugeFn: func(_, _ string) (*keeper.CtxFile, time.Time, error) {
			return &keeper.CtxFile{Pct: 95.0, SessionID: "sess-refire"}, time.Now(), nil
		},
		CrispIdleFn:       func(_, _ string) bool { return true },
		HoldingDispatchFn: func(_, _ string) bool { return false },
		WriteJournalFn:    jc.write,
		SetTmuxEnvFn:      func(_ context.Context, _, _, _ string) error { return nil },
	}
	cycler := keeper.NewCycler(cfg, &keeper.RecordingEmitter{})

	// Simulate the live loop signature. The SID never changes (no /clear ever
	// completed), but the gauge pct oscillates: after each aborted cycle the
	// gauge briefly reads below WarnPct (the handoff was truncated and the agent
	// momentarily resets / the keeper heartbeat re-stamps a low reading) and then
	// climbs back above ActPct. That below-warn dip on the SAME un-cleared SID is
	// exactly what the anti-loop escape hatch keys on, re-arming the cycle to
	// re-fire a fresh nonce. A correct keeper must NOT re-fire merely because the
	// gauge dipped while the session was never actually cleared.
	tick := func(pct float64) {
		cf := &keeper.CtxFile{Pct: pct, SessionID: "sess-refire"}
		if err := cycler.MaybeRun(context.Background(), cf); err != nil {
			t.Fatalf("MaybeRun: %v", err)
		}
	}
	tick(95.0) // cycle 1 fires → handoff times out → abort
	tick(50.0) // post-abort dip below WarnPct on the SAME un-cleared SID
	tick(95.0) // climbs back → buggy keeper re-fires nonce 2 here
	tick(50.0)
	tick(95.0)

	// Count distinct /session-handoff injections (one per re-fired nonce).
	handoffInjects := 0
	for _, txt := range spy.texts() {
		if strings.Contains(txt, "/session-handoff") {
			handoffInjects++
		}
	}
	if handoffInjects > 1 {
		t.Fatalf("Bug 3a: cycle re-fired %d /session-handoff nonces on an un-cleared session (loop); want 1", handoffInjects)
	}
}

// silence unused import in case core is only referenced conditionally.
var _ = core.EventTypeSessionKeeperCycleAborted
