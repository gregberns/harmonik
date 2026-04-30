# Round 1 Critic Review — reconciliation/spec.md + schemas.md v0.2.0

Target: `specs/reconciliation/spec.md` and `specs/reconciliation/schemas.md`, both at `v0.2.0`, status `draft`, spec-shape `taxonomy-first`.

## Verdict summary

**Proceed with revisions.** The taxonomy is the right shape — eleven categories are well-motivated, the action-map discipline (RC-007/RC-008) is declared, the investigator contract is snapshot-bound with a staleness check, and malformed-verdict handling is concrete. The walkthrough-level architecture (reconciliation-as-workflow with a single verdict commit, verdict-executed trailer, bounded recursion via RC-002/RC-INV-002) holds up under pressure.

The load-bearing softness sits in five places, every one a concrete omission rather than an architectural re-think:

1. **Category orthogonality is asserted (RC-009 calls the shape "settled") but never mechanically defended.** A walk of the 11 values (see the table below "Eleven-value walk") turns up at least seven overlap pairs where a divergence can plausibly land in two categories with different default actions — the spec has no tie-break rule.
2. **Cat 0 pre-check is declared to gate classification (RC-012) but detector preconditions are incomplete** — RC-012 lists four prerequisites; at least three detectors (Cat 3a, Cat 6a, Cat 6b) read resources not covered by those prerequisites. The sub-category routing order is prose in §7.1 pseudocode rather than a normative requirement.
3. **The three Cat-3 sub-categories (3a, 3b, 3c) are distinct auto-resolvers, but no requirement routes them ahead of the generic Cat-3 investigator** — the action-map treats them as peers of Cat 3, yet the detector MUST try 3a/3b/3c first or every torn-write falls into a Cat-3 investigator workflow.
4. **Verdict enum is declared "exactly six" (RC-020) but missing a "no-op / accept-as-is" verdict for Cat 3c** — the §8.6 detection rule says the daemon "auto-verdicts `accept-close-with-note`," which synthesizes a verdict without an investigator. The verdict enum has no dedicated value for "detector-synthesized decision"; `accept-close-with-note` is being overloaded.
5. **Reconciliation-as-workflow bootstraps via a chicken-and-egg hidden assumption, and evidence-corroboration gating per [event-model.md EV-023a] is not integrated.** RC-INV-002 plus RC-002 cover the recursion bound, but not the bootstrap order; and RC-014 never mentions the `corroboration` field or `divergence_inconclusive` emission that event-model v0.3 introduced to prevent false positives from lossy-tail JSONL.

Five reviewer-enforced invariant failures: three §5 invariants are emphatic restatements of §4 requirements (fail the template selection test), and only two (RC-INV-001, RC-INV-004) are genuinely cross-subsystem; also, none of the five §5 invariants name their sensor per AR-042 (foundation-cross-cutting obligation).

Two first-plausible-answer findings: the 600/300/900 budget defaults of RC-017, and the verdict-execution idempotency claim for `resume-here` / `resume-with-context` (RC-025).

## Eleven-value walk — does a given divergence land in exactly one category?

This walk responds to the task brief directly: treat the 11 categories as a disjoint partition of a run's post-crash state-space and ask, for each category, "what state machine predicate uniquely selects me?" The answer for most categories is "I'm uniquely selected only if every higher-priority category is checked first and falsified" — which means the partition is *not* disjoint over raw state, only disjoint under an evaluation-order assumption the spec does not declare.

| Cat | Unique-selector candidate | Conflicts with |
|---|---|---|
| 0 (infra unavailable) | `br --version` fails OR `git rev-parse` fails OR `.harmonik/` unwritable | Cat 6b (`git fsck` fails implies some `rev-parse` paths fail) |
| 1 (idempotent rerun) | node has `idempotency_class = idempotent` AND run in-flight | Cat 4 (idempotent + retry-pending — Cat 1 or Cat 4?), Cat 2 (`recoverable-non-idempotent` sub-case) |
| 2 (non-idempotent in-flight) | node has `idempotency_class ∈ {non-idempotent, recoverable-non-idempotent}` AND bead `in_progress` AND no run-terminal event | Cat 4 (non-idempotent agent in retry-wait), Cat 3c (merge landed but bead still `in_progress` — Cat 2 detects "no run-terminal event" which is *false* if the merge is the terminal; spec is ambiguous whether the merge commit counts as the run-terminal event) |
| 3 (generic) | store disagreement AND none of {3a, 3b, 3c} matches | all of {3a, 3b, 3c}; Cat 6a overlaps on structurally-wrong-data |
| 3a (torn Beads write) | intent-log entry present | Cat 3 generic, Cat 6b (if intent-log file corrupt) |
| 3b (verdict-unexecuted) | verdict-commit present + verdict-executed-commit absent | Cat 3 generic (could read as "store disagreement") |
| 3c (inverse premature-close) | merge commit with success terminal + bead `in_progress` + no sub-checkpoints | Cat 2 (bead `in_progress` on a non-idempotent node — is the merge commit the terminal or not?); Cat 6a (two branches advertising `Harmonik-Run-ID`) |
| 4 (recoverable known state) | agent in retry/gate-wait at crash | Cat 1 and Cat 2 per above |
| 5 (clean restart) | nothing in-flight for this run | Cat 3 generic (orphaned branch might look like "store disagreement") — §8.8 addresses this explicitly, but only for re-claimed beads |
| 6a (integrity, LLM-triageable) | structurally wrong data with operator-reasoning path | Cat 3 generic, Cat 3c per above |
| 6b (integrity, mechanically unrecoverable) | `git fsck` fails OR JSONL corrupt OR checkpoint missing from git | Cat 0 per above |

**Result.** Nine of eleven categories have a visible conflict with at least one other category at the level of raw state. Only Cat 0 (narrow: prerequisite failure only) and Cat 5 (narrow: nothing in-flight) are plausibly uniquely-selected. This is the mechanical basis for Challenge 1: without a declared evaluation order, the partition is not a partition.

**Worked example of why this matters.** Consider a run R whose builder agent (non-idempotent) crashed while holding an HTTP 429 backoff timer, with `br close` having been issued moments before the crash (the adapter wrote the intent-log entry, the `br` call succeeded, but the daemon's in-memory state didn't get to record the success because of the crash). At restart:
  - Cat 0 pre-check passes.
  - Cat 3a fires (intent-log entry present; audit entry matches).
  - Cat 2 fires (non-idempotent node, bead `in_progress` at the snapshot the daemon built its view from — though the bead is actually `closed` per Beads now).
  - Cat 4 fires (retry-backoff state at crash).
  - Cat 5 might even fire depending on how "in-flight for this run" is evaluated.
  - Four detectors all match. The correct answer is Cat 3a (clean up the torn write), but the detector ordering is the only thing that disambiguates — and the spec does not declare it.

The spec's implicit solution appears to be "the daemon's detector code happens to check Cat 3a first because it's earliest in the `if/else` chain." That is an implementation accident, not a contract. A reader of the spec cannot know what's supposed to happen.

## Challenges (8 load-bearing items)

### Challenge 1 — Category orthogonality is asserted but not mechanically defended

- **Challenge.** RC-009 (§4.2, line 308) locks in the 6-detection-category taxonomy as "settled" and the §1 Purpose (line 25) claims the taxonomy classifies every in-flight run into a "bounded set" of categories. For the bound to be useful, the categories must be disjoint — one run's divergence lands in exactly one category. A walk of the 11 values produces at least three overlap pairs with different default actions.

- **What the spec says.** §8.1 through §8.11a define detection rules in prose. §A.3 of the template's exemplar (and the task brief here) implies the spec should contain rationale for orthogonality; the current §8 has no such subsection. §8.12 (lines 230-242) is a flat dispatch table, not a priority-ordered routing rule.

