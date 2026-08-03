# Scratch daemon runbook research findings

## Questions

1. How does a scratch clone run the named candidate?
2. Which proof files must survive the run?
3. How does the procedure enforce the one-item canary?
4. When may it write to the fleet ledger?

## Findings

`docs/scratch-daemon-runbook.md` and `scripts/scratch-daemon.sh` provide a
general isolated harness. `init` has no branch or commit option. `build` uses
the scratch checkout's current HEAD. `up` defaults to concurrency one, DOT,
and no eager refill. `batch` accepts any non-empty bead list or queue file.
It retains a batch JSON result, but deletes its temporary subscribe stream.

The harness guards the fleet path and supervised projects, verifies teardown,
and redirects scratch pushes to a private bare repository. It has no
controlled-load, Step 9, or core-loop procedure. `feedback` deliberately
writes to the fleet ledger.

## Patterns and risks

The harness is safely isolated but deliberately general. Its remote and wave
examples are valid outside this gate. A PASS could test the wrong revision or
lack the event order needed for later review.

## Design constraints

- Fetch, check out, verify, and record the full candidate commit before build
  and assessment.
- Retain an evidence manifest with candidate, effective configuration,
  controlled-load result, Step 9, core loop, daemon log, event trace, batch
  JSON, and assessor report.
- Preserve the event trace before cleanup.
- Define controlled-load acceptance before the daemon suite.
- Add a restricted readiness section that uses one-item submission and rejects
  wave, remote, Pi, and cross-repository input. Keep general examples.
- Do not use `feedback` on a passing run. File a confirmed finding only as an
  explicit evidence-to-ledger action after the proof.
