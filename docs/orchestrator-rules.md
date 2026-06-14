# Orchestrator Rules

Permanent directives for the harmonik orchestrator agent. Extracted from HANDOFF.md to keep session state lean. Loaded on every `/session-resume` alongside HANDOFF.md.

---

## Identity

**ROLE.** You are the orchestrator. Delegate substantively. Keep the main thread minimal.

---

## Dispatch discipline

**HARMONIK IS THE DEFAULT DISPATCHER (HARD RULE).** The default dispatcher is the ONE persistent daemon per project (`harmonik --project . --no-auto-pull --max-concurrent N` in a detached tmux session); agents dispatch by **submitting beads to its queue**, not by becoming a daemon themselves. The intended daily loop: `kerf next` → pick batch of 3–5 → `harmonik queue submit --beads id1,id2,...` (the `--beads` shorthand landed — hk-m9a7g; or submit a `QueueSubmitRequest` JSON file) → while it runs, append the next batch (`harmonik queue append`) / drain triage / file follow-ups → on group completion, review + submit the next batch. Target: ≥75% of substantive commits per session land through the daemon queue (committer identity / `Refs:` trailer in `git log`). The three exceptions: (a) the bead is a bug-fix to harmonik itself in code that breaks dispatch; (b) ≤2-line typo/cross-reference fix where ~30s daemon overhead isn't worth it; (c) untested workload class per the readiness-audit caveats. Sub-agent dispatch is otherwise the WRONG move. **`harmonik run --beads` is the LEGACY / solo-bootstrap path (hk-b3wqd landed):** with a daemon already up it **submits its beads to that daemon's queue** (no exit-5, no pidfile collision); it only *becomes* the inline daemon — and can hit **exit code 5** (`pidfile locked`) — when no daemon is running. Use it ONLY to bootstrap a one-shot solo batch when you don't want a persistent daemon (matches AGENTS.md §"`harmonik run` is the legacy / solo-bootstrap path"). Full design: `docs/orchestration-protocol-v2.md`.

**SUBMIT A BATCH AS ONE STREAM GROUP (HARD RULE).** When dispatching N beads, submit them ALL in one `harmonik queue submit --beads id1,...,idN` call (one `kind: "stream"` group). Add more mid-flight via `harmonik queue append [--queue-id <uuid>] <group-index> <bead-id ...>` — do NOT split the batch into a queue submit + sub-agents for overflow.

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
4. **If the orchestrator session is keeper-managed:** signal in-flight dispatch before submitting:
   `harmonik keeper set-dispatching <agent>` — sets `.harmonik/keeper/<agent>.dispatching` so
   `HoldingDispatch` returns true and the keeper cycle defers the handoff action (hk-rc51s).
5. Submit via `harmonik queue submit <file>`.
6. Arm a Monitor running `harmonik subscribe` (see Monitor pattern below).
7. **When all in-flight work completes** (group drains, no more `pending` beads):
   `harmonik keeper clear-dispatching <agent>` — removes the marker; keeper resumes normal checks.

---

## Bead lifecycle discipline

**SMOKE-SCRATCH DISCIPLINE (HARD RULE).** Real-daemon validation MUST use the smoke scratch lane — never commit scratch/canary files to the main trunk. The scratch lane runs in a throw-away temp project with its own daemon so all smoke commits land there and are discarded. Use `make smoke-scratch` (or `scripts/smoke-scratch.sh`) for all concurrency-fix and deploy validation. Direct commits to main of scratch markers (e.g. `docs/_smoke_*.md`) followed by cleanup commits are PROHIBITED. Source: logmine F17 — 6 smoke commits + 3 cleanups netted to zero code on main (hk-nk9pu).

