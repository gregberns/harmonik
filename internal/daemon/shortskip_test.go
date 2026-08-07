package daemon_test

import "testing"

// skipRealDaemonE2EInShort keeps the heavy real-daemon end-to-end tests out of
// `-short` runs. `-short` is the INNER LOOP and nothing else: `make fast`, and
// the whole-tree step of `make full`.
//
// WHAT RUNS THEM. `make core` — the gate that decides whether beads go through
// the queue, and the gate an assessor sign-off rests on — runs WITHOUT `-short`,
// so every test guarded here runs there. `make test-scenario`, inside
// `make full`, runs them again under `-race` with the scenario build tag.
//
// WHAT THIS IS NOT. It used to say the shelving was temporary and named
// `hk-p258q` twice as the issue tracking the un-shelve. That issue never
// existed, which made the shelving permanent by accident rather than by
// decision, and the file itself was named after it. It also claimed "~21 of them
// are red on main HEAD for environmental reasons". Measured 2026-08-07 at this
// HEAD: `go test -count=1 ./internal/daemon/` with no `-short` gave 820 passes
// and ZERO failures. The claim was stale by a wide margin and had been treated
// as a reason not to look.
//
// SO THE RULE IS NOW A DECISION, NOT A TODO. These tests boot a real daemon,
// spawn real twin binaries and drive real git worktrees. They cost about 145
// seconds on top of a 144-second `-short` run of the core set. That is too slow
// for the edit-compile-test loop and cheap for a gate. Guard a test here when it
// needs a real daemon or a real subprocess. Do not guard one here to make a red
// go away — if a test must stay off, `t.Skip` it naming an issue that exists.
//
// Refs hk-od9d4.
func skipRealDaemonE2EInShort(t *testing.T) {
	t.Helper()
	if testing.Short() {
		t.Skip("real-daemon E2E — excluded from -short (the inner loop). Runs in `make core` and `make test-scenario`.")
	}
}
