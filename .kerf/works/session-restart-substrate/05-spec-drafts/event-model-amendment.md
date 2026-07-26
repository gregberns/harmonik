# Amendment to `specs/event-model.md` (prefix EV) — session-keeper interior events + §8 drift reconciliation

> **Pass 5 (Spec-draft), events component — session-restart-substrate.** This is an
> **AMENDMENT** an implementer applies to the live 1622-line normative spec
> `specs/event-model.md`; it is NOT a rewrite. It gives the exact new/changed sections, the
> four new §8.20 rows with verbatim canonical payloads, and five new `EV-NNN` requirements that
> continue the live sequence. Grounded in `00-decisions.md` (D6/D7/D8/D9), the authoritative
> payload pins in `00b-review-resolutions.md` (R1+R2), `events-design.md`, and `02-components.md`
> (EV-U1..U5). Every RFC 2119 keyword is normative; every code anchor is `file:line` against
> HEAD `5160326b`.

---

## 1. Header — what this amends

- **Target spec:** `specs/event-model.md`, `requirement-prefix: EV`, `spec-shape: taxonomy-first`.
- **Version bump:** `version: 0.6.4` → **`0.7.0`** (a new §8 taxonomy cohort + five new top-level
  `EV-NNN` requirements — a minor-version substantive addition on the scale of the 0.5.0
  external-queue cohort, larger than the 0.6.x single-note patches). `status` stays `reviewed`.
- **Requirement-ID sequence:** the highest ID currently in the live file is **`EV-045`**
  (§8.14 hitl-decisions `decision_id` keying). **Next free number: `EV-046`.** This amendment
  assigns **`EV-046`, `EV-047`, `EV-048`, `EV-049`, `EV-050`** (see §4). No prior ID is
  renumbered or retired; no prior §8 row is renumbered.

**One-paragraph summary.** The session-restart-substrate change makes the keeper's restart cycle
observable and machine-checkable. This amendment (a) closes a real §8-number collision — 18
`session_keeper_*` types + four code-invented cohorts squat spec section numbers the spec assigns
to other families — by adding real sections **§8.16–§8.19** and correcting the code-comment
citations (EV-U5, the blocking prerequisite); (b) registers **four new O-class interior events**
(`session_keeper_handoff_written` / `session_keeper_model_done` / `session_keeper_clear_sent` /
`session_keeper_new_session_up`) as a fresh cohort at **§8.20**, each joinable by a required
payload `cycle_id` (EV-U1/U1a); and (c) makes the previously-dead typed-decode registry the
sanctioned decode/assert layer for a new offline replay-invariant-checking consumer, declares
`ScanAfter` its normative read surface, and records the keeper cohort-guard carve-out
(EV-046..EV-050 / EV-U2/U3/U4). The `§8.20` cohort satisfies every §8.9 criterion under an argued
§8.9(b) cycle-interior exception (§3.3).

---

## 2. §8-numbering reconciliation (EV-U5) — the exact spec edits

**Load-bearing ordering (D8):** this reconciliation MUST land **before** the §8.20 additive
registration (§3). Registering four events into a catalog whose §8.13–8.19 numbering is
self-inconsistent would bake the collision in.

### 2.1 The collision (code vs spec)

`event-model.md` §8 headings end at **§8.15**. Its §8.13/§8.14/§8.15 are **correct and MUST NOT
move**: §8.13 Epic-completion (`epic_completed`), §8.14 HITL-decisions
(`decision_needed`/`decision_resolved`/`decision_withdrawn`), §8.15 Bead-ledger merge. Code has
squatted these numbers and invented §8.16–8.19 never written into the spec:

