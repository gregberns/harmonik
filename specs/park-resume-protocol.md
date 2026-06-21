# Park / Resume Protocol — Session-Side Contract

```yaml
---
title: Park / Resume Protocol — Session-Side Contract
spec-id: park-resume-protocol
status: draft
spec-shape: contract
spec-category: foundation-cross-cutting
version: 1.1.0
spec-template-version: 1.1
owner: sleep-wake-author
last-updated: 2026-06-21
depends-on:
  - crew-handoff-schema
  - queue-model
  - event-model
  - system-state
---
```

> **Normative spec for hk-s8qi (M2 of hk-rl4b, codename:sleep-wake).**
> This spec defines what LLM sessions (captain and crews) MUST do when they are
> parked or woken. The daemon-side plumbing (marker files, wake triggers,
> routing) lives in `internal/daemon/quiesce.go` (internal symbol name;
> operator-facing vocabulary uses the terms defined in §0 below).
>
> **v1.1 additions (hk-wrjv):** §0 Vocabulary (SLEEP/PARK/STOP/TEARDOWN),
> §8 the `--level` enum, §9 workflow sequences (W1/W3/W4/teardown).
>
> Load alongside `crew-launch/SKILL.md` (crew session) or
> `captain/STARTUP.md` (captain session).

---

## 0. Vocabulary

The following terms are **normative**. The word "quiesce" MUST NOT appear in
any operator-facing string, CLI output, or doc. (Internal Go symbol names such
as `QuiesceArbiter` and `quiesce.go` are out of scope here — they are low-pri
plumbing rename candidates; see `internal/daemon/quiesce.go`.)

| Term | Scope | Meaning |
|---|---|---|
| **SLEEP** | Whole fleet | Operator-initiated end-of-day wind-down. All sessions stop their waking loops; daemon stays alive as the sole wake trigger. Reversible via `harmonik wake`. |
| **PARK** | One session or fleet | Interrupt-and-hold at the nearest safe point. Work in flight is not guaranteed to finish before the park. Reversible on pane nudge. |
| **STOP** | One crew | Captain-issued crew teardown (`harmonik crew stop <name>`). Pane removed, keeper marker cleared. Crew is fully recoverable via `harmonik crew start <name>`. |
| **TEARDOWN** | Whole fleet | Irreversible fleet shutdown. All sessions removed; daemon stopped; markers cleared. NOT recoverable by a pane nudge — requires a fresh boot. |
| **at rest / asleep** | Any session | Descriptive state: session is parked or sleeping; its waking loops are disarmed. |
| **WAKE / RESUME** | Any session or fleet | Pane nudge or `harmonik wake` returns a parked/sleeping session to its normal operating loop. |

> Cross-reference: `system-state` spec §3 Glossary also defines these terms;
> the definitions are normatively identical. `park-resume-protocol.md` owns the
> session-side *behavior*; `system-state.md` owns the *observation* of that
> state in a snapshot.

---

## 1. Parties

| Role | Session | Primary watcher |
|---|---|---|
| **Captain** | One per project; MUST NOT exit | `comms recv --agent captain --follow --json` + `/loop 12m` |
| **Crew** | One per named queue; created/stopped by captain | `comms recv --follow --json` + `harmonik subscribe --heartbeat 60s` |
| **Daemon** | Deterministic Go process; stays up across fleet sleep | Wind-down coordinator (`internal/daemon/quiesce.go`, M1) |

---

## 2. Park signal

When the captain (LLM) initiates a SLEEP or PARK, the daemon's wind-down
coordinator (`internal/daemon/quiesce.go`) executes the session-side park:

1. Writes `.harmonik/.sleeping.<session_id>` marker files — one per known session.
2. Emits an `agent_message` event (via `CommsBus.EmitAgentMessage`) for each session:
   ```json
   { "from": "daemon", "to": "<agent_name>", "topic": "park",
     "body": "{\"type\":\"park\",\"reason\":\"<reason>\"}" }
   ```
   Where `reason` is one of `operator_sleep`, `operator_park`, or `semantic_park`
   (see §0 vocabulary and §9 workflow sequences).
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
last output line, the crew is **at rest** (see §0):

1. **Stop the `harmonik subscribe` Monitor.** Kill or let expire any active
   subscribe Monitor (the one with `--heartbeat 60s` for run events). Do NOT
   re-arm it.
