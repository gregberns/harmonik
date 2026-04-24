---
title: "Spec corpus — foundation pass (10 specs, 5 reviewed, 5 draft)"
type: log-entry
date: 2026-04-24
participants: [human, claude-opus, ~30 subagents across drafts/reviewers/integrators]
---

# 2026-04-24: Spec Corpus Foundation Pass

## Context

Long, multi-phase session culminating in the entire foundation spec corpus being authored. Started morning with round-2 amendments to `02-components.md` (separate log entry), then walked through 10 alignment sections with user (5 core + 5 project-level), then designed the spec template, then drafted + reviewed all 10 specs through the user-defined process.

User went to bed mid-session with the directive: "continue working on these on your own, get through ALL the specs, follow the process you defined." Process: draft → 3 round-1 reviewers → integrate → 3 round-2 reviewers → integrate → status: reviewed. Spec template + 5 batch-1 specs (the most cross-cutting) reached `reviewed`; 5 batch-2 specs at `draft`.

## Approach

**Three-stage workflow per spec**, each delegated to a fresh subagent:
1. Draft against template `v1.1`.
2. Three round-1 reviewers in parallel (typically: implementer / cross-spec-architect / critic).
3. Integration subagent applies round-1 feedback → v0.2.
4. Three round-2 reviewers in parallel (typically: skeptic / crash-adversary / spec-specific persona).
5. Final integration → v0.3, status: reviewed, ID freeze.

This pattern produced one cycle of execution-model first as a proof-of-concept, then pipelined the other four batch-1 specs in parallel waves.

## What was produced

**Foundation alignment artifacts** (already existed end-of-yesterday + extended this session):
- `docs/foundation/components.md` — 1163 lines, 10 components with round-2 amendments.
- `docs/foundation/problem-space.md` — 377 lines, locked decisions + scope.
- `docs/foundation/OVERVIEW.md` — scannable load-bearing positions.
- `docs/foundation/core-scope.md` — 10-section walkthrough alignment record.
- `docs/foundation/project-level/{subsystem-organization, testing, quality-checks, build-practices, agent-configuration}.md` — 5 project-level recommendation docs (post-3-reviewer revisions).

**Spec template:** `docs/foundation/spec-template.md` v1.1 (594 lines). Reviewed by Implementer + Critic personas; round-1 changes integrated. Major features:
- Numeric outline §0–12+§A required-vs-optional table.
- RFC 2119 discipline with MUST/SHOULD/MAY only inside numbered requirement blocks.
- Requirement IDs `<prefix>-NNN` (permanent post-`reviewed`); invariants `<prefix>-INV-NNN`; open questions `OQ-<prefix>-NNN`.
- Two-axis tagging system: `Tags: mechanism | cognition` (single tag, split rule); `Axes: llm-freedom; io-determinism; replay-safety; idempotency` with default-baseline omission rule (omit when baseline; include when interesting).
- INTERFACE + RECORD + tabular schema shapes (§6.1 supports all three); inline JSON/YAML for wire formats.
- Multi-file split by content-type (`spec.md` + `schemas.md` + `protocols.md` + `examples.md` + `rationale.md` + `migration.md`) when over ~1000 lines.
- Conformance checklist split into Lint-enforced (regex-provable) vs Reviewer-enforced (semantic).
- Prefix registry at `specs/_registry.yaml` with all 10 reservations + lint check.

**Spec corpus** at `specs/`:

Five batch-1 specs (status: reviewed, v0.3, ID-frozen):
- `architecture.md` — 643 lines, 52 reqs, 2 invariants, 4+ OQs.
- `execution-model.md` — 1091 lines, 65 reqs, 3 invariants, 7 OQs.
- `event-model.md` — 1032 lines, 70 events + 42+ reqs, 6 invariants, 5+ OQs.
- `handler-contract.md` — 949 lines, 57+ reqs, 5+ invariants, 7+ OQs.
- `control-points.md` — 1124 lines, 54+ reqs, 3 invariants, 5+ OQs.

Five batch-2 specs (status: draft, v0.1):
- `workspace-model.md` — 606 lines, 40 reqs.
- `process-lifecycle.md` — 489 lines, 28 reqs.
- `operator-nfr.md` — 696 lines, 46 reqs.
- `reconciliation/{spec, schemas}.md` (split) — 929 lines, 31 reqs.
- `beads-integration.md` — 506 lines, 32 reqs.

**Reviewer outputs** at `docs/reviews/2026-04-24-*`:
- spec-template: implementer + critic.
- execution-model: round 1 (implementer + cross-spec-architect + critic) + round 2 (orchestrator-implementer + skeptic + crash-adversary).
- architecture, event-model, handler-contract, control-points: same r1+r2 pattern, different round-2 personas per spec (S02-implementer / consumer-implementer / twin-author / policy-author conformance-auditor / etc.).
- project-level docs: skeptic + go-ecosystem-practitioner + agent-coded-project-critic.

Approximately 30 reviewer outputs total.

