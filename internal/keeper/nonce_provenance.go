package keeper

// nonce_provenance.go — the T6 render leg of the keeper-restart nonce provenance
// channel (hk-keeper-delivery-nonce-provenance-0hk8n, SK-030/SK-031).
//
// The cycle_id minted at cycle entry (cyc-<ts>-<seq>) is the single join key. It
// is written into the handoff's KEEPER:<cycle_id> marker (nonceMarker, cycle.go)
// and — via this render — into the K2 leader-defer nudge's
// `restart-now --nonce <cycle_id>` command string. When the agent runs that
// command, restart-now echoes the same value on its emitted
// session_keeper_restart_now event (T5), so a query of events.jsonl by the nonce
// joins the self-restart to its originating cycle. The value travels ONLY as text
// (marker → command string) — no shared runtime state between the keeper and the
// separate restart-now process (SK-031).
//
// Lives in its own file (not watcher.go) so the T6 render does not contend with
// the peer-owned T2/T4 edits in that hot file — the method attaches to
// WatcherConfig regardless of file. Consumption by the K1 delivery decision
// (comms vs terminal) is T7 (SK-024); T3 provides the validated template.

// leaderDeferTextForHandoff renders the K2 leader-defer nudge with its restart-now
// --nonce slot filled from the cycle_id carried in handoffContent's
// KEEPER:<cycle_id> marker. Returns ("", false) when handoffContent has no
// well-formed cycle marker — there is then no joinable cycle_id, and the caller
// MUST NOT ship a nudge carrying a bogus/empty nonce. Refs: SK-030, SK-031, T6.
func (c *WatcherConfig) leaderDeferTextForHandoff(handoffContent string) (string, bool) {
	cycleID, ok := CycleIDFromNonceMarker(handoffContent)
	if !ok {
		return "", false
	}
	return c.selectLeaderDeferText(cycleID), true
}
