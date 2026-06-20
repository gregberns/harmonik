# Placement Contract — Phase A local consolidation (BINDING for all implementer agents)

> This is the authoritative seam spec. Every Phase-A agent follows it so parallel
> edits stay consistent. Derived from docs 06/07/09. If 06/07/09 and this file
> disagree, THIS file wins. Read this + 07 + 09 before editing.

## Global rules for every agent
- **Filesystem edits only.** Do NOT run `git add`, `git commit`, `git mv`, `git rm`,
  or any index-writing git command (7 other agents are live; the orchestrator commits
  centrally after review). Use plain `mv` / `rm` / `cp` for relocations and report them.
- **Edit only the files in YOUR lane** (below). Never touch another lane's files.
- **No bead-ID rot:** when moving rules, strip inline `hk-xxxxx` provenance into a
  trailing `## Provenance` footnote (or drop it); rule text reads clean.
- **Preserve operator content:** `.harmonik/context/project.yaml` and `captain-lanes.md`
  have UNCOMMITTED operator edits — read current on-disk content and RESTRUCTURE it into
  the right tiers; never blind-overwrite or drop an operator directive.
- Report back: files changed/created/deleted + a 5-line summary of what moved. <150 words.

## The self-describing header (put on every kind-C state file + the orchestrator skill front-matter note)
```
<!-- TIER: <n> (<kind>, <cadence> cadence)
     LOADED BY: <role> @ <when>; NOT loaded by <roles>
     OWNER: <who>, updated <when>
     DO NOT PUT HERE: <what belongs elsewhere> (→ <where>) -->
```

---

## Target end-state per file (the contract)

### Behavioral contracts (kind B)

**`.claude/skills/orchestrator/SKILL.md`** *(NEW — the universal standing-rules contract)*
- Holds the SINGLE canonical statement of: dispatch discipline (the daily loop, the 3
  HARD-RULE exceptions), priority = kerf-first, bead lifecycle (daemon owns terminal
  transitions; never pre-set in_progress), the review gate, the monitor pattern, CWD
  discipline (never cd into a worktree), autonomy/flow boundaries, the major-issue
  fan-out trigger (pointer to the fanout skill for detail).
- ABSORBS, verbatim-in-spirit (dedup + clean): the content currently in
  `docs/orchestrator-rules.md`, AGENTS.md §"Daily loop (canonical)"/"Monitoring"/"CWD
  discipline"/"Multi-agent comms" bodies, AGENT_OPERATING_MANUAL.md §3 (Daily Loop) & §4
  (Monitoring), the HANDOFF.md `<!-- ORCHESTRATION DIRECTIVES — DO NOT EDIT -->` block,
  PRIORITIES.md "## Standing rules", and project.yaml's DURABLE operator directives
  (captain orchestrates-not-implements, daemon precedence, review-gate).
- POINTS to detail-owner skills; does NOT duplicate them: dispatch→harmonik-dispatch,
  comms→agent-comms, beads→beads-cli, lifecycle→harmonik-lifecycle, keeper→keeper,
  fanout→major-issue-fanout.
- Wrap the universal body in managed-region markers for future sync:
  `<!-- BEGIN harmonik:managed orchestrator-rules -->` … `<!-- END harmonik:managed -->`.
- MIRROR byte-identical to `cmd/harmonik/assets/skills/orchestrator/SKILL.md`.
- REGISTER it in init's skill list (the slice/embed walk that writes skills on init) and
  EXTEND `cmd/harmonik/init_skills_sync_test.go` so the sync-guard covers it.
- Then convert `docs/orchestrator-rules.md` to a 3-line STUB pointing at the skill
  (keep the path alive for any external refs): "Moved → the `orchestrator` skill
  (.claude/skills/orchestrator/SKILL.md). Standing rules are a loaded contract, not docs."

**`AGENTS.md`** (== CLAUDE.md symlink) → **pure ROUTER**
- KEEP: "Start here" reading order, "Key conventions" (specs/ location, kerf bench paths,
  codename:<name> label rule), the Beads/kerf command quick-blocks.
- ADD at top: a PRECEDENCE statement —
  "Standing behavioral rules: the `orchestrator` skill is canonical. On conflict:
  orchestrator skill > AGENTS.md prose > per-domain skills own their detail. Operational
  state lives in `.harmonik/context/` (captain) and `HANDOFF.md` (session) — never in
  this file."
- ADD: the per-ROLE load map (from 07/09 §per-role): captain cold-boot order, captain
  restart-resume (lean), crew minimal-load, implementer-orchestrator order.
- DELETE the duplicated bodies now owned by the orchestrator skill: §"Daily loop
  (canonical)" prose, §"Monitoring the daemon" prose, §"Multi-agent comms" body,
  §"CWD discipline" body — replace each with a ONE-LINE pointer to the orchestrator skill
  / the relevant domain skill.
