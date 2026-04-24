# Implementer Review — Event Model r1

- Spec: `specs/event-model.md` v0.1.0 (taxonomy-first, 809 lines)
- Lens: build the in-process event bus + JSONL persistence in Go
- Date: 2026-04-24
- Requirements sampled: EV-001, EV-002, EV-008, EV-010/011/013, EV-015/016/017/020, EV-028/029, EV-032/033/034

## Verdict

**Implementable with known gaps.** The envelope (§6.1), taxonomy (§8), tagged-union Go shape (EV-032), fsync-boundary list (EV-016), and three-class consumer taxonomy (EV-010..EV-014) are unambiguous enough to start coding. But an implementer hits under-specification in five places that will be invented if not written down: (1) subscription API shape, (2) back-pressure / buffer policy for async consumers, (3) the exact writer serialization model (single-goroutine or mutex), (4) UUIDv7 tiebreaker when two events share a millisecond, (5) the payload-registration API signature. Two type-vs-reference issues (`TransitionKind`, `FailureClass`) need attention before bootstrap. Nothing here warrants re-architecting; all are additive clarifications.

## Implementation sketches

### EV-032/033/034 — envelope + registry

The tagged-union shape is clear. A working skeleton:

```go
type Event struct {
    EventID          uuid.UUID       `json:"event_id"`            // UUIDv7
    SchemaVersion    int             `json:"schema_version"`
    Type             EventType       `json:"type"`
    TimestampWall    time.Time       `json:"timestamp_wall"`
    TimestampMonoNs  *int64          `json:"timestamp_mono_nsec,omitempty"`
    RunID            *uuid.UUID      `json:"run_id,omitempty"`
    StateID          *uuid.UUID      `json:"state_id,omitempty"`
    SourceSubsystem  string          `json:"source_subsystem"`
    TraceContext     *TraceContext   `json:"trace_context,omitempty"`
    Payload          json.RawMessage `json:"payload"`
}

type EventPayload interface{ eventPayload() }

var registry = map[EventType]func() EventPayload{}

func RegisterEventType(t EventType, ctor func() EventPayload) { /* panic if sealed */ }
```

Clean. Question: the spec says "via `init()` functions OR via explicit `RegisterEventType` calls" (EV-034) but does not name the function or its signature, nor whether the registry is sealed on daemon startup or on first `Emit`. An implementer will invent both.

### EV-015/016/017/020 — JSONL writer

The lossy-tail design plus append-only plus mandatory-fsync list gives a clean implementation:

```go
type JSONLWriter struct {
    mu       sync.Mutex
    f        *os.File
    enc      *json.Encoder
    flushTimer *time.Ticker  // default 1s per EV-016 optional flush
}

func (w *JSONLWriter) Append(e *Event) error {
    w.mu.Lock(); defer w.mu.Unlock()
    if err := w.enc.Encode(e); err != nil { return err }  // newline per encoder
    if mustFsync(e.Type) { return w.f.Sync() }
    return nil
}

func mustFsync(t EventType) bool {
    switch t {
    case RunStarted, RunCompleted, RunFailed, CheckpointWritten:
        return true
    }
    return false
}
```

This satisfies EV-015, EV-016, EV-020. EV-017 (loss between fsyncs OK) is purely a contract on consumers, not a writer behavior. **Gap:** the spec does not say whether `Append` returns before or after `fsync(2)` on boundary events. Returning after fsync blocks the Emit caller on disk I/O on every `checkpoint_written`; returning before couples durability to a goroutine that can be crash-killed between the enqueue and the fsync. This matters because EM-023a makes `checkpoint_written` a synchronization point for reconciliation detectors.

Recommendation: call it out. The sensible reading is synchronous fsync on boundary events, because the checkpoint-write sequence in `execution-model.md §4.4` (`git update-ref` lands before the event is emitted) already paid a disk round-trip, so fsync on the JSONL adds one more syscall, not a scaling concern. State this.

### EV-010/011/013 — bus and consumer taxonomy

The three-class distinction and the at-most-one-synchronous rule (EV-INV-003) give a simple dispatcher:

```go
type Subscription struct {
    Name       string
    Class      ConsumerClass  // Synchronous | Asynchronous | Observer
    Types      []EventType
    Handler    func(context.Context, *Event) error
}

func (b *Bus) Subscribe(sub Subscription) error { /* validates EV-014 */ }

func (b *Bus) dispatch(e *Event) error {
    if sync := b.syncFor[e.Type]; sync != nil {
        if err := sync.Handler(b.ctx, e); err != nil {
            b.emitConsumerFailed(sync, e, err)
            return quarantineErr{err}  // EV-010 halts the run
        }
    }
    for _, a := range b.asyncFor[e.Type] { b.asyncQueue <- workItem{a, e} }
    for _, o := range b.observerFor[e.Type] { go o.Handler(b.ctx, e) }
    return nil
}
```

**Gap:** `asyncQueue` is a channel. What size? The spec does not specify the async-dispatch buffer depth, the shed policy when full, or whether a slow async consumer can back up the bus. Two viable implementations:

- Per-consumer bounded buffer; if full, drop the event for that consumer and emit `consumer_failed` with `error_category = overflow`.
- Per-consumer bounded buffer; if full, block Emit (which couples bus liveness to the slowest async consumer, defeating EV-011's "MUST NOT block producer").

EV-011 says "MUST NOT block the producer" — so option 1 is the correct reading. State this explicitly (and extend the `error_category` enum in §8.8.3 to include `overflow`).

### EV-028/029 — schema versioning

The envelope-version-plus-per-type-version model is fine. Adding a new event type `widget_spawned`:

1. Add to `EventType` enum.
2. Define `WidgetSpawnedPayload` struct implementing `EventPayload`.
3. Register in an `init()`: `RegisterEventType(WidgetSpawned, func() EventPayload { return &WidgetSpawnedPayload{} })`.
4. Add a row to §8 via the foundation amendment protocol (EV-027).

This works. But OQ-EV-004 flags a real concern: the payload has no version field. With only an envelope `schema_version` and a registry-internal per-type version, an N-1 reader cannot tell from the wire whether the payload it holds is v1 or v2 of a given type — the registry is startup-time-wired to exactly one version. EV-029's N-1 window therefore relies on "additive fields are non-breaking" plus Go's `json.Unmarshal` default behavior of ignoring unknown fields, which works for readers, but the reverse direction (a v2 producer's JSONL consumed by a v1 binary after downgrade) is silently lossy. Acceptable for MVH; worth writing a sentence stating that.

### EV-001/002/008 — envelope build at emit time

An implementer wiring up `Emit` writes roughly:

```go
func (b *Bus) Emit(ctx context.Context, typ EventType, payload EventPayload) error {
    now := time.Now().UTC()
    id, err := uuidv7.NewAt(now)  // monotonic generator, see U1
    if err != nil { return err }
    raw, err := json.Marshal(b.redact(payload))  // EV-035
    if err != nil { return err }
    e := &Event{
        EventID:         id,
        SchemaVersion:   envelopeSchemaVersion,
        Type:            typ,
        TimestampWall:   now,
        TimestampMonoNs: ptrInt64(monoNs()),
        SourceSubsystem: b.subsystem,
        Payload:         raw,
    }
    b.inferScope(ctx, e)  // fills RunID, StateID, TraceContext from ctx
    if err := b.writer.Append(e); err != nil { return err }  // EV-015/016
    return b.dispatch(e)  // EV-010/011/012
}
```

The ctx-threaded scope-inference is not covered by the spec. An implementer will need a conventional context-key contract (`ctx.Value(runIDKey{})`) to fill `run_id` / `state_id` / `trace_context` without every callsite passing them explicitly. Probably belongs in a short §7.1a.

## Under-specified

**U1 — UUIDv7 tiebreaker when timestamps collide.** EV-002 says UUIDv7 supplies ms-resolution total ordering; EV-008 says no finer cross-process ordering. What about intra-process ordering of two events emitted in the same millisecond from the same emitter? RFC 9562 UUIDv7 has a 12-bit sub-ms random or counter field and then 62 bits of random. The spec does not mandate the monotonic-counter variant, so two events emitted 10µs apart from the same process could sort backwards. The fix is one sentence: "The UUIDv7 generator MUST produce strictly monotonic values within a process (RFC 9562 §6.2 method 1 or 3)." Without it, the partial-order contract of EV-008 is weaker than it reads.

**U2 — Async consumer buffer depth, shed policy, and worker model.** EV-011 says bounded retry (3 attempts, exp backoff from 1s) but does not specify: in-flight buffer per consumer, worker pool size, what happens when the buffer fills, what happens on daemon shutdown with unprocessed items. An implementer will pick numbers. The operator-nfr spec or this spec needs the defaults.

**U3 — Emit semantics under load.** `Emit` in §7.1 is pseudocode. Is it non-blocking (returns after JSONL append, async dispatch happens later) or synchronous through all consumer classes except observers? The interface comment on `EventBus.Emit` in §6.1 says "non-blocking," but the pseudocode shows in-line `DISPATCH_TO_SUBSCRIBERS`. These disagree for synchronous consumers (which by EV-010 run on the producer's critical path, meaning Emit MUST block until the sync consumer returns). Pick one reading and state it.

**U4 — Subscription lifecycle.** EV-009 forbids dynamic mid-run subscription but does not name the startup hook where subscriptions land. `RegisterEventType` is one thing; `Subscribe` is another. Both are "startup-time" but the spec does not say whether Subscribe precedes, follows, or is interleaved with RegisterEventType, nor whether the bus has a `Seal()` call after which Subscribe fails.

**U5 — Redaction registry accessor.** EV-035 says "the redaction registry ([handler-contract.md §4.7])" is applied. The bus needs to pull that registry at emit time. The spec does not name the accessor function. Minor, but it affects the `EventBus` constructor signature.

**U6 — Dead-letter replay interface.** §6.1 names `DeadLetterReplay(consumer_name, filter?)` but no requirement defines its semantics. Is it idempotent re-dispatch to the named consumer only? Does it respect the same 3-retry policy? Does it re-emit `dead_letter_enqueued` on repeated failure? MVH can skip this surface, but if it ships it needs normative text.

**U7 — Partial-write detection on the reader.** §6.2 says readers "discarding a final partial-write line (no terminating newline OR JSON parse fails)" is normative. Two ambiguities: (a) what about a *middle* line that fails to parse (impossible under EV-020 append-only but possible under disk corruption)? Is that a hard error, a skip-with-warning, or a reconciliation trigger? (b) what if the final line is syntactically valid JSON but lacks an `event_id` (truncated mid-field could still parse if the truncation falls cleanly on a `}`)? Specifying "final line: must pass envelope validation, else discard; non-final line: must pass envelope validation, else emit `store_divergence_detected{divergence_kind=parse_failure}` and stop reading" would close this.

**U8 — Clock source for `timestamp_wall`.** EV-001 says "RFC 3339 wall-clock time at the emitter" — but on Go, `time.Now()` is a wall-plus-monotonic composite. Is the wall reading captured before or after UUIDv7 generation (which also reads wall time)? If they disagree, does the envelope reflect that? Minor, but implementations differ on whether `time.Now().UTC()` or `time.Now().Round(0)` is called and whether the UUIDv7 library exposes its internal timestamp. State: emitter MUST use a single wall read per emission and MUST feed that read to UUIDv7 generation and to `timestamp_wall` so the envelope is self-consistent.

## Type-vs-reference issues

**T1 — `TransitionKind` is referenced, not imported.** §8.1.6 and §6.3 `transition_event` payload list `transition_kind` as an enum whose values match `execution-model.md §4.10 EM-044`. The enum is also duplicated in `execution-model.md §6.1` as a Go `ENUM`. An implementer needs one Go type. Either (a) this spec says "imports `exec.TransitionKind`" (cross-package dependency in the Go source tree) or (b) the event-model package redeclares it and they drift. State the import.

**T2 — `failure_class` vs. `error_category` on `run_failed`.** §6.3 `run_failed` payload carries both `failure_class` (6 values) and `error_category` (7 `ErrX` values). The two enums overlap but do not align (e.g., `compilation_loop` is a `failure_class` with no obvious `ErrX`; `ErrSkillProvisioningFailed` has no obvious `failure_class`). The relationship is not stated. Is one derived from the other? Does the consumer need both? An implementer will either pass both through opaquely or invent a mapping.

**T3 — `OutcomeStatus` enum reference.** §8.1.8 `outcome_emitted` cites `outcome_status` "per execution-model.md §4.1 EM-005". EM-005 lists four values `{SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS}`. EV-006.3 payload schema for `run_failed` adds a fifth concept (`compilation_loop`) via `failure_class`. Keep OutcomeStatus as the handler-return surface; do not let it leak across.

**T4 — `Checkpoint` type is not referenced from events.** `checkpoint_written` carries `commit_hash` and `transition_id`, not a `Checkpoint` struct. That's correct per EM-028 (event is a projection, not the record) and per EV-INV-001. No issue; worth noting this is intentional so readers do not file it as a gap.

**T5 — `bead_id` typing.** Payload schemas use `bead_id: <String> | null` (see §6.3 `run_started`, `checkpoint_written`). `execution-model.md` typed-IDs section names `BeadID` as a typed alias. Either these events carry `BeadID` (which would round-trip as an opaque string anyway) or they carry a plain `string`. Either is fine; pick one.

**T6 — `session_id` type.** §8.3 payloads use `session_id` as a string, but no spec names the generator or format (UUID? handler-chosen? ntm-session-name?). An implementer will pick. If it is a UUIDv7 it should say so; if it is opaque it should say that and forbid parsing.

## Recommendations

1. Add **EV-002a** (or amend EV-002): UUIDv7 generator MUST be monotonic within a process per RFC 9562 method 1 or 3. Closes U1 and strengthens EV-008.
2. Resolve **U2+U3** in a new subsection §4.3a "Dispatch semantics and back-pressure": Emit is non-blocking for observer+async consumers, blocking for the at-most-one synchronous consumer; per-async-consumer bounded buffer with a stated default (e.g., 1024); overflow drops and emits `consumer_failed{error_category=overflow}`; worker-pool size default; shutdown drains async queue with a bounded deadline.
3. Add **EV-034a**: name the `Subscribe` function, state that the bus is sealed after `daemon.Start()` returns, and define the sealed-after-start error.
4. Import **TransitionKind** from `execution-model` (T1) — either by cross-reference ("the Go type defined in [execution-model.md §6.1]") or by normative duplication with a "keep in sync" note.
5. State the **failure_class ↔ error_category** relation (T2): either "both are set by the classifier; failure_class is the coarse bucket, error_category is the sentinel" or drop one.
6. Accept **OQ-EV-004's default** (no per-payload version field for MVH) and add a one-line note that downgrades are lossy.
7. State fsync ordering for `Append` (synchronous, returns after `fsync(2)` on boundary types).

## Affirmations

- **Taxonomy-first reading order works.** Reading §8 before §4 makes the requirements land as "rules about the table" rather than rules in the abstract. Keep it.
- **Envelope-plus-registry beats the alternatives.** The rationale (§A.3) is right; one all-fields struct or one-Go-type-per-event would not survive the first amendment.
- **Three-class consumer taxonomy with default-observer.** This is the load-bearing design choice. Producers cannot accidentally couple the bus to their liveness. Keep it.
- **Lossy-tail-plus-idempotent-producer.** The invariant EV-INV-002 plus EV-018 make non-blocking Emit viable. This is the right trade for an observational substrate.
- **JSONL-never-reconstructs-state.** EV-021/022/023 are a clean separation. The divergence-evidence-read carve-out is worth keeping.
- **Compile-time secret-field check (EV-036).** Rare to see a spec demand a startup scan for field-name patterns; it correctly discharges EV-INV-006 without trusting redaction alone.
- **Checkpoint-written on the mandatory-fsync list.** Aligns the event tail with the git-authoritative checkpoint boundary; reconciliation detectors can trust that a crash did not swallow the last checkpoint's event silently.

File: `/Users/gb/github/harmonik/docs/reviews/2026-04-24-event-model-r1/implementer.md`
