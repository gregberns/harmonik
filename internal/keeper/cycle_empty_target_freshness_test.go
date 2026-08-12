package keeper_test

// cycle_empty_target_freshness_test.go — the ordering guard that
// observeHandoffFreshness in shell.go names, and which did not exist anywhere
// in the tree until now. Product code cited a test that was never written.
//
// WHAT THE GUARD CLAIMS. A cycle with no tmux target emits no
// ActInjectHandoffCmd, so the freshness anchor (handoffInjectedAt) is not
// stamped there. executeArmTimer stamps it instead, on the trailing
// ActArmTimer(handoff_timeout). The cycle orders that action AFTER
// ActTruncateHandoff — the nonce scrub — and the scrub REWRITES the handoff
// file. A handoff left over from an earlier cycle therefore always lands with a
// mod-time strictly BEFORE the anchor, reads as not-fresh, and the cycle aborts.
//
// WHY IT MATTERS. Move the stamp above the scrub and the ordering inverts: the
// scrub's own write now looks newer than the anchor, a prior cycle's handoff
// reads FRESH, the recovery path runs, and /clear lands over a handoff this
// agent never wrote (SK-INV-001). That is the whole reason the ordering is
// load-bearing, and it is invisible to every other test in this package.
//
// WHY THERE IS NO INJECTED CLOCK HERE. The anchor comes from Clock.Now() and
// the handoff mod-time comes from the filesystem. The two are only commensurate
// while they are the same clock. A fake clock started at an unrelated epoch
// makes every comparison come out one way for free — the exact defect hk-3ty39
// records. Real time on both sides is what gives this test the ability to fail.
//
// WHAT THE NEGATIVE CONTROL NEEDS FROM THE FILESYSTEM. The scrub and the stamp
// happen microseconds apart, so the control — move the stamp above the scrub
// and watch this go red — needs a mod-time resolution finer than that gap.
// APFS gives nanoseconds and the control is red here. A filesystem with
// one-second mod-times truncates DOWN, which keeps this test green and also
// keeps the control from going red. Read a green control on such a box as
// no result, not as a pass.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

