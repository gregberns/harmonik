# validation-net — Problem Space

## Summary

A latent, concurrency-only daemon bug (`hk-37giq` — the `tapCh` competing-consumer race) survived **~2 weeks
undetected** and cost ~7 hours / a 12-agent fan-out to root-cause. It hid because **no scenario or high-level test
exercised concurrent real-bead dispatch through the real spawn/heartbeat/watchdog wiring.** `validation-net` closes
that class of gap: build the missing high-level (scenario / integration) tests that validate the daemon's
load-bearing behaviors — starting with concurrent dispatch — and stand up a lane that actually *runs* them.

Full incident analysis: `postmortem-concurrent-dispatch-wedge.md` (this bench).

## Goals

1. **Close the concurrency-coverage gap (flagship).** An automated test that dispatches N≥3 beads concurrently
   through the real heartbeat/launch/watchdog path and asserts all reach merge with no `launch_stall`/`run_stale`
   wedge — the exact surface that hid `hk-37giq`.
2. **Cover the other high-blast-radius behaviors** the postmortem + behavior catalog surfaced as
   partial/no-coverage: spawn-semaphore liveness, multi-bead merge serialization + conflict-skip, commit_gate
   loop-escape, comms redelivery/dedupe, review-loop verdict-absent.
3. **Make scenario tests cheap to author and runnable.** A reusable parameterized concurrency fixture, a focused
   `make` target, and a documented determinism recipe — so this class of test stops being a 30-min,
   worktree-sub-agent-only chore.
4. **Stand up a lane that runs them.** There is **no CI** today (local `make` only) and the per-bead commit_gate
   is fail-open + skips scenario tests + skips the 21 `-short`-quarantined E2E tests. Restore the quarantined set
   and run the scenario tier somewhere that isn't "a human remembers to run `make check-full`."

## Non-goals

- **Not a new testing philosophy.** `docs/methodology/TESTING.md` (5-layer model: unit/integration/scenario/
  crash/property) and `docs/foundation/project-level/testing.md` already define it. The gap is **execution**, not doctrine.
- **Not re-fixing the bugs.** `hk-37giq` is fixed; `hk-goczd`/`hk-pj4b6` are separate open fix beads. validation-net
  writes the *tests that would have caught them* and references the fix beads — it does not own the fixes.
- **Not duplicating feature-owned scenario beads.** The captain (`hk-tfxjp`/`hk-zi4ej`/`hk-rbpss`) and codex
  (`hk-vfmn9`) scenario beads stay with their works; validation-net references them as the same-class exemplar.
- **Not real-claude/token-burning tests by default.** Twin-driven, deterministic, token-free wherever feasible.

## Constraints

- **The per-bead commit_gate SKIPS `//go:build scenario` tests** and runs `go test -short` — so scenario-test beads
  must be authored via a **worktree sub-agent + targeted fast gate + cherry-pick** (the daemon's 30-min commit
  budget times out on real-daemon-boot tests) and **re-run by hand** under the supervisor. (`reference_scenario_test_authoring`.)
- **The flagship bug class needs the watchdog ENGAGED.** The existing N=2 concurrency scenario
  (`scenario_concurrent_multiqueue_hkumemp`) uses the `single-happy-path` twin, which short-circuits the
  heartbeat/launch interplay → would NOT have caught `hk-37giq`. The flagship test must use a twin scenario
  (`commit-on-cue-startup-delay` / `silent-hang`, or a new one) that forces `waitAgentReady` and the
  `pasteInjectQuitOnCommit` watchdog to run concurrently against the real per-run taps.
- **Daemon-touching beads currently can't LAND** until the commit_gate no-escape loop (`hk-pj4b6`) is resolved
  (named-queues owns that). Scenario beads are gate-skipped so this affects fewer of them, but the infra/CI beads
  that touch `internal/daemon` or the Makefile are gated on that fix.
- **Determinism recipe is non-obvious** — only discoverable by reading `hkumemp`'s comments: worktree-factory
  pre-commit + merge-mutex + phase-aware twin wrapper + `Skip*` flags.

## Relationship to prior work

A prior kerf work **`testing-strategy-uplift`** (2026-05-20, status `integration`, **zero beads ever filed**) covers
overlapping intent with a ready 8-track SPEC at `~/.kerf/projects/gregberns-harmonik/testing-strategy-uplift/SPEC.md`.
**Decision: `validation-net` is the active execution vehicle and SUPERSEDES it** — pull its reusable SPEC content as
input; do not run two parallel testing works. (Flag to operator; reversible.)

## Success criteria (concrete, verifiable)

1. A `go test -race -tags=scenario` test dispatches N≥3 beads concurrently through the real spawn/heartbeat path and
   asserts every bead reaches `run_completed` + merges to `main` + closes, with zero `launch_stall_detected`/`run_stale`
   terminal wedge. Reverting `53ead2aa` (the fan-out fix) makes this test FAIL. ← the regression guard the incident lacked.
2. A reusable `RunConcurrentMerge(t, N, twinScenario)` fixture exists in `internal/daemon/scenariotest` and backs ≥2
   scenario tests.
3. `make test-scenario` runs the scenario tier in <10 min without a human assembling the command.
4. The 21 `hk-p258q`-quarantined E2E tests run green in a dedicated non-merge-blocking lane.
5. Each behavior in the catalog's P0/P1 shortlist has either a filed test bead or an explicit "covered by X" note.
