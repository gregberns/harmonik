# Workflow Modes — Kerf Session Log

**Codename:** workflow-modes
**Project:** gregberns-harmonik
**Jig:** spec (v1)
**Driven through:** problem-space → decompose → research → change-design → spec-draft → integration → tasks → ready
**Status at end of session:** ready

## What this kerf proposes

A first-class three-mode dispatch knob on the harmonik daemon (`single`, `review-loop`, `dot`), with `review-loop` as the first non-trivial mode. Review-loop runs implementer→reviewer→(loop|close) up to 3 iterations with session-resume on the implementer, fresh reviewer each round, and a JSON verdict file (`.harmonik/review.json`) conforming to the existing agent-reviewer schema. `dot` is mode #3 placeholder for the later workflow-graph kerf.

## Decisions made in this kerf

- **Three modes:** `single`, `review-loop`, `dot`. Spec name is `review-loop` (NOT `ralph` — that name in current usage means Huntley's context-reset pattern, which is the opposite of what we're building).
- **Implementer session-resume across iterations** (Claude Code's `--resume <session-id>`). Reviewer is a fresh subprocess each iteration. User accepted the sycophancy/mode-collapse risk because exercising the mechanism is part of the goal.
- **Iteration cap = 3, hardcoded for v1.** Cap-hit closes with `needs-attention` label.
- **Beads encoding:** `workflow:<mode>` label prefix. Matches existing harmonik label conventions.
- **Reviewer verdict:** reuses existing `agent-reviewer` JSON schema v1 (APPROVE / REQUEST_CHANGES / BLOCK).
- **One `run_id` per cycle**, multiple `session_id`s under it.
- **No new spec files** — seven existing specs amended additively.
- **Mode-resolution precedence:** per-bead label → project config → daemon default → built-in `single`.

## Specs amended

handler-contract.md, execution-model.md, event-model.md, process-lifecycle.md, beads-integration.md, workspace-model.md, operator-nfr.md.

## What ships next

`07-tasks.md` enumerates 32 implementation tasks. Sequential dependency on:
- The four parallelism-prep beads already filed (`hk-cdb9f`, `hk-fx6zl`, `hk-7s9z9`, `hk-5zode`) for the state-cleanup foundation.

After tasks are filed as beads, the daemon's work loop can be extended to dispatch review-loop mode.
