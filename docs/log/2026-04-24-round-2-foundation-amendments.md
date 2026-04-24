---
title: "Round-2 Foundation Amendments (Phase 3 findings applied)"
type: log-entry
date: 2026-04-24
participants: [human, claude-opus, delta-plan-subagent, amendment-subagent]
---

# 2026-04-24: Round-2 Foundation Amendments

## Context

Phase 3 of the kerf `harmonik-foundation` work had concluded the prior day (2026-04-23) with six reviewer personas producing 24+ findings clustered into six buckets. Architectural direction was affirmed; the findings were detail gaps. This session executed the round-2 amendment pass to close those gaps before advancing kerf to `change-design`.

Work location: `/Users/gb/.kerf/projects/gregberns-harmonik/harmonik-foundation/` (kerf artifacts live outside the repo until `kerf finalize`).

## Approach

Three-stage pattern, each stage a subagent with a self-contained brief:

1. **Delta-plan subagent.** Produced `round2-delta-plan.md` (~1,180 lines, 36 findings). Each finding includes the target file + section, operation (insert/replace/append), proposed text verbatim, cross-references to update, and dependencies. Main-agent pre-resolved two integration decisions in the brief: (a) taxonomy question — keep 6 detection categories + add an explicit §9.2a action-mapping layer; (b) recursive-reconciliation bound — prefer Option B (verdict-only commit, no intermediate checkpoints).
2. **Main-agent review.** Verified structure (36 findings, application order with dependencies, final verification checklist); spot-checked high-risk entries (F-C1-1 transition storage, F-C1-5 recursive reconciliation, F-C3-9 action-mapping, F-C2-3/4 investigator contract, F-C4-4 goroutine ownership, F-C6-1 no-DTW); confirmed the F-C1-5 Option B pick.
3. **Amendment subagent.** Applied all 36 findings per the application order (Phase A–F). Batch-added event-type entries to §3.2 after individual reconciliation edits to keep §3.2 edited once. Verified against the final checklist.

Net impact on the foundation spec corpus:
- `02-components.md` grew 924 → 1,163 lines (+239).
- `01-problem-space.md` grew 367 → 377 lines (+10) — the only change outside the component doc was expanding locked decision #12 with explicit applicability conditions.

## Findings applied (36 total)

**Cluster 1 — Architectural gaps (6).** Sibling-file transition storage at `.harmonik/transitions/<transition_id>.json` (§2.1b); subsystem term pinned to "Go package inside daemon for MVH" (§1.4a); JSONL divergence-evidence permitted as third read class (§3.6 (c), §9.3a); node idempotency tag (`idempotent` / `non-idempotent` / `recoverable-non-idempotent`, §2.1c); recursive-reconciliation bound via verdict-only commit (§9.1a); orphan sweep on startup step 1a (§8.2).

**Cluster 2 — Investigator contract hardening (4).** Mandatory wall-clock budget per reconciliation workflow (§9.4a); verdict schema + one-verdict rule (§9.5a); durable verdict execution via `reconciliation_verdict_executed` event + `Harmonik-Verdict-Executed` trailer (§9.5b); snapshot-token binding on investigator inputs (§9.4b) with daemon-side staleness check.

**Cluster 3 — Taxonomy additions (9).** Cat 0 (infrastructure unavailable); Cat 3a (torn Beads write); Cat 3b (verdict-unexecuted); Cat 3c (inverse premature-close); Cat 6a/6b split (LLM-triageable vs mechanically unrecoverable); structurally-broken-worktree detector; run-scoped detector invariant; DAG-parentage-only ordering; §9.2a action-mapping layer (the integrator finding that ties everything together).

**Cluster 4 — Subsystem authoring clarity (5).** ControlPoint registry owned by S02, Hook dispatch by S05, Gate/Guard invocation by S01 (§6.1b); session-log pipeline split S04→S06→S08 (§5.3a); `agent_type` as URN-like stable string (§1.6a); goroutine ownership split daemon-watcher vs S04-adapter (§4.3); reconciliation workflow library owned by S01 (§9.1b).