**THROWAWAY-CANARY DISCIPLINE (HARD RULE).** When probing for daemon spawn wedges or verifying spawn behavior after a restart, use a **throwaway trivial smoke bead** as the canary — NOT a real implementation bead. Re-dispatching a real bead >2× as a spawn canary violates the never-re-dispatch-without-investigation rule and burns real captain-impl slots. If no throwaway canary exists, create one with `br create --title="canary: throwaway smoke probe" --type=chore --priority=4` and close it after the probe. Incident: hk-w6y70 (a real T2 bead) was used as the spawn-wedge canary 5× across restarts (logmine F15, 2026-06-09).

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

**MAJOR-ISSUE FAN-OUT PROTOCOL (HARD RULE).** When a wedge/failure has survived ≥2 fix attempts OR the root cause has flip-flopped ≥2×: stop single-thread investigation and trigger the fan-out protocol. Fan out 10–15 agents at DISTINCT angles + ≥2 adversarial verifiers that can OVERRULE a wrong synthesis. **NEVER hand-grep `events.jsonl` by `run_id`** — use `jq 'select(.run_id == "<id>")'` or `harmonik subscribe --json`. Full protocol: `docs/major-issue-fanout-protocol.md`. Skill: `.claude/skills/major-issue-fanout/SKILL.md`. Source: logmine F14 + 2026-06-09 postmortem (~18h burned on 6 refuted diagnoses from hand-grep false negatives).

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

## Monitor pattern

Use `harmonik subscribe` — one process, NDJSON to stdout, server-side heartbeat so the agent wakes periodically even if the daemon goes quiet:

```bash
# In a Monitor tool call:
harmonik subscribe --types run_completed,run_failed,run_stale,heartbeat --heartbeat 60s --json
```

`subscribe` attaches to the running daemon, so one Monitor sees every bead the daemon dispatches regardless of which agent submitted it. Re-arm if it hits the Monitor timeout.

**DEPRECATED — Events-tail fallback** (use only if `harmonik subscribe` is unavailable):

```bash
# In a Monitor tool call (timeout_ms = 3600000, persistent = false):
tail -F /Users/gb/github/harmonik/.harmonik/events/events.jsonl 2>/dev/null \
  | grep --line-buffered -E "run_completed|run_failed|run_stale|merge_conflict|reviewer_verdict"
```

NOTE: there is no `daemon.log` file and no per-run output file to tail; `--notify-stream` belonged to the foreground `harmonik run` path, which the persistent daemon does not use.

---

## Run liveness: stale ≠ wedged

**`run_stale` IS NOT A WEDGE SIGNAL (HARD RULE).** Before flagging a daemon run as wedged OR re-dispatching it, the orchestrator MUST:

1. **Wait for the slow-recovery ceiling.** `run_stale` fires at ~10 min of silence — well before the actual recovery window closes. A silent implementer grinding through a long node, a reviewer thinking, or the commit_gate working through the merge sequence are all legitimate. The real ceiling is **~30 minutes from the relevant phase launch** (implementer launch, reviewer launch, or commit_gate entry), not from the first `run_stale` event. Do not call a run wedged until the 30-min ceiling has passed with no forward progress.

2. **Ground-truth via the durable run_id event trace.** Check `.harmonik/events/events.jsonl` filtered by the specific `run_id`:

   ```bash
   jq 'select(.run_id == "<run_id>")' /Users/gb/github/harmonik/.harmonik/events/events.jsonl | tail -20
   ```

   Inspect the **last event TYPE and node** for that run — e.g. `node_dispatch_requested` for `commit_gate` means the run is grinding through the merge gate (normal); a long-silent implementer or reviewer node is also normal. Do NOT use the `harmonik subscribe` heartbeat's `last_event_id` field as a proxy — that is a GLOBAL cursor across all runs, not a per-run liveness indicator.

**A `run_stale` during legitimate slow recovery is NOT a wedge.** Observed pattern: ~4 captain↔crew false-wedge round-trips per window (e.g. hk-4mten: `run_stale` fired at 601s during `launch_initiated`, retracted via `run_id` trace, never re-dispatched). Each false alarm wastes a crew turn and burns context. Refs: hk-9gkwa hk-fdoa.
