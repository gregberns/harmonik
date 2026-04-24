# Critic Review — event-model.md (Round 1)

**Target:** `/Users/gb/github/harmonik/specs/event-model.md` (v0.1.0, 54 events, taxonomy-first)
**Template:** v1.1
**Aligned-scope source:** `docs/foundation/core-scope.md §2` (user endorsed async emission + lossy tail)
**Date:** 2026-04-24
**Reviewer role:** Critic — challenge weakly-justified decisions, not confirm them.

---

## Verdict

**Needs substantive revision before `reviewed`.** The spec is structurally disciplined and internally consistent, but at least four load-bearing choices rest on first-plausible justifications rather than comparison against reasonable alternatives. Two of them (event granularity, fsync cadence) leak complexity into every consumer; one (three-class consumer taxonomy) underspecifies the interesting failure surface; and one (schema-evolution N-1 rule) is weaker than the spec's own prose implies. The event-loss-OK invariant survives critic pressure intact — but only because the affirming chain passes through `execution-model §4.7` and `EV-018`, which themselves carry load the event-model cannot prove alone.

---

## Challenges (5 load-bearing)

### C-1 — Paired rate-limit events duplicate state; should be a single event with status

§8.3.6 and §8.3.7 declare `agent_rate_limited` and `agent_rate_limit_cleared` as distinct types. This is the textbook case the template's "granularity criterion" (§8.9 (c)) was supposed to guard against: there is exactly one consumer pair (orchestrator-core + observability), the information content is identical across the two events except a boolean phase, and consumers have to write correlation logic to reunite them.

The cleaner shape is one type:

```
rate_limit_status: {run_id, session_id, status: active|cleared, rate_limit_source, retry_after_seconds?, started_at|cleared_at}
```

The same pattern appears in §8.5.3/8.5.4 (`workspace_merge_pending` / `workspace_merged`) and §8.7.1/8.7.2 (`operator_pausing` / `operator_paused`). These pairs inflate the taxonomy from ~48 to 54 events without adding expressiveness. The spec must either (a) merge them and amend §8.9 to require a "phased lifecycle without paired events" criterion, or (b) prove — per criterion — that at least one consumer genuinely needs the two events to arrive on distinct bus dispatches. Neither rationale is present.

**Concretely:** The taxonomy ends with 48 events after the obvious merges. That is an 11% reduction with zero expressiveness lost and a permanent reduction in downstream correlation code.

**Counter-consideration:** Paired events have one real advantage — simpler consumer dispatch (`switch event.type` with no payload branching). A merged event forces every consumer that cares about one phase to branch on `status`. But the spec already forces consumers to branch on payload fields for most event types (§8.1.3 `run_failed` has `failure_class`; §8.6.2 has `category`). The "simple switch" argument is not honored elsewhere, so it cannot justify pair-preservation here.

**A further asymmetry worth naming:** §8.6.1 `reconciliation_started` has no paired `reconciliation_completed` — the completion signal rides on §8.6.4 `reconciliation_verdict_executed`. Either the pairing rule is inconsistent, or the reconciliation sublist is the model the rest of the taxonomy should follow.

### C-2 — Fsync cadence is first-plausible, not compared against a durability-class frame

§4.4 (EV-016) commits to fsync on exactly four event types (`run_started`, `run_completed`, `run_failed`, `checkpoint_written`) plus an operator-configurable 1s timer. The justification is implicit: "these are run boundaries." Missing:

1. **Why exactly these four?** `workspace_merged` commits a git merge that is authoritative state — losing the event between fsync and crash means reconciliation must do real work to re-establish that the merge happened. By the spec's own EV-INV-001, git is authoritative, so reconciliation *can* reconstruct — but that moves cost to reconciliation rather than solving it here. A durability-class framing ("events whose loss costs reconciliation more than a fsync costs the critical path") would be defensible. The current framing is not.

2. **Why fsync-on-every-checkpoint-written but not fsync-on-transition-event?** The transition event contains the commit hash too; losing it loses divergence-evidence for reconciliation Cat 3/4 detection. The pair `checkpoint_written` + `transition_event` is conceptually inseparable.

3. **Is the 1s timer defensible or cargo-culted from Linux filesystem lore?** A 1s lossy window on a dev machine with ext4 + journaled writeback is arguably already there at the kernel level. The spec should either prove the timer buys something the kernel's own flush does not, or drop it as redundant.

**The right shape:** a short durability-class table (`critical` → fsync inline; `important` → fsync-within-N-events; `bulk` → best-effort) with each §8 event assigned a class. The current "four names hardcoded in EV-016" will rot the first time a new event type is added and the author must decide whether to amend EV-016 or leave the type in the lossy tier.