**Cluster 5 — Operational obligations named (6).** Startup failure-mode catalog, `harmonik upgrade` contract, silent-hang detection, multi-daemon commands, reconciliation operator override, exit-code + config inventory — each named as a spec-draft deliverable; full enumeration deferred to spec-draft.

**Cluster 6 — Acknowledgments (6).** No-DTW applicability conditions (single-machine, cheap re-execution, no irreversible external actions, bounded waits) — `01-problem-space.md` locked decision #12 expanded; multi-tenancy deferral acknowledged not dismissed; centralized-controller graceful-degradation trade-off acknowledged; DOT untyped validator obligation named; WIP-loss mitigation on `reopen-bead` required; taxonomy audit pointer to QUESTIONS.md.

Event taxonomy in §3.2 grew by 7: `reconciliation_verdict_executed`, `reconciliation_verdict_malformed`, `reconciliation_budget_exhausted`, `reconciliation_verdict_stale`, `infrastructure_unavailable`, `daemon_orphan_sweep_completed`, `operator_escalation_required`.

## Key decisions made by main-agent this session

1. **Taxonomy shape (Q-P3-1).** Keep 6 detection categories; add explicit §9.2a action-mapping layer; Skeptic's 3-action-outer-taxonomy question is preserved in `QUESTIONS.md` for optional post-spec-draft audit. Rationale: the 6 categories represent heterogeneous detectors (node-type lookup vs store pairwise comparison vs file integrity check); collapsing to 3 loses the detector taxonomy or pushes it into sub-classification. The Skeptic's real grievance — that the doc hides the action layer — is fixed by making §9.2a explicit. Crash-adversary sub-detectors (Cat 0, 3a, 3b, 6a, 6b) slot in cleanly under (b); they would sit awkwardly under a 3-action outer shape.
2. **Recursive reconciliation (F-C1-5).** Option B: reconciliation workflows emit exactly one verdict commit and no intermediate checkpoints. Crash mid-investigation means the outer run's original category re-triggers reconciliation; no Cat-of-a-Cat classification question arises. Rationale: reconciliation workflows are short, budget-bounded; not-checkpointing avoids the recursion question entirely.
3. **Transition-record storage (F-C1-1).** Sibling JSON file at `.harmonik/transitions/<transition_id>.json` committed as part of the checkpoint tree. Trailers remain as a cheap index; the sibling file carries the full AlphaGo record. Rationale: discoverable by deterministic path, travels with clones by default, composes with the existing `git show <commit>:path` read path; git notes would be detached from the tree and lost on clone without explicit `refs/notes/*` fetch config.

## Affirmations preserved across round-2

Six reviewer affirmations did not receive structural edits this session because they held up; each was re-affirmed implicitly by round-2 preserving the decision: three-artifact separation (§1.9), handler-as-modularity-boundary (§4.12), reconciliation-as-workflow (§9.1 principle, even with §9.1a cadence exception), three-store cross-reference (§2.6), daemon-vs-orchestrator-agent distinction (§8.6), centralized-controller principle (§1.8, with added graceful-degradation tradeoff acknowledgment per Cluster 6).

## Questions surfaced for user

- **Q-P3-1 (taxonomy shape).** Recorded in `QUESTIONS.md`. Main-agent committed to 6-categories-with-action-mapping; user confirmation requested. Not blocking — a later restructure would be an additive re-amendment.

## Follow-ups

- Advance kerf to `change-design` (user call).
- Consider periodic snapshotting of `02-components.md` into the repo — the live 1,163-line artifact lives at `/Users/gb/.kerf/projects/` outside version control until `kerf finalize`. Options flagged in `TASKS.md`.
- Round-3 re-review is optional and not recommended by main-agent; the amendments address every Phase 3 finding directly without introducing materially new surface.

## Process notes

- **The plan-first pattern worked.** Producing the delta plan as a separate artifact (reviewed before execution) made the 36-finding amendment pass tractable in one shot; the amendment subagent reported no snags and no deviations from proposed text.
- **Self-contained subagent briefs matter.** Both the delta-plan and amendment briefs pre-resolved the integration questions; subagents didn't need to re-derive "which option did the main agent pick?"
- **Methodology gap:** prior sessions skipped log entries; this session's handoff includes one. The logging discipline is worth maintaining — future agents tracing decision history have strictly less context without the log.
