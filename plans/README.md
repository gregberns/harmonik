# plans/

One folder per plan. Each folder contains `_plan.md` (the plan itself) and may also contain `beads.md`, `source/` (supporting material — research, drafts, reviews migrated from kerf bench), and other artifacts.

`_plan.md` format:

```
# Plan NNN: <name>

## Objective
<one sentence — what this plan accomplishes>

## Status
active | mostly-done | superseded | not-started | research-phase

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
