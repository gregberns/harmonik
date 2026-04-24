# Round 1 Cross-Spec Architect Review — execution-model.md v0.1

## Verdict summary

The spec sits roughly right in the foundation graph: it correctly claims
ownership of the core execution types, correctly cedes event shapes, handler
interface, workspace lease, policy grammar, and reconciliation verdicts, and
gets the three-store authority rule right. Three material issues: (1) the
front-matter `depends-on` list disagrees with components.md §Component 2 and
introduces a likely cycle with event-model; (2) several inter-spec citations
that the body treats as co-references should arguably be depends-on (and vice
versa); (3) a handful of sections silently cite specs that will not exist at
finalize time without a bootstrap citation.

## Dependency graph correctness

**Front-matter `depends-on` = [architecture, event-model].** Components.md
§Component 2 Dependencies says *only* architecture.md (§1.1, §1.3, §1.5, §1.8,
§1.9). components.md §Component 3 (event-model) Dependencies in turn says
event-model depends on execution-model. Listing event-model in this spec's
`depends-on` therefore (a) disagrees with decompose's dependency graph and (b)
implies a cycle (execution-model → event-model → execution-model).

The intra-foundation §"Co-dependency resolution rules" in components.md is
explicit: `execution-model ↔ event-model` is resolved directionally —
execution-model owns types, event-model owns wire formats, each cites the
other. Under the template §9.3 co-reference rule, the correct shape is:
execution-model's front-matter `depends-on: [architecture]`; every cite of
`[event-model.md §…]` lives in §9.3 Co-references.

