package keeper

// export_test.go — test-only helpers that expose internal Cycler state to the
// keeper_test package. Only compiled during `go test`. Refs: hk-wjzf.

// SetCyclerLastFiredSID sets the Cycler's lastFiredSID field, allowing test
// code to pre-arm the anti-loop gate without running a real cycle.
func SetCyclerLastFiredSID(c *Cycler, sid string) {
	c.lastFiredSID = sid
	c.seenLowPctAfterLastFire = false
}