**Core-scope evidence:** `core-scope.md §2` says "Fsync events at run boundaries + every `checkpoint_written`." The spec faithfully encodes this — but core-scope was a walkthrough-alignment, not a derivation. The spec's job is to derive the cadence from the invariants (event-loss-OK + git-authoritative), not to copy the aligned line verbatim. The derivation is missing.

### C-3 — Three-class consumer taxonomy is credible, but `synchronous` is underspecified

The `synchronous / asynchronous / observer` split in §4.3 is the right number of classes — fewer would collapse meaningful distinctions, more would over-engineer MVH. Affirm that.

But `synchronous` as written is a sharp edge without a guardrail:

- EV-010 says a synchronous-consumer failure quarantines the run and "requires operator escalation." There is exactly one operator (solo dev) per `core-scope.md`. Quarantine during the critical path of a run that takes 20 minutes to replay is a real cost. Against what benefit?
- The only example of a synchronous consumer in the entire corpus of specs is — none. §9.3 lists no subsystem that registers a synchronous consumer. If the answer is "no MVH subsystem uses synchronous, but the class exists for future-proofing," the spec should say so and move the class behind a feature flag.
- EV-INV-003 ("at most one synchronous consumer per event type") is load-bearing for liveness but is proved only by registration-time enforcement (EV-014). A synchronous consumer that itself emits an event and is reentrant-called creates a deadlock that EV-014 does not catch. The spec needs an acyclicity requirement on synchronous subscriptions or a proof that synchronous consumers cannot emit.

**The cheap fix:** delete the synchronous class for MVH (and state that deferral in an OQ); if it is kept, add the acyclicity requirement AND name at least one concrete MVH synchronous consumer so the class has a real constituency.

**Additional concern — the `observer` default (EV-013) is right but hides a sharp edge:** observers MUST NOT mutate state. This is enforced by... what? The spec does not name a mechanism. Go does not give you a read-only capability boundary; an observer that imports the Beads package and calls `WriteBead` is a compiled program. The rule is a coding discipline, not an enforced invariant, and the spec should name it as such (or describe an enforcement path — e.g., observer packages must not depend-on state-mutating packages; enforceable via `depguard`).

### C-4 — N-1 compatibility is declared but not scoped; what does "N" enumerate?

§4.7 and §6.4 commit to "N-1 readable compatibility." Unaddressed:

- **Is N the envelope version, the per-type version, or both?** The spec hedges in EV-028 ("per-type schema versions MAY increment independently"). If per-type versions are independent, "N-1" is 54 independent compatibility contracts, each requiring its own migration release window per EV-029. That is a large surface the spec does not acknowledge.
- **What does "breaking" mean precisely?** EV-030 says rename/remove/narrow. Is widening a type breaking? Is adding a required field breaking (surely — but EV-030 says only "additive changes [new optional field] are non-breaking"). What about removing an enum variant? These are not edge cases; they are the normal shape of schema evolution.
- **Migration release at an operator pause** (EV-029) imports an `operator-nfr.md §7.3` contract. The pause semantics for MVH are "between runs" per locked decision #10. A migration release that requires all in-flight runs to drain can take hours on a busy machine. The spec should either acknowledge this or relax to "new writes use new schema; old-version events remain readable indefinitely."

**The right fix:** drop "N-1" as a summary claim and replace with a concrete table of what counts as breaking, what counts as additive, and what the reader obligation is in each case. Something like:

| Change kind | Breaking? | Reader obligation |
|---|---|---|
| Add optional field | No | accept, ignore if unknown |
| Add required field | Yes | fail closed on missing field |
| Rename field | Yes | no on-the-fly rewrite; migration release |
| Remove field | Yes | migration release |
| Widen type (int32→int64) | No | accept widened values |
| Narrow type (string→enum) | Yes | migration release |
| Remove enum variant | Yes | migration release |
| Add enum variant | Depends | N-1 readers see `unknown` variant |

Until that table exists, "N-1 readable" is an aspirational claim.

### C-5 — Event-loss-OK invariant survives pressure but leaks state authority through `divergence-evidence read`

EV-INV-001 ("events are observational") and EV-INV-002 ("event-loss between fsyncs is acceptable") are load-bearing and defensible. The rationale chain is: git is authoritative (EM-INV-001) → producers are idempotent (EV-018) → replay is safe → lossy tail is OK.

But EV-023 (divergence-evidence read) introduces a subtle leak:

