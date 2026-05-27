---
name: harmonik-dispatch
description: >
  Canonical "main-agent's daily loop" for the harmonik project. Routes ≥75% of
  substantive work through `harmonik run --beads <ids>` rather than spawning
  Agent-tool sub-agents. Loads on session-resume; gates dispatch decisions.
  Authoritative: AGENTS.md §"Daily loop (canonical)" + docs/orchestration-protocol-v2.md.
---

# Harmonik dispatch — the daily loop

When working in this project (`/Users/gb/github/harmonik`), the FIRST tool call of the working phase should be `kerf next` (ranked bead feed with work-context), then a proposed `harmonik run --beads` dispatch batch — BEFORE any Agent-tool sub-agent invocation.

## The loop

1. **Triage.** `kerf next` — ranked feed of beads with work-context. Use `kerf triage` for drift detection (untriaged beads, external changes).
2. **Pick a batch of 3–5 beads.** Previously-flagged caveats (hk-rp48p priority-sort, hk-wx8z8 parallel pane allocator, hk-cj0gm Stop-hook delivery) are all FIXED; broad-class dispatch is now safe. The remaining caveat is the test-coverage gap for the spawn-path itself (parent `hk-p3diy`) — until scenario tests land, prefer single-class batches when validating new daemon changes.
3. **Dispatch in background.** `harmonik run --beads id1,id2,id3 --max-concurrent 1 2>&1 | tee /tmp/dogfood-<date>.log &` (background-mode pattern; do NOT block inline). The daemon spawns claude, watches for completion, commits, merges to main, pushes, and closes each bead.
4. **Stay active while harmonik runs.** Queue the next batch's candidates; drain `kerf triage` untriaged items; file follow-up beads observed from prior runs; review recently-merged commits per the per-commit-reviewer gate.
5. **On harmonik exit.** Inspect: exit code (0 success / 1 paused-by-failure / 2 unexpected); `git log --oneline -N` for landed commits; `br list --status=closed --limit 10`. Run reviewer on any load-bearing commit.

## When to NOT route through harmonik (exceptions)

Sub-agent dispatch (via the Agent tool) is justified ONLY when:

- **(a)** You're fixing harmonik itself in code that breaks dispatch (e.g. hk-wx8z8 itself).
- **(b)** The change is ≤2 lines of typo / cross-reference cleanup where ~30s daemon overhead isn't worth it.
- **(c)** The work touches an untested workload class per the readiness audit.

Anything else: route through harmonik. If you're on the 4th Agent-tool call in a row, STOP and batch them.

## API rate-limit concurrency rule (HARD RULE — hk-kumjl / hk-ocbh2)

**Do NOT dispatch `harmonik run` AND ≥10 parallel Agent-tool sub-agents at the same time on the same Anthropic account.**

Observed failure mode: orchestrator dispatched ~40 parallel sub-agents while a harmonik run was in flight. The harmonik-launched claude processes were queued behind the sub-agents by the Claude API rate-limiter. `run_started` fired at 09:24; `handler_capabilities` did not arrive until 10:20 — a **56-minute stall** with no error surfaced.

**Rule:** Pick one mode per work phase:
- **Harmonik phase** — `harmonik run --beads ...` in background; ≤3 Agent-tool sub-agents concurrently (monitoring, triage, review).
- **Sub-agent phase** — heavy Agent-tool dispatch (research, parallel investigation); hold off on new `harmonik run` invocations until the sub-agent wave drains.

If you must interleave, cap total concurrent claude sessions (harmonik + sub-agents) to **≤5** across both modes to stay safely within the rate limit.

## Failure handling

Exit code 1 → read the paused queue.json, classify the failing bead:
- **Flake / transient** (network, lock contention) → re-dispatch single bead via `harmonik run <id>`.
- **Genuine bug in the bead's work** → fix-up sub-agent on the worktree branch.
- **Bug in harmonik itself** → fall back to sub-agent dispatch for THIS bead AND file an `hk-...` bug bead.

Document classification in the post-mortem.

## 75% criterion

Each session ends with a tally: substantive commits this session, of which N landed via `harmonik run` (committer identity / `Refs:` trailer in `git log`). Target: N/total ≥ 0.75. Trivial typos and hygiene-only commits don't count. Sessions that miss the target log a one-line reason in `/session-handoff`.

## References

- `AGENTS.md` §"Daily loop (canonical)" — the canonical project rule.
- `HANDOFF.md` §"HARMONIK IS THE DEFAULT DISPATCHER (HARD RULE)" — the orchestration directive.
- `docs/orchestration-protocol-v2.md` — full design with rationale and exact text deltas.
- `docs/kerf-feedback/2026-05-19-phase2-readiness-audit.md` — what's still untested.
