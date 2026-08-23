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

func realHandoffCycler(t *testing.T, agent, projectDir, cycleID string, spy *cycleSpyInjector, jc *journalCapture) *keeper.Cycler {
	t.Helper()
	handoffPath := filepath.Join(projectDir, "HANDOFF-"+agent+".md")
	cfgOverrides := testCycleOverrides{CycleIDs:

	// disable the force-clear path for this test

	func() string { return cycleID }, HandoffPath: func(_, _ string) string {
		return handoffPath
	}, Inject:
	// Use the package defaults for ReadHandoff / TruncateHandoffFn /
	spy.inject, Gauge: func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		return &keeper.CtxFile{Pct: 95.0, SessionID: "sess-act"}, time.Now(), nil
	}, JournalWrite: jc.write}
	cfg := keeper.CyclerConfig{
		AgentName:      agent,
		ProjectDir:     projectDir,
		TmuxTarget:     "fake-pane",
		ActPct:         90.0,
		WarnPct:        80.0,
		ForceActPct:    200.0,
		HandoffTimeout: 60 * time.Millisecond,
		ClearSettle:    30 * time.Millisecond,
		PollInterval:   10 * time.Millisecond,
	}
	return mustNewCyclerWithOverrides(cfg, &keeper.RecordingEmitter{}, cfgOverrides)
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

	prior := "# Prior handoff\n\nLane: keeper-redesign. Important fleet intent here.\n"
	if err := os.WriteFile(handoffPath, []byte(prior), 0o600); err != nil {
		t.Fatalf("seed handoff: %v", err)
	}

	spy := &cycleSpyInjector{}
	jc := &journalCapture{}
	cycler := realHandoffCycler(t, agent, dir, cycleID, spy, jc)

	cf := &keeper.CtxFile{Pct: 95.0, SessionID: "sess-act"}
	if err := cycler.MaybeRun(context.Background(), cf); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	for _, txt := range spy.texts() {
		if txt == "/clear" {
			t.Fatalf("/clear issued on an unconfirmed handoff: %v", spy.texts())
		}
	}

	got, err := os.ReadFile(handoffPath) //nolint:gosec // test path
	if err != nil {
		t.Fatalf("read handoff after cycle: %v", err)
	}
	if strings.TrimSpace(string(got)) == "" {
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

	var idMu sync.Mutex
	var idSeq int
	idGen := func() string {
		idMu.Lock()
		defer idMu.Unlock()
		idSeq++
		return "cyc-refire-00000" + string(rune('0'+idSeq))
	}
	cfgOverrides := testCycleOverrides{CycleIDs: idGen, HandoffPath: func(_, _ string) string {
		return handoffPath
	}, Inject: spy.inject, Gauge: func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		return &keeper.CtxFile{Pct: 95.0, SessionID: "sess-refire"}, time.Now(), nil
	}, JournalWrite: jc.write}
	cfg := keeper.CyclerConfig{
		AgentName:      agent,
		ProjectDir:     dir,
		TmuxTarget:     "fake-pane",
		ActPct:         90.0,
		WarnPct:        80.0,
		ForceActPct:    200.0,
		HandoffTimeout: 40 * time.Millisecond,
		ClearSettle:    20 * time.Millisecond,
		PollInterval:   10 * time.Millisecond,
	}
	cycler := mustNewCyclerWithOverrides(cfg, &keeper.RecordingEmitter{}, cfgOverrides)

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

var _ = core.EventTypeSessionKeeperCycleAborted
