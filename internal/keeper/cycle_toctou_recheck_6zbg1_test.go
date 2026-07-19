package keeper_test

// cycle_toctou_recheck_6zbg1_test.go — T8 (hk-keeper-delivery-toctou-recheck-6zbg1)
// acceptance for SK-035: the in-cycle operator-attached TOCTOU re-check. An
// operator who becomes attached AFTER cycle entry but DURING the handoff wait is
// respected — the nonce-confirm (the sole gate to the destructive /clear) is held,
// so /clear never fires over the operator's in-flight turn — whereas the single
// entry-time Gate-7 sample would have missed it. Reuses the newAttachTestCycler
// harness (cycle_operator_attached_test.go).

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/gregberns/harmonik/internal/core"
	"github.com/gregberns/harmonik/internal/keeper"
)

// TestCycler_OperatorAttachesDuringWait_HoldsClear — a mid-wait attach holds the
// /clear. attachFn returns false on the FIRST probe (cycle-entry Gate-7, so the
// cycle opens and injects /session-handoff) and true thereafter (the handoff-wait
// polls), so the nonce is never confirmed and the cycle aborts on the handoff
// timeout WITHOUT a /clear. The handoff (nonce) is ALWAYS present, so the only
// thing withholding /clear is the re-check (SK-035).
func TestCycler_OperatorAttachesDuringWait_HoldsClear(t *testing.T) {
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
	// The re-check HELD the /clear: no destructive reset over the operator.
	for _, tx := range texts {
		if strings.Contains(tx, "/clear") {
			t.Fatalf("/clear was injected over a mid-wait operator attach (SK-035 violated): %v", texts)
		}
	}
	// The cycle did NOT complete (it aborted on the handoff timeout, warn-only).
	if evts := em.EventsOfType(core.EventTypeSessionKeeperCycleComplete); len(evts) != 0 {
		t.Errorf("cycle_complete emitted despite a held /clear; want 0 (aborted), got %d", len(evts))
	}
	// The re-check was actually consulted during the wait (probes beyond entry).
	if probes < 2 {
		t.Errorf("operator-attached re-check not consulted during the wait (probes=%d)", probes)
	}
}
