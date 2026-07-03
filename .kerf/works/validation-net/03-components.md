# validation-net — Decomposition

Five components. C1 (the flagship concurrency coverage) depends on C2 (the reusable fixture). C3/C4/C5 are
independent and can run in parallel. Priority reflects blast-radius × current coverage-gap (from the behavior catalog).

## C1 — Concurrency-class scenario coverage  *(closes the hk-37giq gap; P0)*
The exact uncovered surface that hid the 2-week wedge. All tests boot a real `daemon.Start` composition root with a
**watchdog-engaging twin** (NOT `single-happy-path`).
- N≥3 concurrent dispatch → all reach merge, no `launch_stall`/`run_stale`, event-ordered lifecycle, under `-race`.
- Concurrent launch-liveness + spawn-semaphore no-leak (slots reclaimed after teardown; `newWindowMu` doesn't wedge).
- Multi-bead merge: A conflicts → auto-skips/reopens while B proceeds; N near-simultaneous completions merge
  one-at-a-time with no lost commit.

## C2 — Scenario-test infrastructure  *(enabler for C1; P0)*
- Promote `scenario_concurrent_multiqueue_hkumemp` into a parameterized, reusable
  `RunConcurrentMerge(t, N, twinScenario)` fixture in `internal/daemon/scenariotest`.
- A **watchdog-engaging twin scenario** in `cmd/harmonik-twin-claude` (heartbeat-then-delay, so `waitAgentReady` and
  `pasteInjectQuitOnCommit` genuinely contend) — the "missing middle altitude" the flagship needs.
- `make test-scenario` target (<10 min) + documented determinism recipe.

## C3 — commit_gate liveness + gate-efficacy  *(P1)*
- Failing commit_gate terminates via the traversal cap as `run_failed` (the `hk-pj4b6` loop), NOT an infinite
  `commit_gate→implement` loop. Pairs with the `hk-pj4b6` fix as its regression guard.
- A genuinely RED scenario test actually BLOCKS a real merge (today the gate is **fail-open**; nothing asserts efficacy).

## C4 — Run lane + quarantine restoration + flake policy  *(P1)*
- Stand up a real CI lane (none exists — local `make` only) running `make check` / `check-full` + the scenario tier.
- Restore the 21 `-short`-quarantined real-daemon E2E tests (`hk-p258q`) in a separate non-merge-blocking lane.
- Flake policy: fold `hk-6ra3p` (flaky `TestReviewLoopBridge_CHB009`) + `hk-3hf9n` (ENOSPC-timeout reds) into one
  de-flake-or-quarantine-with-policy track.

## C5 — Under-tested subsystem coverage  *(P2; stretch)*
- Comms at-least-once redelivery + consumer dedupe-on-`event_id` (the normative N3 guarantee — cursor/presence
  covered, redelivery-and-absorb not).
- Review-loop verdict-absent salvage (known transient; manual cherry-pick today, no scenario pins it).
- (Crew coverage is captain-owned — reference `hk-rbpss`/`hk-zi4ej`, do not duplicate.)

## Cross-references (fix beads under test — do NOT duplicate)
`hk-37giq` (tap fan-out, the flagship's subject) · `hk-goczd` (newWindowMu slowdown + slot-leak) ·
`hk-pj4b6` (commit_gate no-escape) · `hk-tcenh` (pre-spawn wedge never budget-fails) ·
`hk-6ra3p`/`hk-3hf9n` (flaky reds) · `hk-p258q` (quarantine lineage) · `hk-n7fw3` (fold scenario gate into the dot).
Feature-owned same-class beads to reference: `hk-tfxjp`/`hk-zi4ej`/`hk-rbpss` (captain), `hk-vfmn9` (codex).
