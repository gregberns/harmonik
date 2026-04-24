# Round 1 Critic Review — execution-model.md v0.1

## Verdict summary

The draft is structurally strong and walkthrough-aligned on the big calls (sub-workflow composition, checkpoint cadence, three-store authority, no workflow-transactionality). The load-bearing softness sits elsewhere: a central term ("durable") is never rigorously defined; the failure taxonomy leaks `compilation_loop` into `structural` without a tie-breaker; several §5 invariants are §4 requirements with extra emphasis; and the subagent picked event names and the PARTIAL_SUCCESS checkpoint rule as "first plausible answer" when both belong to neighboring specs or should have been resolved here.

The verdict is **proceed with revisions**: none of the gaps require architectural re-work, but five of them require concrete text before the spec can advance past draft. The taxonomy-first reading order is correctly applied, and the spec's scope boundaries (§2.2) are honest — the only boundary that leaks is the sub-workflow event names.

## Challenges (5 load-bearing items)

### Challenge 1 — "Durable transition" is used as a MUST trigger but is never rigorously defined

- **Challenge** — the MVH checkpoint cadence is gated on the predicate "durable transition," yet the spec never gives a mechanically-decidable definition for which transitions are durable.

- **What the spec says** — Glossary entry for `durable transition` (§3, line 65): "a transition that crosses a state boundary the system considers recoverable; checkpointed per §4.5." EM-023 ("one commit per successful durable transition"), EM-024 ("git always knows the last durable state"), EM-025 (failure commits not required), EM-031 (state reconstruction walks "durable" trail) all route on this predicate.

- **Is the justification adequate?** — no. The glossary is circular: "durable" means "what gets checkpointed per §4.5," and §4.5 triggers on "durable transitions." An implementer cannot answer "is this transition durable?" from the spec. Candidates left unresolved:
  - A gate denial that leaves the run in the source state (EM-042) — does that fire a durable transition?
  - A `context-restore` per EM-046 — the text says "the checkpoint still lands" but never names the predicate by which we concluded this was durable.
  - A sub-workflow entry event (EM-036) — lifecycle event, but is crossing the sub-workflow boundary itself a durable transition distinct from the first nested-node transition?

- **Stronger alternative** — define durability by enumerating what IS durable and what is NOT, as a table tied to transition_kind × outcome_status × node_type. Concretely: "A transition is durable iff (a) `transition_kind ∈ {forward, local-patchback, architectural-rollback, policy-rollback, context-restore}` AND (b) `outcome.status ∈ {SUCCESS, PARTIAL_SUCCESS}` AND (c) the transition advances or relocates the run's active state." Gate-denial and validator-rejection are then explicitly not durable.

