# Round 2 Skeptic Review — execution-model.md v0.2

## Verdict

Round-1 integration did real work: the durability predicate is now mechanically
decidable (EM-023a is the single most important fix of the revision), the
sub-workflow namespacing and acyclicity holes are closed, and three §5
invariants that were emphatic §4 restatements have been retired honestly
(with the retired IDs explicitly non-reusable). This is not a face-lift round.

But the revision accrued ceremony to satisfy reviewers and, in two places,
papered over underlying issues rather than resolving them. The UUIDv7
globally-unique claim in EM-018a is promoted to contract on a probabilistic
primitive without any enforcement mechanism. EM-017a's "dispatch reconciliation
Cat 6" closes a detection hole but opens a new circular-write hole that the
spec does not acknowledge. EM-046's `context-restore` still sits oddly inside
the new durability table. And EM-INV-004, called out by r1 critic as arguably
§4 content, was kept as an invariant by adding an adjective list of subsystems
rather than by satisfying the selection test.

Recommendation: **proceed with one more pass focused on the three load-bearing
challenges below**; the ceremony-padding items are clean-up and can ride the
same pass.

## Integration-fix audit (did round-1 fixes actually fix things?)

- **C1 "durable" is circular → EM-023a decision table.** Genuine fix. A
  mechanical `transition_kind` × `outcome.status` table replaces a circular
  definition; glossary (§3) forwards to EM-023a. But `context-restore` sits
  awkwardly inside the table — see Challenge A.
- **C2 `compilation_loop` vs `structural` → §8 preamble "DISJOINT" +
  OQ-EM-006.** Genuine fix. Disjoint-at-emission is concrete; the sentinel
  question is deferred to handler-contract correctly.
- **C3 Sub-workflow event names leaking → EM-036 cites event-model for
  names; §6.5 declares WHEN.** Genuine fix. Scope discipline restored.
- **C4 PARTIAL_SUCCESS silence → EM-023a lists it durable with a
  `partial_success=true` evidence flag.** Genuine fix.
- **C5 Validator recursion non-termination → EM-034b + EM-038 "detects
  cycles during transitive resolution".** Present but distributed; see
  Challenge C.
- **R1 MUST/SHOULD items (EM-010/EM-025/EM-041/EM-020, verdict trailer).**
  All landed.
- **R1 invariant collapse (INV-002/003/006 retired).** Done correctly;
  retirement note at §5 close is honest.
- **R1 hidden-assumption 1 (transition_id uniqueness) → EM-018a contract.**
  Underdeveloped — see Challenge B.
- **R1 hidden-assumption 8 (gate denial checkpoint) → EM-023a + EM-042.**
  Genuine fix.

Net: round-1 got ~85% real resolution. The one that got dressed up rather
than resolved is transition_id uniqueness — the r1 critic flagged it as an
unstated assumption, and the v0.2 response was to declare it a contract
rather than to make it mechanically guaranteed.

## Challenges

### Challenge A — `context-restore` as a durable transition does not survive EM-023a

EM-023a conjuncts `transition_kind ∈ {..., context-restore}` with
`outcome.status ∈ {SUCCESS, PARTIAL_SUCCESS}`. But EM-046's context-restore
is an operator-directed or reconciliation-directed context-window swap, not
the outcome of a handler running. What produces its `outcome.status`?

Two readings, neither stated:

- **Reading 1**: the daemon synthesizes a SUCCESS outcome. Then durability
  is satisfied — but Outcome's enum acquires a non-handler-originated
  source, and `actor_role` has to be `daemon`/`operator` (not enumerated).
- **Reading 2**: context-restore has no Outcome in the handler-contract
  sense, and EM-023a is a category error for this kind tag.

If Reading 1: EM-046 should say so; §4.1 EM-005 should acknowledge daemon-
produced outcomes. If Reading 2: EM-023a needs a carve-out keying durability
on `transition_kind` alone for context-restore.

**Load-bearing because**: reconciliation workflows produce context-restore
transitions (per [reconciliation.md §9.5] cited in §9.3). Ambiguity here
will land reconciliation with a different assumption than execution-model,
and Cat 3/Cat 6 detectors will disagree on "last durable state" after a
context-restore.

### Challenge B — EM-018a promotes probabilistic UUIDv7 uniqueness to contract without enforcement

