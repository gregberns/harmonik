# Research — Operator NFR

## Questions

1. Does graceful shutdown prevent close or redispatch before release is known?
2. Where does a committed but unmerged DOT run fit in the drain steps?
3. Can restart reconstruct an interrupted drain?
4. Does the spec require controlled-load evidence for daemon tests?
5. Can the command watchdog bypass the drain procedure?

## Findings

- `ON-008` requires pause and upgrade to wait for in-flight runs and the full
  drain sequence. `ON-027` orders queue stop, run checkpoint, handler exit,
  ledger intent drain, event flush, cleanup, then exit or pause.
- `ON-030` reconstructs from git and Beads. `ON-027a` persists each global
  drain step and resumes after a crash.
- The corrected DOT path supplies a per-run result that the current drain rule
  does not state: merge then close, or reopen for review.
- `ON-032` bounds the RTO fixture. It does not require host load, test
  concurrency, or a contention classification for daemon-suite evidence.
- `cmd/harmonik/run.go` has a five-second signal watchdog that calls
  `os.Exit(1)` if `daemon.Start` does not return. It can cut across the
  configured drain bounds on that command path.

## Patterns to keep

- `ON-013` puts a drain summary in the paused event.
- `ON-027a` is the pattern for durable, sequential drain progress.
- `ON-INV-006` rejects control surfaces that bypass graceful drain, except an
  explicit immediate stop.

## Risks and decisions

- Require a committed DOT run to merge or reopen before drain step 2 completes.
- Add a readiness-evidence record for host load and allowed daemon-suite
  concurrency. Scope it to readiness, not normal RTO measurement.
- Decide whether the five-second watchdog is an emergency immediate stop or
  must wait for the drain boundary. Its current role is ambiguous.
