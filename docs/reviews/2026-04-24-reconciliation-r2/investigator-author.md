# Reconciliation R2 — Investigator-Author Review

Persona: the engineer tasked with building the investigator workflow this spec describes — prompt construction, evidence harness, verdict emission, verdict executor. I am trying to actually wire this up. Materials read: `specs/reconciliation/spec.md` v0.3.0 (880 lines), `specs/reconciliation/schemas.md` v0.3.0 (221 lines), and the cited surfaces of `handler-contract.md`, `process-lifecycle.md`, `event-model.md`, `workspace-model.md`, `operator-nfr.md`, `execution-model.md`, `architecture.md`.

---

## 1. Verdict summary

**Proceed with revisions before finalization.** The taxonomy shell is stable, the auto-resolver subclass carve-outs (3a/3b/3c) are good engineering, and the restart-classification loop closes via RC-002 + RC-003. The verdict vocabulary plus the verdict-execution table in `schemas.md §6.2` is the strongest authoring-ready surface — the executor is buildable from that table alone.

But the investigator-author surface — the four things I was hired to build — has load-bearing gaps:

- **LaunchSpec ↔ InvestigatorInput is under-specified at the wire.** RC-015 says InvestigatorInput "extends" LaunchSpec, but HC-006 pins LaunchSpec's fields and its `snapshot_token` is `String | None` (scalar), while `schemas.md §6.1 SnapshotToken` is a 3-field record. Nothing in the spec says whether those extension fields ride in LaunchSpec, are read lazily by the investigator via its skills, or are delivered through a side-channel. I cannot build the dispatcher without this.
- **Verdict emission has no wire contract.** `reconciliation_verdict_emitted` is an event. The verdict commit is a commit. The `outcome_emitted` progress-stream message is HC's existing terminal-message contract. Nothing says how the investigator subprocess's text output becomes the `VerdictEvent` — who parses it, who validates the schema, who writes the commit. Three plausible wirings exist; pick one.
- **Evidence-corroboration contract (RC-019a) contradicts the upstream EV-023a shape it claims to implement.** RC-019a demands `evidence.sources[]` with `>= 2` entries; EV-023a and `event-model.md §8.6.8` carry `corroboration: <enum: git-corroborated | beads-corroborated>` — a two-valued scalar, no array. The sensor RC-019a describes cannot compile against the payload the event-model spec owns.
- **The reconciliation-workflow self-classification question is not resolved.** PL-025 says a crash-during-startup-reconciliation re-runs from PL-005 step 0; RC-003 says the outer run is re-classified from original category; RC-026 says reconciliation runs before `ready`. None of these answers what happens to a reconciliation workflow's own branch after mid-flight crash — RC-002 ("no mid-investigation durable state") and PL-025 ("typically Cat 5 for the outer run and Cat 3b for the investigator") point in different directions about whether there is a branch to classify at all.
- **§8.12 action-mapping table and `schemas.md §6.3` carry a specific row I cannot mechanically dispatch** for Cat 3c (auto-verdict `accept-close-with-note`) and Cat 3b (re-execute a prior verdict) without subsystem-local reads that the spec does not authorize.

Detail below. Severity-ranked recommendations at §13.

---

## 2. The four handoff-mandated probes

### 2.1 LaunchSpec ↔ InvestigatorInput alignment [BLOCKING]

**What the spec says.**

- `handler-contract.md §6.1` defines `LaunchSpec` as a fixed record (line 587-602). Required fields: `run_id, workflow_id, node_id, agent_type, workspace_path, required_skills, skill_search_paths, timeout, provisioning_timeout, budget, freedom_profile_ref`. Optional: `bead_id, snapshot_token, schema_version`. `snapshot_token` is declared as `String | None` — a scalar opaque string.
- HC glossary line 71: "snapshot token — an opaque token carried in LaunchSpec for investigator-agent handlers, binding the agent's reads to a captured `(git_head_hash, beads_audit_entry_id)` per [reconciliation/spec.md §4.4]."
- `schemas.md §6.1 SnapshotToken` (lines 20-25) defines it as a **structured three-field record**: `git_head_hash`, `beads_audit_entry_id`, `captured_at_timestamp`.
- RC-015 (spec.md line 422-434) declares `InvestigatorInput` is "an **adapted form of the LaunchSpec** record ... it carries every LaunchSpec field ... AND the reconciliation-specific fields below." Then it lists seven reconciliation-specific fields (snapshot token, run metadata, bead ID + Beads record, git state at last checkpoint, JSONL tail, workspace state, agent session log).
- `schemas.md §6.1 InvestigatorInput` (lines 28-43) formalizes thirteen fields. None of `bead_record`, `last_checkpoint`, `last_transition`, `jsonl_tail`, `workspace_state`, `session_log_ref`, `category`, `playbook_ref`, `budget_wall_clock_seconds` appear in LaunchSpec per HC-006.

**What's broken.**

1. **Type mismatch on `snapshot_token`.** LaunchSpec declares `String | None`. The RC record declares a struct with three fields. Either (a) LaunchSpec actually carries a JSON-serialized form of the record (in which case HC-006 should say so; the type `String | None` is a lie about shape), OR (b) the "opaque token" is an ID reference the investigator dereferences through a skill. The spec is silent. The cross-spec-architect R1 review flagged exactly this and the response in §0.3.0 was to rewrite RC-015 to say "adapted / extended LaunchSpec" — that phrasing does not close the gap; it papers over it.