2. **Do NOT re-arm `comms recv --follow`.** The Monitor has self-exited; leave it
   stopped.
3. **Pause the ≤10-min progress-feed timer.** Do not post status while at rest.
4. **Await the pane nudge (WAKE).** The daemon will inject an Enter key into the
   session's pane when new work arrives, an epic completes, or the operator runs
   `harmonik wake`.

### 3.3 LLM-session behavior (captain) — NORMATIVE

When the captain's `comms recv --follow` Monitor exits with the park message,
the captain is **at rest**:

1. **Cancel the `/loop 12m` health tick.** Do not let it re-arm.
2. **Do NOT re-arm `comms recv --follow`.** The Monitor self-exited; leave it
   stopped.
3. The captain pane remains open but idle (MUST NOT self-exit — R-C4.11).
4. **Await the pane nudge (WAKE).**

---

## 4. Wake procedure

The daemon wakes a session by calling `SendKeysEnter` on its stored pane target
(e.g. when `QueueStore.WakeCh` fires on a new queue submit, or an
`epic_completed` event fires for the captain, or the max-sleep failsafe
expires, or `harmonik wake` is invoked by the operator).

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

If a crew was NOT stopped (only parked / at rest via the protocol above), on
pane nudge:

1. Re-arm `harmonik subscribe` Monitor.
2. Re-arm `comms recv --follow` Monitor.
3. Resume the progress-feed timer.
4. Re-poll `br ready` and continue draining the epic.

---

## 5. Risk ownership

| Risk | Owner | Mechanism |
|---|---|---|
| **Risk 1 — false drain** | M0 GatherDrainFacts (shared with keeper M3) | All fact axes must be empty before captain initiates park (veto-on-execute in non-force `harmonik sleep`) |
| **Risk 2 — missed wake** | Max-sleep failsafe (4 h ceiling) | Auto-wake any session at rest > 4 h (`internal/daemon/quiesce.go`) |
| **Risk 3 — wrong crew woken** | Queue-routing in daemon wake path | `WakeCh` routes to crew bound to the queue with pending items |
| **Risk 4 — captain wake on epic** | `epic_completed` + `agent_message` handlers | Captain is woken on `epic_completed` or message to "captain" |

---

## 6. Invariants

1. **No re-arm after park, before wake.** A session MUST NOT re-arm any loop
   between receiving the park message and receiving the pane nudge.
2. **Park is idempotent.** If `comms recv --follow` re-connects before the park
   message is delivered (e.g. a transient reconnect), the park message will be
   re-delivered on the new connection. The session exits cleanly on the first
   delivery; duplicate deliveries are no-ops (session is already at rest).
3. **Exit 0, not 1.** The `comms recv --follow` park exit is code 0. Code 1
   would indicate an error; the skill MUST distinguish these and only enter the
   at-rest state on code 0 with a park message as the last output line.
4. **Crew stop is sufficient.** `harmonik crew stop <name>` fully brings a crew
   to rest (pane removed, keeper marker cleared). It is equivalent to — and a
   superset of — the crew's own park procedure. The captain MAY use `crew stop`
   instead of sending a park message to crews. (See §0 vocabulary: STOP is the
   one-crew variant of the wind-down surface.)

---

## 7. Bead references

| Bead | Role |
|---|---|
| hk-95uf | M0 — GatherDrainFacts oracle (formerly GenuineDrain) |
| hk-jeby | M1 — daemon wind-down plumbing (park/wake/markers; `internal/daemon/quiesce.go`) |
| hk-s8qi | M2 — session-side park/resume contract (§§1–6) |
| hk-rl4b | Parent epic — fleet sleep-wake |
| hk-wrjv | P3 — vocabulary + `--level` enum + workflow sequences (§§0, 8, 9) |
| hk-up4b | Fleet-state umbrella epic (Phase 0–4) |

---

## 8. The `--level` enum — hard ↔ soft wind-down spectrum

Every wind-down verb (SLEEP, PARK, STOP, TEARDOWN) accepts an optional
`--level` flag that controls how aggressively work in flight is interrupted.
**Choosing the level is the LLM's judgment; executing it is deterministic Go.**

