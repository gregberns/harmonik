package daemon_test

import "testing"

// skipRealDaemonE2EInShort excludes heavy real-daemon / real-binary end-to-end
// tests from the per-bead commit_gate, which runs `go test -short` (see
// scripts/scenario-gate.sh "affected unit" step). These tests boot a real
// daemon (daemon.Start), spawn real twin/claude binaries, exercise the
// review-loop over a real Unix-domain socket, or assert strict event ordering
// across goroutines — none of which is deterministic enough for a merge gate,
// and ~21 of them are red on main HEAD for environmental reasons (no registered
// claude adapter, no twin binary, socket-bind/event-ordering races).
//
// They are NOT deleted: without -short they still run (full CI lane). This is a
// temporary shelving, approved by the operator (Refs: hk-p258q). TODO un-shelve:
// restore these to the per-bead gate (or move them to a dedicated CI lane) once
// the real-daemon-boot reds are fixed — tracked by the un-shelve follow-up bead.
func skipRealDaemonE2EInShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("real-daemon E2E — excluded from per-bead commit_gate (-short); runs in full CI lane. TODO un-shelve: hk-p258q")
	}
}
