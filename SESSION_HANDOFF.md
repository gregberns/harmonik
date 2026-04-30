# Session Handoff

> Message to the next agent picking up this project. Overwritten at each session boundary; git history preserves prior handoffs.
>
> Written 2026-04-27 after a session that smoke-loaded BI into a project-local `.beads/` workspace, surfaced 11 findings (6 discipline-level, 5 pilot-level), patched discipline v0.3 → v0.4 and pilot v0.1.2 → v0.1.3, validated a clean reload under corpus prefix `hk`, and drafted the pilot review protocol that will gate the remaining 9 specs.

## Read this first

1. **`HANDOFF.md`** — structured handoff produced by `/session-handoff`. Run `/session-resume` to use it.
2. **`NEXT_AGENT.md`** — ordered instructions for the next session. Priority 1: draft AR pilot, then run the 3-reviewer protocol on it, then load.
3. **`STATUS.md`** — project state snapshot. 10/10 specs `reviewed`; BI loaded into `.beads/`.
4. **`TASKS.md`** — remaining decompose-to-tasks work.
5. **`docs/decompose-to-tasks/discipline.md` v0.4** — the durable decomposition discipline. New material: §2.10 mnem→assigned-ID rule, §2.12 corpus prefix `hk` decision, F11–F16 in §6 revision history.
6. **`docs/decompose-to-tasks/pilot-review-protocol.md` v0.1** — gates every pilot before load. 3 reviewer roles + synthesis + load gate.
7. **`docs/decompose-to-tasks/bi-pilot.md` v0.1.3** — corrected pilot. Read as the structural template for AR.
8. **`docs/decompose-to-tasks/bi-smoke-load-findings.md`** — full report of the 2026-04-27 smoke load.

## Summary: what landed this session (2026-04-27)

### BI smoke load against live `br` v0.1.45

First end-to-end execution of the decompose-to-tasks discipline against live Beads. Two iterations:

- **First load** under `--prefix bi`: 66 beads created, 56/61 attempted intra-spec `blocks` edges landed, 5 cycle rejections each surfacing a real pilot bug.
- **Reload** under `--prefix hk` (after discipline v0.3 → v0.4 + pilot v0.1.2 → v0.1.3 patches): 66 beads, 110 edges, zero cycle rejections, `br dep cycles` clean. Epic at `hk-872`.

### Discipline v0.3 → v0.4 (6 deltas, F11–F16)

