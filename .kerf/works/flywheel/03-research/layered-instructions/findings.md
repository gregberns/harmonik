# Research/Design — Layered instructions architecture

> Component: `layered-instructions`. Round-3. Source: sub-agent (opus), 2026-05-30.

## TL;DR
- **5 layers** stack above-vs-below a single `cache_control` breakpoint: L0 Identity+Safety / L1 Project Goal / L2 Operational / L3a Skill Index — all ABOVE; L3b Skill bodies (lazy tool_result) / L4 Digest / L5 Conversation — BELOW. Skills live on disk and arrive as tool results (cacheable via last-user-block lookback).
- **Project goals live in a human-edited `./.flywheel/goals.md`**, sealed at process start; **kerf remains the runtime ranking source** (`kerf next` per batch-composition decision); operational params sealed at `harmonik supervise start`.
- **The 4096-token cache floor problem:** the naive prefix (~750 tok) is below Opus's min-cacheable → DELIBERATELY BULK it with skill index + glossary + tool schemas + project preamble (~3000-4500 tok). Every line is load-bearing prefix content the agent benefits from having always-on.

## Prompt structure (one cycle, one model call)
```
+========================================================+
| L0  IDENTITY + SAFETY                  (immutable)     |
|     "You are flywheel. No force-push. Untrusted-input  |
|      boundary. Loop = supervise a harmonik run."       |
+--------------------------------------------------------+
| L1  PROJECT GOAL                       (per-restart)   |
|     contents of .flywheel/goals.md                     |
|     "Active epic: codename:dot-cascade.                |
|      Pause-on-signal: .flywheel/PAUSE present."        |
+--------------------------------------------------------+
| L2  OPERATIONAL CONTRACT               (per-session)   |
|     "max-concurrent=4. priority-source=kerf next.      |
|      Pre-screen for landed. Never re-dispatch          |
|      failed-twice w/o investigation."                  |
+--------------------------------------------------------+
| L3a SKILL INDEX + GLOSSARY + TOOL SCHEMAS (per-restart)|
|     "Skills: triage-failure, investigate-run,          |
|      compose-batch, escalate, reconcile-state.         |
|      Read via read_skill(name). Glossary: hk-XXX=..."  |
+========================================================+
|              <<< cache_control breakpoint >>>          |
+========================================================+
| L3b SKILL BODY (lazy, tool_result)     (per-fetch)     |
| L4  STATUS DIGEST `harmonik digest`    (per-cycle)     |
| L5  CONVERSATION (tool calls / results)(per-turn)      |
+========================================================+
```

## Layer table
| # | Layer | Contents | Volatility | Position | Updated by |
|---|---|---|---|---|---|
| L0 | Identity+Safety | "you are flywheel", safety rails | immutable / process | above | developer (extension src) |
| L1 | Project Goal | active epic(s), pause signals | per-restart (weekly) | above | operator edits `.flywheel/goals.md` |
| L2 | Operational | max-concurrent, priority source, dispatch rules | per-session | above | `harmonik supervise start` flags |
| L3a | Skill Index + Glossary + Tool schemas | catalog + plain-English map | per-restart | above | developer + operator (skills dir) |
| L3b | Skill body | one skill's markdown | lazy (tool_result) | below, cacheable via lookback | author of skill markdown |
| L4 | Digest | running set / failures / notes | per-cycle | below | `harmonik digest` (pure Go) |
| L5 | Conversation | tool calls + results | per-turn | below | the agent |

## Project goal in `./.flywheel/goals.md` (not kerf-only)
Kerf knows *works* but not *focus* — "active epic X, defer Y until X drains" is operator intent, not derived ranking. A 10-20 line human-readable markdown file is the cheapest operator surface (no CLI, no schema). Format: top section names active epic(s) by kerf codename; second section names explicit deferrals; third section lists pause-signals (e.g. `.flywheel/PAUSE`). **Combined with kerf:** `goals.md` says *which works are active*; agent runs `kerf next --work codename:X` for ranked feed within focus. Best of both — kerf owns ranking, operator owns focus.

