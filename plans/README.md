# plans/

One folder per plan. Each folder contains `_plan.md` (the plan itself) and may also contain `beads.md`, `source/` (supporting material — research, drafts, reviews migrated from kerf bench), and other artifacts.

`_plan.md` format:

```
# Plan NNN: <name>

## Objective
<one sentence — what this plan accomplishes>

## Status
active | mostly-done | superseded | not-started | research-phase

## Done means...
Explicit observable conditions that define completion. NOT "the beads shipped" — actual
behavioral acceptance criteria an agent or operator can verify without reading chat context.
Format: numbered list, each item names: the observable state, the mechanism (test name,
CLI command, or JSONL record) that confirms it, and which bead closes it.

Example:
1. <observable state>. Verified by <test or command>.
2. <observable state>. Verified by <test or command>.
...
N. Smoke test GREEN: describe the end-to-end scenario that must pass before this plan closes.

This section is REQUIRED for new plans. It guards against agents declaring a plan "done"
when only a subset (a "Phase-1 scope" or "bootstrap slice") has shipped.

Each plan MUST list at least one **scenario-test bead** covering the end-to-end workflow
(using a twin or real-claude substrate), and at least one **exploratory-test bead** for
the operator-facing surface (CLI or API a human or submitter agent would actually invoke).
These beads belong in "What's remaining" until closed, and in the "Done means..." numbered
list as explicit acceptance items.

**Motivating example — hk-37zy8 (handler-pause policy goroutine):** The policy goroutine
that triggers a pause on handler-fatal outcomes was unit-tested and reviewer-APPROVED, but
was never wired into the composition root. A twin-based scenario test — verifying that
`harmonik run` dispatching a bead that returns `handler_fatal` actually emits
`handler_paused` and writes `.harmonik/handler-state.json` — would have caught the gap
at PR time instead of requiring a separate fixup bead (hk-c8k4c). For the canonical
inventory of all scenario-test gaps found in the 2026-05-18 audit, see
[`docs/scenario-test-gap-audit-2026-05-18.md`](../docs/scenario-test-gap-audit-2026-05-18.md).

## What's done
- bullet, with code SHA or bead ID where relevant

## What's remaining
- bullet, with bead ID where relevant

## References
- code: file:line or commit SHA
- specs: paths
- beads: ids (label-search hint where useful)
- docs: paths
- chat-context: brief summary of how this plan came to be

## Next steps
- 1-3 concrete next actions

## Open questions
- bullets only where a real decision is pending
```

Plans are not ordered. Numbering is just for stable folder names. See individual `_plan.md` files for current status.