// TestCycler_EmptyTarget_ScrubbedStaleHandoff_StillAborts pins the ordering
// claim documented on observeHandoffFreshness: with no tmux target, a handoff
// left on disk by an EARLIER cycle must not be mistaken for one written for
// THIS cycle, because the scrub rewrites it before the anchor is stamped.
func TestCycler_EmptyTarget_ScrubbedStaleHandoff_StillAborts(t *testing.T) {
	t.Parallel()

	const (
		agent      = "empty-target-scrub-agent"
		cycleID    = "cyc-empty-target-0002"
		priorNonce = "<!-- KEEPER:cyc-empty-target-0001 -->"
		sid        = "sess-empty-target"
		prose      = "Decision: the daemon owns terminal transitions."
	)

	projectDir := t.TempDir()

	// A handoff from the PREVIOUS cycle: real crew prose plus that cycle's
	// keeper marker. The marker makes the scrub run; the prose survives it, so
	// the freshness read gets past its empty-content guard and reaches the
	// mod-time compare. Without surviving prose this test would pass on the
	// wrong branch and prove nothing about ordering.
	handoffPath := filepath.Join(projectDir, "HANDOFF-"+agent+".md")
	staleHandoff := "# Handoff — prior cycle\n\n" + prose + "\n" + priorNonce + "\n"
	if err := os.WriteFile(handoffPath, []byte(staleHandoff), 0o600); err != nil {
		t.Fatalf("seed handoff: %v", err)
	}

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	gauge := func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		return &keeper.CtxFile{Pct: 95.0, Tokens: 320_000, WindowSize: 1_000_000, SessionID: sid}, time.Now(), nil
	}

	// No handoff overrides: Read, ScrubNonce and ModTime all run against the
	// real file at the production path, so the scrub really rewrites it and the
	// mod-time is the one the filesystem stamped.
	cfgOverrides := testCycleOverrides{
		CycleIDs:     func() string { return cycleID },
		Inject:       spy.inject,
		Gauge:        gauge,
		JournalWrite: jc.write,
	}
	cfg := keeper.CyclerConfig{
		AgentName:      agent,
		ProjectDir:     projectDir,
		TmuxTarget:     "", // the whole point: no pane, so no inject action
		ActPct:         90.0,
		WarnPct:        80.0,
		HandoffTimeout: 60 * time.Millisecond,
		ClearSettle:    30 * time.Millisecond, // unreached on the abort path
		PollInterval:   5 * time.Millisecond,
	}
	cycler := mustNewCyclerWithOverrides(cfg, em, cfgOverrides)

	if err := cycler.MaybeRun(context.Background(),
		&keeper.CtxFile{Pct: 95.0, Tokens: 320_000, WindowSize: 1_000_000, SessionID: sid}); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	// FIXTURE GUARD, and it is not decorative. The freshness read returns early
	// on empty content. If the scrub had taken the crew's prose with the marker,
	// the abort below would be caused by an absent handoff and the ordering
	// claim would never have been exercised at all.
	after, err := os.ReadFile(handoffPath) //nolint:gosec // G304: path is constructed under this test's t.TempDir fixture.
	if err != nil {
		t.Fatalf("read handoff after the cycle: %v", err)
	}
	if !strings.Contains(string(after), prose) {
		t.Fatalf("the scrub destroyed the crew's prose, so the freshness read never reached the mod-time compare; file now: %q", string(after))
	}
	if strings.Contains(string(after), priorNonce) {
		t.Fatalf("the prior cycle's marker survived, so the scrub never rewrote the file and never moved its mod-time; file now: %q", string(after))
	}

	// THE CLAIM. A scrubbed prior-cycle handoff is not fresh, so the cycle
	// aborts. Flip the stamp above the scrub in executeArmTimer and this goes
	// red: the handoff reads fresh, the recovery path runs, and the cycle
	// completes instead.
	aborted := em.EventsOfType(core.EventTypeSessionKeeperCycleAborted)
	if len(aborted) != 1 {
		t.Fatalf("want 1 cycle_aborted on a scrubbed prior-cycle handoff; got %d", len(aborted))
	}
	var ap core.SessionKeeperCycleAbortedPayload
	if err := json.Unmarshal(aborted[0].Payload, &ap); err != nil {
		t.Fatalf("unmarshal cycle_aborted: %v", err)
	}
	if ap.Reason != "handoff_timeout" {
		t.Errorf("cycle_aborted.reason = %q; want \"handoff_timeout\"", ap.Reason)
	}
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleComplete)); n != 0 {
		t.Errorf("want 0 cycle_complete; a stale handoff must never carry a cycle to completion, got %d", n)
	}
	// cycle_recovered is the signature of the freshness-recovery path. It is the
	// single most direct read on which way the ordering went.
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleRecovered)); n != 0 {
		t.Errorf("want 0 cycle_recovered; the recovery path ran over a handoff this cycle never asked for, got %d", n)
	}
	// Journal corroboration, from a different channel than the events.
	phases := jc.snapshot()
	if len(phases) == 0 {
		t.Fatal("no journal phases recorded; the cycle did not open")
	}
	if last := phases[len(phases)-1]; last != "aborted" {
		t.Errorf("last journal phase = %q; want \"aborted\" (phases: %v)", last, phases)
	}
	// Belt and braces, and NOT the discriminator: with an empty tmux target
	// every inject action is suppressed by the reactor, so "no /clear" would be
	// true on both sides of the ordering. The event and journal assertions above
	// are what actually tell the two apart.
	if texts := spy.texts(); len(texts) != 0 {
		t.Errorf("want 0 injections with an empty tmux target; got %v", texts)
	}
}
