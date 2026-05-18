# Event-model research findings — extqueue spec edit (entry 5)

## Questions

1. How are new event types registered in Go, and how does that connect to the spec?
2. What is the spec-side declaration template for a new event type?
3. Schema-version policy for new types and additive changes?
4. Identity correlation across a related event chain (precedent for `queue_id` on `run_*`)?
5. `source_subsystem` registration process and naming convention?
6. Optional vs required field policy?
7. Durability class for each queue_* event?

## Findings (file:line)

### 1. Event registration pattern (Go side)

- `internal/core/eventreg_hqwn59.go:26-36` — a single package `init()` calls eight `register*Events()` helpers, one per §8 subsection (8.1 through 8.8). A ninth helper `registerReviewLoopEvents` covers §8.1a (lines 35, 257-265).
- Each helper calls `mustRegister("<type>", func() EventPayload { return &XxxPayload{} })` — see `eventreg_hqwn59.go:53-63` for §8.1.
- `mustRegister` (`eventreg_hqwn59.go:276-280`) wraps `RegisterEventType` and panics on duplicate/nil; panic is acceptable because init() runs before any event is emitted (EV-034).
- Each helper has a banner docstring enumerating per-row durability class verbatim from the §8 table (`eventreg_hqwn59.go:40-51` for run-lifecycle). This is the load-bearing cross-check catching drift between code and spec.
- Payload-type convention: `Xxx` event → `XxxPayload` Go type, declared in its own file (`agentevents_hqwn59.go`, `budgetevents_hqwn59.go`, `cpevents_hqwn59.go`, `busevents_hqwn59.go`). Suffix `_hqwn59` is the bead codename for the wave that introduced the registry framework.
- Spec hook: `specs/event-model.md:568-590` (EV-032, EV-033, EV-034, EV-034a) declares the tagged-union shape, deterministic dispatch, startup-time registration, and the `source_subsystem` duplicate-fail-startup rule. `eventreg_hqwn59.go:6-10` cites EV-032/EV-034.

### 2. Spec-side event declaration template

Per `specs/event-model.md:79-91` (§8.1 table), each row has six columns:

```
| # | Type | Dur | Emitter | Typical consumers | Payload fields |
```

Numeric index (`8.1.1`), backtick-quoted type, one-letter durability class (F/O/L), emitter subsystem, comma-separated consumers, payload-field list with `?` suffix for optional. Immediately after each §8.x table the spec emits a `> Section Axes (§8.x …)` paragraph naming class-default four-axis tagset and per-row exceptions (e.g. `event-model.md:93,118,165`).

Best templates to copy:
- `event-model.md:81` for `run_started` — minimal field set with optional `bead_id?`.
- `event-model.md:107-112` for the six §8.1a review-loop types — strongest precedent for a newly added cohort under one emitter, mixed F/O durability, with a dedicated emission-ordering paragraph (`event-model.md:120`).
- `event-model.md:185-190` for workspace events — paired-phase `status` field pattern (§8.9(h)).

YAML payload schemas live in §6.3 (`event-model.md:730-994`). `run_started` at lines 734-743; `workspace_merge_status` at 802-812. Each YAML lists field-name, scalar/enum/UUID type, and `| null` for optional. Enums declared inline (`event-model.md:807`).

### 3. Schema-version policy

- `event-model.md:540-544` (EV-028): every event AND envelope carry a `schema_version` integer; per-type versions evolve independently.
- `event-model.md:546-550` (EV-029): N-1 readability for every event type AND envelope; 71+ independent contracts.
- `event-model.md:1000-1010` (§6.4 table) — verbatim rules relevant to extqueue:
  - "Add optional field | No | Accept; ignore unknown fields on older readers."
  - "Add required field | Yes | Older readers fail closed with typed error on missing field."
  - "Add enum variant (non-required semantics) | No | … unknown variants as non-fatal."
