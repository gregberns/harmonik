# hitl-decisions — Change Spec (v1 draft)

**Work:** hitl-decisions (bead hk-0zxv6, P3) · **Pass 5 (change-spec)** · author crew **liet** · 2026-06-11.
**Scope:** DESIGN-ONLY. This spec defines *what to build*; **paul owns the daemon-infra implementation**. No code is changed by this work.
**Decisions adopted** (see `01-problem-space.md`): D1 harmonik/comms-layer owns it · D2 both surfaces on one contract, harmonik-first · D3 pick-an-option, no auto-decide · D4 event-log projection is source-of-truth, bead marker daemon-written-only.
**Substrate** (see `04-research/findings.md`): the existing `agent-comms` bus, `harmonik subscribe`, `events.jsonl`, and the daemon socket — extended additively. No new transport, datastore, or always-on service.

`hitl-decisions` is the **agent→human dual** of `agent-comms`: an agent hits a decision point, emits a typed `decision_needed`, blocks cleanly, and resumes on the human's `decision_resolved`; one `harmonik decisions` command aggregates every open decision into one "what-needs-me" queue.

---

## §1 Event schemas

Three new typed events on the standard EV-001 envelope (`schema_version`, `event_id` UUIDv7 bus-minted, `timestamp_wall`, `type`, optional `run_id`, `source_subsystem`, `payload`). All three are **F-class** (see N1). Type constants added to `internal/core/eventtype.go`; constructors registered via `RegisterEventType` (`eventregistry.go:79`); payloads modeled on `AgentMessagePayload` (`agentcommspayloads_djqc9.go:77`) with a `Valid()` method.

**`decision_id`** = the `decision_needed` event's own `event_id` (UUIDv7), returned to the agent by `raise`. The two terminals carry it as `payload.decision_id` (mirrors `agent_message.in_reply_to`). It is distinct from the terminals' own `event_id`s (satisfies C7).

### 1.1 `decision_needed`
```json
{ "type": "decision_needed", "payload": {
  "question":        "string (required) — the decision the human must make",
  "options":         ["string", "..."],   // required, ≥1 enumerated choices
  "context_link":    "string — free-form: bead id / work codename / thread / run_id",
  "blocked_agent":   "string — the emitting agent (who is blocked)",
  "value_requested": false                  // optional; v1.1 hook — if true the answer MAY carry free-text value; v1 ignores
}}
```
The decision is keyed in the projection by this event's `event_id`.

### 1.2 `decision_resolved`
```json
{ "type": "decision_resolved", "payload": {
  "decision_id":   "string (required) — the decision_needed event_id",
  "chosen_option": "string (required) — MUST be one of decision_needed.options (N7)",
  "value":         "string (optional, v1.1; empty in v1)",
  "resolver":      "string — who answered (e.g. \"operator\")"
}}
```

### 1.3 `decision_withdrawn`
```json
{ "type": "decision_withdrawn", "payload": {
  "decision_id": "string (required)",
  "reason":      "self_obsoleted | orphaned",
  "by":          "string — agent name (self_obsoleted) | \"keeper\" (orphaned; keeper is the sole emitter, N9)"
}}
```

## §2 CLI surface — `harmonik decisions`

New top-level route at `cmd/harmonik/main.go:465` (sibling of `"comms"`); verb switch copied from `runCommsSubcommand` (`comms.go:88`) as `runDecisionsSubcommand`. All daemon-bound verbs dial the socket (`<project>/.harmonik/daemon.sock`, `{"op":...,"payload":{}}` → `{ok,result,error}`, exit 17 if absent); new daemon op-cases at `internal/daemon/socket.go:394`.

**Verb → daemon-op map (explicit):** `raise` → `decisions-raise` (emit `decision_needed`, return `decision_id`) · `answer` → `decisions-answer` (emit `decision_resolved`) · `withdraw` → `decisions-withdraw` (emit `decision_withdrawn`) · `list` and `show` → `decisions-list` (`show` = `list` filtered to one `decision_id`, client-side) · **`wait` has NO daemon op** — it is a pure client-side `subscribe` stream over the existing subscribe op (§4 arm-then-check). So three emit-ops + one read-op; `wait` reuses `subscribe`.

