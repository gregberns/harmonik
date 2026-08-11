# C21 dispatch replay design

Status: design review

Date: 2026-08-11

## Goal

Replay each durable dispatch intent before the daemon enables new dispatch.
Each replay returns one LB-004 result. Replay does not use events or process
names as authority.

## Current ordering problem

The current boot path resets dead run sessions before queue startup recovery.
It can therefore change Beads before a dispatch intent classifies the same run.
The current scheduler also writes the queue reservation before any dispatch
intent exists.

The current durable run record is not universal. `internal/run.Record` exists
only for a local run that gets an independent tmux session. A remote run and a
local run without that session can reach the live registry with no run record.
C21 cannot call such a run `run_durable` or `handoff_durable` under LB-007.

## Required slices

### C21a — Durable intent store

Add a filesystem adapter outside `internal/dispatch`.

The adapter owns `.harmonik/dispatch-intents/<run-id>.json`. It supports these
operations:

- create `prepared` with no replacement
- replace an exact prior phase with the next phase
- load one exact run ID
- list all regular intent files
- remove one exact terminal intent and sync the parent directory

Each write uses temp, file sync, no-replace or exact compare, rename, and parent
sync. An ambiguous write reloads exact bytes. A conflicting file returns
`repair-required` and remains unchanged.

The intent root must be a real directory. It must not be a symlink. Each entry
whose name has the intent shape must be a regular non-symlink file with valid
content and matching identity. A wrong-type, corrupt, or conflicting entry
fails startup closed before an orphan sweep runs. The adapter must not skip
such an entry as if no owner exists. This rule implements LB-005 and LB-009.

### C21b — Universal run record and spec amendment

Replace the session-only meaning of `internal/run.Record` with a dispatch run
record that every claimed queue run writes. The record uses the C20 base
binding. It adds the selected execution location and the intended session
identity when those facts become known.

The session adopter reads only records that contain an adoptable local session.
Remote and shared-session records remain valid run records. They do not claim
that a local tmux session exists.

This slice amends LB-002, LB-003, LB-004 D8 and D9, and LB-007. It also amends
the C20 contract.
`RunBinding.SessionName` cannot be required at `run_durable`. The session
identity becomes required only at `handoff_durable`. `run_durable` binds the
universal run-record ID. Handoff must match the run record's session identity.

The run-record adapter uses no-replace creation for the base record. It uses
exact-byte compare-and-swap to add execution location and session identity.
It syncs the record and parent directory before the scheduler advances the
dispatch intent. A conflict returns `repair-required`. The scheduler must make
the session identity durable in the run record and intent before it starts the
session.

### C21c — Scheduler transaction wiring

The scheduler uses this order for one dispatch:

1. Mint the run ID and claim transition ID.
2. Create the prepared intent.
3. Commit the queue reservation with the same run ID.
4. Claim the bead with the same transition ID.
5. Advance the intent to claim-durable.
6. Write the universal run record.
7. Advance the intent to run-durable.
8. Create or select the execution handoff.
9. Advance the intent to handoff-durable.
10. Register and launch the run.

A definite intent-create failure stops before reservation and leaves no intent.
An ambiguous create reloads the exact path. It proceeds only when the exact
prepared bytes are durable. After a durable prepare, replay performs any
missing reservation with the same IDs. An error after step 3 also leaves the
intent for replay unless an exact compensation transaction becomes durable.
The scheduler does not mint replacement IDs during a retry.

### C21d — Startup classifier and executor

Run dispatch-intent replay before dead-session reset, queue reconciliation, and
new dispatch. The classifier reads the intent first. It then reads the exact
queue item, Beads status, run record, worktree lease, and session identity that
the intent names.

The classifier is pure. It returns one action from LB-004. The executor applies
that action through queue, Beads, run-record, and session ports. It advances or
removes the intent only after the required durable fact exists.

## Startup order and spec amendment

Amend PL-005 and PL-006 to use this order:

1. Recover queue replace intents and completion transactions.
2. Recover dispatch intents against durable queue files and Beads.
3. Run generic tmux, worktree, and bead orphan sweeps only for identities that
   no dispatch intent owns.
4. Adopt or reset unowned session records.
5. Load and reconcile canonical queues.
6. Install queues in QueueStore.
7. Enable the work loop.

The dispatch replay result controls whether the later orphan sweep can touch a
run. The orphan sweep cannot reset an intent-owned claim.

## Fault matrix

| Cut | Required replay result |
| --- | --- |
| Prepared intent only | Replay the reservation with the same IDs |
| Prepared plus exact reservation | Replay claim with the same IDs |
| Claim accepted before phase update | Classify Beads status and advance or repair |
| Claim-durable before run record | Write the same universal run record |
| Run record before phase update | Validate exact identity and advance phase |
| Run-durable before handoff identity | Create the same handoff identity and make it durable |
| Handoff identity durable in run record, intent still run-durable | Validate exact identity and advance the intent |
| Handoff-durable, session absent | Start the bound session once |
| Handoff-durable with a live session | Adopt exactly once |
| Handoff-durable with a dead session | Resume or repair from durable run facts |

The session must not exist before the run record and intent contain its durable
handoff identity. This reverses the old LB-004 D8 and D9 order. The spec
amendment must replace those rows and add the handoff-durable, session-absent
crash cut before scheduler wiring lands.

## Stop conditions

C21 must not start implementation until review closes these points:

- the universal run-record shape for local, remote, and shared-session runs
- the C20 phase amendment for session identity
- the exact startup order relative to queue namespace recovery
- the ownership rule between dispatch replay and the old orphan sweeps
- the action for a prepared intent with no reservation

## Landing units

Review C21a, C21b, C21c, and C21d as separate units. C21a and C21b can land
before the producer changes. C21c must not land without C21d. The safe landing
order is C21a, C21b, then one atomic integration of C21d replay and C21c
producer wiring. No committed integration tree may create dispatch intents
that startup cannot replay.
