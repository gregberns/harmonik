package daemon

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// The audit must be DERIVED from the live bootState, not recited from a
// constant. The version this replaced was a hand-maintained []wiringEntry that
// printed identical text no matter what had been wired — so it could not detect
// the silent drop it existed to catch. These tests pin the property that makes
// it a real drop detector: the same field reads differently depending on
// whether it actually holds a value.
//
// Bead ref: hk-4mupj.

func auditByField(t *testing.T, bs *bootState) map[string]wiringState {
	t.Helper()
	out := map[string]wiringState{}
	for _, e := range bs.wiringAudit() {
		out[e.field] = e
	}
	return out
}

// A nil singleton reports ABSENT and a constructed one reports constructed —
// from the same code path, with no table edited in between.
func TestWiringAudit_ReflectsWhatIsActuallyWired(t *testing.T) {
	t.Parallel()

	absent := auditByField(t, &bootState{})
	if got, ok := absent["handlerPauseCtrl"]; !ok {
		t.Fatal("wiringAudit omitted handlerPauseCtrl; the audit must cover every nilable bootState singleton")
	} else if got.constructed {
		t.Error("handlerPauseCtrl reported constructed on an empty bootState; the audit is not reading the live state")
	}

	wired := auditByField(t, &bootState{handlerPauseCtrl: &HandlerPauseController{}})
	if !wired["handlerPauseCtrl"].constructed {
		t.Error("handlerPauseCtrl reported ABSENT while holding a value; a hand-maintained table would have printed the same either way")
	}

	// A field left nil in the second case must still read ABSENT: the audit
	// distinguishes per field, not per bootState.
	if wired["sharedRunRegistry"].constructed {
		t.Error("sharedRunRegistry reported constructed while nil; per-field state is what makes this a drop detector")
	}
}

// Configuration is not wiring: scalar fields carry no nil/constructed
// distinction and must not pad the audit with meaningless rows.
func TestWiringAudit_SkipsNonWireableFields(t *testing.T) {
	t.Parallel()

	got := auditByField(t, &bootState{})
	for _, skipped := range []string{"cfg", "hooks", "clockRegressionDetected"} {
		if _, ok := got[skipped]; ok {
			t.Errorf("wiringAudit included %q; scalars are configuration, not wiring", skipped)
		}
	}
	if len(got) == 0 {
		t.Fatal("wiringAudit returned nothing; it must enumerate the bootState singletons")
	}
}

// The log is gated on HARMONIK_DEBUG_WIRING=1 and, when on, renders both states
// off the derived audit.
func TestLogCompositionRoot_GatedAndDerived(t *testing.T) {
	ctx := context.Background()
	bs := &bootState{handlerPauseCtrl: &HandlerPauseController{}}

	var off bytes.Buffer
	bs.logCompositionRoot(ctx, &off)
	if off.Len() != 0 {
		t.Errorf("logCompositionRoot wrote %q without HARMONIK_DEBUG_WIRING=1; the audit is opt-in", off.String())
	}

	t.Setenv("HARMONIK_DEBUG_WIRING", "1")
	var on bytes.Buffer
	bs.logCompositionRoot(ctx, &on)
	out := on.String()

	// The header doubles as the "daemon reached startBackgroundLoops" beacon
	// that the subsystem-partition tests key on; keep it stable.
	if !strings.Contains(out, "composition-root wiring audit") {
		t.Errorf("audit header missing from %q; boot-progress tests key on this line", out)
	}
	if !strings.Contains(out, "handlerPauseCtrl") || !strings.Contains(out, "constructed") {
		t.Errorf("audit did not report the constructed singleton: %q", out)
	}
	if !strings.Contains(out, "ABSENT") {
		t.Errorf("audit reported no absent singletons for a mostly-empty bootState: %q", out)
	}
}
