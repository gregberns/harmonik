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

#### Exact session start receipt

The handoff record does not prove that a session started. The daemon writes the
session name before spawn. Tmux reports only current state. A workspace session
directory or sidecar can also exist before the handler starts.

C21 adds this durable value:

```go
type SessionStartReceipt struct {
    SchemaVersion int
    Binding       dispatch.Binding
    SessionName   string
    WindowName    string
}
```

`Intent.Handoff` and `DispatchRecord` bind the same deterministic session name
and window name. These fields are immutable after the handoff write. The
receipt repeats the full dispatch binding. It has no clock or process ID.

One canonical naming function owns both target names. It uses this versioned
preimage:

```text
"harmonik-dispatch-target-v1" ||
len(project-real-path) || project-real-path ||
len(run-id) || run-id ||
len(claim-transition-id) || claim-transition-id ||
len(execution-kind) || execution-kind ||
len(worker-name) || worker-name
```

Each length is an unsigned 32-bit big-endian byte count. Each value uses UTF-8.
The project path is the cleaned, symlink-resolved absolute project root. The
UUID values use canonical lowercase text. The worker name is empty for a local
run.

The function computes SHA-256 over the preimage. It uses the first 16 digest
bytes as 32 lowercase hexadecimal characters. The session name is
`harmonik-run-<digest>`. The window name is `run-<digest>`. These strings are
durable wire identity. A schema change must use a new version prefix. The
reader recomputes both names from the durable binding and location. A name that
differs is a conflict.

The exact tmux window carries these immutable user options:

```text
@harmonik-run-id
@harmonik-claim-transition-id
@harmonik-session-name
@harmonik-window-name
```

The bootstrap sets the options on the exact window before it sends the start
acknowledgement. An existing option cannot change. The receipt installer reads
the options through the same local or worker tmux adapter that owns the target.
It compares each value with the intent, run record, and acknowledgement before
it writes the receipt.

The session fact reader also uses the owning adapter. It finds the exact
session and window, reads all four options, and compares them with the durable
facts. It then reads `pane_pid` and `pane_dead` for the exact pane. It reports
`live` only when every option matches, `pane_pid` is positive, and `pane_dead`
is `0`. A matching retained pane with `pane_dead=1` is dead target state. An
absent session, window, or pane is absent target state. An unreadable liveness
probe, an invalid PID or dead value, a partial option set, a duplicate target,
or any identity mismatch is target conflict.

The coordinator owns this receipt root:

```text
.harmonik/dispatch-session-starts/<canonical-run-id>.json
```

The receipt root must be a real directory. The `.harmonik` parent must also be
a real directory. Creation syncs each new directory in its real parent before
the first receipt write. Each receipt entry must be a regular non-symlink file.
The basename must equal `Binding.RunID`. The decoder rejects unknown fields,
unsupported schema versions, non-canonical UUIDv7 values, invalid queue names,
invalid indexes, empty session or window names, and trailing JSON values. A
scan fails closed on every unsupported entry. It does not skip corrupt facts.

The store installs a receipt without replacement. It writes and syncs a
sibling temporary file. It then uses an atomic no-replace publish and syncs the
receipt root. Temporary names use
`.tmp-<canonical-run-id>-<canonical-uuidv7>`. A startup scan classifies every
temporary entry before normal receipts. It removes and syncs only a regular
temporary file whose strict bytes equal the expected canonical receipt and
whose canonical receipt already exists with the same bytes. Any other
temporary entry returns `repair-required` and remains on disk.

An exact existing receipt is a converged retry only after the
store syncs the root. If publish or root sync returns an error after a possible
effect, the store reloads the exact path. It proceeds only when the exact bytes
exist and a root sync succeeds. Otherwise it returns an ambiguity error and
keeps all evidence.

A bootstrap command runs inside the exact execution target. It sends a typed
start acknowledgement through the authenticated coordinator control channel.
The acknowledgement contains the exact receipt value. The coordinator checks
the intent and run record again, installs the receipt, and returns an explicit
success response. The bootstrap starts the handler only after that response.
It stops if the request, receipt write, or response fails.

For a remote run, the bootstrap runs inside the exact worker session and
window. SSH transport startup alone is not a receipt. The worker sends the
acknowledgement after it enters the bound target. For a local shared or
independent run, the bootstrap uses the same request from the bound local
window. All substrates use one receipt contract.

Use this order:

1. Bind the execution location and launch target in the run record.
2. Advance the dispatch intent to `handoff_durable`.
3. Create the exact session or window.
4. Run the bootstrap inside that target.
5. Install and sync the exact start receipt.
6. Start the handler after the receipt is durable.
7. Keep the receipt through terminal queue application.
8. Remove and sync the exact receipt.
9. Remove the dispatch intent.

An exact retry reloads the intent, run record, receipt, and target. It does not
mint a new session or window name. A live target without a receipt can mean the
bootstrap is between target creation and receipt durability. Startup waits for
one bounded bootstrap interval and reloads all facts. If the receipt stays
absent, startup returns `repair-required`. It does not start a second target.

After terminal queue durability, cleanup removes the exact receipt and syncs
the receipt root. It then removes the dispatch intent. An ambiguous remove
reloads the exact path and syncs the root. An absent exact path is converged
only after that sync succeeds. Startup must fail closed while receipt cleanup
is unresolved.

The session classifier uses these results:

| Receipt fact | Exact target fact | Result |
| --- | --- | --- |
| absent | absent | `replay-session-start` |
| exact | live | `adopt-live` |
| exact | absent or dead | `resume-dead` |
| absent | live | wait for a bounded bootstrap interval, then `repair-required` |
| absent | dead | `remove-dead-unstarted-target`, reload, then `replay-session-start` |
| conflict | any | `repair-required` |
| any | conflict | `repair-required` |

`remove-dead-unstarted-target` applies only to an exact target whose four
identity options match and whose pane reports dead. The executor removes that
exact session or window through its owning adapter. It then reloads all facts.
It does not write a receipt or start a handler in the same step. An error or an
unreadable post-remove probe returns `repair-required`. An absent post-remove
target lets the next decision return `replay-session-start`.

Session existence alone is not run liveness for a shared or remote session.
Those paths must bind and inspect the run-specific window target.

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