| Level | Name | What happens to in-flight work | When to use |
|---|---|---|---|
| **L0** | `abandon` | Kill immediately — current run abandoned; bead returns to `open` | Emergency stop; operator force-teardown |
| **L1** | `drain` | Current run allowed to commit; queue paused immediately after; no new dispatches | Clean pause with minimal delay |
| **L2** | `handoff` | Current run finishes; crew writes `/session-handoff`; captain integrates the handoff; then stop | Graceful end-of-shift; state durable in handoff file |
| **L3** | `finish-lane` | Run to natural queue empty (no kill, no pause); park only when the lane drains itself | Low urgency; let work complete organically |

**Default when `--level` is omitted:** L2 (`handoff`) for operator-issued SLEEP;
L1 (`drain`) for operator-issued PARK; L0 (`abandon`) for TEARDOWN.

### 8.1 Level semantics by role

| Role | L0 | L1 | L2 | L3 |
|---|---|---|---|---|
| **Captain** | `harmonik sleep --force` immediately; no handoff | Cancel health tick + subscribe; `harmonik sleep` (veto-gated) | Run `/session-handoff`; `harmonik sleep` | Stay alive; re-arm watcher; sleep when drain fires |
| **Crew** | `harmonik crew stop <name>` immediately | Stop subscribe Monitor; let current run commit; then `crew stop` | Finish run; write `/session-handoff`; captain integrates; `crew stop` | Continue operating until epic drains; then `crew stop` |
| **Queue** | `harmonik queue pause --force` | `harmonik queue pause` (drains current item) | Same as L1, but wait for handoff write before pause confirmation | No pause; natural drain |

### 8.2 Invariants

1. **Level is chosen once per wind-down verb invocation.** The captain MAY
   escalate (L3→L2→L1→L0) if a level's conditions are not met within a
   reasonable window (e.g. a L3 that never drains after 30 min), but MUST NOT
   de-escalate silently.
2. **`--force` overrides the non-force veto gate** (§2, `harmonik sleep --force`)
   regardless of level, but does NOT change the level's in-flight treatment:
   `--force --level L2` still writes the handoff first; it only skips the
   work-present refusal.
3. **L0 is the only level that strands a bead.** L1+ guarantee at minimum that
   the current run commits (bead closes) before the session goes at rest.

---

## 9. Workflow sequences

Each workflow names: who initiates, the `--level` used, the session-side steps
in order, and the terminal state. **Build is deferred (Phase 3)** — these are
spec-only sequences for sign-off before implementation.

### W1 — Operator SLEEP (L2 handoff)

**Trigger:** operator runs `harmonik sleep` (or `harmonik sleep --level L2`).
Typical scenario: end of working day; want all work to finish and hand off
cleanly before sessions go at rest.

```
Operator → `harmonik sleep [--level L2]`
  │
  ├─ Daemon veto gate: GatherDrainFacts — if any dispatchable/in-flight axis
  │    non-empty AND `--force` not set → REFUSE with reason
  │
  ├─ [If proceeding] For each crew (parallel):
  │    1. Daemon sends park message (reason: operator_sleep, level: L2)
  │    2. Crew finishes current run (commits)
  │    3. Crew writes `/session-handoff`
  │    4. Captain receives handoff via comms; integrates it into lane state
  │    5. Daemon executes `crew stop` → pane removed, marker written
  │
  ├─ Captain:
  │    1. Receives park message (after crew handoffs integrated)
  │    2. Cancels `/loop 12m` + subscribe Monitor
  │    3. Writes own `/session-handoff` (captain-level state summary)
  │    4. Pane stays alive; enters at-rest state
  │
  └─ Terminal state: fleet ASLEEP. Daemon alive. `.sleeping.*` markers present.
       Wake trigger: `harmonik wake`, `queue submit` (auto-wake), or 4h failsafe.
```

**Exit condition:** all crew panes removed; captain pane alive but at rest;
no active subscribe or loop monitors firing.

---

### W3 — Captain semantic no-work PARK (L3 finish-lane)

**Trigger:** captain LLM determines, via `harmonik state --json`, that all
dispatchable work is blocked (operator-blocked, or no ready beads and no beads
that can be decomposed right now). This is the **one LLM-owned park decision** —
Go cannot make this judgment because generative/latent work is invisible to the
fact bundle.

