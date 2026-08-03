# Change Design — Execution model

## Current state

`EM-016`, `EM-023a`, and `EM-024` make the run-branch checkpoint the durable
authority. `EM-052` requires merge, push, then close on normal success.
`EM-053` reopens on release failure. No rule defines a committed DOT run that
reaches shutdown before normal release.

## Target state

Amend `EM-052` with a shutdown-drain clause. After DOT returns during shutdown,
the daemon resolves the worktree tip with a live context. A tip ahead of the
dispatch head is committed but not released. The daemon synchronizes a remote
branch before every drain merge, including no-change. It then has one result:

- A synchronized merge or synchronized no-change closes the bead.
- Missing tip, sync failure, merge failure, or close failure reopens the bead.

The daemon must not redispatch or close before that result. It preserves the
run branch and checkpoint trail on reopen.

Add the shutdown-window regression proof. It stops the daemon after a DOT
handler commits and before merge, then proves merge before close and no reopen.
The run-state-machine design adds the remote synchronization fixture.

Amend `EM-031` and `EM-031a`. A non-terminal bead plus a run branch ahead of
its dispatch head is evidence of unfinished release. Restart reconstructs that
state from git and Beads. It does not infer failure from JSONL.

## Rationale

A committed worktree is durable work. Shutdown cannot turn it into an ordinary
DOT failure. Merge-or-reopen retains the existing release boundary and gives
restart one reliable authority.

## Requirements traceability

- `02-components.md`: shutdown drains committed work or retains reviewable
  recovery state.
- `03-research/execution-model/findings.md`: checkpoint authority,
  merge-before-close, and restart reconstruction.
- Existing requirements: `EM-016`, `EM-023a`, `EM-024`, `EM-025a`, `EM-031`,
  `EM-031a`, `EM-052`, and `EM-053`.
- Depends on the run-state-machine shutdown edge and operator drain ordering.
