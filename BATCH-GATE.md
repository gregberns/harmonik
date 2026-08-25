# Batch gate — how charlie's work reaches the alpha branch

Operator directive, 2026-08-24. Replaces per-bead landing on `work/alpha-integration-merge`.

## The model

1. Beads land on **`work/charlie-batch-1`**, never on the alpha branch.
2. When the batch is ready, it goes through the gate below.
3. Only a batch that passes the gate merges into `work/alpha-integration-merge`.

Enforced in `.harmonik/branching.yaml`: `lands_on: work/charlie-batch-1`, and
`work/alpha-integration-merge` is in `protect_branches` so a per-bead landing on it is refused.

**The two keys do NOT share one loading rule.** One edit is inert until you restart. The other
takes effect at once, on work that is already running. Verified in code on 2026-08-24.

- `protect_branches` is read ONCE, at daemon boot. `branching.Load` runs in `daemon.Start`, the
  result is frozen into the run environment (`internal/daemon/bootworkloop.go`, field
  `ProtectBranches`), and every run plan tests against that frozen copy. **Edit it while the daemon
  runs and the edit does nothing until a restart.** A branch you just protected is not protected.
- `lands_on` is re-read on EVERY run plan. `resolveBranchingFrom` calls `branching.LoadCached`,
  and `LoadCached` reloads the file when its modification time changes, so the next run picks up
  the new value.
  **Edit it mid-batch and live runs silently re-target.** `start_from` behaves the same way.

**The `daemon_config` event cannot confirm the landing target.** It prints the boot snapshot, so it
tells you the truth about `protect_branches` and a possibly stale value for `target_branch`. To know
where work actually went, read where the commit landed: `git log --oneline work/charlie-batch-1`.

## The gate

**The gate runs on the MERGED tree, not on the batch tip.** Cut a clean throwaway worktree
detached at the CURRENT integration tip, merge the batch into it, and gate that. Never gate in
the main checkout, which carries other sessions' uncommitted work.

```bash
git worktree add --detach <scratch>/gate work/alpha-integration-merge
git -C <scratch>/gate merge --no-commit --no-ff work/charlie-batch-1
```

  A green gate on the batch tip is not a green gate on what ships. The integration branch runs
  tens of commits ahead of the batch's merge base, so the batch tip is a tree that will never
  exist anywhere after the merge. Gating it answers a question nobody asked. Corrected
  2026-08-24, after a session measured integration at 31 commits ahead of the batch and the doc
  still said to gate the batch tip.

  **Record what you gated BEFORE you merge, not after.** The merge above is `--no-commit`, so
  `HEAD` in that worktree still points at the integration tip and `git log -1` reports a commit
  that contains none of the batch. The two facts that identify the gated tree are the base you
  cut and the content you merged onto it:

  ```bash
  git -C <scratch>/gate rev-parse --short HEAD      # the base — capture this BEFORE the merge
  git -C <scratch>/gate diff --cached --stat | tail -1   # what the merge brought in
  ```

  The merge target moves while you work. Re-cut the worktree and re-run both if the integration
  tip changed between the merge check and the gate.

**Step 1 — the whole-branch build.** `make full`. Not `make core`. The daemon's per-bead
commit gate runs `make core` (29 packages); `make full` is the merge decision and nothing
in the daemon path runs it. This step is why the batch exists.

  Know what `make full` does NOT give you. Its main sweep is `go test -short ./...`, so
  every `testing.Short()` guard is skipped. It does boot the real binary — but only via
  `test-subprocess`, one ~11-second smoke test, plus the scenario tier. That is thin
  end-to-end coverage, not absent. Step 2 exists because of it; do not treat a green
  `make full` as evidence the software works.

**Step 2 — real end-to-end runs, by sub-agents.** A green suite is not evidence the system
does what we want. Spin the software up and put real work through it. At minimum:
  - Boot a daemon built from the MERGED tree you gated in Step 1 — the same worktree — against a
    THROWAWAY project dir, never this repo. A daemon built from the batch tip is not the software
    that ships, for the same reason Step 1 does not gate that tree.
  - Submit a real bead and watch it reach a terminal state.
  - Exercise the paths this batch touched.
  - Confirm the failure modes still fail: a protected-branch landing is refused; a red
    gate does not merge.
Each sub-agent reports what it RAN and what it OBSERVED, not what it read.

**Step 3 — merge, then turn the batch over.** Only if steps 1 and 2 both pass. Merge the
batch into the alpha branch as one reviewed unit. Then cut `work/charlie-batch-2` from the
new alpha tip.

  **Do the turnover in this order. The order is the safety.** `lands_on` re-targets live
  runs the moment the file changes, so an edit made while work is in flight sends that work
  to the new branch before the restart that you think gates it.
  1. Confirm NOTHING is in flight: `harmonik queue status --queue charlie-batch` must show
     `in flight: 0`.
  2. Cut the new branch FIRST: `git branch work/charlie-batch-2 work/alpha-integration-merge`.
     Name it in `branching.yaml` before it exists and every bead is refused at `start_from`
     and reopened.
  3. Edit both keys in `.harmonik/branching.yaml`.
  4. Restart the daemon, so the new `protect_branches` list takes effect.
  5. Confirm the new landing target by where the next commit goes, not by an event:
     `git log --oneline work/charlie-batch-2`.

  **A restart that adds `--protect-branch` flags silently DROPS the whole YAML list.** The
  flags REPLACE the file's list, they do not add to it (`bootconfig.MergeBranchingDefaults`
  falls back to the YAML only when no flag is given). Restart with this exact command, which
  passes no branch flags:
  `/Users/gb/go/bin/harmonik start daemon --project /Users/gb/github/harmonik --no-auto-pull`

## Known gaps this gate does not close

- The review-node verdict never reaches the commit trailer, so merged history understates
  its own review coverage (`hk-review-verdict-never-reaches-trailer-ci43q`).
- A failed run erases its review findings; the retry starts blind
  (`hk-run-retry-loses-review-context-wmypv`).
