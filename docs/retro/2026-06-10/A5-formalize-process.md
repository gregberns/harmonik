# A5 — Formalize Planning, Documentation, and Handoff Processes
**Date:** 2026-06-10  
**Scope:** Read-only analysis; no tracked-file edits.

---

## 1. PLANNING (kerf + beads)

### 1.1 Current state

- kerf is the declared priority source of truth (`kerf next` = canonical dispatch feed, per orchestrator-rules.md and CLAUDE.md).
- The spec-first jig pipeline (problem-space → decompose → research → spec → tasks → ready) is well-designed on paper.
- Works live on the bench at `~/.kerf/projects/{id}/{codename}/`; specs land in `specs/` on `kerf finalize`.
- Local jig overrides at `.kerf/jigs/` add a mandatory Validation/Acceptance Tests section (per the 2026-05-19 feedback that drove hk-37zy8-class bugs).
- Bead labels use `codename:` prefix for kerf-work attachment; `bead_filter` clauses in works control what `kerf next` surfaces.

### 1.2 Gaps and friction

| Gap | Evidence |
|-----|----------|
| `kerf next` does NOT factor `br` priority; P0 and P3 beads tie at identical scores | kerf-feedback/2026-05-26 KF-026-02 |
| Inter-work ranking is opaque; no operator-tunable priority on works | kerf-feedback/2026-05-26 KF-026-01 |
| Works without `bead_filter` clauses yield empty `kerf next`; agents bypass to explicit IDs | orchestrator-rules.md §KERF IS IN BETA |
| No formal decision rule for "when to create a kerf work vs file beads directly" — agents must guess | Absent from CLAUDE.md, AGENTS.md, orchestrator-rules.md |
| `kerf triage` mixes phantom suggestions with real drift; agents spend time evaluating false positives | orchestrator-rules.md §KERF BETA + REALIGNED |
| `kerf new --jig plan` and the multi-agent captain/crew model have never been formally reconciled — crew epics bypass the kerf work lifecycle entirely | No doc bridges the two |
| No explicit "kerf work graduation checklist" (when does a work-in-bench become a bead in the queue?) | Gap between kerf finalize and daemon dispatch |
| The `contrib-open` / aggregation work mode is approximated ad-hoc (kerf-beta-feedback.md 2026-06-01) — no formal convention in CLAUDE.md | kerf-beta-feedback.md |

### 1.3 Proposed formalized process

**Decision rule: kerf work vs direct beads**

| Situation | Path |
|-----------|------|
| New cross-cutting feature, new subsystem, spec change that touches ≥2 specs | `kerf new --jig spec` |
| Non-trivial bug with unknown root cause | `kerf new --jig bug` |
| Scoped feature with known design, ≤5 beads, no spec impact | File beads directly; attach to nearest kerf work via `codename:` label |
| Typo, one-line fix, doc cross-reference | File bead directly; skip kerf |
| Aggregation / open-contribution (tooling captures, feedback logs) | File beads directly with `contrib-open` label; no kerf work required |

**Kerf work graduation gate** (add to CLAUDE.md §Planning):
Before dispatching beads from a kerf work, ALL of these must hold:
1. `kerf square <codename>` passes.
2. `specs/` has the finalized spec (if jig=spec).
3. Every bead has a `codename:<name>` label (so `bead_filter` picks it up).
4. At least one scenario-test bead and one exploratory-test bead exist per the jig Validation section.

**Prioritization fix (interim, until upstream kerf fixes KF-026-01/02):**
- Always pass `kerf next --only=bead` to skip cleanup/warning noise.
- For cross-work prioritization, use `kerf next --work <codename>` or explicit IDs.
- Add a `## Current session priority` comment at the top of each wave JSON before submitting — operator-visible override record.

### 1.4 Concrete bead/doc proposals

| Action | Bead/doc | Notes |
|--------|----------|-------|
| Add "kerf vs direct-bead decision rule" to CLAUDE.md §Planning | bead: `chore` P2 | 10 lines; no spec impact |
| Add "graduation gate" to CLAUDE.md §Planning | same bead | |
| Add `kerf next` priority workaround to orchestrator-rules.md | same bead | |
| File upstream kerf issues KF-026-01/02 as a harmonik meta-bead | `question` P1 | track upstream resolution |
| Formalize `contrib-open` convention in CLAUDE.md | `chore` P3 | |

---

## 2. DOCUMENTATION

