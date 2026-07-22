# 04-Design — events component — session-restart-substrate

> **Pass 4 (Change Design), events component.** Elaborates D6/D7/D8/D9 within the pins in
> `00-decisions.md`; grounded in `03-research/events/findings.md` and `02-components.md` (EV-U1..U5).
> This is the design a spec-drafter and an implementer follow. It is NOT spec text. Every code
> anchor is `file:line` against HEAD 5160326b. Nothing here reverses a pinned decision.

Package touched: `internal/core` (registry, payloads), `internal/keeper` (emit sites), NEW
`internal/replay` (harness), `specs/event-model.md` (EV amendment). Requirement IDs stay EV-* per
02-components.

---

## 0. The three-part shape of the EV change, and its internal ordering

The EV update is three landings, and **the order is load-bearing**:

1. **EV-U5 — §8 drift reconciliation (spec + code-comment fix).** Amends `specs/event-model.md`
   and corrects the code citations so the catalog is internally consistent. Adds NO new registrations.
2. **EV-U1/U1a/U2 — additive registration** of the four new `session_keeper_*` interior events at
   the reconciled §8.20, each with the required `cycle_id`.
3. **EV-U3/U4 — the `internal/replay` harness** (typed-decode adoption + `ScanAfter` read surface).

**Ordering rule (pinned, D8):** EV-U5 lands **before** EV-U1. Registering the four events into a
catalog whose §8.13–8.19 numbering is self-inconsistent would bake the collision in; the four
events must land in a clean, renumbered catalog. EV-U3/U4 (the harness) can be authored in parallel
but **compiles against** the registrations, so it merges last. Within EV-U1, the `PayloadCompatEntry`
rows and the `mustRegister` calls must land in the **same commit** — `pertypecompat_hqwn38_test.go:58-91`
enforces both directions and will red-fail a half-landing.

---

## 1. §8 drift reconciliation (EV-U5, D8) — the exact plan

### 1.1 The collision, enumerated (code vs spec)

`specs/event-model.md` §8 headings end at **§8.15** (`grep '^### 8\.'` → last is `:392`). The three
top sections that code collides with are **correct in the spec** and must NOT move:

| Spec heading (authoritative) | Spec line |
|---|---|
| `### 8.13 Epic-completion lifecycle` (`epic_completed`) | `event-model.md:354` |
| `### 8.14 HITL-decisions lifecycle` | `event-model.md:368` |
| `### 8.15 Bead-ledger merge lifecycle` | `event-model.md:392` |

Code has squatted these numbers and invented §8.16–8.19 that were never written into the spec:

| Code cohort | Code §-comment (wrong/unspecced) | Anchors |
|---|---|---|
| Session-keeper (18 types) | **§8.13** — collides with spec Epic-completion | `eventtype.go:932`, `eventreg_hqwn59.go:478-489`, `keeperevents.go:3`, `pertypecompat_hqwn38.go:266` |
| Alarm / self-check (`review_gate_anomaly`) | **§8.14** — collides with spec HITL | `eventtype.go:1105`, `eventreg_hqwn59.go:523-529`, `pertypecompat_hqwn38.go:301` |
| HITL-decisions (`registerHITLDecisionEvents`) | **§8.15** — collides with spec Bead-ledger; spec's real HITL is §8.14 | `eventtype.go:1221`, `eventreg_hqwn59.go:537-552` |
| Remote-substrate workers (`worker_offline`, …) | **§8.16** — unspecced | `eventtype.go:1148,1164` |
| Flywheel governor observe | **§8.17** — unspecced | `eventtype.go:1261` |
| Flywheel G-liveness halt | **§8.18** — unspecced | `eventtype.go:1340` |
| Stall-sentinel Layer A (`stall_detected`) | **§8.19** — unspecced | `eventtype.go:1363`, `eventreg_hqwn59.go:532`, `pertypecompat_hqwn38.go:331` |

Note the code is internally inconsistent too: `eventtype.go:892` correctly calls spec §8.14 the
"HITL-decisions family," while `eventtype.go:1221` labels the HITL registration "§8.15."

### 1.2 The spec amendment — new sections at the next-free numbers

The spec's §8.13/8.14/8.15 stay. The reconciliation **adds** real sections starting at the first
free number (§8.16) and the code citations are corrected to match. Pinned allocation:

| NEW spec §-heading to add | Cohort it homes | Load-bearing for THIS change? |
|---|---|---|
| **§8.16 Session-keeper watcher & cycle lifecycle** | the 18 existing `session_keeper_*` types (§1.2, findings) | **YES** — the 4 new events reference this family; the §8.13 mis-cite must be fixed |
| **§8.17 Alarm / self-check** | `review_gate_anomaly` | drift-closure (D8 mandate) |
| **§8.18 Remote-substrate workers** | `worker_offline` + siblings | drift-closure |
| **§8.19 Flywheel governor / liveness-halt / stall-sentinel** | flywheel + `stall_detected` cohorts | drift-closure |
| **§8.20 Session-keeper interior cycle events** | **the 4 NEW events (EV-U1)** | **YES — the target cohort** |

