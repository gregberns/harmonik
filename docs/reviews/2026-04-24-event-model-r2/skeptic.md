# Skeptic Review — event-model.md (Round 2)

**Target:** `/Users/gb/github/harmonik/specs/event-model.md` v0.2.0 (935 lines, 70 events)
**Round 1 closed:** 22 events added, 3 pairs merged, durability-class fsync, EV-002a monotonic UUIDv7, EV-011a non-blocking back-pressure, EV-014a/b dispatch + idempotency, EV-019a panic SHOULD, EV-034a source-subsystem registration, §6.4 breaking-change table, EV-023 post-crash-window guardrail.
**Reviewer role:** Skeptic — does the integration hold up, or have fixes papered over the original concerns?
**Date:** 2026-04-23

---

## Verdict

**Integration is broadly sound; three new load-bearing claims have been introduced that were not in scope for Round 1 and deserve the same pressure the original decisions received.** The spec is materially stronger than v0.1: the paired-event prohibition in §8.9(h) is self-enforcing, the durability-class frame is a real derivation rather than a hardcoded list, EV-002a closes the U1 tiebreaker hole cleanly, and EV-023 now carries teeth. But the taxonomy now carries 70 entries (not 69 as the revision-history claims — count discrepancy is itself a minor signal), the "cross-daemon ordering" claim in EV-002a's rationale is quietly load-bearing in a way the surrounding prose does not acknowledge, and the `synchronous`-class acyclicity clause has been added without a named enforcement mechanism. The orphan removals are clean against sibling specs. This spec is close to reviewer-ready; the items below are sharpenings, not re-architectures.

One meta-observation: the v0.2 revision integrated cross-spec-architect, critic, and implementer concerns into a single coherent pass without any of the three sets of concerns dominating. That is unusual in integration passes of this size (22 new events + 6 new requirements + 3 merges + 3 retirements + 2 new invariants + §6.4 table), and it is evidence the author has a clear model of which concerns are cross-cutting versus subsystem-local. The challenges below pick at the seams of that integration; they are not claims that the integration was wrong.

---

## Integration-fix audit

Round-1 asks tracked against Round-2 deliveries:

| R1 concern | R2 resolution | Verdict |
|---|---|---|
| C-1 paired events | §8.9(h) prohibits pairs; merged `agent_rate_limit_status`, `workspace_merge_status`, `operator_pause_status` | Resolved; see S-2 |
| C-2 hardcoded fsync list | EV-016 now derives from per-row `durability_class`; §A.3 rationale names the assignment rule | Resolved; see S-1 |
| C-3 synchronous sharp edge | EV-010 adds "MUST NOT emit events that would re-dispatch to itself"; EV-INV-003 amended; §A.3 explains retention | Partially resolved; see S-3 |
| C-3 observer enforcement | EV-012 now names `depguard` as the mechanism | Resolved |
| C-4 N-1 scope | §6.4 breaking-change table landed with per-type + envelope versioning; migration-release pause acknowledged | Resolved |
| C-5 divergence-evidence false positives | EV-023 mandates `post_crash_window: true`, landmark via `daemon_started.event_id`, corroboration against git/Beads before flagging | Resolved |
| U1 UUIDv7 tiebreaker | EV-002a mandates monotonic generator (RFC 9562 §6.2 method 1 or 3); EV-INV-004 strengthened | Resolved at intra-process; see S-4 |
| U2 async buffer/shed | EV-011a: durability-class-ordered shed; spill file for fsync-boundary; `bus_overflow` emission | Resolved |
| U3 Emit semantics | EV-014a states order of operations (redact → append+fsync → sync-dispatch → async off critical path) | Resolved |
| U4 subscription lifecycle | §6.1 `Seal()` + EV-009 sealed-bus error | Resolved |
| 22 missing events (cross-spec-architect gap list) | §8 now registers daemon lifecycle, upgrade, silent-hang FSM, hook/guard family, `node_dispatch_requested`, `control_points_registered`, `bus_overflow` | Verified against sibling-spec citations |
| Orphan removals | `policy_violation`, `health_check` gone; `gate_evaluated` split; `guard_denied` → `guard_reordered`+`guard_failed` | Verified; see S-5 |