| Code cohort | Wrong §-comment in code | Registration / const site |
|---|---|---|
| Session-keeper watcher/lifecycle (18 types) | **§8.13** — collides with spec Epic-completion | `eventtype.go:932`, `eventreg_hqwn59.go:478-489`, `keeperevents.go:3`, `pertypecompat_hqwn38.go:266,285` |
| Alarm / self-check (`review_gate_anomaly`) | **§8.14** — collides with spec HITL | `eventtype.go:1105`, `eventreg_hqwn59.go:523-529`, `pertypecompat_hqwn38.go:301` |
| HITL-decisions registration | **§8.15** — collides with spec Bead-ledger | `eventtype.go:1221`, `eventreg_hqwn59.go:537-552` |
| Remote-substrate workers (`worker_offline`, …) | **§8.16** — unspecced | `eventtype.go:1148,1164` |
| Flywheel governor / G-liveness halt | **§8.17 / §8.18** — unspecced | `eventtype.go:1261,1340` |
| Stall-sentinel Layer A (`stall_detected`) | **§8.19** — unspecced | `eventtype.go:1363`, `eventreg_hqwn59.go:532`, `pertypecompat_hqwn38.go:331` |

### 2.2 New spec sections to ADD (at the first free numbers)

Add these sections after §8.15, each carrying the §8.9-shaped catalog content the taxonomy uses
elsewhere (a `| # | Type | Dur | Emitter | Typical consumers | Payload fields |` table + a
`Section Axes` note). For §8.16–§8.19 this is **documentation of already-shipped types**
(transcribe the field lists from the existing code doc-comments; no behavior changes); for §8.20 it
is the new contract in §3.

| NEW spec §-heading | Cohort it homes | Load-bearing here? |
|---|---|---|
| **§8.16 Session-keeper watcher & cycle lifecycle** | the 18 existing `session_keeper_*` watcher/lifecycle types (renumbered OUT of the §8.13 collision) | **YES** — the §8.20 events reference this family; the §8.13 mis-cite MUST be fixed |
| **§8.17 Alarm / self-check** | `review_gate_anomaly` cohort | drift-closure (D8) |
| **§8.18 Remote-substrate workers** | `worker_offline` + siblings | drift-closure |
| **§8.19 Flywheel governor / liveness-halt / stall-sentinel** | flywheel observe/halt + `stall_detected` | drift-closure |
| **§8.20 Session-keeper interior cycle events** | **the 4 NEW events (§3 below)** | **YES — the target cohort** |

The keeper family thus splits into two non-adjacent spec cohorts **by design**: **§8.16** = the
coarse watcher/lifecycle signals shipped today; **§8.20** = the fine-grained interior milestones
this change adds. This is exactly what D8 pins; the non-adjacency is preferable to renumbering the
whole §8 tail.

> **Do not enumerate all 18 §8.16 payloads here.** §8.16 documents an already-registered cohort;
> the implementer transcribes the field lists from `eventreg_hqwn59.go:478-489` /
> `eventtype.go:935-1081` doc-comments into the §8.16 table 1:1. The registration site is
> `registerKeeperEvents()` (`eventreg_hqwn59.go`), unchanged. This amendment's normative new
> contract is entirely in §8.20.

### 2.3 The code-comment citation fixes (pure comment edits, no code motion)

These correct the citations so the catalog is no longer a lie. There is no test asserting
comment↔spec agreement, so they cannot break the build — but they ARE the point of EV-U5:

- `eventtype.go:932` banner `§8.13 Session-keeper` → **§8.16**; sub-refs `§8.13.1..§8.13.8` → `§8.16.x`.
- `eventreg_hqwn59.go:478-489` (`registerKeeperEvents` doc block, all `§8.13.*`) → **§8.16.***.
- `keeperevents.go:3` header (`§8.13 session-keeper`) → **§8.16**; per-struct `(event-model.md §8.13.n)` → `§8.16.n`.
- `pertypecompat_hqwn38.go:266,285` keeper banners `§8.13` → **§8.16**.
- `eventtype.go:1105` alarm `§8.14` → **§8.17**; `eventreg_hqwn59.go:523-529`, `pertypecompat_hqwn38.go:301` likewise.
- `eventtype.go:1221` / `eventreg_hqwn59.go:537-552` HITL `§8.15` → **§8.14** (citation-correct — spec's real HITL is §8.14).
- `eventtype.go:1148,1164` remote `§8.16` → **§8.18**.
- `eventtype.go:1261,1340,1363`, `eventreg_hqwn59.go:532`, `pertypecompat_hqwn38.go:331` flywheel/stall `§8.17/8.18/8.19` → **§8.19**.

> **Bonus one-line doc fix (findings §5.8):** `pertypecompat_hqwn38.go:26` cites a phantom
> `SetPayloadSchemaVersion`; the real version mechanism is `RegisterEventTypeAtVersion`
> (`eventregistry.go:114`). Correct that comment in the same sweep so no one follows a
> nonexistent API. (Not §8-numbering, but it sits in a file this amendment edits.)

---

## 3. §8.20 — the new interior-event cohort (EV-U1 / EV-U1a)

### 3.1 The four §8.20 rows

Add **§8.20 Session-keeper interior cycle events**. All four are durability **class O**
(observational), emitter **internal/keeper** (the existing `Emitter`/`FileEmitter` path,
`watcher.go:57-99`), consumed by the **internal/replay** invariant harness (EV-048). Names follow
EV-U1a's `session_keeper_*` catalog convention (all 18 existing keeper types use it) — **not** the
`keeper_*` prose shorthand.

| # | Type | Dur | Emitter | Typical consumers | Payload fields |
|---|---|---|---|---|---|
| 8.20.1 | `session_keeper_handoff_written` | O | internal/keeper | replay-invariant-harness, audit, observability | `agent_name`, `cycle_id` (REQUIRED), `session_id?`, `nonce?`, `recovered?`, `handoff_mtime?` |
| 8.20.2 | `session_keeper_model_done` | O | internal/keeper | replay-invariant-harness, audit, observability | `agent_name`, `cycle_id` (REQUIRED), `session_id?`, `source` (REQUIRED: `idle_marker`\|`transcript_turn`\|`timeout`), `degraded?` |
| 8.20.3 | `session_keeper_clear_sent` | O | internal/keeper | replay-invariant-harness, audit, observability | `agent_name`, `cycle_id` (REQUIRED), `session_id?`, `attempt` (1-based) |
| 8.20.4 | `session_keeper_new_session_up` | O | internal/keeper | replay-invariant-harness, audit, observability | `agent_name`, `cycle_id` (REQUIRED), `prev_session_id` (REQUIRED), `new_session_id` (REQUIRED, ≠ `prev_session_id`) |

**Emission ordering (§8.20).** Within one restart cycle (keyed by the composite
`(agent_name, cycle_id)` — see EV-046) the emission order MUST be:
`session_keeper_handoff_written` → `session_keeper_model_done` → `session_keeper_clear_sent` →
`session_keeper_new_session_up`. `session_keeper_clear_sent` MUST NOT be emitted before both
`session_keeper_handoff_written` (SR3) and `session_keeper_model_done` (SR4) for the same cycle.
The full ordering/liveness semantics (SR3/SR4/SR6/SR7/SR9) are owned by `specs/session-keeper.md`
(SK); this spec owns the type registration, payload shape, and durability class.

**Section Axes (§8.20 Session-keeper interior cycle events):** all four entries are
mechanism-tagged, **class O** (ordinary — observational; loss does not gate the restart cycle,
whose crash-recovery need is met by the retained keeper journal `RecoverFromCrash`, D10). Default
per-entry Axes: `llm-freedom=none; io-determinism=best-effort; replay-safety=safe;
idempotency=non-idempotent`.

> **§8.20 durability row (D9 — O-class, explicit not implicit).** These four are class O for this
> phase, NOT class F. F-class would require routing keeper emits through the daemon or adding
> `Sync()` to `FileEmitter` (`watcher.go:86-99` opens/appends/closes with no fsync) — larger blast
> radius, out of scope (Constraint 1: no daemon in execution). F-class is deferred; revisit if a
> later phase routes keeper through the daemon. **Required hardening (D9.1):** unlike the 14
> existing keeper `_ = emit(...)` sites, the four §8.20 emit helpers MUST NOT silently swallow a
> marshal or emit error — they MUST log on failure (both the `json.Marshal` error and the
> `EmitWithRunID` error), because "a durable event that silently fails to write" is a spec lie.
> This hardening is scoped to the four new helpers ONLY; the 14 existing `_ =` emits are out of
> scope.

### 3.2 The four canonical payload structs — VERBATIM from `00b` R1+R2 (AUTHORITATIVE)

Declared in `internal/core/keeperevents.go`; land in the amendment's §6.3 additions. Each struct
carries `AgentName` + **required** `CycleID` (json `cycle_id`, **no** `omitempty`) with a `Valid()`
method asserting non-empty `cycle_id` (precedent: `ReconciliationStartedPayload.Valid()`,
`reconciliationevents_hqwn59.go:94-102`). These definitions are canonical and supersede any
divergent restatement in the component design docs.

```go
// §8.20.1
type SessionKeeperHandoffWrittenPayload struct {
    AgentName    string `json:"agent_name"`
    CycleID      string `json:"cycle_id"`                // REQUIRED
    SessionID    string `json:"session_id,omitempty"`
    Nonce        string `json:"nonce,omitempty"`         // confirmed nonce marker (audit)
    Recovered    bool   `json:"recovered,omitempty"`     // true iff accepted via freshness recovery (not nonce)
    HandoffMtime string `json:"handoff_mtime,omitempty"` // RFC3339; carried on the recovery edge
}
// §8.20.2
type SessionKeeperModelDonePayload struct {
    AgentName string `json:"agent_name"`
    CycleID   string `json:"cycle_id"`                   // REQUIRED
    SessionID string `json:"session_id,omitempty"`
    Source    string `json:"source"`                     // REQUIRED: "idle_marker" | "transcript_turn" | "timeout"
    Degraded  bool   `json:"degraded,omitempty"`         // true iff reached via model_done_timeout
}
// §8.20.3
type SessionKeeperClearSentPayload struct {
    AgentName string `json:"agent_name"`
    CycleID   string `json:"cycle_id"`                   // REQUIRED
    SessionID string `json:"session_id,omitempty"`
    Attempt   int    `json:"attempt"`                    // 1-based; increments on defensive re-injects
}
// §8.20.4
type SessionKeeperNewSessionUpPayload struct {
    AgentName     string `json:"agent_name"`
    CycleID       string `json:"cycle_id"`               // REQUIRED
    PrevSessionID string `json:"prev_session_id"`        // REQUIRED (needed for the != check)
    NewSessionID  string `json:"new_session_id"`         // REQUIRED; Valid(): non-empty AND != PrevSessionID
}
```

`Valid()` rules (value receiver, matching the reconciliation precedent): all four require non-empty
`AgentName` and `CycleID`; `clear_sent` additionally requires `Attempt >= 1`; `new_session_up`
additionally requires non-empty `NewSessionID` and `NewSessionID != PrevSessionID`. The `^cyc-`
prefix is a **soft** check kept OUT of `Valid()` (a future `CycleIDGen` change MUST NOT
retro-invalidate historical corpora); the harness reports a non-conforming id as a low-severity
finding rather than dropping the event. `Valid()` is exercised by the roundtrip/prop tests and the
harness explicitly — it is NOT wired into `DecodePayload` (`EventPayload` is an empty marker
interface, `eventregistry.go:27`).

### 3.3 §8.9 acceptance-criteria justification (the cycle-interior exception — MUST be recorded)

§8.9 admits a candidate type iff **all** of (a)–(h). The four events are **cycle-interior**, which
reads against §8.9(b) ("a lifecycle-boundary signal rather than an intra-lifecycle detail"). Per
events-design §7.1, the exception is argued and RECORDED inline in the §8.20 section (the same way
§8.12/§8.14 carry their §8.9 compliance evidence):

- **§8.9(a) — SATISFIED.** A real cross-subsystem consumer exists: the **internal/replay**
  invariant-checking harness (EV-048). The events cross a genuine **process boundary**
  (keeper process → `events.jsonl` → the replay package), which is also why they are NOT
  EV-026-internal (see the box below).
- **§8.9(c) — SATISFIED (granularity).** The harness *requires per-boundary access*: SR3/SR4/SR6
  are orderings *between* these interior milestones — a single summary event cannot express
  "`clear_sent` after `model_done`" or "`complete` only after `new_session_up`." Per-boundary
  granularity is the criterion working exactly as intended, not a violation.
- **§8.9(b) — REFRAMED and satisfied.** These four ARE the lifecycle boundaries **of the restart
  state machine's sub-lifecycle** (handoff-write / model-done / clear / new-session are its
  transition points, D11) — not incidental intra-lifecycle chatter. The deferred
  `tool_command_completed` candidate (§8.9 note) failed because it had *no* consumer and *no*
  load-bearing invariant; here the consumer is named and SR3/SR4/SR6/SR7/SR9 are load-bearing.
