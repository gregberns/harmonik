//go:build scenario

package daemon

import "testing"

// skipRealDaemonE2EInShort — internal-package copy of the daemon_test helper of
// the same name in shortskip_test.go. Read that file for the rule. This copy
// exists only so the internal-package scenario tests can reach it.
//
// epiccompleted_scenario_hktfxjp_test.go lives in `package daemon` (it touches
// unexported internals: emitBeadClosedAndMaybeEpic, maybeEmitEpicCompleted, the
// emittedEpics guard). An internal-package test file cannot reference symbols
// declared in `package daemon_test`, so it cannot see the original helper —
// referencing it broke the whole `-tags scenario` build. `package daemon` and
// `package daemon_test` compile as separate units inside the test binary, so
// this same-named helper does NOT collide with the daemon_test one. Body is kept
// identical on purpose.
func skipRealDaemonE2EInShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("real-daemon E2E — excluded from -short (the inner loop). Runs in `make core` and `make test-scenario`.")
	}
}