### 2.1 Current state

- AGENT_INDEX.md claims "every document reachable within two hops" — the KB structure (problems/goals/concepts/components/subsystems/ideas/log) is well-organized and the claim is mostly true.
- `specs/` is normative; code follows spec (spec-first rule is established and enforced via `specs/_registry.yaml`).
- docs/methodology/METHODOLOGY.md documents document conventions and status lifecycle.
- Operational docs (orchestrator-rules.md, known-workarounds.md, major-issue-fanout-protocol.md, postmortems/) are well-maintained.
- The captain boot runbook (`.claude/skills/captain/STARTUP.md`) is detailed and load-bearing; it was updated reactively after a bad boot incident.

### 2.2 Gaps and friction

| Gap | Evidence |
|-----|----------|
| AGENT_INDEX.md is substantially stale — last updated 2026-05-12 (v33 workflow-modes); it does not reflect crew system, named queues, captain/crew skills, validation-net, codex-harness, or any 2026-06-x work | AGENT_INDEX.md header + STATUS.md header |
| STATUS.md is even more stale (2026-05-12) and contains detailed decompose-to-tasks phase 0/1 content that is now historical noise; its "where to start next session" section points to HANDOFF.md which is now captain-format | STATUS.md |
| No doc covers the captain/crew operating model at the documentation (not skill) level — STARTUP.md is ops runbook, not conceptual doc | Missing |
| No doc maps which skills are mandatory vs optional at session start; agents must grep CLAUDE.md and piece together required reading | CLAUDE.md §Start here lists 3 docs then defers to orchestrator-rules.md |
| docs/ has grown organically — kerf-feedback/, audits/, bead-reassessment-2026-06-02/, qa-scratch/ etc. have no index | `ls docs/` shows ~30 top-level entries and subdirs, no top-level docs/INDEX.md |
| plans/ contains only one file (captain) — the plans/README.md "Done means..." convention referenced in orchestrator-rules.md is not findable from AGENT_INDEX.md | `ls docs/plans/` |
| Postmortems are not linked from AGENT_INDEX.md (only the 2026-06-09 one appears there) | AGENT_INDEX.md |
| Kerf works (bench artifacts) are not reflected in any doc map — a new agent cannot discover which kerf works exist or their status without running `kerf map` | No doc lists active works |
| The normative-spec-first rule is stated in CLAUDE.md but the enforcement mechanism (what happens when code diverges from spec) is only in specs themselves, not in an operational doc | |

### 2.3 Proposed formalized process

**Doc ownership rules (add to docs/methodology/METHODOLOGY.md):**

| Layer | Authority | Update trigger |
|-------|-----------|----------------|
| `specs/` | Normative, frozen IDs | Only via kerf finalize or explicit spec-amendment bead; never edited inline |
| `AGENT_INDEX.md` | Master map | Must be updated within the same commit batch as any new doc created under `docs/` |
| `STATUS.md` | High-level structural summary (NOT session state) | Update once per major milestone (e.g., new subsystem ships, phase completes); NOT per session |
| `HANDOFF.md` | Per-session live state | Written by `/session-handoff`; gitignored; never used as ground-truth |
| `docs/orchestrator-rules.md` | Permanent operational directives | Updated when a new hard rule is established; old rules annotated but not deleted |
| `docs/known-workarounds.md` | Incident-driven operational patches | Updated when a new recurring workaround is discovered; entries dated |
| `docs/postmortems/` | Post-incident analysis | One file per incident; linked from AGENT_INDEX.md within 24h of filing |
| `.claude/skills/captain/STARTUP.md` | Captain boot runbook | Updated when a boot anti-pattern is confirmed; not duplicated in HANDOFF.md |
| `~/.claude/projects/.../memory/` | Cross-session institutional memory | Updated by memory tool; topic files ≤200 chars in MEMORY.md index |

**AGENT_INDEX.md maintenance rule:**  
Every session that creates docs under any `docs/` subdirectory MUST update AGENT_INDEX.md in the same commit. If a session ends without doing so, the cleanup is a P2 chore bead filed before handoff.

**STATUS.md restructure:** Separate "current state" (the live portion, monthly-updated) from "historical" (everything pre-2026-06) into a `docs/historical/` archive. STATUS.md becomes a 2-section file: §Current Phase and §Decisions in force.

### 2.4 Concrete bead/doc proposals

