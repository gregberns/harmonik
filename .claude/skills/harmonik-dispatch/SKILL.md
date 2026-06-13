---
name: harmonik-dispatch
description: >
  Canonical "main-agent's daily loop" for the harmonik project. Routes ≥75% of
  substantive work through the persistent daemon's queue (`harmonik queue submit`
  / `append` / `subscribe`) rather than spawning Agent-tool sub-agents. Loads on
  session-resume; gates dispatch decisions.
  Authoritative: AGENTS.md §"Daily loop (canonical)" + §"Submitting work" +
  docs/orchestration-protocol-v2.md.
---

# Harmonik dispatch — the daily loop

The dispatch model is **one persistent daemon per project + a shared queue**. The daemon (`harmonik --project . --no-auto-pull --max-concurrent N`, running in a detached tmux session) is the dispatcher; agents dispatch by **submitting beads to its queue**. Multiple agents/orchestrators share that single daemon — the shared queue IS the multi-agent coordination mechanism.

When working in this project (`$HARMONIK_PROJECT`), the FIRST tool call of the working phase should be `kerf next` (ranked bead feed with work-context), then a proposed `harmonik queue submit` dispatch batch — BEFORE any Agent-tool sub-agent invocation.

## Start the daemon once (if not already up)

`harmonik queue status` → exit 17 means no daemon. Start exactly one, queue-only, in a detached tmux session:

```bash
tmux new-session -d -s harmonik-daemon \
  'harmonik --project $HARMONIK_PROJECT --no-auto-pull --max-concurrent N'
```

- `--no-auto-pull` = **queue-only**: the daemon dispatches only work that arrives via the queue; it will NOT auto-drain `br ready` (safe default after the 2026-05-30 credit-burn incident).
- `--max-concurrent N` is the concurrent-dispatch ceiling for the whole daemon (~4–5 wide on a 10-core box — wider oversubscribes cores and exhausts disk).
- If a daemon is already up, `harmonik queue status` returns the live queue; do NOT start a second one — it collides on the pidfile lock and exits code 5.

## The loop

1. **Triage.** `kerf next` — ranked feed of beads with work-context. Use `kerf triage` for drift detection (untriaged beads, external changes).
2. **Pick a batch of beads** from the top of the feed (skip the untested-workload classes documented in `HANDOFF.md` until the probes land). The previously-flagged caveats (hk-rp48p priority-sort, hk-wx8z8 parallel pane allocator, hk-cj0gm Stop-hook delivery) are all FIXED; broad-class dispatch is now safe.
3. **If the orchestrator session is keeper-managed:** signal in-flight dispatch before submitting:
   ```bash
   harmonik keeper set-dispatching <agent>
   ```
   This writes `.harmonik/keeper/<agent>.dispatching` so `HoldingDispatch` returns true and the
   keeper cycle defers any handoff action while queue work is in flight (hk-rc51s).
4. **Submit to the running daemon's queue.** `harmonik queue submit --beads id1,id2,id3` (or `harmonik queue submit /tmp/batch.json` for a hand-authored `QueueSubmitRequest`). This does NOT block — it returns the daemon-minted `queue_id`. The daemon spawns claude per bead, watches for completion, commits, merges to main **one-at-a-time**, pushes, and **auto-skips** any bead whose merge conflicts. Review-loop is on by default.
5. **Arm a Monitor.** Submitting returns only the `queue_id`; without a Monitor you are blind from submit to group-completion. Run `harmonik subscribe --types run_completed,run_failed,run_stale,heartbeat --heartbeat 60s --json` in a Monitor call (it attaches to the running daemon, so one Monitor sees every bead regardless of which agent submitted it).
6. **Stay active while the daemon works.** Append the next batch (`harmonik queue append [--queue-id <uuid>] <group-index> <bead-id ...>` on a stream group); drain `kerf triage` untriaged items; file follow-up beads observed from prior runs; review recently-merged commits per the per-commit-reviewer gate.
7. **On group completion.** Inspect outcomes via the subscribe stream / `.harmonik/events/events.jsonl`; `git -C $HARMONIK_PROJECT log --oneline -N` for landed commits. Run reviewer on any load-bearing commit, then submit/append the next batch.
8. **When all in-flight work drains** (no more `pending` or `in_progress` beads in the group):
   ```bash
   harmonik keeper clear-dispatching <agent>
   ```
   Removes the `.dispatching` marker; the keeper cycle resumes normal threshold checks.

