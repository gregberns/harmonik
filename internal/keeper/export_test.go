package keeper

import "time"

// export_test.go — test-only helpers that expose internal Cycler state to the
// keeper_test package. Only compiled during `go test`. Refs: hk-wjzf.

// ResolveCyclerDefaultsForTest returns a CyclerConfig whose zero-valued numeric
// fields have been replaced by their production defaults (CyclerConfig.applyDefaults,
// sourced from thresholds.go). It lets the external keeper_test suite PIN the
// operator-decided warn/act/force band without re-declaring the unexported default
// constants. Refs: hk-nlio (operator real-env validation gate), hk-bpkv.
func ResolveCyclerDefaultsForTest() CyclerConfig {
	var c CyclerConfig
	c.applyDefaults()
	return c
}

// OperatorActiveSinceForTest exposes the pure operatorActiveSince distinction
// (tmuxresolve.go) to the keeper_test package so the suite can assert the
// idle/remote-control-client-vs-live-typist split that the operator-attached
// guard depends on (hk-0t5s). Refs: hk-nlio.
func OperatorActiveSinceForTest(listClientsOutput string, now time.Time, window time.Duration) bool {
	return operatorActiveSince(listClientsOutput, now, window)
}

// OperatorActiveWindowForTest exposes the production operatorActiveWindow so the
// suite feeds the real window to OperatorActiveSinceForTest. Refs: hk-nlio.
const OperatorActiveWindowForTest = operatorActiveWindow

// SetCyclerLastFiredSID sets the Cycler's lastFiredSID field, allowing test
// code to pre-arm the anti-loop gate without running a real cycle.
func SetCyclerLastFiredSID(c *Cycler, sid string) {
	c.lastFiredSID = sid
	c.seenLowPctAfterLastFire = false
}

// DeriveContextTokensForTest exposes deriveContextTokens to the keeper_test
// package so the transcript token-derivation logic can be exercised directly.
// Refs: hk-81wk.
func DeriveContextTokensForTest(transcriptDir, sessionID string) (int64, bool) {
	return deriveContextTokens(transcriptDir, sessionID)
}
