# Research — Scratch daemon runbook

## Questions

1. Does scratch execution isolate the fleet daemon and push target?
2. What result file does a batch create?
3. What can write to the fleet ledger?
4. Does the command surface enforce the narrow canary?

## Findings

- The runbook safety guarantee uses path, PID, tmux, supervision, and origin
  checks to keep `init`, `build`, `up`, `batch`, and `down` in the named scratch
  clone.
- `batch` writes `<scratch>/.harmonik/batch-<name>-<queue_id>.json`. The file
  is the authoritative input to `feedback`.
- `feedback` is the one deliberate fleet write. It must not run for readiness
  evidence unless the operator authorizes fleet findings.
- The script defaults to `SCRATCH_MAX_CONCURRENT=1` and DOT workflow mode.
- The generic runbook permits remote work, waves, and feedback. It does not
  enforce the first-canary limits.

## Patterns to keep

- `scripts/scratch-daemon-smoke.sh` proves offline batch folding and feedback
  deduplication. It does not prove a live agent run.
- Save or copy artifacts before scratch cleanup. Batch files live under the
  scratch clone and can disappear during later cleanup.

## Risks and decisions

- The mission and retained proof record must state and verify one local,
  repeat-safe, non-Pi, non-remote, non-cross-repository stream item.
- The canary must not call `feedback` without separate operator authority.

---

## Folded in from lane bravo's parallel pass (2026-08-03)

Lane bravo ran the same pass on branch `work/queue-dogfood-readiness` before any
lane contract named that branch. The two passes reached the same shape. Bravo's
text is kept below because it names evidence, measurements and review records
this document does not. The plan of record stays T1..T12 plus T5a in
`07-tasks.md`. Where the two disagree on behaviour, the section above wins.

### Scratch daemon runbook research findings

#### Questions

1. How does a scratch clone run the named candidate?
2. Which proof files must survive the run?
3. How does the procedure enforce the one-item canary?
4. When may it write to the fleet ledger?

#### Findings

`docs/scratch-daemon-runbook.md` and `scripts/scratch-daemon.sh` provide a
general isolated harness. `init` has no branch or commit option. `build` uses
the scratch checkout's current HEAD. `up` defaults to concurrency one, DOT,
and no eager refill. `batch` accepts any non-empty bead list or queue file.
It retains a batch JSON result, but deletes its temporary subscribe stream.

The harness guards the fleet path and supervised projects, verifies teardown,
and redirects scratch pushes to a private bare repository. It has no
controlled-load, Step 9, or core-loop procedure. `feedback` deliberately
writes to the fleet ledger.

#### Patterns and risks

The harness is safely isolated but deliberately general. Its remote and wave
examples are valid outside this gate. A PASS could test the wrong revision or
lack the event order needed for later review.

#### Design constraints

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
