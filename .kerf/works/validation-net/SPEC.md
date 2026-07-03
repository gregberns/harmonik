# validation-net — Change Spec (consolidated)

> Build the missing high-level (scenario / integration) tests that validate harmonik's load-bearing daemon
> behaviors — starting with concurrent dispatch — plus a lane that actually runs them. Motivated by `hk-37giq`, a
> concurrency-only wedge that hid ~2 weeks because no test exercised concurrent real-bead dispatch.

**Read first:** `postmortem-concurrent-dispatch-wedge.md` (the incident) · `01-problem-space.md` (scope) ·
`02-analysis.md` (gap synthesis) · `03-components.md` (decomposition) · `07-tasks.md` (the 13 beads + dispatch order).

## What changes
1. **A reusable concurrency fixture + a watchdog-engaging twin** (VN1 `hk-944c2`, VN2 `hk-he18w`) make the bug-class
   reproducible and the test cheap to write.
2. **The flagship guard** (VN4 `hk-ukhzu`): N≥3 concurrent dispatch reaches merge through the real
   spawn/heartbeat/watchdog path, `-race`, no `launch_stall`/`run_stale` wedge. **Acceptance: reverting `53ead2aa`
   makes it FAIL.** Plus launch-liveness/no-leak (VN5) and multi-bead merge serialization/conflict-skip (VN6).
3. **commit_gate liveness** (VN7 `hk-i8g59`: failing gate terminates via traversal cap, the hk-pj4b6 guard) and
   **gate efficacy** (VN8: a RED test actually blocks a merge).
4. **A run lane** (VN9 `hk-l11dr`: CI where none exists) + **quarantine restoration** (VN10 `hk-6hzci`: the 21
   `-short`-shelved E2E tests) + **flake policy** (VN11).
5. **Stretch subsystem coverage** (VN12 comms redelivery/dedupe, VN13 verdict-absent salvage).

## What does NOT change
No new testing philosophy (TESTING.md's 5-layer model stands). No bug fixes (hk-37giq done; hk-goczd/hk-pj4b6 are
separate open fix beads — validation-net writes the tests that catch them). No duplication of captain/codex
scenario beads (reference only).

## Done means
- VN4 fails on reverted `53ead2aa`, passes on main, runs under `-race`.
- `RunConcurrentMerge(t,N,twin)` exists and backs ≥2 scenario tests.
- `make test-scenario` runs the tier in <10 min.
- The 21 quarantined E2E tests run green in a non-merge-blocking lane.
- Every P0/P1 behavior in the catalog has a filed test bead or a "covered by X" note.

## Supersedes
`testing-strategy-uplift` (stalled at integration, 0 beads). Its 8-track SPEC is reusable input.
