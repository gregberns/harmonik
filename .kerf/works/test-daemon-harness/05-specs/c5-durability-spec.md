# Change Spec — C5 Durability & standing-capability wiring

## S-C5.1  `make scratch-daemon-smoke` target
- Add a Makefile target (non-colliding with the existing `smoke-scratch`) that runs
  `scripts/scratch-daemon-smoke.sh` (default hermetic phases) with a `## ...` help
  comment so it appears in `make help`.
- **Done:** `make help` lists it; `make scratch-daemon-smoke` exits 0 on a clean tree.

## S-C5.2  `gc` / `reset` subcommand on scratch-daemon.sh
- New subcommand: `gc <scratch-path> [--hard]`.
- Default: prune the scratch clone's daemon-created git worktrees
  (`git -C <scratch> worktree prune` + remove leftover dirs) and delete
  `<scratch>/.harmonik/batch-*.json` artifacts. `--hard`: additionally reset the
  scratch beads DB to a clean baseline.
- MUST inherit `guard_path` (scratch != fleet) and `assert_not_supervised`. MUST
  refuse (or first `down`) if the scratch daemon is currently UP. MUST NEVER touch
  anything outside `<scratch>`.
- Emit a stable `GC_SUMMARY worktrees_pruned=<n> artifacts_removed=<n> hard=<0|1>` line.
- Wire into `usage()` + the runbook command table.
- **Done:** `gc` reclaims disk on a scratch clone, refuses on the fleet path and on a
  live daemon, leaves the fleet untouched; smoke covers the refusal guards.

## S-C5.3  Runbook: standing-capability section
- Add a short "Durable standing capability" section to the runbook: when to `gc`, the
  CI/regression-gate contract, and the `make` entry point. (Documentation close-out of
  the problem-space "promote to a documented STANDING capability" goal.)
- **Done:** runbook documents gc + the regression gate + the make target.

## Open decision surfaced to operator
`gc --hard` beads-DB reset default: spec defaults to worktrees+artifacts only, with DB
reset behind `--hard`. If the operator prefers `gc` to always start fully clean, flip
the default. Noted, not blocking.
