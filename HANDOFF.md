<!-- PP-TRIAL:v2 2026-05-26 main — v62 (commit 95f89c3). Clean. P0 empty-pane ROOT CAUSE FIXED (waitAgentReady added to reviewloop implementer). 8 commits landed, 14 beads closed. V61 fixes validated (reviewer-cancel, close-without-impl both confirmed working). New issue: concurrent-session rate limit (hk-muvk9). -->

Roadmap: [ROADMAP.md](ROADMAP.md). Cross-project working-style rules: `~/.claude/CLAUDE.md`. Plans index: [plans/README.md](plans/README.md).

**Orchestrator rules (permanent directives): [docs/orchestrator-rules.md](docs/orchestrator-rules.md). Known workarounds: [docs/known-workarounds.md](docs/known-workarounds.md).**

ROLE. You are the orchestrator. Delegate substantively. Keep the main thread minimal.

LEARNING LOG (READ ON EVERY RESUME). `docs/orchestration-learnings.md` — friction-and-fix log. Read on `/session-resume`. Append new entries when you observe friction. Promote durable rules to `docs/orchestrator-rules.md` or `.claude/implementer-protocol.md`.

# Where we are (v62, 2026-05-26)

**Main at `95f89c3`** (origin parity, working tree clean). 8 commits landed across 3 harmonik batches.

## What v62 landed

- **P0 FIX (d8dd16e): `waitAgentReady` added to reviewloop implementer path.** Root cause: paste fired before Claude's REPL was ready. Fix mirrors the single-mode and reviewer-phase patterns. Empty-pane rate: 60% → 0%. Test: `TestScenario_ReviewLoop_ImplementerAgentReadyTimeout`.
- DOT workflow-graph validator (3a83fe2, 1110 lines)
- `harmonik graph validate` CLI subcommand (1f2118c, hk-voyf4)
- CP-031 beads-cli default skill enforcement (91e8dc0, hk-a8bg.31)
- Pane cleanup after killed/failed runs (8401b99, hk-e6mtt)
- CP-029 role default permissions (f2b4c71, hk-a8bg.29)
- Evaluator edge-case tests (0d840ce, hk-a8bg.59)
- CognitionMeta record §6.1.6 (94ecd1f, hk-a8bg.73)

## NEW ISSUE: Concurrent-session rate limit (hk-muvk9, P1)

With the empty-pane fix, agent_ready fires 100% of the time. But ~80% of concurrent dispatches (max-concurrent 5) still fail with `no_commit_during_implementer` — Claude starts, runs ~4.5 min, exits cleanly without committing. All failures are uniform-timed, suggesting an external rate limit. Workaround: use `--max-concurrent 2-3` to stay below the limit. Filed as hk-muvk9.

## Wave HOL-blocking bug (hk-n91y0, P1)

When a bead in a wave has bead-level blockers (deps still open), `br claim` fails and the daemon retries the same bead forever, starving all subsequent pending items. Discovered when hk-waj4b (blocked by hk-7okmx) HOL-blocked 7 other beads. Workaround: don't include dependency-blocked beads in wave batches.

## PRIORITY 1: DOT implementation chain

- hk-7okmx (T-IMPL-003 loader) — still open, failed twice (rate limit, not code)
- hk-waj4b (T-IMPL-004 daemon wiring) — blocked by hk-7okmx
- hk-qo9pq (T-IMPL-013 CLI dot mode) — blocked by hk-waj4b

## PRIORITY 2: Spec-corpus + quality work

Still open: hk-hqwn.37 (event schema_version), hk-24xn1 (queue submit wake), hk-rnsjs (claim-failure auto-close), hk-n91y0 (wave HOL-block fix), hk-a8bg.52 (effective skill set union).

## Files to open first

1. `HANDOFF.md` (this)
2. `docs/orchestrator-rules.md` — all permanent directives
3. `internal/daemon/reviewloop.go` — P0 fix landed here (waitAgentReady wiring)

## Plain-English glossary

- **hk-kunm4** — P0 empty-pane bug: paste before REPL ready (~60%). **FIXED** in v62.
- **hk-muvk9** — NEW: implementer runs but doesn't commit (~80% at concurrent 5). Likely Claude session rate limit.
- **hk-n91y0** — wave HOL-blocking: daemon retries blocked bead claim forever, starving pending items.
- **waitAgentReady** — gate that blocks paste delivery until Claude's REPL emits agent_ready event.
- **DOT** — workflow-graph-defined bead processes, replaces --review-loop.
- **`--wave`** — queue mode for concurrent dispatch; use when `--max-concurrent > 1`.

## No hard blockers requiring user input.
