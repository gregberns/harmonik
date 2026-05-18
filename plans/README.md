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