```
Captain (LLM) notices:
  • GatherDrainFacts work_axes: no ready beads, nothing in flight
  • needs_decomposition epics are all operator-blocked / awaiting input
  • Captain judgment: "no meaningful work I can initiate; semantic drain"

Captain initiates:
  1. Logs decision to comms (reason: semantic_park)
  2. Runs `harmonik sleep --level L3`
       └─ veto gate re-checks GatherDrainFacts; if still empty → proceeds
  3. For each crew with a running epic:
       • Sends park message (reason: semantic_park, level: L3)
       • Crew continues to natural queue empty, then self-stops (L3 finish-lane)
  4. Captain cancels `/loop 12m` + subscribe Monitor
  5. Captain pane alive; enters at-rest state

Terminal state: fleet ASLEEP (captain at rest; crews either stopped or draining
  to stop). Daemon alive.
Wake trigger: `harmonik wake`, `queue submit` (new work → WakeCh auto-wake),
  `epic_completed` (captain woken to re-plan), or 4h failsafe.
```

**Key constraint:** W3 MUST NOT fire if `GatherDrainFacts` shows any
dispatchable or in-flight work (the veto gate enforces this). The
`needs_decomposition` axis alone does NOT block W3 — generative work is flagged
for the captain's judgment, not a Go veto (per `system-state` SS-INV-003).

---

### W4 — Crew context-pressure handoff (L2→L0 self-stop)

**Trigger:** keeper WARN fires on a crew (context at or above the WARN
threshold). The crew must hand off and stop itself before reaching the ACT
ceiling and losing context.

```
Keeper WARN fires on crew session:
  │
  ├─ Crew LLM receives WARN injection in pane
  │
  ├─ Crew attempts L2 handoff:
  │    1. Finishes current bead (commits) if one is in flight
  │    2. Writes `/session-handoff` (current epic state + in-progress summary)
  │    3. Sends comms message to captain: "handing off due to context pressure"
  │    4. Captain receives handoff + marks crew for restart
  │
  ├─ Crew runs `harmonik crew stop <crew-name>` on itself (self-stop)
  │    → pane removed; keeper marker cleared; `.sleeping.*` NOT written
  │      (this is a STOP, not a PARK — crew is removed, not at rest)
  │
  └─ Captain:
       1. Receives handoff
       2. Notes crew epic + queue in mission file
       3. Runs `harmonik crew start <crew-name>` (fresh context)
          → new crew boots from mission file; resumes epic

Escalation to L0 (abandon): if the crew reaches the ACT ceiling before
  completing the L2 handoff, the keeper triggers `/clear` + `/session-resume`.
  The active bead returns to `open` (stranded, L0 semantics). The captain
  re-dispatches on the next health tick.
```

**Note:** W4 is crew-autonomous — it does NOT require captain initiation. The
captain is notified via comms but is not the trigger. The keeper's WARN/ACT
cycle is the trigger (see `keeper` skill for thresholds and config).

---

### Teardown-as-transition (TEARDOWN, L0)

TEARDOWN is an **irreversible fleet transition** — it is not a park that can be
resumed by a pane nudge. It differs from SLEEP in that the daemon itself stops
and all markers, panes, and keeper bindings are cleared.

```
Operator → `harmonik teardown` (or equivalent)

For each crew (parallel, L0):
  1. `harmonik crew stop <name> --force` (pane killed immediately)
  2. Bead in flight → stranded, returns to `open`

Captain:
  1. Receives teardown signal (comms or direct)
  2. Runs final `/session-handoff` (best-effort; context may be short)
  3. Pane killed

Daemon:
  1. Clears all `.sleeping.*` markers
  2. Clears queue state (or archives, per operator preference)
  3. Stops gracefully (SIGTERM)

Terminal state: NO sessions alive. NO daemon. NO markers.
Recovery: manual `harmonik start captain` after daemon restart.
         Beads left `open` are re-dispatchable — the bead ledger survives.
```

**Teardown vs SLEEP distinction:**
- SLEEP: daemon stays alive; sessions at rest; recoverable by pane nudge or `harmonik wake`.
- TEARDOWN: daemon stops; sessions removed; recovery requires a fresh boot.

---

## 10. Changelog

| Version | Date | Author | Summary |
|---|---|---|---|
| 1.0.0 | 2026-06-17 | sleep-wake-author | Initial spec (M2): park signal, session PARK/WAKE procedures, invariants |
| 1.1.0 | 2026-06-21 | hk-wrjv (P3-SPEC) | Added §0 vocabulary (SLEEP/PARK/STOP/TEARDOWN; no "quiesce"), §8 `--level` enum (L0–L3), §9 workflow sequences (W1/W3/W4/teardown); scrubbed operator-facing "quiesce" from §§2–6 |
