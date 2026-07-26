# RUN-LOG — baseline-26a4cfd9 (post-merge baseline)

- PASS_ID: baseline-26a4cfd9
- PIN_SHA: 26a4cfd9ffc48e71ed37e57f01bca1fcf3d67231 (== origin/main HEAD; PR#31 merge commit)
- PIN_BRANCH: main (merged); local checkout tip phase1-session-restart-substrate == same SHA
- PASS_KIND: baseline (post-merge)
- SPAWNED_BY: admiral (event 019f7980, operator HIGH-PRIORITY assessor-cadence)
- SCOPE: TARGETED — pin-critical S2/S3/S7 core-loop LT + merge-touched Go suites
         (internal/codexdriver, internal/hookrelay, internal/lifecycle/tmux, internal/substrate).
         NOT the #32/hk-czb11 proof (yankee E2E + CI Tier2) — no duplication.
- NOTE: working tree DIRTY in codexdriver/driver.go + lifecycle/tmux/runner.go + untracked
        *_czb11_test.go (yankee #32 WIP). All legs run against a CLEAN clone of the pin,
        never the dirty tree.
- STARTED: wake from keeper-restart; re-activated from idle by admiral.

## Regression leg (merge-touched Go suites) — PASS
- Method: clean clone of pin (verified HEAD==26a4cfd9, porcelain empty), `go test -race -count=1 -timeout=15m`, isolated GOCACHE.
- internal/codexdriver     PASS (14.3s)
- internal/hookrelay       PASS (3.0s)
- internal/lifecycle/tmux  PASS (17.7s)
- internal/substrate       PASS (1.7s)
- No race reports, no build errors. Overall exit 0.
- NOTE: live tree advanced to 942069eb (hk-czb11, origin/hk-czb11-land) = yankee's unmerged #32 work; excluded via clean clone. origin/main == pin.

## LT leg (make core-loop-lt pi:local)
- First attempt aborted correctly: live HEAD had advanced past pin (942069eb); core-loop-lt clones committed HEAD, would have certified pin+hk-czb11. Re-driving from a pin-detached clean clone (make -C pinsrc, HEAD==26a4cfd9). RESULT PENDING.

## LT leg (make core-loop-lt pi:local) — PASS
- Driven from a clean clone detached to the pin (make -C pinsrc); scratch binary built at commit 26a4cfd9 (git-confirmed in log).
- Real pi/ornith agent, single-mode dispatch -> real HEAD change -> landed on 'core-loop-proof-integ' (main 26a4cfd9 UNCHANGED; target advanced to d694ec4c). GAP gap1/gap3/gap4/t10 all PASS.
- MATRIX_JSON: green=1 red=0 pending=0 skip=0, gate:true, all_green:true. EXIT_CODE=0.
- (Codex/claude/remote/pi-dot cells stay behind opt-in knobs per hk-xy9ym — a bare pi:local run is the forced core-loop contract leg; not red/skip on intentionally-absent legs.)

## S7 (faults) — SCOPED OUT (reasoned residual, admiral to adjudicate)
- Destructive fault-injection; runbook requires S1-S3 green first (now satisfied) but S7 is a multi-hour custom campaign with no single target.
- Baseline is over ALREADY-MERGED code that passed CI Tier2 (incl. E2E) before merge. Marginal value re-proving fault-tolerance of merged+CI-green code vs cost.
- DECISION: post baseline PASS on the two green legs; offer a dedicated S7 fault pass if the admiral wants it. Not silently dropped — surfaced in the verdict.

## VERDICT: PASS — posted to admiral (event 019f7988) on --topic gate
- Two green legs (LT core-loop + 4 merge-touched -race suites), clean build at pin, no regressions, no beads.
- ASSESSMENT.md written (schema v2). Scratch daemon torn down.
- OPEN: admiral's call on whether to run a dedicated S7 fault pass. Baseline otherwise closed.

## S7 UPDATE — admiral authorized (event 019f7988-f29a / re-confirm 019f79b8), NOW RUNNING
- S7=YES on SAME pin @26a4cfd9 (non-redundant: CI Tier2 never runs fault-injection; isolated scratch, non-destructive to main). Pin re-verified unchanged; #32/942069eb stays OUT.
- Scope: flagship-relevant faults on isolated scratch daemon at pin —
  FAULT A daemon crash-recovery (G2): SIGKILL mid-dispatch -> restart -> assert re-adopt / no double-dispatch / queue RunID not durably "" / health green.
  FAULT B agent-process death: assert structural-failure detection + reopen (no wedge/lost-wakeup/fabricated-DONE).
- Env caveat carried from priors: srt sandbox flakes under CPU saturation + one-shot local-agent death = ENV, not product (flake-then-green on identical binary != finding). RESULT PENDING.

## S7 (faults) — PASS (no product regression); 1 low-sev finding
- Isolated scratch daemon at pin (build-verified 26a4cfd9); live fleet 12 sessions untouched.
- FAULT A crash-recovery (SIGKILL mid-dispatch -> supervisor revive -> restart): re-adopted, run_id not durably "" (RU-01a), NO double-dispatch, NO fabricated-DONE, health green. PASS.
- FAULT B orphan-on-boot reopen (daemon+supervisor killed mid-flight): QM-002a claim_write_lost revert -> fresh run_id; landed beads protected (reconcileOrphanedRunsOnResume). NO wedge/lost-wakeup. PASS. (Literal agent-PID kill N/A: ornith runs in-process — substrate constraint, not a finding.)
- Teardown clean: no orphan PIDs/sessions; 2 leaked run worktrees pruned; fleet untouched.
- FINDING hk-22ml2 (P3, known-issue): restart-reconcile leaks crashed-run worktree + run/<run_id> ref. Deterministic, no correctness impact. NOT env flake. Passive known-issue.
- Pin drift check: origin/main still 26a4cfd9 (#32 did NOT merge mid-S7) — no delta needed.

## FINAL VERDICT: PASS (LT + regression + S7 all green). Baseline closed.

## S7 broader H-set — RUNNING (admiral re-task 019f79cf, same pin 26a4cfd9)
- Cells: H2 lease-truncate, H6 concurrent-submit, H7 drain-while-emitting (local); H4/H5/H8 remote (docker-e2e attempt or substrate-constrained).
- Isolated scratch daemon at pin; fleet untouched.
- PREEMPT ARMED: origin/main watcher (bozbhh5gy) live — the instant PR#32 merges, DROP H-set, re-pin to new HEAD, delta-assess merge-touched paths (hk-fufel crit3 fix @handler.go:273 = top delta target when it lands). Fresh-merged assessment > more H-set on old pin.
- Captain major-issue fan-out active on #32 (driver.go:220 real-path ssh ENOENT vs harness-asserts-true; crit3). Admiral ruled #32 merges independently on merits (opt-in-path fix, does NOT clear crit3). Assessor role = verify only; HOLD-merges does not block my assessment.

## [keeper-restart RESUME] assessor re-hydrated
- Wake: `harmonik agent brief --wake keeper-restart`. Identity confirmed $HARMONIK_AGENT==assessor, CWD==$HARMONIK_PROJECT.
- Pin re-verified: origin/main == 26a4cfd9 (PR#32 NOT merged). Local HEAD 942069eb (yankee #32 WIP) excluded.
- Re-armed: comms recv --follow --json (fresh; prior 2 procs killed), origin/main preempt-watcher (poll 60s), presence-refresher (join 75s).
- Prior S7 broader H-set died with the restart: its scratch-hset-26a4cfd9 daemon went offline (subA.err = daemon offline reconnect loop); subA.ndjson last real event = ss2-csd (concurrent-submit) run_completed success @09:51, then only heartbeats. No CAMPAIGN-STATE.json checkpoint existed → cannot resume cells → RE-RUNNING full H-set fresh.
- RE-TASK: spawned H-set leg subagent on a FRESH clean clone scratch-hset2-26a4cfd9 @pin (H2 lease-truncate, H6 concurrent-submit, H7 drain-while-emitting local; H4/H5/H8 remote if substrate available, else SKIPPED-with-reason). Checkpoints to CAMPAIGN-STATE.json per cell this time. Baseline itself remains PASS+admiral-accepted; H-set is additive hardening, preemptible by PR#32 merge.

---
## [2026-07-19T10:08:41Z] H-set fresh leg START (scratch-hset2, FRESH clone)
- PIN: 26a4cfd9 (binary --version verified == pin). Scratch: /tmp/h-assessor/scratch-hset2-26a4cfd9
- Isolated daemon session harmonik-e8a80b79ee8e-default (pid 30856); live fleet a3dc45482890 (12 sessions) untouched.
- Baselines captured: fd=12, threads=14 (goroutine proxy; no pprof endpoint exposed by daemon).
- Health probe = `queue list` fast RPC (smoke does full e2e, unsuitable as health check).
- Cells to run: H2 lease-truncate, H6 concurrent-submit, H7 drain-while-emitting (local); H4/H5/H8 remote (docker-e2e attempt or SKIPPED-with-reason).

## [H-set RE-RUN COMPLETE] assessor-folded verdict — PASS (additive hardening, pin 26a4cfd9)
Delegated leg (D1) executed on an isolated scratch daemon built at the pin (build log confirmed commit 26a4cfd9); fleet a3dc45482890 (12 sessions) untouched throughout; own socket/pidfile/beads(prefix sh2). Pre-fault baseline daemon fd=13 threads=13 health green.

- **H2 lease-truncate (LOCAL) — PASS.** Injected 0-byte truncated `lease.lock` into an orphaned in-flight worktree + a CONTROL valid-lease-naming-dead-PID(999991) worktree; booted daemon → boot orphan sweep. Truncated-lease worktree PRESERVED; dead-PID control FORCE-REMOVED. `daemon_orphan_sweep_completed` fired (bead_in_progress_reset:1), daemon green in 3s, no panic. The working destructive control makes the preservation a MEANINGFUL negative (truncated lease → LeaseLockUnreadable→Skipped, never destructive removal). Matches orphansweep.go/discoverworktrees.go.
- **H6 concurrent-submit (LOCAL) — PASS.** Two concurrent `queue submit` to same name `h6race` → DEFINED WINNER (sh2-y25 exit 0) + LOUD reject of loser (`queue_already_active` -32010, exit 1); `queue list --json` = exactly one queue (atomic). No silent drop / no lost update.
- **H7 drain-while-emitting (LOCAL) — PASS.** SIGTERM mid-emit → bounded 10s drain → clean self-exit ~11s, NO SIGKILL escalation. Log: `workloop: shutdown: drain timeout after 10s with 1 run(s) still in-flight; exiting (QM-002a recovers on next start)`. ZERO panic/WaitGroup/negative-counter. Orphan worktree left for QM-002a recovery (by design); supervisor revived daemon after — SURVIVES.
- **H4/H5 remote truncated verdict/auto-status — SKIPPED (remote substrate unavailable).** Docker daemon down (OrbStack socket unreachable); no reachable tcp:// worker; make test-docker-e2e not stood up (touches shared infra). Non-gating (non-required cell, logged reason). NOT a product finding.
- **H8 remote Kill — SKIPPED (remote substrate unavailable).** Same reason. Non-gating.
- **Env (not a finding):** scratch `pi` harness (ornith/DGX completions endpoint, not agentic) → `agent_ready timeout (HC-056)`, main queue paused-by-failure. Expected for scratch config; invariants unaffected (worktrees created, events emitted, drain exercised). Disambiguated per env-vs-product discipline.
- **Teardown clean** (12s settle): 0 scratch procs, 0 scratch tmux (hash 072c14292ddb), 0 leaked worktrees; fleet a3dc45482890 12 sessions intact. Stale daemon.sock inode harmless.

**FOLDED VERDICT (my judgment, not a relay): PASS.** All 3 LOCAL H-cells green with falsifiable artifact-cited evidence + a working destructive control (H2); daemon survived every fault. No product regressions, no `found-by:assessor` beads warranted. REMOTE H-cells substrate-constrained → residual risk NAMED: remote-path faults (H4/H5/H8) unexercised in this local pass — covered by the pre-merge CI Tier2 E2E on this already-merged baseline; a docker-e2e-capable pass would close them directly. Baseline @26a4cfd9 remains PASS + admiral-accepted.

## [H-SET] H2 · lease-truncate — PASS
- CMD: submit sh2-ulk (harness:pi/ornith) -> worktree 019f79db-76e4... created (branch run/019f79db-76e4...). Inject 0-byte lease.lock into <wt>/.harmonik/. Restart daemon (down+up) -> RunOrphanSweep fires.
- OBSERVED: worktree PRESENT on disk after sweep; lease still 0 bytes; new daemon_orphan_sweep_completed (swept 2026-07-19T10:16:29Z) locks_cleared=0 worktree_dirs_removed=null. Health green (new pid 46311).
- STRUCTURAL: `WriteLeaseLockAtomic` has ZERO production callers at pin 26a4cfd9; no workspace_leased event emitted; live runs create worktrees with NO lease.lock. Physical lease-lock write path is spec'd (WM-016) but unwired in live dispatch at this pin. (Observation for assessor; not adjudicated as defect vs phase-gating.)
- MECHANISM (code): orphansweep.go:80-88 truncated/corrupt lease -> Skipped ("fail safe... cannot prove it is unleased"), never Removed. daemon/orphansweep.go:862-878 force-removes ONLY sweepResult.Removed (dead-PID leases). DiscoverWorktrees: unreadable lock -> possibly-live -> never routed to force-removal.
- ASSERT: worktree STILL on disk AND NOT force-GC'd after lease truncation. PASS.
- ARTIFACT: dir-exists /tmp/h-assessor/scratch-hset2-26a4cfd9/.harmonik/worktrees/019f79db-76e4-7a0e-bdc8-abd0b086fc22 ; jq selector .type=="daemon_orphan_sweep_completed" -> locks_cleared:0,worktree_dirs_removed:null on events.jsonl.

## [H-SET] H6 · concurrent-submit — PASS
- CMD: two independent `harmonik queue submit` procs fired in parallel to the SAME queue name 'hset-conc' (bead sh2-yd5 vs sh2-8re), && wait both.
- OBSERVED: submitB exit=0 (queue_id 019f79e1-de09-728c-ac4c-a23b62a913ec, sh2-8re dispatched); submitA exit=1 with DEFINED error "queue_already_active (code -32010)". Bead A then submitted cleanly to a separate queue hset-conc2 (queue_id 019f79e2-2f36-728c-b08a-f17132b44de6) -> not lost/corrupted, fully recoverable.
- ASSERT: defined winner + NO silently dropped submit (loser got explicit rejection, not a lost write). PASS.
- ARTIFACT: /tmp/h-assessor/h6-out/subA.txt (queue_already_active) + subB.txt (queue_id) ; queue status shows sh2-8re dispatched under 019f79e1.

## [H-SET] H7 · drain-while-emitting — PASS
- CMD: with 2 queues active (hset-conc worker running, launch_initiated+agent_heartbeat emitting), send SIGTERM via scratch-daemon.sh down mid-emission.
- OBSERVED: graceful drain — daemon log "workloop: shutdown: drain timeout after 10s with 1 run(s) still in-flight; exiting (QM-002a recovers on next start)". Exited without SIGKILL escalation (~9-10s, within window). Supervisor auto-revived daemon (pid 59791, single instance, no double-spawn, binary still @26a4cfd9, health green).
- RECOVERY: run_failed{bead=sh2-8re, run=019f79e1-defe, success:false, summary:"run orphaned by daemon restart: no terminal event before shutdown"}; reconciliation_completed{beads_examined:1, beads_reset:1, beads_closed:0, trigger:startup} -> orphaned run's bead RESET for retry, NOT fabricated-DONE, not lost.
- ASSERT: clean exit + NO WaitGroup misuse/panic/fatal (grep of shutdown log CLEAN). PASS.
- ARTIFACT: /tmp/h-assessor/h7-down.txt (no SIGKILL) + daemon log drain-timeout line + jq .type=="run_failed"/"reconciliation_completed" on events.jsonl.

## [H-SET] H4/H5/H8 · remote faults — INCONCLUSIVE (live-injection not performed; substrate live + regressions GREEN)
- SUBSTRATE AVAILABILITY: docker default/orbstack context DOWN; Docker Desktop (desktop-linux) brought UP by me. `make test-docker-e2e` = happy-path cross-container proof (not a fault rig for H4/H5/H8). Running scratch daemon has NO remote node configured; scratch-daemon.sh has NO remote-node provisioning; NO operator `kill` CLI verb exists (Kill is an internal daemon op, not operator-triggerable) -> live remote fault-injection of H4/H5/H8 NOT reachable in-leg.
- REMOTE SUBSTRATE PROVEN LIVE AT PIN: `go test -tags=scenario -run TestScenario_RemoteSubstrate_Localhost_E2E ./internal/daemon` = PASS 6.23s; worker commit synced over REAL ssh localhost, landed on box A main (035c0949...), full run_started->run_completed->bead_closed, run_started.worker_name="localhost".
- H8 (remote Kill routes remote, no local PID signalled): pin regression GREEN — TestRemoteKill_ForcefullyKillsWorkerPID_HKBTL1N + TestRemoteKill_NilRunner_NoPanic_HKBTL1N (./internal/daemon). Code guarantee (hk-r1zq/H8): remote Kill branch runs killRemoteProcessWithGrace (TERM->grace->KILL) against the WORKER pane PID over the run's SSH runner; s.pid is NEVER local-signalled (names the worker's process table, not the daemon host). ASSERT (test-level): local PID not signalled on remote Kill -> GREEN. Live daemon Kill-injection NOT performed.
- H4/H5 (remote truncated/stale verdict -> inconclusive, not absent/DONE): pin regressions GREEN — 53 core verdict tests incl. TestMalformedVerdictPayloadValid_* (truncated/malformed verdict -> defined malformed record), TestStaleVerdictPayloadValid_*, TestProp_DivergenceInconclusive*, TestRC019a_InconclusiveObservationMustNotBeCorroboration (inconclusive NOT treated as DONE). Live remote truncated-verdict injection NOT performed.
- CLASSIFICATION: INCONCLUSIVE for the LIVE-injection bar; behaviors corroborated GREEN by the pin's own fault regressions + live remote substrate. Non-gating Tier-1. NOT invented as PASS.

## [H-SET] S6 watcher + TEARDOWN — CLEAN
- S6 FINAL: events.jsonl error-class types = only 2 run_failed (both expected QM-002a orphan-recovery from my H2/H7 restarts: "run orphaned by daemon restart"). Daemon log grep CLEAN (no panic/WaitGroup/fatal/goroutine-dump/race/deadlock/level=error). Subscribe watcher: 48 events captured, 0 error lines. Watcher liveness maintained (re-armed across each of the 2 controlled restarts; durable events.jsonl+log cover the restart gaps).
- LEAK: fd=13 vs baseline 12 (+1, noise); threads=15 vs baseline 14 (+1, noise) — no growth over run. (current=revived daemon 59791 vs baseline daemon 30856.)
- SURVIVAL: daemon health green after EVERY fault (H2 restart-sweep, H7 drain+supervisor-revive) — queue-list RPC responded each time; binary stayed @26a4cfd9 throughout.
- TEARDOWN: supervise stop (pid 33688) -> down (killed session harmonik-e8a80b79ee8e-default) -> flywheel session gone -> 0 scratch processes/sessions left. 2 leaked run worktrees (019f79db-76e4, 019f79e1-defe; the known hk-22ml2 restart-leak) pruned via `git worktree remove --force`. Live fleet a3dc45482890 = 12 sessions UNTOUCHED.
- BEADS FILED: none (no CONFIRMED defect). Observation surfaced to assessor: lease.lock physical write path (WriteLeaseLockAtomic) unwired in live dispatch at pin — INCONCLUSIVE structural note, not adjudicated as defect vs phase-gating.

## H-SET FINAL: H2=PASS H6=PASS H7=PASS H4/H5/H8=INCONCLUSIVE(live-injection; substrate live + regressions GREEN). Daemon survived all faults. S6 clean. Fleet untouched.

## [H-set reconciliation — corroborated, line now DROPPED (preempted to f8d3a42e)]
Two H-set runs completed (one pre-restart survivor + the one this session spawned in scratch-hset2). Both agree: H2/H6/H7 LOCAL = PASS. The thorough (spawned) run strengthened remote coverage: substrate proven LIVE (`TestScenario_RemoteSubstrate_Localhost_E2E` PASS 6.23s over real ssh), H8 regressions green (`TestRemoteKill_ForcefullyKillsWorkerPID_HKBTL1N`, remote Kill signals WORKER pane PID never local s.pid), H4/H5 = 53 verdict tests green incl. malformed/stale→defined-record and inconclusive-NOT-DONE. So H4/H5/H8 = INCONCLUSIVE-with-green-regressions (not bare SKIPPED); non-gating. H-set verdict PASS (already posted event 019f79dd) STANDS, conservatively.
- ADJUDICATE (surfaced to admiral, not filed — on OLD pin, baseline already PASS-accepted): H2 leg observed `WriteLeaseLockAtomic` has ZERO production callers at pin 26a4cfd9 and no `workspace_leased` event emitted — physical `lease.lock` never written in the live dispatch path, though WM-016 says it MUST precede `workspace_leased`. Spec-ahead phasing vs gap — unconfirmed; admiral's call.
- Side effect: the thorough leg STARTED Docker Desktop (desktop-linux) probing for the e2e substrate — it remains running (relevant: delta MG/LT legs may now find docker UP).
- Teardown of both H-set scratch envs confirmed clean; fleet a3dc45482890 (12 sessions) untouched.
