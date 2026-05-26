<!-- PP-TRIAL:v2 2026-05-26 main — v60 (commit 7367249). Clean. 8 beads landed: 3 daemon friction fixes (liveness check, bracketed-paste, stream HOL), 2 spec-corpus (Role permission schema, deferred-role shells), failure-class classifier, NodeType cleanup, launch-verification heartbeat. Two systemic issues remain: ~60% empty-pane rate on concurrent dispatch, ~80% implementer-exits-without-committing rate. -->

Roadmap: [ROADMAP.md](ROADMAP.md). Cross-project working-style rules: `~/.claude/CLAUDE.md`. Plans index: [plans/README.md](plans/README.md).

<!-- ORCHESTRATION DIRECTIVES — DO NOT EDIT EXCEPT BY EXPLICIT USER REQUEST. Loaded every /session-resume. -->

ROLE. You are the orchestrator. Delegate substantively. Keep the main thread minimal.

LEARNING LOG (READ ON EVERY RESUME). `docs/orchestration-learnings.md` — friction-and-fix log. Read on `/session-resume`. Append new entries when you observe friction. Promote durable rules to this directives block or `.claude/implementer-protocol.md`.

STREAM-NOT-WAVES (HARD RULE). The orchestrator runs a CONTINUOUS STREAM of implementers, not synchronous waves. On every implementer-completion notification, do exactly two things, in order: (1) Merge the returning implementer; (2) inspect dispatchable depth and either spawn one replacement OR note "queue draining" and stop spawning.

Per-return acknowledgment is ≤2 lines. Full session summary lives at `/session-handoff` time.

**HARMONIK IS THE DEFAULT DISPATCHER (HARD RULE, v51).** Substantive work routes through `harmonik run --beads <ids>` unless an exception applies. The intended daily loop: `bv --robot-triage` → `kerf next` → pick batch of 3–5 → `harmonik run --beads id1,id2,... --max-concurrent N` → while it runs, queue next batch / drain triage / file follow-ups → on exit, review + dispatch next batch. Target: ≥75% of substantive commits per session land via `harmonik run` (committer identity / `Refs:` trailer in `git log`). The three exceptions: (a) the bead is a bug-fix to harmonik itself in code that breaks dispatch; (b) ≤2-line typo/cross-reference fix where ~30s daemon overhead isn't worth it; (c) untested workload class per the readiness-audit caveats (priority-sensitive routing — until hk-rp48p's regression test lands). Sub-agent dispatch is otherwise the WRONG move. If you find yourself reaching for the Agent tool on a 4th task in a row, STOP — batch them and run `harmonik run --beads`. Full design: `docs/orchestration-protocol-v2.md`.

**USE `--wave` FOR CONCURRENT DISPATCH (v60 NEW — HARD-LEARNED).** `--max-concurrent N` only works with `--wave` mode. Stream-mode (`kind=stream`, the default) enforces head-of-line blocking via `streamEligible()` — only ONE item dispatches at a time regardless of max-concurrent. The HOL fix (hk-9a27q, `b81a76b`) landed but hasn't been validated in production yet. Until confirmed, use `--wave` when you want N>1 concurrent beads. Stream-mode is fine for sequential single-bead dispatch.

**ALL BEADS IN ONE HARMONIK BATCH (HARD RULE, v59 — USER-ORDERED 2026-05-26).** When dispatching N beads, put them ALL in one `harmonik run --beads id1,...,idN --max-concurrent N` call. Do NOT split into a harmonik batch + sub-agents for overflow.

**STREAM-DEFAULT IS NOW LIVE (v59, hk-7nbey).** `harmonik run --beads` now creates `kind=stream` queues by default. Pass `--wave` to opt back into wave-mode. Remaining gap: hk-24xn1 (daemon wake-on-submit).

**EVERY BEAD GETS A REVIEW PHASE (HARD RULE, v53 — USER-ORDERED 2026-05-21).** `harmonik run` MUST be invoked with `--review-loop` on every batch. No exceptions. P0 bead **hk-g0ckv** flips the default — until that lands, the orchestrator MUST pass `--review-loop` explicitly.

**HARMONIK DOES (BASICALLY) ALL THE WORK (HARD RULE, v53 REINFORCEMENT).** The Agent tool is for the THREE narrow exceptions. Any Agent-tool dispatch must justify itself against those exceptions.

**FRICTION GETS PRIORITY (HARD RULE, v53 — USER-ORDERED 2026-05-21).** Any bead labeled `phase2-dogfood-friction` MUST be filed at P1 minimum. Friction beads jump ahead of substantive feature work.

**KERF IS THE PRIORITY SOURCE OF TRUTH (HARD RULE, v53 — USER-ORDERED 2026-05-21).** Use `kerf next` as the dispatch feed.