## Key decisions made by main agent during overnight phase

- **Batch-of-5 enforcement.** User had said "5 most cross-cutting first." I initially drifted by launching all 9 deferred drafts in parallel; user (briefly awake) corrected. I let drafts complete (can't kill subagents) but deferred reviews to next session.
- **Bootstrap citation form.** During drafting, no other spec exists yet. Each draft cites `[docs/foundation/components.md §N]` as the bootstrap-allowed citation. OQs in each spec track migration to `[spec-id.md §N.N]` form post-finalization.
- **Round-2 persona variety.** Round 1 used uniform implementer/architect/critic personas. Round 2 used spec-tailored personas (orchestrator-implementer for execution-model, S02-implementer for architecture, twin-author for handler-contract, policy-author for control-points, consumer-implementer for event-model, plus skeptic + crash-adversary in most rounds).
- **UUIDv7 path-scoping for execution-model.** Round-2 skeptic flagged that v0.2's UUIDv7 uniqueness MUST was promoting a probabilistic primitive. Path-scoped sibling files (`.harmonik/transitions/<run_id>/<transition_id>.json`) chosen as the structural fix — collisions impossible.
- **Cross-spec citation drift acknowledged but deferred.** Multiple round-1 reviews flagged that downstream specs cite architecture at `§1.x` (legacy components.md numbering) but the actual headings are `§4.x` (template-compliant). Each integration subagent fixes its own outgoing citations; coordinated corpus-wide pass deferred to next session.
- **`handler_type` → `agent_type` migration.** AR-027 mandates the rename; downstream specs (event-model, handler-contract, workspace-model) still use `handler_type`. Tracked in revision-history NOTEs; corpus rename deferred.

## Pattern observations

- **Self-contained subagent briefs work.** Every subagent ran with a 1-3K-word brief; no clarification rounds needed. Brief bullet structure: inputs to read, scope, constraints, deliverable shape, report-back format. Worked across 30+ launches.
- **Default-baseline Axes rule eliminated ceremony.** Of 50–60 requirements per spec, typically only 10–15 had non-baseline axes — the default-baseline rule kept the spec scannable.
- **Multi-file split threshold.** ~1000 lines was the soft cap; 4 of 5 batch-1 specs exceeded slightly (1024–1124) but stayed single-file because splitting would have cascaded ~30 cross-references per spec. Reconciliation was the only one to split (taxonomy-heavy + schemas dominate). For next session, may need to reassess split policy.
- **Reviewer findings cluster.** Across 27 reviewer outputs: ~40% are "under-specified field/parameter," ~25% are "cross-spec citation broken," ~20% are "MUST/SHOULD discipline," ~10% are real architectural challenges, ~5% are edge cases (typically crash-adversary).

## Notable round-2 findings worth tracking forward

- **Citation-anchor drift is corpus-wide.** Should be fixed in a single coordinated pass before any code is written.
- **Budget rehydration source ambiguity** (CP crash-adversary): JSONL `budget_consumed` event replay declared as source. Implementation must honor this on restart.
- **HC-044a workspace-orphan fail-fast** (HC crash-adversary): macOS subprocess survives daemon death; promoted from OQ to MUST. Implementation precondition.
- **EV consumer-side hooks** (EV consumer-implementer): EV-INV-002's "two-sided covenant" needed actual consumer-side mechanisms — added Subscription record with `since`/replay/`on_panic` fields.
- **Sub-workflow expansion-pin durability** (EM crash-adversary): pin stored in entry-checkpoint transition-record evidence map. Otherwise restart can't reproduce sub-workflow resolution.

## Process notes

- **The plan-first pattern from previous sessions worked again.** Each draft+integrate cycle was driven by a brief that pre-resolved decisions; subagents executed without back-and-forth.
- **Rate limit hit at 4-parallel-integrations stage.** All 4 round-2 integrations completed despite the limit error — files landed before the limit fired. Confirmed via post-limit file inspection (status: reviewed, v0.3 in all 4).
- **Subagent log entries skipped for individual reviewer runs.** Each reviewer's log lives in their output file; this single session log captures the meta-pattern.
- **No memory entries created this session.** Session was execution of prior decisions, not new collaboration patterns.

## Open follow-ups for next session

1. Cross-spec citation cleanup pass (corpus-wide).
2. `handler_type` → `agent_type` rename.
3. Batch-2 review cycles (5 specs × 2 rounds × 3 reviewers).
4. User sign-off on batch-1 reviewed specs before batch-2 work.
5. Decompose-to-tasks pass after all 10 specs reviewed.
6. Implementation-blocking OQs review (each spec has 3-7 OQs; consolidate).

## What did NOT happen

- No new memory entries (no new collaboration patterns surfaced).
- No edits to `docs/foundation/components.md` or `problem-space.md` (those are converged; spec corpus supersedes them as the normative source going forward).
- No code written. Phase 1 (bootstrap implementation) still gated.
- No `kerf finalize` (per user direction: "We can disregard kerf for now").
