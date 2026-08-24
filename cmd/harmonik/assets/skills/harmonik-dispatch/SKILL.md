---
name: harmonik-dispatch
description: >
  The daily dispatch loop: route substantive work through the persistent
  daemon's queue rather than spawning sub-agents. Owns the ranking procedure,
  the subscribe command, and stream-vs-wave.
---

<!-- Generated from cmd/harmonik/assets/skills/harmonik-dispatch/SKILL.md
     Edit there and mirror in the same commit; scripts/skill-mirror-check.sh fails on drift. -->

# Harmonik dispatch — the daily loop

The model is **one persistent daemon per project plus a shared queue**. The
daemon is the dispatcher; agents dispatch by submitting beads to its queue. Many
agents share that one daemon, and the shared queue IS the coordination mechanism.

The working phase opens by deciding what to work on and then proposing a
`harmonik queue submit` batch — before any Agent-tool sub-agent.

## Start the daemon once

`harmonik queue status` exiting 17 means no daemon. Start exactly one, queue-only,
in a detached tmux session:

```bash
tmux new-session -d -s harmonik-daemon \
  'harmonik start daemon --project $HARMONIK_PROJECT --no-auto-pull --max-concurrent N'
```

- `start daemon` is the only spelling that starts a daemon. A bare `harmonik`, a
  flag-first argv, and an unknown verb all print help and exit 2. They start
  nothing and write no files.
- `--no-auto-pull` makes it queue-only: it dispatches what arrives on the queue
  and never auto-drains `br ready`. That is the safe default — the alternative
  once burned a month of credit overnight.
- `--max-concurrent N` is the ceiling for the whole daemon. Sized to the box:
  wider than the core count oversubscribes and exhausts disk.
- Never start a second daemon. It collides on the pidfile lock and exits 5.

## The loop

1. **Triage — priority comes from stated intent first, then from the ledger.**

   Work the named initiatives of the operator and the admiral first. They live in
   the active plan's order, in the dated directives in
   `.harmonik/context/captain-lanes.md`, and in the direction-log return path.

   Below that line, order the unclaimed backlog with `br ready --sort priority
   --limit 0`, scoped to a lane with `--parent <epic_id>`, and use `--sort oldest`
   to surface work that is starving. `--limit 0` is not optional: `br ready`
   returns 20 rows by default, so a short listing is not evidence of a short
   backlog.

   **`kerf` plans work; it does not rank work.** Use `kerf map` to see which work
   owns a bead and what context it carries, and `kerf triage` for drift. Do not
   take an order from `kerf next` — its score comes from graph structure and never
   reads the `br` priority field, so a P0 and a P3 come back the same, and it
   reports empty for a work with no `bead_filter`.

2. **Pick a batch** from the top of that ordering.

3. **If your session is keeper-managed**, run `harmonik keeper set-dispatching
   <agent>` before submitting so the keeper does not `/clear` you mid-dispatch.

4. **Submit.** `harmonik queue submit --beads id1,id2,id3` (or a hand-authored
   `QueueSubmitRequest` file). It does not block; it returns the minted
   `queue_id`. The daemon then spawns a claude per bead, commits, merges into the
   target branch one at a time, pushes, and auto-skips any bead whose merge
   conflicts. Review-loop is on by default.

   **The target branch comes from `.harmonik/branching.yaml` key
   `defaults.lands_on`.** When that file is absent the daemon resolves the target
   to `main`, so check it before you dispatch.

5. **Arm a Monitor.** Submitting returns only the `queue_id`; without a Monitor
   you are blind from submit to completion.

   ```bash
   harmonik subscribe --types run_completed,run_failed,run_stale,queue_paused,heartbeat \
                      --heartbeat 60s --json
   ```

   It attaches to the running daemon, so one Monitor sees every bead whichever
   agent submitted it. Re-arm it if it hits the Monitor timeout.

   **Keep `queue_paused` in the type list.** It is the one event that says the
   QUEUE stopped rather than one bead. A queue that stops emits it once and then
   goes silent, so a watcher without it reads a stopped queue as a quiet one.
   See § Restart a queue that stopped.

6. **Stay active while the daemon works.** Append the next batch with `harmonik
   queue append [--queue-id <uuid>] <group-index> <bead-id ...>`, drain untriaged
   items, file follow-up beads, review recently-merged commits.

7. **On completion**, read outcomes off the subscribe stream or
   `.harmonik/events/events.jsonl`, confirm the commits landed in `git log`, get
   a review on anything load-bearing, and submit the next batch.

8. **When everything drains**, `harmonik keeper clear-dispatching <agent>`.

### Rebuild before you dispatch

Run `go install ./cmd/harmonik` before each batch. A stale binary is the top
cause of "but I fixed that" — the daemon keeps running the code you built last
time, so a landed fix looks like it never happened.

### Pre-screen for already-landed beads

A bead can be stale-open: the work landed but nobody closed it. Dispatching it
wastes a slot. Grep history first and drop any bead already in it:

```bash
git -C $HARMONIK_PROJECT log --all --grep "Refs: <id>" --oneline
# any hit → br close <id> --reason "Subsumed: landed as <sha>"
```

### Stream vs wave

Use `kind: "stream"` for the daily loop. A stream accepts mid-flight appends and
scans items in stored order, and a blocked or dispatched item does not stop a
later dependency-ready item from taking an open slot — so one stream runs a graph
such as `A -> [B, C, D] -> E` with no supervisor advancing it.