The keeper family thus splits into two non-adjacent spec cohorts by design: **§8.16** = the coarse
watcher/lifecycle signals that exist today; **§8.20** = the fine-grained interior milestones this
change adds. That is exactly what D8 pins ("keeper renumbered out of §8.13; the four interior events
land at §8.20"); the non-adjacency is acceptable and preferable to renumbering the whole tail.

HITL is a citation-only fix, no new section: code `eventtype.go:1221` / `eventreg_hqwn59.go:537-552`
should cite **§8.14** (the spec's real HITL-decisions section), not §8.15. Fold this into the
same comment sweep.

Each new spec section must carry the §8.9-shaped row content the catalog uses elsewhere (type,
durability class, emitter, consumers, payload fields) — for §8.16–8.19 this is documentation of
already-shipped types (transcribe from the existing doc-comments), for §8.20 it is the new contract
(§2 below).

### 1.3 The code-comment fixes (no behavior change)

Pure comment/citation edits, no code motion:

- `eventtype.go:932` banner `§8.13 Session-keeper` → **§8.16**; sub-refs `§8.13.1..§8.13.8` → `§8.16.x`.
- `eventreg_hqwn59.go:478-489` doc block (`registerKeeperEvents`, all `§8.13.*`) → **§8.16.***.
- `keeperevents.go:3` header (`§8.13 session-keeper`) → **§8.16**; per-struct doc `(event-model.md §8.13.n)` → `§8.16.n`.
- `pertypecompat_hqwn38.go:266,285` keeper banners `§8.13` → **§8.16**.
- `eventtype.go:1105` alarm `§8.14` → **§8.17**; `eventreg_hqwn59.go:523-529`, `pertypecompat_hqwn38.go:301` likewise.
- `eventtype.go:1221` / `eventreg_hqwn59.go:537-552` HITL `§8.15` → **§8.14** (citation-correct).
- `eventtype.go:1148/1164` remote `§8.16` → **§8.18**.
- `eventtype.go:1261/1340/1363`, `eventreg_hqwn59.go:532`, `pertypecompat_hqwn38.go:331` flywheel/stall `§8.17/8.18/8.19` → **§8.19**.

These are §-string edits; there is no test asserting comment↔spec agreement, so they cannot break
the build — but they are the whole point of EV-U5 (the catalog is a lie until they land). The four
new events' doc-comments (§2) are written correct-from-birth at §8.20.

---

## 2. The four new events — registration design (EV-U1, EV-U1a)

Names per EV-U1a (`session_keeper_*`, not the `keeper_*` prose shorthand):
`session_keeper_handoff_written`, `session_keeper_model_done`, `session_keeper_clear_sent`,
`session_keeper_new_session_up`. None exist today (grep: 0 hits).

Each event is the four coupled artifacts (findings §1.1) plus a roundtrip/prop test. All schema
**version 1** (default `mustRegister` → `RegisterEventTypeAtVersion(…,1)`, `eventregistry.go:100-132`).

### 2.1 EventType constants — `internal/core/eventtype.go`

Append a **§8.20 block** after the §8.19 block (currently ends `eventtype.go:1385`), mirroring the
existing keeper doc-comment shape (`eventtype.go:935-1081`): each const carries emitter + durability
class **O** + bead/spec ref.

```go
// §8.20 Session-keeper interior cycle events (codename:session-restart-substrate)
//
// Fine-grained restart-cycle milestones, durable on the bus and joinable by
// payload.cycle_id (EV-U2). Emitted by internal/keeper; consumed by the
// internal/replay invariant harness (EV-U3). Durability class: O.

// EventTypeSessionKeeperHandoffWritten — the handoff nonce was confirmed
// written by the model (cycle.go confirmed-phase). Precondition of clear_sent
// (SR3). (§8.20.1)
EventTypeSessionKeeperHandoffWritten EventType = "session_keeper_handoff_written"
// EventTypeSessionKeeperModelDone — the model reached an await-input boundary
// after the handoff turn (D12); Degraded=true if the liveness bound fired.
// Precondition of clear_sent (SR4). (§8.20.2)
EventTypeSessionKeeperModelDone EventType = "session_keeper_model_done"
// EventTypeSessionKeeperClearSent — /clear was injected into the pane
// (cycle.go cleared-phase). (§8.20.3)
EventTypeSessionKeeperClearSent EventType = "session_keeper_clear_sent"
// EventTypeSessionKeeperNewSessionUp — a new session_id was observed after
// /clear; precondition of briefing (SR6). (§8.20.4)
EventTypeSessionKeeperNewSessionUp EventType = "session_keeper_new_session_up"
```

### 2.2 Payload structs — `internal/core/keeperevents.go`

Follow `SessionKeeperHandoffStartedPayload` (`keeperevents.go:58-65`) for the common
`AgentName`/`CycleID`/`SessionID` trio; add each event's extra fields per the task. **`CycleID` is
required** (json `cycle_id`, NO omitempty) with a `Valid()` method — precedent is
`ReconciliationStartedPayload.Valid()` (`reconciliationevents_hqwn59.go:94-102`).

```go
// SessionKeeperHandoffWrittenPayload — session_keeper_handoff_written (§8.20.1).
// Durability class: O.
type SessionKeeperHandoffWrittenPayload struct {
    AgentName    string `json:"agent_name"`
    CycleID      string `json:"cycle_id"`               // REQUIRED
    SessionID    string `json:"session_id,omitempty"`
    Nonce        string `json:"nonce,omitempty"`         // confirmed nonce marker (audit)
    Recovered    bool   `json:"recovered,omitempty"`     // true iff accepted via freshness recovery (not nonce)
    HandoffMtime string `json:"handoff_mtime,omitempty"` // RFC3339; carried on the recovery edge
}

// SessionKeeperModelDonePayload — session_keeper_model_done (§8.20.2).
type SessionKeeperModelDonePayload struct {
    AgentName string `json:"agent_name"`
    CycleID   string `json:"cycle_id"`             // REQUIRED
    SessionID string `json:"session_id,omitempty"`
    Source    string `json:"source"`               // REQUIRED: "idle_marker" | "transcript_turn" | "timeout"
    Degraded  bool   `json:"degraded,omitempty"`   // true iff reached via model_done_timeout
}

// SessionKeeperClearSentPayload — session_keeper_clear_sent (§8.20.3).
type SessionKeeperClearSentPayload struct {
    AgentName string `json:"agent_name"`
    CycleID   string `json:"cycle_id"`             // REQUIRED
    SessionID string `json:"session_id,omitempty"` // session_id before /clear
    Attempt   int    `json:"attempt"`              // 1-based /clear attempt in this cycle
}

// SessionKeeperNewSessionUpPayload — session_keeper_new_session_up (§8.20.4).
type SessionKeeperNewSessionUpPayload struct {
    AgentName     string `json:"agent_name"`
    CycleID       string `json:"cycle_id"`         // REQUIRED
    PrevSessionID string `json:"prev_session_id"`   // REQUIRED (needed for the != check)
    NewSessionID  string `json:"new_session_id"`   // REQUIRED, must differ from prev
}
```

`Valid()` methods (value receiver, matching the reconciliation precedent). Base rule: non-empty
`AgentName` and `CycleID`; optionally assert `strings.HasPrefix(CycleID, "cyc-")` (see §3.2 for the
"optional pattern" stance). `new_session_up` additionally requires non-empty `NewSessionID` and
`NewSessionID != PrevSessionID`:

```go
func (p SessionKeeperHandoffWrittenPayload) Valid() bool { return validCycleScope(p.AgentName, p.CycleID) }
func (p SessionKeeperModelDonePayload)      Valid() bool { return validCycleScope(p.AgentName, p.CycleID) }
func (p SessionKeeperClearSentPayload)      Valid() bool { return validCycleScope(p.AgentName, p.CycleID) && p.Attempt >= 1 }
func (p SessionKeeperNewSessionUpPayload)   Valid() bool {
    return validCycleScope(p.AgentName, p.CycleID) && p.NewSessionID != "" && p.NewSessionID != p.PrevSessionID
}

// validCycleScope is the shared keeper-interior precondition.
func validCycleScope(agent, cycleID string) bool {
    return agent != "" && cycleID != "" // pattern check (^cyc-) is a soft assertion; see design §3.2
}
```

**Note — `Valid()` is not wired into decode.** `EventPayload` is an empty marker interface
(`eventregistry.go:27`); `DecodePayload` does NOT call `Valid()`. So `Valid()` is exercised by (a)
the roundtrip/prop tests and (b) the replay harness explicitly (§4.4) — never automatically. This
matches how reconciliation `Valid()` is used today (only in `reconciliationevents_hqwn59_prop_test.go`).
Do not assume the registry validates payloads.

### 2.3 `mustRegister` — `internal/core/eventreg_hqwn59.go`

Do NOT extend `registerKeeperEvents` (that stays the §8.16 family). Add a new
`registerKeeperInteriorEvents()` and call it from `init()` (`:28`), keeping the §8.16 vs §8.20 split
legible:

```go
// registerKeeperInteriorEvents registers §8.20 session-keeper interior cycle
// event constructors (codename:session-restart-substrate). All schema v1, class O.
func registerKeeperInteriorEvents() {
    mustRegister("session_keeper_handoff_written", func() EventPayload { return &SessionKeeperHandoffWrittenPayload{} })
    mustRegister("session_keeper_model_done",      func() EventPayload { return &SessionKeeperModelDonePayload{} })
    mustRegister("session_keeper_clear_sent",      func() EventPayload { return &SessionKeeperClearSentPayload{} })
    mustRegister("session_keeper_new_session_up",  func() EventPayload { return &SessionKeeperNewSessionUpPayload{} })
}
```

### 2.4 `PayloadCompatEntry` rows — `internal/core/pertypecompat_hqwn38.go` (MANDATORY)

Add under a new `§8.20` banner in `allPayloadCompatEntries` (after the §8.19 block). Missing rows =
red `pertypecompat_hqwn38_test.go:58-91` (bidirectional: registered⇒entry AND entry⇒registered).

```go
// ── §8.20 Session-keeper interior cycle events (codename:session-restart-substrate) ──
{TypeName: "session_keeper_handoff_written", CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
{TypeName: "session_keeper_model_done",      CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
{TypeName: "session_keeper_clear_sent",      CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
{TypeName: "session_keeper_new_session_up",  CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true},
```

### 2.5 Roundtrip + Valid() tests

Extend `internal/core/keeperevents_roundtrip_test.go` with a marshal→`DecodePayload`→`DeepEqual`
case per new type (existing pattern). Add a small prop test mirroring
`reconciliationevents_hqwn59_prop_test.go`: `Valid()` true on a well-formed value, false on
empty `CycleID`, false on `new_session_up` with `NewSessionID == PrevSessionID`, false on
`clear_sent` with `Attempt == 0`.

### 2.6 EV-036 secret-prefix compliance — confirmed

Field names introduced: `agent_name, cycle_id, session_id, nonce, degraded, source, attempt,
prev_session_id, new_session_id`. None match the EV-036 regex
`(?i)(secret|token|password|api[_-]?key|auth)` (`eventregistry.go:68`, enforced by
`ScanRegisteredPayloadsForSecretFields`, `:298`). `nonce` is a session-handoff nonce (not a secret
token) and does not match. Compliant.

### 2.7 Cohort guard — do NOT touch `allEventTypeCohort` / `wantCount`

Per the 18-event keeper precedent (findings §1.1/§4), keeper types are excluded from
`allEventTypeCohort` (`eventtype_coverage_gjyks_test.go`, 0 keeper hits) and from the EV-027 count
guard (`ev027_amendment_guard_hqwn36_test.go` `wantCount=126`). The four new events follow the
exclusion — add **nothing** to those two files. This decision is recorded explicitly in the spec
and the gjyks doc-comment is amended (§7.2) so the carve-out is no longer silently false.

---

## 3. The cycle_id joinability fix (EV-U2, D7)

### 3.1 Why payload-level `cycle_id`, not envelope `run_id` (restated)

Three code-grounded reasons (findings §2.4), all pinned by D7:

1. **Type mismatch.** Envelope `RunID` is `type RunID uuid.UUID` (`runid.go:19`) with `Event.Valid()`
   rejecting non-nil Nil (`event.go:129-131`). Keeper cycle ids are non-UUID strings
   `cyc-<UTC-ts>-<seq>` (`cycle.go:434-441`). Envelope adoption forces re-minting ids and migrating
   the journal/nonce marker/78K historical events.
2. **Semantic collision.** EM-013 `run_id` is the *workflow-run* join key with live folds
   (`daemon.go:2067`, `runinflightreconcile_hkr73qr.go:100`, `eventbus.Filter`
   `jsonlwriter.go:380`). Injecting keeper cycles pollutes that keyspace — keeper restarts would
   look like workflow runs to every fold.
3. **Exact precedent.** Reconciliation put its own run concept in the payload
   (`ReconciliationRunID`, `reconciliationevents_hqwn59.go:80-82`, `Valid()`-checked).

### 3.2 The `Valid()` enforcement and the composite join key

`Valid()` (§2.2) asserts non-empty `cycle_id`. The `^cyc-` prefix check is a **soft assertion**: keep
it OUT of `Valid()` (a future `CycleIDGen` change must not retro-invalidate historical corpora), and
instead let the harness *report* a non-conforming id as a low-severity `Violation` rather than
dropping the event. `Valid()` stays "non-empty" so it never rejects a legitimately-formed id.

**Composite join key (D7, measurement findings §1):** `cycle_id` alone is not globally unique —
`newCycleIDGen` resets its seq per process (`cycle.go:431-441`), yielding ~476 distinct ids across
507 real cycles. The harness join key is the composite **`(agent_name, cycle_id)`** = 507 cycles.
All four new payloads carry both fields, so the composite is always available. `CycleState` is keyed
on the composite (§4.4), never `cycle_id` alone.

### 3.3 Optional additive backfill onto `ack_timeout`

`SessionKeeperAckTimeoutPayload` (`keeperevents.go`, fields `AgentName, Nonce, Kind, TimeoutSeconds,
TmuxTarget, Reason`) fires *inside* a restart handshake (`awaitack.go:204`) but carries no cycle
join key. Additively add `CycleID string \`json:"cycle_id,omitempty"\`` (omitempty here — unlike the
four new events it is a backfill onto an existing type, so old records legitimately lack it). No
schema-version bump (additive; the compat entry's `AdditiveOnly:true` already covers it). Wire the
value at `awaitack.go:204` if `cycleID` is in scope; if not trivially in scope, defer as a follow-up
bead — it is a nicety, not blocking.

### 3.4 Explicit non-change

**No change to the 14 `core.RunID{}` envelope arguments** (findings §2.2: `cycle.go:1421,1484,1586,
1598,1610,1621,1632`; `awaitack.go:204`; `watcher.go:1497,1515,1713,1790,1807,1824`). Absent
envelope `run_id` is correct for non-run-scoped events (EV-001 "when scoped to a run"). The four new
emit helpers likewise pass `core.RunID{}` and rely on payload `cycle_id`.

---

## 4. Typed-decode adoption (EV-U3, D6) — the `internal/replay` harness

### 4.1 Home and the pinned API

NEW leaf package **`internal/replay`** (not in keeper, not in core). It becomes the registry's
**first production reader** — the EV-033 "observational consumer" the API was specced for
(all four decode symbols have zero production readers today, alive only in 9 test files; findings §3.1).
Pinned surface (from 00-decisions D6):

```go
package replay

type Checker interface {
    Types() []core.EventType // empty ⇒ all types
    Check(ev core.Event, p core.EventPayload, s *CycleState) []Violation
}
type Violation struct { EventID core.EventID; CycleID string; Rule, Detail string }
type Report struct {
    Events, Skipped, Malformed int
    SchemaMismatches           []core.EventID
    RegisteredNeverObserved    []core.EventType // §4.6 operator_attached precedent
    Violations                 []Violation
}
func Replay(path string, since core.EventID, strict bool, checkers []Checker) Report
```

### 4.2 The decode/assert loop (how it uses the registry)

Per envelope yielded by `eventbus.ScanAfter` (§5):

1. `core.ValidateEnvelopeSchemaVersion(ev)` (`eventregistry.go:165`) — a mismatch is a *finding*
   (writer/reader drift), recorded in `Report.SchemaMismatches`, not fatal. On mismatch consult
   `core.LookupPayloadCompatEntry(ev.Type)` (`pertypecompat_hqwn38.go:373`): `CompatWindowHolds`
   tells the harness whether N-1 replay is declared safe.
2. Decode:
   - **non-strict (default):** `core.DispatchObservational(ev)` (`eventdispatch.go:53`). Unknown
     type ⇒ `core.ErrSkipUnknown` ⇒ `Report.Skipped++; continue` (EV-033 observational policy —
     newer-binary types and pre-registry junk are counted, not fatal).
   - **strict:** route through the new `DecodePayloadStrict` (§4.3) + `DispatchSynchronous`
     semantics — unknown type ⇒ `*core.DispatchUnknownEventError` (`eventdispatch.go:24`) is a hard
     finding. Strict is for replaying the harness's OWN freshly-recorded corpus, where an unknown
     type means a `mustRegister` was forgotten (the hk-gjyks failure mode).
3. Route the decoded `core.EventPayload` to every `Checker` whose `Types()` matches (or is empty),
   keyed into `CycleState` by the composite `(agent_name, cycle_id)` via the `GetCycleID` interface
   (§4.4).

`Malformed` counts genuine JSON errors (decode error that is neither unknown-type nor skip).

### 4.3 `DecodePayloadStrict` — the one real additive registry gap

`DecodePayload` uses `json.Unmarshal` (`eventregistry.go:227`) — no `DisallowUnknownFields`, so it
**cannot see additive writer drift** (findings §3.3.2). Add a strict sibling to the registry
(additive, small):

```go
// DecodePayloadStrict decodes like DecodePayload but rejects unknown payload
// fields — surfacing additive writer drift a newer binary introduced. Used by
// the replay harness in strict mode (EV-U3).
func (e Event) DecodePayloadStrict() (EventPayload, error) {
    ctor, ok := lookupConstructor(e.Type)          // internal accessor over the registry map
    if !ok { return nil, fmt.Errorf("%w: %s", ErrUnknownEventType, e.Type) }
    payload := ctor()
    dec := json.NewDecoder(bytes.NewReader(e.Payload))
    dec.DisallowUnknownFields()
    if err := dec.Decode(payload); err != nil { return nil, err }
    return payload, nil
}
```

`lookupConstructor` is a thin internal accessor over the existing registry map (the same map
`DecodePayload` reads at `eventregistry.go:218-231`). Optionally add a
`DispatchSynchronousStrict` wrapper mirroring `DispatchSynchronous` (`eventdispatch.go:75`) so the
harness's strict path is one call; or the harness calls `DecodePayloadStrict` directly and applies
the unknown-type policy inline. Either is additive; prefer exposing `DecodePayloadStrict` + letting
`Replay` own the skip-vs-fail policy (keeps `eventdispatch.go` untouched).

### 4.4 The `GetCycleID` mini-interface + `CycleState`

`EventPayload` is `interface{}`, so routing needs a structural interface (findings §3.3.1). Define in
`internal/replay` and implement `GetCycleID()` on the four new payloads (additive methods in
`keeperevents.go`; and on the existing cycle payloads that already carry `CycleID` — handoff_started,
cycle_complete, cycle_aborted, clear_unconfirmed, cycle_recovered — so the harness sees whole cycles):

```go
// cycleScoped is implemented by keeper payloads that carry a cycle join key.
type cycleScoped interface{ GetCycleID() string }

type CycleState struct {
    AgentName, CycleID string
    Seen               map[core.EventType]core.Event // first occurrence per type
    Terminal           core.EventType                // "" until a terminal is seen
    LastEventID        core.EventID
    // ...ordering timestamps as needed by checkers
}
```

Routing: `cid, ok := p.(cycleScoped); if !ok { /* non-cycle keeper or foreign type */ }`; the
map key is `agentName + "\x00" + cid.GetCycleID()` — `agentName` read off the concrete payload
via a second tiny interface `agentScoped interface{ GetAgentName() string }` (also additive), giving
the composite key of §3.2.

### 4.5 The concrete keeper-cycle invariant checkers (SR3/SR4/SR6/SR7/SR9)

Each SR invariant is a `Checker`. State transitions update `CycleState.Seen`; violations reference
`ev.EventID` + the composite `cycle_id`.

- **SR3 — handoff-write-done before /clear.** `Types() = {clear_sent}`. On `clear_sent`, require
  `Seen[handoff_written]` present; else `Violation{Rule:"clear_sent-before-handoff_written"}`.
  (Machine-checks the `runCycle` SAFETY invariant `cycle.go:930-934`.)
- **SR4 — /clear NEVER before model-done.** `Types() = {clear_sent}`. On `clear_sent`, require
  `Seen[model_done]` present; else `Violation{Rule:"clear_sent-before-model_done"}`. This is the
  headline new invariant (D12); for old corpora it is version-gated (§7.5).
- **SR6 — brief only after new-session confirmed.** `Types() = {cycle_complete}`. On terminal
  `cycle_complete`, require `Seen[new_session_up]` OR `Seen[clear_unconfirmed]` (the degraded path);
  a `cycle_complete` with neither ⇒ `Violation{Rule:"complete-before-new_session_up"}`.
- **SR7 — no overlapping restarts.** `Types() = {handoff_started}`. Track a per-`agent_name`
  "open cycle" set; a `handoff_started` for an agent that already has a non-terminal cycle ⇒
  `Violation{Rule:"overlapping-restart"}`. (Structurally guaranteed by the D11 `Idle`-only gating,
  but the harness verifies it against recorded corpora.)
- **SR9 — bounded liveness.** A finalizing checker: at end of replay, any cycle with
  `Seen[handoff_started]` but empty `Terminal` (no `cycle_complete`/`cycle_aborted`) ⇒
  `Violation{Rule:"unterminated-cycle"}`. This is the "unterminated 1 → must be 0" baseline anchor
  (measurement D13). Terminal exclusivity (`cycle_complete` XOR `cycle_aborted`) is a companion
  check in the same checker.

`Replay` runs all checkers over each event; SR9's finalization runs after the `ScanAfter` loop drains
(the harness holds `map[compositeKey]*CycleState`, iterates at the end).

### 4.6 Registered-but-never-emitted — report, don't fail

`session_keeper_operator_attached` is registered but its emitter is a no-op (`cycle.go:1502`, empty
body — findings §1.2). The harness must not treat "registered type never seen in the corpus" as a
failure or the taxonomy rots invisibly. `Replay` computes
`RegisteredNeverObserved = registeredTypes − observedTypes` and **reports** it (informational), never
a `Violation`. `operator_attached` is the standing precedent this handles.

---

## 5. ScanAfter as the replay read surface (EV-U4)

### 5.1 It is already a live, declared surface

`eventbus.ScanAfter(path string, sinceID core.EventID) iter.Seq[core.Event]`
(`jsonlwriter.go:312-353`) is exported, spec-cited (EV-020/EV-002, bead hk-a5sil), and has **15+
production callers** (findings §4.3: `cmd/harmonik/comms.go:667`, `digest/builder.go:371,516`,
`presence/presence.go:163`, `sentinel/governor.go:329`, `daemon/daemon.go:2067`,
`watch/ledger.go:158,176`, …). EV-U4 promotes it from the incidental EV-038 mention to a **declared
read surface** — it already IS the project's offline-replay primitive. No new read code is needed;
the harness consumes it directly.

### 5.2 Watermark / incremental pattern

Full history: `ScanAfter(eventsPath, core.EventID{})` (zero id = from the beginning — the established
idiom, `supervisorrevival_hkrnkuy.go:48`, `daemon.go:2067`). Incremental re-runs pass the last
processed `EventID` as `sinceID` (the `> sinceID` filter is `bytes.Compare` at `:336-337`) — exactly
the cursor/watermark pattern `watch/ledger.go:176` (`l.cursor`) already uses. `Replay`'s `since`
parameter is this watermark.

### 5.3 Cross-process EventID / file-order caveat (D9)

`ScanAfter` yields **file order** and does not re-sort. Within one writer, file order = EventID order
(single drainer, UUIDv7 monotone). Across processes — daemon `JSONLWriter` + N keeper `FileEmitter`s
append to the SAME `events.jsonl` (`watcher.go:47-51`, `core.EventsJSONLPath`
`jsonlformat_hqwn58.go:20`) — each has its own `EventIDGenerator`, so concurrent UUIDv7s can
interleave and file order ≠ global EventID order (findings §4.2). **Decision (D9): the harness sorts
collected events by `EventID` after the `ScanAfter` drain** (cheap at this scale), then feeds the
checkers. This makes ordering invariants (SR3/SR4/SR6, EventID-monotone-within-cycle) deterministic
regardless of cross-writer interleave.

### 5.4 `Filter` stays dead — reinforces D7

`eventbus.Filter(path, runID)` (`jsonlwriter.go:380`) has **zero production callers** (all hits are
tests; findings §4.3). It selects by envelope `run_id`. Building cycle extraction on `Filter` would
mean adopting the envelope-run_id design D7 rejected; the harness type-filters
(`strings.HasPrefix(ev.Type, "session_keeper_")`) and groups by `payload.cycle_id` on the live
`ScanAfter` surface. `Filter` is left untouched and undeclared — its deadness is evidence, not a gap.

---

## 6. Durability + error handling (D9)

### 6.1 O-class classification

The four events are class **O (observational)** for this phase — emitted through the existing keeper
`Emitter` / `FileEmitter` path (`watcher.go:57-99`). The spec states this as an explicit §8.20
durability row (not implicit). The restart cycle's crash-durability need is met by the retained
journal's `RecoverFromCrash` role (D10), so O-class here is not a gap.

### 6.2 Emit-failure MUST NOT be swallowed (the required hardening)

Today every keeper emit is `_ = c.emitter.EmitWithRunID(...)` with `//nolint:errcheck`, and the
`json.Marshal` error is dropped too (`cycle.go:1585-1586`, and all sibling helpers `:1579-1622`).
For the four new events this is a spec lie ("durable event that silently fails to write"). Required
change (D9.1): the four new emit helpers **log on failure** — both the marshal error and the emit
error. Minimum shape:

```go
func (c *Cycler) emitClearSent(ctx context.Context, cycleID, sessionID string, attempt int) {
    payload := core.SessionKeeperClearSentPayload{AgentName: c.cfg.AgentName, CycleID: cycleID, SessionID: sessionID, Attempt: attempt}
    raw, err := json.Marshal(payload)
    if err != nil { c.logf("keeper: marshal clear_sent cycle=%s: %v", cycleID, err); return }
    if err := c.emitter.EmitWithRunID(ctx, core.RunID{}, core.EventTypeSessionKeeperClearSent, raw); err != nil {
        c.logf("keeper: emit clear_sent cycle=%s: %v", cycleID, err)
    }
}
```

(Use the keeper's existing log sink — same one the watcher uses; do not invent a new logger.) The
other three helpers follow. Insertion points (findings §1.4.5): `handoff_written` at the
confirmed-phase `cycle.go:1081-1083` (+ the ack-timeout recovery branch `:1018-1026`); `model_done`
needs the new D12 detection between confirm and clear (the only event without an existing seam);
`clear_sent` at `cycle.go:1118-1123` (right after the `/clear` inject); `new_session_up` at
`cycle.go:1139` (non-empty `newSID`; the negative branch already emits `clear_unconfirmed`).

**Scope note:** do NOT retrofit logging onto the 14 existing `_ =` emits — that is out of scope and
would balloon the diff. Only the four new helpers get the hardening (D9 is explicit: "for these
four").

### 6.3 File-order vs EventID — decided

Per §5.3 / D9.2: **the harness sorts by EventID after collection.** The spec states the
cross-process ordering fact explicitly (per-process EventID generators ⇒ file order is only
approximate global order) and names EventID-sort as the harness's determinism mechanism, rather than
declaring "file order authoritative."

### 6.4 F-class deferred — why

`FileEmitter.EmitWithRunID` opens/appends/closes per event with **no `Sync()`** (`watcher.go:86-99`)
— no fsync, no batching, unlike the daemon's `JSONLWriter` (caller-driven fsync, batching drainer,
`jsonlwriter.go:37-75`). F-class would require routing keeper emits through the daemon or adding a
sync flag to `FileEmitter` — larger blast radius (Constraint 1: no daemon in execution). D9 defers
F-class; revisit if a later phase routes keeper through the daemon. Stated as an explicit deferred
spec row, not left implicit.

---

## 7. Risks for the spec-drafter

### 7.1 §8.9(b) acceptance-criteria tension — argue the exception (findings §5.5)

`event-model.md:278-291` §8.9 admits a type iff **all** of (a)–(h). The four events are
**cycle-interior**, which reads against **§8.9(b)** ("lifecycle-boundary signal rather than
intra-lifecycle detail"). The drafter MUST write the exception into the amendment, argued thus:

- **§8.9(a) satisfied** — the `internal/replay` harness is a real cross-subsystem consumer (keeper
  emits → jsonl → replay package asserts).
- **§8.9(c) satisfied** — the harness *requires per-boundary access*: SR3/SR4/SR6 are orderings
  *between* these interior milestones; a single summary event cannot express "clear_sent after
  model_done." This is the granularity criterion working exactly as intended.
- **§8.9(b) reframed** — these four ARE the lifecycle boundaries *of the restart state machine's
  sub-lifecycle* (handoff-write / model-done / clear / new-session are its transition points, D11),
  not incidental intra-lifecycle chatter. The `tool_command_completed` deferral (§8.9 note) failed
  because it had *no* consumer; here the consumer is named and the invariants are load-bearing.
- **§8.9(h) does NOT force a merge** — the four are four distinct single-emission milestones, not a
  paired pending/resolved lifecycle, so the "use one type with a `status` field" rule doesn't apply.

**EV-026 is NOT the escape hatch:** EV-026-internal events "MUST NOT be passed to RegisterEventType"
(`eventregistry.go:21-26`), which would kill the D6 typed-decode adoption. Register them (they cross
the keeper→jsonl→harness process boundary, so they are not EV-026-internal) and carry the §8.9
justification in the amendment.

### 7.2 Cohort-guard / EV-027 — follow the keeper exclusion, amend the gjyks contract (findings §5.4)

`eventtype_coverage_gjyks_test.go:17-19` *states* "every EventType constant … MUST also be appended
to `allEventTypeCohort`," but 18 keeper types already violate that (0 keeper entries in the cohort;
the test only iterates the cohort list, so it passes). Decision (D8): **follow the exclusion** — add
nothing to `allEventTypeCohort` or `wantCount` (126). But the stated contract is currently **false**;
amend the gjyks doc-comment to name the keeper carve-out explicitly (e.g. "…except the
`session_keeper_*` families §8.16/§8.20, which are registered + compat-tabled but excluded from the
cross-bus taxonomy cohort per D8"). Record the same carve-out in the spec so future additions know
the rule. This is the "amend the gjyks test's stated contract" the task calls for — a comment edit,
not a test-logic change.

### 7.3 Non-strict decode (findings §5.7)

The default decode path (`DecodePayload` → `json.Unmarshal`) silently ignores unknown payload fields,
so replay cannot see additive writer drift without `DecodePayloadStrict` (§4.3). The strict variant
IS in scope (D6). The drafter should spec that the harness's strict mode is the mechanism that
catches "a newer binary added a field"; non-strict mode is for tolerant historical replay.

### 7.4 pertypecompat doc-drift trap (findings §5.8)

`pertypecompat_hqwn38.go:26` prescribes calling `SetPayloadSchemaVersion` for version bumps — **that
function does not exist**; the real mechanism is `RegisterEventTypeAtVersion` (`eventregistry.go:114`).
Fix that comment during this work (a one-line doc edit) so no one follows the phantom API. The four
new events are all v1, so they don't exercise it — but the comment sits in the file the drafter edits.

### 7.5 Historical-corpus asymmetry — version-aware invariant checks (findings §5.9)

The ~476 recorded cycles predate the four new events; 13 of the 18 existing keeper types lack
`cycle_id`. Invariant checks MUST be version-aware:

- **Post-change corpora** (recordings that contain `handoff_written`/`model_done`/`clear_sent`/
  `new_session_up`): full SR3/SR4/SR6/SR7 sequence invariants apply.
- **Pre-change corpora** (no interior events): only the weaker `handoff_started`↔terminal pairing
  and SR9 liveness apply; SR4 in particular is skipped (there is no `model_done` to order against).
  The measurement design's old-corpus `model_done`-synthesis (D12/D13) keeps old-corpus goldens
  identical; the harness detects "this cycle has interior events" by the presence of any §8.20 type
  and gates the strict checkers on it.

The drafter states this as a normative "checkers are version-aware; absence of §8.20 events selects
the reduced invariant set" clause, so the harness is not read as claiming SR4 held historically.

---

## Traceability

| Requirement (02-components) | Decision | This design |
|---|---|---|
| EV-U1 / EV-U1a (register 4 events) | D8 | §2 (§8.20 cohort) |
| EV-U2 (joinability) | D7 | §3 (payload cycle_id, composite key) |
| EV-U3 (adopt typed-decode) | D6 | §4 (`internal/replay`, `DecodePayloadStrict`) |
| EV-U4 (declare ScanAfter) | D6/D9 | §5 |
| EV-U5 (§8 drift reconcile) | D8 | §1 (lands before EV-U1) |
| durability | D9 | §6 (O-class, emit-failure, EventID-sort) |