## Prioritization — defer to `kerf next` with logged overrides
Default: dispatch the top of `kerf next` after pre-screen. Override triggers (encode in `compose-batch.md` skill): top bead depends on a not-yet-merged bead not in the ready batch → skip; bead in an untested-workload class (flagged in HANDOFF.md) → skip; failed-twice this session → halt re-dispatch, queue investigation. Every override appends one line to `.flywheel/dispatch-log.jsonl`: `{ts, skipped_bead, reason, picked_instead}`. Cheap audit trail.

## Fat skills — trigger + fetch
Skills triggered by the L3a INDEX in the prefix (not inline). System-prompt index says: *"when you face a class of decision named below, call `read_skill(name)` before deciding; do not improvise."* Initial catalog: `triage-failure.md`, `investigate-run.md`, `compose-batch.md`, `escalate.md`, `reconcile-state.md`. Tool: `read_skill(name) → {content: string, sha: string}` (sha verifies same-skill across calls). Lazy fetch — same fetch result remains in conversation history → cache-hit on subsequent turns within the cycle; new cycle re-fetches if needed.

## Change-over-time (deliberately asymmetric)
- **L0 Safety** — extension version bump; reload = process restart.
- **L1 Goal** — operator edits `goals.md`; effective on next supervise restart (NOT mid-session — prevents goal-thrash).
- **L2 Operational** — sealed at `supervise start --flags`; change = restart.
- **L3 Skills** — live-reload (loaded lazily; an operator edit takes effect on the next `read_skill()`).
- **L4 Digest** — recomputed each cycle.
- **L5 Conversation** — agent.
Asymmetry is deliberate: **structural** layers (identity, goal, operational) seal per process; **procedural** layers (skills) live-reload. Matches where change actually lives: procedures > goals > operational shape > identity.

## Cache consequence — the 4096-tok floor problem
Naive size: L0 (10 lines) + L1 (15) + L2 (15) + L3a (10) ≈ 50 × ~15 tok = ~750 tok. **Opus min-cacheable = 4096 tok → naive prefix earns ZERO cache discount.** **Deliberately bulk the prefix** with load-bearing content:
1. **Full skill index with one-line descriptions** of each skill (not just names) — ~40 lines.
2. **Glossary**: plain-English translations of every internal code that appears below (`hk-XXX = bead`, `kerf work = epic codename`, `digest = computed status sheet`, etc. — matches CLAUDE.md plain-English rule) — ~30 lines.
3. **Tool schemas inline as readable contracts** (`read_skill`, `kerf_next`, `harmonik_dispatch`) — ~80 lines.
Combined ~210 × 15 ≈ 3150 tok. Add a 1000-tok project-context preamble (north-star phases, locked-in 2026-04-19 decisions, vocabulary) → comfortably over 4096. **Not padding — every line is load-bearing.**

## Who updates what (operator-facing)
| Layer | Who | How | Effective when |
|---|---|---|---|
| L0 Safety | developer | edit extension source | next deployment |
| L1 Goal | operator | edit `.flywheel/goals.md` | next supervise restart |
| L2 Operational | operator | `harmonik supervise start --flags` | this run only |
| L3 Skills | author | edit `.flywheel/skills/*.md` | next `read_skill()` call |
| L4 Digest | (none) | computed by `harmonik digest` | every cycle |
| L5 Convo | the agent | tool calls | continuously |
Operator surface = one editor for `goals.md`/skills + one terminal for `harmonik supervise start`. That's the whole control plane.

## Notes
- ZFC-consistent: disjoint membership, well-defined cache cut, lazy-skills are bounded fetches with sha-pinned identity.
- The 30-40-line instruction rule applies **per layer** (L0/L1/L2 each ≤40 lines); bulk in L3a is *reference material*, not instructions — distinction matters for Greg's locked rule.