- New event types start at `schema_version=1`. JSONL example `event-model.md:707-720` shows `"schema_version": 1` for `checkpoint_written`. Per-type bumping is independent (`event-model.md:998`).
- Adding `queue_id`/`queue_group_index` to existing `run_*` as OPTIONAL is non-breaking per §6.4 row 1 — no version bump required.

### 4. Identity-field correlation precedent

- `run_id` is on the envelope (`event-model.md:285` EV-001) AND on every §8.1 row's payload (`event-model.md:81-91`).
- §8.1a is the strongest precedent: `event-model.md:101-103` declares "A single `run_id` covers the entire review-loop cycle"; multiple `session_id`s exist under the umbrella `run_id`. Same shape works for queue: single `queue_id` covers the chain, one `run_id` per dispatched item underneath.
- `TraceContext` (`event-model.md:664-668`) carries `parent_event_id`/`root_event_id` — SHOULD-populate, best-effort. Primary correlation is payload-side identity fields, not trace_context.
- Workspace precedent: `workspace_id` cross-event correlator vs `run_id` per-lease correlator (`event-model.md:185-190`). `queue_id` ↔ `workspace_id`, `queue_group_index` ↔ run/lease are the structural analogues.

### 5. Source-subsystem identifier registration

- Code: `internal/core/subsystemregistry_hqwn44.go:43-55`. API is `RegisterSourceSubsystem(id string) error`; duplicates return `ErrDuplicateSourceSubsystem` (line 14) intended to fail startup per EV-034a (`event-model.md:586-590`).
- Naming convention is Go-package-identifier per EV-004 (`event-model.md:319-323`): `"github.com/harmonik/internal/<subsystem>"`. Test fixtures use `…/internal/orchestrator`, `…/internal/daemon` (`subsystemregistry_hqwn44_test.go:23,33`).
- No `queue` or `queue-manager` identifier is registered yet — nothing under `internal/` calls `RegisterSourceSubsystem` outside tests. Convention is bare one-word package names. Recommendation: `github.com/harmonik/internal/queue` (NOT `queue-manager`).

### 6. Optional vs required field policy

- `event-model.md:81` precedent: `bead_id?` on `run_started`. The `?` in the §8 column denotes optional; the §6.3 YAML renders as `bead_id: <String> | null` (`event-model.md:740`).
- `workflow_mode` field rule (`event-model.md:95`) is the most explicit precedent: "The field is OPTIONAL on `run_started` / `run_completed` / `run_failed` for backward compatibility with v0.3.x consumers … it is REQUIRED on every §8.1a review-loop event payload because those events are emitted only when `workflow_mode = review-loop`."
- Apply same pattern: on `run_*`, `queue_id?` and `queue_group_index?` are OPTIONAL (omitted when run is non-queued). On every new `queue_*` event, `queue_id` is REQUIRED; `queue_group_index` is REQUIRED when the event is group-scoped (queue_submitted omits it; group_started / item_* / group_completed / paused / deferred carry it).

### 7. Durability class assignment

Per §4.4 EV-016 (`event-model.md:433-438`): "events whose loss forces reconciliation into work greater than the cost of a disk sync are `fsync-boundary`; high-cardinality granular events … are `lossy-tail-ok`; everything else is `ordinary`."

Proposed assignment for the 9 candidate types:

| Event | Class | Rationale |
|---|---|---|
| `queue_submitted` | F | Defines the execution plan; loss orphans all subsequent group/item events. Mirrors `daemon_started` (F) as session-defining landmark. |
| `queue_group_started` | O | Observable progress signal; reconstructible from item-level dispatch + ledger. Mirrors `state_entered` (O). |
| `queue_item_dispatched` | O | Per-item observability; `ClaimBead` is the authoritative landmark. Mirrors `node_dispatch_requested` (O). |
| `queue_item_completed` | O | Tandem with `run_completed` (F); F-class run event carries durability. **See risk below — recommend dropping.** |
| `queue_item_failed` | O | Tandem with `run_failed` (F). **Same — recommend dropping.** |
| `queue_group_completed` | F | Group-boundary landmark; gates advancement. Loss leaves advance/pause state ambiguous after crash. |
| `queue_paused` | F | Hard execution stop, discrete landmark (not paired-phase). |
| `queue_appended` | O | Stream-mutation observability; `queue.json` is authoritative for contents. |
| `queue_item_deferred_for_ledger_dep` | O | Informational; ledger authoritative for the blocker. |

