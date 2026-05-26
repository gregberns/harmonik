<!-- PP-TRIAL:v2 2026-05-26 main — v61 (commit 6d10e3d). Clean. Audit session: 9 issues identified, 8 commits landed, 20 agents dispatched. Close-without-impl root cause FOUND AND FIXED (protocol contradiction). Reviewer context-cancel FIXED (stopDispatchCtx split). Empty-pane (~60%) observability landed but root cause still TBD — P0. HANDOFF restructured: permanent rules now in docs/orchestrator-rules.md. -->

Roadmap: [ROADMAP.md](ROADMAP.md). Cross-project working-style rules: `~/.claude/CLAUDE.md`. Plans index: [plans/README.md](plans/README.md).

**Orchestrator rules (permanent directives): [docs/orchestrator-rules.md](docs/orchestrator-rules.md). Known workarounds: [docs/known-workarounds.md](docs/known-workarounds.md).** Session audit: [docs/session-audit-2026-05-26.md](docs/session-audit-2026-05-26.md).

ROLE. You are the orchestrator. Delegate substantively. Keep the main thread minimal.

LEARNING LOG (READ ON EVERY RESUME). `docs/orchestration-learnings.md` — friction-and-fix log. Read on `/session-resume`. Append new entries when you observe friction. Promote durable rules to `docs/orchestrator-rules.md` or `.claude/implementer-protocol.md`.

# Where we are (v61, 2026-05-26)

**Main at `6d10e3d`** (origin parity, working tree clean). Audit session — 8 commits landed.

## What v61 landed

8 commits on main (process + daemon fixes from friction audit):

| Commit | Bead/Ref | Description |
|--------|----------|-------------|
| `2f5c652` | hk-4jipv | Bead Lifecycle section moved before Task Description in agent-task.md |
| `fbfa97c` | hk-n0l5a | 3 HANDOFF/CLAUDE.md instruction conflicts resolved |
| `61fc973` | hk-odgrp | Orchestrator delegation rules strengthened (HARD RULE + batch failure checklist) |
| `c4da134` | hk-vidp6 | L-021: 2-failure investigation gate added |
| `2b1c501` | hk-0dola | HANDOFF separated: permanent rules → docs/orchestrator-rules.md |
| `28ec000` | hk-0zzj4 | **ROOT CAUSE FIX: implementer-protocol.md br-close contradiction** |
| `62c4660` | hk-5cox8 | Observability: run_id + claude_session_id on agent_ready events |
| `34fb644` | hk-2o2i9 | **Reviewer context-cancel fix: stopDispatchCtx split from runCtx** |

## PRIORITY 1: Empty-pane ~60% (P0, hk-kunm4)

**This is the #1 blocker. Approach: REPRODUCE FIRST, then fix.**

Paste is delivered to tmux pane but Claude never starts processing. Observability step landed (agent_ready now carries run_id). Prior fix attempts (bracketed-paste, splash timing) all failed because they targeted hypotheses without reproduction.

**Investigation hypotheses (ranked by two independent reviews):**
- H1: Claude Max concurrent session limit (challenged — event log shows 56% scattered failure, not clean N-in-5 pattern)
- H2: `waitAgentReady` falls through on `context.Canceled` before REPL is input-ready
- H3: 750ms splash delay too short under concurrent CPU load
- H4: `agent_ready` fires before REPL stdin is actually connected

**Next steps (REPRODUCE FIRST):**
1. Dispatch agents to write a reproduction test that reliably triggers the empty-pane failure
2. Use the new agent_ready/agent_ready_timeout events in events.jsonl to correlate which runs fail
3. Only after reproduction is reliable, dispatch fix agents targeting the confirmed root cause

## PRIORITY 2: DOT implementation chain

DOT workflow-graph is the near-term endgame — replaces `--review-loop` with graph-defined bead processes. Once DOT is working, generate a DOT template that ensures implementation quality.

Unblocked beads in the DOT chain:
- hk-7okmx (T-IMPL-003 loader) — unblocked, validator landed
- hk-qo9pq (T-IMPL-013 CLI `harmonik run --workflow-mode dot`)
- hk-voyf4 (T-IMPL-014 CLI `harmonik graph validate`)
- hk-y48vs (T-IMPL-015 review-loop.dot fixture)

## PRIORITY 3: Spec-corpus + quality work

- hk-a8bg.29 (role default permissions), hk-a8bg.70 (DelegationPath)
- hk-hqwn.37 (event schema_version), hk-a8bg.31 (Beads-CLI default skill)

## What was fixed this session (context for validation)

1. **Close-without-impl (~80%) — ROOT CAUSE FIXED.** `implementer-protocol.md` told agents "you own br close" while `agent-task.md` said "DO NOT." Protocol now says daemon-owned. Agent-task.md Bead Lifecycle moved before Task Description. Next batch of harmonik dispatches should show dramatically lower close-without-impl rate — validate this.

2. **Reviewer context-cancel — FIXED.** `CancelOnQueueDrain/Exit` now cancel `stopDispatchCtx` (stops dispatching new beads) instead of `runCtx` (which killed in-flight goroutines). Reviewers should now survive sibling bead completion. Validate with `--wave --max-concurrent 3+`.

3. **Instruction conflicts — FIXED.** HANDOFF/CLAUDE.md no longer contradict on review-loop default, stream-default, or max-concurrent.

## Dispatch volume (HARD RULE — user-ordered 2026-05-26)

**Dispatch 7-10 concurrent agents/beads minimum, not 3-4.** Fill all non-conflicting slots. If fewer than 7, explain why (dependency, file conflict, etc.). This applies to both harmonik batches and sub-agent dispatches.

## Files to open first

1. `HANDOFF.md` (this)
2. `docs/orchestrator-rules.md` — all permanent directives live here now
3. `docs/session-audit-2026-05-26.md` — full audit findings
4. `.claude/implementer-protocol.md` — br-close fix landed here
5. `internal/daemon/workloop.go` — stopDispatchCtx split landed here

## Plain-English glossary

- **hk-kunm4** — empty-pane bug: tmux pane has prompt but Claude never processes (~60%, P0)
- **hk-0zzj4** — close-without-impl fix: protocol contradiction that caused ~80% failure (FIXED)
- **hk-2o2i9** — reviewer context-cancel: shared runCtx killed sibling reviewers (FIXED)
- **stopDispatchCtx** — new context layer: signals "stop dispatching" without killing in-flight work
- **DOT** — workflow-graph-defined bead processes, replaces --review-loop
- **`--wave`** — queue mode for concurrent dispatch; use when `--max-concurrent > 1`

## No hard blockers requiring user input.
