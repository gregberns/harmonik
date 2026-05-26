# Session Audit: v59-v60 Friction Analysis (2026-05-26)

Produced by a 9-agent parallel investigation of session b218b8dc (v60) plus surrounding sessions (e121221f/v58, d41c243c/v59), HANDOFF.md structure, daemon code, bead health, kerf feedback, and orchestration learnings.

---

## Executive Summary

Session v60 dispatched ~25 bead slots across 5 batches and landed 8 commits — a **32% hit rate**. The two systemic failures (empty-pane ~60%, close-without-impl ~80%) are multiplicative: theoretical throughput floor is ~8% per slot. The close-without-impl issue has a **known root cause** (contradictory instructions) and a ~5-line fix. The empty-pane issue has several contributing factors but no single smoking gun. Neither issue has a tracking bead, making them invisible to kerf and bv.

---

## Issue 1 (P0): Implementers close beads without committing (~80%)

### Root cause: CONFIRMED — contradictory instructions

- `implementer-protocol.md` line 104: **"the agent (you) owns `br close`... Close as you go."**
- `agent-task.md` lines 366-371: **"DO NOT run `br close`... The daemon owns all bead lifecycle transitions."**

The protocol doc wins because it's loaded as part of the CLAUDE.md instruction chain (always-present context). The agent-task.md instruction requires the implementer to read a file — and by the time they do, they've already internalized "I own br close" from the protocol.

### Compounding factor: terse bead descriptions

When the bead body is empty, `claudelaunchspec.go` line 247-248 falls back to the title. A one-line title gives implementers almost nothing to act on. They conclude "already done / subsumed," run `br close`, and exit.

### Fix (immediate, ~5 lines)

1. **Remove `br close` ownership from `implementer-protocol.md` lines 102-106.** Replace with:
   ```
   ## Bead-close ownership (DAEMON-OWNED)
   DO NOT run `br close` or any bead status transition. The daemon closes beads
   after verifying your commit landed and passed review. Your job: implement,
   commit, and `/quit`.
   ```
