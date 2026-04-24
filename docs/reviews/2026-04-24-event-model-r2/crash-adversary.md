# Crash Adversary Review — event-model.md (Round 2)

**Target:** `/Users/gb/github/harmonik/specs/event-model.md` (v0.2.0, 69 events, taxonomy-first)
**Template:** v1.1
**Date:** 2026-04-24
**Reviewer role:** Crash Adversary — pressure event-model against crash / partial-write / concurrent scenarios.

---

## Verdict

**Needs localized tightening.** The v0.2 revision genuinely closed most of the Round-1 crash-surface gaps: EV-011a's non-blocking back-pressure names a spill file for `fsync-boundary` overflow, EV-014b pairs consumer idempotency with producer idempotency, EV-023 carries the post-crash-window guardrail, and §6.2 names the read-recovery rule. But five crash-edge seams remain under-specified, and one of them (mid-fsync crash ambiguity) is load-bearing for the correctness of EV-023 detectors. None are overturning; all are closable with 5-20 lines of normative text.

Six scenarios below. Each is keyed to specific requirement IDs, names the observable failure, judges current coverage, and proposes a concrete fix.

---

## S-1 — Crash mid-fsync: did the event "make it"?

**Requirement IDs affected:** EV-014a (dispatch returns after fsync for boundary events), EV-015 (JSONL is durable form), EV-016 (fsync on boundary class), EV-017 (lossy-tail-OK), EV-020 (append-only), EV-023 (divergence-evidence read), §7.1 (Emit-and-flush sequence).

**Failure mode.** `Emit` appends a `fsync-boundary`-class line to `events.jsonl` (EV-015), calls `fsync(2)` (EV-016), and is expected to return only after the fsync completes (EV-014a step b). Between the `write(2)` (bytes reach page cache) and the `fsync(2)` return, a hard crash (SIGKILL, kernel panic, power loss) can leave three on-disk states: (i) no bytes persisted, (ii) a torn partial line, (iii) a fully persisted line. The caller does not return from `Emit`, so no upstream producer knows which state obtained. On restart, the synchronous consumer the producer was about to halt-dispatch to (EV-014a step c) has no record it was ever going to run, AND the JSONL may or may not carry the line.

Concrete instance: `checkpoint_written` (§8.1.7, class F). If the line made it, reconciliation sees evidence that a commit was made. If it didn't, reconciliation walks git, finds the commit, and has to decide whether the daemon thought it finished. The `post_crash_window` guardrail (EV-023) correctly marks the evidence ambiguous, but EV-023's corroboration rule ("if the on-disk authoritative stores also disagree is the divergence real") implies the detector TESTS git. For `checkpoint_written` that works. For events whose payload does NOT reference a commit_hash (e.g., `daemon_started`, `daemon_ready`, `workspace_merge_status.status=pending`), the detector cannot corroborate against git — it has no on-disk authority to test against.

**Spec coverage.** Partial. §6.2's read-recovery rule discards a trailing torn line — good. EV-017 says loss of non-boundary events is acceptable. But there is no normative rule for the boundary-event mid-fsync case when corroboration is impossible. EV-023 implicitly assumes every divergence-relevant event references an authority, which §8 does not enforce.

**Recommended fix.** Add to EV-023 or a new EV-023a: "If a divergence-evidence read's candidate event has no payload field pointing to an on-disk authoritative store (git commit_hash, bead_id), AND the event falls inside the post-crash window, the detector MUST treat the event as `evidence-inconclusive` and MUST NOT emit `store_divergence_detected` for that event alone. The event MAY contribute to a divergence event corroborated by a peer event whose payload does reference an authority." This closes the "mid-fsync on a non-corroborable event" hole without weakening the guardrail for commit-referencing events.

**Second-order concern.** §7.1's pseudocode shows `APPEND_LINE` and `FSYNC` as sequential primitives. Real OS-level semantics: `write(2)` reaches the page cache; `fsync(2)` forces the page cache to the storage device's durability domain; the storage device may further buffer in its own write cache unless barriers are honored. On consumer SSDs without power-loss-protection, `fsync(2)` returning does NOT guarantee the write survives power loss — only a kernel-level drive cache flush (part of `fdatasync` on Linux only if the filesystem mounts with `barrier=1`) does. The spec names `fsync(2)` explicitly; it should either cite the assumption that the storage stack honors barriers, or soften EV-016 to "best-effort durability subject to filesystem and drive behavior." This is not a fix request, but the assumption should be called out in §A.3 rationale so downstream operators on consumer-grade SSDs understand the floor.