### Stream vs wave

Use `kind: "stream"` groups for the daily loop — they accept mid-flight appends and dispatch in order (head-of-line blocking). Use `kind: "wave"` only when you need true concurrent dispatch of a fixed, immutable set up to `--max-concurrent`; waves do not accept appends. Remaining gap: hk-24xn1 — the daemon doesn't wake on submit/append when idle, so newly-added beads sit `pending` until the next workloop tick.

## `harmonik run` is the legacy / solo-bootstrap path

`harmonik run --beads ...` is NOT the canonical dispatcher. Its current behavior (hk-b3wqd):

- **If a daemon is already up** (detected via `daemon.sock`): `harmonik run` **submits its beads to that daemon's queue** as a stream group and blocks until they reach a terminal state — it does NOT collide on the pidfile lock, and exit 5 is NOT returned.
- **If no daemon is running:** `harmonik run` *becomes* the inline daemon for the duration of its beads, then exits when they finish. Use this ONLY to bootstrap a one-shot solo batch when you don't want a persistent daemon.

For all ongoing multi-agent work, run the persistent daemon and submit to its queue — don't reach for `harmonik run` as the default dispatch verb.

## When to NOT route through the daemon (exceptions)

Sub-agent dispatch (via the Agent tool) is justified ONLY when:

- **(a)** You're fixing harmonik itself in code that breaks dispatch (e.g. hk-wx8z8 itself).
- **(b)** The change is ≤2 lines of typo / cross-reference cleanup where ~30s daemon overhead isn't worth it.
- **(c)** The work touches an untested workload class per the readiness audit.

Anything else: route through the daemon queue. If you're on the 4th Agent-tool call in a row, STOP and batch them onto the queue.

## API rate-limit concurrency rule (HARD RULE — hk-kumjl / hk-ocbh2)

**Do NOT run the daemon dispatching beads AND ≥10 parallel Agent-tool sub-agents at the same time on the same Anthropic account.**

Observed failure mode: orchestrator dispatched ~40 parallel sub-agents while the daemon had beads in flight. The daemon-launched claude processes were queued behind the sub-agents by the Claude API rate-limiter. `run_started` fired at 09:24; `handler_capabilities` did not arrive until 10:20 — a **56-minute stall** with no error surfaced.

**Rule:** Pick one mode per work phase:
- **Daemon phase** — beads in flight on the queue; ≤3 Agent-tool sub-agents concurrently (monitoring, triage, review).
- **Sub-agent phase** — heavy Agent-tool dispatch (research, parallel investigation); hold off on new queue submissions until the sub-agent wave drains.

If you must interleave, cap total concurrent claude sessions (daemon-dispatched + sub-agents) to **≤5** across both modes to stay safely within the rate limit.

## Failure handling

A `run_failed` event on the subscribe stream → read the failure class from `.harmonik/events/events.jsonl` (`no_commit`, `context_cancelled`, etc.), then classify the failing bead:
- **Flake / transient** (network, lock contention) → re-submit the single bead (`harmonik queue submit --beads <id>`, or append it to the live stream group).
- **Genuine bug in the bead's work** → fix-up sub-agent on the worktree branch.
- **Bug in harmonik itself** → fall back to sub-agent dispatch for THIS bead AND file an `hk-...` bug bead.
- **Same bead failed twice this session** → STOP; dispatch an investigator sub-agent before any further re-dispatch. Never dispatch the same bead more than twice without investigation.

Document classification in the post-mortem.

## 75% criterion

Each session ends with a tally: substantive commits this session, of which N landed via the daemon queue (committer identity / `Refs:` trailer in `git log`). Target: N/total ≥ 0.75. Trivial typos and hygiene-only commits don't count. Sessions that miss the target log a one-line reason in `/session-handoff`.

## References

- `AGENTS.md` §"Daily loop (canonical)" + §"Submitting work" — the canonical project rule.
- `HANDOFF.md` — the current orchestration directive.
- `docs/orchestration-protocol-v2.md` — full design with rationale and exact text deltas.
- `specs/queue-model.md` — the normative wave/stream/append contract.
- `docs/kerf-feedback/2026-05-19-phase2-readiness-audit.md` — what's still untested.
