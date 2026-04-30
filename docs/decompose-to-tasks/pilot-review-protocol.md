# Pilot Review Protocol

`protocol-version: 0.2` — drafted 2026-04-27 to run between "pilot draft written" and "load into Beads" for each `<spec>-pilot.md` in the decompose-to-tasks pass, then patched same day to v0.2 to add finding-triage rules (pilot-patch lane vs discipline-patch lane) before the protocol runs against the first non-BI spec. The protocol spawns three reviewer subagents in parallel; their combined output decides whether the pilot is ready to load.

> Companions: [discipline.md](discipline.md) v0.4 (the rules being reviewed against), [bi-pilot.md](bi-pilot.md) v0.1.3 (the worked instance), [bi-smoke-load-findings.md](bi-smoke-load-findings.md) (the bug classes that motivated this protocol).

## 1. Purpose

The decompose-to-tasks pass produces a `<spec>-pilot.md` paper draft per spec — a translation of the spec's requirements into Beads-loadable tasks. Drafts can have bugs that neither the discipline alone nor Beads's load-time check will catch:

- **Missed requirements.** A `<prefix>-NNN` exists in the spec but no bead covers it.
- **Tally arithmetic errors.** §7's totals don't match the actual table row counts.
- **Stale references.** The pilot was drafted against an older spec version; the schema or count is out of date.
- **Decomposition judgment errors.** A coalesce groups separable concerns; a multi-step split fragments a single state machine; a description doesn't match what the spec actually says.
- **Invented or missed cross-spec edges.** The pilot emits an edge to a target the source spec never inline-cites, or omits an edge for a target the spec does cite.

Beads catches structural bugs (cycles, bad refs) at load. The reviewer pass catches the rest.

## 2. When to run