**PHASE-3 DOT IS THE NEAR-TERM ENDGAME (v53 — USER-ORDERED 2026-05-21).** DOT-defined bead-process workflow is the planned replacement for `--review-loop`.

PHASE 2 IS UNBLOCKED (v38). Dispatch beads via harmonik daemon.

`harmonik run --beads` MULTI-BEAD + --context + --review-loop (v49). Multi-bead one-shot with parallel dispatch.

`harmonik run --notify-stream` (v53 LIVE). Per-bead completion lines to stdout.

**PANE LIVENESS CHECK LANDED (v60, hk-fbydv, da89ce4).** `pasteinject.go` now checks `pgrep -P <pane-shell-pid>` before killing on heartbeat staleness. If claude process is alive (thinking phase), kill is suppressed and staleness clock resets. Empty panes (no child process) still get killed fast. This replaced the need for the temporary 30-min threshold bump.

**BRACKETED-PASTE ENTER FIX LANDED (v60, hk-8cq23, 81921b4).** All three pasteinject paths (implementer-initial, implementer-resume, reviewer) now send `SendEnterToLastPane` after `WriteLastPane` to ensure text submission regardless of bracketed-paste mode state. **However: empty-pane rate is still ~60% in v60 testing.** The root cause may be deeper than bracketed-paste — investigate further.

**QUEUE SEMANTICS (v60 UPDATE).** Stream HOL fix landed (hk-9a27q, b81a76b) — `streamEligible` now skips dispatched items instead of blocking. Not yet validated in production. `--wave` remains the safe choice for concurrent dispatch.

**PRE-SCREEN BEADS THOROUGHLY (v59).** Before dispatching, verify the work hasn't already been done by checking for the actual artifact in the codebase.

**CLOSE DEPENDENCY BEADS IMMEDIATELY AFTER MERGE (v59).** Run `br close <id>` immediately when merging code that satisfies a bead.

IMPLEMENTER COMMIT DISCIPLINE (REINFORCED v38). Briefs MUST end with "COMMIT EXPLICITLY" and the orchestrator MUST verify the commit landed.

**IMPLEMENTERS CLOSE BEADS WITHOUT COMMITTING (v60 NEW — SYSTEMIC).** ~80% of implementer sessions in v60 ran `br close` then exited without producing any code (exit=0, no commit). The bead lifecycle section in agent-task.md tells handlers NOT to close beads, but they ignore it. Every failed batch requires reopening 4-5 incorrectly-closed beads. This is the #1 throughput blocker. Needs investigation — possibly a bead-description-quality issue (title-only beads with no body), or an implementer-protocol issue.

IMPLEMENTER MUST PUSH BRANCH (v49). EVERY implementer brief MUST end with `git push origin HEAD`.

AGENTS IN BACKGROUND (v46). When dispatching ≥2 parallel sub-agents, pass `run_in_background: true`.

QUEUE WITH CONTEXT (v46, L-020). Don't queue minor work to user; when queuing, include plain-English context.

REVIEWER GATE ON SIGNIFICANT WORK (v48). Dispatch reviewer after merging load-bearing code.

REVIEWERS MISS COMPOSITION-ROOT WIRING (v49). Reviewers SHOULD include "find the production call site" check.

DON'T LET BEADS CLOSE WITHOUT IMPL (v49, REINFORCED v60). Reopen any beads marked closed-without-commit.

WORKTREE BEADS-JSONL STALE-AT-FORK (v48 PATTERN). Resolve with `git checkout --theirs .beads/issues.jsonl`.

WORKTREE TASK-INJECTION LEAK (v36, ONGOING). Stash before merge.

WORKTREE AUTO-REMOVED BY HARNESS (v41). Branch survives; merge directly.

WORKTREE-REMOVE STEALS CWD (v45). Prepend `cd /Users/gb/github/harmonik`.

WORKTREE BEADS-JSONL LEAK (v41). Stash before rebase; never pop.

ISOLATED-WORKTREE STALE-BASE BUG (v35, ONGOING). Rebase before reading code.

TRUST `br ready` BUT VERIFY (L-011, L-017). Cross-check `br stats`.

DON'T ASK — EXECUTE. On `/session-resume` with no hard blocker, EXECUTE.

ACTIVE DISPATCH — DON'T PARK THE STREAM (v44, L-018). Pull from broader queue when critical-path is serialized.

SUBSUMED BEADS ARE COMMON (v45, REINFORCED v59). Dispatch audit-then-sweep before assuming open-count is real.

PUSH AUTONOMY (v40). Orchestrator pushes without confirmation.

NO CI (v41). Do not propose GitHub Actions.

IMPLEMENTER LIFECYCLE — ENFORCED IN PROTOCOL. `.claude/implementer-protocol.md` is authoritative.

DISPATCH SHAPE. Implementers: `model=sonnet`, `effort=high`, `isolation=worktree`, `run_in_background=true`. Reviewers: `model=sonnet`, `effort=high`, no isolation.

