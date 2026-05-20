# Orchestration Protocol v2 — harmonik as the Default Dispatcher

**Status:** PROPOSAL (2026-05-20). Awaits user review before landing in `CLAUDE.md` / `HANDOFF.md` / `session-resume`.
**Goal:** Make `harmonik run --beads <ids>` the default dispatch mechanism for harmonik's own development. Target: ≥75% of substantive work flows main-agent → kerf → harmonik (not main-agent → sub-agent).

## Synopsis (≤25 lines)

Today: 6–7 beads have shipped through `harmonik run`. The rest went via inline sub-agent dispatch. Future agents don't know to default to harmonik because nothing in `CLAUDE.md` / `HANDOFF.md` / the session-resume skill makes it the default move — it's mentioned, not enforced.

The intended daily loop is:

1. **Main agent** decides what's important next; records priority via `br update --priority` and (when needed) `kerf pin` / `kerf work edit`.
2. **kerf** exposes the prioritized feed via `kerf next`.
3. **Main agent** picks a batch (≥2, typically 3–5) and dispatches: `harmonik run --beads id1,id2,... --max-concurrent N`.
4. **harmonik** runs each bead end-to-end: spawn claude → watch → commit → merge to main → push → close. No orchestrator intervention.
5. **Main agent** keeps moving while harmonik runs — queues the next batch, reviews completed work, files follow-ups, drains untriaged kerf items.
6. On harmonik exit: review outcomes, dispatch next batch. Sub-agent dispatch is the **exception**, used only when (a) harmonik itself is the thing being debugged, (b) the change is a single-line typo / cross-reference fix, or (c) the work spans a code/spec gap harmonik can't currently handle (e.g. a new untested workload class — see `2026-05-19-phase2-readiness-audit.md`).

The 75% criterion: count substantive commits (not hygiene/typo) per session; ≥3 of every 4 must carry a "Refs: hk-..." trailer landed by the daemon (visible in `git log --grep='Refs:' --format='%H %cn'` where committer is the agent identity).

The bottleneck for "stops being mentioned, becomes the default" is three text edits — see Section B. Once landed, the next session-resume invocation will start with `bv --robot-triage` + `kerf next` and propose a harmonik batch BEFORE any sub-agent.

---

## A. Workflow design — the canonical daily loop

1. **Read state.** `/session-resume` reads `HANDOFF.md`, the `ORCHESTRATION DIRECTIVES` block, and `docs/orchestration-learnings.md`. Translate every codename on first mention.
2. **Triage.** Run `bv --robot-triage` for the graph-aware top-N picks. Run `kerf next` for the kerf-managed prioritized feed. The two views are complementary: bv ranks by graph metrics (PageRank, unblocking fan-out); kerf ranks by work-attachment + momentum.
3. **Priorities are recorded in beads.** `br update <id> --priority <0-4>` is the authoritative write. P0 = unblock-the-project; P1 = next-meaningful-feature; P2 = useful-soon; P3+ = backlog. Main-agent owns the decision; never defers to user for routine priority calls (cross-project memory `feedback_br_ownership`).
4. **Kerf is kept in sync via `kerf work edit --bead-filter` / `kerf pin`.** When a bead doesn't surface in `kerf next`, the fix is at the work-attachment layer, not by hand-picking IDs. Multi-matched beads get a `kerf pin <work> <bead>`. Untriaged beads get `kerf triage --ack` after each session.
5. **Choose the batch.** Take the top 3–5 beads from `kerf next` (or `bv --robot-triage`) that (a) are P0/P1/P2, (b) are not blocked by an in-flight bead, (c) are NOT in the untested-workload classes listed in `HANDOFF.md` §"Three caveats" (priority-sensitive routing; >1 concurrency; code-touching if not yet probed). Mixing 1 code-touching + 2 docs is fine if probes have passed.
6. **Dispatch.** `harmonik run --beads id1,id2,... --max-concurrent N [--context "..."] [--review-loop]`. `N=2` is the validated ceiling until the parallelism probe lands. Run in background; do NOT block on it inline.
7. **While harmonik runs**, the main agent does NOT wait. It (a) drafts the next batch's candidate list, (b) drains `kerf triage` untriaged items, (c) files follow-up beads observed from prior runs, (d) reviews recently-merged commits for the per-commit-reviewer gate, (e) reads context-window-cheap docs.
8. **On harmonik exit**, inspect: exit code (0 = success / 1 = paused-by-failure / 2 = unexpected); `git log --oneline -N` for landed commits; bead statuses via `br list --status=closed --limit 10`. Run reviewer agent on any load-bearing commit (per HANDOFF v48 directive).
9. **Failure handling.** Exit code 1 → read the paused queue.json, classify the failing bead. (a) Flake / transient (network, lock contention) → re-dispatch single bead via `harmonik run <id>`. (b) Genuine bug in the bead's work → fix-up sub-agent on the worktree branch. (c) Bug in harmonik itself → fall back to sub-agent dispatch for THIS bead + file an hk-... bug bead. Document which class in the post-mortem.
10. **The 75% rule.** Each session ends with a short tally: substantive commits this session, of which N landed via `harmonik run` (committer identity / `Refs:` trailer). Target: N/total ≥ 0.75. Trivial typos and hygiene-only commits don't count. Sessions that miss the target log a one-line reason ("harmonik bug in the loop", "untested workload class", "user check-in-required spec change") in the session-handoff.
11. **The rare exception.** Sub-agent dispatch is justified when: (a) the bead is a fix to harmonik itself in a path that breaks harmonik dispatching; (b) the change is ≤2-line cross-reference fix and a 2-minute round-trip via harmonik isn't worth the ~30s+ daemon overhead; (c) the work touches an untested workload class (per `2026-05-19-phase2-readiness-audit.md`) and the probe hasn't passed. Anything else routes through harmonik by default.

