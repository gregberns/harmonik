# Instruction Architecture Audit — Tier Separation & Consolidation Seed

> Audit date: 2026-06-20. Goal: cleanly separate three tiers of guidance
> (short-term/operational, medium-term/process, long-term/roadmap) PLUS a
> cross-cutting axis (stable BEHAVIORAL directives vs volatile OPERATIONAL STATE),
> and define a single clear owner + update-cadence for each.

## Tier vocabulary used in this audit

- **SHORT-TERM / OPERATIONAL** — hour-to-hour, session-to-session. Current handoffs, live lanes, "what should I do this hour," which crews are up, which beads are in-flight.
- **MEDIUM-TERM / PROCESS** — epics-in-progress, initiative tracking, "how we process work," lane→crew mapping, what's blocked/parked.
- **LONG-TERM / ROADMAP** — phases, roadmap, objectives, progress through the roadmap, "what are we building this month."
- **BEHAVIORAL (stable)** — how to behave / how to perform actions. Dispatch discipline, boot sequences, comms discipline, review gates, CWD discipline. Changes rarely.
- **REFERENCE** — CLI surface, command syntax, flags, exit codes. Stable, lookup-only.
- **OPERATIONAL STATE (volatile)** — what is happening NOW. Crew names, current epic IDs, current lane assignments, in-flight bead IDs, "as of <date>" snapshots.

The operator's complaint: BEHAVIORAL and OPERATIONAL STATE are heavily intermixed today. This audit confirms it and proposes a clean split.

---

## A. MATRIX — what each source covers, and where it mixes tiers