- **F11** — step beads MUST NOT add explicit `blocks` to umbrella. Beads's `parent-child` is already a dep edge; explicit `blocks` plus `parent-child` produces a cycle. (§2.2 new clause.)
- **F12** — sensor↔impl edges are one-way. Sensor `blocks-on` impl, never the inverse. (§2.5 emphasis.)
- **F13** — bidirectional inline cites are a smell. If A inline-cites B AND B inline-cites A, surface to discipline author for resolution; usually one is informational. (§2.7 paragraph.)
- **F14** — mnemonic IDs (`bi-001`, `bi-030.s4`) vs Beads-assigned IDs (`hk-872.10`, `hk-872.37.4`). Load procedure maintains a mnem→assigned-ID map. (§2.10 new subsection + zsh implementation pattern.)
- **F15** — default priority `P2` accepted (Beads has no "unset" value; v0.3's claim was inaccurate). (§2.9.)
- **F16** — corpus loads into one `.beads/` workspace with prefix `hk`; spec scope is the `spec:<spec-id>` label. Per-spec workspaces rejected (cycle detection across 10 DBs needs multi-DB tooling we don't justify). (New §2.12.)

### bi-pilot v0.1.2 → v0.1.3 (5 deltas, F-pilot-1 through F-pilot-5)

- **F-pilot-1** — §7 tally arithmetic fixed. Actual: 40 first-class req beads (not 36; v0.1.2 dropped the 008a/010a/010b coalesce arithmetic), 8 schemas (not 7; BI v0.4.1 added `HarmonikWriteStatus`), total 66 (not 61). Multiplier ~1.53× (was 1.42×).
- **F-pilot-2** — removed bidirectional `bi-004 ↔ bi-027` edges. Neither requirement actually inline-cites the other in the BI body; pilot-author cycle bug.
- **F-pilot-3** — removed wrong-direction `bi-011 → bi-inv-001` and `bi-022 → bi-inv-003` edges. Sensor→impl is the right direction (already on the §3 sensor rows); impl→sensor was wrong-direction.
- **F-pilot-4** — removed redundant `bi-030.s1 → bi-030` and `bi-031.s1 → bi-031` step→umbrella edges. Parent-child encodes them; explicit `blocks` produces a cycle.
- **F-pilot-5** — added `bi-schema.harmonik-write-status` bead + edges from `bi-007` and `bi-010` per BI v0.4.1 §6.1 split. Updated §1 spec-version + spec-parent description count.

### Pilot review protocol v0.1

`docs/decompose-to-tasks/pilot-review-protocol.md` (~210 lines, 8 sections):

- §1 purpose, §2 when to run.
- §3 three reviewers — **Coverage** (every spec ID accounted for, tally adds), **Decomposition-quality** (faithful descriptions, sound coalesce/split judgment), **Reference** (cross-spec edges trace to real inline cites). Each reviewer has an explicit method checklist + output format.
- §4 synthesis — BLOCKER / MAJOR / MINOR severity tiers + patch decision rules.
- §5 load gate — `br dep cycles` clean is the final structural check.
- §6 subagent spawning — Task tool with worked invocation example.
- §7 limitations — protocol does NOT cover discipline-rule changes, spec correctness, or runtime ingestion validation (deferred to harmonik proper).

### Other state

- `.beads/` added to `.gitignore` at MVH.
- Working tree carries this session's edits to `discipline.md`, `bi-pilot.md`, `STATUS.md`, `SESSION_HANDOFF.md`, `TASKS.md`, plus new files `bi-smoke-load-findings.md`, `pilot-review-protocol.md`, `HANDOFF.md`.

## What's NOT done

1. **AR pilot draft** — the next concrete step. Use `bi-pilot.md` v0.1.3 as the structural template.
2. **Run pilot review protocol on AR** — first real exercise of the protocol on a fresh pilot.
3. **Per-spec pilots for the remaining 8 specs** (EM, EV, HC, CP, WM, PL, ON, RC). Order: small/simple first; RC second-to-last (first to exercise discipline §2.11 multi-file / retired IDs / large §8 taxonomy).
4. **Cross-spec cycle check across the corpus** — single `br dep cycles` after all 10 specs load. Single DB makes this trivial.
5. **Bulk promotion `draft → open`** via the readiness workflow (workflow itself not yet designed).
6. **Phase 1 implementation** — begins after the bead set is cycle-clean and promoted.

## Process lessons from this session

- **Smoke load IS a review.** Beads's cycle detector caught 5 real pilot bugs at load time. But 2 non-structural bugs (tally arithmetic, stale spec ref) only surfaced via re-reading. The pilot-review-protocol is the response: 3 parallel AI reviewers cover the non-structural bug classes.
- **Markdown parsing for validation is yak shaving.** User pushed back on writing an automated checker because (a) markdown is fragile to parse, (b) Beads catches most of what a script would, (c) the real validation logic belongs in harmonik's eventual task-ingestion subsystem, not in throwaway smoke-load helpers. Decision: rely on AI reviewers + Beads load gate; defer the script.
- **Mnemonic IDs are author aids only.** The biggest discipline gap pre-smoke-load was the assumption that `bi-001`, `bi-030.s4` etc. would be loadable IDs. Beads auto-assigns hierarchical IDs (`hk-872.10`, `hk-872.37.4`); the discipline now documents the translation explicitly.
- **First execution always finds bugs the rules didn't.** v0.3 → v0.4 added 6 findings nobody had thought to look for until the load actually ran. Same lesson likely applies to the protocol itself when AR is the first pilot to go through it.

## Critical context for continuing

- **Spec template v1.1 is normative.** No template edits without user sign-off.
- **All spec IDs FROZEN.** AR, EM, EV, HC, CP, WM, PL, ON, RC, BI — never renumber, never reuse retired IDs.
- **No kerf at this phase.** User paused kerf; resume when something's working.
- **Direct to main. No PRs for MVH.**
- **Don't `git commit` without explicit ask.** Working tree is the source of truth.
- **`.beads/` is gitignored.** Local DB per-environment; regenerable from JSONL.
- **Corpus prefix is `hk`.** Single `.beads/` workspace per discipline §2.12.

## What should NOT be re-opened

(Unchanged from prior handoffs.) Key items: 10 locked decisions from 2026-04-19 + 4 candidates from 2026-04-20/21; reconciliation taxonomy (11 categories + §8.12 action-mapping); direct-to-main; AGENTS.md canonical; CONSTITUTION.md; coverage targets; `depguard` v2 alone; three-tier `make check`; spec-template structure; ALL spec IDs; the BI-007 v0.4.1 status-enum reframe.

Plus added this session:
- Workspace prefix `hk` and single `.beads/` decision (discipline §2.12).
- Step→umbrella implicit-via-parent-child rule (F11).
- Sensor↔impl one-way rule (F12).
- Mnem→assigned-ID translation pattern (F14).

## Files worth knowing about

- `specs/` — 10 normative specs + `_registry.yaml`. All `reviewed`. Unchanged this session.
- `docs/foundation/spec-template.md` v1.1.
- `docs/decompose-to-tasks/{discipline.md, bi-pilot.md, bi-smoke-load-findings.md, pilot-review-protocol.md}`.
- `docs/reviews/2026-04-24-*-r{1,2}/` — ~63 reviewer outputs from prior session.
- `HANDOFF.md` — skill-formatted handoff (this session).
- `NEXT_AGENT.md` — ordered next-session instructions.
- `TASKS.md` — task tracking.
- `.beads/` — local Beads DB with BI loaded (66 beads under `hk-872`).

## Suggested session shape for the next agent

1. Read HANDOFF.md, STATUS.md, TASKS.md (≤3 minutes).
2. Read `docs/decompose-to-tasks/discipline.md` v0.4 — especially §2.10 + §2.12.
3. Read `docs/decompose-to-tasks/pilot-review-protocol.md` v0.1.
4. Draft `docs/decompose-to-tasks/ar-pilot.md` against `specs/architecture.md`, using `bi-pilot.md` v0.1.3 as the structural template.
5. Run the 3-reviewer protocol on the AR draft per pilot-review-protocol.md §6 (Task tool, three parallel subagents, output to `docs/reviews/<date>-ar-pilot-r1/`).
6. Apply review findings, then load AR's beads into the existing `.beads/` workspace (no `br init` — append to current DB; epic for AR will be a new top-level `hk-<suffix>`).
7. Verify `br dep cycles` clean (covers BI + AR union).
8. After AR validates the protocol, scale to EM next.

---

## Prior session: 2026-04-26

> Preserved for context. The prior handoff (covering the v0.4.x cross-spec coordination wave + decompose-to-tasks discipline iterations through v0.3 + BI v0.4.1 status-enum reconciliation) is available in git history.