---

## B. Specific text changes (PR-style)

### B.1 — HANDOFF.md ORCHESTRATION DIRECTIVES — add HARD-RULE block

Insert immediately after the `STREAM-NOT-WAVES (HARD RULE).` paragraph, before the `PHASE 2 IS UNBLOCKED` line:

> **HARMONIK IS THE DEFAULT DISPATCHER (HARD RULE, v51). Substantive work routes through `harmonik run --beads <ids>` unless an exception applies.** The intended daily loop: `bv --robot-triage` → `kerf next` → pick batch of 3–5 → `harmonik run --beads id1,id2,... --max-concurrent N` → while it runs, queue next batch / drain triage / file follow-ups → on exit, review + dispatch next batch. Target: ≥75% of substantive commits per session land via `harmonik run` (committer identity / `Refs:` trailer in `git log`). The three exceptions: (a) the bead is a bug-fix to harmonik itself in code that breaks dispatch; (b) ≤2-line typo/cross-reference fix where ~30s daemon overhead isn't worth it; (c) untested workload class per the readiness-audit caveats (priority-sensitive; max-concurrent>1 before probe; code-touching before probe). Sub-agent dispatch is otherwise the WRONG move. If you find yourself reaching for the Agent tool on a 4th task in a row, STOP — batch them and run `harmonik run --beads`. See `docs/orchestration-protocol-v2.md` for the full design.

### B.2 — CLAUDE.md — new section "Daily loop (canonical)" after "Start here"

Insert before `## Planning with kerf`:

```markdown
## Daily loop (canonical)

**`harmonik run` is the default dispatcher for this project's own development.** The intended loop:

1. `bv --robot-triage` and `kerf next` — surface the prioritized work.
2. Pick a batch of 3–5 beads from the top of the feed (skip the untested-workload classes documented in `HANDOFF.md` §"Three caveats" until the probes land).
3. `harmonik run --beads id1,id2,... --max-concurrent N` — run in background; the daemon spawns claude, watches for completion, commits, merges to main, pushes, and closes each bead.
4. While harmonik runs: queue the next batch, drain `kerf triage` untriaged items, file follow-ups from prior runs, review recently-merged commits.
5. On exit: review outcomes, dispatch next batch.

**Sub-agent dispatch (via the Agent tool) is the EXCEPTION**, justified only when (a) you're fixing harmonik itself in a code path that breaks dispatch, (b) the change is ≤2 lines of typo/cross-reference cleanup, or (c) the work touches an untested workload class (see `docs/kerf-feedback/2026-05-19-phase2-readiness-audit.md`).

Target: ≥75% of substantive commits per session land via `harmonik run`. If sub-agent dispatch is creeping above 25%, stop and audit why — that's a signal harmonik has new friction or you've drifted into batch-mind.

Full design: `docs/orchestration-protocol-v2.md`. The kerf workflow below remains the planning surface for non-trivial NEW work (spec drafts, plan epics); kerf hands off beads, harmonik executes them.
```

### B.3 — `session-resume` skill — append a "First moves" paragraph

The skill currently lives at `~/.claude/skills/session-resume/SKILL.md` (user-global). Proposed addendum (append after the existing single-paragraph body):

```markdown
## First moves on harmonik (project-specific)

For the harmonik project specifically: after reading HANDOFF.md, the FIRST tool calls in the working phase should be `bv --robot-triage` and `kerf next` — in parallel. Then propose a harmonik dispatch batch (3–5 beads, `--max-concurrent 2` until parallelism probe passes). Do NOT spawn a sub-agent for the first task of the session unless an exception applies (per `docs/orchestration-protocol-v2.md` §B.1). This is the project-level default; CLAUDE.md's "Daily loop (canonical)" section is the authoritative version.
```