EM-018a: "Every `transition_id` MUST be generated as a UUID v7 … and MUST
be globally unique across all runs within a harmonik project." This is
circular. UUIDv7 itself produces only probabilistic uniqueness — a 74-bit
random tail appended to a millisecond timestamp. The MUST does not hold
across:

1. **Same-millisecond generation.** Under parallelism, collision probability
   reduces to the 74-bit random draw. Fine in expectation; not guaranteed.
2. **Clock non-regression.** VM suspend/resume, NTP step-backs, and container
   live-migration can make the millisecond field go backwards. "Time-ordering"
   is a property of the environment, not the UUID.
3. **Cross-process generation.** The spec does not say WHERE transition_ids
   are generated. Daemon-local generation allows a monotonic counter; agent-
   local generation across machines does not.

The §A.3 rationale argues the contract makes the un-scoped sibling-file path
safe. But the contract is aspirational; the mechanism is not stated. Three
defensible shapes:

- **(a)** Name the generation locus: daemon write-path only, with a
  per-process monotonic counter mixed into the random bits.
- **(b)** Drop "globally unique" to SHOULD and elevate the EM-020a audit tool
  (which already flags `transition_id` appearing on more than one commit) to
  the primary guarantee.
- **(c)** Scope the path: `.harmonik/transitions/<run_id>/<transition_id>.json`.
  §A.3 considered and rejected this for "minimal surface change" — weaker than
  the hole it leaves behind.

**Load-bearing because**: the improvement-loop cross-run merge is exactly
the case EM-018a cites. If collision occurs, the sibling-file path silently
corrupts the replay tree; EM-020a catches it post-hoc, after the write.

### Challenge C — EM-017a opens a reconciliation-recursion hole the spec does not address

EM-017a routes corrupted checkpoints to reconciliation Cat 6. Combined with
EM-024 ("task-branch tip MUST be the run's last-durable-state checkpoint")
and EM-026 (reconciliation workflows emit exactly one verdict commit), an
unaddressed case appears:

The corrupted checkpoint IS on the run's task branch. EM-024's precondition
is already broken at the corrupted tip. The reconciliation workflow must
itself write a verdict commit — but where?

- **Same task branch**: verdict lands atop the corrupted commit, which
  remains in the run's history; EM-024 is violated for the whole duration
  of the reconciliation workflow.
- **Separate branch**: EM-INV-005's "git wins on completion disagreement"
  becomes ambiguous — two branches carrying the same `Harmonik-Run-ID`
  with different tips.

EM-026 declares the reconciliation exception to cadence but says nothing
about branch placement when the reconciliation is triggered by EM-017a.
Reconciliation.md §9.3 may own Cat 6 mechanics, but this spec owns EM-024
and EM-INV-005 — the recursion case has to be addressed here or explicitly
delegated with a named hook.

**Load-bearing because**: corrupted checkpoints are exactly where automation
has to be airtight. A Cat 6 dispatch that itself violates EM-024 for the
duration of its own run is the shape of "why did the system escalate twice"
bug reports.

### Challenge D — EM-INV-004 survives by adding a subsystem list, not by passing the selection test

R1 critic flagged EM-INV-004 as borderline. The v0.2 response expanded the
body to name workspace-model and beads-integration as places the invariant
"constrains." But the template's §5 selection test is: if the rule fits
inside one subsystem's §4 without reference to others, it is a requirement,
not an invariant.

The rule — "no mechanism that atomically rolls back prior checkpoints" —
still lives entirely inside this spec's §4.7. Listing downstream subsystems
as places the rule PROPAGATES is not the same as it SPANNING them. By
contrast EM-INV-001 requires event-model to ABSTAIN from JSONL replay as
reconstruction — a property event-model could violate at author time
without this spec noticing. That is what spanning means.

Cheap fix: either delete EM-INV-004 and let EM-033 carry the weight, or
rewrite it so workspace-model or beads-integration could ship a violating
primitive without execution-model noticing — e.g., "Any subsystem that
writes to git, Beads, or workspace state MUST NOT implement an
undo-previous-N-operations primitive." That framing makes the invariant
fail the "fits inside one §4" trigger honestly.

**Load-bearing because**: the selection test is the only discipline keeping
§5 from filling with emphatic §4 restatements. Three invariants were retired
honestly; the fourth should meet the bar.

## Hidden assumptions v0.2 introduces (not flagged in r1)

