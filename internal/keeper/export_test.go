package keeper

// export_test.go — test-only helpers that expose internal Cycler state to the
// keeper_test package. Only compiled during `go test`. Refs: hk-wjzf.

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
