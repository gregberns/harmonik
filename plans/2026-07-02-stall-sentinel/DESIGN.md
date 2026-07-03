# Stall-Sentinel — deterministic stall/wedge detection + tiered escalation

**For:** the agent implementing this. **From:** admiral design, 2026-07-02 (operator idea + captain's relayed task).
**Companion fix (separate, daemon-side):** `hk-thbbv` (flagless REQUEST_CHANGES = APPROVE). This brief is the **detection** side.

---

## 0. TL;DR — the decision

Build a **deterministic (no-LLM) sentinel** that periodically snapshots harmonik agent/run liveness, computes "nothing is happening" per a small set of signatures, and **escalates on tiered thresholds** — nudge the **crew** after X min, the **captain** after Y min, the **operator** after Z min. Detection + first-nudge are pure Go/shell; a coding agent is only ever the *last-resort* responder, never the detector. This is the concrete mechanism for the standing **"fleet must not stall"** mandate.

**Motivating evidence — 3 distinct silent-stall classes in ONE night (2026-07-02), each caught only by a human/admiral polling by hand:**
1. **Coordination deadlock** — captain↔leto each waited ~20 min for the other to run the daemon redeploy. Both individually "idle/fine."
2. **Silent run hang** — a run went `run_started` → one `agent_message` → then ZERO events for ~22 min (zombie process, tmux window lingered so `sess.Wait` never returned).
3. **Review-loop wedge** — a daemon claude run sat 30 min: reviewer emitted a **flagless REQUEST_CHANGES** whose own notes said "correct + complete," implementer `/quit` with nothing to change, run **never finalized the merge**. No `run_completed` ever fired.

## 1. Signal source — `events.jsonl`, NOT tmux windows (load-bearing)

The single most important design constraint, learned the hard way: **tmux pane/window presence and worktree-dir mtime are UNRELIABLE liveness signals** — a correctly-idle pane looks identical to a hung one, and the review phase doesn't write the worktree so mtime goes stale on healthy runs. The **authoritative** signal is the typed event stream `.harmonik/events/events.jsonl`:
- `run_started`, `run_completed`, `run_failed` — run lifecycle + terminal.
- `agent_heartbeat` / `agent_message` — mid-run liveness (silence = candidate hang).
- `implementer_phase_complete`, `reviewer_verdict` (verdict fired) — phase transitions.
- `agent_presence`, `governor_signal` — daemon-alive baseline (distinguishes "daemon dead" from "run hung").
- `bead_closed`, commits on `main` — forward-progress markers.

tmux `capture-pane` is allowed ONLY as a **secondary confirm** on an already-suspect run (e.g. to see an idle prompt / `/quit` / frozen spinner), never as the primary trigger.

## 2. The detection algo — two layers

**Layer A — per-run stall** (easy, high-precision). For each run with `run_started` and no terminal event:
- **heartbeat-gap:** no `agent_heartbeat`/`agent_message` for > `run_silence_stall` min (caught class 2).
- **review-stall:** a `reviewer_verdict` fired but NO `run_completed`/`run_failed` within `review_finalize_stall` min (the EXACT class-3 signature).
- **run-age:** any run `dispatched` > `run_max_age` min with no terminal event (backstop).

**Layer B — global no-forward-progress** (catches the deadlock, class 1). Across a lane's window: there is **work with an expectation of progress** (a bead assigned/submitted, a queue non-empty, a run mid-flight) AND a live crew, but **zero** forward-progress events (no `run_started`, no `bead_closed`, no commit) for > `lane_noprogress_stall` min → the whole lane is stuck even though each agent looks individually idle.
- **Critical false-positive guard:** Layer B fires ONLY when an expectation of progress exists. A correctly-idle crew (queue empty, nothing assigned) must NOT trip it. This guard is the make-or-break of the whole design — get it wrong and the sentinel cries wolf and gets ignored.

## 3. Escalation — tiered, deterministic

On a confirmed stall, escalate by elapsed time, each tier a deterministic `harmonik comms send`:
- **Tier 1 (X min) → the crew:** "run <id>/bead <id> shows <signature>; no progress since <ts> — check it." For a *wedged* (not merely idle) crew, a bare comms message won't wake it — the nudge must be a **pane keystroke** (reuse the keeper's `send-keys` mechanism).
- **Tier 2 (Y min) → the captain:** same payload + "crew didn't clear it."
- **Tier 3 (Z min) → the operator:** lands in the **operator-mailbox** (the separate discussion-item #2 surface), not a pane dump.

Thresholds `X/Y/Z` and every `*_stall` value are **config-driven, fail-loud if unset** (per the no-hardcoded-thresholds mandate — resolve like the keeper/watch config). Emit a typed `stall_detected` event for each firing (auditable; the watch/ops-monitor can consume it).

## 4. Where it lives — deterministic Go, daemon-adjacent

Recommend building it **into the daemon (or a daemon-hosted deterministic goroutine)**, because the daemon already: holds the **run registry**, owns the **event bus**, ALREADY emits `agent_ready_stall_detected` (this generalizes that exact mechanism), and is **always-on / survives session restarts**. It is deterministic Go — no LLM, matching the operator's "without coding agents" requirement and harmonik's "deterministic skeleton" philosophy.
- **Why not the LLM watch tier:** the watch is Sonnet (an agent) and session-hosted — it's the wrong place for the always-on deterministic probe. The sentinel should *feed* the watch a `stall_detected` event, not BE an agent.
- **Why not ops-monitor:** it's a session-hosted shell loop with no scheduler and watches backlog/queue counts, not per-run liveness. Could consume `stall_detected` for display, but shouldn't own detection.
- Open decision for the kerf design pass: fully daemon-integrated vs a small standalone Go sidecar with its own timer reading the socket/event log. Lean daemon-integrated.

## 5. Acceptance — replay the 3 real stalls

The acceptance tests are the 3 classes above, replayed against recorded/synthetic event streams:
1. Deadlock: pending work + live crews + zero progress → Layer B fires at `lane_noprogress_stall`.
2. Silent hang: `run_started` + one message + silence → Layer A heartbeat-gap fires.
3. Review-wedge: `reviewer_verdict` fired, no `run_completed` within window → Layer A review-stall fires.
Plus a **negative** test: a correctly-idle crew (empty queue, nothing assigned) does NOT trip Layer B.

## 6. Relationship to existing pieces
- **`agent_ready_stall_detected`** (daemon, config `AgentReadyStallThreshold`) — the seed; Layer A generalizes it from launch→ready to mid-run + review.
- **keeper** — owns context-fill + the `send-keys` pane-nudge mechanism the escalation reuses; distinct concern (context vs liveness).
- **watch tier** (LLM) — CONSUMES `stall_detected`; does not detect.
- **`hk-thbbv`** (daemon fix: flagless REQUEST_CHANGES = APPROVE) — the FIX for class 3; this sentinel is the DETECTION so future novel wedges still surface.

## 7. Build order (beads to file)
1. **Signal library** — read `events.jsonl` + run registry, compute per-run last-event-age + phase state (the deterministic core). Unit-tested against recorded streams.
2. **Layer A detectors** — heartbeat-gap, review-stall, run-age. Emit `stall_detected`.
3. **Layer B detector** — lane no-forward-progress WITH the expectation-of-progress guard (+ the negative test).
4. **Config block** — X/Y/Z + `*_stall` thresholds, fail-loud (mirror keeper/watch resolve).
5. **Tiered escalation** — deterministic comms send per tier; pane-keystroke path for wedged crews (reuse keeper send-keys).
6. **Acceptance suite** — replay the 3 stall classes + the negative idle test.
7. **Watch/ops-monitor consumption** — surface `stall_detected` in the watch feed / latest.json (display only).
