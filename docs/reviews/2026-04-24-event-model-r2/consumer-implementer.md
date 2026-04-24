# Consumer Implementer Review — Event Model r2

- Spec: `specs/event-model.md` v0.2.0 (taxonomy-first, 935 lines)
- Lens: implement an **event subscriber** in Go — the downstream side of the bus
- Date: 2026-04-24
- Distinct from r1 implementer (producer/bus internals); this lens reads `Subscribe`, handles delivery, survives restart, and consumes payloads as declared in §6.3 / §8
- Requirements sampled: EV-009, EV-010/011/011a/012/013/014a/014b, EV-023, EV-028/029, EV-032/033, §6.3 payload shapes, §7 protocols, §8 consumer columns

## Verdict

**Implementable with gaps that each of my five concrete consumers will hit at wiring time.** The three-class taxonomy, payload registry, and dispatch sequence (§7.1) are coherent enough to write a subscriber against. But the `Subscribe` API signature, the handler-method shape, the redelivery-boundary guarantees, and several `typical consumers` / payload mismatches in §8 will each force an invention. EV-INV-002 (two-sided covenant) is **stated** but the spec does not actually give the consumer-side implementer the hooks it needs to honor it — there is no recovery-startup signal, no "last processed `event_id`" checkpoint contract, and no guidance on whether the bus delivers the tail of JSONL on restart or starts fresh. These will decide whether the audit-consumer use case works at all.

The five use cases below are drawn from the `typical consumers` columns in §8 (observability, reconciliation, audit, improvement-loop, operator-observability) — i.e. actual consumers the spec anticipates, not hypothetical ones. Each is graded on what the spec lets me build today.

## Scope of the consumer lens (distinct from r1)

- r1 implementer focused on: bus internals, producer-side `Emit`, JSONL writer, envelope generation, registry registration.
- This review focuses on: what it means to **subscribe**, **handle**, **survive restart**, **interpret payloads declared in §6.3**, and **deliver the guarantees §4 promises to consumers**.
- Overlap with r1 (bus API, EV-032/033) is unavoidable but I only flag where the consumer-side contract is unclear, not where producer-side would invent a different answer.

## Concrete subscriber sketch (what the spec makes me invent)

Before walking the five use cases, here is the subscriber shape I would write against v0.2 today. It is the union of what the spec implies and what it leaves blank:

```go
// Spec gives me this (EV-032/033):
type Event struct { /* envelope fields */ Payload json.RawMessage }

// Spec does NOT give me this — I have to invent it:
type ConsumerClass int
const (ClassObserver ConsumerClass = iota; ClassAsync; ClassSync)

type Subscription struct {
    Name    string                        // consumer identifier (EV-009)
    Types   []EventType                   // enumerate, or... wildcard?
    Class   ConsumerClass
    Handler func(context.Context, Event) error
}

// What about offset / replay-from? Nothing in the spec.
// What about "tell me when drain is complete"? Nothing in the spec.
```

Every implementer will choose a slightly different `Subscription` shape. The spec should nail this down because it is the single widest surface area consumers touch.

## Use case 1 — Observability logger (all types, observer)

Consumer: subscribes to every §8 type, writes structured logs to a sink. Class = `observer` (EV-013 default).

**What works:**
- EV-032/033 give a clean tagged-union to switch on: `event.Type` + registry lookup → typed payload. A logger only needs the envelope plus `json.RawMessage` passthrough for most types, so it can skip registry decode for unknown types (consistent with EV-033's "surface `ErrUnknownEventType` and skip the event (observational consumers)").
- Redaction is spec'd as bus-side (EV-035), so the logger does not re-implement redaction. Good.
- `timestamp_wall` being advisory (EV-006) for cross-process ordering is fine for a logger; it will sort by `event_id` for stable UI display.

**What breaks:**
- **Subscription wildcard is not spec'd.** EV-009 says a consumer "MUST register... the event types it consumes." The envelope interface (§6.1) shows `Subscribe(sub Subscription)` but `Subscription` fields are not named. Is there an `AllTypes` sentinel, a "types: []EventType" list, or a predicate? An observability consumer wants all 69 current types plus any future ones. If the subscription is enumerated, a new event type added via amendment silently bypasses the logger until the logger is redeployed. This is a real operational hazard for the one consumer whose job is totality.
- **Observer dispatch concurrency is underspecified.** §7.1 says "FANOUT\_OBSERVERS(event) — goroutine-per-observer." Per-event or per-consumer goroutine? If per-event, a slow observer backs up the bus's goroutine scheduler. If per-consumer with its own queue, there is no bound declared (EV-011's 1024 default is explicitly "per-consumer dispatch queue depth" for async — does it apply to observers?). EV-011a's overflow rules talk about "async queues"; observer queues are silent.
- **Tag filter on the subscriber side.** A logger often wants "everything at log-level warning-or-higher." The spec carries four-axis tags on the §8 row (EV-031) but tags live "in the Go registry" — there is no public accessor in the envelope. A subscriber cannot filter by `io-determinism` or `replay-safety` without reaching into internals.

