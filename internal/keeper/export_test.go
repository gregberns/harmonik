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

// SetCyclerLastFiredSID sets the reactor's LastFiredSID state, allowing test
// code to pre-arm the anti-loop gate without running a real cycle. (T7: the
// anti-loop fields moved from the Cycler onto the pure reactor's CycleState.)
func SetCyclerLastFiredSID(c *Cycler, sid string) {
	c.machine.state.LastFiredSID = sid
	c.machine.state.SeenLowPctAfterLastFire = false
}

// DeriveContextTokensForTest exposes deriveContextTokens to the keeper_test
// package so the transcript token-derivation logic can be exercised directly.
// Refs: hk-81wk.
func DeriveContextTokensForTest(transcriptDir, sessionID string) (int64, bool) {
	return deriveContextTokens(transcriptDir, sessionID)
}

// RecentTranscriptTurnForTest exposes recentTranscriptTurn to the keeper_test
// package for deterministic transcript-detection testing. Refs: hk-74iyd.
func RecentTranscriptTurnForTest(transcriptDir, sessionID, role string) (time.Time, bool) {
	return recentTranscriptTurn(transcriptDir, sessionID, role)
}

// StripNonceMarkersForTest exposes the pure stripNonceMarkers scrub to the
// keeper_test package so its behavior can be pinned directly, character by
// character, instead of only through a full cycle. Refs: hk-4tjyj.
func StripNonceMarkersForTest(content string) string {
	return stripNonceMarkers(content)
}

// ShellQuoteIfNeededForTest exposes the shell-quoting allowlist used to build the
// injected reboot command. The output is pasted into a live pane and executed, so
// the quoting rule is directly test-pinned. Refs: hk-4tjyj.
func ShellQuoteIfNeededForTest(s string) string {
	return shellQuoteIfNeeded(s)
}