- **Is the justification adequate?** No. Concrete overlaps:

  - **Cat 0 vs Cat 6b.** A corrupted git object database (Cat 6b per §8.11a line 216: "`git fsck` fails") causes `git rev-parse HEAD` to fail (Cat 0 per RC-012 line 333: "`git rev-parse HEAD` succeeds" as prerequisite). The same underlying condition can satisfy both detectors. Default actions differ (Cat 0 `degraded` + wait-and-retry vs Cat 6b `auto-escalate operator`). Which wins? The Cat 0 pre-check per RC-012 fires first — but then Cat 6b never fires at all, and the operator is kept in a degraded-retry loop for a condition that will never self-heal.
  - **Cat 3a vs Cat 3 generic.** Cat 3a (§8.4a, line 133) fires when the intent-log records an outstanding write AND the Beads audit log "either shows no corresponding entry OR shows an entry matching the idempotency key." This is *permissive* — the same conditions could be a Cat 3 generic "bead `in_progress` but worktree missing" (§8.4, line 121). The spec never states "Cat 3a is checked before Cat 3 generic."
  - **Cat 3c vs Cat 6a.** A merge commit tagged `Harmonik-Run-ID R` with a reached success terminal state (Cat 3c, §8.6 line 157) where the bead for R has *two* task branches without `Harmonik-Verdict-Executed` (Cat 6a last bullet, line 200) is a single scenario that matches both detectors. Default actions diverge sharply: Cat 3c auto-closes mechanically; Cat 6a escalates to a human via an investigator.
  - **Cat 2 vs Cat 4.** A non-idempotent builder agent mid-implementation that happened to be in an HTTP-429 retry wait at crash time satisfies both: `idempotency_class = non-idempotent` AND "well-defined retry/backoff state at crash." Cat 4 auto-resumes; Cat 2 dispatches an investigator. The spec does not say which detector fires first, and the `idempotency_class` flag does not itself reveal whether the retry-state metadata was captured.

- **Stronger alternative.** Two defensible shapes:
  - (a) Declare a priority order for detector evaluation as a normative requirement (RC-013 adjacent): "Detectors MUST fire in priority order: Cat 0 → Cat 6b → Cat 3a → Cat 3b → Cat 3c → Cat 4 → Cat 1 → Cat 2 → Cat 3 generic → Cat 6a → Cat 5. The first matching category wins; subsequent categories are not evaluated for that run." Publish the rationale for the ordering in §A.3.
  - (b) Rewrite each detection rule as a mutually-exclusive predicate (e.g., Cat 3 generic = "store disagreement AND NOT any-of-{3a, 3b, 3c}"). This makes the classifier a pure function of state. Current §8.4 already gestures at this ("no Cat 3 sub-category matches") but does not do it for every category.
  - Either form makes the orthogonality claim of RC-009 mechanically verifiable. The current form does not.

- **Load-bearing level.** Blocking. Every consumer of `reconciliation_category_assigned` routes on the category; if two detectors can fire on the same state, the event payload is ambiguous and the action-map hands out the wrong verdict.

### Challenge 2 — Cat 0 is declared to gate classification, but RC-012 does not say detectors cannot fire until it passes

- **Challenge.** The §8.1 prose (lines 79-82) and §7.1 pseudocode (lines 606-611) describe Cat 0 as a pre-check that halts classification. RC-012 (§4.3, line 327) declares this as a normative requirement: "Before any §8 category detector executes, the daemon MUST verify infrastructure prerequisites." Good — except the prerequisites listed in RC-012 are narrower than the detector surface. Some detectors read resources that would *also* need Cat 0-style pre-verification and don't appear in RC-012.

- **What the spec says.** RC-012 lists four prerequisites (line 329-334): `br --version`, `br list --limit 1`, `git rev-parse HEAD`, `.harmonik/` writable. The Cat 3a detector (§8.4a) reads `.harmonik/beads-intents/`; the Cat 6a detector (§8.11 line 199) reads `.git/rebase-merge`, `.git/MERGE_HEAD`, etc.; the Cat 6b detector (§8.11a line 216) runs `git fsck` — none of these appear in RC-012's prerequisites.

- **Is the justification adequate?** No. Two problems:
  - (a) A missing `.harmonik/beads-intents/` directory (which can legitimately be empty, or legitimately not exist on first run, or pathologically be deleted by an operator) is the *normal* state during Cat 5 (clean restart) and an *error* state during Cat 3a investigation. RC-012 does not verify the directory exists before Cat 3a fires — the detector is implicitly robust-against-absence, but that's not declared.
  - (b) `git fsck` is expensive (O(repo size) time and I/O) and yet Cat 6b fires during the Cat 0 pre-check window per RC-012 ordering. The spec never names when `git fsck` runs — at every startup? Only on suspicion? On a schedule? The prose in §8.11a is silent on detection cadence.
  - (c) More fundamentally, RC-012 declares prerequisites in terms of capability probes (`br --version`), not in terms of "can this detector run." The correct shape is: each detector MUST declare its own preconditions, and the daemon MUST NOT invoke a detector whose preconditions are unmet.

- **Stronger alternative.** Replace RC-012's flat prerequisite list with a structured rule:
  - (a) Every detector (normative entry per category in §8) MUST declare a `preconditions[]` set: the resources it reads and the mechanical probes that establish readability. Cat 0 pre-check is the union of all detectors' preconditions.
  - (b) Add RC-012a: "A detector MUST NOT fire if any of its declared preconditions is unmet. A precondition failure for a specific detector MUST NOT halt all classification (distinct from Cat 0, which halts all of it); instead the daemon MUST reclassify the affected run as Cat 6b with sub-reason `precondition-failed-<detector>`."
  - (c) Declare Cat 6b's `git fsck` as a non-default probe: it runs on suspicion (prior checkpoint hash not in git object database per the bullet already in §8.11a line 215) or on operator demand (future `harmonik reconcile --deep`), never unconditionally at every startup.

- **Load-bearing level.** Important. The MVH restart RTO claim of [operator-nfr.md §7.8] depends on reconciliation being fast at startup; running `git fsck` unconditionally would blow that budget on any repo larger than toy size. The spec's silence on when Cat 6b probes run is a real gap for an operator trying to size a machine.

### Challenge 3 — Cat 3a/3b/3c proliferation: three sub-categories, but the routing contract treats them as peers of Cat 3 generic

- **Challenge.** Cat 3 is nominally "store disagreement (generic)" and 3a/3b/3c are specializations with auto-resolvers instead of investigator dispatch. The action-map in §8.12 (lines 230-242) lists them as flat peers. Nothing in the taxonomy declares that 3a/3b/3c detectors MUST fire before Cat 3 generic. Without that ordering, the detector that matches first wins — and Cat 3 generic's detection rule (§8.4 line 121) is phrased "no Cat 3 sub-category (3a/3b/3c) matches," which assumes an ordering the spec never declares.

- **What the spec says.** §8.4 (line 121): "git, Beads, and JSONL tell inconsistent stories about the same run, and no Cat 3 sub-category (3a/3b/3c) matches." §8.4a/8.5/8.6 state their detection rules but not their relative order. §7.1 pseudocode uses `detect_category(run)` as a single function — opaque. RC-009 locks in the 6-category count, but nothing normative describes the 3a/3b/3c precedence.