| Action | Bead/doc | Notes |
|--------|----------|-------|
| Update AGENT_INDEX.md to reflect 2026-05-12 → today: crew system, named queues, validation-net, postmortems, kerf-feedback/ | `docs` P1 | High value; AGENT_INDEX claim is broken for any new agent |
| Archive STATUS.md pre-2026-06 content to `docs/historical/status-2026-05.md` | `chore` P2 | |
| Add `docs/concepts/captain-and-crew.md` (conceptual overview of captain/crew model) linked from AGENT_INDEX.md | `docs` P2 | |
| Add top-level `docs/INDEX.md` listing all subdirectories with one-line purpose | `chore` P3 | |
| Update docs/methodology/METHODOLOGY.md with doc ownership table above | `chore` P2 | |
| Add `docs/plans/README.md` "Done means..." convention (currently only referenced, not findable) | `docs` P2 | |

---

## 3. HANDOFF

### 3.1 Current state

**Single-agent path:**
- `~/.claude/skills/session-handoff/SKILL.md` drives HANDOFF.md writes: PP-TRIAL:v2 header, directives block (preserved verbatim), ≤40-line body, translations glossary, next step.
- `~/.claude/skills/session-resume/SKILL.md`: read HANDOFF.md + pointed files, check date/branch staleness, say-back ≤10 lines, proceed.
- HANDOFF.md is gitignored (correct — ephemeral state).
- orchestrator-rules.md is the durable rules layer, extracted from HANDOFF.md to keep session state lean.

**Captain/crew fleet path (NEW territory):**
- HANDOFF.md is written in captain-format (PP-TRIAL:v2, ORCHESTRATION DIRECTIVES block, per-crew lane state, open/next items).
- `.claude/skills/captain/STARTUP.md` is the boot runbook that EXTENDS the handoff (5-step boot: anchor → load context → ground-truth → reconcile crews → verify fleet).
- Crew missions live in `.harmonik/crew/missions/<crew>.md` (gitignored) — ephemeral per-crew context.
- The HANDOFF.md says "STARTUP.md is ground-truth, don't trust this handoff's live claims — measure." This is the right posture.

### 3.2 Gaps and friction

| Gap | Evidence |
|-----|----------|
| Single-agent handoff skill (session-handoff/SKILL.md) is unaware of the captain/crew fleet — it produces a HANDOFF.md that is correct for solo sessions but incomplete for multi-crew restarts | SKILL.md mentions nothing about crew state, crew missions, or fleet verification |
| No formalized crew mission template — the `example-handoff.md` in `.harmonik/crew/missions/` is a good scaffold but it's not linked from any skill or doc | `.harmonik/crew/missions/example-handoff.md` exists but undiscoverable |
| Crew context is LOST when a crew is stopped/restarted — the mission file is overwritten, and there is no "crew handoff" format equivalent to HANDOFF.md. The captain is the only continuity bearer | HANDOFF.md "liet/duncan/stilgar/chani" crew state is entirely in the captain's HANDOFF.md, not durable crew-side artifacts |
| HANDOFF.md regularly grows beyond 40 lines (current = 49 lines body) with per-lane state that should live in crew missions, not the captain handoff | Current HANDOFF.md observed at ~130 lines |
| memory/MEMORY.md is at 29.9KB against a 24.4KB limit — index entries are too long (warning in MEMORY.md itself); topic file splits exist but aren't being used aggressively enough | MEMORY.md over-size warning |
| No formalized rule for what belongs in MEMORY.md vs orchestrator-rules.md vs known-workarounds.md vs HANDOFF.md — agents must infer | Absent from METHODOLOGY.md and CLAUDE.md |
| The translations glossary in HANDOFF.md is manually maintained — easy to forget new codenames | Current HANDOFF.md has a translations section that is good but not enforced structurally |
| There is no "fleet handoff" skill — the captain's STARTUP.md handles boot but there is no symmetrical shutdown/handoff runbook for the captain role | No `captain/SHUTDOWN.md` exists |

### 3.3 Proposed formalized process

**Tiered handoff model (three layers):**

| Layer | What it is | Owner | Format |
|-------|------------|-------|--------|
| Captain HANDOFF.md | Fleet-level live state: daemon status, per-lane summary (1 line/lane), open operator decisions, translations | Captain | ≤50 lines; STARTUP.md boot reads it |
| Crew mission file | Per-crew context: epic, queue, ordered beads, operating constraints | Captain writes at lane-creation | `.harmonik/crew/missions/<crew>.md`; schema_version:1 |
| Crew progress doc | Crew's own ongoing findings (research, playbook, key file paths) | Crew writes to bench | `.kerf/works/<codename>/04-research/` or a gitignored `.harmonik/crew/<crew>-context.md` |