Net: seven original concerns cleanly resolved, three partial, zero regressions. The shape of the integration is honest — fixes address root causes rather than surface symptoms.

One calibration: the partial-resolutions (C-3 synchronous, durability-class mechanism, U1 cross-process extension) are all partial for the same reason — each resolution correctly identifies the rule but leaves enforcement to discipline rather than mechanism. Round-2 could not have fixed this without adding a testing.md that does not yet exist, so the partials are structurally unavoidable. That is worth naming: "partial" here means "rule correct, enforcement deferred to post-bootstrap," not "rule incomplete."

A separate observation: the integration did not over-correct. A common failure mode in v0.1→v0.2 integrations is to add normative text proving every Round-1 concern was heard, bloating the spec with defensive prose. The v0.2 spec grew from 809 lines to 935 (+16%) while adding 22 events, 6 requirements, and an entire breaking-change table. That growth ratio is lean.

---

## Challenges (5)

### S-1 — 70 events is at the "is this still a taxonomy or a protocol" boundary

The revision history says 69; the actual count in §8 tables is 70 (11 + 9 + 13 + 3 + 6 + 9 + 15 + 4). That's a small bookkeeping error, but it points at a real question: a taxonomy that grew 30% in one round (54 → 70) without any new subsystem should get stress-tested.

- **The Operator / daemon lifecycle section (§8.7) carries 15 events, more than any other category.** That's 21% of the taxonomy on a single subsystem (process-lifecycle + operator-nfr). Several of these (`operator_pause_status`, `operator_resuming`, `operator_stopped`, `operator_upgrading`, `operator_upgrade_completed`, `operator_upgrade_rejected`, `operator_command_rejected`, `dispatch_deferred`, `daemon_orphan_sweep_completed`) are essentially "the daemon is in some state now" signals. §8.9(h) already prohibits paired phases; would an `operator_state_changed` event with `from_state` / `to_state` fields fold four or five of these into one? The current shape forces every consumer to subscribe to N individual types when what they usually want is "tell me when daemon state changed." The counter-argument is that distinct consumers exist per type (e.g., upgrade observers care about `operator_upgrade_rejected` but not `dispatch_deferred`) and a single unified event would force every consumer to branch on `to_state` anyway — but this asymmetry also applies to the already-merged pairs (rate-limit active/cleared had distinct consumer behavior at each phase too) and the spec chose to merge those. The taxonomy is applying §8.9(h) selectively without documenting why.
- **§8.3's silent-hang FSM family adds four events** (`agent_warning_silent_hang`, `agent_resumed_after_warning`, `agent_soft_terminating`, `agent_hard_terminating`). The last two are paired-phase-adjacent: they differ only by which threshold breached. §8.9(h)'s exemption wording ("paired-phase lifecycles") could arguably exclude them because they are not symmetric pairs, but a reader applying §8.9(h) aggressively could argue for `agent_termination_stage: soft|hard`. The spec should name whether the FSM-stage events are exempt from §8.9(h) or not. Without that, the rule is enforced inconsistently.
- **Soft budget claim (§935): "≤120 is advisory, not normative."** That is a permissive ceiling — doubling from here is still "within budget" without review. The MVH surface is now at ~58% of the advisory budget after one revision. Either tighten the budget or acknowledge that amendment volume is the real control, not the budget.
- **Reconciliation section (§8.6) has 9 events for one subsystem.** These are mostly distinct lifecycle stages (`started`, `category_assigned`, `verdict_emitted`, `verdict_executed`, `verdict_malformed`, `verdict_stale`, `budget_exhausted`, `store_divergence_detected`, `operator_escalation_required`) — all distinct enough that §8.9(h) does not apply. But three are verdict-related (`emitted`, `executed`, `malformed`); it is worth asking whether a single `verdict_event` with a `phase` field would collapse them. Unlike the operator-state case above, the reconciliation verdicts are distinct artifacts with distinct downstream audit needs, so the granularity is probably right — but the spec should say so rather than leave it as implicit.