- **Is the justification adequate?** No. The `no sub-category matches` gate in Cat 3 generic is a forward reference that depends on evaluation order, but the spec never states that order as a rule. A naïve implementer could easily produce a detector that fires Cat 3 generic first (it's the simpler detection rule) and never reach 3a/3b/3c — the resulting system spawns investigators for cases that should auto-resolve, burning LLM tokens on a torn write that the adapter should silently re-issue.

  Related: schemas.md §6.3 (line 157) explicitly calls Cat 3 a "git-wins orientation per RC-INV-001" case. RC-INV-001's "git wins on completion" is an *invariant*, not a routing rule; the detector-order confusion is not cured by citing it.

- **Stronger alternative.**
  - (a) Collapse Cat 3a/3b/3c into Cat 3 with a `divergence_kind` sub-field on `store_divergence_detected` (schemas.md §6.1-adjacent). The action-map then becomes a two-level dispatch: Cat 3 with `divergence_kind ∈ {torn-beads-write, verdict-unexecuted, inverse-premature-close}` auto-resolves; Cat 3 with `divergence_kind = generic` dispatches an investigator. Five categories, two-level dispatch, no ordering confusion. This matches how [event-model.md §8.6.8] already carries `divergence_kind` on the `store_divergence_detected` payload.
  - (b) Keep the current count but add RC-013a: "The detector MUST evaluate Cat 3 subcategories in the order [3a, 3b, 3c, generic]. The first matching subcategory wins. Cat 3 generic MUST NOT be emitted if any of 3a, 3b, or 3c matches." Pair with a §A counter-example for each of the three (not yet present in the draft).
  - Either form is defensible; the current "four peer categories with a dependency rule buried in prose" is the worst of both.

- **Load-bearing level.** Important. The entire point of sub-categories is to auto-resolve torn writes without an investigator; if ordering is ambiguous, the auto-resolve path is unreliable and reconciliation-token-cost blows up.

### Challenge 4 — Action-mapping §8.12 is duplicated in schemas.md §6.3 with a lint-enforce-sync note, but divergences are already present in the draft

- **Challenge.** §8.12 (lines 230-242) and schemas.md §6.3 (lines 152-164) are declared-identical dispatch tables with a `MUST stay in sync; divergence is a lint failure` guard (§8.12 line 244). Walking the two tables row-by-row turns up shape divergence: schemas.md §6.3 has a "Detection-rule summary" column that §8.12 lacks, and §8.12 has a "Default action" column that is phrased differently in schemas.md (where the "Default action" column is present but collapsed). This is exactly what the template's multi-file-split rule (§Multi-file split, rule 4) warns against: declaring two identical tables in two files invites drift, and the drift already exists.

- **What the spec says.** The two tables differ column-set (6.3 has 6 columns including detection-rule and typical-verdict; 8.12 has 5 columns but collapses detection-rule out). The intent per §6 (line 577) is "the canonical tabular schema" is schemas.md §6.3; §8.12 is "duplicated for taxonomy-first reading order." But divergence in columns is not lintable — the declared lint is "divergence is a lint failure," which a grammar check can't enforce across two tables with different column schemas.

- **Is the justification adequate?** No. Either:
  - (a) The lint check must be implementable (e.g., both tables have identical column-sets and identical row-ordering, and a differ-by-byte check runs on pre-commit), or
  - (b) The duplication is explicit and each table owns a distinct slice (e.g., §8.12 owns the action-map column; §6.3 owns the detection-rule column; no overlap).
  - The current form claims (a) but implements neither.

- **Stronger alternative.**
  - (a) Reduce §8.12 to a one-line pointer: "See [schemas.md §6.3] for the canonical category × detector × action table." Taxonomy-first reading order survives because §8.1-§8.11a still carry detection rules inline; §8.12 becomes a reference.
  - (b) OR: make §8.12 and §6.3 byte-identical (same columns, same row order). Add a lint rule in §10.2 testing obligations: "A corpus lint MUST fail if §8.12 and [schemas.md §6.3] differ by any byte."
  - (a) is cleaner; (b) is OK if the author wants both tables visible.

- **Load-bearing level.** Important. Not blocking MVH, but the exact failure mode the template's multi-file-split rule was written to prevent; landing the spec as-is plants the drift seed.

### Challenge 5 — Investigator-agent contract (RC-015) vs HC handler interface: alignment is by prose, not schema-level

- **Challenge.** RC-015 (§4.4, line 371) declares the investigator's inputs as a list of seven fields bound to a snapshot token. [handler-contract.md §4.2 HC-006] declares the LaunchSpec record as the handler's input, with an optional `snapshot_token` field "present for reconciliation-investigator handlers." These are two shapes describing what an investigator sees. Do they align?

- **What the spec says.** RC-015 lists: snapshot token; run metadata; bead ID + Beads record; git state at last checkpoint; JSONL tail; workspace state; agent session log. schemas.md §6.1 (lines 27-42) formalizes these as the `InvestigatorInput` RECORD. [handler-contract.md HC-006] lists LaunchSpec fields: `run_id, workflow_id, node_id, agent_type, workspace_path, required_skills[], skill_search_paths[], timeout, provisioning_timeout, budget, freedom_profile_ref, bead_id?, snapshot_token?`. The overlap is partial: both have snapshot token, both have run/bead IDs. The investigator-specific fields (`bead_record`, `last_checkpoint`, `last_transition`, `jsonl_tail`, `workspace_state`, `session_log_ref`, `category`, `playbook_ref`, `budget_wall_clock_seconds`) are *not* in LaunchSpec.

- **Is the justification adequate?** No. The spec never reconciles these two records. Either:
  - (a) InvestigatorInput is a post-Launch payload the daemon hands to the agent *after* the subprocess starts (via the progress-stream per HC-007?) — but the spec doesn't say this.
  - (b) InvestigatorInput extends LaunchSpec with investigator-specific fields — but LaunchSpec is fixed per HC-006 and `bead_record`, `last_checkpoint`, `last_transition` etc. are not declared extensions.
  - (c) The investigator reads these fields itself via its Beads-CLI and git-inspection skills, bounded by the snapshot token — which would mean RC-015 is describing *what the investigator logically has*, not *what the daemon delivers*. This is a different contract from the LaunchSpec.
  
  The spec leaves this ambiguous. The > INFORMATIVE block at line 388 says "the investigator delegates to a Claude Code agent (role: `investigator`)" without resolving whether the fields are LaunchSpec-delivered or investigator-read.

- **Stronger alternative.** Pick one:
  - (a) Declare (in RC-015) that InvestigatorInput is assembled by the investigator itself from its skill set (Beads-CLI, git-inspection), scoped to the snapshot token. The daemon's delivery surface is LaunchSpec per HC-006; the investigator uses the snapshot token to bound what it reads. Drop the pseudo-record shape and describe it as a *view* the investigator must construct.
  - (b) Declare (in RC-015) that the daemon pre-assembles InvestigatorInput and delivers it via LaunchSpec or via a file-path reference in LaunchSpec (analogous to HC-005's 1-MiB cutover). Add a new LaunchSpec optional field `investigator_input_ref` pointing at a JSON file. Require HC to accept it.
  - (a) matches the locked decision that investigators use Beads-CLI and git-inspection skills (RC-004 line 278) and avoids bloating LaunchSpec with 7 conditional fields. (b) is more daemon-centric but works if the daemon already walked git and can hand the trail over. Either way, RC-015 needs to pick.

- **Load-bearing level.** Important. The ambiguity forces every S01 workflow author to guess what the investigator's inputs actually are, and forces handler-contract to carry or not carry fields whose ownership is undeclared. Subsystem specs consuming this (S04 Agent Runner) cannot be written without the resolution.

### Challenge 6 — Verdict enum completeness: no dedicated "no-op / detector-synthesized" verdict, so `accept-close-with-note` is overloaded

- **Challenge.** The verdict enum is `{resume-here, resume-with-context, reset-to-checkpoint, reopen-bead, accept-close-with-note, escalate-to-human}` (schemas.md §6.1 lines 74-82, RC-020 line 430). Two distinct semantic roles are conflated onto `accept-close-with-note`:
  1. An investigator reasoned about a Cat 3 generic divergence and concluded "git wins; bead was legitimately closed; annotate the audit gap" — a *cognition-produced* verdict.
  2. The Cat 3c detector mechanically concluded "merge commit exists + bead still `in_progress` + no in-flight checkpoints ⇒ close the bead" — a *detector-synthesized* verdict without an investigator (§8.6 line 159).
  
  These are different provenance, different trust levels, different auditability requirements. The spec uses the same enum value for both.

- **What the spec says.** §8.6 (line 159): "Auto-verdict `accept-close-with-note` with mechanical close-write (routed through the idempotency-keyed adapter per [beads-integration.md §10.8a]). No investigator is spawned." RC-020 (line 430) says "a verdict event's `verdict` field MUST be exactly one of the six enum values" — but Cat 3c's "auto-verdict" is not an investigator's verdict; it's a detector synthesizing an enum value that was designed for cognitive output. RC-021 (line 436) compounds this: "Exactly one `reconciliation_verdict_emitted` event per reconciliation workflow" — but Cat 3c doesn't spawn a reconciliation workflow (§8.12 line 238 says "Investigator spawned? No"), so no workflow emits the event.

  Who emits `reconciliation_verdict_emitted` for Cat 3c? The auto-resolver in daemon Go code? The spec does not say. RC-025 (line 478) describes the daemon executing a verdict *after* a workflow emits it, which is the investigator path; the detector-synthesized path is absent.

- **Is the justification adequate?** No. The Cat 3c "auto-verdict" path is structurally different from the investigator path in three ways the spec never handles:
  - No reconciliation workflow runs (so RC-021 "exactly one verdict per workflow" doesn't apply).
  - No snapshot token was captured at investigator-dispatch time (so RC-024 staleness check has no basis).
  - The verdict's provenance is a deterministic detector, not an LLM — which is *better* (no ZFC concern), but loses the per-RC-024 protection against action-on-stale-state.

- **Stronger alternative.**
  - (a) Add a seventh verdict value, `detector-synthesized-close`, with its own row in the verdict-execution table (schemas.md §6.2). Require the Cat 3c detector to capture a snapshot token at detection time and route through the same staleness check (RC-024) before writing the close. The reconciliation invariant "exactly one verdict per workflow" becomes "exactly one verdict per reconciliation event (workflow OR detector)," with the detector-synthesized path being a one-node-workflow semantically. This is the cleanest match for the existing machinery.
  - (b) Keep the 6-value enum but add RC-020a: "A detector-synthesized verdict MUST be indistinguishable from an investigator-emitted verdict in the event payload, but MUST carry `synthesizer = 'detector'` in a new optional `provenance` field; investigator-emitted verdicts carry `synthesizer = 'investigator:<role>'`." The staleness check (RC-024) still runs.
  - (a) is more honest. The enum is a taxonomy of *actions*, but the spec treats it as a taxonomy of *decisions*; those are different taxonomies with different sub-type needs.
  
  Separately: none of the six values covers "there was never a divergence; the detector was wrong." If Cat 3c fires but the investigator later finds the detection was a false positive (different `Harmonik-Run-ID` on the merge commit), there's no "no-op / detector-was-wrong" verdict — the investigator would have to emit `escalate-to-human` for what is operationally a non-event.

- **Load-bearing level.** Important. Cat 3c is the motivating example, but the same pattern recurs in Cat 3a (torn Beads write resolves mechanically — what verdict does that synthesize?) and Cat 3b (verdict-unexecuted gets re-executed — what verdict is emitted for the re-execution event?). The §8.12 auto-resolver column is silent on what fills the verdict field in each case.

### Challenge 7 — Reconciliation-as-workflow bootstrapping: chicken-and-egg not fully resolved

- **Challenge.** The spec's central architectural move is that reconciliation runs as a harmonik workflow (RC-001 line 256). Other workflows cannot dispatch until reconciliation is complete (per [process-lifecycle.md §8.2 PL-009 step 5] ready-state criteria). But a reconciliation workflow is itself a workflow — and if a reconciliation workflow was interrupted by the prior crash, RC-003 (line 270) requires re-classifying the outer run it was reconciling. How does the bootstrap detector know that the bootstrap-phase reconciliation workflow is *itself* the one to run, and not an earlier interrupted one? The recursion bound of RC-002 prevents infinite regress but does not by itself establish a start point.

- **What the spec says.** RC-002 (line 262) declares reconciliation workflows emit exactly one checkpoint commit; RC-003 (line 269) declares a crash leaves no mid-investigation durable state, so re-classification from original state is safe. RC-INV-002 (line 544) invariant-restates this. But the dispatch protocol in §7.1 `reconcile_at_startup` (line 604) jumps straight from "collect in-flight runs" to "for each run, detect category and dispatch." The reconciliation workflow that is *about to run* to handle an outer run is not itself among "in-flight runs" — because it hasn't started yet. But its predecessor (a prior reconciliation workflow that crashed mid-investigation) might be in-flight, with a verdict commit but no verdict-executed commit — that's Cat 3b. Fine. What about one that crashed *before* emitting a verdict? RC-002's "exactly one verdict commit" guarantees there is no commit on that branch at all. The detector then classifies the *outer* run as whatever category its original state suggests — Cat 2 or similar — and re-dispatches a fresh reconciliation. OK. Now what about two overlapping reconciliation workflows that crashed, both investigating the same outer run? RC-001's "each investigator-required category has its own reconciliation workflow" doesn't forbid concurrent investigation.

- **Is the justification adequate?** No. Two gaps:
  - (a) The spec does not declare that at most one reconciliation workflow can be in-flight per outer run at a time. If the daemon's pre-crash state had two reconciliation workflows running against the same outer run (race between two dispatch attempts), restart produces two task branches advertising different reconciliation runs for the same outer run — this matches the Cat 6a detector's last bullet (line 200: "bead `in_progress` with two or more task branches each advertising `Harmonik-Run-ID`"), but the Cat 6a detector is for the *outer* run, not the reconciliation workflows. The recovery path is undefined.
  - (b) RC-003 says "the outer run… MUST be re-classified against unchanged git + Beads state." "Unchanged" is doing a lot of work: if the operator modified Beads between crash and restart, the state is *changed*, and the pre-crash reconciliation's in-progress reasoning is doubly stale. The spec handles per-verdict staleness (RC-024) but not per-workflow-dispatch staleness (do we proceed with the pre-crash category assignment or re-classify?). §7.1 implies re-classify — `detect_category(run)` is called fresh — but this is not a named requirement.

- **Stronger alternative.**
  - (a) Add RC-003a: "The daemon MUST NOT dispatch a reconciliation workflow against an outer run if another reconciliation workflow for the same outer run is already in-flight. If two in-flight reconciliation workflows for the same outer run are detected at startup, the daemon MUST terminate the older (by commit timestamp on the investigator-run's initial branch commit) and let the newer complete; if the branches cannot be disambiguated, the daemon MUST classify the outer run as Cat 6a with sub-reason `concurrent-reconciliation-workflows`."
  - (b) Add RC-003b: "At startup, every outer run MUST be re-classified from its current (post-crash) state. Pre-crash category assignments found in JSONL MUST NOT be used; JSONL is observational per RC-014, and the pre-crash assignment is one observational event the detector does not consume."
  - (c) Name the bootstrap ordering explicitly in RC-001: "During daemon startup, the reconciliation workflow library is the FIRST dispatch target. The daemon MUST NOT dispatch any non-reconciliation workflow until every reconciliation workflow dispatched in the current startup sequence has reached a terminal event (verdict or budget-exhausted)." This closes the "how do we know we're the first thing to run" question by making it a normative ordering.

- **Load-bearing level.** Blocking for correctness under daemon-crash-during-reconciliation, though not blocking for happy-path MVH. A crash during reconciliation is precisely the scenario RC claims to handle; the handler is under-specified.

### Challenge 8 — Detector cadence is undeclared; the spec implicitly assumes "once at startup"

- **Challenge.** The task brief asks whether detectors fire "only at startup, or periodically?" The spec provides §7.1 `reconcile_at_startup` (line 604) and RC-012 ("before any detector executes the daemon MUST verify prerequisites") but never says whether classification is a one-shot event per daemon-start or a recurring sweep. Several detectors — notably Cat 3a (torn Beads write) and Cat 3b (verdict-unexecuted) — describe divergences that can arise mid-run (e.g., a `br` call that fails *during* normal operation), not only from crash state.

- **What the spec says.** §7.1 is a startup protocol. §8.1 Cat 0's "Escalation path" (line 83) says "Daemon retries at configurable cadence" — but that's the Cat 0 prerequisite retry, not detector re-evaluation. Nothing in §4 declares the detector's firing regime. RC-013's `reconciliation_category_assigned` emission timing (line 342) is "after classifying an in-flight run" — which is silent on when classification happens.

- **Is the justification adequate?** No. Three scenarios expose the gap:
  - (a) A `br reopen` call during normal operation writes to the intent log but the subsequent `br` invocation times out. This is a mid-run Cat 3a condition. Does reconciliation detect it immediately, or wait for the next daemon restart? The adapter's intent-log resolution path per [beads-integration.md §10.8a] presumably handles this without a full detector sweep, but the relationship is undeclared.
  - (b) An investigator emits a verdict commit. RC-025 says the daemon executes mechanically and writes the verdict-executed commit. If the daemon crashes between these two steps, Cat 3b fires — but only at restart. The "every detector runs at startup" model handles this. Fine.
  - (c) A long-running reconciliation workflow's target bead is touched by a sibling daemon (ruled out by PL-001's one-daemon-per-project rule) or by an operator via `br` CLI (possible). The staleness check of RC-024 catches this *at verdict-execute time* but not *during investigation*. The investigator could be reasoning off a stale snapshot the whole way through; it is only the action that's blocked.
  - The spec's answer to (c) is correct (snapshot bounds the view; staleness blocks execute; fresh dispatch re-investigates). But the spec never explains the *cadence* premise: reconciliation is a once-at-startup-plus-event-driven-mid-run model, not a polling model. This premise is load-bearing for anyone implementing the daemon loop.

- **Stronger alternative.** Add RC-010a or similar: "Detectors MUST fire at (a) daemon startup per §7.1, and (b) on specific trigger events during normal operation: `br` adapter failure (Cat 3a), `reconciliation_verdict_emitted` emission (Cat 3b, to advance verdict-execute), and operator-issued `harmonik reconcile` (future post-MVH). Detectors MUST NOT be run on a polling cadence during normal operation; the overhead is not justified." Make the event-driven triggers explicit.

- **Load-bearing level.** Important. An implementer assuming a polling-cadence model will burn CPU and log noise on every startup interval; an implementer assuming startup-only will miss mid-run torn writes. The spec has to pick.

### Challenge 9 — Evidence-corroboration gating: inconclusive handling is cited but not integrated

- **Challenge.** The task brief asks whether inconclusive handling is concrete, citing EV-023a as the backing rule. [event-model.md §4.5 EV-023a] (line 425) declares that divergence detectors MUST classify evidence as `git-corroborated`, `beads-corroborated`, or `inconclusive`, and MUST NOT emit `store_divergence_detected` for inconclusive evidence — instead emitting `divergence_inconclusive`. Reconciliation.md does not pick this up. RC-014 (line 348) describes JSONL divergence-evidence scope, but never mentions corroboration classification or the `divergence_inconclusive` event. An inconclusive-evidence code path is missing from reconciliation's view.

- **What the spec says.** RC-014 (lines 348-367) lists permitted and forbidden JSONL uses. The first permitted use is "Detecting that a checkpoint commit referenced in a JSONL `checkpoint_written` event is missing from git (triggers Cat 6b)" — this is exactly the kind of evidence EV-023a requires classifying. RC-014's emission rule (line 365) says the detector MUST emit `store_divergence_detected` — with no mention of the `corroboration` field that §8.6.8 requires, no mention of `divergence_inconclusive` as an alternative emission, no mention of the post-crash-window guardrail of EV-023.

- **Is the justification adequate?** No. Three concrete gaps:
  - (a) A JSONL `checkpoint_written` event whose payload carries no `commit_hash` field (or an unparseable one) is `inconclusive` per EV-023a. The detector MUST emit `divergence_inconclusive`, not `store_divergence_detected`. RC-014 has no such rule — it assumes emission is always `store_divergence_detected`.
  - (b) The post-crash-window guardrail per EV-023 says evidence inside the window may be a lossy-tail artifact, not real divergence. Reconciliation's detector sees an event it cannot corroborate and — under the current spec — emits a Cat 6b. This is a false positive: the event was simply never flushed before the crash. The `post_crash_window` flag on the event would tell the detector this, but RC-014 never mentions the flag.
  - (c) What does reconciliation *do* with a `divergence_inconclusive` event? Does it dispatch a Cat 3 generic investigator to reason about whether it matters? Does it log-and-ignore? Does it feed it into Cat 6b? The spec is silent. Reconciliation should own the downstream of this event — it's the only subsystem that *could* act on it.

- **Stronger alternative.** Rewrite RC-014's emission rule to match EV-023/023a:
  - (a) "On detecting candidate divergence, the detector MUST classify evidence per [event-model.md EV-023a]. For `git-corroborated` or `beads-corroborated` evidence, emit `store_divergence_detected` per §8.6.8. For `inconclusive` evidence, emit `divergence_inconclusive` per §8.6.10 and MUST NOT dispatch a reconciliation workflow on inconclusive evidence alone; reconciliation only acts on corroborated divergence."
  - (b) Add an explicit reconciliation response to `divergence_inconclusive`: either "log and move on" (treat as observational) or "escalate to Cat 6a for investigator reasoning only if a sibling corroborated event appears within the post-crash window."
  - (c) Cite EV-023's post-crash-window flag in RC-014's permitted uses; declare that Cat 6b detectors MUST ignore corroborable-absence evidence inside the post-crash window unless a sibling corroborated event is also present.

- **Load-bearing level.** Important. The event-model spec at v0.3 already added EV-023a specifically to prevent reconciliation false-positives from lossy JSONL tails. Reconciliation.md at v0.2 has not integrated the rule. This is an inter-spec drift that lands the corpus in an inconsistent state if both are finalized as-is.

## Scope leaks — §2.2 violations

- **Event payload fields in RC-018** (line 411). RC-018 describes the `reconciliation_budget_exhausted` payload as `(run_id, workflow_id, budget_seconds, elapsed_seconds)`. §2.2 (line 48) declares "Event payload schemas for reconciliation events — owned by [event-model.md §3.2]." Naming fields in a normative RC-* requirement leaks event-payload ownership. Stronger: replace with "emit `reconciliation_budget_exhausted` per [event-model.md §3.2]" — drop the field list. The fields belong in event-model and the sibling schemas.md §6.1 (which already defines `BudgetExhaustedPayload`, line 110) is the canonical source.

- **Event payload fields in RC-023** (line 453). Same leak: RC-023 names `(investigator_run_id, target_run_id, malformation_reason, raw_verdict_excerpt)` inline. The malformation enum is correctly delegated to schemas.md §6.1 (`MalformationReason`, line 100), but the payload-field list leaks here. Same fix: delegate the payload shape to event-model/schemas.md.

- **Event payload fields in RC-024** (line 470). `(snapshot_token, current_values, divergence_reason)` — same leak. schemas.md §6.1 already defines `StaleVerdictPayload` (line 118). Point RC-024 at the schema and drop the inline list.

- **Event payload fields in RC-025** (line 481). `(investigator_run_id, target_run_id, verdict, executed_at_timestamp, action_summary)` — same leak. No corresponding `VerdictExecutedPayload` appears in schemas.md; either define it there and cite, or delegate entirely to event-model.

- **RC-017 budget defaults**. (line 401) "Cat 2 default: 600 seconds; Cat 3 generic default: 300 seconds; Cat 6a default: 900 seconds." §2.2 (line 49) declares "DOT authoring details for the reconciliation workflow library — owned by the S01 Orchestrator Core subsystem spec, post-MVH." Budget defaults for reconciliation-workflow YAML policies are squarely in S01's scope; baking them into reconciliation.md couples this spec to S01's choices.
  - Stronger: "S01 MUST declare per-category defaults in the workflow library; values are S01's call." Keep the per-category structure (RC-017 is right that there must be a default per category) but not the numbers.

- **§A.3 fails to exist as a rationale appendix** (grep confirms no §A in the file). The task brief notes "§A.3 of the spec probably has rationale for [orthogonality]." It does not — the spec has no Appendix section. For a taxonomy-first spec whose central claim is "11 categories are disjoint," an §A.3 rationale on orthogonality + a §A.2 counter-examples section is load-bearing. Not a scope leak strictly, but a scope *gap*.

## First-plausible-answer findings

- **RC-017 per-category budget defaults: 600 / 300 / 900 seconds.** The numbers are not justified in text — no calibration against expected investigator turns, no tie to handler-contract's 600s silent-hang default. A Cat 3 generic investigation that has to walk git history, query Beads, read JSONL tail, and reason about store divergence is not obviously smaller than a Cat 2 investigation — why is its budget half? A Cat 6a investigation that will usually recommend `escalate-to-human` is not obviously larger — why is its budget 50% higher than Cat 2? These feel chosen as "some number a reviewer would accept," not as a calibrated estimate. Since S01 owns these per the scope-leak finding above, they should leave this spec entirely.

- **RC-025 idempotency claim for `resume-here` and `resume-with-context`.** (line 487) The claim is: "dispatching the outer run's next node is idempotent at the dispatch layer (the next dispatch check sees the outer run already running and does not re-dispatch)." This is too glib. For `resume-with-context`, the verdict carries an investigator-supplied `context` string that is injected into the run's shared context. If the verdict is executed twice (e.g., via a Cat 3b re-execution path per RC-026), the context injection happens twice — the claim that "context injection is additive (repeated application produces the same context map)" in the schemas.md §6.2 table (line 140) is only true if the map key is pinned across re-execution. The spec does not name the map key for the injection. If the investigator's context is keyed by `investigator_run_id`, idempotency holds. If keyed by the outer `target_run_id`, two re-executions with different investigator contexts (e.g., after a fresh dispatch per RC-024 staleness) might or might not produce the same final map depending on which wrote last. Spec needs to name the key explicitly.

- **Cat 0 Beads SQLite lock rule.** §8.1 (line 79): "Beads SQLite locked by a non-harmonik process." The "non-harmonik" qualifier requires a mechanical probe that distinguishes harmonik-owned from non-harmonik-owned SQLite connections. SQLite's file-lock mechanism doesn't carry process-identity; what probe does the daemon use? `lsof` + argv inspection? The detector rule reads plausible but is not mechanically grounded. Further: on a laptop, a developer's `sqlite3` REPL open against the Beads DB is "a non-harmonik process holding a lock" — Cat 0 then fires and the daemon enters `degraded` for the duration of the REPL. Is that the intended behavior? Plausibly yes, but the spec should say so.

- **RC-019 WIP capture for `reopen-bead`.** The requirement says "capture a diff plus file listing" into the reconciliation commit. Plausible, but the format and the re-application path are unspecified. If a later run claims the re-opened bead, does the new run's worktree get the WIP re-applied? The requirement says "mandatory for `reopen-bead` verdicts and OPTIONAL for other verdicts (which keep the worktree and retain WIP by default)" — which leaves the operator/re-runner to figure out what to do with the captured diff. The capture is a museum piece, not a recovery mechanism. Stronger: declare it "captured for operator inspection, not automatically re-applied. Re-application is a post-MVH workflow-library feature."

- **RC-016 "playbook" as a named artifact without a schema.** (line 390) "The S01-shipped YAML policy MUST define a playbook: a specific ordered sequence of checks producing evidence for the verdict." No schema for what a playbook *looks like* — ordered YAML list? What's a "check"? Is a check a node in the reconciliation DOT, or something inside a node's prompt? InvestigatorInput includes `playbook_ref: String` (schemas.md §6.1 line 41) — a string pointing at... what? Pick one: (a) playbooks are inline YAML lists of mechanical check steps with a schema declared here; (b) playbooks are prompt fragments the S01 library composes — then drop the "MUST define a playbook" from a normative requirement here.

- **RC-020 "six enum values" count vs seven semantic needs.** The six are `resume-here`, `resume-with-context`, `reset-to-checkpoint`, `reopen-bead`, `accept-close-with-note`, `escalate-to-human`. Missing: a "no-op / investigator-sees-no-divergence" value for the case where an investigator dispatched for Cat 2 concludes the prior work actually finished and the apparent in-flight signal was a race. Current option: route via `resume-here` with an empty context (and hope the next dispatch check correctly observes the run as completed). Better: a `no-op-mark-run-consistent` verdict whose mechanical action is "emit a reconciliation-concluded event, write the verdict-executed commit, do nothing else." The current six-value enum assumes every investigation produces an action; investigations that produce a *determination of no action needed* are squeezed through a misfit value.

- **§8.12 "typical verdict" column conflates legality with likelihood.** (lines 234-235) "Typical verdict (if investigator): `resume-with-context` / `reset-to-checkpoint` / `reopen-bead`" for Cat 2. These are the verdicts the *author* expects most often, not an exhaustion of the enum. Does Cat 2's investigator have the full six-value enum available, or is it restricted to the three "typical" ones? The spec never says. If restricted, that's a per-category policy that needs a normative declaration. If not restricted, the "typical" column is documentation, not contract — fine, but the column header should be `Likely verdicts (informative)`.

- **Cat 5 "includes orphaned branches from prior runs of beads that have since been re-claimed."** (§8.8 line 183) Plausible but entangled. The prior-run's `Harmonik-Run-ID` on the orphaned branch is not in-flight for any current bead — agreed, that's Cat 5. But *how* does the detector know the bead was re-claimed? It must cross-reference the bead's current state (claimed by a different run_id) via Beads. RC-010 says filter-by-`Harmonik-Run-ID`; RC-INV-005 forbids filter-by-bead_id. Yet Cat 5's "re-claimed" judgment requires exactly a bead_id cross-lookup. The two rules pull against each other. Fix: state the rule as "a run is Cat 5 when no in-flight checkpoints exist for its run_id; whether the bead has been re-claimed is irrelevant" — the re-claim is the bead's problem, not the run's.

## Invariant audit — cross-subsystem + sensor per AR-042

Per template §5 selection test + AR-042 sensor rule, the five §5 invariants:

- **RC-INV-001 — Git wins on completion disagreement** (line 538). Genuinely cross-subsystem (git + Beads + JSONL + reconciliation routing). HOLDS the template selection test. But the invariant block does not name its sensor per AR-042 — there's no `Sensor:` line or §10.2 cross-ref. Fix: add "Sensor: corpus lint across [beads-integration.md §10.7] authority rules + every Cat 3/Cat 6 detector emission path; reviewer persona (conformance-auditor) for non-lintable cases."

- **RC-INV-002 — No mid-investigation durable state** (line 544). This is RC-002 + RC-003 restated. RC-002 is a cadence requirement inside §4.1; RC-003 is a recursion-bounding requirement also inside §4.1. The invariant says the same thing emphatically. FAILS the selection test — this is a §4 requirement promoted to §5. Delete the §5 copy OR rewrite as a cross-subsystem claim (e.g., "any subsystem observing a mid-investigation durable artifact MUST flag reconciliation as a defect"). Also missing AR-042 sensor.

- **RC-INV-003 — (verdict-emitted, verdict-executed) is the complete durable record** (line 550). This is RC-022 + RC-025 restated. FAILS the selection test — both the emit and the execute commits are declared in §4.5 with exact shapes. Delete the §5 copy. Also missing AR-042 sensor.

- **RC-INV-004 — Snapshot-bounded investigator view** (line 556). This is RC-015 + RC-024 restated. Borderline cross-subsystem (the investigator lives in a handler subprocess per handler-contract; the snapshot lives in the daemon's memory pre-dispatch). Can be saved by rewriting as "every subsystem delivering data to an investigator agent MUST bound that data to the snapshot token; reconciliation routes, handler-contract transports." Otherwise collapse into RC-015. Also missing AR-042 sensor.

- **RC-INV-005 — Detectors filter by run_id, never bead_id** (line 562). This is RC-010 restated. FAILS the selection test — the rule fits inside §4.3 without reference to other subsystems. Delete the §5 copy. Also missing AR-042 sensor.

Summary: three of five §5 invariants (INV-002, INV-003, INV-005) are §4 emphatic restatements and should be deleted per template rules. Only INV-001 is clearly cross-subsystem; INV-004 can be saved with a rewrite. None of the five names its sensor; AR-042 is a foundation-cross-cutting obligation that applies to every invariant across every spec. This is a blocker for a reviewed-status transition (the architecture spec checks AR-042 in its §10.2 reviewer-enforced obligations).

## Minor findings (MUST/SHOULD discipline and definitional gaps)

- **RC-020 "exactly one of the six enum values" vs a missing seventh.** See Challenge 6. Also: the phrase "Any other value is a malformed verdict" folds together "value not in the enum" and "value-in-enum-but-wrong-for-context" (e.g., `reset-to-checkpoint` on a verdict event where `checkpoint_ref` is null). RC-023 MalformationReason enum (schemas.md line 100) *does* split these (`unknown-verdict-value`, `missing-required-field`, `wrong-type`) — but RC-020's prose "Any other value" only covers the first. Fix: RC-020's "malformed" should cite the full MalformationReason enum.

- **"in-flight" definition gap.** The spec uses "in-flight run" heavily (§8 intro, RC-010, RC-INV-005, §7.1 pseudocode) but never defines it. The execution-model critic exemplar flags the same term in its "Definitional gaps." Reconciliation inherits the problem and also doesn't patch it — it's one of the most-used terms in the spec. Either add a glossary entry cross-referencing execution-model's definition (once execution-model adds one) or define it here as "a run whose last durable state is not a workflow-terminal state AND whose bead is not `closed`."

- **"outer run" vs "target run" terminology.** The spec uses both interchangeably (§4.4 "outer run being reconciled"; §4.5 "target (outer) run"; schemas.md "target_run_id"). Pick one. The schemas.md field is `target_run_id`, so "target run" should be canonical; "outer run" should be retired or glossed as a synonym.

- **"divergence" definition gap.** The task brief flags this directly. The spec uses "divergence" as a MUST trigger (RC-014: "On detecting divergence, the detector MUST emit `store_divergence_detected`"; RC-INV-001: "the divergence MUST route through reconciliation") but never gives a mechanical decision procedure. A normal lifecycle event is not a divergence; a Cat 3 generic case is. Where is the line? Candidates:
  - A merge commit landing in git while the bead is still `in_progress` — mid-commit race, not divergence? Or is this exactly Cat 3c?
  - A `br reopen` call in-flight (intent log written, `br` call still running) — not divergence, in-transit state?
  - A checkpoint commit whose transition-record sibling file hasn't been written yet (atomic-commit race window per [execution-model.md §4.4 EM-018]) — race, or Cat 6a?
  The definition should live in §3 glossary: "divergence = a cross-store state whose reconciliation rule (§8) requires a non-default daemon response, as distinct from in-transit states covered by adapter idempotency." Then every "divergence" reference is mechanically dereferenced.

- **RC-027 MUST vs MAY.** (line 500) "A per-reconciliation-workflow policy option MUST allow operators to pause... Default: execution proceeds without operator confirmation. Operators opt in by policy." The MUST is on the *provision of the option*; the default is execution-without-confirmation. §10.1 (line 698) then says "MAY ship as a follow-on within one release after MVH" — which contradicts the MUST. Either the option MUST be in Core MVH (and §10.1 is wrong) or it MAY be deferred (and RC-027 should be SHOULD). Pick one.

- **RC-028 continuation of run_id after reopen-bead forbidden.** (line 512) "continuation of the prior `run_id` after a `reopen-bead` verdict is forbidden." Who enforces? The workspace-model's re-run rule per [workspace-model.md §5.9] presumably, but this spec needs to name the sensor (is it a lint, a scenario test, a runtime check?).

- **§8.13 "failure-commit deferral" belongs in §A.3 or in OQ-RC-001.** It is 3 lines of rationale + a pointer to the open question. Already duplicated in OQ-RC-001 (line 719). Delete §8.13; the open question carries the content. The §8.13 prose contaminates §8 with informative text that looks like a taxonomy category ("8.13 Failure-commit deferral"). Either move to §A.3 Rationale or compress into an `> INFORMATIVE:` callout under §8.

- **RC-005 "detectors and verdict-execution mechanics are NOT in the workflow library."** (line 282) Good rule. But it's at odds with RC-001 "each investigator-required category has its own reconciliation workflow in the S01-shipped library." The detector fires a category assignment and the daemon dispatches a workflow; the verdict-execution is daemon code. What's in the workflow library is only the investigator reasoning. That's correct — but the split reads as "the detector, the action-map, the verdict-execution, and the verdict-executed commit are ALL daemon Go code; only the investigator's reasoning path is S01 YAML/DOT." Consider saying so plainly.

- **Absence of §A Appendices.** The template §A reserves slots for examples (§A.1), counter-examples (§A.2), and rationale (§A.3). A spec whose central claim is "11 categories form a bounded partition" benefits enormously from §A.2 counter-examples walking each overlap pair. The current draft has no §A at all. Recommended additions: §A.1 worked example of each investigator-required category end-to-end (Cat 2 crash → investigator → verdict → execute); §A.2 counter-examples for the five overlap pairs of Challenge 1; §A.3 rationale for the 11-value taxonomy count and the sub-category split.

- **"Emitted event" prose in §8 subsections is redundant with §6.5.** Every §8.N category block ends with "Emitted event: X + Y". §6.5 (lines 586-596) lists all events emitted by this spec with their emission timing. The two lists should cross-reference, not duplicate. Fix: in each §8.N block, replace the "Emitted event" line with "See §6.5 for emission; events for this category: X, Y."

- **Detector cadence (task-brief §9.1a).** See Challenge 8 above. Also: the §7.1 pseudocode is titled "Startup-reconciliation dispatch (protocol pseudocode)" which is good scoping, but the spec never declares the *other* dispatch path. A second pseudocode section "§7.1a Mid-run divergence dispatch" is missing; that would resolve the cadence question by declaring the mid-run entry points (adapter failure, operator CLI call, etc.) explicitly.

- **RC-026 "startup detector of [process-lifecycle.md §8.2 step 5]."** (line 495) Process-lifecycle §PL-005 (per the actual file) lists seven numbered steps; step 5 there is "Beads query for in-progress beads," not the step that checks for reconciliation-workflow presence. Step 7 is "Dispatch reconciliation." The cite is off by at least one step. Either the cite is wrong or process-lifecycle's numbering changed and this spec didn't follow. This is a lint-detectable drift.

- **Glossary entry "Cat N" is load-bearing but awkward.** (§3 line 63) "Cat N — shorthand for detection category N in the §8 taxonomy; references like `Cat 3a` resolve to §8.4a." The shorthand is used throughout the spec. Recommend either: (a) canonical form is `Cat N`, never `Category N` or `cat-N` (schemas.md uses lowercase `cat-N`, which diverges); or (b) canonical form matches schemas.md's enum value (`cat-3a`). Currently all three forms appear. Pick one and apply.

## Affirmations

Six decisions that hold up under pressure.

1. **Reconciliation-as-workflow plus bounded recursion via verdict-only commits (RC-001 + RC-002 + RC-003 + RC-INV-002).** Elegant. The single-verdict-commit rule is load-bearing and well-placed; the recursion bound follows mechanically. Matches the locked decision (locked 2026-04-24 per user) and avoids the obvious trap of "reconciliation needs its own reconciliation." The `workflow_class = reconciliation` tag as the routing key is the right mechanical handle for the exception to normal checkpoint cadence.

2. **Snapshot token + staleness check (RC-015 + RC-024 + RC-INV-004).** The `(git_head_hash, beads_audit_entry_id)` tuple is the right mechanical handle. Staleness detected by advance-of-either-stream + fresh-dispatch is exactly the right behavior under a multi-minute investigator wall-clock. "Changes to sibling beads... MUST NOT trigger staleness" (line 472) is the right tightening — otherwise any active daemon invalidates every in-flight investigation.

3. **Malformed-verdict as fallback-to-escalate + ZFC rationale (RC-023).** The `> RATIONALE:` block (line 461) nails the principle: trusting LLM-generated text to conform is a ZFC violation. The fallback-to-`escalate-to-human` path converts cognition-failure into deterministic escalation. This is the cleanest treatment of this class of failure in the corpus. The MalformationReason enum with six explicit values (schemas.md line 100) including `verdict-after-terminal` and `multiple-verdicts` shows genuine care about edge cases.

4. **Run-scoped, not bead-scoped, classification (RC-010 + RC-INV-005).** The explicit forbid on filtering by `Harmonik-Bead-ID` matches the one-bead-many-runs model of [execution-model.md §4.3]. The "orphaned branches from prior runs... classify as Cat 5" rule is surgically precise — it avoids the trap where re-claimed beads drag old runs into reconciliation. (The invariant itself should still be deleted per the selection test; the rule is right, the placement is wrong.)

5. **Split between detector code (daemon Go) and investigator reasoning (S01 library) per RC-005.** Clean ownership boundary. Detectors are mechanism-tagged; investigators are cognition-tagged; the verdict-execution mechanics are mechanism-tagged. This mapping to the architecture-level mechanism/cognition split is correct and makes conformance testing tractable (detectors get unit tests; investigators get scenario tests with twin handlers).

6. **Cat 6b's refusal to spawn an investigator.** (§8.11a line 224: "spawning an investigator to 'explain why git is corrupt' burns tokens on an operator-only problem") The rationale here is exactly right and rare enough to call out: not every failure class deserves cognition. An investigator reasoning about a corrupt git object database is strictly more expensive than calling `git fsck --dangling` and emailing the operator. The auto-escalate path is the honest answer.

## Hidden assumptions

1. **`.harmonik/beads-intents/` is always the intent-log path.** The Cat 3a detector (§8.4a, line 133) hard-codes this path. `.harmonik/` is the daemon's owned directory (per [process-lifecycle.md §PL-004]), but the intent-log location is a beads-integration concern. If beads-integration moves the path, Cat 3a's detection rule breaks silently. Spec should cite [beads-integration.md §10.8a] as the owner of the path.

2. **The snapshot token's `beads_audit_entry_id` is a monotonic, mechanically-orderable ID.** RC-024's staleness check (line 466) requires comparing "the target run's bead has changed status in the Beads audit log since the snapshot" — this is a range test over audit entries. `beads_audit_entry_id` is typed `String` in schemas.md §6.1 (line 22). An opaque string is not range-testable without a secondary ordering. Spec should either (a) type this as a UUIDv7 for time-ordering, or (b) store `beads_audit_entry_seq: Integer` alongside, or (c) cite [beads-integration.md §10.8a] for the comparison rule.

3. **An investigator's reconciliation-commit-plus-evidence fits in one git commit.** RC-002's "single git commit" plus RC-019's WIP-capture-for-`reopen-bead` could push the commit size unbounded (a large WIP + diff). Git tolerates large commits; operator tooling may not. No budget is declared on reconciliation-commit size.

4. **Cat 6a's `.git/rebase-merge` etc. probes tell us the work is not-harmonik's.** The §8.11 third bullet (line 199) says "not the work product being tracked by harmonik." How does the detector decide "not harmonik's"? The presence of `.git/MERGE_HEAD` during a harmonik-orchestrated merge (which is a plausible thing a workflow node might do) would match this detector and misclassify. Spec needs a distinguishing rule — presumably the merge was initiated by a harmonik node and the daemon knows about it, but the detector's predicate is "presence of the file," which is ambiguous.

5. **`reconciliation_verdict_emitted` on budget-exhausted is never emitted.** RC-018 (line 411) on budget exhaustion: "emit `reconciliation_budget_exhausted`... issue a default verdict... NOT write a reconciliation commit." So the budget-exhausted path produces a daemon-default `escalate-to-human` action *without* emitting `reconciliation_verdict_emitted`. §6.5 (line 589) lists `reconciliation_verdict_emitted` as "emitted when the investigator produces a verdict" — fine for the happy path, but the operator-observable "a verdict was made" fact is carried on different events for the two paths (investigator → `reconciliation_verdict_emitted`; budget-exhausted → `reconciliation_budget_exhausted` + synthesized verdict with no emission). An audit query "show me all verdicts for run R" has to union two event types. Declare this explicitly or harmonize the emission.

6. **Schema-version field is per-event.** `VerdictEvent.schema_version` (schemas.md line 71) is on the record; §6.4 (line 582) says "versioned via the event envelope's `schema_version` field." These are two different `schema_version` fields (one on the record, one on the envelope). [event-model.md §6.4] presumably pins the envelope field; if the record also carries one, the compatibility contract is ambiguous. Drop the record-level field and use the envelope's.

7. **The investigator sees the JSONL tail as observational context.** RC-015 (line 380) and schemas.md `InvestigatorInput.jsonl_tail` (line 37) include the JSONL tail "since the last checkpoint as-of snapshot, observational only." But JSONL has the post-crash-window problem of EV-023 — events in the lossy-tail may be incomplete or torn. The investigator is presumably expected to reason about this, but the input doesn't flag which entries fall within the post-crash window. The investigator would then hallucinate certainty about incomplete evidence. Fix: schemas.md `InvestigatorInput` should carry `jsonl_tail_post_crash_window_boundary: EventID | None` — the boundary marker beyond which entries are observed-complete and before which they're lossy. Or the jsonl_tail becomes `List<(EventEnvelope, post_crash_window: Bool)>`.

8. **Every run has exactly one task branch.** Cat 6a's last bullet (line 200) contemplates "a bead in `in_progress` with two or more task branches each advertising `Harmonik-Run-ID`" as an integrity violation — which implies the normal case is one run → one task branch. [workspace-model.md §5.8 Branching model] is cited but not quoted. If a legitimate use case produces multiple task branches per run (replay experimentation? branch-and-test?), Cat 6a has to be narrowed.

9. **An investigator can actually finish within 600/300/900 seconds.** The budgets in RC-017 assume a Claude Code subprocess can walk evidence, reason, and produce a schema-valid verdict event within those wall-clock ceilings. For a Cat 3 generic case with a complex git/Beads history, this is tight. An Opus-class reasoning pass over a 200-commit task branch is not obviously <300s. No calibration is cited. (See the earlier first-plausible-answer finding; this is the downstream consequence.)

10. **RC-004's "skill-injection set (Beads-CLI and git-inspection skills at minimum)."** The "at minimum" is doing work. What are the *maximum* skills an investigator gets? Does it get a web-search skill? A code-execution skill? The template for a reviewer-persona would say the investigator can only use explicitly-allowed skills (principle of least authority); the spec's "at minimum" phrasing invites maximal injection. Recommend flipping to "exactly these skills and no others": Beads-CLI, git-inspection, and a to-be-defined reconciliation-evidence-reader skill (for JSONL + workspace probes).

## Invariant-audit rewrite suggestions

For the three invariants that fail the selection test, concrete replacements:

- **Delete RC-INV-002 entirely.** Its content is in RC-002 + RC-003. No cross-subsystem claim is present. The bounded-recursion property follows from the requirements; it does not need a separate invariant.

- **Delete RC-INV-003 entirely.** Its content is in RC-022 + RC-025. The "pair of commits" structure is declared in the verdict-execution requirements; restating as an invariant adds emphasis but no cross-subsystem constraint.

- **Delete RC-INV-005 entirely.** Its content is in RC-010. The "detectors filter by run_id" rule fits inside §4.3 without reference to another subsystem.

For the two that are genuinely cross-subsystem, add sensors:

- **RC-INV-001** — add: "Sensor: (a) corpus lint across [beads-integration.md §10.7] authority rules + every Cat 3/Cat 6 detector emission path; (b) reviewer persona `conformance-auditor` for non-lintable cases (semantic match of detector rules to the git-wins invariant)."

- **RC-INV-004** — rewrite as: "Every subsystem delivering data to an investigator agent MUST bound that data to the snapshot token captured at dispatch time. Reconciliation owns the snapshot capture (RC-015); handler-contract owns the LaunchSpec delivery (HC-006 snapshot_token field); the investigator's skills (Beads-CLI, git-inspection per RC-004) MUST consume the snapshot token as a ceiling on reads. Sensor: scenario test that dispatches an investigator against a pre-recorded snapshot, then advances the target bead externally; the verdict-execute path MUST detect staleness and refuse."

## Recommendation

**Proceed with revisions.** Of the nine load-bearing findings:

- Blocking (must land before status: draft → reviewed): Challenge 1 (orthogonality routing rule), Challenge 7 (bootstrap ordering + concurrent-reconciliation handling), Challenge 9 (EV-023a corroboration integration — inter-spec drift), the AR-042 invariant-sensor obligation for all 5 invariants, and the three §5 invariants that fail the selection test (RC-INV-002, RC-INV-003, RC-INV-005).
- Important (should land before reviewed but can be defer-tracked in OQ if necessary): Challenge 2 (detector preconditions), Challenge 3 (3a/3b/3c ordering), Challenge 4 (§8.12 / §6.3 duplication), Challenge 5 (investigator-input vs LaunchSpec alignment), Challenge 6 (verdict enum completeness / detector-synthesized verdicts), Challenge 8 (detector cadence declaration).
- Scope leaks (payload-field lists in RC-018/023/024/025): straightforward delete-and-cite edits; should land with the rest.

None of the findings require re-opening the locked 11-category shape or the reconciliation-as-workflow decision. The taxonomy is right; the contracts around it need sharpening. Estimated revision size: the spec's normative shell (§§4, 5, 8, 6.5) is the bulk of the work; schemas.md is largely already correct and needs only the field-list hand-offs and (for Challenge 6) a seventh enum value or a `provenance` field addition.

Adding an §A.3 Rationale on taxonomy orthogonality and an §A.2 Counter-examples section (for each of the cross-category overlap pairs in Challenge 1) would materially strengthen the spec and is cheap relative to the value. Recommended as part of the revision.

## File references

- `/Users/gb/github/harmonik/specs/reconciliation/spec.md` (v0.2.0 draft; lines cited inline above)
- `/Users/gb/github/harmonik/specs/reconciliation/schemas.md` (v0.2.0 supplement; lines cited inline above)
- `/Users/gb/github/harmonik/docs/foundation/spec-template.md` v1.1 (template conformance reference)
- `/Users/gb/github/harmonik/specs/architecture.md` (AR-042 sensor obligation, line 357; AR-020/AR-021 amendment protocol)
- `/Users/gb/github/harmonik/specs/handler-contract.md` (HC-006 LaunchSpec, line 113; HC-007/007a/007b progress-stream wire protocol)
- `/Users/gb/github/harmonik/specs/event-model.md` (EV-023/023a divergence-evidence + corroboration; event-type registry §8.6)
- `/Users/gb/github/harmonik/specs/process-lifecycle.md` (PL-005 startup order; PL-009 ready criteria; PL-010 degraded state)
- `/Users/gb/github/harmonik/docs/reviews/2026-04-24-execution-model-r1/critic.md` (exemplar for review shape and tone)
