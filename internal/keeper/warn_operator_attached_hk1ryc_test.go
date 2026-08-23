package keeper

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

	if txt := c.selectWarnText(ctxWith(primarySID), true /*crispIdle*/, false /*operatorAttached*/); !strings.Contains(txt, restartNowStem) {
		t.Fatalf("baseline (detached): want actionable restart instruction, got: %s", txt)
	}

	txt := c.selectWarnText(ctxWith(primarySID), true /*crispIdle*/, true /*operatorAttached*/)
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
	txt := c.selectWarnText(ctxWith(primarySID), true /*crispIdle*/, true /*operatorAttached*/)
	if strings.Contains(txt, restartNowStem) {
		t.Fatalf("operator attached: custom actionable command must also be suppressed, got: %s", txt)
	}
	if txt == custom {
		t.Fatal("operator attached: custom actionable override must NOT be injected over an operator's turn")
	}
}