**Task prompt says `depends-on: [architecture, event-model, process-lifecycle]`**
— the spec itself has only two entries and no process-lifecycle dependency.
The spec does reference `[process-lifecycle.md §8.2]` in §9.3, so listing
process-lifecycle as a depends-on is defensible ONLY if that cite is in fact
normative. It isn't: §9.3's wording ("daemon walks the git checkpoint trail
defined here during the reconciliation phase of startup") describes
process-lifecycle's consumption of THIS spec, which is a reverse dependency,
not a forward dependency. Keep process-lifecycle in §9.3 or drop it entirely;
do NOT put it in `depends-on`.

**Missing from §9.3 that the body cites.** `[operator-nfr.md §7.3]` is cited
in the §7.1 run-state-machine note ("operate BETWEEN runs per operator-nfr.md
§7.3") and in §8.4, but §9.3 lists only operator-nfr.md §7.5. Add §7.3 to
§9.3, or drop the §7.1 / §8.4 prose cites.

**Legitimately needed §9.3 additions.** `[handler-contract.md §4.1]` (Outcome
shape obligation), `[handler-contract.md §4.5]` (ErrTransient/Structural/
Deterministic/Canceled/Budget sentinels), `[handler-contract.md §4.11]`
(skills resolution) are all cited in the body (EM-005, §8, EM-008). None
appear in §9.3. Add them.

**Miscategorized as depends-on (if event-model stays).** Even if the authors
elect to keep event-model in `depends-on`, the body's actual cites to
event-model are: §3.1 (envelope — consumes run_id/state_id fields this spec
originates), §3.2 (taxonomy — lists events this spec emits), §3.4 (fsync), and
§3.6 (replay split). §3.1 and §3.2 are bidirectional consume-produce
relationships; §3.4 and §3.6 are genuine read-from. A directional split
(depends-on §3.4+§3.6, co-reference §3.1+§3.2) might model the relationship
more honestly than either "all depends-on" or "all co-reference."

## Type placement

| Type | Placement | Verdict |
|---|---|---|
| `Workflow` | execution-model §6.1 | Correct — workflow is an execution artifact. |
| `Node` | execution-model §6.1 | Correct. |
| `Edge` | execution-model §6.1 | Correct. |
| `State` | execution-model §6.1 | Correct. |
| `Transition` | execution-model §6.1 | Correct. |
| `Checkpoint` | execution-model §6.1 | Correct — schema belongs with the cadence rule. |
| `Outcome` | execution-model §6.1 | Correct, even though handler produces it (handler-contract owns the *producing interface*, this spec owns the *type*, per components.md §Co-dep rules — handler-contract emits instances conforming to this shape). |
| `OutcomeStatus` / `NodeType` / `IdempotencyClass` / `TransitionKind` enums | execution-model §6.1 | Correct. |
| `RunID`, `StateID`, `TransitionID` | Used as `UUID` in §6.1 | Under-declared. The task prompt lists them as concrete type names; components.md §Component 6 subsystem-organization names `RunID`, `StateID`, `TransitionID`, `BeadID` as shared types in `internal/core`. This spec uses the generic `UUID` type and never names the aliases. Either add a one-line note in §3 Glossary or §6.1 intro that `RunID = UUID`, etc., so event-model and other specs can cite `[execution-model.md §6.1 RunID]` per the type-citation form. |
| `BeadID` | Used as `String` in Run record | Under-declared AND inconsistent. §6.1 `Run` has `bead_id: String`; §6.1 `Checkpoint` also `bead_id: String`. Beads-integration.md (components.md §10.3) calls these "stable bead IDs" with no specified shape. If BeadID is a typed alias, declare it. If it's opaque string, say so explicitly. |
| `PolicyRef`, `PolicyExpression` | Used in §6.1 Workflow and Edge | Referenced without definition. Appropriate if `[control-points.md §6.5]` declares the type — the spec cites that section, but the cite is in a schema-field comment, not in §9.3. Add control-points §6.5 to §9.3 co-references. |
| `ActionDescriptor`, `AxisTags`, `ModeTag` | Used in §6.1 Node and Transition | Never defined. `AxisTags` and `ModeTag` presumably live in architecture.md (four-axis classification). `ActionDescriptor` is introduced without any pointer at all. Either declare locally or cite the defining spec. |
| `WorkspaceRef` | Used in §6.1 Run | Correctly cited to `[workspace-model.md §5.1]`. Good. |
| `CommitRange` | Used in §6.1 State | Never defined. Declare it (it's clearly `[first_commit_sha, last_commit_sha]`) or remove in favor of explicit fields. |

## Schema-vs-downstream fit

**event-model will need.** event-model.md §3.2 (per components.md) declares
`transition_event`, `checkpoint_written`, `state_entered`, `state_exited`,
`run_started/completed/failed`, `sub_workflow_entered/exited`. Each needs
`run_id`, `state_id`, `transition_id`. Execution-model §6.1 exposes
`transition_id`, `state_id`, `run_id` as UUIDs — event-model can consume.
`checkpoint_written` per components.md §3.2 requires `run_id`, `state_id`,
`transition_id`, optional `bead_id` — all present in execution-model's
Checkpoint record. Fit is clean.

**handler-contract will need.** Components.md §4.1 `Handler.Wait` returns
`(Outcome, error)`; the Outcome schema in execution-model §6.1 supplies
`status`, `preferred_label`, `suggested_next_ids`, `context_updates`, `notes`.
Handler-contract also per §4.5 wraps errors in five sentinel categories
(`ErrTransient`, `ErrStructural`, `ErrDeterministic`, `ErrCanceled`,
`ErrBudget`); EM-005's Outcome does NOT carry an error-category field. Instead
§8 of this spec says the classifier determines class "from structured fields
of the outcome (exit code, error type, timeout flag, budget counter)" — but
none of those fields is in the Outcome record. Either (a) add a `failure_class`
or `error_category` field to Outcome for FAIL-status outcomes, or (b) declare
explicitly that the classifier reads handler-contract's error sentinel (not
the Outcome) — in which case the §8 detection prose ("Outcome fields indicate
…") is misleading.

**workspace-model will need.** `Run.input` is typed as `WorkspaceRef` and
cited correctly. workspace-model.md §5.8 per components.md needs the task
branch name convention keyed on `run_id` and `node_id`; `run_id` exposed. Fit
is clean.

**reconciliation will need.** Components.md §9.3 Cat 1/2 detectors consume
node `idempotency_class` — exposed on Node record. Cat 3a/3b detectors
consume `Harmonik-Run-ID` and `Harmonik-Transition-ID` trailers — §6.2 table
declares both. §9.3 Cat 6a consumes trailer-vs-sibling-file mismatch — EM-018
and EM-INV-003 provide the sibling-file discovery contract. Fit is clean.

## Emission vs schema ownership

§6.5 correctly declares execution-model as normative-for-WHEN and cites
event-model as normative-for-SHAPE. No schema redeclaration found for any of
the nine events in §6.5.

**Two minor leakage risks.**

1. §7.1 run-state-machine's Emits column includes `budget_exhausted`. Per
components.md §6.9 + §7.9, budget enforcement (including the
`budget_exhausted` event) is owned by control-points.md. Execution-model's
run can ENTER a `failed` state as a consequence of a budget exhaustion, but
the EMISSION of `budget_exhausted` belongs elsewhere. Rewrite the table cell:
the run enters `failed`; `budget_exhausted` was already emitted by the agent
runner per control-points.md §6.9.

2. §7.1 last row lists `operator_stopped` as emitted by execution-model on
`stop --immediate`. Per components.md §7.3, operator_* events are owned by
operator-nfr.md §7.3. Same fix: this spec emits `run_failed (class canceled)`;
operator-nfr emits `operator_stopped`.

Neither case redeclares a schema, but both over-claim emission.

## Cross-reference form issues

**Bootstrap citations missing.** Per template §Cross-reference convention, if
the target spec does not yet exist, the citation MUST take the bootstrap form
`[docs/foundation/components.md §<N>]`. At 2026-04-23, the only finalized
foundation spec is this one (v0.1 draft). Every `[event-model.md §…]`,
`[handler-contract.md §…]`, `[workspace-model.md §…]`, `[control-points.md §…]`,
`[reconciliation.md §…]`, `[operator-nfr.md §…]`, `[process-lifecycle.md §…]`,
`[beads-integration.md §…]`, and `[architecture.md §…]` cite is forward-
citing a non-existent spec file. Two ways to handle:
(a) leave as-is and accept migration debt (spec acknowledges this model for
most of its references),
(b) switch to bootstrap form for every forward cite.

Neither the spec body nor its Revision history acknowledges the bootstrap-
citation status. Add an OQ or an informative note in §9 ("all inter-spec
citations are forward-references; migrate to bootstrap form if target specs
are not finalized at this spec's review gate").

**Intra-spec `§N.N` forms.** All intra-spec cites of the form `§4.3.EM-014`,
`§4.4.EM-018`, etc. resolve — each requirement ID appears under the cited
section. No broken internal refs found.

**Letter suffix avoidance.** §4.3.EM-014 in prose reads "per §4.3.EM-014";
this is template-legal (requirement ID with section hint). No `§4.2(b)` form
found; good.

**Glossary entries marked "(see §4.1, §4.4)".** These are legal per template
§3 convention. Good.

**§2.2 out-of-scope items** cite e.g. `[event-model.md §3.2]`, `[reconciliation.md §9.2]`.
These citations are parenthetical-why and do not need to appear in §9 —
template §2 requires naming WHY, which is done. Good.

## Failure taxonomy completeness

All six classes (`transient`, `structural`, `deterministic`, `canceled`,
`budget_exhausted`, `compilation_loop`) have detection, default response,
escalation, and emitted event in §8. Each row is populated.

**Well-justified:** `transient`, `structural`, `deterministic`, `canceled` —
clean mechanism-tagged detection via `ErrX` sentinels from handler-contract
§4.5.

**Two weaknesses:**

1. **`budget_exhausted` row — emitter is wrong.** Row says "emitted event:
`budget_exhausted`; `run_failed` if terminal." Per components.md §6.9, the
agent runner emits `budget_exhausted` at DISPATCH (before launching the
handler); the run's state machine then transitions to failed. Execution-model
does not own the `budget_exhausted` emission.

2. **`compilation_loop` detection maps to `ErrStructural`** — the §8 prose at
the bottom says "error mapping is `ErrStructural` with a `compilation_loop`
sub-tag." Handler-contract's sentinel vocabulary (components.md §4.5) is five
categories: transient/structural/deterministic/canceled/budget. A sixth class
that does NOT have its own sentinel but piggybacks on `ErrStructural` with a
sub-tag is a novel pattern handler-contract has not been consulted about.
Either coordinate with handler-contract (add `ErrCompilationLoop` as a
sentinel, OR formalize the sub-tag mechanism), or flag as an OQ.

## Recommendations

1. **Front-matter `depends-on` cleanup.** Remove `event-model` from
`depends-on`; move event-model cites to §9.3 co-references. Rationale:
components.md §Co-dependency rules resolves this edge directionally and
event-model's own `depends-on` already includes execution-model.

2. **§9.3 expansion.** Add `[handler-contract.md §4.1, §4.5, §4.11]` (Outcome
obligation, error sentinels, skill resolution), `[control-points.md §6.5]`
(policy schema / PolicyExpression), `[operator-nfr.md §7.3]` (between-runs
operator semantics). These are cited in the body; add them to §9.3 or remove
the body cites.

3. **Type alias declarations.** Either in §3 Glossary or a one-line §6.1
preamble, name `RunID`, `StateID`, `TransitionID`, `BeadID` as typed aliases
of UUID (for the first three) and opaque String (for BeadID). Downstream
specs need to cite `[execution-model.md §6.1 RunID]` per template's type-cite
form; that cite fails if the type isn't named here. Similarly, define or cite
`CommitRange`, `ActionDescriptor`, `AxisTags`, `ModeTag`.

4. **Outcome / failure-class shape.** Decide whether `Outcome` carries an
error-category field (for FAIL outcomes) or whether the classifier reads
handler-contract's `ErrX` sentinel directly. Write the decision into §4.1
EM-005 and into §8's detection rules so the two agree.

5. **§7.1 and §8 emission ownership.** Strip `budget_exhausted` and
`operator_stopped` from the Emits column where they imply this spec emits
them; replace with `run_failed (class X)` + informative note that the
upstream event was emitted by the owning subsystem (control-points.md §6.9 /
operator-nfr.md §7.3).

6. **Bootstrap-citation status.** Add an informative note in §9 (or an OQ)
acknowledging that most inter-spec cites are forward-references to specs not
yet drafted; document the migration plan per template §Cross-reference
convention ("bootstrap refs migrate within one revision cycle").

## Affirmations

1. **Sub-workflow run-model is consistently asserted.** EM-034, EM-035,
EM-036, EM-037, the §4.8 prose, the §3 Glossary run definition, and §7.1's
single `run_started` emission per execution all converge on one-run-per-
workflow-invocation. No ambiguity in the shape.

2. **Three-store authority is codified correctly.** EM-INV-001, EM-INV-005,
EM-031, and §2.1 in-scope bullet 7 all say git-wins-on-completion and
JSONL-is-not-state-authoritative. The §7.2 pseudocode correctly makes the
git.commit step the durability boundary. This is precisely the core-scope §2
ground-rule pinned normatively.

3. **Outcome-spine segmentation is clean.** §4.6 EM-027 names the five
segments (handler outcome → hook → gate → transition → event) with each
segment's owning spec. §6.5 draws the emit-when / shape-where boundary with
event-model correctly. No segment is double-owned or silently skipped.

4. **Failure-class table structure matches the template §8 taxonomy-first
shape.** Each class has detection, default response, escalation, emitted event
— the four columns the template requires for an error taxonomy. The `tags:`
line at §8's head (mechanism-tagged classifier) is well-placed.