**Agent side**
- `harmonik decisions raise --question "…" --option A --option B [--option …] [--context <link>] [--from <agent>] [--wait]` — emits `decision_needed`; prints the minted `decision_id`. With `--wait`, blocks until the terminal (see §4) and prints the `chosen_option`.
- `harmonik decisions wait <decision_id>` — block until this decision's terminal arrives; print `chosen_option` (resolved) or the withdrawal reason. Holds an open subscribe stream (§4).
- `harmonik decisions withdraw <decision_id> [--reason self_obsoleted]` — agent cancels its own decision.

**Operator side**
- `harmonik decisions list [--json]` — the **what-needs-me queue**: every open decision rendered as `question · options · blocked_agent · context_link · decision_id`, across all agents/works.
- `harmonik decisions answer <decision_id> <option> [--value <text>] [--resolver <name>]` — emits `decision_resolved` → unblocks the agent. No-op on an unknown/terminal id (N3).
- `harmonik decisions show <decision_id>` — full detail + lifecycle.

## §3 Open-set projection (K3)

The open-decision set is a **pure fold over `events.jsonl`**, computed on demand (no persistent aggregator — C3). Mirror `ComputePresenceRegistry()` (`comms.go:759-847`): a single `eventbus.ScanAfter(path, 0)` (`jsonlwriter.go:312`), folding into `map[decision_id]Decision`:
- add on `decision_needed` (key = event_id);
- remove on `decision_resolved` / `decision_withdrawn` (key = payload.decision_id);
- dedupe on `event_id` (N2).
Open = `needed − (resolved ∪ withdrawn)`. Restart-survivable for free (the daemon replays the log on boot, `daemon.go:1514-1528`). **Both** `decisions list` and the kerf reader view (K7) read this one projection (D2).

## §4 Blocked-wait contract (K2 + K6 unified) — NORMATIVE

Delivery is **at-least-once and PULL-only**: an idle agent does NOT wake on event arrival unless it is *holding an open subscribe/recv stream* (`A2-crew-retro.md:21-29`). Therefore:

> **A blocked agent MUST wait by holding an open `harmonik subscribe --types decision_resolved,decision_withdrawn` stream (this is what `decisions wait` / `raise --wait` do), not by idling bare.**

This single mechanism solves two problems at once:
1. **Wake:** the stream delivers the matching terminal (filtered by `decision_id`), and the agent resumes with `chosen_option`. Dedupe on `event_id` (N2); a second terminal for the same `decision_id` is a no-op (N3).
2. **Keeper-alive:** the subscribe stream's heartbeat (every 60s, `internal/daemon/subscribe.go:506-542`) keeps the agent's keeper gauge fresh, so session-keeper does not reap it as a 120s-silent hang (`internal/keeper/watcher.go:208`). Belt-and-suspenders: a `.decision_waiting` marker may be added to the keeper staleness-exemption (mirroring the `.dispatching` gate, `gates.go:79`) — optional, since the heartbeat already covers it.

**Read-after-arm ordering (NORMATIVE — N8, prevents the answer-vs-arm race).** A subscribe stream only delivers events that arrive *after* it is armed. If `answer` fires `decision_resolved` between the agent reading §3 (saw "open") and arming its stream, the terminal is already in the log and the fresh stream never sees it → the agent waits forever. Therefore `decisions wait` / `raise --wait` MUST: (1) **arm** the `subscribe --types decision_resolved,decision_withdrawn` stream *first*; (2) **then re-project §3** (re-scan the log) for this `decision_id`; (3) if already terminal, return immediately with the logged result; (4) else block on the stream. This is the standard subscribe-then-check pattern — paul will get it wrong without the explicit ordering.

On restart, the agent re-derives its open decisions from the §3 projection and re-establishes the wait via the same arm-then-check ordering (S7b); if it does not return, K5 reaps the decision.

## §5 Lifecycle, orphan reaping (K5), keeper seam (K6)

