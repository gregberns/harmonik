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