## Use case 2 — Reconciliation divergence detector

Consumer: subscribes to `store_divergence_detected` (§8.6.8), `checkpoint_written` (§8.1.7), `transition_event` (§8.1.6), `workspace_merge_status` (§8.5.3), `run_completed` / `run_failed`. Class = `asynchronous` (mutates Beads / emits verdicts).

**What works:**
- `store_divergence_detected` payload (§6.3) carries `post_crash_window: Boolean`, which is exactly what a detector needs to suppress false-positive noise in the lossy-tail window. EV-023 gives the determination rule and the `daemon_started` landmark.
- `transition_event` carries `commit_hash` and `transition_kind`, which makes git corroboration trivial.
- `checkpoint_written` carries `bead_id?` for cross-store check against Beads. Good.

**What breaks:**
- **EV-023 is a producer-side rule, not a consumer-side rule.** It tells the reconciliation **detector** to set `post_crash_window`, but my consumer here is downstream of detection. If an investigator agent is the detector, the consumer is reading already-stamped events. Fine — but the spec does not name this split in §8.6.8's emitter column (`daemon-core`); it implies the daemon flags `post_crash_window` without explaining how the daemon knows where the last durable fsync was. A consumer has to trust the stamp, but if the stamp is computed wrong (lossy-tail gap not measured) the consumer has no second line of defense.
- **Ordering across `checkpoint_written` and `store_divergence_detected` is unstated.** A divergence may be detected referencing a commit that the consumer will see a `checkpoint_written` event for three events later. The partial-order contract (EV-008) guarantees only UUIDv7 ms precision cross-process; within process it is strict. But `checkpoint_written` is emitted by orchestrator-core and `store_divergence_detected` by daemon-core — the spec does not say whether these are the same process. Assumed same daemon process but should be spelled out for a consumer that correlates the pair.
- **No "wait until quiescent" primitive.** A reconciliation consumer wants to know "I've drained every event up to time T." No such API in §6.1's `EventBus` interface. Without it, the detector cannot distinguish "I haven't seen a matching `checkpoint_written` yet" from "there is no matching `checkpoint_written`."

## Use case 3 — Improvement loop (post-MVH, schema-stability lens)

Consumer: subscribes to `agent_output_chunk`, `budget_accrual`, `run_completed`, `outcome_emitted`, `transition_event`. Trains / analyzes offline.

**What works:**
- §8.9 explicitly calls out `agent_output_chunk` and `budget_accrual` as "retained fine-grained for the improvement-loop subsystem." Good, the taxonomy anticipates the consumer.
- Per-type `schema_version` with N-1 compatibility (EV-029) means a training pipeline pinned to N-1 binary can still read N's output for one version — enough runway to stage upgrades.
- `session_id` is declared UUIDv7 and opaque to non-handler consumers (§6.3 note). Stable type. Good.

**What breaks:**
- **OQ-EV-004 is a landmine for this consumer.** Default-if-unresolved: "No per-payload version field for MVH. **Note:** downgrade is lossy." An offline training pipeline consumes historical JSONL from earlier daemon versions — exactly the case OQ-EV-004 flags. The training job cannot distinguish payload versions from the wire alone. "Revisit when external tooling needs Go-registry-free consumption" describes this consumer. For MVH it will work; for improvement-loop-v1 it will not, unless the consumer is pinned to the daemon's current Go registry. Call out explicitly.
- **`agent_output_chunk.chunk_digest` is optional.** A replay-safe training pipeline wants a stable dedup key; without a digest, `(run_id, session_id, chunk_index)` is the fallback but chunk-index monotonicity across restarts is not spec'd. Handler-contract may spec it; event-model should cross-ref.
- **`budget_accrual.cost_basis` has no enum.** §8.4.2 lists `cost_basis` as a payload field but §6.3 does not declare it. A training pipeline aggregating costs across handler versions needs a stable vocabulary. Either define the enum or cross-ref handler-contract.
- **Timestamp stability across replays.** Improvement loop ingests JSONL historically. The spec guarantees `event_id` monotonicity intra-process (EV-002a) but says nothing about whether `timestamp_wall` is stable across re-reads — it is, since JSONL is append-only (EV-020), but the consumer MUST trust that. Worth stating under replay semantics.

