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

---

## Folded in from lane bravo's parallel pass (2026-08-03)

Lane bravo ran the same pass on branch `work/queue-dogfood-readiness` before any
lane contract named that branch. The two passes reached the same shape. Bravo's
text is kept below because it names evidence, measurements and review records
this document does not. The plan of record stays T1..T12 plus T5a in
`07-tasks.md`. Where the two disagree on behaviour, the section above wins.

### Operator NFR research findings

#### Questions

1. Does ordered drain cover work committed before merge?
2. Which timeout and operator result apply to it?
3. What controlled-load evidence is required before the first batch?

#### Findings

`ON-008` requires pause and upgrade to wait for all in-flight runs and drain
steps. `ON-027` stops queue advance, reaches a checkpoint, drains handlers and
events, unlocks workspaces, then exits or pauses. `ON-029` makes drain
timeouts per-step configurable. `ON-018` requires N-1 readable durable
artifacts.

`PL-011` consumes this ordering. `exitClean` in
`internal/daemon/scheduler.go` instead has one fixed ten-second wait and then
cancels active queues. That path can turn a slow terminal ladder into
cancellation without a durable recovery outcome.

#### Patterns and risks

ON owns cross-subsystem order, timeout policy, and the operator meaning. A
commit before merge is a stricter safe point than a checkpoint. Unlocking a
workspace after only a checkpoint conflicts with terminal recovery.

#### Design constraints

- Define committed-before-merge as a special drain outcome.
- Its timeout must leave an inspectable recovery record, not ambiguous
  cancellation.
- Stop dispatch, resolve durable intent work, flush observations, and release
  resources only when the recovery owner permits it.
- Controlled-load evidence records load, timeout values, stop point, and
  retained artifacts. Use additive, N-1 compatible storage where possible.