- **§8.9(d)–(f) — SATISFIED.** Typed Go payloads in §6.3 (§3.2); four-axis + mechanism tags and
  durability class O per the §8.20 Section Axes note; replay side-effect classification `safe`
  per §4.5.
- **§8.9(g) — SATISFIED.** `specs/session-keeper.md` (SK-R4) cites all four by their registered
  names as its ordering-invariant subjects.
- **§8.9(h) — DOES NOT force a merge.** The four are four distinct single-emission milestones, not
  a paired pending/resolved lifecycle; the "use one type with a `status` field" rule does not
  apply.

> **EV-026 is NOT the escape hatch.** EV-026-internal events "MUST NOT be passed to
> `RegisterEventType`" (`eventregistry.go:21-26`), which would kill the EV-048 typed-decode
> adoption outright. Because the four events cross the keeper→jsonl→harness **process** boundary,
> they are cross-bus, NOT EV-026-internal — so they MUST be registered per EV-027, which is also
> exactly what makes the typed-decode harness (EV-048) work.

---

## 4. New EV requirements (continuing the live sequence from EV-046)

Add these under §4.5 (Replay semantics) / §4.7 (Schema versioning) as noted per requirement.
Highest prior ID found: **EV-045**. Next free: **EV-046**. Assigned here: **EV-046..EV-050**.

