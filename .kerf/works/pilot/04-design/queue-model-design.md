# Change Design — A4: `specs/queue-model.md`

**Area:** A4 — stream-curation path confirmation + producer-side pause confirmation
**Maps to:** G2 (S5/S10 stream path), G3 (S9 producer→consumer wiring), G5 (coherence).
**Authoring order:** second (depends on A3, which defines the producer A4 confirms).
**Research:** `03-research/queue-model/findings.md`.

---

## 0. One-line summary

The thinnest of the four areas: **confirm** (do not re-specify) that Pi-driven curated dispatch uses a **stream** group (not the wave default of `harmonik run --beads`), and **confirm** that the A3 `operator_pause_status` producer is the production driver of the already-correct QM-054 `active → paused-by-drain` consumer — with zero change to consumer semantics.

## 1. Current state — what the spec says now

Every contract A4 touches is already normative and (for the consumer) already coded correctly:

- **Group kinds (QM, `:36`, `:93-98`).** `GroupKind ∈ {wave, stream}`. `wave` = fixed, closed, "not appendable post-submit"; `stream` = "ordered, open-ended sequence; dispatched as slots open; appendable while pending or active".
- **Append validation (QM-024-class, `:504`).** `queue-append` requires `group_index` to reference a group with `kind == stream` AND `status ∈ {pending, active}`. Append to a wave group, a completed group, or a non-existent index is rejected.
- **Stream-only append target (QM-040, `:612`).** `queue-append` targets exactly one stream group; wave groups are immutable after submit.
- **Append in-flight-safe (QM-043, `:634`).** Appending to an active stream does not block/pause/interfere with dispatched items; the dispatch loop sees new tail items on its next eligibility evaluation (per execution-model §4.3).
- **Stream dispatch ordering (`:439`).** Earliest-indexed pending item; appended items go to the tail; head-first selection ensures append-order dispatch.
- **Submit-as-start (QM-050-class, `:644-662`).** `queue-submit` mints `queue_id`, builds the envelope at `status: active`, transitions group 0 to `active` (QM-031), and the dispatcher picks it up. There is no separate `start` method — the four queue methods are `submit | append | status | dry-run`.
- **Pending→active gate (QM-031, `:425-427`).** A group activates only when its predecessor is `complete-success` AND the queue is `active`; group 0 activates immediately on submit.
- **Pause consumer (QM-054, §8.5 `:695-704`).** On operator-pause/shutdown-drain (ON-027 step 1), the queue MUST transition `active → paused-by-drain`, persist (QM-001), emit one `queue_paused{reason: operator_drain}`, and dispatch no new items while paused. The drain pseudocode is **owned by ON-027, not duplicated here**.
- **Pause consumer is coded (research F2).** `queue_operatoreventconsumer_7urls.go:118-150` (`handleOperatorPauseStatus` → `transitionToPausedByDrain`; `handleOperatorResuming` → `transitionToActive`), idempotent and subscribed.
- **No auto-resume across restart (QM-055, §8.6 `:706-708`; `:271`).** A loaded queue in `paused-by-drain`/`paused-by-failure` stays paused; v0.1 recovery is operator-driven.
- **Wake-on-submit (execution-model EM-NOTE-WAKE, `:816`).** `SetQueue` on every accepted submit/append wakes the workloop at sub-poll-interval latency.
- **Stream concurrency correction (execution-model EM-NOTE-STREAM-CONCURRENCY, `:818`).** A stream group at `max_concurrent > 1` runs items concurrently (`streamEligible` skips `dispatched` items, no HOL block); `--wave` is for append-closed semantics, NOT for concurrency. The old "use `--wave` when `--max-concurrent > 1`" guidance is SUPERSEDED.

**What is NOT in the spec:**

1. **No statement that the Pi-driven curation path uses a stream group.** The contract supports it (stream is appendable) but queue-model prose never says "Pi-driven curated dispatch uses a stream group; the wave default of `harmonik run --beads` is the wrong primitive for incremental curation." A future reader could "fix" `harmonik run --beads` to default to stream (it should not — wave is correct for closed one-shot batches).
2. **No statement that the A3 producer is the production driver** of the QM-054 consumer. The consumer subscribes to an event nothing emits today (the A3 gap).

## 2. Target state — what the spec should say after the change

A4 adds **annotations / one confirmation note**, not new mechanism (constraint N4 — no new queue primitive). The two edits are deliberately small to avoid the R1 risk of regressing a correct, tested consumer.

### T1 — Annotate the stream group as the Pi-curation path (in §2.4 GroupKind and/or §7 append)

Add prose (an informative note keyed to the existing stream definition + a sentence in §7/QM-040) stating:

- **Pi-driven curated dispatch (per cognition-loop.md §4.9 CL-071 and execution-model.md §4.13 EM-062) MUST target a `stream` group.** Stream is the only appendable group kind (QM-024/QM-040); CL-071's eager refill and EM-062's deficit-refill both call `queue-append`, which a wave group rejects.
- **Selection rule (avoid the "fix the wrong surface" trap, research R2).** `harmonik run --beads` defaults to a **wave** group — correct for a closed, one-shot batch submitted and run to completion. It is the **wrong** primitive for incremental Pi curation. These two entry points coexist: wave for closed batches, stream for Pi-driven incremental curation. A future change MUST NOT alter the `harmonik run --beads` wave default to "fix" appendability; the fix for Pi curation is for Pi to submit a stream group, not to change the wave entry point.
- Cite **EM-NOTE-STREAM-CONCURRENCY** as the authority that stream is concurrency-safe at `max_concurrent > 1`, so the curation path gets both concurrency AND appendability.
- Cite **EM-NOTE-WAKE** so the curation prose does not re-introduce a poll-interval-wait assumption: an appended item wakes the workloop at sub-poll-interval latency (constraint C6).