- A reconciliation detector reads the JSONL tail to detect divergence between git/Beads/JSONL.
- If the JSONL tail lost events during the crash window, the detector CANNOT distinguish "JSONL disagrees with git because of lossy tail" from "JSONL disagrees with git because of a real bug." Both surface as `store_divergence_detected`.
- The result is that divergence-evidence read on a recently-crashed daemon produces false positives that the reconciliation agent then has to adjudicate. That is work the state-authority-is-git claim was supposed to avoid.

The spec should state one of: (a) divergence-evidence read is SKIPPED on post-crash startup until a clean checkpoint is written (expensive but clean); (b) divergence events carry a `post_crash_window: bool` field so the investigator can down-weight them; or (c) divergence-evidence reads deliberately ignore the final event-loss window. The current spec picks none of these and passes the cost to reconciliation.

This does NOT overturn EV-INV-002; it shows that the invariant has a downstream cost that the spec obscures.

**Related concern — EV-INV-002 is worded permissively** ("a hard crash... MAY lose events"). The "MAY" makes it informative-sounding; it is actually a load-bearing operational covenant with every consumer. A consumer that is not coded against lossy-tail recovery is not merely inefficient — it is wrong. Strengthen the wording: "Consumers MUST be coded to handle a tail-truncated event stream on recovery." Without that obligation on consumers, the invariant is a producer-side claim with no teeth on the receiving side.

---

## MUST / SHOULD discipline

Spot-checks against the template's §4.N+1:

- EV-019 ("panic handler flushes logs, best-effort flushes bus") uses both MUST and SHOULD in the same requirement. This is a split bug: the MUST (flush logs) and the SHOULD (best-effort flush bus) are two separate obligations and belong in two requirements. Recommend split into EV-019a (MUST) and EV-019b (SHOULD).
- EV-011's "default: unlimited" async-consumer quota is a SHOULD masquerading as declarative. An unbounded quota with no back-pressure surface is a DoS primitive; either bound it or mark the absence explicit.
- EV-017's rationale "(a) git plus Beads is authoritative... and (b) producers emit events idempotently..." is RATIONALE written inside a normative block. Move to §A.3 per the template's "non-normative callouts MUST NOT contain MUST/SHOULD/MAY" rule (EV-017 itself is normative, but the parenthetical reasoning is not and muddies the requirement).

Three MUST/SHOULD hygiene issues total. Not fatal but would fail the reviewer-enforced checklist item "every MUST/SHOULD/MAY appears inside a requirement block."

---

## Invariants challenged

- **EV-INV-002 (event-loss OK)** — survives C-5 with caveat; invariant is correct but the downstream cost is not accounted for.
- **EV-INV-003 (at-most-one-synchronous)** — vulnerable to reentrant emission per C-3; need acyclicity requirement.
- **EV-INV-005 (four-axis tags on every §8 entry)** — not actually enforced in §8 itself; §8 tables declare emitter/consumers/payload but NOT the axes. The invariant is honored in spirit (§4 requirements are tagged) but the §8 surface per-event does not visibly carry tags. Either add an axis column to every §8 table or weaken the invariant to "every §8 entry's payload is mentioned in a §4 requirement that carries the tags."
- **EV-INV-006 (no secret-bearing fields)** — discharged by EV-035 + EV-036, which is fine. But EV-035's redaction pattern is a regex — regexes for secret detection are a known-lossy approach. The invariant is written as an absolute; it is actually a best-effort.

---

## Affirmations

Not everything is wrong. These choices hold up:

- **Taxonomy-first shape** is justified. The spec's substance IS the taxonomy, and the reading order (§8 before §4) makes the requirements read as "each entry in §8 obeys this" — exactly the template's stated use case. Challenge 2 of the prompt is answered: this is not cargo-culted.
- **UUIDv7 for `event_id`** (EV-002) with explicit "UUIDv4/v1 MUST NOT be used" is the right call; cross-process total ordering at ms resolution is exactly what the bus needs, and the spec correctly names monotonic time as intra-process-only.
- **Envelope-plus-registry Go shape** (EV-032) is the right choice; the alternatives are correctly dismissed in §A.3.
- **JSONL append-only + read-recovery rule** (EV-020 + §6.2) is the right durability shape for the observational stream.
- **Separation of observational replay from state reconstruction** (EV-021 / EV-022) is load-bearing and correctly stated.

---

## Hidden assumptions (uncalled-out)

1. **"In-process pub/sub bus"** (§2.1) assumes a single-process daemon. Reconciliation investigator agents run in separate Claude Code sessions; they cannot subscribe to the bus. The spec should name this and declare that cross-process consumers read JSONL (subject to EV-021/EV-022). As written, a reader could assume agents subscribe.

2. **§6.2 JSONL layout is one file per project.** For a dev machine running 3 projects in parallel, that is 3 separate event buses with no cross-project correlation. Not wrong, but the "one JSONL per project" choice is unjustified against the alternative of one daemon-wide log with `project_id` scoping.