### 4.1 EV-046 — Cycle-scoped keeper events MUST join on a required payload `cycle_id`, never a zero envelope `run_id`

> Add to §4.6 (Producer / consumer contract), after EV-025. (EV-U2, D7.)

The four §8.20 session-keeper interior event types MUST carry their cycle correlation identity in a
**REQUIRED payload field `cycle_id`** (JSON `cycle_id`, no `omitempty`), NOT in the envelope
`run_id`. A `§8.20` event MUST NOT be emitted with an empty `cycle_id`; the payload `Valid()` method
MUST reject an empty `cycle_id`. The envelope `run_id` for these events MUST be absent
(`core.RunID{}` / `None`); a zero-valued (Nil-UUID) envelope `run_id` MUST NOT be written for a
cycle-scoped keeper event — this reuses the §6.1 "`run_id` present when scoped to a run" rule
(these events are cycle-scoped, not workflow-run-scoped) and keeps them out of the EM-013
workflow-run keyspace that live daemon folds (`daemon.go:2067`, `eventbus.Filter`
`jsonlwriter.go:380`) walk. Because `cycle_id` alone is not globally unique (`newCycleIDGen` resets
its sequence per process, `cycle.go:431-441`), the normative join key is the **composite
`(agent_name, cycle_id)`**; every §8.20 payload MUST carry both `agent_name` and `cycle_id` so the
composite is always available. Precedent: the §8.6 `reconciliation_run_id` payload-level run
identity (`ReconciliationStartedPayload`, `Valid()`-checked). Consumers MUST group cycle-scoped
keeper events by the composite, and MUST NOT attempt to correlate them via envelope `run_id`.

