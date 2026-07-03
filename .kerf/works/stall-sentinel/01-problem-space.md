# Stall-Sentinel — Problem Space

> Seeded from the authoritative design brief at
> `plans/2026-07-02-stall-sentinel/DESIGN.md` (admiral design, 2026-07-02).
> The brief IS the design; this artifact formalizes it into the kerf record. Do not re-derive or
> re-open — the brief is authoritative. Locked decisions are marked **[LOCKED]**.

## Summary

Build a **deterministic (no-LLM) sentinel** that periodically snapshots harmonik agent/run liveness,
computes "nothing is happening" per a small set of signatures, and **escalates on tiered thresholds**
— nudge the **crew** after X min, the **captain** after Y min, the **operator** after Z min.
Detection + first-nudge are pure Go/shell; a coding agent is only ever the *last-resort* responder,
never the detector. This is the concrete mechanism for the standing **"fleet must not stall"**
mandate.

## Motivating evidence — 3 distinct silent-stall classes in ONE night (2026-07-02)

Each caught only by a human/admiral polling by hand:

1. **Coordination deadlock** — captain↔leto each waited ~20 min for the other to run the daemon
   redeploy. Both individually "idle/fine."
2. **Silent run hang** — a run went `run_started` → one `agent_message` → then ZERO events for
   ~22 min (zombie process; tmux window lingered so `sess.Wait` never returned).
3. **Review-loop wedge** — a daemon claude run sat 30 min: reviewer emitted a **flagless
   REQUEST_CHANGES** whose own notes said "correct + complete," implementer `/quit` with nothing to
   change, run **never finalized the merge**. No `run_completed` ever fired.

## Goals

- Deterministic Go/shell sentinel that detects per-run stalls AND global lane no-forward-progress,
  with zero LLM in the detection or first-nudge path.
- Authoritative signal source = the typed `events.jsonl` stream (run registry + event bus), NOT
  tmux panes / worktree mtime.
- Tiered, deterministic escalation (crew → captain → operator-mailbox) on elapsed time.
- Config-driven, fail-loud thresholds (no hardcoded values), mirroring keeper/watch config resolve.
- Emit a typed `stall_detected` event for each firing (auditable; watch/ops-monitor consume it).

## Non-goals (explicitly out of scope)

- Any LLM-based detection. The sentinel is Go/shell; a coding agent is only the last-resort responder
  in the escalation tier, never the detector.
- Using tmux pane/window presence or worktree-dir mtime as a *primary* trigger. tmux `capture-pane`
  is allowed ONLY as a **secondary confirm** on an already-suspect run.
- The FIX for class 3 (flagless REQUEST_CHANGES = APPROVE) — that is the separate daemon-side work
  `hk-thbbv`. This work is the **detection** side, so future novel wedges still surface.
- Owning detection in the LLM watch tier or the ops-monitor shell loop. Those CONSUME `stall_detected`
  for display; they do not detect.

## Locked decisions (design steer — do NOT re-open)

1. **[LOCKED]** DETERMINISTIC (no-LLM) detection; the sentinel is Go/shell, never a coding agent.
2. **[LOCKED]** Signal source = `events.jsonl` typed events (authoritative). tmux only as a
   secondary confirm.
3. **[LOCKED]** Two-layer algo: Layer A per-run stall (heartbeat-gap, review-stall, run-age);
   Layer B global lane no-forward-progress WITH an expectation-of-progress guard.
4. **[LOCKED]** Tiered escalation: crew at X min, captain at Y min, operator (mailbox) at Z min;
   deterministic `harmonik comms send`; pane-keystroke for wedged crews (reuse keeper `send-keys`).
5. **[LOCKED]** Thresholds config-driven + fail-loud (no hardcoded values; mirror keeper/watch
   config resolve).
6. **[LOCKED]** Home: daemon-integrated deterministic Go (generalizes the existing
   `agent_ready_stall_detected`). watch/ops-monitor CONSUME the `stall_detected` event.

## Open decision (for the design pass to note; NOT re-open the locked set)

- Fully daemon-integrated goroutine vs. a small standalone Go sidecar with its own timer reading the
  socket / event log. **Lean daemon-integrated** (brief §4). Flagged for the design pass, not a
  blocker to task creation.

## Constraints (each can veto a design)

1. **events.jsonl is the only authoritative liveness signal.** tmux/mtime are unreliable — a
   correctly-idle pane looks identical to a hung one; the review phase doesn't write the worktree so
   mtime goes stale on healthy runs.
2. **Layer B false-positive guard is make-or-break.** Layer B fires ONLY when an expectation of
   progress exists. A correctly-idle crew (empty queue, nothing assigned) must NOT trip it. Get this
   wrong and the sentinel cries wolf and gets ignored.
3. **No hardcoded thresholds.** X/Y/Z and every `*_stall` value resolve from config, fail-loud if
   unset (locked no-hardcoded-defaults principle).
4. **Wedged crews need a pane keystroke, not a comms message.** A bare comms send won't wake a
   wedged (not merely idle) crew — reuse the keeper's `send-keys` mechanism.
5. **Daemon-hosted / always-on.** The probe must survive session restarts; the daemon already holds
   the run registry + event bus and is always-on.

## Success criteria (concrete, verifiable)

- Replaying the class-1 stream (pending work + live crews + zero progress) trips Layer B at
  `lane_noprogress_stall`, and no earlier.
- Replaying the class-2 stream (`run_started` + one message + silence) trips Layer A heartbeat-gap.
- Replaying the class-3 stream (`reviewer_verdict` fired, no `run_completed` within window) trips
  Layer A review-stall.
- The negative test — a correctly-idle crew (empty queue, nothing assigned) — does NOT trip Layer B.
- Each firing emits a typed `stall_detected` event with run/bead/lane + signature + elapsed.
- Escalation sends a deterministic `comms` message to the correct tier at X/Y/Z, and a pane keystroke
  for a wedged crew.
- Every threshold resolves from config; a missing required key fails loud (no silent default).
- The watch feed / ops-monitor `latest.json` surfaces `stall_detected` (display only; not detection).
