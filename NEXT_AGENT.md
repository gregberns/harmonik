# Instructions for the Next Agent

> Read this first. Short on purpose.

## Where the project is (end of 2026-04-27 session)

**All 10 normative specs are `reviewed` and ID-FROZEN.** No spec work is gating the next phase.

**This session landed:**
- BI smoke-load against live `br` v0.1.45. Two iterations (first under `--prefix bi`, second under `--prefix hk` after patches): 66 beads, 110 intra-spec `blocks` edges, zero cycles. State preserved in `<repo>/.beads/`, epic `hk-872`.
- Discipline v0.3 → **v0.4** (6 deltas F11–F16). New material: §2.10 mnem vs Beads-assigned-ID rule; §2.12 corpus prefix `hk` single-DB decision.
- bi-pilot v0.1.2 → **v0.1.3** (5 deltas F-pilot-1..5). Cleaned up tally arithmetic, three pilot bug classes (bidirectional cycles, impl→sensor wrong direction, step→umbrella redundant edges), and the BI v0.4.1 schema split (added `bi-schema.harmonik-write-status`).
- `bi-smoke-load-findings.md` (new) — full report of the 11 findings.
- `pilot-review-protocol.md` v0.1 (new) — 3-reviewer parallel pass + load gate. Gates every pilot.
- `.beads/` added to `.gitignore`.

## What to do next (priority order)

### 1. Draft AR pilot

Create `docs/decompose-to-tasks/ar-pilot.md` against `specs/architecture.md` (52 reqs, mostly declarations — small/simple shakedown for the protocol).

Use `docs/decompose-to-tasks/bi-pilot.md` v0.1.3 as the structural template — same section layout (§1 spec under decomposition, §2 per-requirement table, §3 sensor/invariant table, §4 schema/error-taxonomy table, §5 cross-spec edge summary, §6 optional infrastructure, §7 tally, §8 rough edges, §9 revision history).