- **How load-bearing** — blocking. Every downstream reconciliation detector (reconciliation.md §9.3 consumes this spec's notion of "last durable state") rests on an undefined predicate.

### Challenge 2 — `compilation_loop` is not orthogonal to `structural`; the spec admits this but provides no classifier rule

- **Challenge** — the six-class taxonomy claims orthogonality but §8.2 and §8.6 describe a relationship where `compilation_loop` is what a `structural` failure becomes after a counter ticks; the classifier rule never names who decides at the moment of emission.

- **What the spec says** — §8.2 `structural` escalation path: "If the re-planning loop exceeds its `traversal_cap`, reclassify as `compilation_loop`." §8.6 `compilation_loop` detection: "The revision-loop traversal cap on an edge per §4.10.EM-043 has been reached." §8 closing: "The `compilation_loop` class is a harmonik-level classification of a traversal-cap event; its error mapping is `ErrStructural` with a `compilation_loop` sub-tag."

- **Is the justification adequate?** — no. The spec names two escape hatches — reclassification (§8.2) and sub-tag (§8 closing) — that together say `compilation_loop` is a post-hoc label on an `ErrStructural`. If the handler returns `ErrStructural` and the daemon observes a traversal cap hit, the event payload has to carry one class. Criterion 1 in components.md §2.3 is explicit: "Two classes with the same response collapse to one." Both classes fail the run (§8.2 after cap; §8.6 immediately), both emit `run_failed`, both route to operator notification. The distinction is "did we exceed the cap or not" — which is a boolean on the `structural` class, not a new class.

- **Stronger alternative** — two defensible shapes:
  - (a) Classify at cap-hit: the daemon emits `compilation_loop` the moment EM-043 fires, and handler-returned `ErrStructural` never transitions to `compilation_loop` — the two paths are disjoint.
  - (b) Collapse `compilation_loop` into `structural` with a `traversal_cap_exhausted` sub-field, reducing the taxonomy to five.
  - Either decision is defensible; the current "both classes exist but overlap" shape is the worst of both.

- **How load-bearing** — important. Every `run_failed` event consumer routes on failure class; an ambiguous boundary produces subtle inconsistency across subsystems (pattern analysis, improvement-loop training signal).

### Challenge 3 — Event names in §4.8 violate the spec's own scope declaration

- **Challenge** — §2.2 declares "Event schemas and taxonomy — owned by event-model.md §3.2." EM-036 then hard-codes the event names `sub_workflow_entered` and `sub_workflow_exited` and their payload fields. Naming IS taxonomy.

- **What the spec says** — EM-036 (§4.8, lines 304-309): "the daemon MUST emit a `sub_workflow_entered` event carrying `run_id`, parent `node_id`, and sub-workflow `name` and `version`. On exiting, it MUST emit a `sub_workflow_exited` event…" §6.5 later lists the same two names. §2.2 says event schemas are out of scope.

- **Is the justification adequate?** — no. This is the subagent picking "first plausible answer" for event names. Per template §6.5 the correct shape is "this spec declares WHEN an event fires; event-model is normative for the payload shape." The WHEN is fine; the names and the field list are taxonomy.

- **Stronger alternative** — rewrite EM-036 as "On entering a sub-workflow expansion, the daemon MUST emit the `sub_workflow_entered` event declared in [event-model.md §3.2] carrying the correlation fields the payload schema requires." Move the name and the field list into [event-model.md §3.2] as a co-declared payload. If event-model.md does not yet exist, add an Open Question OQ-EM-005 to migrate when it lands.

- **How load-bearing** — important. If event-model ends up naming these `sub_workflow_enter`/`sub_workflow_exit` (verb form) or adding a `parent_node_id` field, this spec has to change too. The coupling is avoidable and is precisely the kind of cross-spec leakage §2.2's scope discipline is meant to prevent.

### Challenge 4 — EM-023 "successful durable transition" is silent on PARTIAL_SUCCESS

- **Challenge** — Outcome.status per EM-005 enumerates `{SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS}`. EM-023's MUST triggers on "successful." The spec never says whether PARTIAL_SUCCESS counts.

- **What the spec says** — EM-005 enumerates four statuses. EM-023 (§4.5, line 220): "The system MUST emit exactly one checkpoint commit… for every successful durable state transition." EM-025: "A failed transition… MUST NOT create a checkpoint commit." RETRY and PARTIAL_SUCCESS are neither named successful nor named failed.

- **Is the justification adequate?** — no. Nothing in §4.5 or §4.1 names which outcome-statuses yield a durable transition:
  - RETRY arguably does not (the run loops back without state advance).
  - PARTIAL_SUCCESS is load-bearing: if it does NOT checkpoint, the work product is lost; if it DOES, the run's graph position is ambiguous (did the node complete?).

- **Stronger alternative** — add a requirement EM-023b explicitly mapping OutcomeStatus to durability:
  - SUCCESS → durable;
  - PARTIAL_SUCCESS → durable with a `partial_success=true` field on the Transition record;
  - RETRY → not durable (intra-run loop per EM-015);
  - FAIL → not durable, routes to failure taxonomy §8.
  - Then §7.1 state machine can cite this rule unambiguously.

- **How load-bearing** — blocking. This is a direct, first-read "what does my handler return and what happens" question an implementer will hit in the first day.

### Challenge 5 — EM-038 sub-workflow recursive validation has no cycle-guard; the validator itself can non-terminate

- **Challenge** — EM-038 requires the validator to resolve sub-workflows "transitively" and "recursively." If sub-workflow A references B and B references A (a legal DOT construction), the validator loops.

- **What the spec says** — EM-038 (§4.9, line 324): "Sub-workflow resolution, transitively: every `sub-workflow` reference resolves to a registered workflow, and every resolved sub-workflow is validated recursively." EM-037 forbids "runtime rewrites, inheritance, or dynamic node insertion" but does not forbid mutual sub-workflow reference.

- **Is the justification adequate?** — no. The validator MUST run to completion (EM-038 last sentence), but the spec provides no termination guarantee for the recursion. The cycle-bound check (EM-043) covers graph cycles at runtime, not workflow-reference cycles at validation time. A recursion-by-reference is a different kind of cycle than an edge-traversal cycle, and EM-043 does not cover it.

- **Stronger alternative** — add to EM-038: "The sub-workflow reference graph MUST be acyclic; mutual or self-referencing sub-workflow references are an authoring error detected by the validator." Alternatively, bound validator recursion depth and emit a validator error on exceed. The first is cleaner because it matches the structural forbids of EM-037.

- **How load-bearing** — important. Directly blocks MVH: the first time two sub-workflows accidentally reference each other, the daemon hangs at workflow-load. This is a shape the validator's existing enumeration does not catch, because DOT references resolve lazily.

## MUST/SHOULD discipline

Six items where keyword choice is wrong or permissive language hides a real requirement.

1. **EM-025 "Failure commits are NOT required for MVH"** — the prose is descriptive, not normative.
   - An implementation that DOES emit failure commits is not declared non-conforming, but reconciliation detectors assume they do not exist (EM-INV-002 talks about "exactly one commit" per successful transition; zero commits on failure is implicit).
   - Fix: upgrade to "A failed transition MUST NOT create a checkpoint commit for Core MVH. Post-MVH extensions MAY add failure commits as an additive change."
   - The "MUST NOT create" clause in sentence 1 is there — but the section title and sentence 2 ("NOT required") undermine it. Pick one register.

2. **EM-010 MVH-baseline idempotency-class defaults** — "The MVH-baseline defaults the foundation assumes (overridable in policy) are: reviewer, researcher, lint, test, typecheck, and analysis nodes default to `idempotent`…"
   - This is MUST-shaped content in MAY-shaped prose.
   - Either the defaults are normative (then the requirement is "Absent a policy override, these node types MUST carry these idempotency classes") or they are informative (then move to a `> INFORMATIVE:` block).
   - The current form lets an implementation pick any defaults and still claim conformance.

3. **EM-017 trailer `Harmonik-Verdict-Executed`** — listed in §6.2 trailer table as "Reserved for reconciliation verdict-executed commits per [reconciliation.md §9.5b]."
   - No EM-NNN requirement governs when it appears, who writes it, or what value it carries.
   - The table is informative-only, but the trailer is declared as "Conditional."
   - Either make it a real requirement (EM-017b) or remove it from the table and let reconciliation.md introduce it.

4. **EM-020 Axes line with `idempotency=idempotent`** — EM-020 states transition records are immutable (a structural declaration).
   - Per template §4.N+1 exemption for declaration-only requirements, the Axes line MAY be omitted.
   - Its presence here with `idempotency=idempotent` is accidentally misleading: "idempotent" applies to state-mutating operations, not declarative facts.
   - Drop the Axes line or restate as a declaration-only shape.

5. **EM-041 "MAY consult… but MUST NOT treat it as authoritative"** — double-modal.
   - Collapsed form: "the cascade consults `suggested_next_ids` as a routing hint after condition and preferred_label; it is not an override." Cleaner, and avoids the reader wondering what "treat as authoritative" means operationally.

6. **EM-039 "no validator check MAY delegate to cognition"** — the MAY-negation is correct per RFC 2119, but the sentence reads ambiguously at speed.
   - Preferred, restructured positively: "Every validator check MUST be mechanism-tagged; delegation to cognition is forbidden." The rule is right; the grammar is sticky.

## Invariants that aren't really invariants

Per template §5 selection test: an invariant spans multiple subsystems. Applying the test:

- **EM-INV-001 — Git is state-reconstruction source.** Genuinely spans this spec, event-model, reconciliation, process-lifecycle. HOLDS.

- **EM-INV-002 — One checkpoint commit per successful durable transition.** This is EM-023 verbatim, plus "zero, two, or more" as the violation shape. §4.5 owns the cadence requirement; reconciliation §9.3 is a consumer, not a co-owner. FAILS the selection test — it is §4.5's requirement promoted to §5. Delete the §5 copy OR rewrite as a multi-subsystem claim ("any subsystem observing zero/two/more commits for one transition MUST flag reconciliation").

- **EM-INV-003 — Transition record discoverable by git-show.** This is EM-019 verbatim. FAILS the test — entirely inside §4.4. Delete the §5 copy.

- **EM-INV-004 — No workflow-level transactionality.** This is EM-033 verbatim. Arguable as cross-subsystem (reconciliation relies on it), but the rule itself is a §4.7 requirement. Either strengthen EM-INV-004 to "No subsystem MAY mechanism a rollback of prior checkpoints; reconciliation relies on this" or collapse into EM-033.

- **EM-INV-005 — Git wins on completion disagreement.** Genuinely cross-subsystem (git + Beads + JSONL). HOLDS.

- **EM-INV-006 — One run per workflow execution against one input.** This is EM-012 verbatim. FAILS the test.

Three of six invariants (INV-002, INV-003, INV-006) are emphatic restatements of §4 requirements. Delete the §5 copies. INV-004 is borderline and can be saved by rewriting as a cross-subsystem claim; INV-001 and INV-005 are genuine cross-subsystem invariants and should stay.

## Definitional gaps

Terms used heavily but not rigorously defined.

- **"Durable" / "durable transition"** — see Challenge 1. Glossary entry is circular.

- **"Successful"** (EM-023) — never tied to OutcomeStatus. See Challenge 4.

- **"Terminal" node / state** (§4.9 EM-038 reachability check; §7.1 run state machine "terminal success" / "terminal failure") — never defined. Is a terminal node a graph vertex with no outgoing edges, or a node flagged `terminal=true`, or any node whose outcome drops to a terminal edge? The validator has to check reachability to "a" terminal node — which means terminal-ness must be decidable before runtime. Adopting "a node with no outgoing edges" is the simplest rule; adopting "a node with `terminal=true`" is more expressive but requires a new attribute. Either is fine; silence is not.

- **"Transition history" / "commit-range"** (EM-003, EM-012) — described as `[oldest_commit_hash, newest_commit_hash]` in §6.1. But the run's task branch may have interleaved commits from reconciliation-workflows per EM-026; how is the commit range filtered? The range semantics need a predicate, not just endpoints — e.g., "the transitive set of commits on the task branch whose `Harmonik-Run-ID` trailer matches the run's `run_id`."

- **"Workflow-class"** (§4.5 EM-026 "keyed on `workflow_class = reconciliation`") — the `workflow_class` metadata field is used as a MUST trigger but is defined only in passing in §6.1's `Workflow` RECORD ("optional class tag"). A MUST trigger should not hang off an optional field without a fallback rule. The fallback is presumably "absence means ordinary workflow," but the spec does not say that.

- **"In-flight run"** (EM-024, EM-031) — a run not in state `completed`, `failed`, or `canceled`? Probably, but the spec does not name the predicate. Reconciliation detection depends on correctly identifying which runs are "in-flight"; the rule needs to live here, not in reconciliation.md.

- **"Terminal outcome"** (EM-036 "sub-workflow's terminal outcome") — is that the final Outcome struct produced by the last node of the sub-workflow, or a derived status? If the sub-workflow has branching terminals, does the parent see the outcome of the node that reached the terminal, or a composed summary?

## Affirmations

Five decisions that hold up under pressure.

1. **Sibling-file contract + trailer-as-index (EM-016, EM-018, EM-019).** The separation of human-scannable commit message from structured trace payload is well-justified in §A.3 and matches the walkthrough (§2). The `git show` discoverability requirement (EM-019) is concrete and testable. The "no cross-commit index may be required" clause forecloses a whole category of accidental complexity.

2. **No workflow-level transactionality (EM-033, EM-INV-004).** Aligned with walkthrough §5 and locks in a load-bearing simplicity. §A.3 rationale cites the right reason (history-rewrite forbidden by EM-020; shadow store would duplicate git). The ripple effect — reconciliation routes recovery, not rollback — keeps the system model small.

3. **Sub-workflow single-run expansion (EM-034, EM-035, EM-037).** Aligned with walkthrough §1; forbids inheritance and runtime rewrite cleanly. The parent-run-owns-the-trail rule is unambiguous. EM-037's "ONLY composition mechanism" wording closes the door on runtime rewriting as an expansion mechanism.

4. **Three-store authority with git winning (EM-INV-005).** Walkthrough-aligned; the invariant genuinely spans subsystems and gives reconciliation its routing rule. The explicit "MUST be treated as a reconciliation flag and NOT silently auto-reconciled" prevents the drift-toward-magic that a "be helpful" default would produce.

5. **Mechanism-tagged validator (EM-039).** The explicit "no cognition in validator" rule prevents drift toward heuristic workflow ingestion. The split ("semantic judgments belong in reviewer nodes") is the right boundary — validator catches structural errors; reviewers catch semantic ones.

## Hidden assumptions

Things the spec assumes that could turn out wrong.

1. **`transition_id` is globally unique across runs.** The sibling-file path is `.harmonik/transitions/<transition_id>.json` with no run-scoping. If two runs' task branches are ever merged or cherry-picked into one tree (improvement loop, scenario-harness replay), a collision is possible. Spec does not say `transition_id` is a UUIDv7 scoped to the run. §6.1 types `transition_id: UUID` which is probabilistically unique — fine at MVH scale, but not stated as a contract, and the sibling-file path discipline of EM-018 breaks if the assumption is wrong.

2. **The run's task branch exists before the first checkpoint.** EM-016 says commits land "on the run's task branch." No EM-NNN requires the branch to be created before `execute_workflow`. Workspace-model.md §5.8 presumably owns this, but this spec does not cite that precondition — if workspace-model moves branch creation late, the first checkpoint fails with "no branch." Add a MUST that names the precondition explicitly, even if by reference.

3. **Sub-workflow version resolution is pinned at workflow-load time.** EM-037 says "resolved at workflow-load time," but EM-034 says the sub-workflow "expands in place at runtime." If a sub-workflow registry is updated between load and expansion, which version wins? Spec assumes load-time pin survives until expansion, but never states it. In a daemon that runs for days, load-time can be far from expansion-time.

4. **JSONL is never walked, ever — except when it is.** EM-031 forbids walking JSONL for state reconstruction, carves out "observational JSONL reads for divergence-evidence detection" per reconciliation.md §9.3a. The carve-out is fine but blurry: an investigator-agent that "reads" JSONL to correlate a crash to an event sequence is close to "reconstructing what happened." The boundary needs a sharper rule than "observational-only" — e.g., "may consult JSONL for evidence, but MUST NOT rely on it to reconstitute a run's state beyond what git + Beads already establish."

5. **`Harmonik-Schema-Version` is a single integer.** EM-022 N-1 readability assumes a linear version sequence. If the spec ever needs branching schemas (e.g., during a long migration), N-1 compatibility breaks silently. Not MVH-blocking, but a post-MVH trap that OQ-EM-002 brushes against but does not close.

6. **§A.3 rationale references components.md §2.1b as the source of the rejected-alternatives reasoning.** Per template bootstrap rule, this is allowed — but the citation is load-bearing argument the SPEC should now own, not a factual cite. Once components.md is deprecated post-foundation, this rationale floats loose. Inline the argument once, then let the foundation doc rot.

7. **The Outcome.context_updates map is applied atomically before edge selection.** EM-005 says `context_updates` are "applied to the run's shared context" but never says in which order this happens relative to condition evaluation in EM-041. If context is updated BEFORE the cascade, the edge conditions see post-update state; if AFTER, they see pre-update state. Attractor's convention (per components.md §2.4 recon) is pre-cascade update, but this spec is silent.

8. **Gate denial does not require a checkpoint.** EM-042 says "gate denial leaves the run in the source state." No new transition, no new commit — implied. But a denied gate is an observable decision the improvement loop will want to audit. Is that audit trail a JSONL-only record (no git commit) or does a gate denial produce a transition with `transition_kind=forward` that happens to land back on the source state? Spec assumes the former but does not say it.
