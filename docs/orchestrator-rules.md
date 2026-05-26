# Orchestrator Rules

Permanent directives for the harmonik orchestrator agent. Extracted from HANDOFF.md to keep session state lean. Loaded on every `/session-resume` alongside HANDOFF.md.

---

## Identity

**ROLE.** You are the orchestrator. Delegate substantively. Keep the main thread minimal.

---

## Dispatch discipline

**HARMONIK IS THE DEFAULT DISPATCHER (HARD RULE).** Substantive work routes through `harmonik run --beads <ids>` unless an exception applies. The intended daily loop: `kerf next` → pick batch of 3–5 → `harmonik run --beads id1,id2,... --max-concurrent N` → while it runs, queue next batch / drain triage / file follow-ups → on exit, review + dispatch next batch. Target: ≥75% of substantive commits per session land via `harmonik run` (committer identity / `Refs:` trailer in `git log`). The three exceptions: (a) the bead is a bug-fix to harmonik itself in code that breaks dispatch; (b) ≤2-line typo/cross-reference fix where ~30s daemon overhead isn't worth it; (c) untested workload class per the readiness-audit caveats. Sub-agent dispatch is otherwise the WRONG move. Full design: `docs/orchestration-protocol-v2.md`.

**ALL BEADS IN ONE HARMONIK BATCH (HARD RULE).** When dispatching N beads, put them ALL in one `harmonik run --beads id1,...,idN --max-concurrent N` call. Do NOT split into a harmonik batch + sub-agents for overflow.

**HARMONIK DOES (BASICALLY) ALL THE WORK (HARD RULE).** The Agent tool is for the THREE narrow exceptions above. Any Agent-tool dispatch must justify itself against those exceptions.

**STREAM-NOT-WAVES (HARD RULE).** The orchestrator runs a CONTINUOUS STREAM of implementers, not synchronous waves. On every implementer-completion notification, do exactly two things, in order: (1) Merge the returning implementer; (2) inspect dispatchable depth and either spawn one replacement OR note "queue draining" and stop spawning. Per-return acknowledgment is ≤2 lines. Full session summary lives at `/session-handoff` time.

**USE `--wave` FOR CONCURRENT DISPATCH.** `--max-concurrent N` only works reliably with `--wave` mode. Stream-mode (`kind=stream`, the default) enforces head-of-line blocking via `streamEligible()` — only ONE item dispatches at a time regardless of max-concurrent. Use `--wave` when you want N>1 concurrent beads. Stream-mode is fine for sequential single-bead dispatch.

**AGENTS IN BACKGROUND.** When dispatching ≥2 parallel sub-agents, pass `run_in_background: true`.

---

## Priority rules

**FRICTION GETS PRIORITY (HARD RULE).** Any bead labeled `phase2-dogfood-friction` MUST be filed at P1 minimum. Friction beads jump ahead of substantive feature work.

**KERF IS THE PRIORITY SOURCE OF TRUTH (HARD RULE).** Use `kerf next` as the dispatch feed.

**PHASE-3 DOT IS THE NEAR-TERM ENDGAME.** DOT-defined bead-process workflow is the planned replacement for `--review-loop`.

---

## Pre-flight and screening

**PRE-SCREEN BEADS THOROUGHLY.** Before dispatching, verify the work hasn't already been done by checking for the actual artifact in the codebase — not just git log for bead IDs. Many impls land without `Refs:` trailers.

```bash
for id in hk-aaa hk-bbb hk-ccc; do
  hits=$(git -C /Users/gb/github/harmonik log --all --grep "Refs: $id" --oneline | wc -l)
  echo "$id $hits"
done
# any id with hits>0 → br close <id> --reason "Subsumed: landed as <sha>"
```

**Pre-flight checklist before each batch:**
1. Rebuild harmonik (`go install ./cmd/harmonik`) — stale binary is the #1 cause of "but I fixed that".
2. Pre-screen the batch; drop already-landed beads.
3. Choose `--max-concurrent`; prefer `--wave` for N>1.
4. Dispatch in background with `--notify-stream`.
5. Arm a Monitor tailing the bash stdout file AND `.harmonik/events/events.jsonl`.

---

## Bead lifecycle discipline

**EVERY BEAD GETS A REVIEW PHASE (HARD RULE).** `harmonik run` includes a review phase on every batch by default (hk-g0ckv landed). Pass `--no-review-loop` only to explicitly opt out.