| Source | Tiers covered | Mixes? | Worst mixing instances |
|---|---|---|---|
| **AGENTS.md** (== CLAUDE.md symlink) | BEHAVIORAL (primary), REFERENCE (br/kerf cmd blocks), pointers to all others | **YES** — behavioral + reference + thin pointers, but the bigger issue is behavioral content **duplicated** from orchestrator-rules.md and the skills (see Overlap §B) | "Daily loop (canonical)" (§37-41) restates dispatch discipline that orchestrator-rules.md + harmonik-dispatch own; embedded bead IDs (`hk-li14r`, `hk-trjef`, `hk-8sm4f`) bake VOLATILE provenance into a stable file |
| **AGENT_OPERATING_MANUAL.md** | BEHAVIORAL (reading order, daily loop), REFERENCE (cmd table, JSON shapes), + 5 "gotchas" | **YES** — explicitly "distills the most operationally critical rules from AGENTS.md" → triplication. Gotchas 1-5 are stable behavioral, but §3 Daily Loop and §4 Monitoring **re-state** harmonik-dispatch + orchestrator-rules verbatim. Concurrency number `--max-concurrent 4` is hard-coded (it's a per-box tuning knob = semi-volatile) | §3 Daily Loop ≈ orchestrator-rules §Dispatch + harmonik-dispatch §The loop; §4 Monitoring ≈ orchestrator-rules §Monitor pattern |
| **docs/orchestrator-rules.md** | BEHAVIORAL (primary — "permanent directives"), REFERENCE (pre-screen/monitor snippets) | **MOSTLY CLEAN of state**, but embeds many VOLATILE bead IDs as provenance (`hk-m9a7g`, `hk-b3wqd`, `hk-rc51s`, `hk-nk9pu`, `hk-w6y70`) and dated incident refs. These rot. Also restates content that lives in harmonik-dispatch / major-issue-fanout skills | This is the **best-positioned** file to be the canonical behavioral home — but it currently competes with AGENTS.md + AOM for that role |
| **docs/known-workarounds.md** | REFERENCE / BEHAVIORAL (workaround registry), some OPERATIONAL (release process) | Partial — workarounds are semi-stable; the release-process block is a runbook (REFERENCE) | Mostly fine as a reference registry; mixing is mild |
| **.claude/skills/captain (STARTUP.md + SKILL.md)** | BEHAVIORAL (boot runbook, autonomy boundaries, spawn/mail mechanics), REFERENCE (CLI) | **CLEAN** — deliberately externalizes volatile state to `.harmonik/context/captain-lanes.md` (tier-2) and `project.yaml` (tier-3). Only illustrative example crews (`alpha`/`bravo`) + example epic in docstrings. **This is the model the rest should follow.** | None load-bearing |
| **.claude/skills/crew-launch/SKILL.md** | BEHAVIORAL (boot, loop, progress feed), REFERENCE (CLI) | **CLEAN** — parses volatile `Current State` from per-crew mission file (tier-1), not hard-coded | None |
| **.claude/skills/keeper/SKILL.md** | BEHAVIORAL (warn/act handling, restart), REFERENCE (cmd surface) + thresholds | Mostly clean; warn/act threshold VALUES are config-volatile but documented as "REAL values from code" | Mild — threshold values are a config mirror |
| **.claude/skills/harmonik-dispatch/SKILL.md** | BEHAVIORAL (daily loop, stream-vs-wave), REFERENCE | **OVERLAP-heavy** — is the detailed twin of orchestrator-rules §Dispatch + AGENTS.md §Daily loop | See §B |
| **.claude/skills/harmonik-lifecycle/SKILL.md** | REFERENCE (init/supervise/reconcile/promote CLI), BEHAVIORAL (idempotency/auto-revive contracts) | **CLEAN** — pure command reference + contracts | None |
| **AGENT_INDEX.md** | LONG-TERM (problem/goal/concept map) + pointers | Partial — "Major Features Landed (2026-06)" + "Active work lanes" duplicate STATUS.md / INITIATIVES.md OPERATIONAL STATE into the supposedly-stable master map | "Major Features Landed", "Named queues — parked", per-crew names (`chani/duncan/liet/stilgar`) embedded in an index doc |
| **STATUS.md** | LONG-TERM (phase, locked decisions, spec inventory) + MEDIUM (active lanes) + OPERATIONAL (recently-completed table) | **YES** — mixes phase/roadmap (long), active-lane table (medium), and a recently-completed log (operational). "Active work lanes" table is stale relative to HANDOFF | Active-lanes table (medium/volatile) sits beside locked-decisions (long/stable) |
| **TASKS.md** | Mostly LONG-TERM/historical (Phase 0/1 archive) | **STALE** — "Last updated 2026-05-12"; almost entirely a historical archive now. Defers live state to HANDOFF. Effectively dead as an operational surface | Whole file is a frozen artifact mislabeled as a "task list" |
| **PRIORITIES.md** (untracked) | SHORT-TERM / OPERATIONAL (this-resume checklist) + a few standing rules | **YES** — a live operational checklist that ALSO carries "Standing rules" (behavioral: default=DOT, captain doesn't babysit, daemon-redeploy recipe). Behavioral rules buried in a volatile checklist | "## Standing rules" (behavioral, stable) at the bottom of a dated operational checklist |
| **HANDOFF.md** | SHORT-TERM / OPERATIONAL (this-session state) + embedded ORCHESTRATION DIRECTIVES header (behavioral) | **YES** — the `<!-- ORCHESTRATION DIRECTIVES — DO NOT EDIT -->` block (4 stable behavioral rules) is pinned at the top of the most-volatile file in the repo | The DO-NOT-EDIT directives block is stable behavioral content living inside the single most ephemeral file |
| **.harmonik/context/project.yaml** | LONG-TERM (phase, locked decisions, forbidden actions) + MEDIUM (operator directives, priority order) | Partial-by-design — tier-3, but the 2026-06-19 "operator_directives" + "priority_order" are MEDIUM-term and time-boxed ("3-day scale-out") living in a weeks-cadence file | Time-boxed scale-out directives in a "weeks cadence" file |
| **.harmonik/context/captain-lanes.md** | MEDIUM (lane→crew registry) + SHORT (in-flight salvage notes) | Partial — tier-2 by design, but carries SHORT-term salvage/in-flight notes ("SALVAGED this session", specific run IDs) | "SALVAGED this session" run-id notes belong in HANDOFF (tier-1) |

### Cross-cutting BEHAVIORAL ↔ OPERATIONAL intermixing — the flagged hotspots

1. **HANDOFF.md DO-NOT-EDIT directives block** — stable behavioral rules pinned atop the most volatile file. Re-pasted every handoff; rots silently.
2. **PRIORITIES.md "Standing rules"** — behavioral (default=DOT, no-babysit, redeploy recipe) at the foot of a dated operational checklist.
3. **project.yaml operator_directives / priority_order** — time-boxed MEDIUM-term process directives ("3-day scale-out") inside a LONG-term weeks-cadence file. These will be stale in days and there is no eviction owner.
4. **STATUS.md** — long-term locked-decisions table interleaved with a medium-term active-lanes table and an operational recently-completed log.
5. **AGENT_INDEX.md "Major Features Landed" + crew names** — operational snapshot embedded in the stable master map.
6. **Bead-ID provenance everywhere** — orchestrator-rules.md, AGENTS.md, and skills embed dozens of `hk-*` incident IDs as inline provenance. Each is volatile and rots; collectively they make stable files read as half-changelog.

---

## B. OVERLAP MAP — same thing said in 2+ places; who wins?

### Overlap 1 — Dispatch discipline / the daily loop (the worst one)
Said in **FOUR** places:
- **AGENTS.md §"Daily loop (canonical)"** — one-paragraph anchor + pointer to harmonik-dispatch.
- **AGENT_OPERATING_MANUAL.md §3 "The Daily Loop"** — full loop + JSON submit shape + append.
- **docs/orchestrator-rules.md §"Dispatch discipline"** — HARD-RULE-tagged, concise, authoritative, with the 3 exceptions.
- **.claude/skills/harmonik-dispatch/SKILL.md** — the most narrative/detailed playbook (8-step loop, pre-screen, stream-vs-wave, 75% criterion).

**Authority ambiguity:** No file declares which wins. orchestrator-rules tags rules "HARD RULE" (implying authority); harmonik-dispatch is the most detailed; AOM duplicates both; AGENTS.md is a pointer. A reader can't tell whether AOM's loop or harmonik-dispatch's loop is canonical when they drift. **They have already drifted** (AOM hard-codes `--max-concurrent 4`; orchestrator-rules says "prefer `--wave` for N>1"; AGENTS.md §Daily loop doesn't mention the wave caveat at all).

### Overlap 2 — Monitor pattern (`harmonik subscribe`)
Said in: AGENT_OPERATING_MANUAL §4, orchestrator-rules §"Monitor pattern", harmonik-dispatch. Identical `subscribe --types ... --heartbeat 60s --json` snippet triplicated, plus the same "events-tail fallback" deprecation note in two of them. **Winner undeclared.**

### Overlap 3 — Pre-screen for already-landed beads
Said in: AGENT_OPERATING_MANUAL §5 (the `for id in ...` git-log loop), orchestrator-rules §"Pre-flight" (same loop verbatim), harmonik-dispatch. Triplicated bash.

### Overlap 4 — CWD discipline (never `cd` into a worktree)
Said in: AGENTS.md §"CWD discipline", orchestrator-rules §"Dispatch shape" (CWD DISCIPLINE), AOM (implied). Triplicated.

### Overlap 5 — Comms bus
Said in: AGENTS.md §"Multi-agent comms", AGENT_OPERATING_MANUAL §7, and the **agent-comms skill** (the actual spec-backed owner). AGENTS.md + AOM both restate the send/recv/who/log surface and the "file-outbox RETIRED" note. **agent-comms skill should be the sole owner;** the other two should be one-line pointers.

### Overlap 6 — Keeper set-dispatching / clear-dispatching
Said in: orchestrator-rules §"Pre-flight checklist" steps 4&7, and the keeper skill (the detailed owner). orchestrator-rules cites keeper — this one is **correctly layered** (pointer + owner). Use it as the template.

### Overlap 7 — Major-issue fan-out
Said in: orchestrator-rules §"Autonomy and flow", the major-issue-fanout skill, docs/major-issue-fanout-protocol.md. Three homes for one protocol.

### Overlap 8 — Roadmap / progress through roadmap / initiatives
Said in: ROADMAP.md (phases, 2026-05-15), STATUS.md ("Active work lanes" + "Recently completed"), docs/INITIATIVES.md ("Captain Initiatives Board", 2026-06-10), AGENT_INDEX.md ("Major Features Landed"). **Four overlapping trackers of medium/long-term progress, all stamped different dates, none authoritative.** A captain asking "what are we building this month / how far through the roadmap are we" gets four partially-contradicting answers.

### Overlap 9 — Locked decisions
Said in: STATUS.md §"Decisions in force" (10+4+session list) and project.yaml `locked_decisions:`. Two copies; project.yaml is the machine-read one, STATUS the prose one. Drift risk.

**Authority-resolution verdict:** Today there is **no declared precedence anywhere.** The cross-project `~/.claude/CLAUDE.md` says "project file wins over global," and AGENTS.md says "skills are global, don't duplicate" — but within the project, AGENTS.md vs orchestrator-rules vs AOM vs the skills have no stated hierarchy. This is the root of the consolidation problem.

---

## C. GAP ANALYSIS — what's missing

1. **No single home for "what should I do this HOUR."** Today it's smeared across HANDOFF.md, PRIORITIES.md (untracked!), captain-lanes.md, and `kerf next`. PRIORITIES.md is the closest but is **untracked (not in git)** and carries behavioral rules. A booting captain must read 3-4 files to assemble current operational intent.

2. **No clean home for MEDIUM-TERM "epics in progress."** It's split between docs/INITIATIVES.md (stale 2026-06-10), STATUS.md "Active work lanes" (stale 2026-06-10), captain-lanes.md (tier-2, 2026-06-18), and the live beads ledger. No file is the declared owner of "which epics are live, who owns them, % done." INITIATIVES.md claims the role ("the high-level tracker") but is not kept current.

3. **No tracked owner of "progress through the roadmap."** ROADMAP.md (2026-05-15) lists phases but its status column is 5 weeks stale; nothing updates "we are HERE on the roadmap." The captain-economy / keeper campaigns that dominated June aren't on the roadmap at all.

4. **Behavioral directives have no single canonical home.** They're split across AGENTS.md, orchestrator-rules.md, AOM, project.yaml (forbidden_actions/operator_directives), PRIORITIES.md (Standing rules), and the HANDOFF directives header. "Where is the authoritative list of stable rules" has no answer.

5. **No declared precedence / authority order** among project instruction sources (see §B verdict).

6. **Time-boxed directives have no eviction owner.** The "3-day scale-out" directives (2026-06-19) in project.yaml will be stale within days; nothing is responsible for retiring them. Medium-term process directives need a dated, expirable home.

7. **PRIORITIES.md is untracked** — the de-facto current-operational-state file isn't in version control, so it's invisible to fresh sessions / sub-agents and lost on machine change.

8. **TASKS.md is dead** — a historical archive masquerading as the active task list; the reading-order (AGENT_INDEX→STATUS→TASKS) still points at it, wasting a boot read.

---

## D. PROPOSED TARGET ARCHITECTURE

Design rule: **each file is EITHER stable-behavioral OR volatile-state, never both.** Each tier has ONE owner and ONE update cadence. Stable files contain rules only; bead-ID provenance moves to a footnote/changelog or is dropped. Volatile files are explicitly dated and expirable.

### The 3 tiers × behavioral/operational grid → concrete files

| | BEHAVIORAL (stable rules) | OPERATIONAL STATE (volatile facts) |
|---|---|---|
| **SHORT-TERM (hours/session)** | *(none — short-term behavior is just "follow the standing rules")* | **HANDOFF.md** — this-session state, in-flight beads, next concrete steps. Owner: ending session. Cadence: every `/session-handoff`. **Strip the DO-NOT-EDIT directives block** (moves to standing-rules). |
| **MEDIUM-TERM (days/weeks)** | **docs/orchestrator-rules.md** → rename role to **the single canonical standing-rules file** (dispatch, priority, lifecycle, autonomy, review-gate, monitor, fan-out). Absorbs PRIORITIES.md "Standing rules" + HANDOFF directives + project.yaml operator_directives' *durable* parts. Cadence: changes only on a deliberate rule change. | **docs/INITIATIVES.md** → the single MEDIUM-term tracker: live epics, lane→crew mapping, %done, blocked/parked. Owner: captain at each heartbeat. Cadence: hourly/daily. Absorbs STATUS.md "Active work lanes" + captain-lanes.md operator_initiatives. |
| **LONG-TERM (weeks/months)** | **STATUS.md** (slimmed) → phase, locked decisions, spec inventory. Stable-ish. Cadence: on phase change / decision lock. **Mirror locked_decisions to project.yaml** with one declared as source (project.yaml = machine source, STATUS = prose mirror, link them). | **ROADMAP.md** → phases + a *current* "we are HERE" marker + progress-through-roadmap. Owner: operator + captain on milestone. Cadence: on milestone. Must be kept current (today it's the most-stale). |

### Specific moves (the consolidation work)

1. **Establish precedence, written at the top of AGENTS.md:**
   `Standing behavioral rules: docs/orchestrator-rules.md is canonical. On conflict: orchestrator-rules > AGENTS.md prose > AGENT_OPERATING_MANUAL. Skills own their detailed surface (dispatch→harmonik-dispatch, comms→agent-comms, etc.); AGENTS.md only points to them.`

2. **AGENTS.md becomes a pure ROUTER.** Keep: "Start here" reading order, the precedence statement above, and one-line pointers to each owner. **Delete** the duplicated Daily-loop / Monitoring / Comms / dispatch bodies (they live in orchestrator-rules + the skills). Keep "Key conventions" (specs/, kerf bench paths, codename labels) — those are genuinely AGENTS-owned and not duplicated.

3. **RETIRE AGENT_OPERATING_MANUAL.md as a parallel rule source.** Its unique value is the 5 Gotchas (billing, wide-waves, epic-dep, $TMUX, stale-binary) — **move those into docs/known-workarounds.md** (they ARE workarounds). Move the cmd-table to a `docs/cli-quickref.md` reference card. Then the file is empty → delete, or leave a stub pointer. This kills Overlaps 1-4 at the source.

4. **orchestrator-rules.md becomes THE standing-rules file.** Absorb: PRIORITIES.md "## Standing rules", the HANDOFF `<!-- ORCHESTRATION DIRECTIVES -->` block, and project.yaml's durable operator directives (captain orchestrates-not-does, daemon-precedence, review-gate). **Strip inline bead-ID provenance** into a trailing "## Provenance" footnote section so the rule text reads clean. Cadence-tag the file "stable — changes are deliberate rule changes."

5. **PRIORITIES.md → fold into HANDOFF.md + git-track the result.** Its operational checklist is exactly tier-1 short-term state = HANDOFF's job. Its "Standing rules" go to orchestrator-rules (step 4). Then delete PRIORITIES.md. (It must not stay untracked.)

6. **project.yaml stays tier-3 machine-state** but **evict the time-boxed scale-out block** into a new dated MEDIUM-term home: add an `## Active operator directives (dated, expirable)` section to **docs/INITIATIVES.md**, each with a `set:`/`expires:` date. Keep in project.yaml only the *durable* `phase`, `forbidden_actions`, `locked_decisions`.

7. **captain-lanes.md stays tier-2** but **strip SHORT-term salvage/run-id notes** (they go to HANDOFF). It becomes purely the lane→crew registry, and should be **reconciled with INITIATIVES.md** so there's one medium-term tracker (or make captain-lanes the machine-mirror that INITIATIVES renders from).

8. **STATUS.md slimmed:** remove "Active work lanes" (→ INITIATIVES) and "Recently completed" (→ ROADMAP milestone log). Keep phase + locked decisions + spec inventory.

9. **TASKS.md retired:** rename to `docs/historical/phase-0-1-tasks.md`; fix the reading order in AGENTS.md + AOM-quickref + STATUS to **AGENT_INDEX → STATUS → INITIATIVES → HANDOFF** (drop TASKS).

10. **AGENT_INDEX.md:** remove "Major Features Landed", "Active work lanes", and embedded crew names; replace with one-line pointers to INITIATIVES.md and ROADMAP.md. Keep it a pure stable map.

### Who reads what, when (the boot contract)

- **Captain/orchestrator boot:** AGENT_INDEX (map) → STATUS (phase+decisions, long) → INITIATIVES (live epics+lanes, medium) → HANDOFF (this-session, short) → orchestrator-rules (standing rules). Skills load per role.
- **"What do I do this HOUR?"** → HANDOFF.md (+ `kerf next`).
- **"What are we building this month / roadmap progress?"** → ROADMAP.md + INITIATIVES.md.
- **"How do I behave / perform action X?"** → orchestrator-rules.md (standing rules) → the owning skill for detail.
- **"Is X a known gotcha?"** → docs/known-workarounds.md.

### End-state file roster (single owner + cadence)

| File | Tier × axis | Owner | Cadence |
|---|---|---|---|
| AGENTS.md / CLAUDE.md | router + precedence + key conventions | maintainer | rare |
| docs/orchestrator-rules.md | BEHAVIORAL standing rules (all tiers) | maintainer | deliberate rule change |
| docs/known-workarounds.md | REFERENCE workarounds (+ ex-AOM gotchas) | anyone hitting one | as discovered |
| docs/cli-quickref.md *(new)* | REFERENCE cmd cards | maintainer | on CLI change |
| STATUS.md | LONG-TERM stable (phase, decisions, specs) | maintainer | phase/decision change |
| ROADMAP.md | LONG-TERM progress marker | operator+captain | on milestone |
| docs/INITIATIVES.md | MEDIUM-TERM live tracker (+ dated operator directives) | captain | hourly/daily |
| HANDOFF.md | SHORT-TERM session state | ending session | every handoff |
| .harmonik/context/project.yaml | tier-3 machine state (durable only) | captain | weeks |
| .harmonik/context/captain-lanes.md | tier-2 lane registry (machine mirror of INITIATIVES) | captain | per session |
| Skills (captain/crew/keeper/dispatch/lifecycle/comms) | BEHAVIORAL+REFERENCE, role-detail owners | maintainer | with the mechanism |
| TASKS.md | RETIRED → docs/historical/ | — | frozen |
| PRIORITIES.md | RETIRED → folded into HANDOFF + orchestrator-rules | — | — |
| AGENT_OPERATING_MANUAL.md | RETIRED → gotchas to workarounds, cmds to quickref | — | — |

This collapses 4 overlapping rule sources into 1 (orchestrator-rules), 4 overlapping progress trackers into 2 (ROADMAP long + INITIATIVES medium), and pulls every stable behavioral directive out of the three volatile files (HANDOFF header, PRIORITIES standing-rules, project.yaml time-boxed directives) where it currently rots.