- **Orphan reaper (K5) — precise "truly gone" predicate (NORMATIVE).** Presence has only Online/Stale/Offline and **no "waiting-on-decision" signal** (`comms.go:718-744`), so reaping on mere absence/Stale would withdraw a *momentarily-quiet-but-alive* blocked agent (TOCTOU). Because §4 keeps a genuinely-waiting agent **Online** via its stream heartbeat, the only sound "gone" signals are: **(a)** the agent emitted an explicit `leave` beat, OR **(b)** the agent is **Offline** — past the ~10-min cutoff, *not* merely Stale (120s). K5 reaps `decision_withdrawn(reason=orphaned, by="keeper")` **only** under (a) or (b) — and only the keeper tick emits it (N9). A Stale (quiet <10min) agent is presumed still blocked and is **not** reaped.
- **Reaper cadence & latency bound (NORMATIVE).** Do **not** hang K5 on the 1-hour reconciliation sweep (`daemon.go:405`) — orphan latency would be ~1h. Split read-visibility from emission to keep reads pure and emission single-writer: **(i)** `decisions list` **flags** an open decision whose `blocked_agent` is Offline as *orphaned-pending* in its output (display-only, no read-side emit — immediate operator visibility); **(ii)** the **keeper watch tick is the SOLE emitter** of `decision_withdrawn(reason=orphaned, by="keeper")` once the predicate holds. Asserted bound: an orphaned decision is *flagged* the instant it's listed and *formally withdrawn* within **≤ the Offline cutoff (~10min) + one keeper tick** — never the 1h sweep. The agent, being gone, never needed the answer → no zombie (G6, S7a).
- **Keeper seam (K6):** before reaping an idle agent, session-keeper consults the §3 projection; an agent with an open decision (and a fresh heartbeat per §4) is *blocked*, not *hung* — exempt. K5 reaps the *decision* when the agent is truly gone; K6 protects the *agent* while it is legitimately blocked. The two are complementary, not in tension.
- **Idempotency (C7):** `answer`/`withdraw` on an unknown or already-terminal `decision_id` is a no-op (N3) — no error, no second wake.

## §6 Normative conditions