CWD DISCIPLINE. Use `git -C /Users/gb/github/harmonik` for ALL git ops.

MERGE DANCE — RUN FROM `/Users/gb/github/harmonik`. See v59 handoff for the full script. If worktree is gone, cherry-pick from reflog.

CONTEXT BUDGET (orchestrator). ~700k effective. v60 used ~25% across 8 landed beads + multiple failed batches.

HARNESS BLOCKS `.md` WRITES FOR SUB-AGENTS (v47). Orchestrator must persist via Write tool.

KERF IS IN BETA + REALIGNED (v48). Use `kerf next` as primary dispatch surface.

PLANS HAVE "DONE MEANS..." (v49). Every `_plan.md` needs observable acceptance criteria.

**DELEGATE INVESTIGATION TO SUB-AGENTS (v60 NEW — USER-ORDERED 2026-05-26).** The orchestrator MUST NOT do inline code reading/investigation on the main thread. When a friction issue or bug is discovered: (1) file a bead, (2) dispatch a sub-agent to investigate/fix, (3) keep the main thread dispatching. v60 wasted ~30% of context on inline investigation of pasteinject.go, workloop.go, and queue/state.go before the user corrected this.

<!-- END DIRECTIVES -->

# Where we are (v60, 2026-05-26)

**Main at `7367249`** (origin parity, working tree clean). 8 beads landed this session.

## What v60 landed

8 commits on main:

| Commit | Bead | Description |
|--------|------|-------------|
| `7c70921` | hk-ex9c4 | Failure-class classifier (T-IMPL-006) |
| `90b6037` | hk-3xknp | Remove NodeTypeControlPoint (WG-001) |
| `08903dd` | hk-3gq0b | Launch-verification heartbeat window |
| `da89ce4` | hk-fbydv | Pane liveness check (pgrep-based, prevents killing active thinking sessions) |
| `01d5aca` | hk-a8bg.28 | Role permission_schema presence (CP-028) |
| `81921b4` | hk-8cq23 | Post-paste SendEnterToLastPane (bracketed-paste race fix) |
| `b81a76b` | hk-9a27q | Stream HOL blocking fix (streamEligible skips dispatched) |
| `7367249` | hk-a8bg.30 | Deferred roles carry empty shells (CP-030) |

Plus closed 5 stale-open beads from v59 (hk-jon6r, hk-pphof, hk-8uy6m, hk-b0cyc, hk-ortkx).

## TWO SYSTEMIC ISSUES remain

### 1. ~60% empty-pane rate on concurrent dispatch
Paste delivered to tmux pane but claude never starts processing. Bracketed-paste fix (hk-8cq23) didn't fully resolve it. Sub-agent investigation found a splash-dismiss timing race but the fix didn't eliminate it. Needs deeper investigation — may be a Claude Max concurrent session limit or a tmux pane lifecycle issue.

### 2. ~80% implementer-exits-without-committing rate
Implementers read the bead, run `br close`, and exit without producing code. Happens consistently across multiple batches and different beads. Possible causes: (a) bead descriptions too terse (title-only, no implementation guidance), (b) implementer protocol not being followed, (c) beads are too complex for a single-shot implementer.

## Next-session intent

1. **Investigate the two systemic issues above** — dispatch sub-agents, don't do it inline.
2. **Continue spec-corpus implementation** — hk-a8bg.29 (role default permissions), hk-a8bg.70 (DelegationPath), hk-hqwn.37 (event schema_version), hk-a8bg.31 (Beads-CLI default skill).
3. **DOT impl chain** — hk-7okmx (T-IMPL-003 loader) is unblocked now that validator landed.
4. **Friction beads still open** — hk-rnsjs (claim-failure auto-close), hk-24xn1 (daemon wake-on-submit), hk-aq17j (runCtx refactor).

## Files to open first

1. `HANDOFF.md` (this)
2. `internal/daemon/pasteinject.go` — liveness check + bracketed-paste fix landed here
3. `internal/queue/state.go` — stream HOL fix landed here

## Plain-English glossary

- **hk-fbydv** — pane liveness check: daemon uses `pgrep` to distinguish "claude thinking" from "empty pane" before killing
- **hk-8cq23** — bracketed-paste fix: sends Enter after paste to ensure text submission
- **hk-9a27q** — stream HOL fix: `streamEligible()` no longer blocks on dispatched items
- **hk-a8bg.28/30** — control-points spec corpus: role permission schema + deferred-role shells
- **empty-pane** — tmux pane has prompt text but claude never started processing (~60% of concurrent sessions)
- **close-without-impl** — implementer runs `br close` then exits without committing code (~80% of sessions)
- **`--wave`** — queue mode that allows concurrent dispatch; use instead of stream-default when `--max-concurrent > 1`

## No hard blockers requiring user input.