3. **Payload redaction happens before dispatch (EV-035).** This means observers see redacted payloads. If an audit observer wants to verify a payload contained a secret in a specific field (for a post-incident review), the evidence is already destroyed. The spec should name this tradeoff — it is a real cost, not a free-lunch safety property.

4. **`trace_context.parent_event_id` (§6.1)** is declared but no requirement forces producers to populate it. Without population obligations, the field is decorative. Either add EV-0NN requiring producers to set it for causally-following events, or delete the field.

5. **54 events are a "complete cross-subsystem emission surface" (§8).** "Complete" for MVH. Post-MVH subsystems (improvement-loop, Pi handler, CASS indexer if it grows one) will expand this. The amendment protocol (EV-027) handles expansion but does not set a budget — is 70 events acceptable? 100? The spec should either set a soft budget or state that there is none.

6. **`session_log_location` is at §8.3.8 with memory-layer as consumer.** The memory layer (CASS) is post-MVH per `core-scope.md §8`; this event has no MVH consumer and should either be deferred or consumer should be widened to include audit (which is already listed — so actually this is fine, but the "CASS indexing" label misleads).

7. **`metric` (§8.8.2)** has `Emitter: any subsystem`. This is the one escape hatch in the taxonomy — anyone can emit a metric with any payload. If the goal of the taxonomy is to be closed (expansion via amendment only, per EV-027), the `metric` event is a hole. Either constrain metric names to a registry, or acknowledge that this is an open-ended escape hatch and move it behind a post-MVH deferral.

8. **Dead-letter replay semantics (§6.1 `DeadLetterReplay`)** is named as an operator-initiated interface method. But EV-018 says emission is idempotent "during recovery." DLQ replay is a different ordering — it can interleave old events with new ones, which producer idempotency alone does not address. Consumers need to be idempotent too, and no requirement currently imposes that.

9. **§8.1.5 `state_exited` and §8.1.6 `transition_event`** describe the same boundary from two angles. The spec offers no guidance on which to consume; a downstream improvement-loop consumer that dedupes on either will produce different results. Either declare one canonical "state left" event, or require both to carry a shared `transition_id` so dedup is mechanical.

---

## What a good next revision looks like

1. Collapse the three paired-phase event pairs → 48 events (C-1).
2. Replace EV-016's four hardcoded names with a durability-class table and assign every §8 event to a class (C-2).
3. Delete the `synchronous` consumer class for MVH OR add acyclicity requirement (C-3).
4. Replace "N-1" with a breaking-change table; acknowledge per-type version independence has N-1 per type (C-4).
5. Add a post-crash-window handling rule to EV-023 to prevent divergence-evidence false positives (C-5).
6. Split EV-019 into two requirements; clean up EV-011 quota; move EV-017's rationale to §A.3.
7. Name the hidden assumptions (cross-process consumers, one-JSONL-per-project, redact-before-observe, `parent_event_id` population, taxonomy budget).

If these land, the spec is reviewer-ready. If they do not, the spec is a first-draft that will calcify costs into every consumer written against it.

**On proportion:** the author did genuinely hard work. The template conformance is meticulous; the §A.3 rationale is present where it belongs; the glossary is disciplined; the cross-references are complete. These are rare in bootstrap specs. The critique above is about the half-dozen load-bearing choices that, if unaddressed, will pay negative compound interest across every subsystem. It is NOT a claim that the spec is broadly wrong. It is a claim that the decisions that most deserve pressure did not receive it.

**One meta-point on the review itself:** the prompt asks whether taxonomy-first was picked to match template guidance versus out of genuine fit. Evidence says fit: the spec's requirements genuinely are shaped as "each entry obeys this rule." The §8 placement before §4 in the reading order is correct. The challenge points in this review are about the taxonomy's *contents* and a handful of requirements' *framing* — not about the taxonomy-first choice itself.

---

## Evidence index for the review

- **C-1 sources:** §8.3.6/§8.3.7, §8.5.3/§8.5.4, §8.7.1/§8.7.2, §8.9 (granularity criterion).
- **C-2 sources:** §4.4 EV-016, §8.1.6 (transition_event), §8.5.4 (workspace_merged), `core-scope.md §2` (aligned cadence).
- **C-3 sources:** §4.3 EV-009..EV-014, EV-INV-003, absence of synchronous consumer in §9.3.
- **C-4 sources:** §4.7 EV-028..EV-030, §6.4, `operator-nfr.md §7.3/§7.5` imports.
- **C-5 sources:** EV-INV-001, EV-INV-002, EV-023, §8.6.8.

*End of critic review.*