## Use case 4 — Audit consumer (totality + durability)

Consumer: subscribes to every event type, appends to an immutable audit sink. Class = `asynchronous` (retries matter).

**This is where EV-INV-002 does or does not deliver.** EV-INV-002: "Event-loss between fsyncs is acceptable; consumers MUST handle it... two-sided operational covenant." Paired with EV-014b (idempotent on recovery + dead-letter replay).

**What works on producer side:**
- `fsync-boundary`-class events survive crash (EV-016). Audit-critical events (`run_started`, `run_completed`, `run_failed`, `transition_event`, `checkpoint_written`, `workspace_merge_status`, daemon-lifecycle events) are all `F`-class. A hard crash loses only `ordinary` and `lossy-tail-ok` events in the unflushed window.
- Spill files for `fsync-boundary` overflow (EV-011a) mean audit does not lose boundary events to back-pressure.

**What breaks on consumer side:**
- **No "last acknowledged `event_id`" checkpoint contract.** EV-014b says "consumers MUST be idempotent on recovery." But the bus interface (§6.1) has no `Ack` / `Commit` / `Checkpoint` method. How does an audit consumer, on daemon restart, know whether events E1..E100 were delivered-and-persisted-to-audit-sink versus queued-and-lost? If the bus restarts from "current" with no replay, an async consumer that crashed mid-batch loses the in-flight window even for `fsync-boundary` events that are safely in JSONL. The two-sided covenant as written is a **consumer obligation to tolerate** loss, not a bus obligation to **replay from JSONL on consumer restart**. This is a real gap.
- **`DeadLetterReplay` is operator-initiated (§6.1).** Automatic redelivery from JSONL on startup is not declared. An audit consumer that wants totality needs either (a) a bus guarantee of "on Subscribe, I replay JSONL from your last-acked `event_id`," (b) a side-channel JSONL reader it runs itself, or (c) operator intervention. The spec should pick one.
- **Spill files are per-consumer but replay is manual.** `.harmonik/events/spill-<consumer>.jsonl` (EV-011a) is created when a `fsync-boundary` event cannot queue. Who drains it? The consumer? The bus on next startup? Not stated. An audit consumer needs to know.
- **Dead-letter idempotency caveat.** EV-014b pairs dead-letter replay with idempotency, but `DeadLetterReplay(consumer_name, filter?)` returns an error — no semantics for "what happens if the consumer is partway through real-time events when a replay starts." Interleaving between live-stream and replay-stream is undefined; an audit consumer needs serial-order to reason about monotonicity.

**Verdict on EV-INV-002 for audit:** the **producer side** of the covenant is well-specified. The **consumer side** is named as an obligation without naming the hooks that make it implementable. An audit consumer can satisfy idempotency (EV-014b) per-event, but cannot satisfy totality without a replay contract.

**Concrete scenario the spec does not answer:** daemon crashes mid-workflow. On restart:
- JSONL holds E1..E500. Events E495..E500 were `ordinary`-class in the lossy-tail window and may have been lost to the audit consumer's in-memory queue before its `Handle` returned.
- `fsync-boundary` events E450..E494 are all durably in JSONL.
- The audit-sink last committed offset is E470.
- On restart, does the bus:
  (a) re-deliver E471..E500 from JSONL to the audit consumer before live events resume? (implies bus reads JSONL on Subscribe, needs "from offset" API)
  (b) start fresh from E501, making the audit consumer responsible for its own JSONL-tail reader? (implies every async consumer that cares about totality runs a parallel JSONL reader)
  (c) nothing — events between last-ack and restart are simply lost, consistent with EV-017?
- The spec implies (c) but EV-INV-002's "two-sided covenant" language suggests (a) or (b). This decision is load-bearing and absent.

## Use case 5 — Bus overflow handler

Consumer: subscribes to `bus_overflow` (§8.8.4). Emits alerts when queues saturate.