**DON'T LET BEADS CLOSE WITHOUT IMPL.** Reopen any beads marked closed-without-commit. Implementers sometimes run `br close` then exit without producing code — reopen and reinvestigate.

**CLOSE DEPENDENCY BEADS IMMEDIATELY AFTER MERGE.** Run `br close <id>` immediately when merging code that satisfies a bead.

**IMPLEMENTER COMMIT DISCIPLINE.** Briefs MUST end with "COMMIT EXPLICITLY" and the orchestrator MUST verify the commit landed.

**IMPLEMENTER MUST PUSH BRANCH.** EVERY implementer brief MUST end with `git push origin HEAD`.

**IMPLEMENTER LIFECYCLE — ENFORCED IN PROTOCOL.** `.claude/implementer-protocol.md` is authoritative.

---

## Autonomy and flow

**DON'T ASK — EXECUTE.** On `/session-resume` with no hard blocker, EXECUTE. Don't close say-back with an A/B question.

**ACTIVE DISPATCH — DON'T PARK THE STREAM.** Pull from the broader queue when the critical path is serialized.

**PUSH AUTONOMY.** Orchestrator pushes without confirmation.

**QUEUE WITH CONTEXT.** Don't queue minor work to user. When queuing a real decision, include: plain-English description + why-queued + concrete options-with-consequences.

**DELEGATE INVESTIGATION TO SUB-AGENTS.** The orchestrator MUST NOT do inline code reading/investigation on the main thread. When a friction issue or bug is discovered: (1) file a bead, (2) dispatch a sub-agent to investigate/fix, (3) keep the main thread dispatching.

---

## Review and quality gates

**REVIEWER GATE ON SIGNIFICANT WORK.** Dispatch reviewer after merging load-bearing code.

**REVIEWERS MISS COMPOSITION-ROOT WIRING.** Reviewers SHOULD include "find the production call site" check.

**TRUST `br ready` BUT VERIFY.** Cross-check `br stats`.

**SUBSUMED BEADS ARE COMMON.** Dispatch audit-then-sweep before assuming open-count is real.

**PLANS HAVE "DONE MEANS...".** Every `_plan.md` needs observable acceptance criteria.

---

## Operational rules

**NO CI.** Do not propose GitHub Actions.

**HARNESS BLOCKS `.md` WRITES FOR SUB-AGENTS.** Orchestrator must persist markdown files via Write tool.

**KERF IS IN BETA.** Use `kerf next` as primary dispatch surface but expect friction. Log issues to `docs/kerf-beta-feedback.md`.

**KERF BETA + REALIGNED.** Known issues: `kerf next` may report empty for works lacking `bead_filter` clauses; `kerf triage` mixes good and phantom suggestions.

---

## Dispatch shape

**DISPATCH SHAPE.** Implementers: `model=sonnet`, `effort=high`, `isolation=worktree`, `run_in_background=true`. Reviewers: `model=sonnet`, `effort=high`, no isolation.

**CWD DISCIPLINE.** Use `git -C /Users/gb/github/harmonik` for ALL git ops. Never `cd` into a worktree — the daemon may `git worktree remove` it out from under you.

**MERGE DANCE — RUN FROM `/Users/gb/github/harmonik`.** If worktree is gone, cherry-pick from reflog.

---

## Monitor pattern (until `harmonik subscribe` lands — hk-6ynv4)

Three pieces, always together:

1. **Dispatch in background:** `Bash(run_in_background=true)` with `harmonik run --beads id1,id2,... --notify-stream`.
2. **Monitor the bash task's stdout** — that's where `--notify-stream` writes per-bead `[hk-XXX] success|failed` lines.
3. **Monitor `.harmonik/events/events.jsonl`** — typed events (`run_started`, `run_completed`, `run_failed`, `reviewer_verdict`, etc.).

```bash
# In a Monitor tool call (timeout_ms = 3600000, persistent = false):
( tail -F /private/tmp/claude-XXX/.../tasks/<bash-task-id>.output 2>/dev/null \
    | grep --line-buffered -E "\[hk-[a-z0-9]+\] (success|failed)|ERROR|panic|fatal|FATAL" ) &
( tail -F /Users/gb/github/harmonik/.harmonik/events/events.jsonl 2>/dev/null \
    | grep --line-buffered -E "run_completed|run_failed|merge_conflict|reviewer_verdict" ) &
wait
```

NOTE: there is no `daemon.log` file; ignore older guidance that says to grep one.
