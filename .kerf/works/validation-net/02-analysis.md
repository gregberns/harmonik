# validation-net — Analysis (coverage-gap synthesis)

Synthesized from a 5-agent research fan-out (postmortem reconstruction, coverage inventory, behavior catalog,
infra assessment, bead/prior-art scan). Anchors in those agents' findings + the postmortem.

## Finding 1 — the strategy exists; the gap is EXECUTION
`docs/methodology/TESTING.md` already mandates a 5-layer model (unit / integration / scenario / crash / property),
*"every non-trivial behavior testable without tokens,"* *"one scenario per workflow-library entry,"* and
`-race` on every CI run of the scenario suite. `docs/foundation/project-level/testing.md` defines the build-tag
tiers and naming. **validation-net does not write a new philosophy** — it executes the one already documented,
which was never carried out for the concurrency class.

## Finding 2 — concurrent dispatch is tested, but never through the path the bug lived in
Tests DO dispatch 2+ concurrent beads (`TestSmoke_MultiBead_MaxConcurrent2`, `TestWorkLoop_TwoConcurrentBeads`,
`scenario_concurrent_multiqueue_hkumemp` at N=2, `t11_throughput` at N=10). **But every one bypasses the real
`pasteInjectQuitOnCommit` heartbeat-tap + `waitAgentReady` contention** via `/bin/sh` stubs, fake `tmux.Adapter`s,
or `single-happy-path` twins + `emptyCommitWorktreeFactory`. The hk-37giq fix shipped only a **narrow unit test**
(`workloopeventsource_hk37giq_test.go`) on the tap primitive. **The end-to-end gap that allowed the 2-week hide is
still open** — closing it (VN4 flagship, driven by the VN2 watchdog-engaging twin) is the work's keystone.

## Finding 3 — the gate is fail-open, scenario-skipping, and there is no CI
- Per-bead `commit_gate` runs `go build && go vet && bash scripts/scenario-gate.sh`; the affected-unit step uses
  `-short` (so the `hk-p258q` quarantine IS in effect) and the gate is **fail-open** (timeout/compile → ALLOW).
- The gate **skips `//go:build scenario`** tests except for scenario-touching changes → scenario tests run only via
  `make check-full` (Tier 3) — and **there is no `.github/workflows`; CI is local `make` only.** So scenario tests
  and the 21 quarantined E2E tests run only when a human remembers. (VN9 stands up a lane; VN10 restores the E2E set.)
- Nothing asserts a genuinely-RED scenario test actually BLOCKS a merge (VN8).

## Finding 4 — behavior catalog: P0/P1 coverage gaps (blast-radius × gap)
1. **P0** N≥3 concurrent dispatch all-reach-merge under `-race` (the hk-37giq surface) → VN4.
2. **P0** concurrent launch-liveness + spawn-semaphore no-leak (`newWindowMu` hk-goczd, slot-leak tmuxsubstrate.go:216) → VN5.
3. **P1** multi-bead conflict-skip "others proceed" + N-completion `mergeMu` serialization (only N=2 indirect today) → VN6.
4. **P1** commit_gate failing-gate terminates via traversal cap, not infinite loop (hk-pj4b6) → VN7.
5. **P2** comms at-least-once redelivery + dedupe-on-event_id (cursor/presence covered, redelivery not) → VN12.
6. **P2** review-loop verdict-absent salvage (manual cherry-pick today, no scenario) → VN13.
Well-covered (no new bead): pidfile lock, reconciliation/restart-recovery, queue stream/wave/append, handler-pause,
review-loop modes, the 2026-05-18 gap-audit's 5 P0s (now largely closed). Crew = under-tested but captain-owned.

## Finding 5 — feasibility & the missing twin
A deterministic N-concurrency fixture already exists (`hkumemp`) and generalizes to N≥3 by parameter — promote it to
`RunConcurrentMerge` (VN1). But `single-happy-path` short-circuits the watchdog, so it would NOT reproduce hk-37giq;
the missing piece is a **watchdog-engaging twin** (VN2: heartbeat-then-delay) that makes `waitAgentReady` and the
launch watchdog genuinely contend. Determinism levers: worktree-factory pre-commit, `WithMergeMutex`, phase-aware
twin wrapper, `Skip*` flags. The right altitude split: **unit** test guards the tap primitive (shipped); **scenario**
test guards "N beads all merge through the real path" (VN4).

## Finding 6 — prior work to supersede
`testing-strategy-uplift` (2026-05-20, status `integration`, **0 beads filed**) overlaps; its 8-track SPEC at
`~/.kerf/projects/gregberns-harmonik/testing-strategy-uplift/SPEC.md` is reusable input. validation-net supersedes it
as the execution vehicle. No kerf areas are defined (validation-net stays unassigned).

## Process lesson baked into the acceptance criteria
The incident's root failure mode was *refuting by reasoning instead of by a reproducing test* + *trivial smokes
giving false-green 3×*. So VN4's acceptance is concrete and adversarial: **reverting the fix `53ead2aa` must make VN4
FAIL.** A test that can't fail on the unfixed code is not a guard.
