# event-model — Change Design

## Current state

`specs/event-model.md` §8 today lists eight cohorts (8.1 run, 8.1a review-loop, 8.2 control-point, 8.3 agent/handler, 8.4 budget, 8.5 workspace, 8.6 reconciliation, 8.7 operator/daemon, 8.8 observability). Each cohort is rendered as a six-column table — `# | Type | Dur | Emitter | Typical consumers | Payload fields` (template at event-model.md:79) — followed by a `> Section Axes (§8.x …)` paragraph naming the cohort's default four-axis tags and per-row exceptions, and (for §8.1a) an emission-ordering paragraph (event-model.md:120) declaring the normative ordering across the cohort. §6.3 carries per-type YAML payload schemas with required fields as bare scalar types and optional fields suffixed `| null`.

No `queue_*` event type exists, and `source_subsystem` `github.com/harmonik/internal/queue` is not registered in `internal/core/subsystemregistry_hqwn44.go`.

Obligations on new types:

- **EV-027** (event-model.md:530) — every new cross-bus type requires a foundation amendment supplying type name, emitter, typical consumers, payload fields, four-axis tags, durability class, and §8.9 acceptance evidence; the amendment MUST also include the §8 row, the emitter-spec edit adding the emission requirement, and at least one consumer cited in another spec.
- **EV-028** (event-model.md:540) — every event AND envelope carries `schema_version`; new types start at `schema_version=1`.
- **EV-034a** (event-model.md:586) — each subsystem registers its `source_subsystem` identifier at daemon init; duplicates fail startup.
- **§6.4 row 1** — adding an optional field to an existing type is non-breaking; no schema bump required.

## Target state

A new §8.10 "Queue lifecycle" cohort, six new event types, and optional additive fields on the existing §8.1 run-lifecycle payloads. Cross-cutting decisions: drop `queue_item_completed` and `queue_item_failed` per EV-016a tandem-emission risk (use `queue_id` + `queue_group_index` on `run_*` instead); drop `queue_item_dispatched` because it is reconstructible from `run_started{queue_id, queue_group_index}` (a per-run F-class landmark dominates the per-item O-class observability event under §8.9 criterion (c)); keep `queue_paused` as a one-shot event in v0.1 (the §Q7 paired-phase collapse to `queue_pause_status` is rejected below).

### §8.10 cohort — six types

| # | Type | Dur | Emitter | Typical consumers | Payload fields |
|---|---|---|---|---|---|
| 8.10.1 | `queue_submitted` | F | queue | audit, observability, orchestrator-core | `queue_id`, `submitted_at`, `group_count`, `total_bead_count`, `schema_version` (queue.json) |
| 8.10.2 | `queue_group_started` | O | queue | audit, observability, orchestrator-core | `queue_id`, `group_index`, `group_kind` (enum: `wave` / `stream`), `item_count`, `started_at` |
| 8.10.3 | `queue_group_completed` | F | queue | audit, observability, orchestrator-core | `queue_id`, `group_index`, `final_status` (enum: `complete-success` / `complete-with-failures`), `success_count`, `fail_count`, `completed_at` |
| 8.10.4 | `queue_paused` | F | queue | audit, observability, orchestrator-core, operator-observability | `queue_id`, `group_index`, `fail_count`, `paused_at`, `reason` (enum: `group_failure` / `operator_drain`) |
| 8.10.5 | `queue_appended` | O | queue | audit, observability, orchestrator-core | `queue_id`, `group_index`, `appended_bead_ids` (String[]), `appended_at` |
| 8.10.6 | `queue_item_deferred_for_ledger_dep` | O | queue | audit, observability, orchestrator-core | `queue_id`, `group_index`, `bead_id`, `blocker_bead_id`, `detected_at` |

> Section Axes (§8.10 Queue lifecycle): All §8.10 event emissions are mechanism-tagged. Class F entries (`queue_submitted`, `queue_group_completed`, `queue_paused`) are fsync-backed because loss orphans the queue's execution plan, group-boundary advance decision, or hard pause landmark respectively (per EV-016). Class O entries (`queue_group_started`, `queue_appended`, `queue_item_deferred_for_ledger_dep`) are best-effort: each is reconstructible from a sibling class-F landmark (group_started from group_completed of predecessor + queue.json; appended from queue.json mutation history; deferred from ledger state + queue.json). Default per-entry Axes — class F: `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent`. Class O: `llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent`.