Apply discipline v0.4 rules. Pay special attention to:
- §2.10 mnem→assigned-ID rule (mnemonic IDs in the pilot are author plan-level; Beads assigns IDs at create time)
- §2.12 prefix `hk` (don't `br init` again — append to existing `.beads/`)
- §2.2 F11 (step beads do NOT add explicit `blocks` to umbrella; parent-child encodes the dep)
- §2.5 sensor↔impl one-way (sensor blocks-on impl, never inverse)
- §2.7 bidirectional inline cites are a smell (resolve by reclassifying one side, never emit both edges)

### 2. Run 3-reviewer protocol on AR draft

Per `docs/decompose-to-tasks/pilot-review-protocol.md` §6, spawn three subagents in parallel via the Task tool:

- **Coverage reviewer** (subagent_type: Explore) — verify every numbered ID in `specs/architecture.md` is accounted for in the pilot; check tally arithmetic; check spec-version reference is current.
- **Decomposition-quality reviewer** (subagent_type: general-purpose) — read sample beads against the spec; judge coalesce/split decisions; flag wrong descriptions or unsound judgments.
- **Reference reviewer** (subagent_type: Explore) — walk every cross-spec edge in the pilot; verify it traces to a real inline cite in the spec body; flag invented or missed edges.

Each writes output to `docs/reviews/2026-MM-DD-ar-pilot-r1/<reviewer>.md`.

Synthesize: apply BLOCKER findings (must-fix), apply MAJOR findings (strongly suggest, document if rejected), apply MINOR at discretion. Re-run the relevant reviewer if patches restructure the bead set.

### 3. Load AR into existing `.beads/`

No `br init` — the workspace already exists from the BI load (prefix `hk`, epic `hk-872`). Append AR by creating a new spec-parent epic via `br create --type epic --parent <none>` (top-level), then loading children with `--parent <ar-epic-id>`. Maintain a per-spec mnem→assigned-ID map per discipline §2.10.

After load: `br dep cycles` (now covers BI + AR union — must remain clean).

### 4. Scale to remaining 8 specs

Order: EM → EV → HC → CP → WM → PL → ON → RC. RC is second-to-last because it's the first spec to exercise discipline §2.11 (multi-file: `specs/reconciliation/{spec,schemas}.md`; retired RC-INV IDs; 11-category §8 taxonomy; cognition tags). Run RC second-to-last so any §2.11 fixes propagate to the last spec only.

If a reviewer in any pass finds a discipline bug class the v0.4 protocol missed, **do not propagate it across pilots** — patch discipline v0.4 → v0.5 first, then continue.

### 5. Cross-spec cycle check

After all 10 specs loaded, single `br dep cycles` covers the whole union (single DB). The known-OK pattern is BI ↔ EM (BI blocks on EM; EM does not block on BI). Other surprises possible.

### 6. Phase 1 implementation gate

Once cycle-clean, the bead set IS the Phase 1 implementation backlog. Promote `draft` → `open` per the readiness workflow (workflow itself not yet designed — that's a Phase 1 design dependency).

## Hard rules (unchanged)

- **Spec template v1.1 is normative.** No template edits without user sign-off.
- **All spec IDs FROZEN.** Never renumber. Never reuse retired IDs.
- **No kerf for now.** User paused it.
- **Direct to main. No PRs for MVH.**
- **Do NOT `git commit`** without explicit ask. Working tree is the source of truth.
- **`.beads/` is gitignored.** Don't commit it; regenerable from JSONL.
- **Corpus prefix is `hk`.** One workspace, one DB.

## Process notes from this session

- **Smoke load IS partial review.** Beads's cycle detector caught 5 of 11 BI findings at load. The other 6 (tally arithmetic, stale spec ref, decomposition judgment) needed re-reading by hand. The 3-reviewer protocol covers the latter class for AR onward.
- **Don't write a markdown-parsing checker.** Markdown tables are brittle to parse and Beads catches most structural bugs anyway. Real validation logic belongs in harmonik's eventual task-ingestion subsystem (Phase 1+), not in throwaway smoke-load helpers.
- **Mnemonic IDs are author aids only.** Pilot doc IDs (`bi-001`, `bi-030.s4`, `bi-schema.intent-log-entry`) are NOT what Beads creates. Live Beads assigns hierarchical IDs (`hk-872.10`, `hk-872.37.4`). The load procedure maintains a mnem→assigned-ID map at load time.

## What should NOT be re-opened

(Unchanged from prior handoffs.) Plus added this session:
- Workspace prefix `hk` and single `.beads/` decision (discipline §2.12).
- F11–F16 disposition (step→umbrella implicit, sensor↔impl one-way, bidirectional cite disambiguation, mnem→assigned-ID translation, default priority P2 accepted).
- The decision to defer automated checker until harmonik's ingestion subsystem implementation.

## Orient yourself in this order

1. This file (you are here).
2. `HANDOFF.md` — skill-formatted handoff. Run `/session-resume` to use it.
3. `SESSION_HANDOFF.md` — prose form of the same.
4. `STATUS.md` — project state snapshot.
5. `TASKS.md` — task list and remaining work.
6. `docs/decompose-to-tasks/discipline.md` v0.4 — read fully; pay attention to §2.10 + §2.12.
7. `docs/decompose-to-tasks/pilot-review-protocol.md` v0.1 — read fully.
8. `docs/decompose-to-tasks/bi-pilot.md` v0.1.3 — structural template for AR.
9. `docs/decompose-to-tasks/bi-smoke-load-findings.md` — context for why the patches landed.

## Working tree state

Lots of uncommitted changes from this and prior sessions. Per session rule: no commits without explicit ask, working tree is the source of truth. `git status` will show specs (unchanged this session), updated `discipline.md` / `bi-pilot.md` / `STATUS.md` / `TASKS.md` / `SESSION_HANDOFF.md` / `NEXT_AGENT.md` / `.gitignore`, plus new `bi-smoke-load-findings.md` / `pilot-review-protocol.md` / `HANDOFF.md`. Plus `.beads/` (gitignored) with BI's loaded state.

Good luck.
