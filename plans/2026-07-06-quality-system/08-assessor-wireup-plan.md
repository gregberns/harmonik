# Assessor launcher wire-up — plan (admiral carry-in, before Phase-1's first gate)
*2026-07-06 · investigation verified against cmd/harmonik + internal/crew + specs*

## Headline: basic spawn needs ZERO code change
`.harmonik/agents/assessor/` exists → `crew.ResolveType("assessor")` resolves the bare name to the assessor
type directly (`internal/crew/registry.go`), so `HARMONIK_AGENT=assessor` composes the assessor brief (not a
crew brief). `harmonik agent check assessor` → **ok**; `harmonik agent brief --agent assessor` composes clean.
Manifest files are `soul.md` / `operating.md` / `manifest.yaml` (not `.yaml` for soul/operating).

**Launch-ready command today (one assessor at a time):**
```
harmonik crew start assessor --queue assessor-gate-q --mission <handoff.md>
```
`HARMONIK_AGENT=assessor`, WorkDir=$HARMONIK_PROJECT, keeper auto-armed. `harmonik start` only knows
`captain|crew` (hardcoded switch in `cmd/harmonik/start.go`) — route through `crew start`, not `start assessor`.

## Two REAL gaps to close before the gate is trustworthy (config/spec, not code)

### Gap A — assessor-specific mission-handoff schema
The crew schema (`specs/crew-handoff-schema.md` v1) mandates exactly 6 fields
(`schema_version, crew_name, queue, epic_id, goal, captain_name`) — NO `branch`, NO `gate`. But the assessor's
`operating.md` step 1 parses **`{branch, epic_id, gate}`, gate ∈ merge|deploy** from frontmatter. Reusing the
crew schema leaves the load-bearing fields in unparseable free-text body.
**Action:** author `specs/assessor-handoff-schema.md`. Proposed frontmatter:
```yaml
---
schema_version: 1
assessor_name: assessor-<epic>
epic_id: hk-xxxxx
branch: integration/<epic>          # branch-under-test
gate: merge                         # merge | deploy
commit: <sha>                       # REQUIRED when gate==deploy (GATE-0 target); omit for merge
found_by_sources: [admiral, fast-follow, assessor]
report_path: .harmonik/reports/<epic>-gate.md
spawned_by: admiral
---
```

### Gap B — the block query is fleet-wide, not branch-scoped
Confirmed: `br list --label "found-by:*"` matches NOTHING (`*` is literal — no glob). Known found-by values:
`found-by:admiral`, `found-by:fast-follow`, `found-by:assessor`. Exact deterministic block query:
```
br list --status open --priority 0 --priority 1 \
  --label-any found-by:assessor --label-any found-by:admiral --label-any found-by:fast-follow --json
```
Open P0/P1 → BLOCK; empty → PASS. **But beads have no branch field** → this set is fleet-wide.
**Action:** the assessor must FILE its `found-by:assessor` beads with an added scope label (`--label <epic_id>`)
and the block query must add `--label <epic_id>`. Edit `.harmonik/agents/assessor/operating.md` §Merge-gate
steps 5 (file) + 6 (query). Without this, PASS/BLOCK is not per-branch.

## Optional CODE changes (only if needed later)
- **Concurrent per-epic named instances** (`assessor-hk123`): `crew.ResolveType` only resolves the bare type
  folder name; a custom instance name falls through to `crew.Load` and errors. Needs either a registry `type:`
  override on the crew record, or a thin `harmonik start assessor` role in `start.go`. Not needed if we spawn
  the bare `assessor` one-at-a-time (gates are serial per epic anyway).
- **Ephemeral teardown:** `crew start` writes a `.harmonik/crew/assessor*.json` record + `crew list` entry that
  lingers after the assessor self-terminates post-verdict. Add `crew stop` after verdict, or an ephemeral path
  that skips the registry. Cosmetic, not blocking.

## Admiral per-epic gate runbook (once A+B done)
1. Crew posts `--topic gate` when the epic branch is fully closed → captain verifies + relays to admiral.
2. Admiral writes the handoff (Gap-A schema) → `harmonik crew start assessor --queue assessor-<epic>-q --mission <handoff>`.
3. Admiral subscribes `comms recv --topic gate`; assessor runs LT+XT+CR on an isolated scratch clone
   (`scripts/scratch-daemon.sh`, never cd into a worktree), files findings as scoped `found-by:assessor` beads,
   posts PASS/BLOCK, self-terminates.
4. Admiral holds the single human epic→main PR (merge gate) / deploy decision (GATE-0) until PASS.

## Sequencing
Gaps A + B are the only must-do-before-first-gate items, and both are pure spec/config edits. The first gate
is still far off (Phase-1 kerf passes not yet emitting beads), so there's runway — but do A+B before
`core-loop-proof`'s epic boundary. Fold in the `07-assessor-severity-framework.md` decision matrix at the same
time (it's what turns a raw open-P0/P1 set into the merge|release|redeploy allow/block call).