Tags: mechanism

> **Optional additive backfill (non-blocking, D7):** `SessionKeeperAckTimeoutPayload` MAY
> additively gain `CycleID string \`json:"cycle_id,omitempty"\`` (`omitempty` — a backfill onto an
> existing type; old records legitimately lack it). No schema-version bump (additive; covered by
> its `AdditiveOnly:true` compat row). Deferrable as a follow-up bead.

### 4.2 EV-047 — `ScanAfter` is the declared normative offline-replay read surface

> Add to §4.5 (Replay semantics), after EV-021. (EV-U4, D6/D9.)

`eventbus.ScanAfter(path string, sinceID core.EventID) iter.Seq[core.Event]`
(`jsonlwriter.go:312-353`) is the **declared read surface** for offline observational replay over
`events.jsonl`. It is promoted from its incidental EV-038 mention to a first-class primitive: it is
exported, already has 15+ production callers, and yields every envelope whose `event_id` is strictly
greater than `sinceID` in **file order**. An offline replay/invariant-checking consumer (EV-048)
MUST read the log through `ScanAfter` — passing `core.EventID{}` (zero) to scan from the beginning,
or a persisted watermark `EventID` for incremental re-runs — and MUST NOT reconstruct authoritative
state from the result (subject to EV-021). Because the keeper `FileEmitter`s and the daemon
`JSONLWriter` append to the **same** `events.jsonl` with **per-process** `EventIDGenerator`s,
`ScanAfter`'s file order is only an approximation of global `EventID` order across writers; a
consumer whose invariants depend on inter-event ordering MUST re-sort the collected events by
`EventID` after the scan drains before evaluating ordering invariants. `eventbus.Filter(path,
runID)` selects by envelope `run_id` and remains undeclared/unused (its deadness corroborates
EV-046: cycle events carry no envelope `run_id`).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 4.3 EV-048 — The typed-decode registry is the sanctioned decode/assert layer for an observational replay-invariant consumer

> Add to §4.5 (Replay semantics), after EV-047. (EV-U3, D6.)