**Captain handoff rules (formalize in STARTUP.md §Handoff or a new `captain/SHUTDOWN.md`):**

1. Per-crew lane summary: one line per crew, format `- **<crew>**: <status 1 line> → <next action>`.
2. Open operator decisions: list ONLY items the human must decide; tag `⚠️ OPERATOR ACTION`.
3. Anomalies: any staged-debris or ref divergence, with investigation status.
4. Translations: every bead ID, codename, or jargon term that appears in the body.
5. Crew missions: before writing HANDOFF.md, refresh each crew's `.harmonik/crew/missions/<crew>.md` to match actual current state (so a crew restart gets accurate context).
6. Deploy procedure: HANDOFF.md must include the current corrected deploy procedure if it changed this session.

**Memory discipline (new rule for MEMORY.md):**

- Index entries MUST be ≤200 chars (one line).
- Detail goes in topic files under `memory/`.
- Each topic file is capped at 500 lines.
- When a topic becomes stale (>30 days since referenced), move to `memory/archive/`.
- Before writing to MEMORY.md, run: `wc -c ~/.claude/projects/.../memory/MEMORY.md` and warn if >20KB.

**Single-agent sessions (no fleet):**
The existing session-handoff/session-resume skills are adequate. The only addition:
- session-handoff skill should add a check: "if any crew missions exist (`ls .harmonik/crew/missions/`), include a one-line status per crew or note 'fleet not active'."

**Fleet sessions:**
Replace the single-agent `/session-handoff` with the captain SHUTDOWN sequence:
1. Run `harmonik comms recv --follow --json | head` to drain any pending messages.
2. For each active crew: update its mission file with current state + next bead.
3. Write HANDOFF.md per the rules above (≤50 lines).
4. Confirm `main == origin/main` (or note any divergence).
5. Note any banked worktree branches (`git branch --list 'bank/*' 'worktree-agent-*'`) for the next captain.

### 3.4 Concrete bead/doc proposals

| Action | Bead/doc | Notes |
|--------|----------|-------|
| Create `captain/SHUTDOWN.md` as the fleet-handoff counterpart to STARTUP.md | `docs` P1 | Current gap: shutdown is unstructured |
| Update session-handoff SKILL.md to add crew-fleet awareness (check for missions/, add 1-line fleet summary) | `chore` P1 | Low-diff; high value for single-agent → fleet transition |
| Formalize memory discipline rules in CLAUDE.md or METHODOLOGY.md | `chore` P2 | Prevents the recurrent MEMORY.md overflow |
| Add a `crew-context` doc convention (per-crew gitignored doc for durable findings) to crew-launch SKILL.md | `chore` P2 | Today's crew findings live only in the captain's HANDOFF.md |
| Formalize "what belongs where" table (HANDOFF vs orchestrator-rules vs known-workarounds vs MEMORY) in METHODOLOGY.md | `docs` P2 | |

---

## Summary of all proposed beads

| ID (to be minted) | Type | Priority | Title |
|---|---|---|---|
| — | chore | P2 | Add kerf-vs-direct-bead decision rule + graduation gate to CLAUDE.md |
| — | question | P1 | Track upstream kerf priority fixes (KF-026-01/02) |
| — | docs | P1 | Update AGENT_INDEX.md to reflect 2026-06 reality (crew, named-queues, validation-net, postmortems) |
| — | chore | P2 | Archive pre-2026-06 STATUS.md content to docs/historical/ |
| — | docs | P2 | Add docs/concepts/captain-and-crew.md |
| — | chore | P2 | Update docs/methodology/METHODOLOGY.md with doc ownership table |
| — | docs | P1 | Create .claude/skills/captain/SHUTDOWN.md (fleet handoff counterpart to STARTUP.md) |
| — | chore | P1 | Update session-handoff SKILL.md with crew-fleet awareness |
| — | chore | P2 | Formalize memory discipline (≤200-char index, topic-file cap, archive convention) |
| — | chore | P2 | Add crew-context doc convention to crew-launch SKILL.md |
| — | docs | P2 | Add "what belongs where" table to METHODOLOGY.md |
