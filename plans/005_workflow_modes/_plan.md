# Plan 005: workflow-modes

## Objective
Introduce a first-class three-mode dispatch knob on the harmonik daemon (`single` / `review-loop` / `dot`), with `review-loop` (implementer + reviewer with capped iteration + session-resume) as the first non-trivial mode and `dot` as a placeholder for the later workflow-graph kerf.

## Status
mostly-done

Spec deltas, core types, event types, daemon driver, bridge wiring, and a GREEN smoke have all landed. Remaining surface is a real-Claude end-to-end test (hk-7uasg) and the workspace settings.json materialization gap (hk-02sp0). The kerf itself reached `ready` (jig progressed all the way through problem-space → decompose → research → change-design → spec-draft → integration → tasks).

## What's done
- Kerf finalized through all seven passes; sources in `source/` (problem-space, components, research/, design/, spec-drafts/, integration, tasks).
- Spec deltas landed across 7 specs (handler-contract, execution-model, event-model, process-lifecycle, beads-integration, workspace-model, operator-nfr) — `caf4b57 feat(specs): workflow-modes — three dispatch modes (single/review-loop/dot)`.
- T-WM-004 seven event types: `f7d75eb feat(core): define seven new event types for workflow-modes`.
- T-WM-009 mode-resolution precedence in claim path: `dbc34ee feat(daemon): T-WM-009 implement workflow-mode resolution precedence in claim path`.
- Daemon review-loop driver + per-phase bridge wiring: `22d8459 feat(daemon): wire review-loop per-phase to bridge (hk-gql20.15)`.
- Tmux WindowName per WM-002a: `6f1efe0`.
- Bridge-integration epic hk-lj1p9 closed (`10d4bf5`); Wave 1 + Wave 2 sub-beads closed (`c2f78c7`, `f1d24e7`); reviewer-input-artifact + reviewer-feedback-delivery spec rules added (`9c8a68c`, `2792058`).
- Review-loop GREEN smoke take 2: `a8b6568 smoke(review-loop): hk-gql20 — GREEN take 2, bridge-integration epic closed`.

## What's remaining
- `hk-7uasg` — real-Claude end-to-end review-loop integration test (P1, open).
- `hk-02sp0` — Workspace `.claude/settings.json` materialization (WM-040a) (P2, open).
- Verify all T-WM-001..T-WM-032 tasks have either a landing commit or a tracking bead; some XS items (T-WM-031 cite-fix, T-WM-032 reconciliation scope-out note) may have folded into other commits.

## References
- source: `plans/005_workflow_modes/source/` (full kerf bench: 01-problem-space.md, 02-components.md, 03-research/, 04-design/, 05-changelog.md, 05-spec-drafts/, 06-integration.md, 07-tasks.md, SESSION.md, spec.yaml).
- specs: `specs/handler-contract.md`, `specs/execution-model.md`, `specs/event-model.md`, `specs/process-lifecycle.md`, `specs/beads-integration.md`, `specs/workspace-model.md`, `specs/operator-nfr.md` (HC-003a, HC-004, HC-006; EM-012/012a, EM-015d/015e; §8.1a + §8.8.6; PL-004a; BI-009a/010c/013a; WM-013e/027a; ON-004a/009a/013d/035a).
- code: `internal/daemon/reviewloop.go`, `internal/daemon/workloop.go` (claim path), `internal/core/eventreg_*` (review-loop events), `internal/workspace/` (verdict reader + diff-hash + sidecar).
- commits (selection): `caf4b57`, `f7d75eb`, `dbc34ee`, `22d8459`, `6f1efe0`, `a8b6568`, `10d4bf5`, `9c8a68c`, `2792058`.
- beads (open): `hk-7uasg` (real-Claude E2E), `hk-02sp0` (settings.json materialization).
- epics (closed): `hk-lj1p9` (bridge-integration umbrella), `hk-gql20` (smoke-bring-up tree).
- chat-context: workflow-modes kerf ran 2026-05-12 → 2026-05-15; the implementation work folded into the Phase-1 operational push and the bridge-integration epic during v38–v45 handoffs. Plan is migrated here so the planning artifacts live with the repo rather than only on the kerf bench.

## Next steps
- Land `hk-7uasg` — real-Claude E2E review-loop test, gating Phase-2 confidence.
- Land `hk-02sp0` — settings.json materialization (WM-040a), to remove the per-run parent-repo mutation pattern.
- Cross-check 07-tasks.md T-WM-001..T-WM-032 against git history to identify any tasks still genuinely open (vs. folded silently); file beads for any gaps.

## Open questions
- None pending user decision. The remaining work is task-level execution.