**The ask:** either (a) consolidate §8.7 operator-state events into one status-carrying event, shrinking the taxonomy by 3-4 entries; or (b) acknowledge in §A.3 that operator-state events are intentionally granular because each has distinct downstream consumers, with a citation for the claim. The current state passes pressure but has no defensive reasoning on the record.

### S-2 — Durability-class assignment rule is stated but not mechanically checked

EV-016 establishes the rule: "events whose loss forces reconciliation into work greater than the cost of a disk sync are `fsync-boundary`; high-cardinality granular events (chunk, accrual, metric) are `lossy-tail-ok`; everything else is `ordinary`." §A.3 gives worked examples (`workspace_merged` as F because losing it forces git-DAG reconstruction; `transition_event` as F for divergence evidence; `agent_output_chunk` as L because chunks are statistical aggregates).

The rule is sound. What's missing is mechanism:

- **`workspace_merge_status` is F** (§8.5.3) but `workspace_discarded` is O (§8.5.4). A discarded workspace that the crash swallows produces the same reconciliation category (workspace with no run reference) as a lost merge event. The rule as stated says "work greater than disk sync cost" — the cost of reconciling a lost `workspace_discarded` is not obviously less than a fsync. Why the split?
- **`agent_failed` is O** (§8.3.5) but `run_failed` is F (§8.1.3). A crash between the two on the same run leaves JSONL saying "run_failed" with no preceding `agent_failed`. That's tolerable under EV-INV-002, but the choice is not explained. Why is agent-level failure cheaper to reconstruct than run-level failure when both reference the same session's exit?
- **`operator_escalation_required` is O** (§8.6.9). This event represents a Cat 3 / Cat 6 escalation where a human operator may need to intervene. Losing it during the post-crash lossy-tail window means a crash can eat the notification. The rule would put this as F — operator escalation cost is not bounded by disk sync cost. Spec should either justify O or promote it to F.
- **No enforcement mechanism.** EV-INV-005 requires every §8 entry to carry a durability class; §8 tables do carry a `Dur` column. But the rule in EV-016 is prose, not a checkable condition. A future amendment adding an event type with the wrong class cannot be caught by lint. Either name reviewer-only enforcement (human judgment per EV-027's three artifacts) or accept that the rule is aspirational.
- **The `F` class has 8 members by my count: `run_started`, `run_completed`, `run_failed`, `transition_event`, `checkpoint_written`, `workspace_merge_status`, `daemon_started`, `daemon_ready`, `daemon_shutdown`, `daemon_startup_failed`, `operator_upgrade_completed`.** That's 11 actually. Every F-class event fsyncs on emit, serializing the critical path on disk I/O. On a busy run emitting 10 `transition_event`s per minute plus workspace merges, that's nontrivial sync pressure. The spec does not name a budget or throughput bound; `operator-nfr.md §7.8` is cross-referenced for bus latency, but fsync-per-F-event is the dominant cost and is not analyzed here.

**The ask:** spot-audit the three cases above, document the reasoning, state in §10.2 that durability-class assignment is reviewer-judged per EV-027(c) not linted, and add an informative note on the aggregate fsync rate under typical load. This keeps the rule honest.

### S-3 — EV-002a's "cross-daemon monotonicity" claim is quietly load-bearing

EV-002a says: *"Generation occurs in-process within the daemon; all cross-subsystem event emissions flow through the daemon's single emitter, so cross-process monotonicity within a project is a consequence of the daemon-as-sole-emitter property."*

This is correct but under-tested:

- **Handler subprocesses emit events** (§8.3 rows list emitter as "handler (via daemon watcher)"). The wording "via daemon watcher" means the handler writes something the daemon reads and then the daemon emits. Good — UUIDv7 generation is the daemon's. But the spec does not make this routing requirement explicit in a numbered requirement. A reader could assume handlers generate their own event_ids.
- **Daemon restart creates a gap.** Counter state does not persist across restarts. RFC 9562 monotonic generators use in-memory state only. After a restart, wall-clock has advanced, so the new UUIDv7 will sort greater than the old — UNLESS the wall clock has slipped backward (NTP correction, container host time sync). The spec says `timestamp_wall` is advisory per EV-006, but UUIDv7 IS `timestamp_wall` in its high bits. A backward clock slip at a restart boundary produces an event with a lower `event_id` than an earlier-emitted event, violating EV-INV-004's "strictly greater than the immediately preceding" wording.
- **EV-INV-004 is stronger than EV-002a delivers.** The invariant says "strictly greater than the immediately preceding event_id emitted by the same process." "Same process" restricts monotonicity to a single daemon run. The cross-process claim in EV-002a's rationale ("consequence of daemon-as-sole-emitter") is about who emits, not about ordering — a second daemon instance (e.g., daemon upgrade) breaks the chain. This is fine if acknowledged; the spec does not acknowledge it.

**The ask:** (a) add a requirement stating handlers route their event_id generation through the daemon emitter (the "via daemon watcher" emitter-column phrasing should be promoted to a numbered requirement); (b) narrow EV-INV-004 to "same daemon process lifetime" and state that cross-restart ordering relies on wall-clock non-regression plus UUIDv7's 48-bit ms; (c) name the clock-regression hazard in §4.2 — if a restart encounters a wall clock earlier than the last observed JSONL line's `timestamp_wall`, the daemon MUST either refuse to start or wait out the slip. This also gives `operator_upgrade_completed` a cleaner story: the upgrade path includes a daemon restart, so the monotonicity chain is necessarily broken at the upgrade boundary and the spec should acknowledge it.

A secondary sharpening: the RFC 9562 §6.2 reference names "method 1 or 3" — method 1 is monotonic-random (regenerate if clock didn't advance, adding a counter in the random bits); method 3 is monotonic-counter (fixed 12-bit sub-ms counter). These have different overflow characteristics (method 3 caps at 4096 events per ms; method 1 has no hard cap but probabilistic degradation under extreme load). The implementer picks one; the spec should either state which or name that the implementation's choice MUST be documented in the Go registry package.

### S-4 — `agent_rate_limit_status` merge loses start-vs-continuous semantics

The merge is the right call; §A.3 rationale is correct that consumers were writing correlation logic to reunite two halves of one signal. But the merged event has a subtle information loss:

- **Old shape:** `agent_rate_limited{started_at, rate_limit_source, retry_after_seconds}` + `agent_rate_limit_cleared{cleared_at}`. Two events, two timestamps, bounded window.
- **New shape:** `agent_rate_limit_status{status: active|cleared, ...retry_after_seconds?, changed_at}`. One event per phase transition.

The loss: the new shape does not tell a consumer whether this is *the* moment rate-limiting began, *a* moment during active rate-limiting, or the clear. `status=active` alone is ambiguous — the emitter is expected to emit this once on entry to limited and once on exit to cleared. But nothing in §8.3.6 or EV-025 requires the emitter to emit exactly-on-transition; a handler that helpfully re-emits `status=active` every 60 seconds "to keep the observability live" is not wrong by the spec.

- **The paired-phase rule §8.9(h) implicitly assumes one event per phase change.** That assumption should be promoted to a normative requirement: emitters of status-carrying events MUST emit only on `status` transitions, not on keepalive.
- **Consumers now need `changed_at` deduplication logic** that they did not need with the split pair. That is the opposite of what the merge was supposed to deliver.

This is not a reason to re-split. It's a reason to add a normative "emit-on-transition-only" clause to §8.9(h).

- **Secondary concern: `retry_after_seconds` is Integer-typed** in the merged event but its meaning depends on `status`. On `status=active` it is the provider's advertised cooldown. On `status=cleared` it is meaningless (and the field is nullable — good). But a consumer tracking "what's the longest rate-limit window this handler has seen?" needs to read this field only on `active` transitions; with keepalive-emits (forbidden above) it would need dedup on `changed_at` as well. The correctness of consumer logic is tied to emitter discipline. Spec-first projects should specify the discipline.
- **Tertiary: `changed_at` as the distinguishing field assumes wall-clock resolution is sufficient.** Two `active` transitions 500ms apart (rapid-fire rate-limit bounce from an unstable provider) would produce two distinct events with distinct `event_id`s but potentially identical `changed_at` at second-resolution. The shape is fine; the spec should either require ms-resolution `changed_at` or acknowledge that dedup is by `event_id`, not `changed_at`.

**The ask:** amend §8.9(h) with: "Status-carrying events MUST be emitted only on `status` transitions; emitters MUST NOT emit successive events with identical `status` values for the same scoped entity. `changed_at` MUST carry sub-second resolution to distinguish rapid transitions."

### S-5 — Orphan removals are clean against sibling specs, but the lint obligation is prose-only

Checked: no sibling spec references `policy_violation` or `health_check` (grep across all of `specs/`). Clean removal. The co-reference in §6.5 matches.

But the enforcement side is soft:

- **§10.2 declares the §8-orphan lint obligation in prose:** "a lint rule MUST fail if any §8 row has no cited emission in a sibling spec." During bootstrap, `testing.md` does not yet exist; there is no harness to run this rule. The current protection against a new orphan is reviewer discipline plus EV-027's "at least one consumer cited in another spec" clause — which is about new events, not about existing events whose cited emitter disappears.
- **`health_check` is the only known case where §8 declared an event with `any subsystem` as emitter that had no cited citation.** `metric` (§8.8.1) has the same shape. The §8.9(g) exception for `metric` is documented. Is `metric` the *only* permitted escape-hatch forever, or can other events land with `any subsystem` later? The spec does not say.
- **Downstream lint robustness:** if a sibling spec deletes an emitter-side requirement (e.g., a future cleanup of `operator-nfr.md` removes the requirement that emits `dispatch_deferred`), §8.7.13 silently becomes an orphan. Nothing in the amendment protocol catches this; EV-027 governs additions, not deletions.

**The ask:** add a symmetric rule to EV-027: "Removing an emission requirement in a sibling spec MUST also update §8 — either by removing the event type or by documenting the orphan status with evidence that it remains load-bearing." This closes the deletion asymmetry.

One secondary observation on the removals themselves: `policy_violation` removal is clean because control-points never declared a `policy-engine` subsystem at the level implied by the event. `health_check` removal is also clean — operator-nfr's ON-036 requires a health-check *interface* (a callable RPC returning status), not an emitted event. These are the right removals, but both removals mean the spec now has zero "arbitrary-subsystem emits arbitrary event" escape hatches except `metric`. That's a tightening, and is good — worth noting as part of the deletion audit.

A further observation: the original `gate_evaluated` single-event-with-verdict-enum shape was arguably cleaner than the three-event split the v0.2 adopted (`gate_allowed`, `gate_denied`, `gate_escalated`). The split is correct against control-points §6.5 — distinct consumers care about distinct verdicts — but it is in tension with §8.9(h)'s paired-phase merge discipline. The distinguishing factor is that gate verdicts are not phases of the same lifecycle (allow/deny/escalate are terminal, not sequential) whereas rate-limit active/cleared are sequential. The spec could usefully add a clarifying note to §8.9(h) naming this distinction, so future amendments don't re-open the question.

---

## Affirmations

Round 2 genuinely improved the spec on five load-bearing axes; these should not be re-litigated:

- **Durability class as per-row attribute** (EV-016 refactor) replaces cargo-cult hardcoding with a semantic property. The rule statement ("loss-cost > sync-cost → F") is a real derivation.
- **EV-002a monotonic UUIDv7** closes U1 cleanly at the intra-process level; EV-INV-004 is strengthened to match.
- **EV-011a non-blocking back-pressure with durability-class-ordered shed** handles the case the Round-1 implementer review flagged: `fsync-boundary` events do not get silently dropped on consumer overflow; they spill to `spill-<consumer>.jsonl`. The `bus_overflow`-cannot-be-shed carve-out is correct.
- **EV-023 post-crash-window guardrail** addresses C-5 without papering over it. The landmark-via-`daemon_started.event_id` mechanism is implementable. The corroborate-against-git-and-Beads-before-flagging rule is the right place to put the reconciliation-cost-absorption.
- **§6.4 breaking-change table** replaces the hand-wave "N-1 readable" with nine concrete rows. The per-type independence is honestly acknowledged ("69 independent compatibility contracts").
- **EV-010 acyclicity clause + EV-012 depguard** give the synchronous and observer classes real guardrails where Round 1 had hand-waves.
- **Paired-event merges** shrink three pairs to three events; §8.9(h) codifies the rule so it generalizes to future additions.
- **The `synchronous` class survived pressure with actual guardrails** (EV-010 acyclicity clause; at-most-one cardinality enforced at registration). The Round-1 critic's alternative of deleting the class was not taken — and §A.3's retention rationale ("future quarantine-on-invariant-violation surface") is weak but honest. Keeping a class with no MVH constituency is a defensible future-proofing bet given the bounded cost.
- **EV-014a dispatch semantics** resolves the Round-1 implementer's U3: `Emit` returns after JSONL append + fsync + synchronous dispatch, and async/observer dispatch occurs off the critical path via a bounded worker pool (default 4). This is the right contract for non-blocking producer liveness.
- **Revision history is detailed.** The v0.2 entry enumerates every added requirement, every merged pair, and every retired orphan. This is the rare spec that makes its own diff legible to a future reviewer, modulo the 69-vs-70 count arithmetic bug.
- **§A.3 rationale has grown to carry the explanatory load.** Round-1 critic complaints about "first-plausible rationales" are answered by rationale sections for every load-bearing choice: taxonomy-first reading order, envelope-plus-registry, three-class consumer taxonomy retention, durability-class-over-hardcoded-list, event-loss between fsyncs, paired-phase merges, per-chunk retention, bead-event-routing decision, `metric` escape hatch, synchronous-class retention, and explicit hidden-assumption acknowledgment. That is eleven distinct rationales; the §A.3 section is where the spec now carries its intellectual weight and it reads as deliberate rather than defensive.

---

## Hidden assumptions

Things the v0.2 draft assumes but does not explicitly state:

1. **Handlers do not generate their own `event_id`s.** The §8.3 emitter column "handler (via daemon watcher)" is the load-bearing phrase, but no numbered requirement forbids a handler from generating UUIDv7s locally and putting them on the wire. EV-002a's "daemon's single emitter" claim rests on this unwritten rule. (See S-3.)

2. **Acyclicity verification in EV-010 is reviewer-discharged, not startup-checked.** The requirement says "the registration path MUST verify acyclicity across declared emission surfaces at startup and fail-closed on cycles." Declared emission surfaces are in sibling specs, not in Go code. The daemon has no machine-readable graph of "which subsystem emits which events in response to which others." The verification is prose-only. §A.3 says "a class with no constituency is kept because removing and re-adding it later costs more" — fine, but the acyclicity claim is stronger than the enforcement mechanism supports. Either add a startup-time topology check (read the §8 tables and sibling-spec §6.5 declarations, derive the graph, check for cycles) or weaken EV-010 to "emitters of events whose synchronous consumers exist MUST NOT emit events that cycle back through those consumers; enforcement is reviewer discipline at EV-027 amendments."

3. **Wall-clock regression across daemon restart is not addressed.** EV-006 says `timestamp_wall` is advisory across processes; but UUIDv7's 48-bit ms timestamp IS `timestamp_wall` in its high bits. If NTP or container host time sync rolls the wall clock back between restart boundaries, EV-INV-004's "strictly greater" is violated across the boundary. §4.2 should name this.

4. **Status-carrying events emit on transition only.** §8.9(h) declares the rule but does not mandate the transition-only emission discipline. See S-4.

5. **Event-count budget is advisory but anchor points are not.** §935 names ≤120 as a soft target. With 70 today, there is room for 50 more events before review. That is a large drift allowance. Either the budget is a review trigger (amendment count > N in a rolling window requires re-review) or it is decorative.

6. **`source_subsystem` registry has no amendment path.** EV-034a requires startup registration with duplicate detection. A new subsystem added later must register a new identifier. The spec does not say whether the registry is closed (only subsystems declared at spec time may register) or open (any Go package can register). Given that this is a compile-time-resolved string, it's effectively open — but a malicious or buggy subsystem registering `orchestrator-core` would fail startup only if the real orchestrator-core registers first. Ordering of init() functions in Go is package-dependency-ordered; this is probably OK in practice but is not asserted.

7. **Per-type schema versions are maintained "in the registry" but the registry is Go code.** §6.4 says migrations happen at operator pauses. But the *source of truth* for "what version is this payload type right now" is the Go source. A spec-first project whose schema authority is code rather than spec has a minor inversion. The spec should either require per-type version numbers to be declared in §8 (a `Ver` column) or accept that code-registry is the authority and state it.

8. **70-event count vs. revision-history's "54 → 69".** Minor, but the discrepancy suggests a last-minute addition not reflected in the revision summary. Worth reconciling before finalize — either the 70th event is legitimate and the revision history needs updating, or an errant addition slipped in and should be audited.

9. **EV-INV-004's "strictly greater than the immediately preceding event_id emitted by the same process" imposes a tight invariant on concurrent goroutines.** In a single daemon process with N goroutines emitting concurrently, strict monotonicity across all emissions requires either a mutex on the generator (serializing all emissions) or a generator that is lock-free but still strictly monotonic (RFC 9562 §6.2 method 1 with CAS on the counter). The implementation burden is real and the spec does not name the approach. For a busy daemon, generator contention could become a throughput floor.

10. **Spill file semantics for `fsync-boundary` events on consumer overflow are under-specified for recovery.** EV-011a mandates that F-class events that can't be queued spill to `.harmonik/events/spill-<consumer>.jsonl`. But §6.2 does not describe how an operator drains the spill file back to the consumer after overflow clears, nor whether `DeadLetterReplay` accepts spill-file paths. The mechanism is declared at emission; the reverse path is implicit.

11. **The `bus_overflow`-cannot-be-shed carve-out has a fallback to "direct JSONL append plus a structured-log warning."** That fallback bypasses the normal emission path — no redaction, no synchronous consumer dispatch, no registered source_subsystem check. For an event named to signal "the bus failed to deliver," that's defensible (you want the warning in the log even if the bus itself is broken). But the spec does not say whether the direct-append version still passes the EV-036 secret-field check, nor whether the structured-log warning counts as a legitimate diagnostic on an EV-INV-006-protected payload. Worth a sentence.

12. **`state_entered` and `state_exited` are both `O` class, while `transition_event` is `F`.** The state-lifecycle events are redundant-with-transition_event in content (every state exit has a paired transition_event carrying the same commit_hash). A consumer that dedupes on transition_id will see both; a consumer that dedupes on event_type will see one but miss the other. The spec offers no guidance on which is canonical.

13. **The redact-before-observe tradeoff noted in the Round-1 critic's hidden assumptions is acknowledged in §A.3 ("deliberate safety-over-forensics tradeoff").** Good. But the acknowledgment is in rationale, not in a numbered requirement — an implementer reading only §4 will not see it. The redaction is structurally correct; the documentation location is suboptimal. This is minor.

---

## Bottom line

The integration is sound. Round 1's big asks all landed, and the three partial resolutions (synchronous acyclicity enforceability, durability-class mechanism-vs-rule, cross-daemon ordering) are amenable to one-sentence fixes rather than re-design. The count discrepancy and the §8.7 taxonomy density are the only items that point at "maybe one more integration pass" — neither is blocking.

The five challenges above share a pattern: the v0.2 spec has strong rules but soft enforcement. §8.9(h) is a rule without transition-only emission discipline. EV-016's durability-class assignment is a rule without a lint. EV-010's acyclicity is a rule without a startup-time graph check. EV-INV-004's monotonicity is a rule that breaks silently at daemon restart. The §10.2 orphan lint is prose-only during bootstrap. This is not a criticism of the rules themselves — each is defensible. It is a criticism of enforcement-gap management: the spec is in a regime where reviewer discipline and Go-source discipline together carry the load that executable checks cannot yet carry. That regime is appropriate for a pre-testing.md bootstrap; the spec should name it as such.

Recommendation: **land v0.2 as `reviewed` after the S-1 through S-5 asks are addressed; the asks are additive clarifications, not re-architecture.** The spec is in materially better shape than the v0.1 critic review assumed it could be in one revision. A follow-up OQ tracking "enforcement mechanisms currently reviewer-discharged that should move to lint rules post-testing.md" would capture the enforcement-gap pattern without requiring fixes now.

*End of skeptic review.*
