# Crash-Recovery Adversary Review — Foundation 2026-04-21

## Summary verdict

The foundation has made meaningful progress: the three-store authority rule, checkpoint-at-every-durable-transition invariant, git-wins-on-completion, and the six-category taxonomy are all structurally sound starting points. However, the spec as written has **multiple concrete gaps where a crash at the wrong microsecond produces silent corruption, reconciliation loops, or states no category detector matches.** The load-bearing assumption throughout is "reconciliation is deterministic, idempotent, re-runnable" — but several crash-interleavings break that assumption, and the investigator-agent contract is under-specified as an agent (the agent can crash, hang, return garbage, be preempted, or run twice). The spec needs roughly a dozen concrete clarifications before I would trust it against a motivated crash-time injection harness. Most findings are recoverable with narrow additions; two are architectural (investigator lifecycle, "crash during reconciliation of reconciliation").

## Scenarios the current spec handles well

- **Cat 5 clean restart.** Quiet state + Beads-derived backlog + git-authoritative completion is a solid path. No failure mode here unless stores themselves are corrupt (then it's Cat 6).
- **Cat 1 idempotent rerun.** Node-type tagging + re-spawn is clean and the re-spawn can safely eat the crash window.
- **Cat 4 rate-limited / waiting-on-gate.** Auto-resume-with-pending-action is a reasonable rule because the pending action was itself durable (either stored as a gate state or derivable from last checkpoint).
- **Git-wins-on-completion.** The store-authority rule (execution-model §2.6, beads-integration §10.7) is simple, correct, and defensible. Any race where Beads reports closed-but-git-disagrees correctly routes to Cat 3.
- **Pause-during-reconciliation carve-out** (operator-nfr §7.3). Good catch; letting reconciliation complete before honoring pause avoids a class of partial-pause corruption.
- **Checkpoint-at-every-durable-transition invariant.** As long as this is enforced, git always knows "last durable state" and reconstruction is bounded.

## Scenarios I can break / find gaps in

### S1. The commit-without-Beads-update race (Cat 3 with a latent Cat 2 tail)

**Step-by-step.** T=0 daemon running. T=1 builder node completes work in worktree; daemon commits checkpoint C1 to the task branch with the `Harmonik-Transition-ID` trailer AND intends to update Beads `in_progress` → `closed` via `br`. T=2 the `git commit` returns successfully. T=3 daemon crashes BEFORE invoking `br`. T=4 restart. Daemon walks git: sees C1 with trailer, and — critically — sees a subsequent merge to main (if the merge happened before the Beads write, which §10.4 does allow since terminal-close is AFTER merge-to-branch). T=5 daemon queries Beads: status `in_progress`. 

**What the spec says.** Cat 3 detector "premature-close" is defined for `closed`-in-Beads-but-no-merge-commit. The **inverse** — merge-commit-exists-but-not-closed-in-Beads — is not explicitly named in §9.3. Is it Cat 3? Is it auto-resolvable (daemon just writes the close)? Is it Cat 2 because the pre-close transition wasn't atomic?

**What actually happens.** Ambiguous. If the investigator runs, it will probably verdict `accept-close-with-note`. But if the detector instead classifies as Cat 5 ("nothing in-flight, run is complete, move on"), the daemon will never issue the `br close` call, and the bead stays `in_progress` forever, blocking dependents.

**Proposed fix.** Add a named Cat 3 sub-detector: **"terminal-transition-without-beads-write"** — merge commit exists on target branch (main or integration) tagged with `Harmonik-Run-ID R`, bead for R in `in_progress`, no subsequent in-flight checkpoints for R. Auto-verdict `accept-close-with-note` + mechanical Beads close-write. No investigator needed (this is a deterministic reconciliation).

### S2. Torn Beads write + missing-transition-event

**Step-by-step.** T=0 daemon commits C1 (transition T1). T=1 daemon fsyncs JSONL `transition_event` for T1 (per §3.4, fsync fires at checkpoint_written). T=2 daemon calls `br update bead B in_progress → closed`. T=3 SIGKILL mid-`br` call. Beads SQLite is WAL-mode; the transaction either landed or didn't, but the daemon has no idea which.

**What the spec says.** §10.2 routes all Beads IO through a `br`-CLI adapter; §10.7 says "Beads status is corrected after the investigator's verdict, NOT silently"; §9.3 Cat 3 detectors include "premature-close".

**What actually happens.** On restart, one of two indistinguishable observational results: Beads shows `in_progress` (write didn't land) or `closed` (write landed, ACK was lost). The daemon cannot distinguish these without additionally reading git, which it does — but the same Cat 3 detector fires in both cases. The investigator gets involved either way. That's fine, BUT: if the daemon crashes while running **the investigator workflow** for a torn-Beads-write before the investigator produces a verdict, we now have "crash during reconciliation of a torn Beads write" (see S6 below).

**Proposed fix.** `br` adapter (§10.8) must wrap writes in an idempotency key (harmonik-generated deterministic token, e.g., `run_id + transition_id + op`). Beads already has stable audit log; check the audit log on restart to determine whether the write landed, before running an investigator.

### S3. Investigator-agent hang or crash

**Step-by-step.** T=0 Cat 2 quarantine: builder was mid-write. T=1 daemon spawns investigator workflow W_I. T=2 investigator agent launches in a new worktree (or is it the same leased worktree? see S7 below). T=3 investigator agent hangs indefinitely (LLM API down, infinite-loop in prompt, exceeds wall-clock budget but no timer enforced at this layer). 

**What the spec says.** §9.4 specifies the investigator contract by input/output, and §9.5 enumerates verdicts, but **the investigator-agent itself is not tagged with a deadline, budget, or hang-detection** in the spec. §7.8 says "each reconciliation workflow's own execution is bounded by that workflow's own policy" — so the policy carries the budget, but §9 doesn't normatively require the investigator workflow to declare a budget.

**What actually happens.** Daemon waits. Forever. No verdict → no resolution → the affected bead stays quarantined. If the hang persists across a daemon restart, the investigator workflow itself becomes an in-flight run and needs its own reconciliation. Recursion.

**Proposed fix.** Add §9.4a: every reconciliation workflow MUST declare a wall-clock budget (hard ceiling) and a verdict-default for budget-exhaustion (proposed: `escalate-to-human`). The daemon emits `reconciliation_budget_exhausted` if the budget fires before a verdict.

### S4. Investigator returns garbage verdict

**Step-by-step.** Investigator agent is an LLM. It emits `reconciliation_verdict_emitted` with `verdict: "resume-kinda-maybe"` — not in the §9.5 enum. Or emits a verdict that mentions the correct enum value but also additional instructions the daemon isn't supposed to execute ("resume-here AND reopen-bead"). Or emits two verdict events before its session ends.

**What the spec says.** §9.5 enumerates the verdict vocabulary. It does not specify: what the daemon does with an unrecognized verdict string; whether verdicts are the last event the investigator emits before session-end or can be emitted multiple times; whether the daemon validates the verdict schema before executing.

**What actually happens.** Depends on implementation. Worst case: daemon crashes on invalid verdict. Better case: the daemon ignores unrecognized verdicts, and the investigator run then appears to have emitted no verdict, producing a silent hang (S3 all over again).

**Proposed fix.** §9.5 needs a normative schema and rejection rule: verdict is a JSON object with a single `verdict` field whose value is in the closed enum; any other shape is a `reconciliation_verdict_malformed` event + automatic `escalate-to-human`. The investigator's workflow MUST end after emitting one verdict (structural contract on the workflow).

### S5. Investigator committed its verdict commit, but crashed before the daemon acted on it

**Step-by-step.** §9.4 says the investigator emits (a) a verdict event AND (b) a reconciliation commit on the relevant branch. T=0 investigator runs. T=1 reconciliation commit lands with verdict `reopen-bead`. T=2 daemon crashes BEFORE reading the verdict event / executing the verdict. T=3 restart. Daemon walks git, sees the reconciliation commit. Does it replay the verdict execution?

**What the spec says.** §9.5 says "the daemon executes these mechanically," but the execution step is not explicitly durable. The verdict is durable (in git + JSONL); the daemon's action on it (e.g., `br reopen`, branch discard, lease release) is not framed as a separate durable step with its own reconciliation rule.

**What actually happens.** Open. If the daemon re-classifies the same bead on restart, the detector might re-quarantine (since the reopen hasn't landed in Beads yet) and spawn a SECOND investigator workflow. Now there are two investigator-reconciliation commits and potentially two conflicting verdicts.

**Proposed fix.** The verdict-execution step must be idempotent and discoverable on restart: (a) a reconciliation commit whose verdict has not yet been executed is itself a Cat 3 condition with a dedicated auto-resolver; (b) the daemon writes a `verdict_executed` event (or better, a second reconciliation commit on the same branch) after executing, and its detector requires both commits to be present before declaring the case resolved.

### S6. Recursive reconciliation

**Step-by-step.** T=0 original run R1 crashed mid-builder → Cat 2 → investigator workflow W_I spawned. T=1 W_I is itself checkpointed (it's a workflow!) with its own `run_id = R_I`, checkpoint commits on its own task branch. T=2 during W_I's second checkpoint, daemon crashes. T=3 restart. Daemon walks git, sees in-flight run R_I. Classifies R_I. What does the detector say?

**What the spec says.** §9.1 "reconciliation runs as a normal harmonik workflow." §8.8 "reconciliation is idempotent; re-running detection rules against the same git + Beads state produces the same classifications." But reconciliation *of* reconciliation isn't the same git + Beads state — the first reconciliation wrote commits.

**What actually happens.** The investigator workflow's last checkpoint presumably says its current node is "investigator-agent-node" which is Cat 2-ish (cognition, non-idempotent in the sense of "re-asking the LLM might produce a different answer"). A Cat 2 for R_I spawns W_{I,I} — an investigator for the investigator. This can nest.

**Proposed fix.** Reconciliation workflows must be tagged differently. Either: (a) add a special node-type "investigator" that the Cat 1 detector auto-classifies as idempotent (re-spawn — acceptable because LLM calls are non-deterministic but the investigator's job is deterministic-enough and can be re-asked), OR (b) reconciliation workflows are not checkpointed per ordinary rules — they commit only their verdict, not intermediate states, and crash-mid-investigation means re-spawn-from-scratch. This needs an explicit decision. Either way, §9 must add a sub-section on reconciliation-of-reconciliation semantics, or recursion is unbounded.

### S7. Orphaned tmux session + orphaned lease

**Step-by-step.** T=0 builder is mid-run, worktree leased, tmux pane alive via ntm. T=1 daemon crashes (but ntm-spawned tmux session persists; tmux outlives its parent by design on many setups). T=2 restart. Daemon re-classifies run. T=3 verdict `reset-to-checkpoint`. T=4 daemon attempts to re-spawn builder in the same worktree. 

**What the spec says.** §5.1 lease rule ("worktree leased by RUN, not by agent"). §5.9 re-run rule (fresh worktree ONLY on `reopen-bead`). §8.5 "agent-subprocess failure is observed by the daemon" — but §8.5 doesn't address agent-subprocesses that *survived* a daemon crash.

**What actually happens.** Two agents may end up in the same worktree: the orphaned tmux session still running the old builder (it doesn't know the daemon is dead and may still be writing files), and the re-spawned agent from `reset-to-checkpoint`. File-level race.

**Proposed fix.** §8.2 step 1 (or immediately after) must include an **orphan-subprocess sweep**: before any reconciliation runs, the daemon enumerates existing tmux sessions matching harmonik naming, kills any process it doesn't have in-memory tracking for, and clears worktree locks. This belongs in process-lifecycle, not §9, but §9 needs to *assume* it has happened (currently the spec doesn't say anything).

### S8. Re-claimed bead has orphan branch AND new in-flight branch

**Step-by-step.** T=0 run R1 against bead B crashes, investigator verdicts `reopen-bead`. T=1 bead B goes `open`. T=2 new run R2 claims B, gets fresh worktree + fresh branch `harmonik/R2/node-X`. T=3 R2 crashes mid-builder. T=4 restart. Daemon walks git. Finds TWO orphaned task branches for bead B: `harmonik/R1/*` (from the first crashed run) and `harmonik/R2/*` (from R2). 

**What the spec says.** §5.9 says prior run's branch is "orphaned for purposes of the new attempt" but "referenced in audit trail." §8.2 startup walks all checkpoints. Does the detector include the R1 branch's checkpoints in classifying R2's state?

**What actually happens.** If the detector filters by `run_id` correctly, R1 is ignored (it has no live bead association — `reopen-bead` cleared in-flight tracking). If it filters by `bead_id`, both runs are seen and the detector might double-classify. Worse, if the R1 branch still has a latest-checkpoint trailer implying in-flight and Beads audit log shows the reopen didn't land cleanly, the detector re-quarantines R1.

**Proposed fix.** §9.3 detectors must be RUN-scoped, not BEAD-scoped. Add explicit rule: an orphaned branch for a prior run whose bead has since been claimed in a NEW run is Cat 5 for the old run (no reconciliation needed) and the new run's state is judged on its own branch alone. Add a Cat 3 sub-detector for the pathological case where the `reopen-bead` Beads write didn't complete: "bead `in_progress` with TWO in-flight run_ids" → escalate.

### S9. Operator `stop --immediate` during reconciliation

**Step-by-step.** T=0 daemon is `reconciling`, three Cat 2 investigator workflows running. T=1 operator issues `harmonik stop --immediate`. T=2 SIGKILL cascade.

**What the spec says.** §7.3 "pause MUST NOT interrupt reconciliation workflows." But `stop --immediate` isn't pause; it's stop. §7.7 says `stop --immediate` "skips steps 2–3" of graceful shutdown. Reconciliation is not steps 2–3.

**What actually happens.** Reconciliation workflows are torn down mid-investigator. On restart, their own runs are in-flight with investigator-type nodes. See S6 (recursive reconciliation).

**Proposed fix.** §7.3 carve-out should extend to `stop --immediate`: operator MUST use a distinct command (e.g., `harmonik stop --abandon-reconciliation`) to indicate "yes, I know this leaves investigator workflows in-flight; I accept the recursion risk." Default `stop --immediate` should block on reconciliation completion with a timeout.

### S10. Git worktree in inconsistent state (uncommitted files, detached HEAD)

**Step-by-step.** T=0 builder in worktree made local changes (not yet committed). T=1 daemon crashes. T=2 restart. T=3 reconciliation classifies run as Cat 2.

**What the spec says.** §9.4 investigator inputs include "workspace state (path exists? branch state? WIP files present?)." So the investigator *can* see uncommitted changes — good. But the spec doesn't say what the daemon's deterministic classifier (§9.3 detectors) does with a worktree where HEAD is detached or a rebase is in progress.

**What actually happens.** Detector probably crashes or silently misclassifies; no explicit rule exists for "git worktree is structurally broken (e.g., `.git/rebase-merge` present) independent of commits."

**Proposed fix.** Add a Cat 6 detector: "worktree has in-progress git operation (rebase, merge, cherry-pick, bisect) that is not the work product being tracked." Auto-verdict `escalate-to-human` unless investigator can prove otherwise.

### S11. Upgrade-during-crash

**Step-by-step.** T=0 daemon v1 is upgrading (binary swap). T=1 crashes between old-binary-stop and new-binary-start. T=2 operator restarts; new binary is v2 with different checkpoint-trailer schema.

**What the spec says.** §7.5 + §2.1 say N-1 readable; §7.3 between-task invariant means no in-flight runs at upgrade time — so there SHOULDN'T be in-flight runs to reconcile. But: reconciliation workflows might be in flight at upgrade time if the carve-out in §7.3 was invoked on a pending pause. Also: the event log schema might have new/removed event types.

**What actually happens.** Open. The spec doesn't clearly forbid reconciliation-in-flight-at-upgrade; the carve-out actually allows it.

**Proposed fix.** §7.3: upgrade is NOT like pause — upgrade must wait for both primary runs AND reconciliation workflows to complete before swapping binaries. Operator-observable waiting state.

### S12. Clock jumped backward

**Step-by-step.** T=0 wall-clock is 10:00:00. Checkpoint C1 timestamped 10:00:00. T=1 NTP adjusts clock backward to 09:59:55. T=2 C2 timestamped 09:59:58. T=3 daemon crashes. T=4 restart.

**What the spec says.** §3.3 "do not use wall-clock for ordering decisions" + `event_id` UUID v7 for total ordering. Git commit timestamps CAN go backward.

**What actually happens.** Most reconciliation logic is fine (uses git parent-links, not timestamps). BUT any detector that uses timestamp comparison (e.g., "the most recent checkpoint for run R") could pick the wrong commit. The spec doesn't explicitly say "detectors MUST use git DAG parentage, not timestamps."

**Proposed fix.** §9.3 add: all ordering decisions in detectors use git DAG parentage + UUIDv7 event IDs; wall-clock timestamps are for display only.

## Category taxonomy gaps

The 6-category taxonomy has several scenarios that fit uneasily or not at all:

- **Cat 1.5 — Re-entrant non-idempotent but cleanly-interruptible.** A node that makes filesystem changes but has an "abort + resume" protocol (e.g., an `rsync --partial`). The spec offers only "idempotent vs not." Proposed: make Cat 2 detector account for "declared-as-recoverable-non-idempotent" node-type tags.

- **Cat 7 — Orphan state without an in-flight run.** A tmux session, a worktree lease, a process, a branch that no in-flight run claims. Current taxonomy is run-scoped; these live outside it. Proposed: add a "cleanup" pass prior to category classification (see S7).

- **Cat 3a — Beads-write-in-flight.** S2 above: a torn `br` write where the daemon can't know if it landed. Currently lumped into Cat 3 generic, but it has a distinct resolution (check Beads audit log). Propose explicit sub-category.

- **Cat 3b — Verdict-unexecuted.** S5 above: reconciliation commit landed, execution didn't. Currently lumped into Cat 3 generic. Distinct auto-resolver available; propose sub-category.

- **Cat 6 overuse.** Cat 6 is "investigator + escalate if unresolvable." But some Cat 6 detectors (corrupted JSONL) are not fixable by an LLM investigator — they're observational store corruption with no recovery path. Proposed: split Cat 6 into "6a — structurally wrong but LLM-triageable" and "6b — structurally wrong and mechanically unrecoverable, auto-escalate without investigator spawn" (because spawning an investigator just burns tokens on a hopeless case).

- **Cat missing — Store unavailable, not corrupt.** Beads SQLite locked by another process, `br` binary missing or wrong version, git index locked. The spec treats stores as always-queryable. Proposed: "Cat 0 — Infrastructure unavailable" that halts reconciliation at the detector layer before classification happens. Daemon reports `degraded` and waits for infrastructure, rather than misclassifying every in-flight run as Cat 6.

## Investigator-agent failure modes the spec doesn't address

- **No deadline.** Investigator can hang. See S3. Needs mandatory budget.
- **No verdict-schema validation.** See S4. Needs normative schema + malformed-verdict handling.
- **No at-most-once-verdict guarantee.** Investigator can emit multiple verdict events. Needs one-verdict-ends-workflow rule.
- **No "two investigators accidentally running on the same bead."** If the daemon crashes mid-dispatch of an investigator workflow and restarts, it can re-dispatch. The spec needs an idempotency check at dispatch time (see S5).
- **No "investigator reads stale state."** The investigator's inputs (§9.4) include git state + Beads state + JSONL tail. If the investigator is slow and another daemon action mutates state mid-investigation, the verdict can be based on stale facts. Needs a "snapshot token" concept — the investigator's inputs are scoped to a git commit hash + Beads audit-log entry ID, and the daemon refuses to execute a verdict if state has moved.
- **Investigator requires skills it can't get.** §4.11 mandates skill injection; if the investigator workflow declares skills that can't be provisioned, fail-launch fires. But this fail-launch during reconciliation is itself a new failure mode: the daemon tried to reconcile, couldn't spawn the investigator, now what?
- **Investigator's own outputs not redacted.** The investigator reads session logs (§9.4) which may contain raw agent internals. Its verdict commit / event output may echo secrets. The redaction registry (§4.7) covers event payloads at emission; the investigator's git commit body is not covered.

## Affirmations

- The decision to make reconciliation a workflow (not a subsystem) is correct and composes well with existing primitives — ONCE the recursion-of-reconciliation problem (S6) is resolved.
- Git-wins-on-completion is the right authority rule and simplifies dozens of edge cases.
- UUIDv7 event IDs + git DAG parentage are the right time-source choices for crash-safety; the spec just needs to make the "no wall-clock ordering in detectors" explicit.
- The verdict-vocabulary enum is well-scoped; the six verdicts cover the realistic resolution space. Adding schema validation and one-verdict-per-workflow ends most investigator-misbehavior paths.
- The checkpoint-at-every-durable-transition invariant is load-bearing and correctly identified as such. The whole reconciliation model depends on it; the spec correctly makes it non-optional.
- The pause-during-reconciliation carve-out (§7.3) is a real insight — interrupting reconciliation makes things worse. Extending it to `stop --immediate` and `upgrade` closes the remaining holes.

**Priority fix order (for phase 4 synthesis):** S6 (recursive reconciliation) and S7 (orphan sweep) are architectural and gate the others; S3/S4/S5 (investigator contract hardening) are mechanical additions; S1/S2/S8 (new Cat 3 sub-detectors) are enumerable; S9/S10/S11/S12 are edge-case cleanup.