> Emission ordering (§8.10). For a single queue's lifecycle the emission order MUST be: (a) `queue_submitted` (one); (b) immediately followed by `queue_group_started{group_index:0}` plus any `queue_item_deferred_for_ledger_dep` events arising from QM-025 submit-time validation; (c) per dispatched bead, the §8.1 chain `run_started{queue_id, queue_group_index} → … → run_completed{queue_id, queue_group_index}` OR `run_failed{queue_id, queue_group_index}` carries per-item lifecycle (no separate `queue_item_*` emission); (d) when every item in the active group reaches terminal, `queue_group_completed`; (e) if any item failed, `queue_paused{reason:group_failure}` MUST follow `queue_group_completed` in that emission order (group_completed first, paused second); (f) otherwise, `queue_group_started{group_index:N+1}` follows; (g) `queue_appended` MAY interleave at any time on a stream group whose status is `pending` or `active`. The §8.1 terminal `run_completed`/`run_failed` for the last item of a group MUST precede `queue_group_completed` for that group, never follow it.

### §6.3 payload YAML additions

```yaml
# queue_submitted
queue_id: <UUID>
submitted_at: <Timestamp>
group_count: <Integer>
total_bead_count: <Integer>
schema_version: <Integer>          # version of queue.json document per queue-model.md §2; distinct from event envelope schema_version
```

```yaml
# queue_group_started
queue_id: <UUID>
group_index: <Integer>
group_kind: <enum: wave | stream>
item_count: <Integer>
started_at: <Timestamp>
```

```yaml
# queue_group_completed
queue_id: <UUID>
group_index: <Integer>
final_status: <enum: complete-success | complete-with-failures>
success_count: <Integer>
fail_count: <Integer>
completed_at: <Timestamp>
```

```yaml
# queue_paused
queue_id: <UUID>
group_index: <Integer>
fail_count: <Integer>
paused_at: <Timestamp>
reason: <enum: group_failure | operator_drain>
```

```yaml
# queue_appended
queue_id: <UUID>
group_index: <Integer>
appended_bead_ids: <String[]>
appended_at: <Timestamp>
```

```yaml
# queue_item_deferred_for_ledger_dep
queue_id: <UUID>
group_index: <Integer>
bead_id: <String>
blocker_bead_id: <String>
detected_at: <Timestamp>
```

### §6.3 amendments to existing run-lifecycle payloads

Append two OPTIONAL fields to `run_started`, `run_completed`, and `run_failed`:

```yaml
queue_id: <UUID> | null              # populated when the run was dispatched from a queued submission
queue_group_index: <Integer> | null  # populated alongside queue_id; identifies the group within the queue
```

These additions are non-breaking per **§6.4 row 1** ("Add optional field — No"); no per-type `schema_version` bump is required. Older readers see the fields as unknown and ignore them per §6.4's reader obligation. The optional-on-`run_*` semantic mirrors the `workflow_mode` precedent at event-model.md:95 ("OPTIONAL on `run_started` / `run_completed` / `run_failed` for backward compatibility … REQUIRED on every §8.1a event") — same pattern, same justification, except the required-on-cohort obligation here applies to the §8.10 events (where `queue_id` is REQUIRED on every payload, and `queue_group_index` is REQUIRED on every group-scoped payload, omitted only on `queue_submitted` which is queue-scoped).

### §6.5 co-ownership bullet

Add to event-model.md §6.5:

> Queue-lifecycle events (§8.10): emission rules in [queue-model.md §4 QM-010..012 (identity), §6 QM-020..026 (validation), §7 (append semantics), §8 (lifecycle)]. All six entries (`queue_submitted`, `queue_group_started`, `queue_group_completed`, `queue_paused`, `queue_appended`, `queue_item_deferred_for_ledger_dep`) are queue-emission-owned; this spec is normative for their payload shape, ordering rule, and durability class.

### source_subsystem registration

The queue subsystem MUST register `github.com/harmonik/internal/queue` via `RegisterSourceSubsystem` at daemon init per EV-034a. The identifier follows the EV-004 Go-package-identifier convention and the one-word `orchestrator`/`daemon` precedent at `subsystemregistry_hqwn44_test.go:23,33`. Spec text: amend the `internal/core/subsystemregistry_hqwn44.go` startup-expectations block (currently silent on the queue subsystem) to add the queue identifier alongside the existing entries; duplicate-registration MUST fail startup per the existing `ErrDuplicateSourceSubsystem` contract.

## Rationale

**Surviving F-class types.**

- `queue_submitted` (F): defines the execution plan; loss orphans every subsequent group/item event in the chain. Mirrors `daemon_started` (F) as a session-defining landmark. Loss-forces-reconciliation per EV-016 because without it the orchestrator cannot correlate `run_*` events to the queue.
- `queue_group_completed` (F): group-boundary advance landmark. Loss leaves the advance/pause state ambiguous after crash — recovery would have to scan every `run_*` event for that group's items and re-derive the success/failure tally; that is greater than the cost of one fsync per EV-016.
- `queue_paused` (F): hard execution stop, discrete landmark (not paired-phase per §8.9(h)). Loss would silently hide the pause from a tailing orchestrator; the pause is operator-visible behavior that MUST be durable per EV-016.

