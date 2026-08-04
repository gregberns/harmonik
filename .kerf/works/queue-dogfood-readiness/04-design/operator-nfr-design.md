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

---

## Folded in from lane bravo's parallel pass (2026-08-03)

Lane bravo ran the same pass on branch `work/queue-dogfood-readiness` before any
lane contract named that branch. The two passes reached the same shape. Bravo's
text is kept below because it names evidence, measurements and review records
this document does not. The plan of record stays T1..T12 plus T5a in
`07-tasks.md`. Where the two disagree on behaviour, the section above wins.

### Operator NFR change design

#### Current state

Ordered drain stops new dispatch and releases resources after checkpointing.
It does not name the post-commit interval or its timeout result.

#### Target state

Add committed-before-merge as a special drain outcome. Its per-step timeout
either completes the terminal ladder or leaves the durable recovery record.
Resource release follows the selected owner. The controlled proof records load,
timeouts, stop point, and retained artifacts. New storage remains N-1 readable.

#### Rationale

A global short wait followed by cancellation cannot be an operator-safe drain.

#### Requirements traceability

Addresses ordered-drain and controlled-load requirements.
