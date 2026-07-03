# Stall-Sentinel — Analysis (detection algo)

> Formalizes brief §2 (two-layer algo) and §1 (signal source). Authoritative source =
> `plans/2026-07-02-stall-sentinel/DESIGN.md`. Do not re-derive.

## Signal source — `events.jsonl`, NOT tmux (load-bearing, §1)

The single most important constraint, learned the hard way: **tmux pane/window presence and
worktree-dir mtime are UNRELIABLE liveness signals**. A correctly-idle pane is indistinguishable
from a hung one, and the review phase doesn't write the worktree so mtime goes stale on healthy runs.

The **authoritative** signal is the typed event stream `.harmonik/events/events.jsonl`:

| Event | Role in detection |
|---|---|
| `run_started`, `run_completed`, `run_failed` | run lifecycle + terminal transitions |
| `agent_heartbeat` / `agent_message` | mid-run liveness — silence = candidate hang |
| `implementer_phase_complete`, `reviewer_verdict` | phase transitions |
| `agent_presence`, `governor_signal` | daemon-alive baseline (distinguishes "daemon dead" from "run hung") |
| `bead_closed`, commits on `main` | forward-progress markers |

tmux `capture-pane` is allowed ONLY as a **secondary confirm** on an already-suspect run (e.g. to see
an idle prompt / `/quit` / frozen spinner), never as the primary trigger.

## The detection algo — two layers

### Layer A — per-run stall (high-precision, easy)

For each run with `run_started` and no terminal event (`run_completed` / `run_failed`):

- **heartbeat-gap:** no `agent_heartbeat` / `agent_message` for > `run_silence_stall` min.
  → the class-2 signature (silent run hang).
- **review-stall:** a `reviewer_verdict` fired but NO `run_completed` / `run_failed` within
  `review_finalize_stall` min. → the EXACT class-3 signature (review-loop wedge).
- **run-age:** any run `dispatched` > `run_max_age` min with no terminal event. → backstop for
  novel hangs neither of the above catches.

Each Layer-A hit emits `stall_detected` carrying `run_id`, `bead_id`, the signature, and elapsed.

### Layer B — global no-forward-progress (catches the deadlock, class 1)

Across a lane's window: there is **work with an expectation of progress** (a bead assigned/submitted,
a queue non-empty, a run mid-flight) AND a live crew, but **zero** forward-progress events (no
`run_started`, no `bead_closed`, no commit) for > `lane_noprogress_stall` min → the whole lane is
stuck even though each agent looks individually idle.

**Critical false-positive guard (make-or-break):** Layer B fires ONLY when an expectation of progress
exists. A correctly-idle crew (queue empty, nothing assigned) must NOT trip it. This guard is the
make-or-break of the whole design — get it wrong and the sentinel cries wolf and gets ignored.

The expectation-of-progress predicate is the crux. Candidate inputs (deterministic, from the run
registry + queue state + bead store):

- queue for the lane is non-empty (submitted/appended work waiting), OR
- a bead is assigned to a crew in the lane and is open/ready, OR
- a run is mid-flight (has `run_started`, no terminal) in the lane.

AND a live crew (recent `agent_presence` / heartbeat for the crew session). If NONE of the
expectation inputs hold → the lane is legitimately idle → Layer B stays silent.

## Signal-library core (the deterministic primitive both layers share)

A small library that reads `events.jsonl` + the run registry and, for each active run, computes:

- **last-event-age** — wall-clock since the most recent event for that run.
- **phase state** — started / in-implementation / verdict-fired / terminal.

Plus lane-level rollups: last forward-progress event per lane, live-crew set, queue/assignment state
for the expectation-of-progress predicate. Unit-tested against recorded / synthetic streams. Both
Layer A and Layer B are thin decision logic over this library.

## Escalation — tiered, deterministic (brief §3)

On a confirmed stall, escalate by elapsed time; each tier is a deterministic `harmonik comms send`:

- **Tier 1 (X min) → the crew:** "run <id>/bead <id> shows <signature>; no progress since <ts> —
  check it." For a *wedged* (not merely idle) crew, a bare comms message won't wake it — the nudge
  must be a **pane keystroke** (reuse the keeper's `send-keys` mechanism).
- **Tier 2 (Y min) → the captain:** same payload + "crew didn't clear it."
- **Tier 3 (Z min) → the operator:** lands in the **operator-mailbox** (the separate discussion-item
  surface), not a pane dump.

Thresholds `X/Y/Z` and every `*_stall` value are **config-driven, fail-loud if unset** (resolve like
keeper/watch config). Emit a typed `stall_detected` event for each firing.

## Where it lives (brief §4)

Build it **into the daemon (or a daemon-hosted deterministic goroutine)** because the daemon already:
holds the **run registry**, owns the **event bus**, ALREADY emits `agent_ready_stall_detected` (this
generalizes that exact mechanism), and is **always-on / survives session restarts**. Deterministic Go
— no LLM.

- **Not the LLM watch tier:** the watch is Sonnet (an agent) and session-hosted — wrong place for an
  always-on deterministic probe. The sentinel *feeds* the watch a `stall_detected` event, not BE one.
- **Not ops-monitor:** a session-hosted shell loop, no scheduler, watches backlog/queue counts not
  per-run liveness. Can consume `stall_detected` for display; shouldn't own detection.
- **Open decision for this design pass:** fully daemon-integrated vs. a small standalone Go sidecar
  with its own timer reading the socket/event log. **Lean daemon-integrated.**

## Relationship to existing pieces (brief §6)

- **`agent_ready_stall_detected`** (daemon, config `AgentReadyStallThreshold`) — the seed; Layer A
  generalizes it from launch→ready to mid-run + review.
- **keeper** — owns context-fill + the `send-keys` pane-nudge mechanism the escalation reuses;
  distinct concern (context vs liveness).
- **watch tier** (LLM) — CONSUMES `stall_detected`; does not detect.
- **`hk-thbbv`** (daemon fix: flagless REQUEST_CHANGES = APPROVE) — the FIX for class 3; this
  sentinel is the DETECTION so future novel wedges still surface.