2. **Extension fields are not declared extensions.** LaunchSpec per HC-006 enumerates its required/optional fields and HC §6.3 says adding a field is a schema-version bump. RC-015 says InvestigatorInput "carries every LaunchSpec field AND the reconciliation-specific fields below" — but RC does not propose a LaunchSpec schema bump, does not name a new LaunchSpec version, and does not declare the reconciliation-specific fields as LaunchSpec extensions. OQ-RC-002 tracks `workflow_class` as an EM-owned Workflow extension; the analogous OQ for LaunchSpec doesn't exist. Either:
   - (a) InvestigatorInput is a separate payload the daemon delivers *alongside* LaunchSpec (e.g., via a file-path referenced by a new LaunchSpec optional field, analogous to HC-005's 1-MiB cutover). Then HC-006 needs the extra field and RC-015 needs to say "InvestigatorInput is delivered via LaunchSpec.investigator_input_ref as a path to a JSON file."
   - (b) InvestigatorInput is a *logical view* the investigator itself assembles via its skills (Beads-CLI, git-inspection) bounded by the snapshot token. Then `schemas.md §6.1` should declare it as a view, not a record the daemon constructs, and RC-015 should drop the LaunchSpec-extension language.
   - (c) InvestigatorInput IS LaunchSpec-extended, in which case HC-006 needs to be amended and the `snapshot_token` field needs to be either a `SnapshotToken` record or a `String` that deserializes to one (with the serialization contract named).

   The spec picks none of (a)/(b)/(c). The R1 cross-spec-architect review lists these three options verbatim (R1 critic line 133-144); the R2 integration response should have picked one. It did not.

3. **RC-015 says `bead_record` is "the Beads record for that bead as-of `beads_audit_entry_id`".** `BeadRecord` is owned by `beads-integration.md §4.3`; the spec cites it. The subject is: how is this record captured? If the daemon pre-captures it at dispatch time, it goes into InvestigatorInput; if the investigator queries Beads at runtime, it uses its Beads-CLI skill. The phrase "as-of `beads_audit_entry_id`" suggests the former (immutable at snapshot), but Beads audit entries are a log — the daemon has to walk the audit log to reconstruct the bead state at a specific entry, which is not trivial. If the investigator does it itself, the snapshot token is the bound. The spec assumes the daemon pre-captures; I don't see the requirement that says so.

4. **`jsonl_tail: List<EventEnvelope>` is unbounded.** RC-014 says "events since last checkpoint" — but if the last checkpoint is an hour old and 50k events have landed, the tail is 50k entries. LaunchSpec's 1-MiB cutover (HC-005) would trigger file-path delivery, but the cutover applies to LaunchSpec itself. If InvestigatorInput is *not* LaunchSpec, HC-005 doesn't help. Either declare a size bound on `jsonl_tail` or require file-path delivery for InvestigatorInput.

**What I cannot build until this is resolved.**

- The daemon-side dispatcher that constructs LaunchSpec + InvestigatorInput and hands them to HC-launch.
- The investigator subprocess's read logic (does it receive pre-assembled data or query skills?).
- Any HC-compatible investigator handler.

**Proposed RC-015 rewrite.** Either:

```
RC-015 (option A — daemon pre-assembles, delivered out-of-band):
  The daemon MUST pre-assemble an InvestigatorInput record per
  [schemas.md §6.1] and write it to a file at
  .harmonik/reconciliation/<investigator_run_id>/investigator_input.json
  before launching the handler. The LaunchSpec MUST carry a new optional
  field investigator_input_ref pointing at that file path. Handler-contract
  MUST accept this field (spec-draft obligation on HC; tracked in new OQ).
  The SnapshotToken record serializes as a JSON struct; the LaunchSpec
  snapshot_token field MAY be retained for cross-spec-compat (carrying
  the serialized JSON), or MAY be dropped once investigator_input_ref lands.
```

or

```
RC-015 (option B — investigator self-assembles, snapshot-token-bounded):
  The investigator receives only LaunchSpec per HC-006. The snapshot_token
  in LaunchSpec binds the investigator's reads — the investigator uses its
  Beads-CLI skill to fetch the Beads record as-of beads_audit_entry_id, its
  git-inspection skill to read the checkpoint and transition-record
  sibling file at git_head_hash, and its workspace-inspection skill to
  probe the workspace state. The jsonl_tail is read via the divergence-
  evidence reader skill (RC-014 scoped reads). The InvestigatorInput
  record shape in [schemas.md §6.1] documents the *view* the investigator
  constructs; the daemon does NOT pre-assemble it. Playbook_ref,
  category, and budget are delivered as required_skills parameters or
  YAML policy references per [control-points.md §4.7].
```

I lean option (B): it's consistent with RC-004 (investigators get Beads-CLI + git-inspection skills), avoids bloating LaunchSpec, and the snapshot token IS the bounding discipline. But I'm not the decider; the spec authors are. Pick one.

### 2.2 Evidence-corroboration gating [BLOCKING]

**What the spec says.**

- RC-014 (spec.md line 373-392) gives permitted and forbidden JSONL reads. Cat 2 liveness probe: "a controlled, indexed query answering 'has `run_completed` or `run_failed` been emitted for this run since the last checkpoint?'" Query MUST be against the bounded divergence-evidence reader, NOT a live-follow tail.
- RC-019a (lines 394-405) declares every `store_divergence_detected` must "cite the specific fact drawn from each of at least two stores ... that, taken together, witness the divergence. A single-store observation ... MUST NOT trigger emission." Payload MUST enumerate corroborating facts under `evidence.sources` (shape declared in [event-model.md §8.6.10]).
- RC-INV-004 sensor: "Detector emission layer MUST validate `evidence.sources.length >= 2`."
- EV-023a (event-model.md line 425-429) declares three classifications: `git-corroborated`, `beads-corroborated`, or `inconclusive`. Detector emits `store_divergence_detected` ONLY for corroborated evidence; `inconclusive` goes to the separate `divergence_inconclusive` event.
- `event-model.md §6.3` store_divergence_detected payload (line 751): `corroboration: <enum: git-corroborated | beads-corroborated>` — a two-valued scalar enum, NOT an array.

**What's broken.**

1. **The RC-19a sensor cannot compile.** RC-INV-004 says "validate `evidence.sources.length >= 2`." The field is not `evidence.sources`; it's `corroboration`. And the field is not a list; it's a scalar enum. RC says "two-store corroboration required"; EV gives you a single two-valued enum that tells you *which* single store corroborated against the originating event. The two specs disagree about the corroboration shape. This is a blocker for the sensor.

2. **The corroboration-model mismatch is semantic, not just structural.** EV-023a's model: an observational event has an `evidence_ref`; the detector tests that ref against git, or tests a bead_id against Beads, and gets back ONE of `git-corroborated | beads-corroborated | inconclusive`. The corroboration is between *the event* and *one of the authoritative stores* — a two-source comparison always. RC's model: the divergence is witnessed by at least two stores disagreeing — a three-way corroboration (event + git + Beads, OR git + Beads + JSONL tail). These are DIFFERENT rules. RC-019a's "`evidence.sources.length >= 2`" language suggests an array of source citations; EV's payload has no such array.

3. **RC-014 Cat 2 liveness probe is underspecified.** "has `run_completed` or `run_failed` been emitted for this run since the last checkpoint?" — the divergence-evidence reader interface is unspecified. Does it:
   - scan the JSONL file filtering by run_id (unbounded read if the JSONL is large);
   - query an in-memory index the daemon maintains (where is the index spec?);
   - use a separate indexed consumer subscribed to the `run_completed` and `run_failed` streams per [event-model.md §4.3] (no spec says this consumer exists)?
   Without picking one, I cannot build the Cat 2 detector. This is load-bearing because Cat 2 is the *most common* non-trivial detector — every non-idempotent agent crash goes through it.

4. **"Controlled, indexed query" vs "live-follow tail".** RC-014 says "controlled, indexed query against the bounded JSONL divergence-evidence reader, NOT a live-follow tail." A "bounded reader" and an "indexed query" are not the same primitive. A bounded reader bounds read scope by a time window or byte offset; an indexed query uses a pre-built index (hash table, B-tree, sorted file). The spec uses both phrases interchangeably. The implementer needs: which is it? If it's a bounded reader, what's the bound (time? size?)? If it's an indexed query, where does the index live (on-disk? in-memory-rebuilt-on-startup?) and who maintains it?

**What I cannot build until this is resolved.**

- The `store_divergence_detected` payload emitter (corroboration shape is contradictory).
- The RC-INV-004 sensor (no `evidence.sources` field exists to read).
- The Cat 2 detector (no liveness-probe primitive is named).
- The "divergence-evidence reader" (primitive is not specified).

**Proposed resolution.** Either:

- **Adopt EV-023a's shape verbatim.** Rewrite RC-019a to require `corroboration ∈ {git-corroborated, beads-corroborated}` (not `inconclusive`) on every `store_divergence_detected`; drop the `evidence.sources[]` mental model; rewrite RC-INV-004 sensor to assert `corroboration != null AND corroboration != inconclusive`. Cat 6b carve-out stays: if even single-source reads fail, skip the emission and escalate.
- **OR push for an EV amendment** (§8.6.8 payload) to add `evidence.sources: List<{store, ref}>` with `sources.length >= 2`. This is the stronger posture RC seems to want. But it requires EV to sign off — file an OQ under RC *and* coordinate with event-model-author. This is a cross-spec amendment, not a RC-local fix.

Either way, pick one and ensure the RC-019a sensor and the §8.6.8 payload agree on the field name and the type.

### 2.3 Verdict-enum exhaustiveness [IMPORTANT]

**What the spec says.**

- RC-020 (line 481-487) declares the enum is seven-valued: `resume-here, resume-with-context, reset-to-checkpoint, reopen-bead, accept-close-with-note, no-op-accept, escalate-to-human`.
- §8.12 action-mapping table lists "Typical verdict (if investigator)" per category.
- `schemas.md §6.2` verdict-execution table maps each verdict to a mechanical action.

**Walk the cross-product.** For every investigator-dispatched category (Cat 2, Cat 3, Cat 6a) the investigator MUST emit exactly one of the seven. Can every reachable evidence-state map to a verdict?

- **Cat 2 (non-idempotent in-flight).** §8.12 typical: `resume-with-context` / `reset-to-checkpoint` / `reopen-bead`. Other enum values:
  - `resume-here` — implicitly OK (investigator concludes no context change needed and the worktree state is fine).
  - `accept-close-with-note` — inconsistent with Cat 2 by premise (the bead is `in_progress` and should remain so for a legitimate resume; accepting a close would silently drop work).
  - `no-op-accept` — nominally legitimate (investigator confirms current mid-flight state is consistent and the run is just *waiting* for orchestrator dispatch) but the mechanical action (`reconciliation_verdict_executed` emission without dispatch) leaves the outer run stuck — no node gets re-spawned. This is a trap. RC-025 says for `no-op-accept`: "The outer run is left untouched; subsequent dispatch cycles treat the run as ordinary." What IS the ordinary dispatch for a non-idempotent crashed run? There isn't one — that's why Cat 2 dispatched an investigator in the first place. `no-op-accept` for Cat 2 is a dead-letter state.
  - `escalate-to-human` — OK as fallback.

  **Gap:** `no-op-accept` for Cat 2 should be forbidden or explicitly re-classified as Cat 6a on emission. The spec does not say which.

- **Cat 3 generic.** §8.12 typical: `accept-close-with-note` / `reopen-bead` / `no-op-accept`. All other verdicts reachable:
  - `resume-here`, `resume-with-context`, `reset-to-checkpoint` — an investigator could plausibly emit any of these if it concludes the target run should continue from where it left off. Unclear whether Cat 3 allows these. The spec is silent.
  - `escalate-to-human` — OK.

- **Cat 6a.** §8.12 typical: `escalate-to-human` (default; investigator MAY downgrade). "Downgrade" is not defined as an enum transition — which verdicts count as "downgrades"? All six non-escalate options? The spec says "the investigator MAY downgrade to a repair path" (line 209) but doesn't enumerate repair paths.

**Missing verdicts?**

- **`defer-to-operator`.** The operator-override contract of RC-027 / ON-014 lets an operator veto/confirm a verdict, but there's no verdict the investigator can emit meaning "I want the operator to decide, but I'm not quite escalating to human." `escalate-to-human` is the only escape hatch. If an operator opts *out* of the confirmation policy (default per RC-027), the investigator has no way to pull the operator in selectively. This may be the intended design (default is auto-execute; operator opts into review wholesale) but it's worth saying so.
- **`abandon-run`.** No verdict means "discard this run as unrecoverable, do not create a new one." Closest is `reopen-bead` (keeps the bead, spawns a new run) or `accept-close-with-note` (closes the bead cleanly). What if the investigator concludes "this bead should be closed because the claim was wrong, but the work product is unusable"? `accept-close-with-note` without a note about the unusable work product is overloaded. Minor.
- **`split-verdict`.** A `reopen-bead` verdict reopens the bead but leaves the investigator-produced evidence on the reconciliation commit. If the investigator wants to say "reopen the bead AND promote my captured context into the new run's starting state" — that's a hybrid. Current workaround: capture context into a well-known CASS-indexed location that the new run's orchestrator reads. Not terrible but not in the spec.

**Verdict parametrization.** Two verdicts need parameters and one doesn't quite:

- `resume-with-context` requires `context: String | None` — per `schemas.md §6.1 VerdictEvent` line 69: "required iff verdict = resume-with-context." OK.
- `reset-to-checkpoint` requires `checkpoint_ref: UUID | None` — per line 70: "required iff verdict = reset-to-checkpoint." OK.
- `accept-close-with-note` — the "note" is not a field; it's described in RC-025 line 541 as "append an annotation to the reconciliation commit." Where does the note live on the VerdictEvent record? It's not a field. The note goes into the commit body via RC-022's "any evidence the investigator produced." OK by inference, but worth stating.

**Recommendation.** Add an RC-020a (or RC-020b) that says:

1. For each category, declare which verdicts are valid. Example table:

   | Category | Allowed verdicts |
   |---|---|
   | Cat 2 | resume-here, resume-with-context, reset-to-checkpoint, reopen-bead, escalate-to-human |
   | Cat 3 generic | resume-here, resume-with-context, reset-to-checkpoint, reopen-bead, accept-close-with-note, no-op-accept, escalate-to-human |
   | Cat 6a | (all seven; `escalate-to-human` is default) |

2. An investigator emitting a verdict outside the category's allowed set MUST route through RC-023 malformation with a new reason `verdict-not-allowed-for-category`.

Also: clarify that `accept-close-with-note`'s "note" is in the reconciliation commit body; or add an optional `note: String | None` field to `VerdictEvent`.

### 2.4 Reconciliation-workflow bootstrapping ordering [IMPORTANT — self-referentially subtle]

**What the spec says.**

- RC-001: reconciliation runs as a normal harmonik workflow.
- RC-002: reconciliation workflows emit exactly one checkpoint commit (the verdict commit). No intermediate state is durable.
- RC-003: "A daemon crash during reconciliation MUST leave no mid-investigation durable state. On restart, the outer run ... MUST be re-classified from its original category."
- RC-003 also: "Reconciliation-workflow nodes MUST NOT be classified under §8 Cat 1 or Cat 2."
- RC-026: "the startup detector MUST treat a reconciliation workflow as resolved ONLY if both the verdict commit AND the verdict-executed commit ... are present. A reconciliation workflow with a verdict commit but no verdict-executed commit MUST be classified as §8.5 Cat 3b."
- RC-026 continued: "The daemon startup sequence MUST dispatch the reconciliation-workflow classification pass BEFORE any ordinary workflow dispatches."
- PL-005 step 8 dispatches reconciliation; PL-009 says `ready` is gated on reconciliation dispatch completion but NOT on investigator workflows completing.
- PL-025: "If the daemon crashes during startup reconciliation ..., the next restart MUST re-run §PL-005 from step 0. Reconciliation is idempotent per [reconciliation/spec.md §4.1 RC-002]: re-running detection rules against the same git + Beads state produces the same classifications. Reconciliation workflows that were in-flight at crash time are re-classified (typically as Cat 5 for the outer run and Cat 3b for the investigator's verdict-unexecuted case)."
- PL-009a (cited in probe 4): on auto-resolver failure, re-classify as Cat 3, dispatch investigator.

**Chicken-and-egg analysis.**

- Scenario A: **Daemon crashes while an investigator is mid-flight but has NOT yet emitted a verdict commit.**
  RC-002 says no mid-investigation durable state exists; RC-003 says the outer run re-classifies from its original category. Good. The investigator's task branch has been created but carries no verdict commit. What does the detector see for THAT branch? It's a `run/<investigator_run_id>` branch per WM-005 with no checkpoints. If the startup sweep walks `git for-each-ref refs/heads/run/`, it discovers this branch too — is the investigator_run_id treated as an in-flight run? If yes, what category does the investigator_run_id classify under? RC-003 says "Reconciliation-workflow nodes MUST NOT be classified under Cat 1 or Cat 2" — but what about Cat 5 (clean restart) or Cat 6a (integrity violation — branch exists, no lease, no sidecar, sibling transition-record absent)?

  The only plausible answer is Cat 5 (clean restart — no checkpoints, no lease). But Cat 5's detection rule is "Nothing in-flight for this run. Includes orphaned branches from prior runs of beads that have since been re-claimed." An investigator's branch IS an orphan branch. I think the spec intends Cat 5 here, but it doesn't SAY so — and Cat 6a's last bullet ("bead in_progress with two+ task branches ... without Harmonik-Verdict-Executed") arguably fires for the *outer* bead that has its original run branch AND the investigator's branch, both claiming that bead (the investigator's branch via the reconciliation workflow's `workflow_class = reconciliation` tag). RC-003a priority ordering (Cat 6a dominates Cat 5) means this plausibly classifies as Cat 6a → investigator dispatch → investigator branch has no `workflow_class = reconciliation` marker until the verdict commit lands → the classifier sees two task branches claiming the same bead, neither with verdict-executed trailer → it's the exact Cat 6a detection rule.

  **Gap.** The reconciliation workflow's branch needs an *early* marker so the detector doesn't misclassify it. Either:
  - The verdict commit is the ONLY marker (current rule) → mid-flight crash leaves no way to distinguish a crashed investigator's branch from an operator-created orphan. Cat 6a fires. Cat 6a dispatches an investigator. That investigator runs. What's the second investigator's branch? Is THAT branch considered mid-flight? Recursion is allegedly bounded by RC-002 (no durable state) — but the RC-003 claim that "outer run MUST be re-classified from its original category" is ambiguous: the "outer run" could mean the work-bead's run OR the reconciliation-bead's run.
  - **Proposed fix.** Add a new RC requirement: the reconciliation workflow MUST emit a `reconciliation_workflow_started` *event* (not a commit) at dispatch time; the detector uses this event (via the divergence-evidence reader) PLUS the branch-existence fact to classify crashed investigator branches as Cat 5. Alternatively: the reconciliation workflow's branch naming convention is distinct (`reconciliation/<inv_run_id>` rather than `run/<inv_run_id>`), and the detector skips reconciliation-branches during the sweep.

- Scenario B: **Daemon crashes AFTER verdict commit lands but BEFORE verdict-executed commit.**
  Cat 3b fires (RC-026). Auto-resolver re-executes the verdict under a fresh staleness check. This is well-specified.

- Scenario C: **Auto-resolver for Cat 3b fails.**
  PL-009a routes to Cat 3 generic. Cat 3 generic dispatches a (new) investigator. The new investigator sees the prior verdict commit, the target run, and must decide what to do. This is Cat 3 generic dispatch of a re-reconciliation. Fine.

- Scenario D: **Auto-resolver for the reconciliation workflow's OWN auto-resolver fails.**
  Cat 3b is its own auto-resolver. If it fails, PL-009a routes through Cat 3. But Cat 3 dispatches a reconciliation workflow, which has its own auto-resolver (the verdict executor), which if it fails routes through Cat 3, which ... this is the self-referential loop. RC-002a (at-most-one-per-target_run_id) bounds simultaneous dispatches but does NOT bound sequential retries. If the executor fails repeatedly (e.g., `br` is intermittently flaky), the daemon could infinite-loop on Cat 3b → Cat 3 → re-investigator-dispatch → verdict → Cat 3b → ...

  **Gap.** There is no retry cap on the Cat 3b → Cat 3 ladder. Add a requirement: "If the same `target_run_id` has been dispatched through Cat 3b auto-resolution N times (default N=3) within a single daemon generation, the next attempt MUST classify as Cat 6a and dispatch a human-escalating investigator instead of re-executing." This is analogous to WM-022a's conflict-resolution re-dispatch cap.

- Scenario E: **The reconciliation workflow's LaunchSpec fails at handler-contract.md §4.1 (e.g., ErrSkillProvisioningFailed for Beads-CLI skill).**
  HC-022 wraps ErrStructural; EM-classifier routes. But the RUN being classified at startup is the reconciliation workflow's own run. If it fails structurally at launch, there's no verdict commit, the investigator run's branch is empty or never created. On next restart, scenario A applies (above).

**Summary.** The self-classification question is handled by RC-003 + PL-025 for the happy path, but Scenarios A and D are under-specified. The spec implies Cat 5 for crashed investigator branches but never says so, and there is no cap on the Cat 3b → Cat 3 ladder.

**Proposed additions.**

- RC-003b: "A reconciliation workflow's task branch (the investigator's branch per RC-022) with no verdict commit MUST classify as Cat 5 (clean restart) on detector sweep, NOT as Cat 6a. The detector MUST scope Cat 6a's 'two task branches without verdict-executed' rule to branches lacking the `workflow_class = reconciliation` metadata or the `reconciliation/*` branch-name prefix (whichever discipline is chosen)."
- RC-026a (retry cap): "The daemon MUST cap Cat 3b auto-resolver re-attempts for a given `target_run_id` at N=3 per daemon generation. On cap exhaustion, the daemon MUST re-classify as Cat 6a and dispatch an investigator-required workflow with `escalate-to-human` as the default verdict."
- Clarify the reconciliation-workflow's branch naming or workflow-class discipline so the detector can distinguish reconciliation branches from work branches at sweep time. The current scheme (both use `run/<run_id>/`) makes this hard.

---

## 3. Investigator runtime contract — concrete walk

RC-015 line 437 says "the investigator delegates to a Claude Code agent (role: `investigator`), model-class per the YAML policy attached to the reconciliation workflow." RC-004 says S01 ships the prompt templates. Tag is `cognition`.

But where in the spec corpus is there a normative statement that:

- the investigator is an `agent_type = claude-code` handler? (RC does not say; `agent_type` per AR-051 is a lowercase-hyphenated string and MVH reserves `claude-code, pi, claude-twin, pi-twin` — none are `investigator`. `investigator` is a *role*, not an agent-type, per AR-050.)
- the investigator handler implements HC's Handler interface? (Implied by RC-018 citing HC §4.6 cleanup, but never stated as a conformance requirement.)
- the investigator subprocess communicates the verdict over the HC wire protocol via `outcome_emitted`? (This is what implementer R1 inferred at line 597; the spec does not say so explicitly.)

**Gap — role vs agent-type.** The spec uses "investigator" as if it were an agent-type (snapshot_token "present for reconciliation-investigator handlers" in HC-006). Per AR-050 it's a role. If I'm registering an agent type for the investigator, what's the identifier? `claude-code` with role `investigator`? The same Claude Code binary that does implementer work, just with a different prompt? The spec doesn't say; the workflow-library spec that would say it is explicitly out of scope (spec.md line 54).

**Gap — handler-contract conformance is implicit.** RC-018 cites HC-contract cleanup. RC-004 says investigators get Beads-CLI and git-inspection skills "per [control-points.md §4.11] and [handler-contract.md §4.11]" — skill injection is HC-owned. So investigators ARE HC handlers. But RC should state this as a direct requirement: "The investigator MUST be a handler conforming to [handler-contract.md §4.1-§4.11]." Without this, an authoring agent could reasonably write a reconciliation workflow whose investigator is an in-process Go function — and the "cognition-tagged" framing wouldn't preclude it.

**Gap — verdict emission over the wire.** Given that the investigator is an HC handler, its output path is the progress-stream + `outcome_emitted`. But the Outcome record (`execution-model.md §6.1`) has fields `{status, preferred_label, suggested_next_ids, context_updates, notes}` — none are a `verdict` field. The investigator's VerdictEvent has to ride somewhere: (a) in `context_updates` as a map entry with a well-known key; (b) in `notes` as a JSON string; (c) as a separate progress-stream message type not in HC §6.4's list; (d) the daemon watches for a specific event the investigator emits via CASS or via git (e.g., the investigator itself writes the verdict commit, and the daemon polls the branch).

The spec picks none of these. See §5 below for detail.

**Proposed RC addition.** Add RC-015a: "The investigator agent MUST be a handler subprocess conforming to [handler-contract.md §4.1]. Its agent-type identifier is recorded in the reconciliation workflow's DOT per RC-004 (S01-owned); MVH reserves `claude-code` as the investigator-handler agent type. The investigator's role identifier is `investigator` per [architecture.md §4.8]. The investigator's verdict MUST be emitted as the payload of its final progress-stream `outcome_emitted` message, with the VerdictEvent serialized into the Outcome record via [RC-015b]."

Add RC-015b specifying exactly how the VerdictEvent is serialized into the Outcome record. See §5.

---

## 4. Snapshot-token lifecycle

**Issuer.** The daemon captures at investigator-dispatch time (RC-015, RC-024).

**Consumer.** The investigator (for reads) and the verdict-executor (for the staleness check, RC-024).

**Format.** Three fields: `git_head_hash, beads_audit_entry_id, captured_at_timestamp`. But LaunchSpec declares `snapshot_token: String | None` — serialization discipline is unspecified. Is it JSON? Base64? Canonical form per [event-model.md §4.2]? This matters for HC-003 deterministic-launch idempotency.

**Lifetime.** Implicit — from dispatch to verdict-executed commit, or to malformed-verdict/budget-exhausted termination. No requirement names the lifetime explicitly. Is the snapshot token persisted in the reconciliation commit body (per RC-022 "the verdict commit MUST carry the verdict event's payload")? VerdictEvent carries `snapshot_token` so yes, the token survives in the commit. But what about pre-verdict-commit persistence — if the daemon crashes mid-investigation, the token is lost and the next classifier re-captures a fresh one (RC-003 re-classify-from-scratch). OK.

**Staleness criteria.** RC-024 says stale iff:

- target run's checkpoint trail has gained a new commit since snapshot, OR
- target run's bead has changed status in the Beads audit log since snapshot.

The `captured_at_timestamp` field is unused in the staleness check — it's wall-clock, advisory-only per the comment in `schemas.md §6.1` line 24. Why carry it then? Presumably for human-observability on operator surfaces. OK, but the fact that it's unused in the staleness comparison should be stated.

**Can the investigator read git history beyond the snapshot point?** RC-015 says "Git state at the last checkpoint as-of `git_head_hash`." The investigator reads at HEAD = `git_head_hash`. But the investigator's git-inspection skill probably doesn't enforce this bound — git has no "read-only as of revision X" mode. The investigator could, in principle, read HEAD~ (older history) or even HEAD (current, post-snapshot) via its skill. The snapshot token is advisory to the investigator's cognition; it is NOT a mechanical bound. The spec treats it as if it were a mechanical bound (the prose of RC-015 reads as an input contract), but there is no mechanism enforcing it.

**Failure-on-stale-token.** Explicit per RC-024 on the executor side: re-dispatch a fresh reconciliation workflow. On the investigator side: silent — the investigator reasons with whatever the snapshot says, emits a verdict; if the world has moved on, the executor catches it.

**Gap.** `captured_at_timestamp` is load-bearing on the operator surface (what was the snapshot when?) but declared advisory-only. State that explicitly or drop the field. If kept, state the issuance-time canonical format (RFC 3339 UTC per event-model).

**Gap.** The investigator's git-inspection skill does NOT mechanically enforce the `git_head_hash` bound. This is an authoring-discipline issue: the investigator's prompt should instruct it to only read at the bounded HEAD. The prompt template (RC-004, S01-owned) is post-MVH. But the spec should require that the investigator's prompt template include a snapshot-bound reminder — call it a MUST on the S01 workflow library.

**Gap — snapshot token serialization.** LaunchSpec field is `String | None`. The daemon has to serialize the 3-tuple record. Declare the serialization format (JSON, base64'd JSON, canonical tuple string). Example: `"<git_head_hash>:<beads_audit_entry_id>:<RFC3339 timestamp>"` as a colon-separated string. Pick one; add to `schemas.md §6.1`.

---

## 5. Verdict emission and execution protocol — implementer's view

The spec declares several distinct artifacts around verdict emission:

1. A bus event `reconciliation_verdict_emitted` (`event-model.md §8.6.3`).
2. A commit on the investigator's task branch (the "verdict commit") per RC-022.
3. Evidence files under `.harmonik/reconciliation/<investigator_run_id>/` per RC-002, RC-019.
4. A second commit carrying `Harmonik-Verdict-Executed: true` per RC-025.
5. A bus event `reconciliation_verdict_executed` per RC-025.

What does the investigator (the Claude Code subprocess) actually do? What does the daemon do? Which artifact is the investigator's output and which is the daemon's?

**Inference from the spec.**

- The investigator (HC handler subprocess) runs its playbook, gathers evidence, emits its output via the HC progress stream.
- The daemon (per RC-005: "the §4.5 verdict-execution mechanics ... MUST also live in the daemon's Go code") writes the verdict commit, emits the bus events, does the staleness check, writes the verdict-executed commit.

This means: **the investigator itself does NOT write the verdict commit.** The investigator emits a verdict "payload" via progress-stream; the daemon writes the commit. But RC-022 says "The verdict event's emission MUST be atomic with the investigator's verdict commit." That reads as "the investigator commits." Is "the investigator's verdict commit" the commit on the investigator's task branch, or the commit written by the investigator subprocess? The spec is ambiguous.

**Three plausible wirings.**

- **(W1) Investigator commits; daemon observes.** The investigator subprocess writes the verdict commit directly (has a git skill that's allowed to commit on the reconciliation task branch). The daemon observes the commit via a branch watcher, emits `reconciliation_verdict_emitted` based on the commit's contents. Problem: per HC-008, Outcome is delivered via progress-stream — so now there are TWO completion signals (commit + outcome_emitted), and it's not clear which the daemon trusts. Also, if the investigator commits but the subprocess crashes before `outcome_emitted`, the daemon has a verdict commit with no outcome.
- **(W2) Investigator emits via progress-stream; daemon commits.** The investigator packs the VerdictEvent into its Outcome (as a context_updates map entry with a well-known key) and emits `outcome_emitted`. The daemon parses the Outcome, writes the verdict commit and the `reconciliation_verdict_emitted` bus event, then (after staleness check) writes the verdict-executed commit and emits `reconciliation_verdict_executed`. The investigator never touches git. This keeps HC-008's Outcome discipline intact.
- **(W3) Investigator emits via a separate progress-stream message type.** The investigator emits a new message type `reconciliation_verdict` on the progress stream (alongside `agent_output_chunk` etc.) and uses `outcome_emitted` for handler-level cleanliness. The daemon parses the verdict message separately. Requires a new message type in HC §6.4.

**My read:** W2 is cleanest. It reuses the existing HC `outcome_emitted` path. It keeps the investigator from having git write capability (good — the skill injection is simpler: Beads-CLI and git-inspection, not git-write). The daemon is the single writer of commits.

**But the spec doesn't pick.** RC-022's "atomic with the investigator's verdict commit" is ambiguous. RC-021 ("exactly one `reconciliation_verdict_emitted` event per reconciliation workflow") is consistent with W2. RC-025's "append a second commit to the investigator's task branch" is the daemon's action per RC-005, consistent with W2 as well. But the spec never says "the daemon writes the verdict commit" — it is implied.

**Recommended RC additions.**

- RC-022a: "The investigator MUST deliver its verdict as the payload of its final `outcome_emitted` progress-stream message per [handler-contract.md §4.2 HC-008]. The Outcome's `context_updates` map MUST carry the key `harmonik.reconciliation.verdict` with value shaped as `VerdictEvent` per [schemas.md §6.1]. The investigator MUST NOT write commits to its task branch; the daemon writes the verdict commit per RC-022 after receiving the outcome."
- RC-025a: "The daemon is the sole writer of the verdict commit (RC-022) and the verdict-executed commit (RC-025). The investigator subprocess MUST NOT invoke `git commit` against its own task branch."

**Commit composition.** RC-022 says the verdict commit carries the VerdictEvent payload plus evidence. But the commit format is underspecified:

- Is the VerdictEvent embedded in the commit message body as JSON? As git trailers?
- Evidence files are in the commit tree — that's clear.
- The commit message trailers per `execution-model.md §6.2` are `Harmonik-Run-ID, Harmonik-State-ID, Harmonik-Transition-ID, Harmonik-Schema-Version, Harmonik-Bead-ID`. Is the verdict commit expected to carry these? RC-022 says "the verdict event's payload" but doesn't constrain the commit format.
- `schemas.md §6.4` says the verdict commit carries `Harmonik-Run-ID, Harmonik-Workflow-Class: reconciliation`, and the verdict payload, but not the `Harmonik-Verdict-Executed` trailer. But it only says this in prose; there's no table.

**Recommendation.** Add a verdict-commit format table to `schemas.md §6.4` parallel to the existing verdict-executed trailer block:

```
TRAILERS on verdict commit:
    Harmonik-Run-ID                : UUID of investigator_run_id
    Harmonik-Workflow-Class        : "reconciliation"
    Harmonik-Target-Run-ID         : UUID of target_run_id
    Harmonik-Verdict               : one of the 7 Verdict enum values
    Harmonik-Schema-Version        : Integer

COMMIT MESSAGE BODY:
    JSON-serialized VerdictEvent per [schemas.md §6.1]
    (provides evidence_ref, context, checkpoint_ref, snapshot_token)

COMMIT TREE:
    .harmonik/reconciliation/<investigator_run_id>/
        wip-capture/       (only on reopen-bead per RC-019)
        evidence.json      (playbook checks + outputs per RC-016)
        ...                (other evidence files per the playbook)
```

Without this, an implementer is guessing at how to compose the commit, and the `reconciliation_verdict_emitted` payload parser doesn't know where to find its data.

---

## 6. Action-mapping table audit (§8.12 and `schemas.md §6.3`)

I went row by row against what the executor needs to know mechanically. The columns `Default action`, `Investigator spawned?`, `Auto-resolver present?`, `Typical verdict` are adequate for dispatch. The column that is NOT adequate is "the mechanical action the auto-resolver must perform."

- **Cat 0** — "halt classification + degraded status" — unambiguous; implementable. No executor, just a daemon state transition.
- **Cat 1** — "auto-resume by re-spawning" — implementable assuming `idempotency_class` is a node attribute per EM-009. The executor is a call to the orchestrator's dispatcher with the last-checkpoint node_id.
- **Cat 2** — dispatches an investigator. Executor = dispatch the reconciliation workflow. Implementable; the detector has the run_id and the category, the YAML policy has the playbook ref (RC-016, S01-owned), the dispatcher builds the InvestigatorInput (§2.1 above, blocking).
- **Cat 3a** — "auto-resolve via adapter re-issue per [beads-integration.md §4.8]." Implementable through the BI adapter — but the RC spec does not cite a specific adapter method. I'd expect something like `adapter.ReconcileIntent(intent_entry)` which reads the audit log and either no-ops or re-issues. BI-031 (referenced in the handoff) is the idempotency contract. I verify: this is implementable assuming BI exposes the right primitive. Gap: name the primitive explicitly. Without it, the Cat 3a detector is declared but the corresponding executor function is not.
- **Cat 3b** — "auto-resolve via RC-026 re-execution." RC-026 says the auto-resolver "re-attempts the verdict's mechanical action under a fresh staleness check." Implementable — but the re-execution path is the *same* as the normal verdict executor, just without re-dispatching the investigator. One implementation detail: how does the daemon know WHICH verdict to re-execute? It reads the verdict commit on the investigator's task branch. Parsing the verdict from the commit is the inverse of the commit-composition in §5 — another reason to nail down that format.
- **Cat 3c** — "auto-verdict `accept-close-with-note` + mechanical close via adapter." The daemon synthesizes a verdict without an investigator. But RC-021 says "exactly one `reconciliation_verdict_emitted` event per reconciliation workflow" — is Cat 3c a reconciliation workflow? Re-reading RC-INV-001: "every reconciliation dispatch — including ... Cat 3c auto-verdicts — MUST run as a DOT-tagged workflow with `workflow_class = reconciliation` and MUST emit at most one `reconciliation_verdict_emitted` event per dispatch." So Cat 3c IS a reconciliation workflow; its "investigator" is the daemon itself (no cognition participates); the verdict event is synthesized by the daemon. This is a trivial reconciliation workflow. Implementable, but note the odd shape: a "reconciliation workflow" with no investigator node, no cognition, just a verdict-emit + verdict-execute node pair, counting against RC-002a's per-target lock.
- **Cat 4** — "auto-resume with pending action." Implementable if the pending action (retry timer, gate) was persisted. The spec implies "Agent was in a well-defined retry/backoff state at crash time" — but where is the retry state persisted? In git (as a checkpoint trailer)? In Beads? In a workspace-local marker? `event-model.md §6.3` has `agent_rate_limited` events but no durable state. This is a gap: Cat 4's auto-resumer can't be built without knowing where the pending action was persisted. HC-008a says "exit 0 = clean shutdown after outcome_emitted" — but a rate-limited handler doesn't exit cleanly; it continues. This is underspecified in the existing specs, not just RC.
- **Cat 5** — no-op. Implementable.
- **Cat 6a** — dispatches an investigator. Same as Cat 2.
- **Cat 6b** — auto-escalate. Implementable.

**Summary.** Cat 2, 3 generic, 3b, 3c, 6a, 6b are implementable from the table. Cat 3a needs the BI primitive name. Cat 4 has a cross-spec gap on retry-state persistence. Cat 3c is implementable but structurally odd and the Cat 3c invoice's "reconciliation workflow" might deserve being called a "deterministic reconciler" — calling it a "workflow" when it has no investigator conflates it with the investigator-bearing reconciliation workflows.

**Recommendation.** 

- Cite explicit BI primitives for Cat 3a executor.
- File an OQ for Cat 4's retry-state-persistence surface.
- Consider renaming Cat 3c's "auto-verdict reconciliation workflow" to something like "deterministic reconciler" to distinguish it from investigator-bearing reconciliation workflows. Or add to `schemas.md §6.5` that `WorkflowClass` has two variants: `reconciliation-investigator` and `reconciliation-deterministic`. (Currently only one value.)

---

## 7. Investigator failure modes

RC-018 (budget exhaustion) and RC-023 (malformed verdict) are both present. Let me walk the failure surface exhaustively.

1. **Investigator subprocess crashes before emitting anything.** HC-028/HC-022 detect; `agent_failed` event. RC has no requirement here. The reconciliation workflow's outer run reaches a terminal state via the classifier. Result: the target run remains unclassified/unresolved. On next daemon restart, the target run re-classifies via RC-003. OK — but we should state it.

2. **Investigator subprocess emits `outcome_emitted` with status=FAIL.** HC-008. The reconciliation workflow ends with no verdict. Same as (1). Should be treated identically.

3. **Investigator subprocess emits `outcome_emitted` with status=SUCCESS but Outcome.context_updates has no `harmonik.reconciliation.verdict` key** (using the W2 wiring of §5). This is a malformed verdict per RC-023 with reason `missing-required-field`. OK — the daemon synthesizes `escalate-to-human`.

4. **Investigator emits a VerdictEvent with an unknown `verdict` enum value** (e.g., the LLM invents `resume-if-safe`). RC-023 reason `unknown-verdict-value`. OK.

5. **Investigator emits a syntactically valid VerdictEvent that fails the per-verdict required-field check** (e.g., `verdict: reset-to-checkpoint` without `checkpoint_ref`). RC-023 reason `missing-required-field`. OK.

6. **Investigator emits the VerdictEvent twice** (e.g., two `outcome_emitted` messages, or two `reconciliation_verdict_emitted` events). RC-023 reason `multiple-verdicts`. OK per RC-021.

7. **Investigator emits a VerdictEvent with `verdict: reset-to-checkpoint` and a `checkpoint_ref` UUID that does NOT match any real checkpoint on the target run's task branch.** This is a semantic error but the schema is valid. RC-023's reasons don't cover this. The verdict-executor (RC-025 line 543) says for `reset-to-checkpoint`: "Revert the outer run to the checkpoint named by `checkpoint_ref`. ... Idempotent: if the current run state already matches the target `state_id`, no-op." But if the `checkpoint_ref` is unknown, reverting fails. What happens?

   **Gap.** RC-025 does not specify behavior for executor-detected semantic errors. Add a requirement: "If the verdict's mechanical action fails due to investigator-introduced semantic error (unknown checkpoint_ref, unknown bead_id, context_updates key-collision), the daemon MUST emit `reconciliation_verdict_execution_failed` (new event) and synthesize a fallback `escalate-to-human` verdict on the target run per RC-023 handling."

8. **Investigator emits VerdictEvent with `target_run_id` not matching the run the daemon dispatched the investigator for.** The daemon dispatched reconciliation for target_run=X; the investigator emits verdict with target_run_id=Y. This is either an LLM hallucination or an authoring error in the investigator's prompt. RC-023 reason `wrong-type` doesn't fit (the type is correct). Propose a new reason `target-run-mismatch`.

9. **Investigator takes too long.** RC-018 budget exhaustion. OK.

10. **Investigator gets rate-limited.** Handler-contract `agent_rate_limited` path. Does the budget clock pause during rate-limit waits? RC-017 says "wall-clock budget" — implies no, it runs regardless. But the Claude-Code handler might wait minutes during a rate-limit burst. Who decides whether the investigator's wall-clock budget is generous enough to cover rate-limit waits? The S01 YAML policy (per RC-017 defaults). Mention this explicitly.

11. **Investigator's Beads-CLI or git-inspection skill fails during investigation.** HC-022/HC-022a ErrStructural on skill provisioning failure → subprocess fails to start. If skill fails mid-investigation (e.g., `br` goes unresponsive), the investigator's output is incomplete. It may still emit a verdict — based on partial evidence. The playbook (RC-016) would hopefully prescribe a defensive posture, but the spec doesn't say. An investigator emitting a verdict based on incomplete evidence is a real risk.

    **Gap.** Add to RC-016: "Playbooks MUST define the per-skill-failure posture — for each skill an investigator depends on, what the investigator does when the skill errors. Default posture MUST be 'abort the playbook; emit `escalate-to-human`'."

**Summary.** RC-023 covers schema-level malformation. It does NOT cover semantic errors the executor detects (7), cross-reference errors (8), or mid-investigation skill failures (11). Add two requirements and extend the MalformationReason enum.

---

## 8. Cat 6 escalation surface

RC-020 enum has `escalate-to-human`; `schemas.md §6.2` verdict-execution table: "Emit `operator_escalation_required` (payload owned by [event-model.md §8]); mark the outer run in a quarantined state per [operator-nfr.md §4.3]. Deduplicated by `target_run_id` (subsequent emissions are no-ops)."

**What the operator actually sees.**

- The `operator_escalation_required` bus event fires. Per `event-model.md §6.3` line 766: `{target_run_id, reason, reference_commits}` where `reason` is one of `cat_6a_investigator_escalated | cat_6b_auto_escalated | cat_3_stale_write | budget_exhausted | merge_conflict | gate_escalated | other_verdict_driven`.
- ON-008 owns the "operator-observable surface" of escalation; `operator-nfr.md §4.8` has the delivery surface.
- Per §8.12 Cat 6a typical verdict is `escalate-to-human`. The investigator's rationale is in the verdict commit body (via `VerdictEvent.context: String | None` — but `context` per `schemas.md §6.1` is "required iff verdict = resume-with-context; MUST be empty otherwise." So `context` is NOT where the escalation rationale lives. It has to live in evidence files or in the commit message body.)

**Gap.** The investigator's rationale for escalation is not wired to the operator-facing surface. `operator_escalation_required.reference_commits` names commits, but what does the operator read to understand WHY the escalation happened? The operator has to walk the git branch, find the verdict commit, read its message body, and interpret it. This is ... functional, but it's not the operator surface `ON-008` implies. 

**Recommendation.** Add a field to the `VerdictEvent` schema: `rationale: String | None` — "human-readable summary the operator sees on `escalate-to-human`; required iff verdict = escalate-to-human; optional otherwise." Wire it into `operator_escalation_required.reason` as a free-form detail string, or add a new payload field `rationale_ref` pointing at the verdict commit's message body.

**How the daemon resumes after human intervention.** The spec doesn't say. If an operator reads the escalation, debugs the issue, manually fixes the underlying state (e.g., runs `br reopen` out of band, or edits the worktree), then signals the daemon — how? 

- Is there a `harmonik unescalate <run_id>` command? Not in the spec.
- Does the operator have to restart the daemon to trigger a fresh reconciliation sweep? This would be heavy-handed.
- Does the operator use `harmonik reconcile --run <run_id>` (per RC-020a's on-demand dispatch point, grammar deferred to OQ-RC-005)?

**Recommendation.** State the human-resume surface. Most natural is RC-020a's on-demand dispatch — but `harmonik reconcile --run <run_id>` is currently described as a detector trigger, not as a re-dispatch after operator fix. Either amend RC-020a to say "re-dispatches reconciliation regardless of quarantine state" or add a separate `harmonik resume <run_id>` command.

---

## 9. Concurrency among investigators

RC-002a bounds per-target: at most one reconciliation workflow per `target_run_id`, 5s registry-lock timeout, second dispatch either Cat 4 or skipped.

**What's still unclear.**

1. **Multiple investigators for different `target_run_id`s in parallel.** Implicitly allowed. How many? No cap. Budget per investigator is per RC-017 (600s / 300s / 900s defaults). If the daemon startup sweep finds 100 in-flight non-idempotent runs and classifies them all as Cat 2, does it dispatch 100 simultaneous investigator workflows? Presumably bounded by the handler-ceiling of PL's `FALLBACK_CAP = 1024` and `RLIMIT_NOFILE_soft / FDS_PER_HANDLER`. But a *reconciliation-specific* dispatch throttle might be wanted so reconciliation doesn't saturate the agent pool. No such requirement exists.

2. **Do parallel investigators share evidence?** E.g., two reconciliation workflows investigating two runs against the same bead (Cat 6a scenario: "bead `in_progress` with two or more task branches"). Both investigators will want to read the same bead_record, the same audit log, the same checkpoint commits. Shared evidence via CASS or the memory layer would make sense but is not named.

3. **Coordination on shared state.** If investigator A issues `reopen-bead` for run X, and investigator B is mid-investigation for run Y on the same bead, does B get stale-canceled? RC-024 staleness applies to the `target_run_id`'s own git branch and Beads audit entries — other runs' beads' changes don't trigger staleness. So B continues with stale Beads data. This might be a real defect, but diagnosing requires understanding the bead-vs-run mapping: one bead has multiple runs, each classified independently (RC-010). Investigator A's `reopen-bead` changes the bead state, which B sees (because B queries Beads via its skill), but the staleness check on B compares B's snapshot `beads_audit_entry_id` to the current one — and the current one HAS advanced. So B IS stale.

   **Verify.** RC-024: "the target run's bead has changed status in the Beads audit log since the snapshot." If A and B are investigating different runs of the SAME bead, A's `reopen-bead` mutates the bead state, and B's staleness check (which is on B's target run's bead = same bead) will see the audit log advanced. B is stale. 

4. **Re-entrancy during reconciliation workflow's own auto-resolver failure.** If the Cat 3b auto-resolver fails and routes to Cat 3 (PL-009a), a new investigator is dispatched for the same target_run_id. RC-002a's lock governs "from dispatch to verdict-executed commit" — but the FIRST reconciliation workflow has already emitted its verdict and landed its verdict commit; it just failed to land the verdict-executed commit. Is its lock released? The lock release condition per RC-002a is "atomically with the verdict-executed commit." No verdict-executed commit = lock never released. Meanwhile, Cat 3b auto-resolver (or the re-dispatched Cat 3 investigator) is trying to dispatch a NEW reconciliation workflow for the same target, which blocks on the lock.

   **Gap.** The lock release in RC-002a is on the verdict-EXECUTED commit, but the Cat 3b path says a verdict-unexecuted state IS recoverable. These are in tension. Proposed fix: RC-002a should release the lock on EITHER (a) the verdict-executed commit, OR (b) a new `reconciliation_workflow_terminal` event indicating the workflow has produced a verdict and released control regardless of whether execution succeeded. The Cat 3b auto-resolver doesn't dispatch a new reconciliation workflow; it re-executes the existing verdict — so it doesn't need the lock.

   Actually re-reading carefully: RC-026's Cat 3b is an auto-resolver, not a new investigator dispatch. So it doesn't need the lock. Good. But if the Cat 3b auto-resolver ITSELF fails (PL-009a), the path is to Cat 3 → new investigator → new reconciliation workflow dispatch. That NEEDS the lock. If the prior workflow's lock is still held (because its verdict-executed commit never landed), the new dispatch times out at 5s per RC-002a and fail-classifies as Cat 4 or is skipped. Neither is right — this is exactly the case where a new investigator IS needed.

   **Recommendation.** Rewrite RC-002a lock-release: "The lock MUST be released atomically with EITHER (a) the verdict-executed commit per RC-025, OR (b) a terminal state of the reconciliation workflow regardless of verdict execution (budget-exhausted per RC-018, malformed-verdict per RC-023, investigator-subprocess-crash)."

---

## 10. Pre-execution verdict confirmation (operator-override) UX

RC-027 names `harmonik confirm-verdict <run_id>` and `harmonik veto-verdict <run_id> [--promote-to escalate-to-human]`. Default: auto-execute; operators opt in by policy.

ON-014 owns the naming convention.

OQ-RC-005 defers the grammar, exit codes, authorization model to operator-CLI spec.

**What the operator sees before a verdict executes** (when opted in):

- A `reconciliation_verdict_emitted` bus event has fired. Some consumer (the operator UI, `harmonik status`, etc.) presents it.
- The verdict commit is on disk; the operator can `git log <investigator_branch>` or read the commit body.
- The daemon is holding the verdict-execution step paused.

**What's missing.**

1. **How does the daemon know it's in "opt-in" mode?** RC-027 says "per-reconciliation-workflow policy option" — presumably a field on the S01-shipped YAML policy (like `require_operator_confirmation: true`). Where is this field declared? `[control-points.md §4.7]` is the policy-ref surface, but RC-027 doesn't specify the field name or schema.

2. **How long does the daemon wait?** RC-017 budget applies to the reconciliation workflow but says nothing about the operator-confirmation wait. Does the wall-clock budget include the confirmation wait? If yes, an operator who takes 20 minutes to confirm a Cat 6a verdict will blow through the 900s Cat 6a default and trigger RC-018 budget-exhausted → `escalate-to-human`. That's a bad UX. If no, the budget is paused — but the spec doesn't say.

3. **What happens if the operator vetoes?** The verdict is replaced with `--promote-to escalate-to-human` (or a different explicit verdict if the operator names one). But `veto-verdict` signature only offers `--promote-to escalate-to-human` per ON-014. The operator can't rewrite a `reopen-bead` to a `reset-to-checkpoint` (say). Is that intentional? Probably — the operator's role is accept-or-block, not rewrite. But an operator who wants a different action has to `veto-verdict --promote-to escalate-to-human` and then manually fix the state. That's a pretty weak surface.

4. **Where does the operator's confirmation/veto get recorded?** The verdict commit has an operator-action trailer? A new event? A new field on VerdictEvent? Not specified.

**Recommendations.**

- RC-027a: Name the policy-field surface. "The per-reconciliation-workflow policy MUST declare an optional boolean field `require_operator_confirmation: Bool` (default false) under the YAML policy per [control-points.md §4.7]."
- RC-027b: "The operator-confirmation wait MUST NOT be counted against the wall-clock budget of RC-017. The budget is paused at verdict emission and resumed at confirm/veto."
- RC-027c: "On operator confirm, the daemon MUST append an operator-action trailer to the verdict-executed commit: `Harmonik-Operator-Action: confirmed` (or `vetoed` on veto, with `Harmonik-Operator-Promote-To: <verdict>` as a second trailer). Trailer shape declared in [schemas.md §6.4]."
- RC-027d: Name the per-category allowlist for confirmation. E.g., "Default policy is: require confirmation for Cat 6a always; for Cat 3 at operator's discretion; never for Cat 2 (automation-heavy, high-volume)."

---

## 11. Affirmations — investigator-friendly decisions that should NOT be reopened

Keep these as-is. They are correct and useful.

1. **Verdict-only commit rule (RC-002).** Bounded recursion follows from this. Don't soften it.
2. **Seven-value Verdict enum (RC-020).** `no-op-accept` was the right addition in R1 integration. Adding an eighth verdict is tempting for the gaps flagged in §2.3 but can probably be handled with per-category allow-lists and enum-local invariants rather than new values.
3. **Snapshot-token staleness discipline (RC-024).** Scoping staleness to the target run's own git+Beads (not sibling beads, not JSONL) is exactly right. Without this, any parallel activity thrashes reconciliation.
4. **Verdict-executed commit as a presence-only trailer (`schemas.md §6.4`).** Clean and recovery-friendly.
5. **Malformed-verdict deterministic fallback (RC-023).** Correctly structured — the fallback IS `escalate-to-human`, the daemon never tries to interpret the malformed payload.
6. **RC-005 split: detectors in Go, playbooks in S01 library.** The right split. Don't blur it.
7. **Cat 3a/3b/3c auto-resolvers as distinct from Cat 3 generic investigator dispatch.** Saves tokens, keeps hot-path deterministic. The R1 priority-ordering via RC-003a resolved the "which fires first" ambiguity.
8. **RC-002a at-most-one-per-target lock.** Correct. See §9 only for the lock-release condition refinement.
9. **Reconciliation-workflow self-reference (RC-INV-001) as cross-lifecycle invariant.** The right level to stake the claim.

---

## 12. Cross-spec coordination from the investigator-author seat

Filing OQs or spec-text amendments I'd need from sibling specs to build this:

- **handler-contract.md** — LaunchSpec extension OR new `investigator_input_ref` optional field OR explicit "investigator inputs are snapshot-token-bounded skill reads, not LaunchSpec fields" clarification. Coordinates with my §2.1 gap.
- **handler-contract.md** — Declare that `outcome_emitted.Outcome.context_updates` is the verdict-delivery surface, OR add a new progress-stream message type `reconciliation_verdict` to HC §6.4. Coordinates with my §5 gap.
- **event-model.md** — Resolve EV-023a / RC-019a corroboration-shape contradiction (see §2.2). Either EV adopts `evidence.sources[]` array on `store_divergence_detected`, or RC adopts EV's scalar enum.
- **event-model.md** — Add new event types: `reconciliation_workflow_started` (for per §2.4 bootstrap-classification fix); `reconciliation_verdict_execution_failed` (for §7 gap 7); `reconciliation_workflow_terminal` (for §9 lock-release fix).
- **execution-model.md** — Accept the `workflow_class` Workflow-record extension into EM's canonical schema (current OQ-RC-002).
- **workspace-model.md** — WM-036 currently maps four verdicts to worktree dispositions. It needs to map all seven, including `accept-close-with-note`, `no-op-accept`, `escalate-to-human`. WM-036 says "Any verdict value not in the table above is a malformed verdict" — but `accept-close-with-note` IS a valid verdict, just not mapped to a worktree action. Fix WM-036 OR add the three missing verdicts with disposition `no-op` / `preserve-worktree`. The RC-020 seven-value update outpaced WM-036.
- **workspace-model.md** — Confirm the reconciliation-workflow's branch naming discipline (`run/<inv_run_id>` vs a distinct prefix). Coordinates with my §2.4 fix.
- **operator-nfr.md** — RC-027a/b/c/d proposed above need cross-check with ON-014 (which owns the naming convention) and ON-041 (operator-CLI surface).
- **beads-integration.md** — Name the explicit BI primitive for Cat 3a adapter re-issue. Probably BI-031 or a nearby requirement, but the RC detector needs a callable function name.

---

## 13. Recommendation (ordered by severity)

**BLOCKING — must resolve before finalize.**

1. **[§2.1] LaunchSpec ↔ InvestigatorInput wire.** Pick one of (a)/(b)/(c) from §2.1. Without this, the dispatcher cannot be coded and HC doesn't know whether to accept extension fields. Highest priority.
2. **[§2.2] EV-023a / RC-019a corroboration-shape contradiction.** The RC-INV-004 sensor cannot compile against `event-model.md §6.3`'s payload. Resolve at the corpus level.
3. **[§5] Verdict emission wire protocol.** Pick W1/W2/W3. Add RC-022a and RC-025a spelling out how the investigator's subprocess output becomes the daemon's verdict commit. Without this, the investigator author doesn't know what to emit and the executor author doesn't know what to parse.

**IMPORTANT — should resolve before finalize but workaroundable.**

4. **[§2.3] Verdict-enum exhaustiveness per category.** Add the per-category allowed-verdict table. Forbid `no-op-accept` for Cat 2 (or document it as re-classification). Add a `rationale: String | None` field to `VerdictEvent` for `escalate-to-human`.
5. **[§2.4] Reconciliation-workflow bootstrap classification + Cat 3b retry cap.** Add RC-003b (crashed investigator branches classify as Cat 5, not Cat 6a) and RC-026a (Cat 3b retry cap).
6. **[§3] Investigator runtime contract — explicit HC conformance.** Add RC-015a naming investigator as an HC handler with `agent_type = claude-code` + `role = investigator`.
7. **[§5] Verdict-commit format.** Add a full trailer + body table to `schemas.md §6.4`.
8. **[§6] Cat 3a BI primitive + Cat 4 retry-state-persistence OQ.**
9. **[§7] Failure-mode coverage.** Extend RC-023 reasons (`target-run-mismatch`, `verdict-not-allowed-for-category`), add new event `reconciliation_verdict_execution_failed`, add posture requirement to RC-016.
10. **[§9] RC-002a lock release on ANY reconciliation-workflow terminal state, not just verdict-executed commit.**
11. **[§10] RC-027a/b/c/d operator-confirmation UX.**

**NICE-TO-HAVE — defer or leave as OQ.**

12. **[§4] Snapshot-token serialization format + `captured_at_timestamp` role clarification.**
13. **[§5] Rename "reconciliation workflow" for Cat 3c's cognition-free dispatch OR add a WorkflowClass sub-variant.**
14. **[§8] `harmonik resume <run_id>` or equivalent human-resume surface.**
15. **[§12] Coordinate the workspace-model WM-036 seven-verdict mapping fix.**

---

**Summary judgment.** The spec's shell is solid — taxonomy, auto-resolver carve-outs, verdict execution table, snapshot-bound staleness. The investigator-author surface has three blockers (LaunchSpec wire, corroboration shape, verdict emission protocol) that prevent anyone from writing the dispatcher, the investigator handler, or the executor. The R1 integration closed the obvious gaps; R2 is where the implementation-facing contracts get filled in. These are tractable. Do not finalize without the three blockers resolved.