(If we want this scoped to harmonik only, an alternative is to create a project-local skill at `/Users/gb/github/harmonik/.claude/skills/harmonik-resume/SKILL.md` and reference it from the project CLAUDE.md. The user-global session-resume skill stays generic.)

---

## C. kerf-surface check

Ran `kerf next` and `kerf triage` at 2026-05-20.

- **`kerf next` IS surfacing a coherent prioritized list.** Top 9 items are correctly ranked: 4 P1 phase-2-completion beads (hk-8jh26, hk-rp48p, hk-ly4w5, hk-44w19), then handler-pause + claude-hook-bridge follow-ups. This is dispatchable as-is.
- **156 untriaged beads is too many** — `kerf next` only sees attached beads, so two-thirds of the corpus is invisible to the prioritized feed. The fix is NOT new works; it's `kerf triage --ack` after re-running pinning + filter edits. Many "Untriaged" items are kerf-upstream-bug beads (e.g. hk-43ate, hk-5py8k, hk-51ivc, hk-2pwgv) that have NO kerf work to attach to — they're feedback ON kerf. Proposed fix: create a `kerf-upstream` work with `bead-filter 'label=kerf-upstream'` so those surface in `kerf next` as a coherent stream.
- **5 multi-matched beads** need `kerf pin` decisions; mechanical, do in next session.
- **2 empty works** (`phase-3-dot`, `workflow-modes`) — filter intent-correct but matches zero open beads. Acceptable for now.
- Feedback filed at `docs/kerf-feedback/2026-05-20.md` (new file, NOT modifying 2026-05-19.md).

---

## D. Priority audit

**P1 list (7 beads):**

- `hk-judtf` — Plan 009 umbrella. STATUS DRIFT: plan is complete per HANDOFF v51. Should be CLOSED, not P1-open. **Propose:** `br close hk-judtf -r "Plan 009 complete v51, all 6 child beads merged."`
- `hk-ux915` — Audit half-built-system terminology drift + Done-means convention. Already partially landed (plans/README.md was updated v49). **Propose:** confirm scope, close if substantively done; otherwise demote to P2.
- `hk-5mjrs` — bead-label convention inconsistent. Hygiene, not P1. **Propose:** demote to P2; not a project-direction blocker.
- `hk-8jh26` — `harmonik run` hang/exit-code/silent-overwrite. **CONFIRMED P1.** This IS the harmonik-self-bug class that breaks the "dispatch-via-harmonik" loop. Top dispatch candidate.
- `hk-rp48p` — Daemon claim ignores priority order. **CONFIRMED P1.** Blocks priority-sensitive auto-dispatch — directly relevant to this v2 protocol. Top dispatch candidate.
- `hk-51ivc`, `hk-kx498` — kerf init bugs. Important but kerf-upstream; we file feedback, not fix. **Propose:** demote to P2 (file-feedback work, not harmonik-codebase work).

**P0 list:** EMPTY. That itself is a signal — either P0 is over-conservative or the truly-critical work has all landed (Phase-2 dogfood validated v51 supports the latter). Acceptable.

**Reranking summary:** keep `hk-8jh26` and `hk-rp48p` at P1 (they're the harmonik-self-fixes that unlock the daily loop); demote/close the other 5. Net result: P1 surfaces to 2 beads, both true blockers of the v2 protocol.

---

## E. "Done means…" criterion for this design effort

**Observable acceptance:** the NEXT `/session-resume` invocation, in a fresh context, opens HANDOFF.md, then makes its FIRST two tool calls `bv --robot-triage` and `kerf next` (in parallel), then proposes a harmonik dispatch batch of 3–5 beads via `harmonik run --beads ...` BEFORE invoking the Agent tool for any sub-agent dispatch. Verified by reading the session transcript: tool-call sequence must show triage-then-harmonik, not triage-then-Agent.

Secondary observable: at the next `/session-handoff`, the tally line reports N substantive commits / M via harmonik with M/N ≥ 0.75, OR the handoff explicitly notes which exception applied to each non-harmonik commit.

---

## Files modified / created by THIS proposal

- NEW: `docs/orchestration-protocol-v2.md` (this file) — design.
- NEW: `docs/kerf-feedback/2026-05-20.md` — kerf-surface friction observed today.
- DEFERRED (awaits user approval): `HANDOFF.md` §ORCHESTRATION DIRECTIVES — add §B.1 HARD-RULE block.
- DEFERRED (awaits user approval): `CLAUDE.md` — add "Daily loop (canonical)" section per §B.2.
- DEFERRED (awaits user approval): `~/.claude/skills/session-resume/SKILL.md` OR new project-local skill per §B.3.

Main agent applies the deferred changes once reviewed.
