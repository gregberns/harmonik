# Park / Resume Protocol — Session-Side Contract

```yaml
---
title: Park / Resume Protocol — Session-Side Contract
spec-id: park-resume-protocol
status: draft
spec-shape: contract
spec-category: foundation-cross-cutting
version: 1.0.0
spec-template-version: 1.1
owner: sleep-wake-author
last-updated: 2026-06-17
depends-on:
  - crew-handoff-schema
  - queue-model
  - event-model
---
```

> **Normative spec for hk-s8qi (M2 of hk-rl4b, codename:sleep-wake).**
> This spec defines what LLM sessions (captain and crews) MUST do when the
> daemon's QuiesceArbiter parks or wakes them. The daemon-side contract (marker
> files, wake triggers, routing) is M1 (`internal/daemon/quiesce.go`).
>
> Load alongside `crew-launch/SKILL.md` (crew session) or
> `captain/STARTUP.md` (captain session).

---

## 1. Parties

| Role | Session | Primary watcher |
|---|---|---|
| **Captain** | One per project; MUST NOT exit | `comms recv --agent captain --follow --json` + `/loop 12m` |
| **Crew** | One per named queue; created/stopped by captain | `comms recv --follow --json` + `harmonik subscribe --heartbeat 60s` |
| **Daemon** | Deterministic Go process; stays up across fleet sleep | The QuiesceArbiter (M1) |

---

## 2. Park signal

When GenuineDrain (M0) reports `DRAINED`, the QuiesceArbiter:

1. Writes `.harmonik/.sleeping.<session_id>` marker files — one per known session.
2. Emits an `agent_message` event (via `CommsBus.EmitAgentMessage`) for each session:
   ```json
   { "from": "daemon", "to": "<agent_name>", "topic": "park",
     "body": "{\"type\":\"park\",\"reason\":\"drain_detected\"}" }
   ```
3. Stores the session's pane target in its sleeping map for the later WAKE nudge.

The park message is the authoritative signal. The `.sleeping.*` marker is written
simultaneously and is available to external observers, but sessions detect the
park via their `comms recv --follow` stream.

---

## 3. Session-side PARK procedure

### 3.1 Go-level (`comms recv --follow`) — NORMATIVE

`runCommsRecvFollowIO` (cmd/harmonik/comms.go) implements this rule:

> **RULE:** when an `agent_message` with `topic == "park"` and `from == "daemon"`
> is received, the function MUST:
> 1. Deliver the park message to the caller (output it as normal).
> 2. Close the connection.
> 3. **Return 0 WITHOUT reconnecting.**

The session's `comms recv --follow` Monitor therefore exits cleanly after
delivering the park message. The LLM session observes the Monitor exit.

**Tests:** `TestCommsRecvFollow_ParkMessageExitsWithoutReconnect`
(cmd/harmonik/comms_recv_follow_hk5xuvc_test.go).

### 3.2 LLM-session behavior (crew) — NORMATIVE

When the crew's `comms recv --follow` Monitor exits with the park message as its
last output line:

1. **Stop the `harmonik subscribe` Monitor.** Kill or let expire any active
   subscribe Monitor (the one with `--heartbeat 60s` for run events). Do NOT
   re-arm it.
2. **Do NOT re-arm `comms recv --follow`.** The Monitor has self-exited; leave it
   stopped.
3. **Pause the ≤10-min progress-feed timer.** Do not post status while parked.
4. **Await the pane nudge (WAKE).** The daemon's QuiesceArbiter will inject an
   Enter key into the session's pane when new work arrives or an epic completes.

### 3.3 LLM-session behavior (captain) — NORMATIVE

When the captain's `comms recv --follow` Monitor exits with the park message:

1. **Cancel the `/loop 12m` health tick.** Do not let it re-arm.
2. **Do NOT re-arm `comms recv --follow`.** The Monitor self-exited; leave it
   stopped.
3. The captain pane remains open but idle (MUST NOT self-exit — R-C4.11).
4. **Await the pane nudge (WAKE).**

---

## 4. Wake procedure

The daemon's QuiesceArbiter wakes a session by calling `SendKeysEnter` on its
stored pane target (e.g. when `QueueStore.WakeCh` fires on a new queue submit,
or an `epic_completed` event fires for the captain, or the max-sleep failsafe
expires).

### 4.1 Captain WAKE

On pane nudge:

1. Run the full `STARTUP.md` boot sequence (Steps 1–6). Treat the wake exactly
   like a fresh session start — re-derive live state, do NOT trust pre-sleep
   snapshots.
2. Re-arm `comms recv --follow` (Step 5 in STARTUP.md).
3. Re-arm the `/loop 12m` health tick (STARTUP.md §6 Watcher 2).
4. Staff all ready lanes (autonomous duty — same as fresh boot).

### 4.2 Crew WAKE

Crews are typically stopped by the captain (`harmonik crew stop <name>`) when
the fleet sleeps, and re-started by the captain on wake (`harmonik crew start
<name>`). The crew's boot sequence (crew-launch SKILL.md §Boot sequence) handles
re-arming all Monitors.

If a crew was NOT stopped (only quiesced via the park protocol above), on pane
nudge:

1. Re-arm `harmonik subscribe` Monitor.
2. Re-arm `comms recv --follow` Monitor.
3. Resume the progress-feed timer.
4. Re-poll `br ready` and continue draining the epic.

---

## 5. Risk ownership

| Risk | Owner | Mechanism |
|---|---|---|
| **Risk 1 — false drain** | M0 GenuineDrain (shared with keeper M3) | All 4 drain checks must pass before park |
| **Risk 2 — missed wake** | M1 max-sleep failsafe (4 h ceiling) | Auto-wake any session asleep > 4 h |
| **Risk 3 — wrong crew woken** | M1 queue-routing in QuiesceArbiter | WakeCh routes to crew bound to the queue with pending items |
| **Risk 4 — captain wake on epic** | M1 epic_completed + agent_message handlers | Captain is woken on epic_completed or message to "captain" |

---

## 6. Invariants

1. **No re-arm after park, before wake.** A session MUST NOT re-arm any loop
   between receiving the park message and receiving the pane nudge.
2. **Park is idempotent.** If `comms recv --follow` re-connects before the park
   message is delivered (e.g. a transient reconnect), the park message will be
   re-delivered on the new connection. The session exits cleanly on the first
   delivery; duplicate deliveries are no-ops (session is already quiesced).
3. **Exit 0, not 1.** The `comms recv --follow` park exit is code 0. Code 1
   would indicate an error; the skill MUST distinguish these and only quiesce
   on code 0 with a park message as the last output line.
4. **Crew stop is sufficient.** `harmonik crew stop <name>` fully quiesces a
   crew (pane removed, keeper marker cleared). It is equivalent to — and a
   superset of — the crew's own park procedure. The captain MAY use `crew stop`
   instead of sending a park message to crews.

---

## 7. Bead references

| Bead | Role |
|---|---|
| hk-95uf | M0 — GenuineDrain oracle |
| hk-jeby | M1 — QuiesceArbiter (daemon-side park/wake) |
| hk-s8qi | M2 — this spec (session-side park/resume) |
| hk-rl4b | Parent epic — fleet sleep-wake |