Cross-check: §8.7.6 `operator_pause_status` is O despite being a state landmark, because §8.9(h)'s paired-phase rule routes durability through `daemon_shutdown` (F). `queue_paused` is not paired-phase, so F defensible. Open: collapse `queue_paused`+`queue_resumed` into a single `queue_pause_status` with `status: paused|resumed`? That would invoke §8.9(h), drop class to O, and align with `workspace_merge_status`/`operator_pause_status`.

## Patterns to adopt

- Add a new helper `registerQueueEvents()` in `internal/core/eventreg_hqwn59.go`, called from `init()` alongside the other eight. Assume §8 subsection number `§8.10` (next free after §8.8 + the inserted §8.1a). Payload types: `internal/core/queueevents_<codename>.go`.
- Spec §8.10 row template: copy §8.1a shape (table + Section Axes paragraph + emission-ordering paragraph). The ordering paragraph is load-bearing: `queue_submitted` → `queue_group_started` → `queue_item_dispatched` → … → `queue_group_completed` is normative.
- §6.3 YAML: one block per type. Required = no `| null`; optional = `| null`.
- §6.5 co-ownership: add bullet "Queue-lifecycle events (§8.10): emission rules in [queue-model.md §<TBD>]. All entries are queue-manager-emission-owned." Mirror `event-model.md:1018-1027`.
- For `run_*` additive change: append `queue_id` (UUID or null) and `queue_group_index` (Integer or null) to §6.3 `run_started` YAML; declare optional below; cite §6.4 row 1 as non-breaking justification.
- Register `github.com/harmonik/internal/queue` as source_subsystem in queue-manager init.

## Risks / conflicts

- **EV-027 amendment burden** (`event-model.md:530-536`). Nine new types is a heavy amendment surface. Design-pass triage of "v0.1 may not need all 9" directly addresses this. Recommendation: cut to minimum (`queue_submitted`, `queue_group_completed`, `queue_paused` load-bearing; per-item events may collapse into `metric` per §8.8.1 escape hatch at `event-model.md:274`).
- **§8.9(g) cross-citation.** Each surviving queue_* type needs a citation in a sibling spec's emission section. Plan citations in execution-model.md §6.5.
- **§8.9(h) paired-phase rule.** `queue_paused`/`queue_resumed` may need collapsing into `queue_pause_status` with `status: paused|resumed`. Conflicts with `02-components.md:84` which lists them as separate types. Flag for design-pass.
- **EV-016a tandem-emission risk** (`event-model.md:440-444`). "Producers requiring two events durably persisted together MUST emit a single event carrying both payloads OR resolve via authoritative stores." Emitting `run_completed{queue_id, queue_group_index}` (F) alone — no separate `queue_item_completed` — is cleaner. **Strong recommendation: drop `queue_item_completed` and `queue_item_failed`; rely on `queue_id` on `run_*`.** Cuts new-type count from 9 to 7.
- **Per-emission fsync cost.** Class-F for `queue_submitted` and `queue_group_completed` is one fsync each per EV-016a. Linear with group count; acceptable at MVH scale; flag for post-MVH batching.
- **Queue.json `schema_version` is separate** from event `schema_version`. Lives in queue-model.md. Don't conflate.
- **Naming.** Use `queue` not `queue-manager` for source_subsystem to match the `orchestrator`/`daemon` one-word convention.
