# Load Discipline — the real target model

> Supersedes the "PROPOSED TARGET ARCHITECTURE" in 06 where they conflict.
> 06 was right that behavioral and operational state are intermixed; it was WRONG
> about the homes — it routed medium-term state and standing-rules into `docs/`.
> The captain never reads `docs/` at boot. This doc fixes that.

## The organizing principle: three KINDS of artifact, loaded differently

The repo has three fundamentally different kinds of file, and the whole problem is
that they're intermixed and there's no stated rule for **who loads what, when, and why**.

| Kind | What it is | Where it lives | How it's consumed |
|---|---|---|---|
| **A. Documentation** (knowledge base) | Reference material to *understand* the system: problems, concepts, components, specs, product/CLI docs | `docs/`, `specs/`, root product docs | **On-demand**, when you need to understand a subsystem. NEVER bulk-loaded at boot. Input to kerf. |
| **B. Behavioral contracts** (how to act) | Stable operating contracts: boot runbooks, dispatch discipline, comms rules, role mechanics | `.claude/skills/*` + a standing-rules contract + `AGENTS.md` as router | **Loaded at boot as a contract**, scoped to your ROLE. Changes only on a deliberate rule change. |
| **C. Operational state** (what's happening now) | Volatile facts: phase, lanes, epics-in-progress, this-session handoff | `.harmonik/context/` (tiered) + `HANDOFF.md` + `ROADMAP.md` | **Loaded at boot, tiered by cadence**, then VERIFIED against live ground-truth. |

The captain STARTUP.md **already implements this** for the captain role (tier-3/2/1
+ ground-truth + the orchestrator standing-rules contract). The fix is to make the
file homes, the headers, and the OTHER roles' load-maps match that model — not to
invent a new one.

`docs/` is for kind A only. The two corrections from the operator:
- **Medium-term "epics in progress" is kind C (operational state), not kind A.** It belongs in `.harmonik/context/`, NOT `docs/INITIATIVES.md`.
- **Standing behavioral rules are kind B (a contract), not kind A.** They belong in the skills/contract tree, NOT `docs/orchestrator-rules.md`.

---

## Operational state, tiered by cadence (kind C) — homes in `.harmonik/context/`

This is the captain's existing tier model, made the canonical home for ALL volatile state:

| Tier | Cadence | File | Owns | Captain boot step |
|---|---|---|---|---|
| **tier-4 / long** | weeks–months | `ROADMAP.md` *(root, human-visible)* OR `.harmonik/context/roadmap.md` | objectives, phases, **"we are HERE" progress marker** | read on milestone / cold boot, not every restart |
| **tier-3** | weeks | `.harmonik/context/project.yaml` | phase, `locked_decisions`, `forbidden_actions` — durable machine state only | Step 0a |
| **tier-2** | days | `.harmonik/context/captain-lanes.md` | current lane→crew→queue→model registry **+ epics-in-progress + parked + pipeline** (= the medium-term tracker) | Step 0b |
| **tier-1** | session/hour | `HANDOFF.md` | this-session narrative, in-flight beads, next steps, salvage notes | Step 1 (last; a CLAIM) |

Key consequence: **medium-term epics-in-progress lives in `captain-lanes.md`** (tier-2),
which the captain already reads at Step 0b — no new boot read, no `docs/` file.
captain-lanes.md becomes the single medium-term operational tracker (absorbing what
06 wanted to call INITIATIVES.md, and STATUS.md's "Active work lanes").

What gets EVICTED from these state files (the intermixing the operator flagged):
- `HANDOFF.md` DO-NOT-EDIT directives block → **standing-rules contract** (kind B).
- `project.yaml` time-boxed "3-day scale-out" operator_directives → tier-2 `captain-lanes.md` as a **dated, expirable** `## Active operator directives (set:/expires:)` section (campaign posture is days-cadence, not weeks).
- `captain-lanes.md` per-session salvage/run-id notes → `HANDOFF.md` (tier-1).
- `PRIORITIES.md` (untracked) → operational half into `HANDOFF.md`; standing-rules half into the contract. Then delete.

---

## Behavioral contracts (kind B) — homes in the skills/contract tree, NOT `docs/`

The skills are "already correct" (06 §A). The one outlier is **standing orchestrator
rules**, which today sit in `docs/orchestrator-rules.md` — wrong bucket (it's a
loaded contract, not on-demand documentation). It should move into the contract tree
so it composes with the captain/crew/keeper skills the same way.

**OPEN DECISION (needs operator steer) — where do standing-rules live?**
- **(a) [recommended]** Fold into a project-local **`orchestrator` skill** (`.claude/skills/orchestrator/SKILL.md`) — loaded by the captain at Step 1.3 and by the implementer-orchestrator on session-resume. Composes with the existing skill model; the captain STARTUP already loads it as a "contract" conceptually.
- **(b)** A top-level operating-contract file (e.g. `OPERATING-RULES.md` at root) — visible, but yet another root file.
- **(c)** Keep the file but relocate out of `docs/` to an operational path.

Whichever home: it **absorbs** the 4 scattered dispatch-discipline restatements
(AGENTS.md §Daily-loop, AGENT_OPERATING_MANUAL §3-4, the HANDOFF directives block,
PRIORITIES "Standing rules") so dispatch discipline is stated **once**, and `AGENTS.md`
becomes a **router that declares precedence** instead of a 4th copy.

Skill ownership stays as-is (the detailed surface): dispatch→`harmonik-dispatch`,
comms→`agent-comms`, beads→`beads-cli`, lifecycle→`harmonik-lifecycle`,
keeper→`keeper`, fan-out→`major-issue-fanout`. The standing-rules contract POINTS to
these for detail; it does not duplicate them.

---

## The per-ROLE load map (this is the "wiring" the operator asked for)

The point: **each role loads only its slice.** A crew should not read the roadmap; a
captain should not full-read STATUS/TASKS every session. The load order lives in each
role's boot runbook, and each context file carries a header saying who loads it & when.

### Captain — full cold boot
(Already specified in `captain/STARTUP.md`. Affirmed, with corrected homes.)
1. Step 0 — identity + CWD guard.
2. Step 0a — tier-3 `project.yaml` (phase, decisions, guardrails).
3. Step 0b — tier-2 `captain-lanes.md` (lanes + epics-in-progress + parked).
4. Step 1 — `captain/SKILL.md` + standing-rules contract + tier-1 `HANDOFF.md` (claim).
5. Step 2 — boot digest = ground-truth; overrides every claim above.
6. Steps 3–6 — reconcile, plan, staff, arm watchers.
- Captain does **NOT** boot-read: `AGENT_INDEX.md`, `STATUS.md`, `TASKS.md`, product docs, `docs/` knowledge base, full `keeper`/other skill bodies. (STARTUP §Step 1 note already forbids this for context economy.) `ROADMAP.md` only on cold boot / milestone.

### Captain — keeper-restart resume (LEAN)
- Re-drain comms → re-read tier-3/tier-2 + ONE boot digest → trust cached tier state as input → re-arm watchers. No heavy re-derive. (Already in STARTUP §"On resume after a restart-now cycle".)

### Crew — boot
- Loads: its **mission file** (tier-1, `.harmonik/crew/missions/<crew>.md`) + `crew-launch/SKILL.md` + `agent-comms` + `beads-cli` + `harmonik-dispatch`.
- Does **NOT** load: `ROADMAP.md`, `captain-lanes.md`, `project.yaml`, standing orchestrator-rules, `STATUS`/`TASKS`/`HANDOFF`, knowledge-base docs. A crew is scoped to ONE epic + ONE queue; fleet-level state is the captain's concern. (Already true in `crew-launch/SKILL.md` — affirm and make explicit.)

### Implementer-orchestrator (main `/session-resume`, non-captain)
- Loads: `AGENT_INDEX → STATUS → HANDOFF` (the CLAUDE.md reading order) + standing-rules contract + `harmonik-dispatch`. This is the heavier reading order the captain STARTUP deliberately opts OUT of.

---

## Self-describing headers (makes the wiring rot-resistant)

Every kind-C state file and the standing-rules contract gets a 4-line header so the
load discipline is documented AT the file, not only in a runbook:

```
<!-- TIER: 2 (operational state, days cadence)
     LOADED BY: captain @ STARTUP Step 0b; NOT loaded by crews or implementers
     OWNER: captain, updated at session end (before HANDOFF.md)
     DO NOT PUT HERE: standing rules (→ orchestrator contract), this-session salvage notes (→ HANDOFF.md) -->
```

This is the single highest-leverage anti-rot move: it makes "why am I reading this,
and what does NOT belong here" answerable from the file itself.

---

## What this changes vs. report 06

| 06 said | Corrected here |
|---|---|
| Medium-term → `docs/INITIATIVES.md` | Medium-term → `.harmonik/context/captain-lanes.md` (kind C, not docs/) |
| Standing-rules → `docs/orchestrator-rules.md` (canonical) | Standing-rules → skill/contract tree (kind B, not docs/); **open decision (a)/(b)/(c)** |
| Boot contract: AGENT_INDEX→STATUS→INITIATIVES→HANDOFF for everyone | Boot contract is **per-role**; captain uses the tier-3/2/1+digest model and skips AGENT_INDEX/STATUS at boot |
| (no header convention) | Self-describing TIER/LOADED-BY/OWNER/DO-NOT-PUT-HERE header on every state file |

Everything else in 06 (retire AOM/TASKS/PRIORITIES, AGENTS.md→router+precedence,
slim STATUS, regenerate CLI-REFERENCE, kill the duplications) stands.

---

## Concrete consolidation checklist (revised)

1. **Standing-rules contract** (pending open decision a/b/c): create it; absorb the 4 dispatch-discipline restatements + HANDOFF directives block + PRIORITIES standing-rules; strip inline bead-ID provenance to a footnote.
2. **AGENTS.md → router**: declare precedence + per-role load order; delete duplicated daily-loop/monitor/comms bodies; keep "Key conventions".
3. **captain-lanes.md → the medium-term tracker**: add epics-in-progress + parked sections; add the dated/expirable operator-directives section; add the self-describing header; strip salvage notes.
4. **project.yaml → durable-only**: phase + locked_decisions + forbidden_actions; evict time-boxed directives to captain-lanes.md.
5. **HANDOFF.md → pure tier-1**: strip the DO-NOT-EDIT directives block; absorb PRIORITIES operational checklist; add header.
6. **ROADMAP.md → current**: add a "we are HERE" marker; bring June campaigns onto it; assign milestone-cadence owner.
7. **Retire**: AGENT_OPERATING_MANUAL.md (gotchas→known-workarounds, cmds→a cli-quickref), TASKS.md (→docs/historical/), PRIORITIES.md (folded, then deleted).
8. **STATUS.md → slim**: phase + locked decisions + spec inventory only.
9. **AGENT_INDEX.md → pure map**: drop "Major Features Landed"/lanes/crew-names; point to ROADMAP + captain-lanes.
10. **Affirm crew minimal-load** in crew-launch/SKILL.md (explicit "do NOT load fleet-level state").
11. **Headers** on every kind-C file + the contract.
