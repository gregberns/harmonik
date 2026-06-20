# Reusable Layout — promoting the three-kinds model into the product

> Builds on 07 (the three-kinds model) + 08 (the embed/init mechanism).
> Goal: structure the behavioral contracts and state scaffolding so they ship WITH
> the harmonik binary and `harmonik init` lays them onto ANY project — not
> hand-maintained per repo. Operator directive: "embed these patterns into the
> product so other projects running harmonik do the same; closer to this project
> the better; compact the diverse things into logically reusable locations."

## The insight: the three kinds map onto the EXISTING ship path

harmonik already has exactly one "ship-and-scaffold" mechanism — the embed FS +
`harmonik init`. The compaction is to route every reusable thing onto it, and leave
only per-project content off it.

| Kind | Ships with product? | Mechanism | Status today |
|---|---|---|---|
| **B. Behavioral contracts** (skills + standing-rules) | **YES** | embedded `.claude/skills/*`, scaffolded by init | ships TODAY (8 skills) — standing-rules is the one missing piece |
| **C. Operational-state scaffolding** (tier-file templates w/ headers) | **YES (structure only)** | NEW: embedded `assets/context/*.tmpl`, rendered by init | gap — needs ~4 small init additions (08) |
| **C. Operational-state content** (the filled-in lanes, handoff, roadmap) | NO | each project fills the scaffold | per-project |
| **A. Documentation** (knowledge base, specs, product docs) | NO | project-specific | per-project |

So the entire reusable surface compacts into **the embedded asset tree**
(`cmd/harmonik/assets/`, canonical-mirrored from repo-root), in two groups:
**(1) the skills bundle** and **(2) the context-template bundle**. Everything else
is per-project and stays out.

---

## This forces the open decision: standing-rules = an embedded `orchestrator` skill

From 07's menu (a/b/c), the mechanism decides it. Only **(a) a project-local
`orchestrator` skill** rides the existing ship path — a root `OPERATING-RULES.md`
(b) or a relocated doc (c) would NOT propagate to other projects via init. So:

- **Create `.claude/skills/orchestrator/SKILL.md`** (canonical) + mirror to
  `cmd/harmonik/assets/skills/orchestrator/SKILL.md` + add to the init skill list +
  extend `init_skills_sync_test.go` to guard it.
- It holds the **universal** standing behavioral contract: dispatch discipline,
  priority/kerf-first, bead lifecycle, review gate, monitor pattern, CWD discipline,
  autonomy boundaries, major-issue fan-out trigger. Absorbs the 4 scattered copies
  (AGENTS.md §Daily-loop, AGENT_OPERATING_MANUAL §3-4, HANDOFF directives block,
  PRIORITIES "Standing rules").
- It POINTS to the detail-owner skills (dispatch→harmonik-dispatch, comms→agent-comms,
  …) — it does not duplicate them.
- **Captain STARTUP Step 1.3** changes from `docs/orchestrator-rules.md` → "load the
  orchestrator skill." Now the standing-rules propagate to every harmonik project
  automatically, exactly like captain/crew/keeper.

`docs/orchestrator-rules.md` is then either deleted or left as a thin stub pointing
at the skill (it was the wrong bucket — a loaded contract masquerading as docs).

### The clean seam: universal contract vs project deltas
- **Universal behavioral contract** → the embedded `orchestrator` skill (ships).
- **Project-specific deltas + routing** → `AGENTS.md`, which is *already* rendered
  from an embedded template (08). It becomes the **router**: precedence statement +
  per-role load order + project-specific conventions (specs/ location, codename
  labels, kerf bench paths). The universal rules move OUT of it into the skill.

---

## The context-template bundle (the build needed)

Add a second embedded asset group so the kind-C tier scaffolding ships too:

```
cmd/harmonik/assets/context/
  project.yaml.tmpl        # tier-3: phase, locked_decisions, forbidden_actions (durable only)
  captain-lanes.md.tmpl    # tier-2: lane→crew registry + epics-in-progress + parked + dated operator-directives
  roadmap.md.tmpl          # tier-4: objectives, phases, "we are HERE" marker
  HANDOFF.md.tmpl          # tier-1 seed: empty session-state template
```

