# Crash-Recovery Adversary Review — reconciliation v0.3.0

**Reviewer role.** Crash-Recovery Adversary (round 2).
**Targets.**
- `/Users/gb/github/harmonik/specs/reconciliation/spec.md` v0.3.0 (880 lines)
- `/Users/gb/github/harmonik/specs/reconciliation/schemas.md` v0.3.0 (221 lines)

**Lens.** Reconciliation IS the crash-recovery subsystem. Pressure the spec against physical-reality boundaries — kernel panics, power loss, partial fsync, torn writes, concurrent operator commands, signal races, drain failures, observability gaps — and watch what happens when the crash-recovery mechanism itself crashes. The bounded-recursion argument (RC-002 + RC-003) is elegant; this review tests whether the recursion really is bounded once the kernel, the disk, and the operator all start pulling at once.

**Date.** 2026-04-24.

---

## 1. Verdict summary

v0.3.0 is a significant step forward from v0.2. The R1 integration pass landed the pieces that make the recursion-bounding argument load-bearing: RC-002a (at-most-one-reconciliation-per-target-run), RC-003a (priority-ordered first-match), RC-019a (evidence-corroboration), RC-020a (three dispatch points), the `no-op-accept` verdict, and the RC-owned `Harmonik-Verdict-Executed` trailer. Cat 6 split into 6a/6b correctly separates LLM-triageable integrity from mechanically-unrecoverable integrity. The verdict staleness check (RC-024) is a solid crash-safety discipline.

But the spec is not yet crash-safe. Nine concrete gaps:

1. **RC-002a's lock cites a registry lock that does not exist.** RC-002a says "The daemon's workflow registry MUST hold a lock keyed on `target_run_id`... released atomically with the verdict-executed commit per [process-lifecycle.md §4.3]." PL §4.3 is "Ready-state transition" (PL-009/009a/009b/010); it defines no per-`target_run_id` workflow-registry lock, and never uses the word "lock." The primitive RC-002a depends on is undeclared. If the primitive is an in-memory mutex, it vanishes on crash and the "at most one reconciliation workflow per target_run_id" claim does not survive crash mid-hold. Scenario 3 below.

2. **"Released atomically with the verdict-executed commit" is not atomic.** Even if the lock existed as a file, the verdict-executed commit is a git commit and the lock release is a filesystem operation. Crash between them leaves either (a) an unreleased lock with a landed verdict-executed commit, or (b) a released lock with no verdict-executed commit. Neither state is explicitly classified; (a) blocks all subsequent reconciliation of that target_run_id until operator intervention. Scenario 2 below.

3. **Cat 3a's "intent log + audit log" corroboration can't corroborate under BI's own terms.** RC §8.4a (line 138) says the detector fires when "the Beads audit log... shows no corresponding entry OR shows an entry matching the idempotency key." BI-031 (line 366–376) rewrote the recovery protocol to be **Beads-idempotency-independent** — it probes current `coarse_status` via `br show`, NOT the audit log's idempotency-key field. The detector's stated evidence source (audit log idempotency-key match) is not what the resolver reads. Scenario 2 below.

4. **RC-019a's two-store-corroboration rule creates a Cat 3a dead zone.** RC-019a requires every `store_divergence_detected` carry evidence from ≥ 2 stores; Cat 6b is exempt. Cat 3a's evidence is the intent file on local disk + Beads status. If the local intent-file directory is unreadable (permissions race, filesystem remount) the detector has **one** store (Beads) AND can't escalate via Cat 6b because git + Beads appear fine. The rule produces a silent blindspot rather than a classification. Scenario 6 below.

5. **Budget-exhaustion path is crash-unsafe between emission and subprocess kill.** RC-018 requires (i) emit `reconciliation_budget_exhausted`, (ii) issue fallback `escalate-to-human` verdict, (iii) kill investigator subprocess, (iv) NOT write a reconciliation commit. Events (i) and (ii) are separate fsync-boundary writes per EV-016a (no multi-event atomicity). Crash between (i) and (iii) leaves a running investigator subprocess that may write a verdict commit *after* restart's orphan sweep has re-classified the target as Cat 5 or re-dispatched Cat 2. Two investigators producing verdicts for the same target_run_id is explicitly a RC-023 `multiple-verdicts` malformation — but the fallback verdict from step (ii) is itself the second verdict. Scenario 5 below.

6. **The fsync cost of detector emission is not bounded.** RC-013 (category_assigned), RC-014 (store_divergence_detected), RC-018 (budget_exhausted), RC-024 (verdict_stale), and RC-025 (verdict_executed) all emit events. If all are `fsync-boundary` class (they should be — RC-017 implies so), classifying N in-flight runs at startup costs N fsync round-trips before `ready`. EV-016a explicitly disallows batching. At N=100 and p95 fsync latency of 10 ms, that's 1 s of raw fsync cost — tolerable. At N=500 on a slow SSD, that's 5 s — a meaningful fraction of the 30s nominal RTO fixture (ON-031). The spec never audits this cost and RC-020a adds scheduled hourly re-classification which re-incurs it. Scenario 10 below.

7. **Investigator workflow-as-run recursion is bounded in the happy path but not under detector panic.** RC-003 asserts reconciliation-workflow nodes MUST NOT be classified under Cat 1/2. The mechanism is a `workflow_class` filter (per RC-INV-001). But a crash in the detector's `workflow_class` check itself (e.g., Go nil-deref on a malformed Workflow registry entry) is not routed by any rule. OQ-RC-003 explicitly defers this ("fail-open reconciliation escalation") — but an *undefined* detector panic at startup could leave the daemon stuck in `reconciling` forever. The spec says OQ-RC-003's default is "refuse to reach ready"; fine, but RC-020a's "scheduled hourly" path has no equivalent fallback. A detector panic an hour after ready is not scoped. Scenario 4 below.

8. **Cat 3b's "re-execute on restart" path has no maximum retry count.** RC-026 says Cat 3b auto-resolves via RC-026 re-execution; RC-026 calls RC-024 staleness + RC-025 execution. If the execution itself fails (e.g., `br reopen` times out), §8.5 says escalate to Cat 3 generic — but that's only for *persistent* failure. A *flapping* failure (succeeds half the time) produces infinite re-dispatch on every daemon restart. No backoff, no cumulative-attempt counter. Scenario 13 below.

9. **Cat 6a "two task branches advertising `Harmonik-Run-ID` without `Harmonik-Verdict-Executed`" fires on a legitimate race.** Scenario: operator runs `harmonik reconcile --run X` (RC-020a path b) while startup reconciliation (path a) is still dispatching. If RC-002a's lock is *not* durable (finding #1), both paths spawn investigator workflows. Both create task branches. Both emit verdict commits. Neither has a verdict-executed commit yet. The Cat 6a detector now classifies the target run as an integrity violation and dispatches *another* investigator at hour-boundary (RC-020a path c) for cascading triple-reconciliation. Scenario 11 below.

No finding reopens a locked decision. Eight of nine are one-to-three-sentence normative additions; finding #1 requires either adding a PL §4.3 requirement or migrating the lock to a BI-style intent-file primitive; finding #6 is an observability addition, not a correctness gap.

---

## 2. Scenarios probed

### Scenario 1 — Crash mid-detector run

**Affected requirements.** RC-002 (one-commit rule), RC-003 (re-classification on restart), RC-010 (detectors operate on runs), RC-012 (Cat 0 pre-check), RC-013 (category_assigned emission), RC-020a (detector cadence).

**What the spec says.** Detectors are idempotent across dispatch points (RC-020a last paragraph: "re-running a detector on the same `(target_run_id, snapshot)` MUST produce the same category assignment"). Category-assignment emission MUST precede dispatch (RC-013).

**What actually happens.** Fine-grained crash profile per the startup pseudocode (§7.1, lines 660–682):

- Crash after `detect_category(run)` returned but before `emit_event("reconciliation_category_assigned")`: no durable trace. Restart re-runs detection; identical classification per RC-003a priority order. **Safe.**
- Crash after `emit_event(...)` but before `invoke_auto_resolver(run, ...)` or `dispatch_reconciliation_workflow(...)`: durable `reconciliation_category_assigned` event exists on JSONL; no side effect on git or Beads. Restart re-runs detection; re-emits `reconciliation_category_assigned` (possibly a second time for the same run — the spec does not say consumers deduplicate). **Unsafe at the observability layer:** duplicate classification emissions with no pairing rule. RC-INV-001's audit sensor ("one tag check per N verdicts") does not cover double-classification of the same target run.
- Crash after auto-resolver's mechanical action (e.g., `br` write via adapter) but before `reconciliation_verdict_executed` emission: adapter's intent log is fsync'd (BI-030), so restart re-enters BI-031's recovery. **Safe** via BI-031 + Cat 3a auto-resolver, modulo finding #3.
- Crash during the detector's *own* enumeration of in-flight runs (the `walk_git_for_run_trailers` + `collect_runs_by_run_id` pre-emit phase): no emission fired; classification incomplete. Restart re-runs from step 0 per PL-025. **Safe.**

**Safe or unsafe.** Mostly safe; the duplicate-emission case is a consumer-side observability gap, not a correctness issue, but the spec never declares what consumers should do.

**Concrete spec-text proposal.** Add to RC-013: "Consumers of `reconciliation_category_assigned` MUST tolerate duplicate emissions for the same `(target_run_id, snapshot)` within a single daemon lifetime caused by crash-between-emit-and-dispatch. Deduplication is consumer-side on `(target_run_id, category, snapshot_token.git_head_hash)`; the event stream does not promise at-most-once for classification."

