# Leanfleet — Runner / Planner Gap Analysis

**Date:** 2026-06-22  
**Scope:** Model-tiering / crew-class vision vs. what is actually landed and where the gaps are.  
**Target model (operator-stated):** Runner crew = Sonnet + final Opus quality-gate at close-out. Planner crew = Opus throughout. Goal = right model for cognitive demand, not cheapness as a policy.

---

## 1. What is already landed

| Feature | Status | Where |
|---|---|---|
| Per-crew `model:` in mission front-matter | **LANDED** (`hk-9j3z`, be9f7453) | `crewstart.go`, `specs/crew-handoff-schema.md` |
| Daemon injects `--model` into crew launch argv | **LANDED** | `internal/daemon/crewstart.go` `readMissionModel` + `buildCrewLaunchSpec` |
| Captain decision rule (Sonnet=lane-drain, Opus=design/test/investigation) | **LANDED** | `.claude/skills/captain/STARTUP.md` §4 model-tiering table |
| "Escalate to captain on ANY run_failed, do NOT self-classify" mission clause | **LANDED** (in captain skill, not in existing mission files) | STARTUP.md §4 |
| Ops-monitor crew runs on Sonnet | **LANDED** (archived mission has `model: sonnet`) | `.harmonik/crew/missions/_archive/2026-06-20/ops-monitor.md` |
| DOT reviewer agent type defaults to Sonnet | **LANDED** | `defaultModelEntries[AgentTypeClaudeCode]` = sonnet; no reviewer-specific override |
| Per-DOT-node `model` attribute (WG-042) spec'd, permissive loader | **LANDED in spec**, NOT wired to override at dispatch | `specs/workflow-graph.md §4 WG-042`, `modelpreference.go` tier-1 bead-label path |
| Failure-triage bright line → Opus crews | **Designed** (leanfleet WS#5 C2), **not enforced** in code | docs/plans/leanfleet/design.md §5 |

---

## 2. Gap Table

| Idea | Landed? | Gap | Minimal Fix |
|---|---|---|---|
| **Crew class abstraction ("runner" / "planner" as named, reusable types)** | NO | Only per-crew `model:` in mission front-matter — no crew-class enum, no bundled defaults, no config template per class. Each mission must be set individually; there is no "runner" default that auto-sets model + escalation clause. | Add a `class: runner | planner` optional field to the mission schema. Let `crewstart.go` map class→model when `model:` is absent: runner→sonnet, planner→opus. Provide two boilerplate mission templates (`_TEMPLATE-runner.md`, `_TEMPLATE-planner.md`). No new code path needed — crewstart already reads front-matter. |
| **Current mission files have `model:` set** | NO | None of the active mission files (`paul.md`, `stilgar.md`, `irulan.md`, `leto.md`, `chani.md`, etc.) have a `model:` field or `class:` field. They all launch at the compiled Sonnet default but without the runner/planner semantic attached. | Captain sets `model:` when writing mission front-matter (STARTUP.md §4 already says to do this). Gap is captain compliance, not mechanism. Short-term: add `model:` to every active mission file matching the captain's assigned class. |
| **Final Opus review at close-out (quality gate for runner crews)** | PARTIAL | DOT `review` node already runs on Sonnet by default (compiled tier-3 default, no reviewer-specific override). The per-node `model` attribute exists in the spec (WG-042) but the dispatch path does NOT yet read it from the `.dot` node attributes — `ResolveModelPreference` uses bead labels, project config, and compiled defaults; it does NOT accept node-level `model=` from the `.dot` graph as a tier-0 input at dispatch. So there is no current path to "the review node uses Opus even when the implementer is Sonnet." | Two options: (A) **Convention (no build):** the captain sets `model:opus` as a bead label on any bead dispatched in a runner lane; bead-label tier-1 model overrides the implementer AND the reviewer. That forces Opus on both nodes — wrong, it defeats the runner saving. (B) **Small build:** wire the DOT node's `model` attribute as tier-0 in `ResolveModelPreference` (read `node.Model` and inject before tier-1 bead label). Then `standard-bead.dot` can set `model="opus"` only on the `review` node, leaving `implement` at Sonnet. This is a clean 1-bead change in `workloop.go` around the `resolvedModel, resolvedEffort := ResolveModelPreference(...)` call at line 2642, and a one-attribute addition to `standard-bead.dot`'s `review` node. |
| **Auto-pick of class from epic type / lane label (captain configures this, not per-bead)** | NO | No mechanism. Captain must read the lane plan and manually annotate each mission. If a lane has mixed work (some beads are clean drains, some are triage tasks), there is no way for the captain to say "this lane is runner except on run_failed beads." | Convention first: add a `class:` column to the captain's lane table in `captain-lanes.md`. Captain records the class assignment at lane-setup time; the mission front-matter reflects it. No new code. A future step: `harmonik crew start --class runner` sets `model:` + adds the escalation clause automatically at mission-write time. That is a medium-size build (agentlaunch / `start crew` path). |
| **C2 overruled items (idle reap-timer) respected by runner/planner split** | YES | The runner/planner split is about the crew orchestrator model, not reap. C2 overruled per-crew reap-timers; the runner class does not reintroduce them. The correct idle treatment (restart-to-small via TA1 keeper band) applies equally to both classes. No gap here. | — |
| **Failure-triage stays Opus (C2 bright line)** | DESIGNED, NOT ENFORCED | The leanfleet design says Sonnet runner crews must escalate all `run_failed` to the captain and NOT self-classify. This clause exists in STARTUP.md as a captain instruction for what to put in mission files. But (a) no active mission files have it, and (b) there is no daemon-level enforcement — the daemon will dispatch any crew regardless of whether it carries this clause. | Convention: captain writes "escalate to captain on ANY run_failed, do NOT self-classify" into every runner-class mission's operating instructions. This is already in STARTUP.md. No code change needed; gap is mission-file authoring discipline. |
| **Captain model knob** | NO | The captain always launches at Opus. `harmonik start captain` has no `--model` flag and no mission front-matter (it is not a crew). No knob exists today. README §5 table says "must stay Opus (planning/judgment/fan-out synthesis)." | Per operator intent: captain stays Opus. Not a gap to fill. |
| **WS#5 model-tiering — leanfleet items DONE vs open** | PARTIAL | See §3 below. | |

---

## 3. WS#5 (model-tiering) — done vs open

| WS#5 item | Done? | Notes |
|---|---|---|
| Mission schema `model:` field (spec) | YES | `specs/crew-handoff-schema.md` updated (LF-A, hk-6xiy) |
| Daemon injects `--model` into crew argv | YES | hk-9j3z, be9f7453 |
| Captain decision rule in STARTUP.md | YES | Sonnet=lane-drain, Opus=design/test/investigation |
| Sonnet ops-monitor crew | YES (archived) | Was live; mission archived — needs re-creation when next needed |
| Per-DOT-node `model` attribute dispatched at run time | NO | WG-042 in spec; not wired in `ResolveModelPreference` |
| Opus reviewer node for runner crews (the "final Opus review") | NO | Standard-bead.dot sets no `model` on the review node; all nodes default to Sonnet |
| Active mission files annotated with `model:` or `class:` | NO | None of paul/stilgar/irulan/leto/chani/gurney have `model:` set |
| Crew class abstraction (named type with bundled defaults) | NO | Not designed or built |
| Auto-pick from epic-type / lane label | NO | Not designed |
| Captain STARTUP.md "assign a model per lane" step | YES | In the lane-table template |

---

## 4. Recommended runner / planner mechanism

### Philosophy

Prefer convention + existing config over new code. One small build is justified (the Opus reviewer node) — everything else can land via mission authoring and templates.

### Concrete mechanism

**Step 1 — Convention (zero build, immediate):**
- Add `class: runner | planner` to the crew mission schema (additive optional field, no schema_version bump, same pattern as `model:`).
- Captain encodes class + model in the lane table in `captain-lanes.md` (a column already exists there by STARTUP.md convention).
- Add two boilerplate templates: `.harmonik/crew/missions/_TEMPLATE-runner.md` (pre-filled: `model: sonnet`, escalation clause, "do NOT self-classify" operating instruction) and `_TEMPLATE-planner.md` (`model: opus`, triage-allowed instruction).
- Update all active mission files (paul, stilgar, irulan, leto, chani, gurney) to add `model: sonnet` or `model: opus` as appropriate.

**Step 2 — Build (one bead, ~1 day): wire per-DOT-node model as tier-0 in dispatch**
- File: `internal/daemon/workloop.go` ~line 2642 (`ResolveModelPreference` call site).
- Pass `node.Model` (from the parsed DOT node attributes) as a new tier-0 argument.
- `internal/daemon/modelpreference.go`: add a tier-0 check — if `nodeModel != ""`, return it immediately before the bead-label walk.
- File: `internal/daemon/standard-bead.dot`: add `model="opus"` to the `review` node.
- Result: every bead dispatched in `dot` mode gets Sonnet on `implement` and Opus on `review` — the "final Opus quality gate" without changing any bead label or captain config.

**Step 3 — Optional build (medium): `harmonik crew start --class runner|planner`**
- At crew-start time, the `--class` flag auto-fills `model:` and the escalation clause into the mission file.
- Eliminates the manual authoring requirement; makes runner/planner a first-class CLI concept.
- This is a medium build (~2-3 beads). Defer until Step 1+2 are validated.

### Class mapping for current lanes

| Crew | Current work | Recommended class | Reason |
|---|---|---|---|
| **paul** | Daemon-reliability bugfixes (hk-sfvc) | planner | Root-cause triage required; failures need self-classification to avoid Captain round-trips |
| **stilgar** | Fleet-state model + ZFC wind-down | planner | Design decisions, pattern recognition across fleet state |
| **irulan** | GH bug-fix lane (reproduction-before-fix) | planner | Bug triage; must reproduce-first, classify failures |
| **leto** | Flywheel wiring (clean wiring beads) | runner | File-disjoint wiring work on a known plan; escalate failures |
| **chani** | Remote substrate validate + re-enable | planner | Investigation/root-cause lane; known gnarly edge cases |
| **gurney** | RC-prefix tail (cleanup beads) | runner | File-disjoint tail cleanup; clean beads, no triage needed |
| **logmine** | Log mining + digest | runner | Read-only analysis; mechanical; no triage decisions |
| **ops-monitor** | Fleet health checks | runner | Deterministic script loop; Sonnet clearly right |
| **admiral** | Hourly alignment audit | planner | Judgment-heavy; intermittent but needs strong model |

---

## 5. Move-forward / needs-more-design verdict

**Verdict: MOVE FORWARD — with a split.**

**Immediate (no build):**
- Annotate active mission files with `model:` now. The knob is already wired; this is a captain authoring task, not a code task. Unblocked today.
- Add the escalation clause to runner-class mission files. Unblocked today.
- Add `class:` column to `captain-lanes.md` tier-2 context so the assignment survives restarts.

**One build (small, high-value):**
- Wire per-DOT-node `model` as tier-0 in `ResolveModelPreference` + set `model="opus"` on the `review` node in `standard-bead.dot`. This delivers the "final Opus review for runner crews" the operator described without any per-bead configuration — it falls out of the default DOT workflow automatically. This is the highest-leverage change because it makes the quality gate structural rather than a convention.

**Needs-more-design (defer):**
- The `class:` field in mission schema and the `harmonik crew start --class` flag are worth building but not urgent — the convention + per-crew `model:` already handles it. Design and build after Step 1+2 are validated with a live audit (`harmonik usage` or Phase 0 join script from README §6).
- Auto-pick from epic-type is a future optimization; the captain already does this reasoning manually.

**Why not bigger?** The biggest spend is uptime of always-on Opus orchestrators, not per-bead model choice. README §5 confirms the fleet is already mostly Sonnet at the bead level. The Opus session cost is the orchestrator sessions (captain + complex crews). The runner/planner split addresses crew orchestrator spend, which is real and addressable — but the uptime/cadence levers (restart-earlier, fleet sleep-wake policy) likely save more absolute tokens than model-swapping crews. The runner/planner mechanism is complementary, not a substitute for those.

---

## 6. Key source pointers

- Leanfleet design (authoritative): `docs/plans/leanfleet/design.md`
- `crewstart.go` `readMissionModel` / `missionFrontMatter`: `internal/daemon/crewstart.go`
- `ResolveModelPreference` 4-tier logic: `internal/daemon/modelpreference.go:190`
- Per-DOT-node `model` attribute spec: `specs/workflow-graph.md §4 WG-042`
- Standard bead DOT (review node, no model set): `internal/daemon/standard-bead.dot`
- Captain model-tiering decision rule: `.claude/skills/captain/STARTUP.md` §4
- Active mission files (no `model:` set): `.harmonik/crew/missions/paul.md`, `stilgar.md`, `irulan.md`, `leto.md`, `chani.md`, `gurney.md`
- ops-monitor archived mission (`model: sonnet`): `.harmonik/crew/missions/_archive/2026-06-20/ops-monitor.md`
- hk-9j3z (per-crew model CLOSED): `br show hk-9j3z`
