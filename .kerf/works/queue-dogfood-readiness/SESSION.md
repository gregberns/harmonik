# Queue dogfood readiness — Session

## Current pass

The work is shelved at **Ready**. `kerf square` passed with all expected
artifacts. The work is ready for finalization and implementation. The fleet
daemon was not started.

## Decisions made

- Failed-item recovery becomes a durable queue transaction. It is distinct from
  drain resume and returns a durable receipt.
- A committed but unmerged run gets one durable recovery record and one
  terminal action. It retains its original worktree until recovery completes.
- A readiness gate is assessor-only. Schema version 3 pins its candidate,
  canary profile, decision owner, and proof artifacts.
- The first canary remains one repeat-safe local stream item at concurrency one.
  It excludes append, remote, Pi, cross-repository, and wave work.

## Ledger triage

The four historical finding IDs are absent from this machine ledger. No closure
was made. Current source has their named regression tests. See
`ledger-triage.md` for the evidence boundary.

## Suggested next steps

1. Review and finalize the ready work before implementation.
2. Implement `07-tasks.md` in dependency order. Alpha owns daemon and workspace
   work. Bravo owns queue, CLI, schema/runbook, and triage.
3. Do not alter registry or mission state without operator authority.

## Reading order

1. `07-tasks.md`
2. `05-changelog.md`
3. `05-spec-drafts/`
4. `ledger-triage.md`
