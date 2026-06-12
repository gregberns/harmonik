//go:build scenario

package daemon

import "testing"

// skipRealDaemonE2EInShort — internal-package copy of the daemon_test helper of
// the same name in shortskip_hkp258q_test.go.
//
// epiccompleted_scenario_hktfxjp_test.go lives in `package daemon` (it touches
// unexported internals: emitBeadClosedAndMaybeEpic, maybeEmitEpicCompleted, the
// emittedEpics guard). An internal-package test file cannot reference symbols
// declared in `package daemon_test`, so it can't see the original helper —
// referencing it broke the whole `-tags scenario` build (introduced by
// b0913b01/hk-ukx). `package daemon` and `package daemon_test` compile as
// separate units inside the test binary, so this same-named helper does NOT
// collide with the daemon_test one. Body is kept identical on purpose.
//
// Refs: hk-t7s
func skipRealDaemonE2EInShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("real-daemon E2E — excluded from per-bead commit_gate (-short); runs in full CI lane. TODO un-shelve: hk-p258q")
	}
}
