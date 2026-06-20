package keeper

// warn_operator_attached_hk1ryc_test.go — hk-1ryc operator-attached guard on the
// WARN-inject path.
//
// When a human operator is actively attached to the pane, the keeper must NOT
// inject the ACTIONABLE self-service restart instruction (the
// "harmonik keeper restart-now" handshake) — issuing that command mid-keystroke
// would race the operator's own input. selectWarnText must instead return the
// lighter finish-the-turn advisory so the warn is still delivered but the
// self-restart command is withheld until the operator detaches. This mirrors the
// Cycler's act-path Gate-7 (cycle.go) for the warn path.

import (
	"strings"
	"testing"
)

// TestSelectWarnText_OperatorAttached_SuppressesActionable verifies that an
// otherwise-actionable captain (self_service enabled, primary SID, CrispIdle) does
// NOT receive the actionable restart instruction while an operator is attached —
// it gets the lighter advisory instead, and that advisory is non-empty (the warn
// is never lost).
func TestSelectWarnText_OperatorAttached_SuppressesActionable(t *testing.T) {
	t.Parallel()
	c := WatcherConfig{
		AgentName:          "captain",
		SelfServiceEnabled: true,
		WarnAbsTokens:      200_000,
	}

	// Baseline: operator NOT attached → actionable text IS selected (proves the only
	// thing suppressing it below is the attach guard).
	if txt := c.selectWarnText(ctxWith(primarySID, 205_000), true /*crispIdle*/, false /*operatorAttached*/); !strings.Contains(txt, restartNowStem) {
		t.Fatalf("baseline (detached): want actionable restart instruction, got: %s", txt)
	}

	// Operator attached → actionable text MUST be suppressed; lighter advisory used.
	txt := c.selectWarnText(ctxWith(primarySID, 205_000), true /*crispIdle*/, true /*operatorAttached*/)
	if strings.Contains(txt, restartNowStem) {
		t.Fatalf("operator attached: actionable restart instruction must NOT be injected, got: %s", txt)
	}
	if txt == "" {
		t.Fatal("operator attached: a (lighter) warn must still be delivered, got empty")
	}
	if txt != wrapUpWarningText {
		t.Errorf("operator attached: want the compiled lighter advisory, got: %s", txt)
	}
}

// TestSelectWarnText_OperatorAttached_HonorsCustomActionableSuppression verifies
// the guard wins even when a custom ActionableWarnText override is configured: the
// custom actionable command is still suppressed while the operator is attached.
func TestSelectWarnText_OperatorAttached_HonorsCustomActionableSuppression(t *testing.T) {
	t.Parallel()
	custom := "[CUSTOM] run harmonik keeper restart-now --agent captain now"
	c := WatcherConfig{
		AgentName:          "captain",
		SelfServiceEnabled: true,
		ActionableWarnText: custom,
		WarnAbsTokens:      200_000,
	}
	txt := c.selectWarnText(ctxWith(primarySID, 205_000), true /*crispIdle*/, true /*operatorAttached*/)
	if strings.Contains(txt, restartNowStem) {
		t.Fatalf("operator attached: custom actionable command must also be suppressed, got: %s", txt)
	}
	if txt == custom {
		t.Fatal("operator attached: custom actionable override must NOT be injected over an operator's turn")
	}
}
