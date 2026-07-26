# RESEARCH (pass 3) — events component — session-restart-substrate

Grounded against the working tree at `/Users/gb/github/harmonik` (branch `main`, HEAD 5160326b), 2026-07-13. Every claim cites file:line. Caller counts proven by grep (results reproduced inline).

---

## 1. Event registration mechanics

### 1.1 The registry, end to end

An event type today is four coupled artifacts, all in `internal/core/`:

1. **EventType constant** — `internal/core/eventtype.go` (1385 lines). Keeper cohort lives at `eventtype.go:931-1082` under the banner comment `§8.13 Session-keeper event types (codename:session-keeper, hk-ekap1)` (`eventtype.go:932`). Each constant carries a doc comment declaring emitter, durability class, and bead ref (e.g. `EventTypeSessionKeeperWarn` at `eventtype.go:942`).
2. **Payload struct** — `internal/core/keeperevents.go` for the keeper family. Structs are plain exported-field JSON structs implementing the empty `EventPayload` marker interface (`eventregistry.go:27`). EV-036 constraint: no exported field name may match `(?i)(secret|token|password|api[_-]?key|auth)` (`eventregistry.go:68`, enforced by `ScanRegisteredPayloadsForSecretFields`, `eventregistry.go:298`).
3. **Constructor registration** — `internal/core/eventreg_hqwn59.go`. `init()` (`:28`) calls `registerKeeperEvents()` (`:42` → func at `:490-521`), which calls `mustRegister(typeName, ctor)` (`:605-618`; panics on duplicate/empty, wraps `RegisterEventType` → `RegisterEventTypeAtVersion(…, 1)` at `eventregistry.go:100-132`). Registration defaults to **schema version 1**; a non-1 version uses `RegisterEventTypeAtVersion` (`eventregistry.go:114`). Note: the evolution doc in `pertypecompat_hqwn38.go:26` references a `SetPayloadSchemaVersion` function that **does not exist** — only `RegisterEventTypeAtVersion` does (doc drift; do not design against it).
4. **N-1 compat entry** — `internal/core/pertypecompat_hqwn38.go:86-369` (`allPayloadCompatEntries`). The keeper family occupies `:266-299` (18 entries). Tests in `pertypecompat_hqwn38_test.go:58-91` enforce **both directions**: every registered type must have a compat entry AND every compat entry must be registered. Adding a registration without a compat entry fails tests.

Two further guard tests exist but keeper events are **precedent-excluded** from both:
- `eventtype_coverage_gjyks_test.go` (`allEventTypeCohort`, 126 entries) — contains **zero** `EventTypeSessionKeeper*` entries (grep `EventTypeSessionKeeper` in that file: 0 hits).
- `ev027_amendment_guard_hqwn36_test.go` pins the cohort count at 126 (`wantCount`); its §8 breakdown comment lists §8.1–§8.15 and omits the keeper section entirely.

So the practical precedent: keeper events are registered + compat-tabled but not counted as "cross-bus taxonomy" for the EV-027 amendment guard. The 18 existing keeper registrations were added without touching `allEventTypeCohort`/`wantCount`.

### 1.2 The 18 existing session_keeper_* registrations (verified)

`eventreg_hqwn59.go:490-521` (`registerKeeperEvents`), doc block at `:478-489` citing "§8.13" with durability classes:

| # | type (register line) |
|---|---|
| 1 | `session_keeper_warn` (:491) |
| 2 | `session_keeper_no_gauge` (:492) |
| 3 | `session_keeper_handoff_started` (:493) |
| 4 | `session_keeper_cycle_complete` (:494) |
| 5 | `session_keeper_cycle_aborted` (:495) |
| 6 | `session_keeper_clear_unconfirmed` (:496) |
| 7 | `session_keeper_cycle_recovered` (:497) |
| 8 | `session_keeper_precompact_blocked` (:499) |
| 9 | `session_keeper_respawn_attempted` (:501) |
| 10 | `session_keeper_operator_attached` (:503) |
| 11 | `session_keeper_restart_now_blocked` (:505) |
| 12 | `session_keeper_blind` (:507) |
| 13 | `session_keeper_hard_ceiling` (:509) |
| 14 | `session_keeper_idle_crew` (:511) |
| 15 | `session_keeper_config_rejected` (:514) |
| 16 | `session_keeper_watcher_dead` (:516) |
| 17 | `session_keeper_live_pane_recover` (:518) |
| 18 | `session_keeper_ack_timeout` (:520) |