- **N1 — F-class durability.** `decision_needed` / `decision_resolved` / `decision_withdrawn` MUST be added to `fsyncBoundaryEventTypes` (`busimpl.go:115-131`). Else a terminal can be lost on crash before the blocked agent reads it.
- **N2 — dedupe on `event_id`.** Consumers MUST treat a re-delivered `event_id` as a no-op (at-least-once / N3-of-agent-comms). Flagged in the agent skill/handler-contract.
- **N3 — decision_id idempotency / first-writer-wins (LOCKED, §9).** Resolution/withdrawal is keyed on `decision_id` and idempotent: resolving an unknown or already-terminal `decision_id` is a no-op. Policy is **first-writer-wins** — the first `decision_resolved` for a `decision_id` is authoritative; any later `decision_resolved` (a second human, a replay) is a no-op, no second wake. (Beyond N2's per-event dedupe.) Multi-human arbitration deferred (NG1).
- **N4 — write discipline.** Agents MUST NOT write terminal bead state; any bead "blocked-on-human" marker is **daemon-written only** (C5/D4).
- **N5 — blocked-wait.** The blocked agent MUST hold an open subscribe/recv stream to wait (§4) — both for wake and keeper-aliveness.
- **N6 — clean tree.** `decisions answer`/`raise` MUST leave `git status` unchanged (events under gitignored `.harmonik/`; mirrors agent-comms SC-3).
- **N7 — option validity.** `chosen_option` MUST be one of `decision_needed.options` in v1 (unless `value_requested`).
- **N8 — read-after-arm.** A waiting agent MUST arm its subscribe stream *before* re-projecting §3 for the terminal, and return immediately if already terminal (§4). Prevents the answer-lands-between-read-and-arm race.
- **N9 — orphan-reap predicate & single-writer.** K5 withdraws an open decision as `orphaned` ONLY when its `blocked_agent` has `leave`d or is Offline (~10-min cutoff), never on Stale (§5). The **keeper tick is the sole emitter**; `decisions list` only *flags* orphaned-pending (no read-side emit). Bound: flagged on list, withdrawn within ≤ Offline-cutoff + one keeper tick (never the 1h sweep).

## §7 Files & changes (implementation handoff — paul's lane)

| Component | Files (anchors from research) | Change |
|-----------|------------------------------|--------|
| K1 events | `internal/core/eventtype.go`; `eventregistry.go:79`; new `…payloads` file modeled on `agentcommspayloads_djqc9.go:77`; **`busimpl.go:115` fsync map (N1)** | 3 type constants + 3 payload structs w/ `Valid()` + registration + fsync-boundary entries; §8.x event-model doc entries (EV-025) |
| K2 raise/wait | `cmd/harmonik/main.go:465`; new `cmd/harmonik/decisions.go` (mirror `comms.go:88`); emit ops mirror `commshandler_nbrmf.go:39` | `raise`→`decisions-raise` (return `decision_id`); `withdraw`→`decisions-withdraw`; **`wait` = client-side `subscribe` stream, NO new op**, with the N8 arm-then-check ordering |
| K3 projection | new `decisionsProjection()` mirroring `ComputePresenceRegistry()` (`comms.go:759-847`) + `ScanAfter` (`jsonlwriter.go:312`) | pure fold → open set; shared by K4 + K7 |
| K4 operator | `cmd/harmonik/decisions.go`; daemon op-cases `socket.go:394` | `list`/`show`→`decisions-list`; `answer`→`decisions-answer` (emits `decision_resolved`, no-op on unknown/terminal — N3) |
| K5 reaper | keeper-tick = **sole emitter**; `decisions list` flags only (read-pure); NOT the 1h reconciliation (`daemon.go:405`) | keeper tick emits `decision_withdrawn(orphaned, by=keeper)` when `blocked_agent` `leave`d or Offline (N9), never on Stale; `list` flags orphaned-pending, never emits |
| K6 keeper | `internal/keeper/gates.go:53-88`, `watcher.go:208` | exempt blocked-on-decision from the 120s reaper (heartbeat covers it; optional `.decision_waiting` gate) |
| K7 kerf view | kerf reader of §3 projection; optional daemon-written bead marker | v1-SECOND; reads same projection, no new transport |

## §8 Acceptance criteria (→ problem-space S1–S8)

- **S1** raise emits `decision_needed` + agent blocks cleanly (holding a stream, §4) — event lands in `events.jsonl`.
- **S2** `decisions list` shows ALL open decisions from ≥2 distinct agents in one output.
- **S3** `decisions answer` emits `decision_resolved` → the originating agent wakes with `chosen_option`.
- **S4** replaying the resolve event → no double-apply, no second wake (N2).
- **S5** the open set is identical after a daemon restart; resolved/withdrawn drop out (§3 replay).
- **S6** `decisions list` renders with no aggregator process running (pure projection, C3).
- **S7** (a) kill a blocked agent → K5 withdraws its decision, it leaves the queue; (b) restart the agent → it re-establishes the wait and still resolves (or is cleanly withdrawn).
- **S8** answering the same `decision_id` twice, or a bogus id, is a no-op — single apply, single wake (N3).

### §8.1 Validation beads (change-spec→integration gate)

Two test beads gate this work (filed 2026-06-13, label `codename:hitl-decisions`):
- **`hk-rz4`** (scenario-test) — `scenario: hitl-decisions K2+K4 — raise→block→answer→wake end-to-end`. Drives `harmonik decisions raise --wait` + `answer`; asserts the `decision_resolved` JSONL record + agent wake with `chosen_option`, plus first-writer-wins (N3). Covers S1,S3,S4,S8.
- **`hk-1vl`** (exploratory-test) — `explore: hitl-decisions K4 — decisions list what-needs-me queue across agents`. Drives `harmonik decisions list [--json]`; asserts the cross-agent open-decision queue renders with no aggregator process (S2,S6) and flags Offline-blocked decisions orphaned-pending (N9).

## §9 Integration seams & risks

- **kerf (K7, v1-second):** kerf has no existing blocked-on-human concept (`kerf.md:65`) — no collision. The cross-works "what-needs-me" view is an out-of-band reader of the §3 projection; note the orthogonality so operators don't conflate it with kerf *planning* decisions (Commander's-Intent).
- **Risk R1:** if N1 (F-class) is missed, a `decision_resolved` can be lost on crash → the agent waits forever. The reaper (K5) bounds the blast (the decision eventually orphan-withdraws), but the human answer is lost — N1 is load-bearing.
- **Risk R2:** an agent that idles bare (violates N5) silently never wakes AND gets keeper-reaped. The skill/handler-contract MUST carry the blocked-wait clause.
- **§9 policy flag — RESOLVED (operator-signed 2026-06-13).** v1 policy is **LOCKED**: a **single human answerer** with **first-writer-wins on `decision_id`** (the first `decision_resolved` for a given `decision_id` wins; later answers are no-ops per N3). **Multi-human arbitration is deferred (NG1)** — out of v1 scope. This was the only open gate; with it signed the work advances change-spec → integration → tasks. (Sign-off path: operator offline → captain-relayed per paul's mission; recorded here as a locked input.)

---
**Next kerf pass:** integration (§6 SPEC.md consolidation) then tasks — only on captain/operator sign-off of this draft. The spec is implementation-ready for paul's lane; nothing here is built by hitl-decisions itself.
