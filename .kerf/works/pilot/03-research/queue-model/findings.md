# Research — A4: `specs/queue-model.md` (stream-curation path + producer-side pause confirmation)

**Component requirement (from 02-components.md):** confirm the stream group is the Pi curation path (accepts CL-071 appends); confirm the `operator_pause_status` producer (A3) feeds the existing consumer side (QM-054) with NO change to consumer semantics. Maps to G2 (S5/S10), G3 (S9 producer->consumer wiring), G5. A4 depends on A3.

All anchors re-verified against the live tree on 2026-05-31.

## Research Questions

1. Does the queue model already define stream-vs-wave append semantics, and is the stream group already named as the appendable path?
2. Is the `operator_pause_status` -> `paused-by-drain` consumer transition already specced AND coded, so A4 only confirms the producer wiring?
3. Does `harmonik run --beads` actually default to wave (append-rejecting), forcing Pi to use a stream group (C5)?
4. Is `queue submit` already the "start" semantics that satisfies S6 (no separate start verb, N4)?
5. Does the wake-on-submit guarantee (EM-NOTE-WAKE) already hold so eager refill can rely on sub-poll-interval append latency (C6)?

## Findings

### F1 — Stream-vs-wave append semantics are ALREADY fully normative; the stream group is already the appendable path; A4 only CONFIRMS, it does not define

The decomposition's "confirm the stream group is the Pi curation path" is exactly the right verb — it is confirmation, because the contract already exists:

- **Group kinds** (`queue-model.md:97-98`): `wave -- fixed, closed set; dispatched concurrently up to --max-concurrent; not appendable post-submit`; `stream -- ordered, open-ended sequence; dispatched as slots open; appendable while pending or active`. (`:36` declares the `GroupKind ∈ {wave, stream}` enum; `:42` "Append semantics on stream groups; rejection rules on wave groups and completed groups.")
- **Append validation** (`:504`, QM): *"`queue-append` requires `group_index` to reference an existing group whose `kind == stream` AND whose `status ∈ {pending, active}`. Append to a wave group, a completed group, or a non-existent index returns [error]."* — wave-append rejection is already normative.
- **Stream dispatch ordering** (`:439`): earliest-indexed pending item; *"Items appended after submit (per §7) are placed at the tail; the dispatcher's head-first selection rule ensures appended items dispatch in append order."* This is exactly CL-071's append-order-preserving refill target.

**Implication:** A4 adds **no new queue primitive** (N4). Its job is to state, in queue-model prose, that *Pi-driven curated dispatch uses a stream group* (so CL-071 appends are accepted) and to cross-link CL-071/EM-062 to the existing stream-append contract. This is a coherence/annotation edit, not a new mechanism.

### F2 — The `operator_pause_status` CONSUMER (QM-054) is fully specced AND coded; A4 confirms the producer feeds it with ZERO consumer-semantics change

