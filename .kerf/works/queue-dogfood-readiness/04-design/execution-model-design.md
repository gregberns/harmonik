# Change Design — Execution model

## Current state

`EM-016`, `EM-023a`, and `EM-024` make the run-branch checkpoint the durable
authority. `EM-052` requires merge, push, then close on normal success.
`EM-053` reopens on release failure. `EM-031b` identifies an unfinished
release by a branch tip ahead of its dispatch head, but it does not retain the
merge target or remote worker endpoint needed to finish that release after a
restart.

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

Add a typed `ReleaseClaim` to the immutable transition record of the final
pre-release checkpoint. The claim has these values:

- `dispatch_head_sha` — the task-branch head recorded at dispatch.
- `merge_target_ref` and `merge_target_sha` — the concrete target selected for
  this release and the SHA it resolved to when the claim was written.
- `remote_endpoint` — absent for local work, or the worker name, host, and
  repository path that the release synchronization must use.

Write the claim before any synchronize, merge, close, or reopen operation.
The checkpoint commit makes the claim immutable. Recovery reads the claim from
Git and reads the current Bead state before it decides whether to finish the
release, preserve the branch for reconciliation, or reject a duplicate
terminal action. It never consults JSONL or a daemon-local registry as a
release-state source. A missing or invalid claim is a safe reconstruction
failure: retain the branch and route to reconciliation without merge, close,
or redispatch.

Amend `EM-031b` to require this claim and make restart recovery depend on it.
The release-claim writer is a separate prerequisite task between graceful
drain and restart reconstruction.

## Rationale

A committed worktree is durable work. Shutdown cannot turn it into an ordinary
DOT failure. The claim turns the release inputs into immutable Git evidence,
so recovery has one reliable authority without reviving daemon-local state.

## Requirements traceability

- `02-components.md`: shutdown drains committed work or retains reviewable
  recovery state.
- `03-research/execution-model/findings.md`: checkpoint authority,
  merge-before-close, and restart reconstruction.
- Existing requirements: `EM-016`, `EM-023a`, `EM-024`, `EM-025a`, `EM-031`,
  `EM-031a`, `EM-031b`, `EM-052`, `EM-053`, and `EM-053a`.
- Depends on the run-state-machine shutdown edge and operator drain ordering.