(The dossier's ":478-489" is the doc-comment block; the `mustRegister` calls are :491-520.)

Note: `session_keeper_operator_attached` (#10) is registered but its emitter is a **no-op** — `emitOperatorAttached` at `internal/keeper/cycle.go:1502` has an empty body ("no longer persisted to events.jsonl", logmine TA3/F55). Precedent for a registered-but-silent type.

### 1.3 The §8.13 code/spec collision — CONFIRMED

- **Code** squats §8.13 for session-keeper: `eventtype.go:932`, `eventreg_hqwn59.go:481-489`, `keeperevents.go:3`, `pertypecompat_hqwn38.go:266`.
- **Spec** assigns §8.13 to **Epic-completion lifecycle**: `specs/event-model.md:354` (`### 8.13 Epic-completion lifecycle`, sole row 8.13.1 `epic_completed`). The compat table itself acknowledges this at `pertypecompat_hqwn38.go:101-102` (`epic_completed … (§8.13)`).
- The collision is wider than §8.13: code uses §8.14 for alarm/self-check (`eventreg_hqwn59.go:523-527`) while spec §8.14 is HITL-decisions (`specs/event-model.md:368`); code uses §8.15 for hitl-decisions (`eventreg_hqwn59.go:537`) while spec §8.15 is bead-ledger merge (`specs/event-model.md:392`). The spec's §8 headings end at **§8.15** (`grep '^### 8\.' specs/event-model.md` → last is :392).
- Code comments have informally claimed further sections never written into the spec: §8.16 remote-substrate workers (`eventtype.go:1148`), §8.17 flywheel governor (`eventtype.go:1261`), §8.18 flywheel liveness halt (`eventtype.go:1340`), §8.19 stall-sentinel (`eventtype.go:1363`, `eventreg_hqwn59.go:532`, `pertypecompat_hqwn38.go:331`).
- **Next free section number:** `§8.20` — zero hits for `§8.20`/`8.20` in `internal/`, `cmd/`, and `specs/event-model.md`. The new 4-event cohort should take **§8.20** (or the Change-Design should first resolve the §8.13–8.19 numbering drift; see Risks §5).

### 1.4 Exactly what adding the 4 new events requires

For `session_keeper_handoff_written`, `session_keeper_model_done`, `session_keeper_clear_sent`, `session_keeper_new_session_up` — **none exist anywhere today** (grep `handoff_written|model_done|clear_sent|new_session_up` across `internal/ cmd/`: 0 hits).

Per event:
1. `EventType` constant in `eventtype.go` — new `§8.20` const block after the §8.19 block (`eventtype.go:1363-1385`), with emitter/durability/refs doc comment following the pattern of the existing keeper block (`eventtype.go:935-1081`).
2. Payload struct in `internal/core/keeperevents.go`. Minimum fields following the cycle-event pattern (`SessionKeeperHandoffStartedPayload`, `keeperevents.go:58-65`): `AgentName string` (json `agent_name`), **`CycleID string` (json `cycle_id`, REQUIRED — see §2)**, `SessionID string` (json `session_id,omitempty`); plus event-specific fields (`new_session_up`: `NewSessionID`; `clear_sent`: nothing extra; `model_done`: idle-detection detail). All field names pass the EV-036 secret-prefix regex.
3. `mustRegister` line in `registerKeeperEvents` (`eventreg_hqwn59.go:490`), schema version 1 (default).
4. Four `PayloadCompatEntry` rows in `pertypecompat_hqwn38.go` (`CurrentVersion: 1, PreviousVersion: 0, CompatWindowHolds: true, AdditiveOnly: true`) — **mandatory**, `pertypecompat_hqwn38_test.go:58-91` fails otherwise.
5. Emit helpers + call sites in `internal/keeper/cycle.go` following the `emitHandoffStarted` pattern (`cycle.go:1579-1587`). Insertion points in the 7-step cycle (`runCycle` `cycle.go:935-1085`, `completeCycleTail` `cycle.go:1101-1176`):
   - `handoff_written` — nonce confirmed: `cycle.go:1081-1083` (`j.Phase = "confirmed"`), plus the ack-timeout recovery branch `cycle.go:1018-1026` (`handoff_timeout_recovered`, fresh-handoff mtime check).
   - `model_done` — no existing observation point; the nearest existing signal is the nonce confirm itself (the model's handoff turn finished) or a `CrispIdleFn` poll (`cycle.go:1435`) between confirm and Step 4. **New detection logic required** — this is the only one of the four without an existing hook.
   - `clear_sent` — Step 4: `cycle.go:1118-1123` (immediately after `InjectFn(ctx, …, "/clear")`, where `j.Phase = "cleared"` is journaled).
   - `new_session_up` — Step 5: `cycle.go:1139` (`waitForNewSessionIDWithBackstop` returned non-empty `newSID`); the negative branch already emits `clear_unconfirmed` (`cycle.go:1140-1141`).
6. Roundtrip test entries per `internal/core/keeperevents_roundtrip_test.go` pattern; do **not** touch `allEventTypeCohort`/`wantCount` (per the 18-event keeper precedent) — but record this decision explicitly in Change-Design (see Risks).
7. Journal parity: each emit point coincides with a `CycleJournal` phase write (`cycle.go:948-955, 1000-1002, 1121-1123, 1156-1158`); the events are the durable externalization of phases the journal already tracks.

"Real cycle/run id": already exists — `CycleIDGen` (`cycle.go:85`, default `newCycleIDGen` at `cycle.go:431-441`) produces `cyc-<UTC-start-timestamp>-<seq>` (e.g. live: `cyc-20260711T211710-000008`), collision-resistant across process instances. It is minted once per cycle at `cycle.go:936`.

---

## 2. The zero-run_id fix — concrete

### 2.1 The envelope field

`internal/core/event.go:73-77`:

    // RunID is present when the event is scoped to a run (EV-001; EM-013).
    // EM-013 requires run_id on every run-scoped event as the join key across
    // git (Harmonik-Run-ID trailer), Beads, and JSONL.
    // Optional; when non-nil must not be uuid.Nil.
    RunID *RunID `json:"run_id,omitempty"`

`RunID` is `type RunID uuid.UUID` (`internal/core/runid.go:19`). `Event.Valid()` rejects a non-nil Nil RunID (`event.go:129-131`).

### 2.2 The emit sites that pass zero run_id (all 14 of them)

Every keeper emit passes the zero value `core.RunID{}` (grep `EmitWithRunID(ctx, core.RunID{}` in `internal/keeper/`, non-test):

- `internal/keeper/cycle.go:1421` (idle_crew), `:1484` (precompact_blocked), `:1586` (handoff_started), `:1598` (cycle_complete), `:1610` (cycle_aborted), `:1621` (clear_unconfirmed), `:1632` (cycle_recovered)
- `internal/keeper/awaitack.go:204` (ack_timeout)
- `internal/keeper/watcher.go:1497` (no_gauge), `:1515` (warn), `:1713` (live_pane_recover), `:1790` (blind), `:1807` (hard_ceiling), `:1824` (respawn_attempted)

`FileEmitter.EmitWithRunID` (`internal/keeper/watcher.go:57-99`) then **drops** the zero RunID: `watcher.go:72-76` — `var zeroRunID core.RunID; if runID != zeroRunID { ev.RunID = &r }` — so the envelope is written with `run_id` omitted entirely (`omitempty`).

**Live proof:** in `.harmonik/events/events.jsonl` (238,412 lines), 78,520 events have `"type":"session_keeper_*"` and **0** of them carry a `"run_id"` key. Sample line:

    {"event_id":"019f5dc4-…","schema_version":1,"type":"session_keeper_cycle_complete","timestamp_wall":"2026-07-13T23:16:08.352047Z","source_subsystem":"internal/keeper","payload":{"agent_name":"admiral","cycle_id":"cyc-20260711T211710-000008","prev_session_id":"2fac4f45-…","new_session_id":"98adbf79-…"}}

### 2.3 Where CycleID exists today (and where it doesn't)

Payload-level `cycle_id` already exists on 5 of the 18 types: `handoff_started` (`keeperevents.go:62`), `cycle_complete` (`:78`), `cycle_aborted` (`:98`), `clear_unconfirmed` (`:117`), `cycle_recovered` (grep `CycleID` in `keeperevents.go`). It is **absent** from `warn`, `no_gauge`, `precompact_blocked`, `idle_crew`, `ack_timeout`, `blind`, `hard_ceiling`, `respawn_attempted`, `live_pane_recover`, `restart_now_blocked`, `operator_attached`, `config_rejected`, `watcher_dead` — most of which are watcher-scope (no cycle in flight), which is legitimate; but `ack_timeout` fires *inside* a restart handshake and carries only `nonce` (`keeperevents.go:350-…`, fields `AgentName, Nonce, Kind, TimeoutSeconds, TmuxTarget, Reason`) with no cycle join key.

### 2.4 Recommendation: REQUIRED payload-level cycle_id, NOT envelope run_id

**Recommend the payload-level `cycle_id` (reconciliation precedent), not the envelope `run_id`.** Reasons:

1. **Type mismatch.** Envelope `RunID` is a `uuid.UUID` newtype (`runid.go:19`) and `Valid()` enforces non-Nil UUID (`event.go:129`). The keeper cycle id is a non-UUID string `cyc-20060102T150405-NNNNNN` (`cycle.go:434-441`). Using the envelope would force re-minting cycle ids as UUIDv7 and migrating the journal (`CycleJournal.CycleID`, `cycle.go:28`), nonce marker (`nonceMarker`, `cycle.go:502-504`), and ~78K historical events' join story.
2. **Semantic collision.** EM-013 defines `run_id` as the join key across git `Harmonik-Run-ID` trailers, Beads, and JSONL for *workflow runs* (`event.go:73-75`). Production consumers treat it exactly that way: `internal/daemon/runinflightreconcile_hkr73qr.go:100` and `internal/daemon/daemon.go:2067` fold run-scoped events to reconcile in-flight *runs*; `eventbus.Filter(path, runID)` (`jsonlwriter.go:380`) selects by envelope run_id. Injecting keeper cycles into that keyspace would make keeper restarts look like workflow runs to every existing fold.
3. **Exact precedent exists.** The reconciliation subsystem faced the same "this subsystem has its own run concept" problem and put it in the payload: `ReconciliationRunID RunID` (json `reconciliation_run_id`) at `internal/core/reconciliationevents_hqwn59.go:80-82`, with `Valid()` rejecting `uuid.Nil` (`:92-95`). The keeper analog is a **required, `Valid()`-checked `cycle_id`** (non-empty string; optionally pattern-checked against `^cyc-`).

**Concrete code changes:**
- `keeperevents.go`: add `CycleID` (json `cycle_id`, no omitempty) to the 4 new payload structs; add `Valid()` methods (the keeper payloads currently have none — the reconciliation structs are the pattern) asserting non-empty `cycle_id` on all cycle-interior types. Optionally backfill `CycleID` onto `ack_timeout` (additive per §6.4; no schema-version bump needed for additive changes, but update the compat entry's reasoning).
- `cycle.go` emit helpers for the 4 new events take `cycleID` exactly as `emitHandoffStarted` does (`cycle.go:1579`); all four insertion points (§1.4.5) are inside `runCycle`/`completeCycleTail` where `cycleID` is already in scope.
- **No change to the 14 `core.RunID{}` arguments** — leaving the envelope run_id absent is correct for non-run-scoped events per EV-001 ("when scoped to a run").
- The replay harness joins on `payload.cycle_id`, not envelope run_id (this also honors the major-issue-fanout skill's warning against grepping events.jsonl by run_id — structured payload query instead).

Counter-case for the envelope (for completeness): setting envelope run_id would make `eventbus.Filter` work as-is for cycle extraction. But `Filter` has zero production callers today (§4) and the harness will type-filter + group-by anyway; not worth the type migration and semantic pollution.

---

## 3. The typed-decode path — ADOPT design

### 3.1 The symbols, signatures, and confirmed caller counts

Grep method: `grep -rn "<symbol>(" --include="*.go" .` excluding `.claire/` worktrees, split test vs non-test. "Production reader" = a non-test caller outside `internal/core` itself.

| Symbol | Location | Signature | Non-test callers outside core | Test callers |
|---|---|---|---|---|
| `DecodePayload` | `eventregistry.go:218` | `func (e Event) DecodePayload() (EventPayload, error)` | **0** (only `eventdispatch.go:54,:76` — the two Dispatch wrappers, same package) | 7 files (`keeperevents_roundtrip_test.go`, `eventregistry_test.go`, `cpinv002_events_only_hka8bg55_test.go`, `decisionpayloads_33p_test.go`, `agentcommspayloads_djqc9_test.go`, `epiccompleted_test.go`, `internal/workers/breach_test.go`) |
| `ValidateEnvelopeSchemaVersion` | `eventregistry.go:165` | `func ValidateEnvelopeSchemaVersion(e Event) error` | **0** | 2 files (`eventregistry_test.go`, `pertypecompat_hqwn38_test.go`) |
| `DispatchObservational` | `eventdispatch.go:53` | `func DispatchObservational(e Event) (EventPayload, error)` | **0** | 1 file (`eventdispatch_test.go`) |
| `DispatchSynchronous` | `eventdispatch.go:75` | `func DispatchSynchronous(e Event) (EventPayload, error)` | **0** | 1 file (`eventdispatch_test.go`) |
| `pertypecompat` table | `pertypecompat_hqwn38.go:86-369`; accessors `LookupPayloadCompatEntry` (:373), `AllPayloadCompatEntries` (:384) | table of `PayloadCompatEntry{TypeName, CurrentVersion, PreviousVersion, CompatWindowHolds, AdditiveOnly}` (:52-74) | **0** (accessor doc itself says "Used by tests and diagnostic tooling", :382-383) | `pertypecompat_hqwn38_test.go` |

**Zero production readers confirmed for all four.** Every production consumer of `events.jsonl` today (digest, sentinel, presence, watch, daemon folds, comms) does ad-hoc `json.Unmarshal` of the envelope and hand-rolled payload handling; none dispatches through the registry.

Behavior details relevant to the harness:
- `DecodePayload` (`eventregistry.go:218-231`): registry lookup by `e.Type` → fresh `entry.constructor()` → `json.Unmarshal(e.Payload, payload)`. Errors: `ErrUnknownEventType` (`:31`) or the raw unmarshal error. **It does NOT check schema_version** — that is `ValidateEnvelopeSchemaVersion`'s job, a separate call.
- `ValidateEnvelopeSchemaVersion` (`eventregistry.go:165-178`): `e.SchemaVersion` vs registered per-type version; errors `ErrUnknownEventType` or wrapped `ErrSchemaVersionMismatch` (`:40`).
- `DispatchObservational` (`eventdispatch.go:53-62`): unknown type → sentinel `ErrSkipUnknown` (`:14`) — the EV-033 "observational consumers skip" policy. Malformed JSON → raw error.
- `DispatchSynchronous` (`eventdispatch.go:75-87`): unknown type → structured `*DispatchUnknownEventError{EventType, EventID}` (`:24-29`, unwraps to `ErrUnknownEventType`) — the EV-033 "synchronous consumers fail" policy.
- The registry is global, `init()`-populated (`eventreg_hqwn59.go:28`), 169 `mustRegister` calls in `eventreg_hqwn59.go` alone — importing `internal/core` gives the harness the complete type universe for free.

### 3.2 How a replay invariant-checking harness USES it

Per recorded envelope from `ScanAfter` (§4):
1. `ValidateEnvelopeSchemaVersion(ev)` — asserts the recorded schema_version matches the registry the harness was built with. A mismatch is itself a finding (writer/reader drift), and `LookupPayloadCompatEntry(ev.Type)` (`pertypecompat_hqwn38.go:373`) tells the harness whether the N-1 window is declared to hold (replay old corpora against a newer binary).
2. `DispatchObservational(ev)` — typed payload or a skip. The harness is the textbook EV-033 observational consumer: unknown types (events recorded by a newer binary, or pre-registry junk lines) are **counted, not fatal**. A strict mode uses `DispatchSynchronous` to hard-fail on unknowns (useful when replaying the harness's own freshly-recorded corpus, where an unknown type means a registration was forgotten — exactly the hk-gjyks failure mode named in `eventtype_coverage_gjyks_test.go:10-13`).
3. Type-switch the `EventPayload` to the concrete `*core.SessionKeeper…Payload` and run per-type invariant checks against a per-cycle state machine keyed on `payload.cycle_id`.

Sketch (new package, e.g. `internal/replay`):

    // Checker inspects one decoded event and may update per-key state.
    type Checker interface {
        Types() []core.EventType                 // empty = all
        Check(ev core.Event, p core.EventPayload, s *CycleState) []Violation
    }

    type Violation struct {
        EventID  core.EventID
        CycleID  string
        Rule     string // e.g. "clear_sent-before-handoff_written"
        Detail   string
    }

    type Report struct {
        Events, Skipped, Malformed int
        SchemaMismatches           []core.EventID
        Violations                 []Violation
    }

    func Replay(path string, since core.EventID, strict bool, checkers []Checker) Report {
        var r Report
        for ev := range eventbus.ScanAfter(path, since) {
            if err := core.ValidateEnvelopeSchemaVersion(ev); err != nil {
                if errors.Is(err, core.ErrSchemaVersionMismatch) { /* record */ }
            }
            p, err := core.DispatchObservational(ev) // or DispatchSynchronous when strict
            if errors.Is(err, core.ErrSkipUnknown) { r.Skipped++; continue }
            if err != nil { r.Malformed++; continue }
            // route to checkers; state keyed on payload cycle_id via a small
            // interface { GetCycleID() string } implemented by keeper payloads.
        }
        return r
    }

Keeper-cycle invariants this enables (all currently violated-undetectably): per `cycle_id` — `handoff_started` precedes everything; `clear_sent` only after `handoff_written` (the runCycle SAFETY invariant, `cycle.go:930-934`, becomes machine-checked); exactly one terminal (`cycle_complete` xor `cycle_aborted`); `new_session_up.new_session_id != prev session_id`; EventID (UUIDv7) monotone within a cycle.

### 3.3 Adoption friction (honest list)

1. **`EventPayload` is `interface{}`** (`eventregistry.go:27`) — the harness still needs a type-switch or a `GetCycleID()`-style mini-interface to route payloads. This is the same per-type code an ad-hoc harness writes; adoption saves the *decode+registry+skip-policy* layer, not the per-type logic.
2. **Non-strict JSON.** `DecodePayload` uses `json.Unmarshal` (`eventregistry.go:227`) — unknown payload fields are silently ignored, so the harness cannot detect *additive writer drift* through this path. Making it strict would need a `DisallowUnknownFields` decode variant on the registry (small additive change: expose the constructor lookup, or add `DecodePayloadStrict`).
3. **Two calls, not one.** Version validation and decode are separate; a harness that forgets `ValidateEnvelopeSchemaVersion` gets silent version drift. Trivially wrapped once in `Replay`.
4. **Global registry sealing is a TODO** (`eventregistry.go:56-58`, EV-034) — irrelevant for a read-only harness but means tests using registry-reset patterns must not run in the same process as a harness relying on full registration.
5. **pertypecompat is declaration-only** — `CompatWindowHolds` is an asserted flag, not computed; the harness can *report* against it but the table won't catch a lie. (The table's real value here is the bidirectional registration test keeping it exhaustive.)

### 3.4 Recommendation

**ADOPT** (agreeing with the operator's lean). The path is dead in production but *alive in tests* — 9 test files already exercise it, the registry is exhaustively populated (169 types), and two guard tests (`pertypecompat_hqwn38_test.go`, `eventtype_coverage_gjyks_test.go`) actively keep it synchronized with the taxonomy. A replay harness is precisely the EV-033 "observational consumer" the API was speced for; adopting it gives typed decode, unknown-type skip policy, schema-version assertion, and N-1 window reporting for the cost of a thin `Replay` loop. Delete+ad-hoc-unmarshal would duplicate 4+ payload structs in the harness, lose unknown-type/version-drift detection, and orphan the guard tests' purpose. The one genuine gap (non-strict decode, §3.3.2) is a small additive registry change, not a reason to bypass. Becoming the **first production reader** also finally closes the loop the registry was built for — future payload edits get a consumer that breaks loudly.

---

## 4. ScanAfter / Filter as the replay read surface

### 4.1 Signatures and semantics

`internal/eventbus/jsonlwriter.go`:

- `func ScanAfter(path string, sinceID core.EventID) iter.Seq[core.Event]` — `jsonlwriter.go:312-353`. Yields events in **file order**, keeping only events whose raw 16-byte `event_id` is lexicographically `>` `sinceID` (`bytes.Compare` at `:336-337`). Doc: "Because EventID is a UUIDv7, lexicographic byte order is chronological order (EV-002)" (`:299-301`). Malformed lines skipped with a log warning (`:331-333`); missing file = empty log, no error (`:315-320`); pure read-side (EV-020, `:306`). Bead ref: **hk-a5sil "since_event_id replay"** (`:309`).
- `func Filter(path string, runID core.RunID) iter.Seq[core.Event]` — `jsonlwriter.go:380-413`. Yields events in file order whose **envelope** `run_id` equals `runID` (`:398`); malformed and torn-tail lines skipped (`:363-369`).

Append side: `JSONLWriter` (`jsonlwriter.go:76-295`) is O_CREATE|O_WRONLY|O_APPEND (`:107-110`), one JSON object + `\n` per `Append` (`:246-272`), never rewrites/truncates/reorders (EV-020, `:29-31`), caller-driven fsync for F-class (`:37-40`), batching drainer coalescing concurrent fsyncs (hk-5zode, `:42-75`). Torn-tail recovery is the reader's job (`:29-31, :367-369`).

### 4.2 Ordering guarantees — precise statement

- **Within one writer process:** EventIDs are monotone (EV-002a, `event.go:33-34`; generator `internal/core/eventidgen.go`, `uuid.NewV7` at `:37`), and file order = append order (single drainer goroutine, `jsonlwriter.go:125-186`).
- **Across processes:** the same `events.jsonl` is appended by (at least) the daemon's `JSONLWriter` AND each keeper's `FileEmitter` (`watcher.go:47-51` → `projectDir/.harmonik/events/events.jsonl`, `core.EventsJSONLPath` = `"events/events.jsonl"` at `internal/core/jsonlformat_hqwn58.go:20`). Each process has its **own** `EventIDGenerator`, so file order is only approximately EventID order across writers (wall-clock UUIDv7s from concurrent processes can interleave). `ScanAfter` does **not** re-sort — it yields file order and filters by `> sinceID`. A harness wanting strict global order must sort by EventID after collection (cheap at this scale) or tolerate small inversions.

### 4.3 Declared surface or incidental?

**ScanAfter is a declared, load-bearing read surface** — exported, spec-cited (EV-020/EV-002, dedicated replay bead hk-a5sil), with **15+ distinct production (non-test) callers**: `cmd/harmonik/comms.go:667`, `cmd/harmonik/decisions.go:524,:552`, `internal/digest/builder.go:371,:516`, `internal/digest/resolver.go:170`, `internal/presence/decisions.go:126`, `internal/presence/presence.go:163`, `internal/sentinel/governor.go:329`, `internal/sentinel/signals.go:214`, `internal/daemon/commsrecvhandler_nnwaa.go:220`, `internal/daemon/dashboardgather.go:160`, `internal/daemon/subscribe.go:468`, `internal/daemon/supervisorrevival_hkrnkuy.go:48`, `internal/daemon/runinflightreconcile_hkr73qr.go:100`, `internal/daemon/daemon.go:2067`, `internal/watch/ledger.go:158,:176`. It IS the project's offline-replay primitive already.

**Filter, by contrast, has zero production callers** — all hits are tests (`internal/eventbus/jsonlfilter_test.go` ×7, `internal/daemon/t11_throughput_test.go:441`, `internal/daemon/t7_parallel_smoke_test.go:435`). Filter is the run_id-keyed dead half; this reinforces §2's recommendation (a cycle-id-in-envelope-run_id design would be building on the dead surface, while payload-level cycle_id builds on the live one, ScanAfter).

### 4.4 How the Twin/harness consumes them

- **Keeper corpus (live, today):** `.harmonik/events/events.jsonl` — 238,412 lines, 78,520 `session_keeper_*` events; **508 `handoff_started`, 428 `cycle_complete`, 79 `cycle_aborted`** (≈ the "~476 keeper cycles"; 508 starts vs 507 terminals hints at already-detectable invariant gaps — exactly what the harness is for). Consumption: `ScanAfter(eventsPath, core.EventID{})` (zero ID = full history, the established idiom at `supervisorrevival_hkrnkuy.go:48`, `daemon.go:2067`), filter `strings.HasPrefix(ev.Type, "session_keeper_")`, dispatch via §3, group by `payload.cycle_id`. Incremental re-runs pass the last processed EventID as `sinceID` — the watermark pattern `internal/watch/ledger.go:176` already uses (`l.cursor`).
- **Codex corpus:** `testdata/codex-app-server/gen/` is TS/JSON-schema codegen, not an event log; the codex replay corpus for the substrate Twin should be *recorded* as the same append-only NDJSON of `core.Event` envelopes (via `JSONLWriter` or a FileEmitter-style appender), after which `ScanAfter` + the §3 harness consume it identically. Nothing new is needed on the read side: any file of one-`core.Event`-per-line parses; unknown/foreign types fall into the `ErrSkipUnknown` bucket.
- The keeper Twin test family already exists as a consumer precedent: `internal/keeper/cycle_twin_*_integration_test.go` (e.g. `cycle_twin_e2e_integration_test.go`) drives the real cycle against a fake pane — the replay harness is its offline dual over recorded corpora.

---

## 5. Risks / unknowns for Change-Design

1. **Keeper writes bypass the bus writer — no fsync, no batching.** `FileEmitter.EmitWithRunID` opens/appends/closes per event with no `Sync()` (`watcher.go:86-99`). If the 4 new events are meant to be "durable" in the F-class sense (survive crash-at-restart — and a restart cycle is exactly when the keeper process is most likely to die), FileEmitter needs a sync flag or the events must route through the daemon. If O-class is accepted, say so explicitly in the spec row. Also: emit failures are swallowed (`_ =` at every cycle.go site) — a "durable" event that silently fails to write is a spec lie; Change-Design should decide error handling.
2. **Two concurrent appenders to one file.** Daemon `JSONLWriter` + N keeper `FileEmitter`s rely on POSIX O_APPEND atomicity per line (`jsonlwriter.go:33-35`); fine for line integrity, but cross-process EventID/file-order divergence (§4.2) means replay determinism needs a sort-by-EventID step or an explicit "file order is authoritative" decision.
3. **§8 numbering is drifted in BOTH directions** — code §8.13/8.14/8.15 collide with spec §8.13/8.14/8.15, and code-invented §8.16–8.19 don't exist in `specs/event-model.md` (headings end at §8.15). Assigning "§8.20" to the new cohort perpetuates squatting on numbers the spec has never allocated. Change-Design must either (a) amend `specs/event-model.md` to add real §8.16–§8.20 sections (renumbering the keeper cohort out of the §8.13 collision), or (b) allocate §8.20 in the spec and accept the historical comment drift. Doing neither reproduces the exact defect this pass was asked to verify.
4. **Cohort-test ambiguity.** Keeper events are excluded from `allEventTypeCohort` (126 entries, 0 keeper) and the EV-027 count guard — but `eventtype_coverage_gjyks_test.go:17-19` *claims* every eventtype.go constant must be in the cohort. The 4 new events force the question: follow the (silent) keeper-exclusion precedent or fix the cohort to include all 18+4 keeper types (which also bumps the EV-027 amendment count). Recommend deciding explicitly; the test's stated contract is currently false.
5. **§8.9 acceptance criteria tension.** `specs/event-model.md:278-291` requires cross-subsystem consumers and *lifecycle-boundary* (not intra-lifecycle) signals; the 4 new events are cycle-interior by design. The replay harness IS the cross-subsystem consumer (§8.9(a)), but §8.9(b) needs an argued exception. The EV-026 internal-events carve-out is NOT an escape: EV-026 events "MUST NOT be passed to RegisterEventType" (`eventregistry.go:21-26`), which would kill the typed-decode adoption. Resolution: register them (they cross the process boundary keeper→jsonl→harness, so they are not EV-026-internal) and write the §8.9 justification into the spec amendment.
6. **`model_done` has no existing detection point** (§1.4.5). Every other new event lands on an existing code seam; this one needs new pane-idle/turn-complete detection (CrispIdleFn polling between confirm and clear, or a nonce-style completion marker). Highest-effort and least-defined of the four.
7. **Non-strict decode** (§3.3.2): replay can't detect additive payload drift without a `DisallowUnknownFields` registry variant; decide whether that additive change is in-scope.
8. **Doc drift trap:** `pertypecompat_hqwn38.go:26` prescribes calling nonexistent `SetPayloadSchemaVersion` for version bumps; the real mechanism is `RegisterEventTypeAtVersion` (`eventregistry.go:114`). Fix the comment or someone follows it during this work.
9. **Historical corpus asymmetry:** the ~476 recorded cycles predate the 4 new events and (for 13 of 18 types) lack `cycle_id`. Invariant checks must be version-aware: full sequence invariants apply only to post-change corpora; pre-change corpora get the weaker started/terminal pairing checks.
10. **`session_keeper_operator_attached` precedent** (registered, emitter deleted, `cycle.go:1498-1502`): registered-but-unemitted types are tolerated — the harness should report them as "registered, never observed" rather than failing, or the taxonomy rots invisibly.