Each template carries the **self-describing header** from 07:
```
<!-- TIER: 2 (operational state, days cadence)
     LOADED BY: captain @ STARTUP Step 0b; NOT crews/implementers
     OWNER: captain, updated at session end (before HANDOFF.md)
     DO NOT PUT HERE: standing rules (→ orchestrator skill); this-session salvage (→ HANDOFF.md) -->
```

`harmonik init` additions (per 08, ~4 small changes to `init_cmd.go`):
1. `mkdir .harmonik/context/`
2. render the 4 templates into it (project.yaml, captain-lanes.md, roadmap.md) + seed HANDOFF.md at root
3. add a sync-guard (mirror canonical repo-root copies, like skills)

Result: every harmonik project boots with the full tiered-state scaffolding +
headers already in place — the captain's Step 0a/0b reads never hit a missing file.

---

## End-state: where everything lives, by ship-mechanism

### SHIPPED (embedded; `harmonik init` scaffolds) — the reusable locations
```
cmd/harmonik/assets/                 (canonical = repo-root mirror, sync-guard tested)
  skills/
    captain/{STARTUP.md,SKILL.md}    # boot runbook + per-crew mechanics
    crew-launch/SKILL.md
    keeper/SKILL.md
    orchestrator/SKILL.md            # NEW — universal standing behavioral contract
    harmonik-dispatch/ harmonik-lifecycle/ agent-comms/ beads-cli/
  context/
    project.yaml.tmpl captain-lanes.md.tmpl roadmap.md.tmpl HANDOFF.md.tmpl   # NEW
  agents-md.tmpl                     # AGENTS.md router (already embedded)
  config consts (config.yaml/.gitignore)   # already embedded
scripts/captain-tools/captain-launch.sh    # already embedded
```

### PER-PROJECT (init scaffolds an empty/seed version; project fills it)
```
.harmonik/context/{project.yaml, captain-lanes.md, roadmap.md}   # kind-C content
HANDOFF.md                                                       # kind-C content
docs/  specs/  README & product docs                            # kind-A documentation
```

This compacts the diverse instruction artifacts into **two embeddable bundles
(skills + context-templates) + a router template**, all under `cmd/harmonik/assets/`
with repo-root canonical mirrors. The behavioral layer and the state-scaffolding
become PRODUCT FEATURES; only volatile content and the knowledge base stay per-repo.

---

## Sequenced execution plan

**Track 1 — safe cleanup (no design risk, do in parallel now):** the P0/P1 items
from 00-INDEX — delete dead HANDOFFs + AGENT_COMMS.md, prune 14 dead/stale missions,
gitignore HANDOFF-*.md, remove root binaries + .beads.bak.*, key already rotated.

**Track 2 — the consolidation/promotion (the real work, review-gated):**
1. Author the `orchestrator` skill (universal contract); absorb the 4 copies; strip bead-ID provenance to a footnote.
2. Rewrite AGENTS.md as router (precedence + per-role load order + project deltas); delete duplicated bodies.
3. Author the 4 context templates with headers; canonical repo-root copies + mirrors.
4. Wire init: mkdir context + render templates + sync-guard test (extend `init_skills_sync_test.go`).
5. Update captain STARTUP Step 1.3 (orchestrator skill, not docs/orchestrator-rules.md); affirm crew minimal-load in crew-launch.
6. Migrate THIS repo's live state into the new homes: captain-lanes.md absorbs epics-in-progress + dated directives; project.yaml → durable-only; HANDOFF.md strips directives block + absorbs PRIORITIES; ROADMAP gets "we are HERE".
7. Retire AGENT_OPERATING_MANUAL.md (→ known-workarounds + cli-quickref), TASKS.md (→ docs/historical/), PRIORITIES.md (folded), slim STATUS.md, prune AGENT_INDEX.md.
8. Re-sync embedded skill mirrors; run the sync-guard + init smoke.

Track 2 is a real initiative — worth a kerf work + an epic + beads, executed through
the daemon/crew flow with the review gate, not a single inline edit pass.