**What works:**
- `bus_overflow` payload (§6.3) carries `consumer_name`, `event_type`, `event_id`, `queue_depth`, `shed_at`. Enough to attribute the overflow and display it.
- EV-011a guarantees `bus_overflow` itself MUST NOT be shed; falls back to direct JSONL append + log warning. A consumer of `bus_overflow` will therefore see every overflow event if the JSONL is readable.

**What breaks:**
- **`bus_overflow` does not say whether the shed event was re-attempted or truly dropped.** For `fsync-boundary`, EV-011a says it spills to a file. For `ordinary`/`lossy-tail-ok`, it is dropped. A handler looking at a `bus_overflow` event cannot tell from the payload which happened — `durability_class` is not in the payload, and the handler would need to look up the §8 row for `event_type`. Add `outcome: spilled | dropped` to the payload.
- **No aggregate rate.** A single-event payload is fine, but an overflow handler observes storms. There is no spec-level guidance on whether consumers are expected to de-duplicate / rate-limit. Not a bug; an ambiguity the first implementer will resolve arbitrarily.
- **Consumer for `bus_overflow` is itself an async consumer and can itself overflow.** EV-011a's "fall back to direct JSONL append plus structured-log warning" for a full `bus_overflow` queue is clear for the bus-internal emission path. But the operator-observability consumer that reads `bus_overflow` could also fall behind. No special treatment; it gets sheds like any other async. This is probably fine, but worth acknowledging that overflow-observer overflow is silent.

## Cross-cutting gaps

1. **`Subscribe` / `Handle` signature absent.** The `Subscription` record is not defined. Every consumer in §8's "typical consumers" column (`audit`, `observability`, `reconciliation`, `improvement-loop`, `beads-integration`, `operator-observability`) implies a concrete subscriber interface, but the Go shape is not given. Compare EV-032 which specifies the Event envelope concretely. Add `Subscription{ Name string; Types []EventType | AllTypes; Class ConsumerClass; Handler func(ctx, Event) error }` or equivalent.
2. **Recovery replay semantics.** EV-014b names consumer idempotency on "recovery" and "dead-letter replay" but does not define **when** recovery replay happens. Does the bus read JSONL on startup and re-deliver to live consumers? This is the crux of EV-INV-002's consumer side.
3. **"typical consumers" column is informative but implies subscriptions the spec does not require.** E.g., `8.1.2 run_completed` lists `improvement-loop` as a typical consumer. Is improvement-loop required to subscribe, or is the column descriptive? EV-029's N-1 compatibility applies to "readers" — readers of what? A consumer that opts out still sees type churn during a migration.
4. **Trace-context propagation is consumer-opaque.** `trace_context.parent_event_id` (EV-008, §6.1) is populated by producers when known. A consumer that wants to stitch causal chains has no API to request "all events in the causal tree rooted at X." For MVH this is fine; for the improvement-loop it is a gap.
5. **No class-change protocol.** EV-013 says observer is default and sync/async are opt-in. If an observer wants to become asynchronous mid-evolution (add retry, add dead-letter), the §8 row's "typical consumers" does not carry class. An amendment to change class is implied but not spelled out.

## Subscription-semantics ambiguities (consolidated)

Independent of any one use case, these consumer-side questions are unanswered:

1. **Is subscription by type-list, by type-pattern, by predicate, or all three?** EV-009 is silent. For 69 types, enumeration is tedious and drift-prone; a predicate like `func(Event) bool` is more useful but the spec does not sanction one.
2. **Does `Subscribe` block until the bus `Seal()` call, or return immediately?** The lifecycle says subscription happens at startup and sealing comes at `daemon.Start()` completion. A consumer goroutine cannot start processing before sealing, but the API does not indicate readiness.
3. **Can a consumer `Unsubscribe`?** Unstated. For a long-running daemon with consumers that may be reloaded (operator-observability UI), this is relevant.
4. **Delivery ordering guarantee per consumer.** Within a single consumer, are events delivered in `event_id` order? Implied by JSONL append order + single writer, but not stated as a consumer-facing contract. Reconciliation and audit both need this commitment.
5. **Slow consumer's effect on peers.** If async consumer A is slow and its queue fills, consumer B is not affected (per-consumer queues per EV-011). Good. But if observer fanout uses a shared goroutine pool, observer A's blocking call affects observer B's latency. Not spec'd.
6. **Panic handling on consumer side.** What happens if a consumer `Handle` panics? EV-019 handles daemon-top-level panic, but a consumer-goroutine panic that is not recovered would crash the daemon. Should the bus wrap consumer calls in `recover()` and emit `consumer_failed`? Implied but not stated.