- **Spec consumer** (`:335` co-owned event + `§8.5 QM-054`, verified text): *"When the daemon enters operator-pause or shutdown-drain per [operator-nfr.md §4.7 ON-027] step (1), the queue MUST transition `Queue.status` from `active -> paused-by-drain`. The drain pseudocode ... is owned by ON-027 and is NOT duplicated here."* On entry: persist status (QM-001), emit exactly one `queue_paused{reason: "operator_drain"}`. *"No new items are dispatched while `status == paused-by-drain`."*
- **Code consumer** (`queue_operatoreventconsumer_7urls.go`, assessment anchor `:118-150`, verified): `handleOperatorPauseStatus` unmarshals `OperatorPauseStatusPayload`, and for `status ∈ {pausing, paused}` calls `transitionToPausedByDrain`; `handleOperatorResuming` calls `transitionToActive`. Both idempotent (duplicate-event safe). **The consumer is real, subscribed, and correct.**
- **No producer in production:** the consumer subscribes to an event that nothing emits today (A3's gap). A4's confirmation: the A3 producer emits `operator_pause_status` -> this existing consumer -> existing `active -> paused-by-drain` transition, **with no change to `handleOperatorPauseStatus`/QM-054 semantics** (C4).
- **No auto-resume across restart** (`:271`, and QM-055 `§8.6`): *"If the loaded queue's `status` is `paused-by-failure` or `paused-by-drain`, the queue MUST remain in that status; no auto-resume across daemon restart in v0.1."* A4 must preserve this — resume is operator/agent-driven via A3's `operator_resuming`, never automatic.

**So A4 is the thinnest of the four areas:** it confirms a producer->consumer wiring where the consumer (spec + code) is done and correct, and adds a sentence that the A3 producer is the production driver. The risk is *over-editing* a correct consumer.

### F3 — `harmonik run --beads` defaults to a WAVE group (append-rejecting), confirming C5: Pi-driven curation MUST use a stream group

The assessment (S10) and CLAUDE.md both state `harmonik run --beads` defaults to wave in the incident-era path. The spec corroborates the consequence: wave groups reject appends (`:504`, F1). The **EM spec-bundle correction** (execution-model `:1741`, EM-NOTE-STREAM-CONCURRENCY) is the key nuance — it *supersedes* the older CLAUDE.md guidance "use `--wave` when `--max-concurrent > 1`": *"`streamEligible` skips `dispatched` items (no HOL block); `--wave` is append-closed not concurrency-required."* So a **stream** group dispatches concurrently up to `--max-concurrent` AND accepts appends — it is strictly the right primitive for Pi-driven incremental curation. A4 states: Pi's curated-dispatch path submits/appends to a **stream** group; the wave default of `harmonik run --beads` is the wrong primitive for this topology (it is fine for closed one-shot batches). EM-065 (`:1004`) already says the orchestrator must `queue-append` against the *active stream group*.

### F4 — `queue submit` returning `status: active` IS the "start" semantics; no separate start verb (N4 confirmed)

The decomposition (A4 requirement) and problem-space N4 assert `queue submit` already *is* start. Corroboration: queue-model §6 defines the four methods `queue-submit | append | status | dry-run` (`:47, :189`) — there is no `start` method. Submit accepts the queue, the group transitions pending->active (QM-031 `:427`: *"A group transitions `pending -> active` only when (a) its immediate predecessor's status is `complete-success`, AND (b) the queue's `status` is `active`"* — the first group's predecessor is none, so it activates immediately on submit when the queue is active), and the dispatcher picks it up (wake-on-submit, F5). So **S6 is satisfied by submit; A4 confirms no new start verb** (N4). A4 states this explicitly so the change-design does not accidentally introduce one.

### F5 — Wake-on-submit (EM-NOTE-WAKE) already holds; eager refill may rely on sub-poll-interval append latency (C6 satisfied)

`execution-model.md:816` (EM-NOTE-WAKE, informative): *"hk-24xn1 (daemon wake on submit/append) is closed. `QueueStore.SetQueue` (called on every accepted `queue-submit` and `queue-append`) signals `wakeC` via a buffered-1 channel; the workloop's `workloopSleep`/`workloopIdleWait` helpers select on `wakeC` alongside the poll timer ... A newly submitted or appended item wakes the workloop at sub-poll-interval latency; no poll-interval wait."* And: *"§4.13 eager-refill relies on this sub-poll-interval guarantee."* So C6 holds: CL-071's eager refill via `queue append` wakes the daemon immediately. A4 references EM-NOTE-WAKE so the queue-model curation prose does not re-introduce a poll-interval-wait assumption (the old, now-stale CLAUDE.md framing). This is a pure cross-reference, no new requirement.

## Patterns to Follow

- **Confirm + cross-link, do not re-specify.** Stream/wave (`:97-98`), append validation (`:504`), QM-054 consumer (`§8.5`), QM-031 gate (`:427`), QM-055 no-auto-resume (`§8.6`) are all done. A4 adds prose: "Pi-driven curated dispatch uses a stream group; the A3 `operator_pause_status` producer drives the existing QM-054 transition unchanged."
- **Cite the producer's owner (A3), the daemon-side refill (EM-062..065), and EM-NOTE-WAKE** rather than restating their contracts.
- **Preserve no-auto-resume (QM-055)** — resume is A3-producer-driven (`operator_resuming`), never automatic across restart.
- **Do not touch `handleOperatorPauseStatus` semantics or QM-054 text beyond a producer-confirmation sentence** (C4 — consumer is correct).
- **Conformance:** A4 needs no new scenario of its own; the SC3 pause scenario (A3) and SC2 append scenario (A2) exercise the queue side. A4 may add a one-line confirmation that the QM-054 transition fires on the A3 producer's event (covered by existing consumer tests).

## Risks / Conflicts

- **R1 (over-edit a correct consumer).** The strongest risk in A4 is editing working, correct, tested consumer text (QM-054) and regressing it (C4 forbids). Change-design must keep the A4 edit to a *producer-confirmation annotation* + a stream-curation-path statement. Flag explicitly: "no change to consumer semantics."
- **R2 (wave-vs-stream default friction with `harmonik run --beads`).** A4 states Pi uses stream, but `harmonik run --beads` (the human/incident path) defaults to wave. These two entry points coexist: wave for closed one-shot batches, stream for Pi curation. Change-design should state the selection rule clearly so a future reader does not "fix" `harmonik run --beads` to default to stream (that would change a separate, working surface). The EM-NOTE-STREAM-CONCURRENCY correction (`:1741`) is the authority that stream is concurrency-safe; cite it.
- **R3 (OQ4 — where dependency-ordering lives).** Problem-space OQ4 leans "rely on `kerf next` rank + cross-group QM-031 sequencing + stream append" for v0.1 rather than the harness building explicit multi-group queues. A4 supports the lean: stream append preserves rank order (`:439` head-first), QM-031 (`:427`) gives hard cross-group ordering if explicit groups are ever needed. The lean is consistent with CL-071's rank-order loop. Not a blocker; change-design records it.
- **R4 (no blocker).** A4 depends only on A3 (the producer it confirms). With A3's producer defined first (decomposition order), A4 is a confirmation pass with no open question blocking the change design.
