package keeper_test

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
	t.Skip("keeper-checkpoint-handshake: timeout abort is retired; an empty target remains pending")
	t.Parallel()

	const (
		agent      = "empty-target-scrub-agent"
		cycleID    = "cyc-empty-target-0002"
		priorNonce = "<!-- KEEPER:cyc-empty-target-0001 -->"
		sid        = "sess-empty-target"
		prose      = "Decision: the daemon owns terminal transitions."
	)

	projectDir := t.TempDir()

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
	if n := len(em.EventsOfType(core.EventTypeSessionKeeperCycleRecovered)); n != 0 {
		t.Errorf("want 0 cycle_recovered; the recovery path ran over a handoff this cycle never asked for, got %d", n)
	}
	phases := jc.snapshot()
	if len(phases) == 0 {
		t.Fatal("no journal phases recorded; the cycle did not open")
	}
	if last := phases[len(phases)-1]; last != "aborted" {
		t.Errorf("last journal phase = %q; want \"aborted\" (phases: %v)", last, phases)
	}
	if texts := spy.texts(); len(texts) != 0 {
		t.Errorf("want 0 injections with an empty tmux target; got %v", texts)
	}
}