## Type-issue nits

- `workspace_id` typed as `<UUID>` in §6.3 `workspace_merge_status`; handler-contract / workspace-model specs should confirm this is UUID, not string. §8.5.3 payload column does not qualify the type.
- `EventType` in `bus_overflow.event_type` is the right type but some payloads in §6.3 use strings where an enum would be safer (`divergence_kind` is enum'd in `store_divergence_detected`; good. `reason` on `operator_escalation_required` is enum'd; good. But `rate_limit_source` on `agent_rate_limit_status` is a bare `String | null` — a consumer wanting to branch on provider-specific logic needs a closed enum).
- `run_failed.error_category` optional-when-orchestrator-originated creates a `nil` branch every consumer must handle; call out in §6.3 with an example, or split into two distinct failure types to avoid the optional.
- `infrastructure_unavailable.failed_prerequisite` enum values are hyphenated and a mix of service identifiers and failure kinds (`br_missing` vs `filesystem_full`). Consumers will want a regex-free stable split; consider two fields (`component` + `failure_mode`).
- `reconciliation_category_assigned.category` is typed `(Cat 0..6)` prose — should be a declared enum with explicit symbolic names (`cat_0_clean`, `cat_6a_investigator_escalated`, etc.) matching the reconciliation spec.
- `event_type` field inside `bus_overflow` is itself an `EventType`; a consumer decoding a `bus_overflow` payload needs the registry to be able to name-match. Should be stable across schema versions (EV-029 guarantees N-1, probably fine).

## Severity ranking

Ordered by impact on the five use cases:

| # | Gap | Blocks | Severity |
|---|---|---|---|
| 1 | No recovery-replay contract on `Subscribe` | Audit (4), Reconciliation (2) | **High** — EV-INV-002 not deliverable consumer-side |
| 2 | `Subscription` record unspecified | All five | **High** — every implementer invents it differently |
| 3 | Observer dispatch concurrency / queue bounds unstated | Observability (1) | **Medium** — works naively, fails under load |
| 4 | No drain / quiescence primitive | Reconciliation (2) | **Medium** — correlation windows cannot be closed |
| 5 | `bus_overflow` does not carry spill-vs-drop outcome | Overflow handler (5) | **Medium** — handler has to cross-ref §8 row |
| 6 | OQ-EV-004 (no per-payload version on wire) | Improvement loop (3) | **Medium** — known, deferred, will bite later |
| 7 | `typical consumers` column's normative force unclear | Audit (4), Improvement loop (3) | **Low** — informative-only is workable |
| 8 | Enum tightening on optional string fields | All | **Low** — cosmetic, catch at review |
| 9 | Consumer panic policy unspecified | All | **Medium** — can crash the daemon |
| 10 | Trace-context causal-chain query API missing | Improvement loop (3), Audit (4) | **Low** — post-MVH feature |

## What would move this to "ready"

1. Spell out `Subscription` record and `Handler` interface in §6.1, including whether a wildcard / predicate subscription is allowed.
2. Decide and document the recovery-replay contract: on daemon restart, does the bus replay JSONL-tail to live consumers, or does each consumer own its offset and run its own reader? This closes EV-INV-002's consumer side. My recommendation: the bus owns tail-replay from a `since: event_id` offset declared in `Subscription`, with the consumer's persisted-offset store being the consumer's responsibility.
3. Add `outcome: spilled | dropped` to `bus_overflow` payload (and optionally `durability_class` so the consumer does not need the §8 registry to interpret it).
4. Resolve OQ-EV-004 before improvement-loop is planned; external consumers are coming and will need registry-free consumption. A per-payload `version` int is cheap.
5. Clarify observer-queue bounds and dispatch semantics (per-event vs. per-consumer goroutine). Default to per-consumer with its own bounded queue; apply EV-011a shed rules symmetrically to observers with a `lossy-tail-ok` default.
6. Declare consumer-goroutine panic policy: the bus MUST `recover()` consumer panics, emit `consumer_failed`, and continue dispatching to other consumers. Without this, a single buggy consumer takes down the daemon.
7. Add a `Drain(ctx)` / quiescence primitive to the `EventBus` interface for reconciliation / test harness use.