---

## S-2 — JSONL torn tail: detector precision

**Requirement IDs affected:** EV-020 (append-only; corruption detected by readers), §6.2 (read-recovery rule), EV-023 (divergence-evidence read).

**Failure mode.** §6.2 specifies: "a final line that lacks a terminating newline OR fails JSON parse MUST be discarded; any non-final line that fails parse MUST emit `store_divergence_detected{divergence_kind=parse_failure}` and halt the reader." Three under-specified cases:

1. **Final line has newline but fails JSON parse.** The write-then-newline sequence under most Go `bufio.Writer` configurations is a single `write(2)` syscall (the JSON bytes + '\n' are buffered together and flushed together). A torn write can truncate mid-JSON and still produce a trailing newline if the line before it had one. The rule "lacks terminating newline OR fails parse" covers this (the final line fails parse and is discarded). But the rule does not say whether the reader emits a `store_divergence_detected{divergence_kind=parse_failure}` for that final line. §6.2 says "non-final line that fails parse" emits divergence; by complement, final-line parse failure does NOT. Is silent discard correct? For a post-crash daemon restart, yes — it is the expected tail. For a non-crash read (e.g., an investigator walking JSONL during normal operation), a torn final line is anomalous and should surface. The spec does not distinguish these call-sites.

2. **Partial line followed by more complete lines** (possible if the filesystem reorders block writes across a crash). §6.2 says non-final parse failure halts the reader. That is safe but surfaces as a pure divergence event with no repair path.

3. **Zero-byte file after crash.** If `events.jsonl` was fresh at startup and the crash happened before first fsync, the file may be empty. The spec does not say whether an empty log is a valid state on daemon startup. Reconciliation Cat 1 (per reconciliation.md §9.2) presumably handles this, but event-model should name the case.

4. **Concurrent readers during append.** Nothing in §4.4 or §6.2 specifies whether readers (investigator agents, observational-replay tools) can safely read `events.jsonl` while the writer is actively appending. On POSIX, append-mode writes are atomic up to `PIPE_BUF` (4096 bytes) per `O_APPEND` semantics, but JSONL lines can exceed that (large payloads, large `session_id` sets). A reader tailing the file can see half-written lines. The read-recovery rule discards the torn final line, but a reader's "final line" is the last line IT sees, which may be the writer's in-flight line. Spec should say: "Tailing readers MUST treat the currently-growing file's final line as non-authoritative until a terminating newline is observed."

**Spec coverage.** §6.2 is precise on the two named cases. The three edge cases above are not covered.

**Recommended fix.** Extend §6.2's read-recovery rule:
- "A final line that parses as JSON but fails schema validation (unknown `type`, missing envelope field) MUST be treated as a torn write if the read is part of a post-crash startup recovery, and discarded silently; in all other read call-sites it MUST emit `store_divergence_detected{divergence_kind=schema_mismatch}`."
- "A non-final line failing parse with subsequent parseable lines MUST halt the reader AND emit `store_divergence_detected{divergence_kind=parse_failure}` carrying `byte_offset`. The reader MUST NOT skip the corrupt line and continue."
- "An empty or absent `events.jsonl` at daemon startup MUST emit `daemon_started` + `store_divergence_detected{divergence_kind=log_missing}` only if reconciliation observes that git or Beads carries a commit/bead referencing a prior daemon cycle; otherwise the empty log is a valid fresh-project state."

---

## S-3 — Overflow during a crash: `bus_overflow` can't save itself

**Requirement IDs affected:** EV-011a (non-blocking back-pressure; spill-file for boundary events; `bus_overflow` cannot shed), §8.8.4 (`bus_overflow` event), §7.2 (async dispatch pseudocode).

