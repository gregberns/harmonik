package daemon

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/gregberns/harmonik/internal/core"
	socketrouter "github.com/gregberns/harmonik/internal/daemon/router"
	"github.com/gregberns/harmonik/internal/eventbus"
)

var cliReachableVerdictOps = []string{"confirm_verdict", "veto_verdict"}

// TestVerdictOverrideOps_AreRegistered is the root-cause guard for C1: it proves
// every CLI-reachable verdict-override op routes to a real handler (a Dispatch
// that does NOT return Unknown), and — as a negative control — that a bogus op
// DOES hit the Unknown path. This is exactly the failure mode C1 was: a
// CLI-reachable op that silently fell through to "unknown op".
func TestVerdictOverrideOps_AreRegistered(t *testing.T) {
	router := buildSocketRouter(&socketDispatch{}) // nil handlers: we only probe routing, not behavior
	registered := make(map[string]struct{}, len(router.Ops()))
	for _, op := range router.Ops() {
		registered[op] = struct{}{}
	}

	for _, op := range cliReachableVerdictOps {
		if _, ok := registered[op]; !ok {
			t.Fatalf("CLI-reachable op %q is NOT registered in buildSocketRouter — operator override path is dead (C1 regression)", op)
		}
		res := router.Dispatch(context.Background(), op, json.RawMessage(`{"op":"`+op+`","run_id":"r1"}`))
		if res.Unknown {
			t.Fatalf("registered op %q resolved to Unknown — routing hole", op)
		}
	}

	bogus := router.Dispatch(context.Background(), "confirm_verdikt_typo", json.RawMessage(`{"op":"confirm_verdikt_typo"}`))
	if !bogus.Unknown {
		t.Fatal("bogus op did not resolve to Unknown — negative control failed")
	}
}

func newVerdictRouter(t *testing.T) (*socketrouter.Router, *OperatorPauseController) {
	t.Helper()
	bus := eventbus.NewBusImpl()
	if err := bus.Seal(); err != nil {
		t.Fatalf("newVerdictRouter: bus.Seal: %v", err)
	}
	ctrl := NewOperatorPauseController(bus)
	router := buildSocketRouter(&socketDispatch{oh: ctrl})
	return router, ctrl
}

func dispatchVerdict(t *testing.T, router *socketrouter.Router, op, runID, promoteTo string) socketrouter.Result {
	t.Helper()
	raw, err := json.Marshal(map[string]string{"op": op, "run_id": runID, "promote_to": promoteTo})
	if err != nil {
		t.Fatalf("dispatchVerdict: marshal: %v", err)
	}
	return router.Dispatch(context.Background(), op, raw)
}

// TestVerdictOverride_ConfirmReleasesParkedRun: a run parked awaiting
// confirmation is released with a Confirm decision when confirm_verdict arrives.
func TestVerdictOverride_ConfirmReleasesParkedRun(t *testing.T) {
	router, ctrl := newVerdictRouter(t)
	const runID = "run-confirm-1"
	ch := ctrl.Verdicts().Await(runID)

	res := dispatchVerdict(t, router, "confirm_verdict", runID, "")
	if res.Unknown || !res.OK {
		t.Fatalf("confirm_verdict: got Unknown=%v OK=%v err=%q", res.Unknown, res.OK, res.Err)
	}

	got := <-ch
	if got.Decision != core.VerdictOverrideDecisionConfirm {
		t.Fatalf("parked run received decision %q, want confirm", got.Decision)
	}
	if got.VetoPromotion != core.VetoPromotionNone {
		t.Fatalf("confirm carried promotion %q, want none", got.VetoPromotion)
	}
}

// TestVerdictOverride_VetoPromoteEscalate: veto_verdict --promote-to
// escalate-to-human releases the parked run with a Veto + escalate promotion.
func TestVerdictOverride_VetoPromoteEscalate(t *testing.T) {
	router, ctrl := newVerdictRouter(t)
	const runID = "run-veto-1"
	ch := ctrl.Verdicts().Await(runID)

	res := dispatchVerdict(t, router, "veto_verdict", runID, "escalate-to-human")
	if res.Unknown || !res.OK {
		t.Fatalf("veto_verdict: got Unknown=%v OK=%v err=%q", res.Unknown, res.OK, res.Err)
	}

	got := <-ch
	if got.Decision != core.VerdictOverrideDecisionVeto {
		t.Fatalf("parked run received decision %q, want veto", got.Decision)
	}
	if got.VetoPromotion != core.VetoPromotionEscalateToHuman {
		t.Fatalf("veto carried promotion %q, want escalate-to-human", got.VetoPromotion)
	}
}

// TestVerdictOverride_NoParkedRun: an override for a run that is NOT parked
// returns the operator-control-invalid-state error (code 16), which the CLI
// surfaces as exit 16.
func TestVerdictOverride_NoParkedRun(t *testing.T) {
	router, _ := newVerdictRouter(t)

	res := dispatchVerdict(t, router, "confirm_verdict", "run-never-parked", "")
	if res.Unknown {
		t.Fatal("confirm_verdict resolved to Unknown for an unparked run — routing hole")
	}
	if res.OK {
		t.Fatal("confirm_verdict succeeded for an unparked run, want failure")
	}
	if res.ErrorCode != 16 {
		t.Fatalf("unparked-run error_code = %d, want 16 (operator-control-invalid-state)", res.ErrorCode)
	}
}