### Scenario 2 — Crash mid-verdict-execution (Cat 3a auto-resolver)

**Affected requirements.** RC-025 (verdict execution is durable and idempotent), RC-026 (verdict-execution discovery on restart), §8.5 Cat 3b detection, BI-031 (status-check-before-reissue), BI-031b (`br show` JSON consistency).

**What the spec says.** RC-025 defines verdict execution as three steps: (a) mechanical action, (b) emit `reconciliation_verdict_executed`, (c) append `Harmonik-Verdict-Executed: true` trailer commit. §7.1 pseudocode (line 684–705) orders these as `action.execute` → `emit_event` → `git.commit(...)`. RC-026 says a verdict commit without a verdict-executed commit is Cat 3b.

**What actually happens.** The three-step sequence is not atomic. Crash profile:

- Crash after (a) but before (b): mechanical side effect landed (e.g., `br reopen` succeeded, or for `resume-with-context`, the context was injected into the outer run's shared state). No `reconciliation_verdict_executed` event. No verdict-executed commit. Restart classifies this as Cat 3b and re-executes. RC-025 says each mechanical action is idempotent; `reopen-bead` re-issues via BI-031 status-check (status is already `open`, so no-op). **Safe for `reopen-bead` and `accept-close-with-note` via BI-031.** Unsafe for `resume-with-context`: the action is "inject context into the run's shared context map" (schemas.md §6.2: "context injection is additive"). If the re-execution re-injects the same context, that's idempotent only if the daemon tracks which contexts have been injected for which run. The spec doesn't say it does. Re-execution may double-append the context string.

- Crash after (b) but before (c): `reconciliation_verdict_executed` event is on JSONL; no verdict-executed commit. Restart reads git, finds no verdict-executed trailer, classifies as Cat 3b. Re-executes RC-025 step (a)—which has already landed side effects—then re-emits (b) as a *second* `reconciliation_verdict_executed` event. Consumers see two execution events for one verdict. For `escalate-to-human`, RC-025's dedup-by-`target_run_id` handles this; for `resume-here`/`resume-with-context`, the re-dispatch check in schemas.md §6.2 says "dispatch check sees outer run already running; no re-dispatch" — but the original dispatch may have failed and the daemon's in-memory state is now re-built from git + Beads. The "outer run already running" signal is in-memory and was lost at crash; restart has no way to distinguish "was running, crash, now Cat 3b" from "verdict never executed mechanically." **Unsafe for resume verdicts under repeated crash.**

- Crash after (c) but before finalizing in-memory state: verdict-executed commit is durable; Cat 3b does not fire on restart; the outer run proceeds normally. **Safe.**

- BI-031 protocol race under finding #3. §8.4a's detection rule (spec.md line 138) consults the Beads audit log for "an entry matching the idempotency key." BI-031 explicitly removed that requirement and uses `br show`'s current status. If the two drift (e.g., audit log has the entry because the call succeeded, but `br show` returns pre-state because of a Beads replication window or a cache lag), the detector and the resolver disagree about whether the write landed. **Unsafe:** the Cat 3a detection rule cites evidence BI does not produce.

**Safe or unsafe.** Mixed. `reopen-bead` and close paths are protected by BI-031 + idempotency keys. `resume-with-context` has an additive-context bug under double-execution. The RC-§8.4a vs BI-031 detection-rule drift is a blocking correctness issue.

**Concrete spec-text proposal.**
1. Reshape RC §8.4a's detection rule to match BI-031: replace "Beads audit log at restart time either shows no corresponding entry OR shows an entry matching the idempotency key" with "BI-031's status-check-before-reissue protocol determines whether a re-issue is warranted; Cat 3a fires iff BI-031 step 4 (reissue) or step 5 (divergence) applies."
2. Amend RC-025's `resume-with-context` idempotency rule to state: "Context injection MUST be keyed on `(target_run_id, investigator_run_id)`; repeated application of the same `(target_run_id, investigator_run_id)` pair is a no-op. The in-memory injection tracking MUST be reconstructible on restart by scanning the target branch for verdict-executed commits in reverse-parent order." Without this, `resume-with-context` under crash-during-execution is not idempotent.

### Scenario 3 — Concurrent reconciliation workflow lock failure

**Affected requirements.** RC-002a (at most one reconciliation per target_run_id), PL-009a (auto-resolver failure routes to Cat 3 investigator), PL §4.3 ready-state transition (cited by RC-002a as the lock's home).

**What the spec says.** RC-002a declares "The daemon's workflow registry MUST hold a lock keyed on `target_run_id`... released atomically with the verdict-executed commit per [process-lifecycle.md §4.3]." Second dispatch within 5s waits; timeout routes to Cat 4 if still progressing, or skip if past verdict emission.

**What actually happens.**

- PL §4.3 does not define such a lock. The spec cites a primitive that does not exist. The nearest PL primitive is the composition-root in-memory registries (PL-020a) — in-memory only, lost on crash.
- If the lock is an in-memory mutex, a daemon crash holding it resets all RC-002a serialization guarantees. Restart reconciliation (PL-005 step 8) dispatches investigators for both the originally-locked target_run_id AND any newly-classified runs. Nothing prevents two concurrent investigators against the same target.
- If the lock is a file lock (e.g., `flock` on `.harmonik/reconciliation-locks/<target_run_id>.lock`), kernel releases it on crash. Restart re-acquires cleanly. **But the spec doesn't name the primitive or the path.** Without mandating the primitive, an implementer may choose in-memory and ship a crash-unsafe serialization.
- "Released atomically with the verdict-executed commit" — the verdict-executed commit is landed via `git commit` + git's refs-update (atomic within git), but the lock release is a separate operation. Crash between them leaves either a stale lock file with a landed verdict-executed commit, or a released lock with no commit. Neither is classified by §8.
- The 5-second wait in RC-002a is a bounded-timeout — fine, but the "5 seconds" is shorter than typical git commit + fsync latency under disk pressure (which can exceed 5 s on a slow SSD under heavy load). A legitimate in-flight verdict-execution can time out the second dispatch into Cat 4 routing even though the first is proceeding normally.

**Safe or unsafe.** **Unsafe.** The stated primitive does not exist in the cited location; the atomicity claim is physically impossible (two separate state-writes); the 5s timeout is shorter than worst-case fsync under load.

**Concrete spec-text proposal.**
1. Migrate the RC-002a lock to a BI-style durable intent primitive: `.harmonik/reconciliation-locks/<target_run_id>.lock` with fsync-on-create and content `{investigator_run_id, acquired_at_monotonic, daemon_generation}`. Kernel releases `flock` on crash; restart re-acquires via a stale-lock sweep modeled on PL-006 orphan sweep.
2. Add **RC-002b**: "Lock acquisition and verdict-executed-commit emission are NOT atomic. On restart, a reconciliation-lock file present for a `target_run_id` whose investigator-run task branch carries a `Harmonik-Verdict-Executed: true` trailer MUST be deleted (lock is stale-after-verdict-executed). A lock file whose investigator-run task branch carries NO verdict-executed commit MUST be classified as Cat 3b (verdict-unexecuted) and processed by the RC-026 re-execution path."
3. Require the lock primitive to live in PL (per the RC-002a citation's intent) by adding a PL requirement, OR move the primitive into RC's schemas.md with an explicit path and fsync rule. Prefer the latter — RC owns reconciliation infrastructure.
4. Increase the RC-002a wait timeout from 5 s to T_drain + small epsilon, or gate the wait on "no verdict-executed commit observed yet" rather than wall-clock.

### Scenario 4 — Investigator workflow as run: panic in detector itself

**Affected requirements.** RC-001 (reconciliation runs as a workflow), RC-003 (bounded recursion), RC-020a (three dispatch points), RC-INV-001 (reconciliation-as-workflow uniqueness), OQ-RC-003 (fail-open escalation).

**What the spec says.** RC-003 asserts reconciliation-workflow nodes MUST NOT be classified under Cat 1/2. The mechanism is the `workflow_class` filter of RC-INV-001. OQ-RC-003 defers the fail-open question — default is "refuse to reach ready."

**What actually happens.**

- Investigator-workflow nodes ARE checkpoint-free per RC-002 — the `workflow_class` filter excludes them from detection. Correct for crashes *inside* investigator nodes.
- But the detector *itself* is in the daemon's Go code (RC-005) and can panic on malformed evidence. A `Harmonik-Workflow-Class: reconciliation` trailer whose value is corrupt (e.g., byte-flipped to `reconciliat1on`) fails the `workflow_class` check. The detector may panic, skip, or classify-as-ordinary — the spec doesn't say which.
- At startup, a detector panic routes through PL-018a (recover() barrier) → exit code 19 per PL-008a. Next restart re-runs the same detector on the same evidence and re-panics. **Daemon never reaches `ready`; the recursion is unbounded in lifecycle-event space (each restart emits `daemon_started` then crashes).**
- RC-020a's hourly scheduled cadence (path c) has no equivalent fallback. An hour after ready, a detector panic on evidence only visible post-hoc (e.g., a transition-record file written by a post-ready handler with corrupt UTF-8) kills the daemon. Restart loops.
- OQ-RC-003's default ("refuse to reach ready") is fine for startup but does not cover post-ready detector panic.

**Safe or unsafe.** **Unsafe** for the post-ready detector panic; partially safe for startup panic (operator-observable via exit code).

**Concrete spec-text proposal.**
1. Add **RC-020b**: "Detector panics MUST be recovered per [process-lifecycle.md §4.6 PL-018a] and MUST NOT terminate the daemon. On recovery, the daemon MUST emit `reconciliation_detector_panic{run_id, category_attempted, panic_message_redacted}`, classify the affected run as Cat 6b (mechanically unrecoverable), and continue scanning other runs. The panicking detector MUST be marked suspended for the remainder of the daemon lifetime; subsequent cadence runs (RC-020a path c) MUST NOT re-invoke it until the binary is upgraded or restarted."
2. Tighten OQ-RC-003: name the fail-open-vs-strict behavior explicitly. The current "conservative default (a)" covers startup; post-ready is undefined.

### Scenario 5 — Verdict-executed but trailer not written; budget-exhausted mid-kill

**Affected requirements.** RC-018 (budget exhaustion), RC-025 (verdict execution three-step), RC-023 (malformed-verdict fallback), schemas.md §6.4 (verdict-executed trailer).

**What the spec says.** RC-018 specifies four daemon actions on budget exhaustion: (i) emit `reconciliation_budget_exhausted`, (ii) issue fallback `escalate-to-human`, (iii) kill investigator subprocess, (iv) NOT write reconciliation commit. The budget-exhausted event + fallback verdict are the durable trace. RC-023 routes `multiple-verdicts` to escalation.

**What actually happens.**

- Events (i) and (ii) are separate emissions. Per EV-016a "per-event fsync; no multi-event atomicity guarantee," a producer that emits two boundary events may observe a post-crash state where the first landed but not the second.
- Crash between (ii) fallback-verdict emission and (iii) subprocess-kill: the investigator subprocess is still running. It may (a) emit its own verdict event after the fallback, producing the `multiple-verdicts` structural violation RC-021+RC-023 handle; (b) complete its investigation and write its own verdict commit to the task branch. On restart: the daemon sees a task branch with a verdict commit (the subprocess's) but also a fallback `escalate-to-human` event in JSONL (the daemon's). The reconciliation detectors read git for classification, not JSONL for state; the task branch has exactly one verdict commit (the subprocess's) and no verdict-executed commit → Cat 3b. Cat 3b re-executes the investigator's verdict, NOT the daemon's fallback. The operator-surface escalation emitted before crash is now orphaned.
- Alternative crash profile: crash between (i) and (ii). The budget-exhausted event is durable; no fallback verdict was emitted. On restart, the daemon sees an in-flight reconciliation workflow with no verdict commit (because RC-002 forbids intermediate checkpoints). Detection classifies the *target* run fresh per RC-003, re-dispatching a new investigator. The old investigator subprocess may still be alive (orphan swept by PL-006 on restart, but mid-execution if crash was brief). **Now two investigators race for the same target_run_id.** RC-002a's lock would serialize if it existed and survived crash; it doesn't (finding #1), so both may progress.
- The `not-write-a-reconciliation-commit` rule (RC-018 step iv) is the right choice — but it means the budget-exhausted fallback is observable *only* via the JSONL event stream. EV-016 puts `reconciliation_budget_exhausted` and `reconciliation_verdict_emitted` on the fsync-boundary durability class (they should be; the spec doesn't explicitly say so). If either is on a less-durable class, a crash between emit and fsync loses the event.

**Safe or unsafe.** **Unsafe.** The budget-exhausted path has at least two crash windows where the "durable trace" isn't.

**Concrete spec-text proposal.**
1. Amend RC-018 step ordering to: (iii-first) kill the investigator subprocess synchronously BEFORE emitting the fallback verdict event. Cite HC-024 cleanup bounds.
2. Amend RC-018 to require both `reconciliation_budget_exhausted` and the fallback `reconciliation_verdict_emitted` events be emitted as a *single* envelope carrying both payloads, matching EV-016a's guidance ("Producers requiring two events to be durably persisted together MUST emit a single event carrying both payloads"). Alternatively: require both events declare `durability_class = fsync-boundary` AND require the fsync be completed for both before the daemon returns from the budget-exhaustion handler.
3. Add a restart-recovery rule: "On restart, if a `reconciliation_budget_exhausted` event exists in JSONL for an investigator_run_id whose task branch has no verdict commit AND no verdict-executed commit AND whose target run's branch shows no intervening activity since the exhaustion event, the daemon MUST replay the fallback `escalate-to-human` verdict per RC-018 (re-emit fallback verdict + append verdict-executed commit). This replay MUST be idempotent via `target_run_id` dedup in `operator_escalation_required`."

### Scenario 6 — Evidence corroboration under torn JSONL (EV-014b)

**Affected requirements.** RC-014 (JSONL scope bounded), RC-019a (two-store corroboration), RC-INV-004 (corroboration guarantee across detectors), EV-014b (tail-truncation tolerance), EV-023a (evidence-inconclusive).

**What the spec says.** RC-019a requires `evidence.sources.length >= 2` before emitting `store_divergence_detected`. Cat 6b exempt. RC-INV-004 declares the cross-detector invariant.

**What actually happens.**

- EV-014b permits consumers to tolerate tail truncation on JSONL. RC's detector reading JSONL may observe a truncated tail where a corroborating event is *expected* but missing because it was in the event-loss window (between two fsync boundaries).
- Under the current rule, the detector sees one store (git OR Beads) with evidence and no JSONL corroborator, and cannot emit `store_divergence_detected`. RC-019a's third bullet says Cat 6b detections where only one store is readable MUST escalate to Cat 6b directly. But Cat 6b is for *unreadable* stores — the JSONL is readable here, it just doesn't contain the expected event because the event was lost. The detector has no classification path.
- EV-023a (event-model §8.6.8) provides the `divergence_inconclusive` fallback emission. RC-019a references EV-023a but does not name the detector's action on inconclusive evidence. Per R1 critic § 4 line 209–219, this was flagged but the v0.3.0 integration handled it only by adding the corroboration rule, not by adding a `divergence_inconclusive` emission path in RC.
- Specific case: Cat 3a. Intent log on local disk (store 1) + Beads `br show` (store 2). The intent log is corrupt / half-written (fsync before write completed in BI-030; but the directory entry was created without content). Detector reads the intent file, parse fails. Per RC-019a, corroboration fails (only one readable store). Cat 6b fires because JSONL is readable but unhelpful — wait, JSONL is not the corroborator here. The two stores are the intent file + Beads. So "Cat 6b because one store is unreadable" MIGHT apply — but the RC-019a text says "git fsck fails" as the example, implying the unreadable store is git/Beads/JSONL, not arbitrary on-disk harmonik state. The rule doesn't generalize.
- Worse: if the detector's Cat 3a check fails to read the intent file, Cat 3a simply doesn't fire. The detector falls through to Cat 3 generic (which classifies "bead in_progress with worktree missing + no terminal marker"). Cat 3 dispatches an investigator. The investigator reads the same intent file, same parse failure, produces... what? Its playbook (RC-016) is not defined for "torn intent file." **Silent blindspot.**

**Safe or unsafe.** **Partially unsafe.** RC-019a improves on v0.2 but doesn't cover the detector's own-evidence-torn case.

**Concrete spec-text proposal.**
1. Extend RC-019a's third bullet: "For any detector whose evidence sources include harmonik-owned local-disk artifacts (e.g., intent files per Cat 3a, transition-record siblings per Cat 6a detectors), a torn or unreadable artifact MUST cause the detector to emit `divergence_inconclusive` per [event-model.md §8.6.8] EV-023a AND classify as Cat 6a (LLM-triageable integrity violation) rather than Cat 3 generic."
2. Add to RC-014 permitted uses: "Detection that a JSONL event cited by another store (git / Beads / intent file) is absent from the JSONL tail due to the post-crash window (per [event-model.md §4.4] EV-017) MUST NOT trigger emission. The detector MUST consult EV-023's `post_crash_window` marker and route absent-but-expected events through `divergence_inconclusive`, not `store_divergence_detected`."

### Scenario 7 — Cat 0 infrastructure failure during in-flight reconciliation

**Affected requirements.** RC §8.1 Cat 0, RC-012 (Cat 0 pre-check), PL-010 (degraded state), RC-017 (wall-clock budget), RC-018 (budget exhaustion).

**What the spec says.** Cat 0 is checked before any §8 detector (RC-012). On failure, daemon enters `degraded`; no classification proceeds. RC-017 declares every reconciliation workflow has a wall-clock budget.

**What actually happens.**

- RC-012 places Cat 0 pre-check at startup. Post-ready Cat 0 (e.g., Beads DB becomes unreachable an hour in, filesystem goes read-only) is not scoped. PL-010's prose (line 336–342) narrows `degraded` to pre-`ready` only; post-ready degradation is `daemon_degraded` with reasons, not a state transition. So post-ready Beads-unreachable leaves the daemon in `ready` with the detector subsystem failing silently.
- Mid-reconciliation Cat 0: the investigator subprocess is running. The daemon's verdict-execution step (RC-025) would call `br reopen` via the BI-031 adapter. `br --version` begins to fail (Beads binary missing from PATH). Adapter returns `BrUnavailable`. RC-025's mechanical-action table doesn't specify the action. Per BI-010b + BI §6.1a, `BrUnavailable` maps to `BrError` which triggers `store_divergence_detected`. The reconciliation workflow's budget ticks. Eventually RC-018 fires budget exhaustion. Fallback `escalate-to-human`.
- The fallback `escalate-to-human` itself wants to emit `operator_escalation_required` (RC-025's `escalate-to-human` row). That's a pure event emission, not a Beads write — survives Cat 0.
- But the verdict-executed commit (RC-025 step c) requires a git commit. If the git index is locked by a non-harmonik process (a Cat 0 condition), git commit fails. The daemon returns with a partial verdict state: event emitted, commit not landed. Restart classifies as Cat 3b (or Cat 0 if still locked).

**Safe or unsafe.** Mostly safe — Cat 3b catches the verdict-executed-commit-failed case on restart. Unsafe for post-ready Cat 0 in that the spec never names the transition from `ready` to `degraded` outside startup. PL-010 explicitly keeps `degraded` pre-ready only.

**Concrete spec-text proposal.**
1. Add **RC-012a**: "Cat 0 conditions detected post-`ready` during a reconciliation-workflow step (e.g., Beads unreachable during verdict execution) MUST NOT transition the daemon to the `degraded` state (which is pre-`ready` per [process-lifecycle.md §4.2 PL-010]). The reconciliation workflow MUST route through RC-018 budget exhaustion; the daemon MUST emit `daemon_degraded{reason=reconciliation_cat_0_detected}` per [event-model.md §8.7.5] as a health-surface signal without state transition. The post-`ready`-Cat-0 health signal is consumed by operator-nfr §4.9."
2. Amend RC-025 mechanical-action table: for each row with a git-commit step, state "If the commit fails (git index locked, disk full, fsync error), the daemon MUST emit `reconciliation_verdict_execution_deferred{target_run_id, reason, retry_at}`. On next detector cadence, the verdict is reclassified as Cat 3b."

### Scenario 8 — Snapshot-token lifetime mismatch

**Affected requirements.** RC-015 (InvestigatorInput bound to snapshot token), RC-024 (staleness check precedes execution), schemas.md §6.1 (SnapshotToken record).

**What the spec says.** Snapshot token is `(git_head_hash, beads_audit_entry_id, captured_at_timestamp)`. Staleness fires if target run's branch advanced OR target bead's audit advanced.

**What actually happens.**

- No upper bound on snapshot-token age. An investigator with a 900-second Cat 6a budget (RC-017 default) may take 14 minutes. A scheduled-cadence detector (RC-020a) running hourly may dispatch an investigator at T=0, produce a verdict at T+30s, execute at T+30.1s. No issue. But: a reconciliation workflow interrupted by daemon crash at T+5s, restart at T+300s, classifies the target as Cat 5 (clean, no in-flight investigator because RC-002 forbids checkpoints), re-dispatches fresh reconciliation at T+301s against a new snapshot. Meanwhile, the *original* investigator subprocess may still be alive (orphan swept by PL-006, fine). No mid-flight snapshot staleness — RC-003's recursion-bounding handles this.
- But consider the operator path: operator runs `harmonik reconcile --run X` (RC-020a path b) against a specific target. The dispatched investigator begins with snapshot S1 at T=0. Operator runs the *same* command at T+1s. RC-002a's lock should serialize — but if the lock is in-memory (finding #1) or doesn't exist in the cited location, both investigators proceed. They share the same snapshot bounds IF the lock serialized; they capture different snapshots if they didn't. First emits verdict at T+300; second emits at T+301. RC-021 says one verdict per workflow (fine, each workflow emits one). But the daemon executes the verdict whose staleness check passes first. The second verdict-execution's staleness check now fails (branch advanced by the first execution's verdict-executed commit). Second routes to `reconciliation_verdict_stale` + fresh reconciliation. Infinite loop if operator keeps invoking.
- Staleness reason enum (StaleDivergenceReason in schemas.md §6.1) has two values: `git-branch-advanced` and `beads-audit-advanced`. It does not cover "JSONL tail truncated between snapshot and execution" — a non-issue for verdict execution (verdict execution doesn't re-read JSONL) but worth noting: the snapshot token doesn't include a JSONL position, so a post-snapshot JSONL event loss is invisible to the staleness check. For RC-014 observational reads this is fine; for evidence-corroboration (RC-019a) across the snapshot lifetime, less fine.

**Safe or unsafe.** Safe for single-investigator crash; unsafe for double-dispatch when RC-002a lock fails.

**Concrete spec-text proposal.**
1. Add to RC-015: "The snapshot token MUST expire when its `captured_at_timestamp` exceeds `max(budget_wall_clock_seconds * 2, 3600 seconds)`. Expired snapshot tokens MUST fail the RC-024 staleness check unconditionally. This prevents an investigator whose daemon crashed mid-execution from emitting a verdict against a snapshot captured an arbitrarily long time ago."
2. Add a `snapshot-token-expired` value to `StaleDivergenceReason` enum in schemas.md §6.1.

### Scenario 9 — Cat 3c auto-resolver vs concurrent mid-close

**Affected requirements.** RC §8.6 Cat 3c, RC-025 (`accept-close-with-note` execution), BI-010b (reconciliation-driven writes), BI-031 (status-check-before-reissue).

**What the spec says.** Cat 3c: merge commit for run R exists on target branch; bead R is still `in_progress`; no subsequent in-flight checkpoints. Auto-verdict `accept-close-with-note` with mechanical close via BI adapter.

**What actually happens.**

- Concurrent scenario: a real workflow node N is mid-close attempt for run R. N has fsync'd intent log `<R>:close`; N invokes `br close`. Meanwhile, scheduled-cadence detector (RC-020a path c) fires independently and sees the merge commit + `in_progress` bead + "no subsequent in-flight checkpoints" (N's transition hasn't landed yet). Detector dispatches Cat 3c auto-resolver which writes `br close` via adapter with idempotency key `<R>:close-reconciliation-driven` (BI-010b route, distinct key from N's `<R>:close`).
- Two concurrent `br close` writes for the same bead R. Beads's own semantics: the second close either no-ops (already closed) or errors. Per BI-031b, a parse failure on the structured output classifies as `BrSchemaMismatch`; but a successful double-close returns OK twice. Both calls succeed, both write audit entries with different idempotency keys, both emit `reconciliation_verdict_executed` or equivalent. The Beads audit log now carries two close entries for one run.
- R1 critic §4 "Worked example" (line 41) flagged this pattern: adapter wrote intent + `br close` succeeded but daemon didn't record success → Cat 3a fires on restart. Cat 3c is a *different* trigger pattern but the same race: two writes for one logical close.
- RC-025's `accept-close-with-note` idempotency rule: "repeated re-runs append the same annotation and see the close already landed." True on re-run of the same workflow; does not cover the concurrent-two-workflows case. The idempotency key per-caller is distinct, so Beads doesn't deduplicate.

**Safe or unsafe.** **Unsafe under concurrent close-write + scheduled-cadence detector.** The window is small but real.

**Concrete spec-text proposal.**
1. Amend RC §8.6 detection rule: "Cat 3c MUST fire only if no in-flight intent-log entry exists for any close operation against target bead R. The detector MUST scan `.harmonik/beads-intents/` per [beads-integration.md §4.10] for any `<R>:close` or `<R>:*` pending intent; if present, Cat 3c classification is suppressed in favor of waiting for the ordinary close to complete (Cat 2 routing for that node if still in-flight, or next detector cadence)."
2. Alternatively, unify the reconciliation-driven close idempotency key with the ordinary-close idempotency key: require Cat 3c's auto-resolver to use key `<R>:close` (same key an ordinary close node would use), not a reconciliation-tagged variant. Let BI-031's status-check-before-reissue detect the already-landed case.

### Scenario 10 — Reconciliation emission during EV-016 fsync stall

**Affected requirements.** RC-013 (category_assigned emission), RC-018 (budget_exhausted), RC-024 (verdict_stale), RC-025 (verdict_executed), EV-016 (per-event fsync), EV-016a (no multi-event atomicity), ON-031 (30s RTO target).

**What the spec says.** Every `fsync-boundary` event's `Append` returns only after fsync completes. RC-017 + RC-018 bound the *investigator* wall clock; no spec bounds the *detector* wall clock.

**What actually happens.**

- Startup reconciliation: §7.1 pseudocode walks in-flight runs, emits `reconciliation_category_assigned` per run, dispatches. Each emission is at least one `fsync-boundary` call per RC-013's nature (classification is load-bearing for correctness; should be F-class). If RC-014's `store_divergence_detected` also fires on N runs, that's another N fsyncs. RC-019a corroboration doesn't add emissions but requires reading git + Beads per run.
- On a slow SSD under fsync saturation (e.g., `fsync` taking 50 ms p95), 100 in-flight runs produces 5+ seconds of raw fsync latency before `ready`. ON-031 nominal fixture target is 30s. The fixture in ON-032 criterion 1 sizes as "≤ a few hundred open beads, ≤ a few dozen in-flight runs" (~25 runs, so ~1.25s fsync). Tolerable at MVH.
- Post-ready Cat 0 (per finding in Scenario 7): disk becomes slow (temporary flash exhaustion, disk-full, EC fsync retries). `fsync` latency spikes to 5s. Detector scheduled-cadence dispatch (RC-020a path c) fires for all in-flight runs at once. Daemon emits N × `reconciliation_category_assigned` events. Each fsync blocks. Daemon's goroutine handling the scheduled detector is blocked serially; concurrent agent dispatches (also fsync-bound for checkpoints) stall.
- Is the daemon non-responsive? `harmonik status` goes through the socket JSON-RPC; JSON-RPC dispatch is not on the fsync goroutine (per PL-003a it's per-connection goroutines). So status responds. But new agent claims (`claim-next`) that require emission of `run_started` or similar also fsync. Throughput drops to fsync-rate.
- EV-011a has the spill-file path for bus overflow; irrelevant here (this is the *writer* stalling, not a *consumer* queue).
- The spec does not audit detector-aggregate fsync cost. At post-MVH workloads (500+ in-flight runs, scheduled-cadence reconciliation + ordinary dispatch contending), this is a real operational concern.

**Safe or unsafe.** Safe at MVH fixture size; under-specified at larger scales.

**Concrete spec-text proposal.**
1. Add **RC-020c**: "The scheduled-cadence detector pass (RC-020a path c) MUST NOT emit classification events for runs whose category has not changed since the most recent `reconciliation_category_assigned` event for the same `(target_run_id, snapshot_token.git_head_hash)`. Re-emission on an unchanged snapshot is a no-op."
2. File an OQ: batched fsync for scheduled-cadence detector at post-MVH scale. Reference EV-016a's "batching multiple boundary events into a single fsync is best-effort only and reserved for post-MVH amendment."

### Scenario 11 — Multi-investigator clash via operator + startup race

**Affected requirements.** RC-002a (at-most-one), RC-020a (three dispatch points), RC §8.11 Cat 6a (two task branches advertising Harmonik-Run-ID).

**What the spec says.** RC-002a locks per `target_run_id` with 5s bounded wait. RC-020a names three dispatch points. Cat 6a's third bullet: bead `in_progress` with 2+ task branches each advertising `Harmonik-Run-ID` without verdict-executed marker → Cat 6a.

**What actually happens.**

- Path: operator runs `harmonik reconcile --run X` at T=0 while startup reconciliation (path a) is still in-flight for the same X (e.g., daemon is mid-dispatch, hasn't yet reached ready). RC-002a's lock serializes if it exists and is durable — per finding #1, it may not be.
- Without a durable lock, both paths create investigator workflows. Each creates a task branch. Each eventually emits a verdict commit. The Cat 6a detector's third bullet fires on the NEXT cadence run (path c, hour later) when scanning X. Cat 6a dispatches a *third* investigator to reason about why X has two task branches.
- Recursion: the third investigator, with its own task branch, when scanning X finds THREE task branches. Cat 6a fires again. Fourth investigator, four task branches...
- Bound: RC-002a should prevent this. If the lock isn't durable or doesn't exist, there is no bound. RC-INV-001's sensor (audit-log filter on `reconciliation_verdict_*`) would *detect* this after the fact but does not *prevent* it.
- Even with a durable lock, the 5s wait timeout in RC-002a routes the second dispatch to Cat 4 (if first still progressing) or skips. Cat 4 auto-resumes — but the target is a *reconciliation workflow awaiting its peer*, not an agent waiting for a retry timer. Cat 4's auto-resume re-spawns the node. Which node? The outer run's node isn't even the thing waiting; the second reconciliation workflow is. The semantics break.

**Safe or unsafe.** **Unsafe** as currently specified. The spec's own Cat 6a detector fires on a state the spec permits to exist (two-plus task branches from serialization failure).

**Concrete spec-text proposal.**
1. Resolve finding #1 first (make RC-002a's lock durable).
2. Amend RC-002a's second-dispatch fallback: "If the first reconciliation workflow is still progressing past the 5-second wait, the second dispatch MUST be SKIPPED (not routed to Cat 4). Emit `reconciliation_dispatch_deduplicated{target_run_id, first_investigator_run_id, second_dispatch_source}` as the observational signal. The second dispatch source (startup, operator, scheduled) is recorded for audit."
3. Amend Cat 6a's third-bullet detection: "Cat 6a MUST fire on two-plus task branches only if (i) no active RC-002a lock exists for the target_run_id, AND (ii) none of the task branches carry a `reconciliation_dispatch_deduplicated` event pairing. A cleanup path under RC-INV-001 audit MUST remove orphan investigator task branches that did not reach verdict emission (their `run_started` has no paired `run_failed`/`run_completed` after a bounded interval)."

### Scenario 12 — Cat 6 escalation under operator-pause

**Affected requirements.** RC §8.11 Cat 6a (escalate-to-human default), §8.11a Cat 6b (auto-escalate to operator), RC-025 `escalate-to-human` row, ON-010 (pause carve-out during reconciling), ON-008 (between-task invariant).

**What the spec says.** Cat 6a dispatches investigator → escalate-to-human. Cat 6b auto-escalates without investigator. `escalate-to-human` emits `operator_escalation_required` + marks outer run quarantined. Deduplicated by `target_run_id`. ON-010 says pause does not interrupt reconciliation workflows; pauses issued during `reconciling` are queued.

**What actually happens.**

- Scenario 12a: operator pauses daemon while a Cat 6a investigator is running. Per ON-010, pause is queued. Investigator completes, emits `escalate-to-human`. Daemon executes verdict per RC-025: emits `operator_escalation_required`, appends verdict-executed commit. Then queued pause fires → drain sequence (ON-027) → `paused`. Operator sees both the escalation event and the paused state. **Safe.**
- Scenario 12b: operator pauses; while paused, daemon crashes. Pidfile stale (PL-024). Restart runs PL-005 from step 0. Reconciliation fires (path a). The Cat 6a target run is re-classified. Its in-flight investigator (pre-crash) left... what? RC-002 forbids intermediate checkpoints. No verdict commit → Cat 6a re-fires → new investigator dispatched. The old `operator_escalation_required` event was emitted pre-crash; new event will (re-)emit on completion of the new investigator. RC-025's dedup-by-`target_run_id` handles this: "subsequent emissions are deduplicated." **But:** the dedup is on the event bus, and the event bus is re-initialized on restart. Dedup state is in-memory per EV § 4.3 (not a durable registry). Restart re-emits.
- Scenario 12c: operator pauses during `reconciling`. ON-010 queues pause. BUT: the daemon is `starting` → `reconciling` per the state machine, not yet `ready`. The pause is queued. Reconciliation encounters Cat 6b (JSONL corrupt past byte offset). Per §8.11a, auto-escalate without investigator. Daemon emits `operator_escalation_required`. Does the daemon proceed to `ready`? OQ-RC-003 default says (a): "Conservative. Daemon refuses to reach `ready` when reconciliation cannot classify." So daemon stays in `reconciling` indefinitely. Queued pause never fires (pause applies at the boundary event "all reconciliation runs have either resumed into normal flow or produced a verdict" per ON-010). Operator's pause command was queued but never fires. Operator gets no feedback — the daemon is stuck `reconciling`, not `paused`. **Unsafe observability.**

**Safe or unsafe.** Mostly safe via dedup; unsafe for post-crash dedup state loss and for operator-pause-stuck-during-Cat-6b.

**Concrete spec-text proposal.**
1. Add to RC-025 `escalate-to-human`: "Dedup-by-`target_run_id` for `operator_escalation_required` MUST use the most recent `reconciliation_verdict_executed` event on disk as the dedup key source, NOT in-memory state. On daemon restart, the dedup key set MUST be reconstructed from JSONL scan of the last N `reconciliation_verdict_executed` events (N configurable, default 1000). Re-emission after restart is acceptable if the prior emission's event is not durably observable."
2. Amend ON-010 (or file an OQ against reconciliation + operator-nfr): "If reconciliation detects Cat 6b during startup and cannot proceed to `ready` per OQ-RC-003's conservative default, a queued pause command MUST be resolved as `operator_command_rejected{reason=daemon_not_ready,underlying_condition=cat_6b_blocking_reconciliation}` so the operator receives feedback rather than indefinite silence."

### Scenario 13 — Cat 3b re-execution infinite loop

**Affected requirements.** RC §8.5 Cat 3b, RC-026 (verdict-execution discovery on restart).

**What the spec says.** A verdict commit with no verdict-executed commit → Cat 3b. Auto-resolver re-executes verdict; if staleness, re-dispatch fresh reconciliation; if execution fails, escalate to Cat 3 generic.

**What actually happens.**

- A flapping failure in the mechanical action (e.g., `br reopen` intermittently times out due to Beads SQLite contention): each daemon restart re-enters Cat 3b, re-executes, fails non-persistently, does NOT escalate to Cat 3 generic (because §8.5 says escalate-if-fails-*persistently*; intermittent failures don't qualify).
- No retry counter. No backoff. No post-N-attempts circuit-break. The same reconciliation workflow re-executes on every restart indefinitely.
- RC-INV-004 audit sensor checks `evidence.sources.length >= 2` on `store_divergence_detected`; it does not check repeated Cat 3b re-execution.
- Operationally this leaks: every restart adds a cycle of (capture snapshot, pass staleness, attempt action, fail, don't commit, emit nothing durable) that costs time and fsync. No operator signal surfaces until eventual "persistent" threshold is crossed — but "persistent" is not defined.

**Safe or unsafe.** Unsafe under flapping mechanical-action failures.

**Concrete spec-text proposal.**
1. Amend RC-026: "The daemon MUST maintain a durable attempt counter per `(investigator_run_id, verdict)` at `.harmonik/reconciliation/attempts/<investigator_run_id>.json` with fsync-on-update. On each Cat 3b re-execution, the counter increments. On the Nth attempt (N = 5 default, operator-configurable), the verdict MUST be routed to Cat 3 generic with a `flapping-execution` reason code. The counter MUST be cleared when a `Harmonik-Verdict-Executed: true` trailer lands."
2. Add to §8.5: emit `reconciliation_verdict_execution_retry{investigator_run_id, attempt, last_error}` per attempt as the observability signal.

### Scenario 14 — Reconciliation-of-reconciliation recursion

**Affected requirements.** RC-001 (reconciliation as a workflow), RC-002 (exactly one checkpoint commit), RC-003 (bounded recursion), RC-INV-001 (uniqueness audit).

**What the spec says.** Reconciliation workflows emit exactly one checkpoint commit (the verdict commit). Reconciliation-workflow nodes MUST NOT be classified under Cat 1/2. The `workflow_class = reconciliation` tag is the filter. Cat 3b covers verdict-emitted-but-not-executed; Cat 5 covers no-in-flight.

**What actually happens.**

- Happy path: reconciliation workflow R1 for target T. R1 emits verdict commit, then verdict-executed commit. Fine. On restart, R1's branch has both commits → not classified. T is classified fresh.
- Crash mid-R1-investigator (investigator agent subprocess running, no verdict yet): R1 has `run_started` in JSONL, no verdict commit, no verdict-executed commit. On restart, PL-006 reaps the orphan investigator subprocess. R1's workflow state in memory is gone. R1's branch exists in git (task branch created at dispatch) but carries no commits. T is re-classified fresh; RC-002a's lock (if durable) prevents double-dispatch. New R2 for T dispatches. R1's abandoned task branch lives on disk until cleaned. **R1's task branch is classified as what?** It has `Harmonik-Run-ID = R1` on branch metadata (the branch was created by workspace-model.md WM-005). Detector filter RC-010 says "classify each in-flight run independently" — is R1 an in-flight run? RC-003 says no (reconciliation-workflow nodes are excluded). But the `workflow_class` filter per RC-INV-001 relies on the Workflow registry entry; R1's registry entry was in memory and lost. How does the restart detector know R1's branch belongs to a reconciliation workflow?
- Answer: by reading the branch's commits. But R1 has NO commits (crashed before verdict). So there's no `Harmonik-Workflow-Class: reconciliation` trailer to read. The branch looks like an ordinary in-flight task branch for an ordinary run R1. Detector classifies R1 as... something. Likely Cat 5 (nothing in-flight for the run since no checkpoints) or Cat 3 (some partial state). Cat 5 routes no-op. Cat 3 dispatches an investigator — an investigator to investigate a reconciliation-workflow ghost.
- The `workflow_class` tag lives only in the Workflow record (schemas.md §6.5) — an *in-memory* registry bootstrapped at startup per PL-020a. The tag is NOT durably trailer-written to branches at dispatch time. So restart cannot recover the tag for a workflow with no commits.

**Safe or unsafe.** **Bounded** but with an observability blindspot: R1's ghost task branch is indistinguishable from an ordinary Cat 5 or Cat 3 target on restart. The `workflow_class` filter is not durable.

**Concrete spec-text proposal.**
1. Add a requirement that reconciliation-workflow task branches carry a durable marker at dispatch time: "On dispatch of a reconciliation workflow (per RC-001), the daemon MUST create the task branch with an initial empty commit carrying the trailer `Harmonik-Workflow-Class: reconciliation` AND `Harmonik-Target-Run-ID: <target_run_id>`. This dispatch-marker commit is the ONE commit exception to RC-002 (which forbids checkpoints but permits branch-metadata commits). Detectors MUST use this trailer to filter reconciliation workflows out of ordinary classification per RC-003, even when no verdict commit exists."
2. Update RC-002 to carve out the dispatch-marker commit from the "one commit" rule: "The verdict commit is the *only* checkpoint; a dispatch-marker commit per [new requirement] is permitted and is NOT a checkpoint."
3. Update Cat 5 detection (§8.8) to exclude branches bearing `Harmonik-Workflow-Class: reconciliation` in their earliest commit, even if no verdict lands.

### Scenario 15 — Schema migration mid-reconciliation (Cat 4 / Cat 6b case)

**Affected requirements.** §6.4 (schema evolution, N-1 readable), §8.9 Cat 4 (recoverable known state — *wait, §8.7 is Cat 4 now*), ON-018 (checkpoint format stability), operator pause for upgrade.

**What the spec says.** Verdict schema N-1 readable per ON-020. Breaking enum changes require migration release at operator pause. Upgrade path: paused → exec-replace → running. Recoverability invariant: paused implies drain complete (ON-021).

**What actually happens.**

- ON-021 requires drain complete before upgrade. Drain (ON-027) completes in-flight runs to next checkpoint. For a reconciliation workflow (no intermediate checkpoints per RC-002), "next checkpoint" is the verdict commit. Drain waits for verdict emission. Fine, at most 900s per Cat 6a budget.
- But RC-002a's lock (if durable) may extend the wait: a reconciliation workflow with an RC-002a lock means no second reconciliation can dispatch. If the workflow's mechanical action deadlocks (Beads SQLite slow), drain timeout fires per ON-029. Daemon SIGKILLs the subprocess, pauses anyway. Restart after upgrade: Cat 3b fires (verdict commit exists, no verdict-executed), RC-026 re-execution runs — but the *new binary* is reading the old verdict commit. If the verdict schema changed (N-1 case), the new binary must parse N-1 format. schemas.md §6.1 `VerdictEvent` has `schema_version: Integer`. Adapter deserializes the old version. If the schema_version is not recognized, RC-023 malformed-verdict fallback fires with `unknown-verdict-value` or `wrong-type`.
- **The fallback verdict (`escalate-to-human`) is synthesized at execution time by the daemon.** The original verdict commit's payload is discarded. This is the right answer — RC-023 says "NOT attempt to interpret the malformed payload." But the operator escalation reason is now ambiguous: is this a crash-recovered old verdict that the new binary can't read, or is this a genuine investigator schema violation? The operator-facing event (`operator_escalation_required`) doesn't carry this distinction. The escalation reason is tracked in event-model §8.6 but not plumbed through RC-023's payload.
- Further: if the `Harmonik-Verdict-Executed` trailer shape changed between versions (schemas.md §6.4 declares it as marker-only — unlikely to change), an N-1 reader might see an unrecognized trailer value. Spec §6.4 says any value other than `"true"` is malformed. If a future version adds `"true-with-note"` as a post-MVH extension, N-1 binaries see it as malformed → RC-023 `wrong-type`.

**Safe or unsafe.** Safe via the malformed-verdict path, with an observability gap on escalation-reason. N-1 compatibility holds at the event-payload level but not at the trailer-extension level unless schema-version-in-trailer is added.

**Concrete spec-text proposal.**
1. Amend RC-023: "The `reconciliation_verdict_malformed` event payload MUST carry an additional field `malformation_source ∈ {investigator-emission, schema-version-mismatch, trailer-shape-mismatch}`. The `operator_escalation_required` fallback MUST plumb this field into the escalation reason so operators can distinguish genuine investigator failures from upgrade-induced format drift."
2. Amend schemas.md §6.4 to declare the trailer schema-versioned: add `Harmonik-Verdict-Executed-Schema-Version: <integer>` as a co-resident trailer on the verdict-executed commit, bumped on trailer-shape change.

---

## 3. Atomicity claims audit

| Claim | Location | Actually atomic? |
|---|---|---|
| "verdict event's emission MUST be atomic with the investigator's verdict commit" | RC-022 | **No.** Event emission is a JSONL append (+ fsync per EV-016); verdict commit is a git commit. Two separate filesystem operations. The spec's "atomic" here means "co-located in the same commit payload," not "crash-atomic." The prose is misleading. |
| "lock MUST be released atomically with the verdict-executed commit" | RC-002a | **No.** See finding #2. Two separate operations. |
| "after every mechanical action, the daemon MUST append a commit..." | schemas.md §6.2 trailer | **Not atomic with the mechanical action.** Mechanical action lands (e.g., `br reopen` succeeds), event emission may or may not fire, trailer commit may or may not land. Three independent failure points. |
| "Cat 3a adapter reads audit log to determine whether the write landed" | §8.4a | **Depends on Beads.** BI-031's status-check-before-reissue uses `br show`, not the audit log directly. The detection rule's cited evidence source doesn't match the resolver's evidence source. |
| "exactly one `reconciliation_verdict_emitted` event over its lifetime" | RC-021 | **True** by RC-002's one-commit rule, because the emission is atomic with the commit at the git-object level (one commit = one trailer set). Crash after emission-before-commit loses nothing durable; crash after commit leaves the emission durable. Sound. |
| "reconciliation workflows emit exactly one checkpoint commit" | RC-002 | **True** at the semantic level; see scenario 14 for the dispatch-marker-commit nuance. |

**Finding.** Three of the six audited atomicity claims are prose, not mechanical. RC-022 and RC-002a need rewording: replace "atomically" with "co-located" or "in the same commit payload" where the claim is about data co-location, not crash-atomicity. Add explicit crash-atomicity rules where the claim was meant to be stronger.

## 4. Fsync discipline audit

Reconciliation does not own the JSONL fsync discipline (EV-016 owns it) or the adapter intent-log fsync (BI-030 owns it). But RC makes implicit assumptions about both:

- **RC assumes `reconciliation_verdict_emitted` is `fsync-boundary`.** schemas.md §6.6 lists the event but does not restate the durability class. event-model §8.6 should declare it (not verified inside this review; checkpoint for EV author).
- **RC assumes `reconciliation_verdict_executed` is `fsync-boundary`.** Same caveat.
- **RC assumes `store_divergence_detected` is `fsync-boundary`.** Same.
- **RC assumes `reconciliation_category_assigned` is `fsync-boundary`.** RC-013 says emission MUST precede dispatch; if the event is lossy-tail-ok, a crash between emit and dispatch can lose the classification record entirely. Should be F-class.
- **RC does NOT own any local-disk state with its own fsync rule.** The proposed `.harmonik/reconciliation-locks/` (finding #1 remedy) and `.harmonik/reconciliation/attempts/` (Scenario 13 remedy) would need fsync-on-update discipline.
- **RC-019 WIP capture: fsync'd?** RC-019 says the investigator MUST capture WIP "into the reconciliation commit's body and/or as annotated files under `.harmonik/reconciliation/<investigator_run_id>/wip-capture/`." The "annotated files" path is not in git; it's on local disk. No fsync rule. A crash after WIP is written to disk but before the reconciliation commit lands can lose the WIP capture silently.

**Proposal.** Add an explicit RC requirement: "All RC-owned local-disk artifacts (`.harmonik/reconciliation-locks/`, `.harmonik/reconciliation/<id>/wip-capture/`, `.harmonik/reconciliation/attempts/`) MUST be fsync'd on write including parent-directory fsync per the durability pattern of [event-model.md §4.4] EV-016. RC-owned events MUST be declared fsync-boundary in event-model §8.6 per-event subsections." Also: cross-check with event-model that each RC event row carries `F` class.

## 5. Recovery rules coverage

Crashes this spec claims to recover from, and whether it does:

| Crash profile | RC's claimed recovery | Holds? |
|---|---|---|
| Crash mid-investigator-agent execution | RC-003 bounded recursion: no intermediate checkpoints; outer run re-classified | Yes (given Scenario 14 amendment to durably mark the task branch at dispatch) |
| Crash after verdict emitted, before executed | Cat 3b auto-resolver re-executes via RC-026 | Yes for `reopen-bead` / close; partial for `resume-with-context` (finding #5, Scenario 2) |
| Crash during budget-exhausted fallback | RC-018 says "no commit written"; next pass re-classifies fresh | Partial — two-event non-atomicity, orphan investigator (Scenario 5) |
| Crash during detector run | RC-020a idempotency claim | Yes at emit-before-action boundary; observability gap at emit-after-action (Scenario 1) |
| Crash during Cat 3a adapter re-issue | BI-031 status-check-before-reissue | Yes for BI-031's actual protocol; the detection rule cites the wrong evidence (finding #3) |
| Crash during Cat 3c auto-close | Idempotency key + BI-031 | Yes for single-path; racy under concurrent ordinary close (Scenario 9) |
| Crash during operator-paused reconciliation | ON-010 pause queued; dedup by target_run_id | In-memory dedup state lost (Scenario 12b) |
| Crash mid-upgrade | ON-021 paused-implies-drain; RC N-1 schema | Yes if schema_version plumbed; trailer N-1 gap (Scenario 15) |
| Crash during concurrent dual-dispatch | RC-002a lock | Depends on lock primitive (findings #1, #2) |
| Post-ready Cat 0 | Not explicitly covered | PL-010 narrows `degraded` to pre-ready; RC has no post-ready Cat 0 rule (Scenario 7) |

**Summary.** 4 holds / 5 partial / 1 uncovered. The partial ones all cluster around emission-commit-lock non-atomicity and in-memory state loss.

## 6. Temp+rename pattern audit

RC does not own any temp+rename write paths directly. Its two on-disk surfaces (WIP capture, proposed reconciliation-locks) should use temp+rename for atomic-replace semantics. Currently unspecified.

BI's intent-log write (BI-030) says "persist an intent-log entry... MUST fsync the file per the durability contract of event-model.md §4.4." Does not say temp+rename. An implementer who writes directly to the target path with `O_CREAT|O_TRUNC` can produce a torn file if crash hits mid-write. BI should clarify; RC should demand temp+rename for any reconciliation-owned artifact that the Cat 3a detector (or its successor under finding #6) reads.

**Proposal.** Add to the finding-#1 remedy (reconciliation-locks file) and finding-Scenario-13 remedy (attempts file): "Writes MUST use temp+rename (write to `<path>.tmp`, fsync tmp, fsync parent directory, rename to `<path>`, fsync parent directory)." Follow the pattern PL-002 references (line 171: "`fsync(fd)`; `fsync(parent_directory_fd)` (the parent-directory fsync is REQUIRED — without it, a power-loss after step 4 can lose the file content on APFS and ext4-data=ordered)").

## 7. Retry vs re-run discipline

RC distinguishes clearly between `reopen-bead` (re-run, fresh run_id) and `reset-to-checkpoint` (intra-run, preserve run_id). RC-028/029/030 own this. Retry (within a node) is out of scope for RC — it's handler-contract territory. Re-run is RC's job.

The gap: **re-execution on restart** (RC-026's Cat 3b path) is a third category distinct from retry and re-run. It's a replay of the verdict-execution mechanical action, not a retry of the investigator's work and not a re-run of the outer run. The spec doesn't name this category explicitly. Scenario 13 exposed that RC-026 has no retry budget, no backoff, no cumulative-attempt bound. Treat it as its own discipline:

**Proposal.** Add a §4.5 note: "Verdict-execution re-attempt on Cat 3b is neither retry (handler-contract-level) nor re-run (RC-028 level). It is a replay of a completed investigator's mechanical action. The attempt counter of RC-026 (proposed under Scenario 13) MUST be distinct from any retry counters maintained by the outer run's handler."

## 8. Cross-spec coordination per scenario

| Scenario | Cross-spec concerns | Spec(s) touched |
|---|---|---|
| 1 | Duplicate `reconciliation_category_assigned` | event-model consumer semantics |
| 2 | Cat 3a detection rule mismatch with BI-031 | beads-integration |
| 3 | RC-002a lock primitive | process-lifecycle (§4.3 claim) or RC self-hosted |
| 4 | Detector panic recovery | process-lifecycle PL-018a |
| 5 | Budget-exhaustion atomicity | event-model EV-016a |
| 6 | Torn-intent-file classification | beads-integration BI-030 fsync discipline |
| 7 | Post-ready Cat 0 | process-lifecycle PL-010, operator-nfr §4.9 |
| 8 | Snapshot TTL | schemas.md §6.1 |
| 9 | Cat 3c concurrent close | beads-integration BI-010b |
| 10 | Fsync cost at scheduled cadence | event-model EV-016a |
| 11 | Cat 6a ghost branches | RC self-hosted |
| 12 | Escalation dedup across restart | event-model (durability of dedup keys) |
| 13 | Flapping re-execution | RC self-hosted |
| 14 | Reconciliation-workflow task-branch marker | workspace-model WM-005 (branch creation), execution-model §4.4 (trailers) |
| 15 | Schema migration trailer | operator-nfr ON-018, execution-model §6.2 (trailer ownership) |

Cross-spec load: 9 of 15 scenarios need at most one sibling-spec coordination; 3 are RC-self-contained; 3 touch event-model's durability-class declarations (should be verified during event-model's next review).

## 9. Requirements that held up well

- **RC-002 one-commit rule.** Elegant and crash-safe. The recursion-bounding argument is load-bearing and survives scrutiny given the Scenario 14 durable-marker amendment.
- **RC-003a priority-ordered first-match.** Closes the R1 critic's disjointness concern. Under crash, priority order is deterministic so long as evidence is read consistently.
- **RC-019a evidence-corroboration.** Bounds false-positive divergence emission. The finding is scope (doesn't cover harmonik-local torn artifacts), not correctness.
- **RC-020a idempotent across dispatch points.** The reproducible-on-same-snapshot rule is sound. The three dispatch points are orthogonal in principle (lock durable serialization in practice — see finding #1).
- **RC-023 malformed-verdict fallback.** ZFC-appropriate. Converts LLM cognitive failure to deterministic escalation. Crash-safe because the fallback is synthesized deterministically from the malformation reason.
- **RC-024 staleness check.** Correctly defends against post-snapshot drift. Proposed TTL addition (Scenario 8) would close the "arbitrary-age snapshot" hole.
- **RC-028 `reopen-bead` → fresh run_id.** The explicit "continuation of the prior run_id is forbidden" rule is crash-safe (restart reads git + Beads; no in-memory continuation state to lose).
- **Cat 6a third bullet (two-task-branches).** Correct detection rule assuming RC-002a lock is durable; finding #1 is about the lock, not this detector.

## 10. Hidden crash-safety assumptions

Things the spec relies on but doesn't state:

1. **JSONL tail lossiness is bounded to the post-crash window.** Reconciliation's RC-014 reads JSONL; EV-017 says post-crash-window events may be lost. RC-019a's corroboration rule handles this but doesn't cite the `post_crash_window` marker from EV-023.
2. **Workflow registry is in-memory.** RC-002a's lock depends on it; the registry is re-bootstrapped on restart per PL-020a. The lock is therefore not durable unless the spec mandates a durable primitive (finding #1).
3. **`workflow_class` tag is in-memory.** RC-INV-001's filter relies on this. Scenario 14 shows the tag is not recoverable for workflows with no commits.
4. **Beads audit-log and `br show` status are consistent.** Finding #3 exposes the RC §8.4a / BI-031 drift.
5. **Git commit + event emission co-location is "atomic."** RC-022's wording. Not crash-atomic in the kernel sense.
6. **Detector emissions are fsync-boundary class.** Not declared in RC; relies on event-model §8.6 per-event durability class. Worth cross-checking.
7. **Operator escalation dedup state persists across restart.** RC-025's `escalate-to-human` says "subsequent emissions are deduplicated by target_run_id." Dedup state is in-memory; Scenario 12b shows it's lost on restart.
8. **`Harmonik-Run-ID` trailer appears on the task branch BEFORE any verdict commit.** RC-010's filter-by-trailer only works if task branches carry the trailer. Workspace-model sets branch names (`run/<run_id>`) but the trailer lives on commits. Pre-verdict task branches carry no commits under RC-002 → no trailer → RC-010 filtering is by branch name, not trailer. The spec says "filter by trailer" but mechanically it's by branch. Minor but worth stating.
9. **Operator-paused-while-Cat-6b-blocking produces operator feedback.** Scenario 12c shows it doesn't.

## 11. Proposed amendments (consolidated)

Ordered by blocking-ness:

**Blocking (crash-safety correctness):**

- **A1 (finding #1, Scenario 3).** Define the RC-002a lock primitive explicitly. Either add a PL §4.3 requirement for a per-`target_run_id` flock-based registry lock, or migrate the primitive into RC's schemas.md as `.harmonik/reconciliation-locks/<target_run_id>.lock` with fsync, temp+rename, and stale-lock sweep semantics. Prefer the latter; RC owns reconciliation infrastructure.
- **A2 (finding #2, Scenario 3).** Add RC-002b: "Lock acquisition and verdict-executed-commit emission are NOT atomic. On restart, a reconciliation-lock file for a `target_run_id` whose investigator task branch carries `Harmonik-Verdict-Executed: true` MUST be deleted. A lock file whose task branch carries NO verdict-executed commit MUST route through Cat 3b."
- **A3 (finding #3, Scenario 2).** Reshape RC §8.4a's detection rule to cite BI-031's actual protocol: replace the audit-log-idempotency-key evidence with the `br show`-status evidence.
- **A4 (Scenario 5).** Amend RC-018 step ordering: kill the investigator subprocess BEFORE emitting the fallback verdict. Require the budget-exhausted event and the fallback verdict event to be emitted as a single envelope OR require both to be `fsync-boundary` class with explicit fsync-before-return on the budget-handler. Add a restart-recovery rule for replay of the fallback.
- **A5 (Scenario 2, `resume-with-context`).** Amend RC-025 `resume-with-context` idempotency: context injection MUST be keyed on `(target_run_id, investigator_run_id)`; repeated application is a no-op. Reconstruction via verdict-executed-commit scan on restart.

**Important (observability / edge-case correctness):**

- **A6 (Scenario 1).** Add to RC-013: consumers must tolerate duplicate emissions; name the dedup key.
- **A7 (finding #4, Scenario 6).** Extend RC-019a to classify torn harmonik-local artifacts as Cat 6a via `divergence_inconclusive`, not Cat 3 generic.
- **A8 (Scenario 4).** Add RC-020b: detector panics are recovered; suspended for daemon lifetime; emit diagnostic event.
- **A9 (Scenario 7).** Add RC-012a: post-`ready` Cat 0 does not transition daemon state; emits health signal only.
- **A10 (Scenario 8).** Snapshot token TTL + `snapshot-token-expired` enum value.
- **A11 (Scenario 9).** Cat 3c detector must consult intent-log presence before firing; or unify idempotency keys.
- **A12 (Scenario 11).** Second-dispatch fallback in RC-002a MUST skip (not Cat 4 route). Add `reconciliation_dispatch_deduplicated` event. Amend Cat 6a detection to exclude actively-locked or recently-deduplicated branches.
- **A13 (Scenario 12).** Dedup-by-`target_run_id` on `operator_escalation_required` MUST reconstruct dedup state from JSONL scan on restart, not in-memory.
- **A14 (Scenario 12c).** Operator pause issued during `reconciling` that cannot complete due to Cat 6b MUST emit `operator_command_rejected` with a specific reason code, not silent queue.
- **A15 (Scenario 13).** Durable attempt counter for Cat 3b re-execution; N=5 default; emit `reconciliation_verdict_execution_retry` per attempt.
- **A16 (Scenario 14).** Reconciliation-workflow task branches carry a dispatch-marker commit with `Harmonik-Workflow-Class: reconciliation` + `Harmonik-Target-Run-ID` trailers. Carve out from RC-002's one-commit rule.
- **A17 (Scenario 15).** `reconciliation_verdict_malformed` payload carries `malformation_source` field distinguishing investigator-emission from schema/trailer drift.

**Nice-to-have (observability / audit):**

- **A18 (§3 audit).** RC-022 and RC-002a reword: "atomic" → "co-located in the same commit payload" where the claim is semantic, not crash-atomic.
- **A19 (§4 fsync audit).** Add a single RC requirement: RC-owned local-disk artifacts (locks, attempts, wip-capture) fsync + parent-directory-fsync + temp+rename per the EV-016 durability pattern.
- **A20 (§10 hidden assumption #8).** Clarify RC-010: filtering is by branch *naming convention* (`run/<run_id>` per WM-005) for pre-commit task branches, by trailer for committed runs. State both paths.
- **A21 (Scenario 10).** RC-020c: scheduled-cadence re-emission is suppressed when the snapshot is unchanged.

## 12. Cross-spec implications

- **event-model.** Verify each RC event row in §8.6 carries `F` class. Add `post_crash_window` flag consumption in RC-014 (cross-ref EV-023). Consider a post-MVH batched-fsync path for scheduled-cadence detector (A21 / file OQ).
- **process-lifecycle.** Either define the RC-002a lock primitive in §4.3 (if A1 resolves upward), or remove the `[process-lifecycle.md §4.3]` citation from RC-002a and point at RC's own schemas.md. PL also needs a tiny amendment per A8 to extend PL-018a coverage to RC detector panics.
- **beads-integration.** BI-030 should explicitly require temp+rename for intent-file writes (or state why `O_CREAT|O_APPEND` is torn-safe at the relevant fsync granularity). A3 requires a coordinated edit to BI to unify Cat 3a detection language with BI-031's actual protocol; or to reshape BI-031 to match the §8.4a claim — but BI's Beads-idempotency-independent version is the better design, so RC should conform.
- **workspace-model.** WM-005 branch-creation should be cited by A16 (task-branch dispatch-marker commit). Either WM creates the dispatch-marker commit on RC's behalf, or RC creates it directly.
- **execution-model.** If A16 creates a new kind of commit (dispatch-marker), EM's trailer registry needs a row for `Harmonik-Workflow-Class` and `Harmonik-Target-Run-ID`. Or RC self-hosts these trailers in schemas.md §6.4 alongside the verdict-executed trailer.
- **operator-nfr.** A14 requires ON-010 to plumb `operator_command_rejected{reason=daemon_not_ready,underlying_condition=cat_6b_blocking_reconciliation}` through §4.1 ON-002 `harmonik status` reporting.

## 13. Strongest single finding

**Finding #1 / Scenario 3. The RC-002a lock primitive does not exist at the cited location.**

RC-002a's entire serialization guarantee rests on a "workflow registry lock keyed on `target_run_id`... per [process-lifecycle.md §4.3]." PL §4.3 is "Ready-state transition" — it defines no such lock, names no such primitive, and never uses the word "lock." The PL primitives that are adjacent (PL-020a composition-root registries, PL-002a fd-lifetime pidfile lock) are either in-memory-only or scoped to a different concern (daemon singleton, not per-run reconciliation serialization).

Without the primitive, every multi-investigator-clash scenario (Scenarios 11, 3, 5's orphan-investigator race, Scenario 8's double-dispatch snapshot divergence, and Cat 6a's "two-plus task branches" detection) rests on guarantees that evaporate under crash. The RC-INV-001 uniqueness audit would *detect* the violation post-hoc but would not prevent it.

The fix is cheap: declare the lock primitive, place it on disk under `.harmonik/reconciliation-locks/`, use `flock` so the kernel releases on crash, stale-sweep on restart modeled on PL-006. All the adjacent specs (BI-030 intent files, PL-002 pidfile) already use durable file-based primitives for similar serialization. RC picked the only abstract-in-memory option and cited it to a section that doesn't carry it.

## 14. Reviewer note

The spec as a whole is the most crash-thoughtful of the foundation specs I've reviewed. The v0.3.0 integration lands real value: RC-002a's serialization attempt, RC-003a's priority order, RC-019a's corroboration rule, and the `no-op-accept` verdict together close most of the R1 critic's "what happens when..." gaps. The bounded-recursion argument is genuinely elegant.

The residual findings cluster around two themes:

1. **In-memory state that the spec treats as durable.** The RC-002a lock, the workflow_class filter for ghost branches, the `escalate-to-human` dedup key, the detector-panic suspension state. Each is stated as normative but lives in volatile memory. Under crash, each evaporates. The fix is consistently the same: migrate to disk with fsync discipline, model on PL's pidfile or BI's intent log.

2. **Multi-step operations labelled "atomic."** Emission + commit, lock-hold + commit, budget-emit + subprocess-kill + fallback-emit. The label is inherited from distributed-systems vocabulary where a single logical operation is atomic; under physical crash semantics, each step is independently recoverable. The fix is case-by-case: either collapse to one step (single envelope for two events), or make each step idempotent and add restart-replay.

Neither theme reopens a locked decision. Both are one-revision-cycle work. If the author merges the 5 blocking items (A1–A5), the spec advances from "elegant under normal operation" to "elegant under normal operation and verifiably crash-safe." The 12 important items are observability hardening that can ship with a v0.4.0 cadence.

One note for the author: the spec's multi-file split is clean (normative shell in spec.md, schemas in schemas.md) but several of the findings here propose additions that cross the split (A16's trailer lives in schemas.md §6.4; A19's fsync discipline is a requirement in spec.md). When you apply them, a brief verification that each amendment lives in the correct file and cross-references the other would prevent the kind of dual-table divergence §8.12 vs schemas.md §6.3 had to explicitly call out.