1. **EM-023a treats `outcome.status` as a field on the Transition, but
   §6.1's Transition record does not carry outcome status.** The Outcome
   record is a separate type. EM-023a's decision procedure is keyed on
   `outcome.status` — the implementer has to know which Outcome is
   associated with which transition. The association is implicit in the
   cascade flow (§7.3), where `chosen_action`'s Outcome is the transition's
   Outcome. Make this explicit: either add `outcome_status: OutcomeStatus`
   to the Transition record, or name the association rule normatively.

2. **EM-027's "each segment MUST consume the immediately prior segment's
   typed output" is unfalsifiable at this spec's surface.** Handler →
   hook → gate → transition → event crosses four specs. Execution-model
   cannot normatively constrain handler-contract's or control-points'
   internal types without co-ownership. This is a MUST-shaped integration
   claim rather than a testable requirement. It probably belongs in §A.3
   rationale or in architecture.md as a cross-cutting discipline, not in
   §4.6 as a MUST.

3. **`workflow_class = reconciliation` keyed MUST (EM-026) still hangs off
   an optional field.** §6.1 Workflow record now says "absence means
   ordinary workflow" — good. But the field is still `String | None`,
   which means any string value other than `reconciliation` is legal
   and has undefined behavior. If reconciliation is the only reserved
   value at MVH, the field should be `ENUM { reconciliation } | None` or
   a validator rule should reject unknown strings. Otherwise authoring
   errors (`workflow_class = "Reconciliation"`, typo) silently become
   ordinary workflows that skip the cadence exception.

4. **EM-044's `local-patchback` is called a rollback but populates no
   target state ID.** The kind enum calls it a rollback; EM-044 says it
   "does not relocate the run's graph position." A rollback that doesn't
   relocate is either (a) mislabeled in the enum or (b) load-bearing
   semantics the spec should state. If local-patchback is a same-state
   re-execution with different context, call it that; if it's a
   forward-then-undo pair, decompose it. The current shape reads like a
   name chosen before the semantics were pinned.

## Ceremony / sub-requirement audit

Of the nine "a" sub-requirements added in v0.2, eight carry real content
(EM-017a/018a/020a — checkpoint integrity; EM-023a — durability table;
EM-031a — active-run discovery; EM-034a — node-ID namespacing; EM-041a —
context-update ordering; EM-043a — traversal-counter locus).

One is partially duplicative: **EM-034b** (sub-workflow acyclicity) restates
what EM-038 already enforces ("detects cycles during transitive resolution").
The acyclicity MUST lives in §4.8 and its enforcement lives in §4.9, split
across two section numbers. Cleaner: fold into EM-038 as a bullet; leave
EM-034b as an informative pointer. Not a rounding issue overall — one of
nine is ceremony.

## Post-collapse invariant discipline

Three of the remaining three §5 invariants:

- **EM-INV-001 (git is state reconstruction source)**: PASSES. Constrains
  event-model (MUST NOT walk JSONL for reconstruction), reconciliation
  (MUST honor git-derived state), process-lifecycle (startup sequence uses
  this). Genuine cross-subsystem property.
- **EM-INV-004**: FAILS by the argument in Challenge D above.
- **EM-INV-005 (git wins on completion disagreement)**: PASSES. Constrains
  beads-integration (MUST NOT prefer bead state), event-model (MUST NOT
  prefer JSONL), reconciliation (MUST route the divergence through Cat 3).

Two genuine invariants + one dressed-up §4 requirement. The template's
selection test is one-in-three soft at §5 post-collapse.

## Affirmations (decisions that survive both rounds and should)

1. **EM-023a decision table** is the cleanest possible shape for the
   durability predicate: mechanical, exhaustive, tied to two enum fields.
   The partial-success flag is a cheap concession that avoids an
   OutcomeStatus split.
2. **Retiring INV-002/INV-003/INV-006 with explicit ID non-reuse** is the
   right discipline; the §5 closing note is honest.
3. **§8 `compilation_loop` vs `structural` disjointness** is a real
   resolution; OQ-EM-006 defers the sentinel-vocabulary question correctly.
4. **§2.2 out-of-scope with "why"-attached citations** is exemplary scope
   discipline; each exclusion names the owning spec.
5. **§A.3 UUIDv7 rationale** transparently documents the rejected
   alternative even where I disagree with the outcome (Challenge B).