2. **Move the Bead Lifecycle section in `agent-task.md` to BEFORE the Task Description** (currently it's at the very end — agents stop reading after the task).
3. **Add `br close` to a deny-list in CLAUDE.md** for implementer-role agents (belt-and-suspenders).

### Tracking

**NO BEAD EXISTS.** Must be created at P0 — called "#1 throughput blocker" in HANDOFF.md.

---

## Issue 2 (P1): Empty tmux pane after paste (~60%)

### Root cause: PARTIAL — multiple contributing factors

1. **Splash-dismiss timing is fragile.** `splashDismissDelay` = 750ms (hardcoded, `pasteinject.go` line 53). Calibrated for single-pane; under concurrent load, Claude's TUI may take longer to initialize.
2. **`agent_ready` ≠ REPL ready.** The gate fires on hook-bridge readiness, not terminal input readiness. Gap between "hooks wired" and "stdin accepting paste."
3. **Possible Claude Max concurrent session limits.** Multiple concurrent sessions per account may be throttled silently.
4. **tmux `paste-buffer` delivery to uninitialized pty.** If the pane's terminal isn't fully connected, paste arrives but gets dropped.

### Fixes attempted (session v60)

- hk-8cq23 (bracketed-paste Enter fix) — did NOT resolve (~60% persists)
- hk-fbydv (pane liveness check) — correctly distinguishes empty vs thinking, but doesn't prevent empty-pane in the first place

### Proposed fixes

- **A.** Scale `splashDismissDelay` by concurrency level (e.g., 750ms * max(1, concurrent_count))
- **B.** Add REPL-ready probe before paste: `capture-pane` check for prompt character, retry Enter+wait if absent
- **C.** Paste-inject retry: capture pane 2s after paste, re-send if claude not processing
- **D.** Stagger concurrent launches: 2-3s delay between spawns to avoid thundering-herd

### Tracking

**NO BEAD EXISTS.** Must be created at P1 — affects 60% of concurrent dispatch.

---

## Issue 3 (P1): Reviewer consistently dies with `context cancelled`

Every reviewer launched in v60 eventually failed with `context cancelled during reviewer wait`. At least 4 occurrences. The orchestrator never investigated.

### Likely cause

Wave-mode batch lifecycle: when other runs in the same wave fail or the batch timeout fires, the wave context gets cancelled, killing the reviewer even though it's still working.

### Tracking

Needs a bead. Reviewers are supposed to be the quality gate — a 100% reviewer failure rate means all merges are manual.

---

## Issue 4 (PROCESS): Orchestrator does inline investigation instead of delegating

Happened in v59 AND v60. User corrected twice in v60:
- "STOP DOING WORK YOURSELF!" 
- "you are the orchestrator, please take the appropriate actions"
- "Did you see my comment about being the orchestrator?"

v60 wasted ~30% of context on inline code reading. A directive was added to HANDOFF but it will need reinforcement — the pattern has persisted across 3+ sessions.

### Root cause

The "DELEGATE INVESTIGATION TO SUB-AGENTS" directive exists in HANDOFF.md but is buried at line 105 among 30+ other directives. An orchestrator under pressure (batch just failed, user frustrated) reverts to the fastest-seeming action (read the code) rather than the correct action (dispatch an investigator).

### Fix

- Move this directive to the top 5 lines of the orchestrator's instruction surface
- Add it to CLAUDE.md as a permanent rule, not just HANDOFF
- Consider a skill that provides the orchestrator a checklist on batch failure

---

## Issue 5 (PROCESS): HANDOFF.md is 66% bloat, with 3 instruction conflicts

### Bloat analysis

| Category | Lines | % |
|----------|-------|---|
| Permanent process rules (should NOT be in HANDOFF) | ~60 | 37% |
| Stale feature announcements | ~25 | 15% |
| Duplicated content | ~20 | 12% |
| **Actual session state (belongs in HANDOFF)** | **~55** | **34%** |

### Active instruction conflicts

1. **Review-loop default:** HANDOFF says "MUST pass `--review-loop` explicitly" (hk-g0ckv hasn't landed). CLAUDE.md says "Review-loop is on by default (hk-g0ckv)" (already landed). Agent behavior depends on read order.
2. **Stream-default status:** HANDOFF says "STREAM-DEFAULT IS NOW LIVE." CLAUDE.md still describes `kind=wave` as default with hk-7nbey as an unresolved P1.
3. **--max-concurrent guidance:** HANDOFF says "USE `--wave` FOR CONCURRENT DISPATCH." CLAUDE.md pre-flight checklist says "keep at `1` until hk-wx8z8 verifies it."

### Also: AGENTS.md = byte-identical copy of CLAUDE.md

285 lines each, same content. One should be deleted or contain only role-specific overrides.

### Fix

Separate HANDOFF.md into:
- **HANDOFF.md** — session state only (~55 lines): version, what landed, systemic issues, next intent, glossary
- **Orchestrator rules** — permanent directives moved to CLAUDE.md or a dedicated `docs/orchestrator-rules.md`
- **Known workarounds** — worktree bugs (lines 65-76) moved to `docs/known-workarounds.md`

---

## Issue 6 (PROCESS): kerf next doesn't surface the right work

### Problem

- `kerf next` scoring ignores `br` priority entirely — P0 and P3 beads are indistinguishable
- 50+ beads tied at identical score (3.486) in v53
- The two systemic issues have no beads, so kerf can't surface them at all
- 4 works have no `bead_filter` and contribute nothing to the feed
- 169 untriaged beads, 306 externally-changed beads in backlog

### Impact

The orchestrator bypasses `kerf next` and dispatches by explicit bead ID, defeating kerf's role as "priority source of truth" (HANDOFF hard rule).

### Fix

- File beads for the systemic issues (so they enter the graph)
- kerf upstream: priority-aware scoring (already filed as kerf feedback across 3 sessions)
- Run `kerf triage --ack` to clear the 306-bead drift backlog
- Wire the 4 unwired works with `bead_filter` clauses

---

## Issue 7 (PROCESS): Known-failing beads re-dispatched without investigation

hk-rnsjs, hk-24xn1, hk-aq17j, hk-7okmx were dispatched across 3 consecutive batches before being dropped. Each failed with close-without-impl. The orchestrator should have investigated after the first repeat failure.

### Fix

Add a rule: "If a bead fails twice in the same session, dispatch an investigator sub-agent before re-dispatching. Never dispatch the same bead more than twice without investigation."

---

## Issue 8 (PRODUCT): Automated check-in cadence too aggressive

20+ automated "check harmonik daemon status" messages during v60, many arriving after the orchestrator already handled the situation. Creates noise and wastes orchestrator turns.

---

## Issue 9 (PROCESS): Investigation dispatch instructions need anchoring

Subagent comparison from v60:
- **Implementation task** (hk-fbydv fix): specific file paths, line numbers, approach → 30 tool calls, 95% productive
- **Investigation task** (empty-pane): pointed at ephemeral tmux state → 77 tool calls, 60% productive, rabbit holes

### Fix

Investigation dispatches should anchor to **durable artifacts** (source code, event logs, config files) first, with live-state inspection as a secondary step. Template: "Start with `<file>:<line>`, then check live state."

---

## What's Working Well (preserve)

1. **Daemon auto-recovery patterns** — quit-on-commit timeout, post-commit /quit watchdog, beads JSONL merge-conflict auto-resolve
2. **3-agent review caught a real bug** in v58 (heartbeat channel-close nil check)
3. **Pre-screening** saved dispatch slots (hk-yozgd closed as subsumed without wasting a cycle)
4. **kerf as a planning tool** — phase-3-dot went cleanly through 8 passes, 34 beads filed, Wave-1+2 executed
5. **Stream-not-waves queue model** — stream-default + notify-stream gives per-bead progress
6. **Orchestration learnings doc** — L-001 through L-020 capture friction with root-cause analysis

---

## Action Items (priority order)

| # | Action | Priority | Effort |
|---|--------|----------|--------|
| 1 | Fix `implementer-protocol.md` br-close contradiction | P0 | ~5 lines |
| 2 | Create P0 bead for close-without-impl | P0 | 2 min |
| 3 | Create P1 bead for empty-pane | P1 | 2 min |
| 4 | Create P1 bead for reviewer context-cancel | P1 | 2 min |
| 5 | Move agent-task.md Bead Lifecycle section before Task Description | P1 | ~5 lines |
| 6 | Resolve 3 HANDOFF/CLAUDE.md instruction conflicts | P1 | 30 min |
| 7 | Separate HANDOFF.md stable rules from session state | P2 | 1 hr |
| 8 | Add "2-failure investigation gate" rule to orchestrator instructions | P2 | 5 min |
| 9 | Promote hk-e6mtt from P2 to P1 (friction label compliance) | P2 | 1 min |
| 10 | Run `kerf triage --ack` + wire 4 unwired works | P2 | 15 min |
| 11 | Add staggered-launch or paste-retry for empty-pane | P2 | 1-2 hr |
| 12 | Delete or differentiate AGENTS.md from CLAUDE.md | P3 | 15 min |