The typed-decode registry path — `Event.DecodePayload` (`eventregistry.go:218-231`),
`core.ValidateEnvelopeSchemaVersion` (`eventregistry.go:165`), the `DispatchObservational` /
`DispatchSynchronous` dispatchers (`eventdispatch.go`), and the `pertypecompat` N-1 compat table
(`LookupPayloadCompatEntry`, `pertypecompat_hqwn38.go:373`) — is declared ADOPTED (not dead, not
deleted) as the decode-and-assert layer of a sanctioned **observational replay-invariant-checking
consumer** (the `internal/replay` harness). This consumer is the EV-033 "observational consumer" the
API was specced for and becomes the registry's first production reader. It MUST decode each scanned
envelope through the registry, and MUST treat a schema-version mismatch as a recorded **finding**
(writer/reader drift), consulting `LookupPayloadCompatEntry(ev.Type).CompatWindowHolds` to decide
whether N-1 replay is declared safe — never as a fatal error. In its default (tolerant) mode it MUST
apply the EV-033 unknown-type policy: an unknown `type` yields `ErrSkipUnknown` and is **counted and
skipped**, never fatal (a newer binary's types and pre-registry junk are tolerated). A type
registered but never observed in the corpus MUST be reported informationally, never as a violation
(precedent: the registered-but-no-op `session_keeper_operator_attached`, `cycle.go:1502`). This
consumer is observational per EV-021: its output is advisory and MUST NOT reconstruct authoritative
state.

Tags: mechanism

### 4.4 EV-049 — Additive `DecodePayloadStrict` variant surfaces additive writer drift

> Add to §4.7 (Schema versioning), after EV-028. (EV-U3, D6.)

The registry MUST provide an additive `DecodePayloadStrict` variant that decodes a payload exactly
as `DecodePayload` does but with `DisallowUnknownFields` set, so that an **additive field a newer
writer introduced** surfaces as a decode error instead of being silently ignored (`DecodePayload`
uses `json.Unmarshal` with no `DisallowUnknownFields`, `eventregistry.go:227`, and therefore cannot
see additive drift). The replay-invariant consumer's **strict mode** MUST route through
`DecodePayloadStrict` and treat an unknown payload field, or an unknown event `type`, as a hard
finding — strict mode is for replaying the harness's OWN freshly-recorded corpus, where such a
surprise means a `mustRegister` was forgotten or a writer drifted. The addition is purely additive:
it introduces no new obligation on existing writers and does not change `DecodePayload`'s tolerant
semantics, which remain the default for historical replay.

Tags: mechanism

### 4.5 EV-050 — The `session_keeper_*` cohorts are carved out of the cross-bus cohort/count guards

> Add to §4.6 (Producer / consumer contract), after EV-027. (EV-U1, D8.)

The `session_keeper_*` event families (the §8.16 watcher/lifecycle cohort AND the §8.20 interior
cohort) are **registered and compat-tabled** (each has a `mustRegister` constructor and a
`PayloadCompatEntry` row) but are **excluded from the cross-bus taxonomy cohort guard**
(`allEventTypeCohort` in `eventtype_coverage_gjyks_test.go`) and from the EV-027 amendment count
guard (`ev027_amendment_guard_hqwn36_test.go` `wantCount`). This carve-out follows the existing
18-type keeper precedent (today those 18 are already absent from the cohort, silently) and is now
stated normatively so future keeper additions know the rule: a new `session_keeper_*` type MUST get
a `mustRegister` constructor and a mandatory `pertypecompat` row, but MUST NOT be added to
`allEventTypeCohort` or to the EV-027 `wantCount`. The `eventtype_coverage_gjyks_test.go`
doc-comment — which currently *states* "every `EventType` constant MUST also be appended to
`allEventTypeCohort`", a contract 18 keeper types already violate — MUST be amended to name this
carve-out explicitly (a doc-comment edit, not a test-logic change), so the stated contract stops
being false.

Tags: mechanism

---

## 5. Cross-references

- **`specs/session-keeper.md` (SK):** SK-R4 owns the §8.20 events' **ordering semantics**
  (SR3/SR4/SR6/SR7/SR9); this spec (EV) owns their **registration, payload shape, and durability
  class**. The §8.20 "Emission ordering" note points to SK; SK cites the four by their EV-registered
  names and cites EV-046 for the `(agent_name, cycle_id)` join key. Mutually discoverable.
- **`specs/replay-substrate.md` (RS):** the RS record→replay + measurement clauses (RS-020, and the
  ReplayCodec contract) **consume** EV-047 (`ScanAfter` read surface) and EV-048/EV-049 (typed-decode
  + strict variant); RS's two-layer decode discipline (RS-014) is enforced writer-side by
  `DecodePayloadStrict` (EV-049) and reader-side by the `ReplayCodec.DecodeLine` skip-vs-fatal split.
  RS cross-references EV-047/048/049; EV's §4.5–4.7 clauses point back at RS as the harness home.
- **§6.1 envelope:** EV-046's zero-`run_id` prohibition is the concrete application of the §6.1
  "`run_id` present when scoped to a run" rule to cycle-scoped (non-run) keeper events.

---

## 6. Conformance additions

- **Mandatory `pertypecompat` rows.** The four §8.20 types MUST each have a `PayloadCompatEntry`
  (`{CurrentVersion:1, PreviousVersion:0, CompatWindowHolds:true, AdditiveOnly:true}`) under a new
  `§8.20` banner in `allPayloadCompatEntries`. `pertypecompat_hqwn38_test.go:58-91` enforces both
  directions (registered ⇒ entry AND entry ⇒ registered) and will red-fail a half-landing — the
  `mustRegister` calls and the compat rows MUST land in the **same commit**.
- **Roundtrip tests.** Extend `keeperevents_roundtrip_test.go` with a marshal →`DecodePayload`
  →`DeepEqual` case per new type, plus `DecodePayloadStrict` roundtrip coverage that asserts a
  well-formed payload decodes clean AND an injected unknown field is rejected in strict mode.
- **`Valid()` prop tests.** Mirror `reconciliationevents_hqwn59_prop_test.go`: `Valid()` true on a
  well-formed value; false on empty `CycleID`; false on `new_session_up` with
  `NewSessionID == PrevSessionID`; false on `clear_sent` with `Attempt == 0`.
- **Cohort/count guards unchanged.** Add NOTHING to `allEventTypeCohort` or the EV-027 `wantCount`
  (EV-050); amend only the gjyks doc-comment to name the carve-out.
- **EV-036 secret-prefix compliance.** The introduced field names (`agent_name, cycle_id,
  session_id, nonce, degraded, source, attempt, prev_session_id, new_session_id`) MUST pass
  `ScanRegisteredPayloadsForSecretFields` (`eventregistry.go:298`); none match the EV-036 regex
  (`nonce` is a handoff nonce, not a secret token). Confirmed compliant.
- **§8.9(b) justification recorded.** The cycle-interior §8.9(b) exception argument (§3.3) MUST be
  written into the §8.20 section body as recorded compliance evidence (as §8.12/§8.14 do), so the
  catalog carries its own justification.

---

## 7. Spec-edits checklist (apply to `event-model.md` in this order)

1. **Front matter:** `version: 0.6.4` → `0.7.0`. (Leave `status: reviewed`.)
2. **§8 (EV-U5 first):** ADD sections **§8.16** (transcribe the 18 existing `session_keeper_*`
   watcher/lifecycle types from code doc-comments), **§8.17** (alarm/self-check), **§8.18**
   (remote-substrate workers), **§8.19** (flywheel/liveness-halt/stall-sentinel) — each with a
   taxonomy table + Section Axes note. Do NOT move existing §8.13/§8.14/§8.15.
3. **§8.20:** ADD the four-row **§8.20 Session-keeper interior cycle events** table (§3.1), the
   §8.20 Emission-ordering note, the Section Axes note, the O-class/D9-hardening durability note,
   and the recorded §8.9(b) cycle-interior justification (§3.3).
4. **§6.3:** ADD the four payload schemas (the §3.2 structs, in the §6.3 YAML form used for other
   payloads), after the existing keeper payload block.
5. **§4.5:** ADD **EV-047** (after EV-021) and **EV-048** (after EV-047).
6. **§4.6:** ADD **EV-046** (after EV-025) and **EV-050** (after EV-027).
7. **§4.7:** ADD **EV-049** (after EV-028).
8. **Changelog:** ADD one row — `| 2026-07-13 | 0.7.0 | agent (codename:session-restart-substrate)
   | …§8.16–8.19 drift reconciliation + §8.20 four O-class interior events + EV-046..EV-050… |`
   (mirror the 0.5.0/0.6.2 cohort-row style; enumerate the new §8 sections, the four new types, the
   five new IDs, and "No prior IDs renumbered or retired").
9. **Code-comment sweep (EV-U5, separate from the spec file):** apply the §2.3 citation fixes in
   `eventtype.go` / `eventreg_hqwn59.go` / `keeperevents.go` / `pertypecompat_hqwn38.go`, plus the
   `pertypecompat_hqwn38.go:26` phantom-API doc fix. Amend the
   `eventtype_coverage_gjyks_test.go` doc-comment to name the keeper carve-out (EV-050).
