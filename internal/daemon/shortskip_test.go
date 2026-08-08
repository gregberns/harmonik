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
// `hk-p258q` twice as the issue tracking the un-shelve, and the file itself was
// named after that id. The id resolves to nothing today — checked across 260
// open and 3,384 closed beads on 2026-08-07 — so nothing was ever going to
// un-shelve these tests, and the shelving became permanent by default rather
// than by decision.
//
// It resolves to nothing. It was not invented: commit 71fafcbe0, "test(hk-p258q):
// fix macOS sun_path overflow in operator-pause socket fixture", names it in the
// subject line, so it was a real bead that has fallen out of a ledger that is
// machine-local and gitignored. That is the general hazard, and the reason this
// comment explains itself instead of pointing at an id. An id in a comment is
// live where it was written and dead everywhere else.
//
// The old comment also claimed "~21 of them are red on main HEAD for
// environmental reasons". Measured 2026-08-07 at this HEAD: `go test -count=1
// ./internal/daemon/` with no `-short` gave 820 passes and ZERO failures. That
// claim was stale by a wide margin and had been treated as a reason not to look.
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