- **Default: always.** Every new `<spec>-pilot.md` draft goes through the protocol before loading into Beads.
- **After source-spec patch:** If the source spec is patched after a pilot has been reviewed and loaded, re-run at least the coverage reviewer (§3.1) to verify the pilot still covers the new requirement set.
- **After pilot patch:** If the pilot is patched in response to review findings, re-run the relevant reviewers. If the patches are mechanical (renames, typo fixes in descriptions), one re-run is enough; if they restructure the bead set (new coalesce, new split, new cross-spec edges), all three reviewers re-run.
- **Skip only when:** the patch touches prose only and demonstrably cannot affect bead structure or edges (e.g., fixing a typo in a row's "notes" column).

## 3. Reviewer roster

Three reviewers run in parallel as subagents spawned via the Task tool. Each has a narrow remit and writes its findings to a separate file under `docs/reviews/<date>-<spec-id>-pilot-r1/`. The synthesis step (§4) combines them.

### 3.1 Coverage reviewer

**Goal.** Verify every requirement, invariant, and schema in the source spec is accounted for in the pilot.

**Inputs.**
- The source spec file (e.g., `specs/architecture.md`).
- The pilot draft file (e.g., `docs/decompose-to-tasks/ar-pilot.md`).
- The discipline file (`docs/decompose-to-tasks/discipline.md`).

**Method.**
1. Enumerate every numbered ID in the source spec's §4 (requirements: `<prefix>-NNN`, including letter suffixes like `-NNNa`).
2. Enumerate every invariant in §5 (`<prefix>-INV-NNN`).
3. Enumerate every named record / interface / enum in §6.
4. Enumerate every error class / category in §8 (if present).
5. Enumerate every requirement marked `[retired]`.
6. For each enumerated ID, verify the pilot accounts for it:
   - **§4 reqs:** must appear as a row in pilot §2, OR be coalesced (§2.3 — must be named in another row's coalesce comment), OR be retired (named in spec-parent description).
   - **§5 invariants:** must appear as a row in pilot §3.
   - **§6 schemas:** must appear as a row in pilot §4 (large taxonomies follow §2.11(c) — one bead per category).
   - **§8 errors:** must appear as a row in pilot §4 (single-table) or §2 cluster (per-category, large taxonomy).
7. Verify pilot §1's "Counts" section matches the actual numbers from steps 1–4.
8. Verify pilot §7's tally arithmetic adds up.
9. Verify pilot §1's spec-version reference matches the source spec's current version (catches stale drafts).

**Output.** A markdown file `coverage-r1.md` containing:
- A list of any missed IDs (in source spec but not in pilot).
- A list of any phantom IDs (in pilot but not in source spec — could be typo).
- Tally inconsistencies, with the specific arithmetic.
- Stale-version flags.
- Severity per finding: BLOCKER (missed req), MAJOR (count mismatch), MINOR (cosmetic).
- Lane tag per finding: `local` (this pilot's application error — discipline applied correctly, just got the answer wrong here) or `class` (the discipline rule has a bug or gap — any pilot in this position would hit this), with a one-line justification.

### 3.2 Decomposition-quality reviewer

**Goal.** Verify each task's structure and description is faithful to the spec and follows the discipline's rules.

**Inputs.**
- The source spec.
- The pilot draft.
- The discipline (especially §2.1 granularity, §2.2 multi-step, §2.3 coalesce, §2.4 test/impl, §2.5 sensor/invariant, §2.6 schema).

**Method.**
1. Sample 10–15 beads across the pilot, weighted toward complex ones: every coalesced cluster, every multi-step umbrella, every sensor, plus 3–5 random first-class beads.
2. For each sampled bead:
   - Read the bead's description against the source requirement's body.
   - Question 1: **Does the description match what the spec says?** Does it leave out any normative requirement? Does it overstate?
   - Question 2 (for coalesces only): **Is the coalesce sound per §2.3?** Specifically — do the requirements share a single data shape or code path? Or are they orthogonal concerns that should stay separate?
   - Question 3 (for multi-step only): **Is the split sound per §2.2?** Specifically — does the protocol have ≥3 steps, are the steps independently testable, and does the umbrella lose meaning when stripped? If any signal fails, the split is over-decomposition.
   - Question 4 (for sensor beads only): **Is the sensor description a real verification mechanism?** Or just a restatement of the invariant? Does it name specific lints, scenario tests, reviewer scans?
   - Question 5 (for schema beads only): **Is the field/enum list complete?** Does it match the spec's §6 declaration?
3. Check for "missing-coalesce smell": clusters of requirements that share the same prefix and could plausibly have been coalesced but were emitted as separate beads. Flag for author review.
4. Check for "over-split smell": multi-step protocols of 2 steps, or step beads whose descriptions could be sub-bullets in their parent.

**Output.** A markdown file `decomposition-r1.md` containing:
- Per-bead findings, with the bead ID and the specific concern.
- Severity: BLOCKER (description doesn't match spec — implementation would diverge), MAJOR (coalesce/split decision is wrong), MINOR (cosmetic / preference).
- Lane tag per finding: `local` (this pilot's application error — discipline applied correctly, just got the answer wrong here) or `class` (the discipline rule has a bug or gap — any pilot in this position would hit this), with a one-line justification.

### 3.3 Reference reviewer

**Goal.** Verify every cross-spec edge in the pilot traces to an actual inline cite in the source spec, and no inline cite was missed.

**Inputs.**
- The source spec.
- The pilot draft.
- All other normative specs in `specs/` (for verifying cross-spec target validity and `depends-on` membership).
- The discipline (§2.7 cross-spec edge rule, §3 cross-spec edge convention).

**Method.**
1. Walk the source spec body top to bottom. For each cross-spec inline cite (`[<other-spec>.md §N]` or `<OTHER-PREFIX>-NNN`), record:
   - Citing requirement ID
   - Target spec
   - Target identifier (if a specific req ID; else section anchor)
   - Whether it's in normative prose (§4 / §5 / §6 / §8) or in informative blocks (`> RATIONALE:`, `> INFORMATIVE:`, §A appendix, §11 OQs, §12 revision history).
2. Walk the pilot's §2 / §3 / §4 / §6 tables. For each `cross:` edge, record citing bead ID and target.
3. Cross-check:
   - For each normative-prose cite from step 1: verify the corresponding pilot edge exists. Missing → "missed edge."
   - For each pilot edge from step 2: verify the corresponding inline cite exists in normative prose. No cite → "invented edge" (per discipline §2.7 forbidden rule).
   - For each pilot edge target: verify the target spec appears in the source spec's `depends-on` front matter. If not, flag as "depends-on violation" (either the edge is wrong, or `depends-on` needs a patch).
4. For section-anchor cites: check whether the pilot tagged the citing bead `cite:wide-fanout` per discipline §3.1.3.
5. Check for bidirectional cite cycles per discipline §2.7's emphasis: if A inline-cites B in normative prose AND B inline-cites A in normative prose, surface for resolution (one is likely informational and should be reclassified).

**Output.** A markdown file `references-r1.md` containing:
- Missed edges, by source-spec cite.
- Invented edges, by pilot row.
- Depends-on violations.
- Bidirectional-cite cycles requiring author resolution.
- Severity: BLOCKER (invented edge — pilot lies about a relationship), MAJOR (missed edge — pilot omits a real dependency), MINOR (missing `cite:wide-fanout` tag on a section-anchor edge).
- Lane tag per finding: `local` (this pilot's application error — discipline applied correctly, just got the answer wrong here) or `class` (the discipline rule has a bug or gap — any pilot in this position would hit this), with a one-line justification.

## 4. Synthesis

Once all three reviewers finish, the discipline author (the agent running the pass, not a fourth subagent) reads the three output files and produces a `synthesis.md` next to them.

### 4.1 Triage — pilot lane vs discipline lane

For every BLOCKER and MAJOR finding (MINOR findings skip triage; they are cosmetic), apply four probes:

1. **Generality.** Could ≥2 of the remaining specs (the 8 not yet drafted) reasonably hit this same finding? (E.g., "no rule for config-only declarations" — yes, multiple specs have those. "Pilot row 12's title is wrong" — no, AR-specific.)
2. **Rule-vs-application.** Did the pilot follow `discipline.md` correctly and STILL produce a bad output? If yes → the discipline rule is wrong or incomplete. If the pilot misapplied an existing rule → application error.
3. **Silence.** Is the discipline simply mute on the kind of content this finding is about? A gap in coverage is a class finding even if no rule was misapplied.
4. **Reviewer self-classification.** Each reviewer tags every finding `local` or `class` per §3 output rules. Any `class` tag from any reviewer routes the finding to the discipline lane unless explicitly overruled in synthesis.md with a stated reason.

If any of probes 1–4 answers "yes" / "discipline" / "class" → the finding is in the **discipline-patch lane**. Otherwise → **pilot-patch lane**.

The bias is toward over-flagging. A false `class` tag costs ~15 minutes of unnecessary discipline patching; a false `local` tag propagates the bug across the remaining pilots.

### 4.2 Lane handling

**Pilot-patch lane.**
- BLOCKER findings: must be fixed before the pilot loads. Patch the pilot, bump version (e.g., `v0.1.0 → v0.1.1`), record in revision history what was applied.
- MAJOR findings: strongly suggest fixing. If the author disagrees, document the disagreement in the pilot's revision-history row.
- MINOR findings: at author's discretion; track in revision history if applied.
- If patches change bead structure (new coalesce, new split, new edges), re-run the relevant reviewers per §2's "after pilot patch" rule.

**Discipline-patch lane.**
- BLOCKER or MAJOR class findings: patch `discipline.md` v0.4 → v0.5 (or the next version) BEFORE the affected pilot loads AND before any other pilot is started. Update the discipline's §6 revision history with the finding tag and a summary of the rule change.
- The affected pilot is RE-DRAFTED against the new discipline version; do not patch the pilot in place against v0.4. (Rationale: a pilot pinned to a superseded discipline version is dead — the gate at §5 will reject it.)
- Other in-flight pilots wait until the discipline patch lands. The whole point of this lane is to prevent bug propagation; pipelining bad pilots defeats it.
- MINOR class findings: at author's discretion. May be batched into the next discipline patch rather than triggering one.

`synthesis.md` records each finding's lane assignment, the probe(s) that put it there, and the action taken (patch applied, disagreement documented, etc.).

## 5. Load gate

After review patches land, the pilot is loaded into the project-local `.beads/` workspace per discipline §2.10 + §2.12 (single DB, prefix `hk`). The load is the final structural gate:

- **Pilot's discipline version MUST match current.** If review surfaced a class finding that bumped the discipline to a new version, the pilot MUST be re-drafted against the new version before loading; the prior-version draft is dead. (Rationale: a class finding propagates if the gate accepts an unrevised pilot.)
- **`br dep cycles` MUST return clean.** Any cycle is either a discipline bug or a pilot bug; resolve before declaring the pilot loaded.
- **All `br create` calls MUST succeed.** Failures (bad enum value, missing parent, etc.) indicate a pilot bug.
- **`br ready` MUST return zero issues for the just-loaded spec** (every bead enters `draft`; readiness workflow promotes later).

If the load surfaces a structural bug not caught by the reviewers, that's a signal to update the reviewer prompts in §3 (add the missed check); record in this protocol's revision history.

## 6. Subagent spawning

The three reviewers spawn in a single message via three parallel `Task` tool calls. Each call uses `subagent_type: general-purpose` (or `Explore` for reviewers that need read-only spec walking — Coverage and Reference are good candidates for Explore).

Each prompt includes:
- The goal (one line, copied from §3.X above).
- The exact source-spec path, pilot path, and discipline path.
- The output format (write to `docs/reviews/<date>-<spec-id>-pilot-r1/<reviewer-name>.md`).
- "Self-contained — you have no context from prior conversation."

A worked invocation (for AR's coverage reviewer) looks like:

    Task tool, subagent_type=Explore, description="Coverage review of AR pilot",
    prompt: """
    You are the coverage reviewer for the AR pilot draft. Your goal is to
    verify every requirement, invariant, and schema in the source spec is
    accounted for in the pilot.

    Read these three files:
    - specs/architecture.md (the source spec)
    - docs/decompose-to-tasks/ar-pilot.md (the pilot draft)
    - docs/decompose-to-tasks/discipline.md (the rules)
    - docs/decompose-to-tasks/pilot-review-protocol.md §3.1 (your full method)

    Follow the method in §3.1. Write output to
    docs/reviews/2026-MM-DD-ar-pilot-r1/coverage-r1.md per the format in §3.1.
    """

The Decomposition-quality and Reference reviewers follow the same shape with their own goal lines and method references.

## 7. Limitations

This protocol does not cover:

- **Discipline-rule changes.** If a reviewer finds a bug class the discipline doesn't address, that's a discipline patch, not a pilot patch. Surface separately.
- **Implementation-correctness.** Reviewers check that the pilot is faithful to the spec, not that the spec itself is correct. Spec correctness is the spec-review process (`docs/reviews/<date>-<spec>-r1/` and `-r2/`).
- **Cross-pilot cycle detection.** The `br dep cycles` check at load is per-DB; with all 10 specs loaded into one workspace, the check naturally covers cross-spec cycles. Per-pilot, only intra-spec cycles surface.
- **The "real" runtime validation.** When harmonik enters implementation, the task-ingestion subsystem will do equivalent checks at runtime against a structured ingestion format (likely YAML/JSON, not markdown). This protocol is the paper analog used during the pre-code decompose-to-tasks pass.

## 8. Revision history

| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-04-27 | 0.2 | foundation-author | Added finding-triage rules in advance of running the protocol on AR (the first non-BI spec). §3.1/3.2/3.3 reviewer outputs now tag each finding `local` or `class` with justification. §4 Synthesis split into §4.1 Triage (four probes — generality, rule-vs-application, silence, reviewer self-classification) and §4.2 Lane handling (pilot-patch vs discipline-patch). §5 Load gate gains a version-match clause: a pilot pinned to a superseded discipline version must be re-drafted before loading. Rationale: a class finding propagates if the gate accepts an unrevised pilot; bias toward over-flagging in early pilots when the discipline is least exercised. F-pilot-protocol-1 (lane discrimination). |
| 2026-04-27 | 0.1 | foundation-author | Initial protocol drafted in response to BI smoke-load findings showing that 5 cycle-bugs were caught by Beads at load but 2 non-structural bugs (tally arithmetic, stale spec reference) needed re-reading by hand. Protocol formalises a 3-reviewer parallel pass (coverage / decomposition-quality / reference) plus the load gate. Designed for the remaining 9 spec pilots (AR, EM, EV, HC, CP, WM, PL, ON, RC). |
