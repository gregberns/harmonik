package keeper_test

// A client-activity change during the handoff wait must not hide a handoff that
// is already on disk. Real operator turns use the transcript gate instead.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

func TestCycler_ClientActivityDuringWait_DoesNotHideWrittenHandoff(t *testing.T) {
	t.Parallel()

	const (
		agent   = "toctou-agent"
		cycleID = "cyc-toctou-6zbg1"
		sid     = "sess-toctou"
	)

	em := &keeper.RecordingEmitter{}
	spy := &cycleSpyInjector{}
	jc := &journalCapture{}

	// Attached only AFTER cycle entry: the first probe (entry Gate-7) is false so
	// the cycle opens; every subsequent probe (the wait polls) is true.
	var probes int
	attachFn := func(string) bool { probes++; return probes > 1 }

	nonce := "<!-- KEEPER:" + cycleID + " -->"
	alwaysNonce := func(string) (string, error) { return "# Handoff\n\n" + nonce + "\n", nil }
	gauge := func(_, _ string) (*keeper.CtxFile, time.Time, error) {
		return &keeper.CtxFile{Pct: 95.0, SessionID: sid}, time.Now(), nil
	}

	cycler := newAttachTestCycler(agent, t.TempDir(), cycleID, em, spy, jc, alwaysNonce, gauge, attachFn)

	if err := cycler.MaybeRun(context.Background(), &keeper.CtxFile{Pct: 95.0, SessionID: sid}); err != nil {
		t.Fatalf("MaybeRun: %v", err)
	}

	texts := spy.texts()
	// The cycle OPENED (entry Gate-7 passed): the /session-handoff was injected.
	if len(texts) == 0 {
		t.Fatalf("cycle did not open — expected the /session-handoff inject before the wait")
	}
	clearSeen := false
	for _, tx := range texts {
		if strings.Contains(tx, "/clear") {
			clearSeen = true
		}
	}
	if !clearSeen {
		t.Fatalf("written handoff was hidden by client activity: %v", texts)
	}
	if evts := em.EventsOfType(core.EventTypeSessionKeeperCycleComplete); len(evts) != 1 {
		t.Errorf("cycle_complete count = %d; want 1", len(evts))
	}
	if probes != 1 {
		t.Errorf("tmux client probe count = %d; want entry probe only", probes)
	}
}
