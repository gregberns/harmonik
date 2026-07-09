# Research — C5 Durability & standing-capability wiring

## Existing idioms to mirror
- **Makefile:** `make smoke-scratch` (line 57-63) runs `scripts/smoke-scratch.sh`
  with a `## ...` help comment surfaced by `make help`. A new target follows the same
  shape. Naming caution: `smoke-scratch` already exists and means the *harmonik*
  smoke; the new one is the *scratch-daemon* smoke -- use a non-colliding name, e.g.
  `scratch-daemon-smoke`.
- **Guards:** every mutating subcommand routes its path through `guard_path`
  (scratch != fleet, symlink-resolved) and, if it stops/starts a daemon,
  `assert_not_supervised`. A new `gc`/`reset` subcommand MUST do the same.

## `gc` / `reset` subcommand — design
Purpose: keep the durable inner loop fast by reclaiming disk WITHOUT re-cloning
(re-clone is the slow path `init` already covers). Disk<10GiB is a known
merge-failure trigger (disk-pressure cache wipe -> merge_build_failed).

Prunes (all UNDER the scratch clone only):
- stale git worktrees the daemon created (`git -C <scratch> worktree prune` + remove
  leftover worktree dirs);
- `<scratch>/.harmonik/batch-*.json` results artifacts;
- optionally the scratch beads DB back to a clean baseline.

Safety: refuse if the scratch daemon is UP (a running daemon owns those worktrees) --
require a `down` first, or have `gc` call `down`. Inherit `guard_path` +
`assert_not_supervised`. NEVER touch anything outside `<scratch>`.

Open choice (leave to implementer, noted in spec): default prunes worktrees+artifacts
only; `--hard` also resets the beads DB. Softer default is safe mid-campaign; `--hard`
is the "start clean" reset.

## CI gate — options considered
- **Committed CI workflow** running the hermetic smoke on `scripts/scratch-daemon*.sh`
  changes. Cleanest; depends on the repo's CI substrate. Recommended if a runner
  exists.
- **Pre-merge documented rule** ("run `make scratch-daemon-smoke` before merging
  changes to this script") tracked in build-practices. Fallback with zero infra;
  weaker (relies on discipline).
Deliverable prefers CI if available; else the documented rule.

## Non-goals reaffirmed
Multi-machine/N-worker scheduling and scratch-daemon auto-revive stay OUT (problem-
space non-goals; bootstrap-trap avoidance is the whole point of standalone).
