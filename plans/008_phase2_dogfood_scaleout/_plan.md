# Plan 008: Phase-2 dogfood scale-out

## Objective
Make harmonik-as-orchestrator-dispatcher *routine* rather than exceptional. Phase 1 (operational smoke GREEN, 2026-05-14) is achieved. Phase 2 (orchestrator dispatches via harmonik, not via sub-agents) has been exercised twice — once cleanly (`hk-iuaed.6`, code at `dcd7f7e`), once with friction (`hk-cd92e`, where the daemon claimed the wrong bead despite a priority bump). This plan tracks the remaining blockers that have to fall before Phase-2 dispatch can be used by default.

## Status
active

## What's done (May 14–18 fixes)
- `hk-yjsk8` (P1) — daemon `br close` retry on `Unavailable`, so a transient `br` outage after `claude` succeeds no longer leaves the bead stuck IN_PROGRESS. Commit `fb809b0`. *(Note: bead still shows OPEN in `br list`; needs status flip on the bead itself.)*
- `hk-jvzc2` (P1) — daemon no longer silently mutates the parent repo's `.gitignore` + `.claude/settings.json` per-run; those entries are expected to be committed once at install/init. Commit `1c5f525`. *(Note: bead still shows OPEN in `br list`; needs status flip.)*
- `hk-gi471` (closed) — queue subsystem wired into the daemon composition root. Commit `9925ce7` (close-out `9b89471`). This is the prerequisite that unblocks plan 006 / `harmonik run`.

## What's remaining

P1 — must land for routine Phase-2 dispatch:

- `hk-icecw` — `harmonik run <bead-id>` subcommand for single-bead invocation. **Covered by plan 006; do not duplicate here.** This is the highest-leverage open item: it removes the "priority-bump-and-pray" anti-pattern that produced the `hk-cd92e` friction.
- `hk-rp48p` — daemon claim-path ignores priority order; claimed a P1 stale-IN_PROGRESS bead instead of the P0 ready bead during dogfood #2. Two sub-causes possible: (a) bead-selection doesn't sort by priority, or (b) IN_PROGRESS beads are wrongly treated as claimable. **Likely subsumed by `hk-icecw`** — once `harmonik run <id>` lands, the orchestrator stops relying on priority-steer at all. Re-evaluate after `hk-icecw` merges; close as resolved-by-design if subsumed, else fix independently.
- `hk-sc3o4` — orphan-sweep telemetry shows `stale_intents_observed=4` but `bead_in_progress_reset=0`. The PL-006 sweep landed at `9779f72` but either its predicate is too narrow or a timing threshold isn't tripped. Options: widen the predicate, or document the gap and rely on the `harmonik reconcile` subcommand from `hk-lgtq2`. Cross-ref `hk-lgtq2`.
- `hk-lgtq2` (currently P2 in `br`, treated as P1 for this plan's purposes per context) — Cat 3a auto-reconciler. Bead IN_PROGRESS but implementation already landed N commits ago. Minimum: `harmonik reconcile` subcommand running the 6-category table on demand. Stretch: invoke on daemon startup.

P2 — quality-of-life, not blocking routine dispatch:

- `hk-44w19` — SIGTERM to the daemon doesn't propagate to child `claude` / tmux windows; orphaned tmux requires manual kill. Fix: process-group / pgid handling.
- `hk-ajchp` — daemon should idle/exit after the target bead completes; currently cascades to next `br ready`. **Subsumed by `hk-icecw`** (one-shot queue semantics built into `harmonik run`). Tracked under plan 006.

## Related plans
- **Plan 006** — `harmonik run <bead-id>` subcommand (`hk-icecw`, `hk-ajchp`). The single largest unlock for this plan. Land first.
- **Plan 007** — handler pause-and-resume. Directory exists but `_plan.md` not yet written; reserved for the handler-pause work surfaced during dogfood. Out of scope here.

## Next steps
1. **Land plan 006 first** (`hk-icecw` → `harmonik run <bead-id>` + `hk-ajchp` exit-on-completion). Most of plan 008 either subsumes or becomes easier to reason about once `run` exists.
2. **After 006 lands, re-triage `hk-rp48p`.** If `harmonik run <id>` removes the orchestrator's reliance on claim-path priority ordering, close `hk-rp48p` as subsumed; otherwise fix the claim-path bug.
3. **In parallel, dispatch two implementers** (no inter-dependency, no shared files expected):
   - One for `hk-sc3o4` — investigate the orphan-sweep predicate at `9779f72`; either widen it or hand off to the reconciler from `hk-lgtq2`.
   - One for `hk-lgtq2` — ship `harmonik reconcile` subcommand wired to the 6-category table.
4. **Defer P2s** (`hk-44w19`) until P1 set is closed; they don't gate routine Phase-2 dispatch.
5. **Hygiene:** flip `hk-yjsk8` and `hk-jvzc2` to closed in `br` — code landed but status didn't advance.

## Open questions
- **Which P1 beads are actually subsumed by `hk-icecw`?** Best current guess: `hk-rp48p` (claim-path priority is moot if the orchestrator names the bead directly) and `hk-ajchp` (one-shot semantics are inherent to `run`). `hk-sc3o4` and `hk-lgtq2` are *not* subsumed — they cover reconciliation of beads stuck from prior crashed runs, which `harmonik run` does not address. Confirm post-implementation.
- **Scope of `harmonik reconcile`:** on-demand subcommand only, or also daemon-startup hook? The bead description says minimum on-demand, stretch on-startup. Decide during implementation, not in plan.

## References
- Memory: `project_harmonik_north_star` (3-phase roadmap), `project_harmonik_operational_milestone` (Phase-1 GREEN 2026-05-14), `project_harmonik_reconciliation` (6-category rule table).
- Commits: `fb809b0` (`hk-yjsk8`), `1c5f525` (`hk-jvzc2`), `9925ce7` + `9b89471` (`hk-gi471`), `dcd7f7e` (dogfood #1 of `hk-iuaed.6`), `cccde42` (dogfood #2 of `hk-cd92e`), `9779f72` (orphan-sweep landing for `hk-iuaed.4`).
- Bead label: `phase2-dogfood-friction` — `br list --label=phase2-dogfood-friction --status=open` for the live set.