**Surviving O-class types.**

- `queue_group_started` (O): observable progress signal; reconstructible from the previous group's `queue_group_completed` (F) plus queue.json. EV-016 threshold not crossed.
- `queue_appended` (O): mutation-observability signal; queue.json is the authoritative store for stream contents per QM-001. Loss tolerable.
- `queue_item_deferred_for_ledger_dep` (O): informational; the ledger remains the authority for `blocks` edges. Loss tolerable because the dispatcher re-evaluates eligibility from ledger state on each dispatch tick.

**Dropped types.**

- `queue_item_completed` / `queue_item_failed` — DROPPED. Per **EV-016a** (event-model.md:440), producers requiring two events durably persisted together MUST emit a single event carrying both payloads OR resolve via authoritative stores. Emitting `queue_item_completed` (O) alongside `run_completed` (F) is a tandem-emission anti-pattern: the F-class run event is the durable landmark and the O-class queue mirror adds no observability that isn't already carried by `run_completed{queue_id, queue_group_index}`. Drop them; the §8.1 chain with the new optional fields covers per-item observability.
- `queue_item_dispatched` — DROPPED. Reconstructible from `run_started{queue_id, queue_group_index}` (F, event-model.md:81) which is emitted at the same moment by orchestrator-core. §8.9 criterion (c) (granularity: cross-subsystem consumer requires per-boundary access rather than a single summary event) is not met because no consumer needs both a dispatch event AND `run_started` — they coincide. Dropping it also avoids a second tandem-emission risk identical to the `queue_item_completed` case.
- `queue_resumed` — DROPPED for v0.1. v0.1 ships no `queue.resume` operation per queue-model-design.md §5 / §8 (pause-by-failure is terminal until daemon restart + re-submit). The type is reserved for v0.2 alongside the resume primitive.

**§Q7 rejection: `queue_paused` does NOT collapse into `queue_pause_status`.**

Research §Q7 (findings.md:89) flagged a possible §8.9(h) paired-phase collapse: emit a single `queue_pause_status` with `status ∈ {paused, resumed}` and downgrade durability to O, mirroring `workspace_merge_status` (event-model.md:185-190) and `operator_pause_status` (event-model.md:230). Rejected for v0.1 on two grounds: (1) §8.9(h) paired-phase requires the resume partner exist; v0.1 has no `queue.resume` operation, so there is no second phase to pair with. A single-status enum with only one inhabited value is a degenerate paired-phase that buys no durability concession — the event is still effectively one-shot. (2) The pause landmark is operationally critical: it gates orchestrator drain decisions and operator visibility, and §6.4's enum-variant-addition rule (row 7, non-breaking) means v0.2 can add `queue_resumed` as a sibling type AND collapse to `queue_pause_status` at that point — or keep them separate — without breaking v0.1 consumers. v0.2 path: introduce `queue_resumed` (F) when `queue.resume` ships; at the v0.2 amendment, re-evaluate the collapse-to-`queue_pause_status` question with both phases now extant. Until then, `queue_paused` stands alone at class F.

## Requirements traceability

| 02-components.md §5 requirement | EV-NNN / §8.10 row |
|---|---|
| `queue_submitted` payload (queue id, group count, total bead count) | §8.10.1 |
| `queue_group_started` (queue id, group index, kind, item count) | §8.10.2 |
| `queue_item_dispatched` | DROPPED — reconstructible from `run_started{queue_id, queue_group_index}` (§8.1.1 + new optional fields) |
| `queue_item_completed` (tandem with `run_completed{success:true}`) | DROPPED per EV-016a — covered by `run_completed{queue_id, queue_group_index}` (§8.1.2) |
| `queue_item_failed` (tandem with `run_failed`) | DROPPED per EV-016a — covered by `run_failed{queue_id, queue_group_index}` (§8.1.3) |
| `queue_group_completed` (success/fail count, final status) | §8.10.3 |
| `queue_paused` (queue id, group index, fail count) | §8.10.4 |
| `queue_resumed` | DEFERRED to v0.2 (no resume operation in v0.1) |
| `queue_item_deferred_for_ledger_dep` (informational) | §8.10.6 |
| `queue_appended` (stream mutation observability) | §8.10.5 |
| `run_started` / `run_completed` / `run_failed` carry queue identity | §6.3 amendments (optional `queue_id`, `queue_group_index` per §6.4 row 1) |
| `source_subsystem` registration | `github.com/harmonik/internal/queue` per EV-034a + EV-004 |

All 02-components.md §5 requirements covered. Surface trimmed from 9 candidate types to 6 §8.10 entries plus 2 optional fields on 3 existing types.
