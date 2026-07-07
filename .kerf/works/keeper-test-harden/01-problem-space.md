# 01 — Problem Space: keeper-test-harden

Source: `plans/2026-07-06-quality-system/11-keeper-test-design.md` (full design already produced and
operator-endorsed prior to this kerf work; this pass distills it into jig form rather than re-deriving).

## Summary

The keeper (per-session context-fill watcher, `internal/keeper/` + `cmd/harmonik/keeper_*`) is already
the most-tested subsystem in the tree (~50 test files: pure-unit, an in-process reactive harness, and a
real-tmux session-twin `cmd/harmonik-twin-session`). Despite that, two live incidents bit the fleet this
month with no regression coverage: B4 (`restart-now` aborts `no_tmux_target` despite a healthy pane-bound
watcher — stranded the admiral) and B3 (`watch` re-stalls every ~5–25 min with no self-healing path). This
work closes those gaps and locks a 6-scenario acceptance corpus as a permanent regression floor.

## Goals

- Fix B4: `restart-now`'s tmux-target resolution seam must return the live pane for a healthy watcher;
  `RunOnDemand` must not abort `no_tmux_target` when a pane is actually bound.
- Fix B3: wire and prove the live-pane `ForceRestart` auto-recover path for the `watch` session end-to-end.
- Lock the 6-scenario acceptance corpus (restart-now resolve, sid-rebind, nonce-gate no-truncate, B3
  auto-heal, hold invariants, binary-upgrade migration) as a named, permanently-green conformance set.
- Do all of the above using the harness seams that already ship (`tmuxRunFn`, `CyclerConfig` injectable
  fns, the reactive session fake, the real-tmux twin) — no new harness layer.

## Non-goals

- Building a keeper test harness from scratch (it exists).
- Crew-restart end-to-end re-hydration (T4) — explicitly deferred, gated behind the dispatch lane's
  `core-loop-proof` scratch-daemon substrate. Bead created but not worked.
- Any change to the band/threshold values themselves (locked decision — no band retune, no hardcoded
  thresholds; memories `feedback_no_hardcoded_keeper_thresholds`, `feedback_keeper_band_no_retune`).
- Touching the daemon-dispatch lane's surface (harness→model→provider→commit) — disjoint from keeper.

## Constraints

- Build on `integration/keeper-test` in an own worktree; never commit to `main`. Daemon executes only;
  integration→main is a human-gated PR.
- Keeper/daemon-core changes need a GATE-0 isolated e2e test proving the fix before the commit lands.
- Prefer Codex for build work (Claude cap ~98% this session); pi is blocked (hk-4ir08).
- Every bead labeled `codename:keeper-test-harden` (bead_filter match).
- Disjoint from `scripted-twin`/`scratch-substrate` epics — runs concurrently, zero contention, except T4.

## Success criteria

- B4: a live-named-tmux integration test exercises `ResolveTmuxTarget` + `RunOnDemand` and asserts no
  `no_tmux_target` abort when a pane is genuinely bound; test is red before the fix, green after.
- B3: a scenario test drives a stale-gauge + alive-pane + mangled-target state and asserts the gated
  `ForceRestart` fires exactly once, re-resolves the target, and does not loop or alert-storm.
- All 6 acceptance-corpus scenarios pass as a named, CI-visible conformance set.
- T4 bead exists, is labeled, and is blocked (dep or note) on the substrate bead — not dispatched.