### T2 — Confirm the A3 producer drives the existing QM-054 consumer (a note on §8.5)

Add a single confirmation sentence to §8.5 (QM-054) — **not** a change to the consumer transition logic:

- The `operator_pause_status{status: pausing|paused}` that drives the QM-054 `active → paused-by-drain` transition is **produced in production by the operator-nfr pause/resume command verb (operator-nfr.md §4.3 ON-014a/ON-014b)**. Prior to that verb being wired, the consumer subscribed to an event with no production emitter; ON-014a/b closes that loop.
- **No change to consumer semantics.** The `handleOperatorPauseStatus` behavior, the single `queue_paused{reason: operator_drain}` emission, the "no new dispatch while `paused-by-drain`", and QM-055 no-auto-resume are all **unchanged**. A4 adds a producer-confirmation annotation only (constraint C4; explicitly stated "no change to consumer semantics").

### T3 — Confirm submit-as-start (no new start verb) — annotation only

A one-sentence confirmation in §6/§9 that `queue-submit` returning `status: active` **is** the "start" semantics that satisfies the lifecycle's S6 step; there is no separate `start` method and none is added (non-goal N4). This pre-empts a future change-author introducing one.

## 3. Rationale

- **Confirm, don't re-specify (research F1/F2/F4).** Stream/wave, append validation, QM-054 consumer (spec + code), QM-031 gate, QM-055 no-auto-resume, submit-as-start are all done and correct. A4's value is *coherence*: naming which primitive Pi uses and which producer drives the consumer, so the end-to-end contract reads as one piece (G5).
- **The R1 over-edit risk is the dominant design concern.** The QM-054 consumer is correct and tested; editing its transition text risks regressing a working surface (C4 forbids). T2 is deliberately a single annotation sentence, not a logic change.
- **The R2 "fix the wrong surface" trap is real.** Without T1's explicit selection rule, a reader seeing "Pi needs appends but `harmonik run --beads` makes a wave" could change the wave default — breaking the (correct) closed-batch surface. T1 names the coexistence and forbids that.
- **Wake-on-submit + stream-concurrency corrections are load-bearing for CL-071 (research F5/F3).** The curation prose must cite EM-NOTE-WAKE (sub-poll-interval append latency, C6) and EM-NOTE-STREAM-CONCURRENCY (stream is concurrent, supersedes the old `--wave` guidance), or it re-imports stale assumptions.

## 4. Decisions recorded

- **OQ4 (where dependency-ordering lives) → rely on `kerf next` rank order + stream-append (which preserves append order, `:439`) + QM-031 cross-group sequencing if explicit groups are ever needed.** The harness does NOT build explicit multi-group queues at v0.1; it appends rank-ordered survivors to a single active stream group. (research R3; consistent with CL-071's rank-order loop. The detailed harness mechanism is A2's; A4 only confirms the queue primitive supports it.)

## 5. Requirements traceability

| Goal / SC | Target state | Requirement touched |
|---|---|---|
| G2 / S5/S10 — stream curation path | T1 (Pi curation uses stream; selection rule; cite wake + stream-concurrency notes) | annotation on §2.4 / §7 QM-040 (no new ID) |
| G3 / S9 — producer→consumer wiring | T2 (A3 producer drives QM-054, no consumer change) | annotation on §8.5 QM-054 (no new ID) |
| S6 — submit-as-start | T3 (confirm; no new start verb) | annotation on §6/§9 (no new ID) |
| G5 — coherence | T1+T2+T3 | — |

Every 02-components.md §A4 requirement is addressed:
- "Pi uses a stream group, not the wave default" → T1.
- "A3 producer feeds existing consumer, no consumer-semantics change, no auto-resume" → T2 (+ QM-055 preserved).
- "submit-as-start satisfies S6, no separate start verb" → T3.
- "reference EM-NOTE-WAKE for eager-refill append latency" → T1 cite.
No target lacks a driver; no new requirement ID is introduced (A4 is pure confirmation/annotation, consistent with N4).

## 6. Constraints honored

- **C4 (consumer correct, do not regress).** T2 is a producer-confirmation annotation; QM-054/QM-055/`handleOperatorPauseStatus` semantics untouched.
- **C5 (stream is the curation primitive).** T1 states it; the wave default of `harmonik run --beads` is preserved (C3-adjacent — do not break the working closed-batch surface).
- **C6 (wake-on-submit holds).** T1 cites EM-NOTE-WAKE; no poll-interval-wait assumption re-introduced.
- **N4 (no new queue primitive / no new start verb).** A4 adds zero new requirement IDs and zero new methods; T3 explicitly forecloses a `start` verb.

## 7. Dependency note

A4 depends on A3: T2 references ON-014a/ON-014b (the producer) by ID. If A3's final IDs shift at spec-draft, A4's T2 citation updates to match. No other dependency.
