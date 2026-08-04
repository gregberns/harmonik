# Failed queues: the archive path and the recovery path

Status: design note, written 2026-08-03.
Scope: what happens to a queue that stops at `paused-by-failure`.

This note answers one question that nobody had answered in writing. A failed-queue
archive path already exists. Lane bravo has added `QueueStore.RecoverFailed`. Is
the new transaction a replacement for the archive path, a layer on top of it, or a
second mechanism for the same state?

## Short answer

They are two mechanisms that act on the same state, and they act in opposite
directions.

- The archive path does not recover anything. It **removes** the queue.
- `RecoverFailed` **resumes** the queue where it is.

`RecoverFailed` is not a replacement, because it cannot read an archive. It is not
a layer, because neither path knows the other exists. It is the recovery path this
system did not have. The archive path was never one. Its name has misled every
reader of this code, including the audit that called it "the failed-queue recovery
path".

There is one real defect that comes out of the trace. It is at the end of this
note under "What must change".

## The archive path — trace

Writer: `queue.ArchiveFailedQueue`. It renames

    .harmonik/queues/<name>.json  →  .harmonik/queues/<name>.json.failed-<timestamp>

and fsyncs the parent directory. After the rename there is no live queue file. The
next `harmonik run` sees an empty slot and starts clean. That is the whole purpose:
the comment on the call site in `cmd/harmonik/run.go` says "so re-dispatch is one
command".

Three callers write archives.

1. `cmd/harmonik/run.go`, when a run exits with the queue at `paused-by-failure`.
2. `cmd/harmonik/run.go` again, at the start of the next run, when it finds a
   `paused-by-failure` or `cancelled` queue still on disk. It also archives an
   `active` queue whose owning daemon is dead.
3. `queue.HandlerAdapter.HandleQueueCancel`, the live-daemon `queue cancel` RPC. It
   archives and then clears the in-memory store slot.

Note what caller 3 means. `ArchiveFailedQueue` is the cancel path too. A
`.failed-` archive means "this queue stopped", not "this queue failed".

Four readers read archives. None of them restores one.

- `internal/daemon/statedisk.go` `diskFailedArchives` — counts them into the
  daemon-down state snapshot.
- `internal/daemon/draindetect.go` `DrainDetector.failedArchives` — counts them as
  defence 3, so the fleet does not report itself drained while stopped work sits
  unread.
- `internal/lifecycle/queuearchiveobserver.go` `ObserveQueueArchives` — the boot
  observer. Counts, measures, reports.
- `internal/lifecycle/startup_pl005_qm002.go` `reapOrphanWorktreesFromArchives` —
  reads archive contents to find worktrees to reap. It uses the archive as a record
  of what was running, then throws the record away.

So the archive is **evacuation plus evidence**. Nothing turns it back into work.
A person or an agent must read it and resubmit by hand.

## The recovery path — trace

`internal/queuewiring/store.go` `QueueStore.RecoverFailed` (lane bravo, branch
`work/bravo`, not yet on the candidate branch).

It takes a snapshot of the LIVE queue under its name. If the queue is absent it
rejects. It calls `queue.PrepareFailedRecovery` to build the candidate, then runs
it through `QueueStore.Transact`, which writes through `queue.WriteReplacement`
with a durable receipt binding. On restart,
`queue.RecoverFailedReplaceIntent` finishes a half-done recovery from the durable
intent.

It never touches `.harmonik/queues/*.json.failed-*`. It cannot: it requires the
live queue file, and an archive exists only because that file was renamed away.

## Why they do not collide today, and why that is luck

The precondition of each path is the negation of the other, and the file system
enforces it.

- While `<name>.json` exists, there is no archive for that run, so `RecoverFailed`
  is the only path that can act.
- Once `ArchiveFailedQueue` renames the file, `RecoverFailed` finds no queue and
  rejects.

Whichever fires first destroys the other's precondition. That is not a designed
interlock. It is a side effect of the rename. Neither function names the other,
neither has a fallback, and nothing in the code states which is supposed to win.

## What must change

`queue.HandlerAdapter.HandleQueueCancel` archives a queue on a LIVE daemon and
then clears the store slot. It calls `ArchiveFailedQueue` and
`QueueSetter.ClearQueueByName` directly. It does not go through
`QueueStore.Transact`, writes no replace intent, and binds no receipt.

That makes it a second writer of queue state on the same running daemon that lane
bravo's transaction owner is meant to be the single writer of. It is the one place
where "two mechanisms for one state" is a live problem rather than a naming
problem. PRINCIPLES.md asks for one writer and one explicit state machine.

The fix is to route the cancel RPC through the transaction owner, so the archive
becomes an outcome of a committed transaction rather than a rename that happens
beside it. That work is NOT part of this change. It is recorded here so the next
reader does not have to re-derive it.

## Naming

Two names are wrong and should be corrected when the code around them is next
touched.

- `ArchiveFailedQueue` also archives cancelled queues. The archive suffix says
  `failed` for both.
- Calling the archive path "the failed-queue recovery path" reads as though it
  restores work. It does the opposite.

## ZFC tags

Per `specs/architecture.md` §4.2 AR-005, this note is a description of existing
code and states no new normative requirement. The code paths it describes are all
**mechanism**: renames, globs, stats, typed transitions, and typed errors. None
depends on judgment, so AR-007 imposes no delegation path here and AR-INV-001
holds.

One judgment does exist in this area, and it is deliberately NOT in the daemon:
deciding which archives may be removed. `ObserveQueueArchives` reports the pile
and stops. The counts leave on the `daemon_orphan_sweep_completed` event and reach
the captain through the boot digest, which is where the decision belongs. See the
file header of `internal/lifecycle/queuearchiveobserver.go`.