**A stream does not serialise, so do not reach for `--wave` to get concurrency.**
A stream runs its items concurrently whenever `max_concurrent` is above 1:
`streamEligible` in `internal/queue/state.go` skips already-dispatched items
rather than treating them as head-of-line blockers. Pick `--wave` only when you
want the group closed at submit, because a wave takes no append. Earlier guidance
to prefer `--wave` for concurrency was wrong;
`specs/execution-model.md` EM-NOTE-STREAM-CONCURRENCY supersedes it.

Submit and append both wake an idle daemon.

## `harmonik run` is the legacy / solo-bootstrap path

With a daemon already up, `harmonik run --beads ...` submits to that daemon's
queue as a stream group and blocks until terminal. With no daemon running it
*becomes* an inline daemon for the life of its beads. Use it only to bootstrap a
one-shot solo batch. For ongoing work, run the persistent daemon and submit.

## When NOT to route through the daemon

A sub-agent takes a commit-producing bead only when:

- you are fixing harmonik itself in code that breaks dispatch;
- the change is two lines or fewer of typo or cross-reference cleanup;
- the work touches an untested workload class.

Anything else goes through the queue. **The smell is a sub-agent whose output is
a commit.** Research, review, triage and monitoring agents produce findings and
no commit — a wave of those is not a routing failure, because none of them is a
bead. Three implementation-shaped sub-agents in a row is the signal to stop:
write them as beads and submit the batch.

## Do not run the daemon and a sub-agent wave at once (HARD RULE)

**Do not have the daemon dispatching beads AND ten or more parallel Agent-tool
sub-agents on the same account at the same time.** The daemon's claude processes
get queued behind your sub-agents by the API rate-limiter. Observed: about forty
parallel sub-agents while beads were in flight produced a 56-minute stall between
`run_started` and the handler coming up, with no error surfaced anywhere.

Pick one mode per phase. In a **daemon phase**, keep sub-agents to a handful and
use them for monitoring, triage and review. In a **sub-agent phase** — research,
parallel investigation, a major-issue fan-out — stop submitting and let the
in-flight beads drain first. If you must interleave, keep the total number of
concurrent claude sessions across both modes small.

**A fan-out is a sub-agent phase, not an exception to this rule.** See the
**major-issue-fanout** skill.

## Failure handling

**A `run_failed` usually stops the whole queue, not one bead.** The group lands at
`complete-with-failures` when every item in it is terminal and at least one
failed. The queue then goes to `paused-by-failure`. Later groups stay pending and
nothing else dispatches until someone recovers it. So read the first `run_failed`
as "my lane is about to stop", not "one bead needs a retry".

The stream says so once, and then goes quiet. `advanceActive`, reached through
`AdvanceGroup` in `internal/queue/state.go`, builds a `queue_group_completed`
intent and then a `queue_paused` intent. `decideFailedGroupCompletion` in
`internal/queue/group_completion_decision.go` applies the `active` →
`paused-by-failure` transition. The daemon persists first and then emits both, in
`internal/daemon/group_completion_shell.go`. The pause cause sits at
`.payload.reason` in the envelope, not at the top level, so filter on
`.payload.reason == "group_failure"`.

Read the failure class from `events.jsonl` (`no_commit`, `context_cancelled`, and
so on), then classify the bead.

- **Transient** (network, lock contention) — re-submit the single bead.
- **A genuine bug in the bead's work** — fix-up sub-agent on the worktree branch.
- **A bug in harmonik itself** — sub-agent this one bead, and file a bug bead.
- **The same bead failed twice this session** — stop. Dispatch an investigator
  before any further re-dispatch, and never a third attempt without one.

### Restart a queue that stopped

**This section owns the explanation.** Other documents give the instruction and
point here. Put a new fact about queue recovery here, not there.

`harmonik queue list --json` prints a `status` for every named queue.
`paused-by-failure` is the stopped one.

```bash
harmonik queue recover --queue <name>     # a bare name also works
```

`recover` re-arms every item whose status is `failed`, flips the group that
reached `complete-with-failures` back to `active`, sets the queue back to
`active`, and asks the dispatcher to wake. An item held at
`deferred-for-ledger-dep` stays held until its blocker completes. **Success does
not mean anything has dispatched** — what `RecoverFailed` in
`internal/queuewiring/recovery.go` promises is that the queue change and its
receipt are durable. Read `queue list` again to confirm the queue moved.

**`harmonik queue resume` cannot do this.** Resume clears an operator drain pause
only. A resume aimed at a failure-parked queue is refused, and the refusal names
the verb you want: "resume releases a drain pause only; run `harmonik queue
recover <name>` to re-arm the failed items" (`ResumeRefusedError` in
`internal/queue`). So the wrong verb teaches you the right one — you do not have
to carry this rule in your head.

**Leave the failed bead open.** Recovery reads the ledger for every failed item
before it touches the queue, and refuses with `recovery_bead_not_open` (`-32033`)
when any of those beads is not `open`. A tidy-up `br close` therefore locks the
queue shut.

**Submitting around a stopped queue is not a fix, and nothing will stop you.**
A submit under a new name always succeeds. So does a submit under the stopped
queue's OWN name: `Validate` in `internal/queue/validation.go` excludes
`paused-by-failure` from the `queue_already_active` refusal on purpose, and
`TestValidate_ResubmitToPausedByFailureName` pins that. Either way you get a
fresh queue and no error, the failed items are never re-armed, and the beads that
stopped the lane stay unworked with nobody watching them. `recover` is the verb
that re-arms them.