- Wrap the universal/router block in `<!-- BEGIN harmonik:managed agents-router -->` …
  `<!-- END harmonik:managed -->`; project-specific deltas (codename glossary, repo
  paths) go OUTSIDE the markers.

### Operational state (kind C) — homes in `.harmonik/context/` + root

**`.harmonik/context/project.yaml`** (tier-3, weeks) → DURABLE-ONLY
- KEEP: `phase`, `locked_decisions`, `forbidden_actions`.
- REMOVE: time-boxed `operator_directives` / `priority_order` / `paused_initiatives`
  (the "3-day scale-out" block) → those go to captain-lanes.md (dated section).
- ADD the self-describing header (TIER 3).

**`.harmonik/context/captain-lanes.md`** (tier-2, days) → THE medium-term tracker
- KEEP: the active lane→crew→queue→model registry.
- ADD sections: `## Epics in progress` (live epics, owner, %/status, blocked/parked) —
  absorbing STATUS.md "Active work lanes"; and `## Active operator directives (dated)`
  with `set:`/`expires:` per item — absorbing project.yaml's 3-day scale-out block.
- REMOVE: any this-session salvage/run-id notes → those go to HANDOFF.md.
- ADD the self-describing header (TIER 2).

**`ROADMAP.md`** (root, tier-4 long) → CURRENT
- ADD a "## We are HERE" progress marker; bring the June captain-economy / keeper /
  productization campaigns onto the roadmap; assign milestone cadence in the header.
- ADD the self-describing header (TIER 4 / long).

**`HANDOFF.md`** (root, tier-1 session) → PURE short-term state
- REMOVE the `<!-- ORCHESTRATION DIRECTIVES — DO NOT EDIT -->` block (→ orchestrator skill).
- ABSORB PRIORITIES.md's operational this-resume checklist (the volatile half).
- ADD the self-describing header (TIER 1).
- Then DELETE `PRIORITIES.md` (its two halves are now in HANDOFF + orchestrator skill).

**`cmd/harmonik/assets/context/{project.yaml.tmpl,captain-lanes.md.tmpl,roadmap.md.tmpl,HANDOFF.md.tmpl}`** *(NEW templates)*
- Empty/seed versions of the four files above WITH the self-describing headers and
  section skeletons but NO project-specific content (these ship for Phase B init/sync).
- Also drop canonical copies at repo root mirror location if the sync-guard expects it;
  otherwise just author under assets/context.

### Retirements & reference (kind A / historical)

**`AGENT_OPERATING_MANUAL.md`** → RETIRE
- Move its 5 "Gotchas" (billing, wide-waves, epic-dep, $TMUX, stale-binary) into
  `docs/known-workarounds.md` (append, keep their headings).
- Move its command table / JSON-shape reference into a NEW `docs/cli-quickref.md`.
- Then DELETE AGENT_OPERATING_MANUAL.md.

**`TASKS.md`** → `mv` to `docs/historical/phase-0-1-tasks.md` (frozen archive).

**`STATUS.md`** → SLIM to: phase + locked decisions + spec inventory. REMOVE "Active work
lanes" (→ pointer to captain-lanes.md) and "Recently completed" (→ pointer to ROADMAP).

**`AGENT_INDEX.md`** → pure map. REMOVE "Major Features Landed", "Active work lanes",
embedded crew names; ADD one-line pointers to ROADMAP.md + captain-lanes.md. Fix the
reading order it advertises to: AGENT_INDEX → STATUS → captain-lanes (medium) → HANDOFF
(drop TASKS).

### Boot wiring (kind B skills)

**`.claude/skills/captain/STARTUP.md`** → Step 1.3 changes from
`docs/orchestrator-rules.md` to "load the `orchestrator` skill (standing rules)." Update
any other `orchestrator-rules.md` reference. MIRROR to
`cmd/harmonik/assets/skills/captain/STARTUP.md` (keep byte-identical for the sync-guard).

**`.claude/skills/crew-launch/SKILL.md`** → make the crew MINIMAL-LOAD explicit: add a
short "Do NOT load fleet-level state (ROADMAP, captain-lanes, project.yaml, orchestrator
standing-rules) — you are scoped to ONE epic + ONE queue; your mission file is your
tier-1 state." MIRROR to the assets copy.

---

## Lane assignments (disjoint — one agent each)
1. **orchestrator-skill** → orchestrator skill + assets mirror + init registration + sync test + orchestrator-rules.md stub.
2. **router** → AGENTS.md only.
3. **context-state** → project.yaml, captain-lanes.md, ROADMAP.md, HANDOFF.md, PRIORITIES.md (delete), assets/context templates.
4. **retirements** → AGENT_OPERATING_MANUAL.md, TASKS.md, STATUS.md, AGENT_INDEX.md, docs/known-workarounds.md, docs/cli-quickref.md (new), docs/historical/ (new).
5. **boot-wiring** → captain/STARTUP.md (+assets mirror), crew-launch/SKILL.md (+assets mirror).

No two lanes share a file. orchestrator-rules.md is owned solely by lane 1.
