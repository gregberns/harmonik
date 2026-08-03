# Change Design — Operator NFR

## Current state

`ON-008` blocks pause completion until full drain. `ON-027` and `ON-027a`
define ordered, durable drain steps. `ON-030` reconstructs from git and Beads.
`ON-032` does not record host load or daemon-suite concurrency. The normal
command path has a fixed five-second signal watchdog that can exit before
graceful drain finishes.

## Target state

Amend `ON-027` step 2 and its completion condition. Each committed DOT run
must resolve its tip, synchronize a remote branch, and reach merge-and-close or
reopen before that step completes. The drain marker records that terminal
release result. A crash resumes by checking git and Beads again.

Amend `ON-030` to reconstruct unfinished committed release from the run branch
and bead state. JSONL remains diagnostic evidence only.

Amend `ON-032` for readiness evidence. Record host load and allowed daemon
suite concurrency. Require one daemon suite at a time. A result from a broken
load rule is machine-contention evidence until a controlled rerun classifies it.

Amend operator control. SIGINT and SIGTERM start graceful drain. A normal
watchdog can report elapsed time and drain state but cannot exit first. Only an
explicit immediate stop or SIGKILL bypasses the drain. The command uses the
configured drain-timeout escalation path instead of a fixed five-second exit.

## Rationale

The watchdog is a control surface. Its forced exit can bypass the graceful
drain invariant. Controlled test conditions distinguish a product defect from
an overloaded development host.

## Requirements traceability

- `02-components.md`: shutdown order and controlled-load evidence.
- `03-research/operator-nfr/findings.md`: `ON-008`, `ON-027`, `ON-027a`,
  `ON-030`, `ON-032`, and the watchdog finding.
- `ON-INV-006`: normal control surfaces do not bypass graceful drain.
- Session decisions: merge-or-reopen before drain completes and no silent
  watchdog bypass.
