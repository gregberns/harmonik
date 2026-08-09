# Parked branches, as of 2026-08-09

A branch name is not a record. Seven snapshots were committed on 2026-08-04 with
bodies that say "preserve in-progress work ahead of a worktree cleanup", and
nothing pointed at any of them for five days. This file is the pointer. Read it
before you delete a branch, and delete an entry here when its branch is gone.

Everything that was FINISHED is already on `work/alpha-integration-merge`. What
is listed here is either unfinished, deliberately not for merging, or superseded.

## Do not merge — these hold deliberately broken code

Both were written to answer "does any test catch this?", and both say so in
their own commit bodies. Merging either injects a real fault.

| Branch | What it breaks |
|---|---|
| `worktree-wf_865dc389-f6b-5` | Removes the `unlink` call from `completeAndUnlinkResult` in `internal/queue/persistence.go`. |
| `worktree-wf_865dc389-f6b-6` | Disables the pre-claim status guard in `internal/daemon/scheduler.go` with `if false &&`. |

`worktree-wf_865dc389-f6b-6` also carries genuine work — workloop and
concurrent-database test hardening, and a new `workloopfixture_dotgraph_test.go`
of about 150 lines. That part is worth having and is NOT lost, but it does not
cherry-pick: it moves five call sites onto a `workloopFixtureCommittingHandler`
helper that replaces the current `workloopFixtureAdvanceHeadHandlerArgs` shape,
and lane bravo has since rewritten the same tests. Landing it is a rewrite
against today's fixtures, not a merge. Drop the `scheduler.go` hunk first
whatever else is done.

## Unfinished, still open

| Branch | What it holds | Why it did not land |
|---|---|---|
| `work/codex-continuity` | A draft `internal/continuity` package: types, a reducer, a reducer test, about 400 lines. | Never built, linted or tested. No production caller. Landing it adds an unreferenced package. |
| `worktree-wf_865dc389-f6b-2` | Repairs two faults in `scripts/scenario-gate.sh`: the file-to-package mapping never checked that the directory still existed, and the classifier asked "did this fail to build?" before "did a test fail?", so one deleted package made every unit failure in the repo pass the gate. Adds `scripts/scenario-gate-test.sh`. | It wires the new test into `check-short`, a Makefile target that does not exist. The fault it repairs is real and the note in its header records that `internal/runloop/scenariogate.go` still carries both faults, so the Go side and the shell side disagree. This one is worth finishing. |

## Superseded — verified, nothing to recover

Checked symbol by symbol on 2026-08-09, not by reading the branch names.

- `worktree-agent-a0d55a1fec3aff93e`, `worktree-agent-ab8b335bdb1c01573`,
  `worktree-agent-a03df0a7cdd1a053b` — older snapshots of lane bravo's own work.
  Bravo's versions are later and strictly larger. The only content unique to them
  was a one-line scratch file.
- `worktree-agent-accdc0370b3951f74` — landed as `a4981d686`.
- `work/bravo-queue-dogfood-artifacts` — already applied.
- `worktree-wf_865dc389-f6b-3` — the `workflow_id` fixture repair landed, with the
  fixture names given a `-fixture` suffix.
- `worktree-agent-ac4d5c4d74bf78119` — the Step 7 mode-boundary measurement landed
  and then grew from 417 lines to 721.
- `work/alpha-sat32` — see below.

## `work/alpha-sat32` — consolidated by content, not by merge

This branch will keep showing as unmerged and that is correct. It carries an
extended version of the run-registry repair. A trimmed variant of the same
commit, from the same parent, landed 35 minutes earlier instead. Merging the
branch means merging a commit against its own twin, which conflicts in seven
files for no gain.

Its remainder was taken apart instead. The boot sweep's live-run exemption, the
tmux teardown-and-retry, and the `hasRunner` repair had all reached the tree by
other routes and were confirmed present. Two pieces had not, and both are now on
the integration branch: the `specs/run-state-machine.md` correction, and the test
covering the live monitor giving a bead back when an adopted agent exits.

Nothing further is owed to this branch. It can be deleted.