**Failure mode.** EV-011a says: "The `bus_overflow` event itself MUST NOT be shed; if its own queue is full, the bus MUST fall back to direct JSONL append plus a structured-log warning." Consider: a consumer queue fills under sustained load; the bus emits `bus_overflow` per shed event; `bus_overflow` is class `O` (ordinary) per §8.8.4, so it appends to JSONL without fsync; the fallback when its own queue is full is "direct JSONL append plus structured-log warning." If the daemon crashes during a burst of sheds — which is PRECISELY the scenario when `bus_overflow` matters — the bus_overflow events written in the last second before the crash are in the lossy tail (no fsync). Post-restart, the observer knows queues were shed (via the downstream consumer's dead-letter queue) but cannot account for which events were dropped versus delivered.

Additionally: `bus_overflow` direct-append during its own queue-full is synchronous with the producer's critical path (it must block on the JSONL write to get the signal durable). This violates EV-011a's own "bus MUST NOT block the producer" invariant at exactly the wrong moment. The spec does not name this tension.

**Spec coverage.** Named in EV-011a but the crash-time behavior is not specified. §7.2 pseudocode calls `EMIT(bus_overflow, ...)` which re-enters the bus; if that emit itself is queued and the queue is also full, the recursion is not addressed.

**Recommended fix.** Tighten EV-011a:
- "`bus_overflow` MUST have a reserved slot (capacity-1 reservation) in the bus-internal observer queue; the reservation guarantees the bus can always enqueue at least one overflow signal per actual shed without a recursive fill check."
- "If `bus_overflow`'s reservation is exhausted (queue full AND reservation consumed), the bus MUST fall back to direct JSONL append with `fsync-boundary` semantics (promoted from `O` to `F` for this one write) so the overflow signal survives a post-overflow crash. This is the sole case where an event's durability class is promoted at write-time; the promotion MUST be recorded in the structured-log channel."
- "A direct-append fallback of `bus_overflow` blocks the producer for one write+fsync (worst case ~5ms on NVMe); this is accepted as the floor-price of telling the operator the bus ran out of queue space."

**Adjacent concern.** EV-011a says `fsync-boundary` events overflow to a spill file. The spill file is opened lazily per consumer; on a crash at the moment the spill file is being created (mkdir / open / first write), the boundary event could be lost despite its class. Spec should say: "The per-consumer spill file MUST be pre-created at subscription-registration time (EV-009) with `O_CREAT|O_APPEND|O_DSYNC` semantics; failure to create the spill file MUST fail daemon startup with a typed error. This removes the create-race from the critical path."

---

## S-4 — Consumer subscription state: in-flight event during crash

**Requirement IDs affected:** EV-009 (subscription at startup only), EV-010 (synchronous consumer halt + quarantine), EV-011 (async retry policy), EV-014a (Emit returns after sync dispatch), EV-014b (consumer idempotency), EV-018 (producer idempotency).

**Failure mode.** The handoff surface between `Emit` returning and an async consumer having processed the event is a three-state machine: (A) event on disk, not yet in consumer queue; (B) event in consumer queue, not yet handled; (C) event handled, side effects committed. A crash at state A or B leaves the consumer on restart with: either no record the event existed (queue is in-memory, dies with daemon), or a JSONL tail containing the event (post-fsync). EV-014b says "every consumer MUST be coded against a tail-truncated event stream on recovery AND against repeated delivery via DeadLetterReplay." That covers consumer idempotency on replay. But:

1. **There is no replay path from JSONL to async consumers on daemon restart.** EV-022 forbids JSONL walks for state reconstruction. EV-021 allows observational replay, but no requirement says a restarted daemon replays JSONL-persisted events to their in-memory async consumers. So events in state A that were durably persisted to JSONL before crash are durably present on disk but will NEVER be delivered to the async consumer post-restart. The async consumer proceeds from the next live emission.

2. **For fsync-boundary events this is acceptable** (they are durable; reconciliation walks git and corroborates). **For ordinary-class events consumed only asynchronously** (e.g., `agent_started`, `hook_fired`, `gate_allowed`), the consumer — typically an observability or audit recorder — has a permanent gap in its view. The spec's observational-replay framing makes this OK in theory (dashboards are advisory) but does not say so explicitly.

3. **For the at-most-one synchronous consumer case** (EV-010), a crash between JSONL fsync and synchronous-consumer `Handle` return leaves the run in an ambiguous state: the event is durable, the run was supposed to be quarantined on consumer failure (EV-010), but the consumer never even ran. Post-restart, the run lookup (via git and Beads) either shows a clean transition (consumer ran successfully before a later crash) or an incomplete transition (never ran). Reconciliation must distinguish these, and the event alone does not tell it which.

**Spec coverage.** EV-014b names consumer idempotency. EV-022 forbids JSONL-to-state-reconstruction. The gap: in-flight-at-crash semantics for async consumers is unaddressed, and EV-010's quarantine covenant has no restart-time recovery rule.

**Recommended fix.** Add EV-014c: "On daemon restart, async consumers start from the live emission stream; the JSONL tail is NOT replayed to async consumers (this would require state reconstruction, forbidden by EV-022). Observability gaps across crash boundaries are accepted as a cost of the observational-stream model. Audit consumers whose gap-free coverage matters MUST register as `observer` AND perform a post-startup JSONL catch-up scan, subject to EV-023's post-crash-window guardrail."

Add EV-010a: "A synchronous-consumer invocation that crashed mid-handle leaves no durable record that the handler ran; post-restart reconciliation MUST treat the presence of the triggering event plus the absence of a follow-on effect in git / Beads as Cat 3/4 (stale write). A `synchronous` consumer whose handler makes external side effects MUST obtain those effects via a git commit or bead transition (the authoritative stores) so reconciliation can corroborate; synchronous consumers whose side effects are purely in-process are acceptable but MUST be idempotent per EV-014b."

**Note on the "no synchronous consumers in MVH" fact** (acknowledged in §A.3 and EV-010 rationale). The class being retained-without-constituency means these edge cases are hypothetical-but-real; a future quarantine-on-invariant-violation synchronous consumer (the named use case) will manifest exactly this crash-mid-handle scenario the FIRST time it ships. Better to spec the rule now than to hit it in production. The cost is 5 lines of normative text.

---

## S-5 — Multi-event atomicity: fsync-boundary batch

**Requirement IDs affected:** EV-015 (JSONL is durable), EV-016 (fsync on boundary class), EV-017 (event-loss OK between fsyncs), EV-020 (append-only), §7.1 (Emit sequence).

**Failure mode.** Consider a transition that emits two logically-paired `fsync-boundary` events: `transition_event` (§8.1.6) and `checkpoint_written` (§8.1.7). Both carry the same `commit_hash`. The current §7.1 pseudocode processes each `Emit` call independently: for each boundary event, append then fsync. Two events = two fsync calls. A crash between the first fsync-return and the second write can durably persist `transition_event` without `checkpoint_written`, or persist both, but NOT the inverse.

Is that a problem? For the specific pair above, `checkpoint_written` carries `transition_id` which the preceding `transition_event` also carries, so reconciliation can join them via Beads / git. But for boundary events emitted by DIFFERENT producers that share a logical atomicity unit (e.g., `run_failed` plus a concurrent `workspace_discarded`), there is no batch-fsync and no atomicity guarantee. The spec's "git is authoritative" escape works in most cases, but the partial-persist asymmetry is not named.

More subtle: even for a SINGLE boundary-class event, `write(2)` + `fsync(2)` is not atomic with the envelope's `event_id` generation. If the generator produces event_id at time T, the caller writes at T+3ms, and crashes at T+3ms, the UUIDv7 carries a timestamp that is durably paired with no on-disk event. On restart, if the generator seeds from the wall clock and re-issues a UUIDv7 at T+10s, there is NO durable record of the gap. This is not wrong but is not called out.

**Spec coverage.** EV-017's "loss OK" invariant implicitly covers this — the asymmetry is a form of tail-loss. EV-018 (producer idempotency) means re-emission is safe. But the per-transaction atomicity is not addressed; the spec leaves it to "git is authoritative" without saying so.

**Related concern — batching for cost.** §7.1's per-event fsync means N boundary events = N fsyncs. On busy paths (burst of `transition_event`s during a rapid multi-node workflow), this is O(N) disk round-trips. A producer wanting to emit a batch cannot batch the fsync; the spec's primitive is one-event-one-fsync. The spec is correct to prefer per-event durability for MVH simplicity, but should name the cost so a future optimization (group commit, combining multiple boundary events into one fsync) has a clean amendment path. Add to §A.3: "Group-commit optimization is a post-MVH amendment. MVH accepts the O(N-fsync) cost in exchange for a single-event durability contract that is trivially reasoned about by each producer independently."

**Recommended fix.** Add an invariant (EV-INV-007 or a note on EV-016): "The bus does NOT guarantee atomicity across multiple boundary events. A producer that requires two boundary events to be durably persisted together MUST emit a single event carrying both payloads, OR MUST checkpoint the logical transaction via git first (authoritative) and accept that the events are observational evidence of that checkpoint. Post-crash partial-persistence of a boundary-event batch is resolved by reconciliation via git authority." Also note explicitly that a `write`+`fsync` pair is not atomic with `event_id` generation, so a generated-but-not-persisted `event_id` has no durable trace (and MUST NOT be a correctness concern per EV-INV-001).

---

## S-6 — UUIDv7 across daemon restart: monotonicity + clock skew

**Requirement IDs affected:** EV-002 (UUIDv7 for event_id), EV-002a (monotonic within a process), EV-INV-004 (every event has monotonic UUIDv7, strictly > prior intra-process).

**Failure mode.** EV-002a says monotonicity is "within a single process." On daemon restart, the new process re-seeds. Two crash-sensitive cases:

1. **Wall clock moved backward across restart.** NTP adjustment, VM pause/resume, or operator clock fix can regress the wall clock. UUIDv7's high bits carry Unix-ms timestamp; a backward-regressed clock issues UUIDv7s that sort BEFORE the pre-restart tail. Observational replay ordering (EV-008: "UUIDv7 supplies ms-resolution total ordering across processes") is violated across the restart boundary. The spec acknowledges UUIDv7 is "best-effort total ordering" (EV-002) but does not name clock-regression as a failure mode. An observer that sorts JSONL globally by event_id and finds a post-restart event sorting before a pre-restart event has no normative rule for what to do.

2. **Within-millisecond monotonic counter resets on restart.** RFC 9562 §6.2 method 1 (monotonic counter) is stateless across restarts. If the last pre-crash event_id was generated at ms=T with counter=N, and the first post-restart event is at the same wall-clock ms=T with counter=0 (fresh process), it could — in principle — collide in the low bits with a pre-crash event. In practice, UUIDv7's 74+ bits of randomness below the timestamp make collision probabilistic-astronomical, but monotonicity is NOT guaranteed across the restart boundary. EV-INV-004's "strictly greater than the immediately preceding event_id emitted by the same process" correctly scopes to intra-process, but EV-008's global partial-order claim silently degrades across restarts.

3. **Spill and dead-letter files outlive the process.** On restart, events replayed from `spill-<consumer>.jsonl` or `dead-letters.jsonl` carry pre-restart event_ids. If they are re-dispatched to consumers and any consumer's dedup logic assumes monotonically-increasing event_ids per source_subsystem, it breaks when a spill-replay event sorts AFTER the post-restart event stream.

**Spec coverage.** EV-002a correctly scopes monotonicity intra-process. EV-008 correctly describes UUIDv7 as ms-resolution. The restart-boundary degradation and clock-regression case are NOT called out.

**Recommended fix.** Add EV-002b: "UUIDv7 generation MUST use a persisted high-water-mark file at `.harmonik/events/uuid-hwm` containing the last-issued UUIDv7's Unix-ms timestamp. On daemon startup, the generator MUST seed so that issued UUIDv7s' timestamp component is strictly greater than the high-water-mark, even if the wall clock has regressed. If the wall clock is more than 1 second behind the high-water-mark, the daemon MUST log a `daemon_degraded{reason=clock_regression}` event and continue by synthesizing timestamps ahead of the wall clock until they catch up. This preserves EV-008's ms-resolution partial order across restarts at the cost of a deliberate clock-drift window."

Add to EV-008: "Event-id ordering across daemon restarts is preserved via the high-water-mark file (EV-002b); event-id ordering across crash boundaries where the high-water-mark file itself was lost (e.g., `.harmonik/events/` wiped) degrades to unordered across the boundary. Consumers MUST NOT assume global total ordering finer than per-process contiguous runs."

**Subtlety on HWM durability.** The high-water-mark file itself must be kept fresh, which implies a write on every — or every-Nth — event emission. Writing the HWM on every emission adds a second fsync to every boundary event (doubling the cost). Writing periodically (e.g., every second) means a crash can lose up to one second of HWM, allowing a clock-backward regression to re-issue IDs in that window. The trade: "fsync HWM every boundary emission" (correct, expensive) vs. "fsync HWM periodically, accept one-second overlap risk" (cheap, degrades EV-008 across restart). The spec should pick one explicitly. Recommended: keep HWM in memory, flush it with `daemon_shutdown` (graceful) and via the same fsync as every `fsync-boundary` event (piggyback — add the HWM as a small header/footer to the JSONL fsync domain or to a sibling single-file atomic rename). This keeps cost flat and durability aligned with boundary-event durability, which is already the contract.

---

## Summary of recommended requirement additions

| # | New / Modified | Purpose |
|---|---|---|
| 1 | EV-023a (new) | Mid-fsync non-corroborable event handling |
| 2 | §6.2 extended rules | Final-line parse failure; non-final parse with tail; empty log |
| 3 | EV-011a tightened | `bus_overflow` reservation slot + promoted fsync fallback |
| 4 | EV-014c (new) | No JSONL replay to async consumers on restart |
| 5 | EV-010a (new) | Synchronous-consumer mid-handle crash resolution via reconciliation |
| 6 | EV-INV-007 or §4.4 note | Multi-event atomicity explicitly disclaimed; git-authority escape |
| 7 | EV-002b (new) | UUIDv7 high-water-mark across daemon restarts |
| 8 | EV-008 extended | Restart-boundary ordering guarantee scoped to HWM preservation |

None of these overturn v0.2's structure; all fit within the existing §4.X sections. Estimated spec delta: 60-80 lines, mostly normative, minimal rationale.

**Priority ordering (if triaged).** S-1, S-3, and S-6 are the load-bearing ones — they can each silently corrupt operator-facing guarantees. S-2 and S-5 are precision-tightening on already-sound rules. S-4 is a future-hazard (no synchronous consumers exist in MVH) that should be closed before the first synchronous consumer ships.

---

## Cross-scenario observation

Five of the six scenarios above share a structural pattern: the spec correctly specifies steady-state behavior (append, fsync, dispatch, replay) and correctly specifies the tail-loss acceptance invariant, but treats the RESTART TRANSITION itself as a point-event rather than as a set of boundary conditions. Specifically:

- The handoff between crash-instant and post-restart-first-emission is never characterized in terms of what state elements survive.
- The spec relies on "git is authoritative" as a universal escape hatch, which works for ANY state element that has a corresponding git trace but leaves soft gaps (async consumer backlogs, UUIDv7 ordering, bus_overflow signals, spill-file creation) where git has nothing to say.
- Each gap is individually small. The risk is composition: a mid-fsync crash on a non-corroborable event (S-1) while `bus_overflow` is itself being lost (S-3) and the HWM file wasn't written (S-6) can deliver a post-restart daemon that has neither the observational evidence to diagnose what happened nor the authoritative evidence (git) to know it went wrong.

The fix is not architectural — the layering is sound. It is a handful of normative rules that explicitly carve the crash-transition as a first-class state and pin what survives. A `§4.11 Crash recovery contract` section bundling EV-002b, EV-010a, EV-014c, EV-023a, and the `bus_overflow` promotion would collect these into one surface.

---

## Affirmations

The v0.2 revision closed the big Round-1 crash-surface concerns: the paired-event merges eliminated the correlation-across-crash problem; the durability-class table aligned fsync cadence with cost-of-reconstruction; the post-crash-window guardrail (EV-023) prevents false-positive divergence under lossy-tail; EV-014b pairs consumer idempotency with producer idempotency so replay is actually safe. The scenarios above are finer-grained than Round 1's; they are the edge cases that remain after the coarse structure is right. Closing them is a normal second-round tightening, not a structural rework.

The taxonomy itself survives crash-adversary pressure intact. No §8 row needs adding or removing for crash-correctness; the durability-class assignments are defensible under pressure; the four-axis tagging captures what it needs to. The remaining work is on §4's requirement surface, not on §8's taxonomy surface.

The `bus_overflow` + spill-file + dead-letter triad is clever and mostly works; the gaps are at its own crash-time edges, not in the concept. The post-crash-window guardrail is the right mechanism; it just needs one more clause for non-corroborable events. The UUIDv7 monotonicity story holds intra-process; it needs an HWM to hold cross-restart. None of these are "the spec was wrong"; they are "the spec stopped one step before the adversarial case."

---

## Evidence index

- **S-1 sources:** EV-014a, EV-015, EV-016, EV-017, EV-020, EV-023, §7.1.
- **S-2 sources:** EV-020, §6.2, EV-023.
- **S-3 sources:** EV-011a, §8.8.4, §7.2.
- **S-4 sources:** EV-009, EV-010, EV-011, EV-014a, EV-014b, EV-018, EV-022.
- **S-5 sources:** EV-015, EV-016, EV-017, EV-020, §7.1.
- **S-6 sources:** EV-002, EV-002a, EV-008, EV-INV-004.

*End of crash adversary review.*
