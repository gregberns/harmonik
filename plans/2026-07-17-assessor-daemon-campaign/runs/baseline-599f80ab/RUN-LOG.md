# Baseline Campaign RUN-LOG — PASS_ID=baseline-599f80ab

- **PIN_SHA:** 599f80ab7f8fc7ef5169db8e37210b04aeb5ccb3
- **PIN_BRANCH:** phase1-session-restart-substrate
- **PASS_KIND:** baseline (AUTHORITATIVE — produces the admiral-gating ASSESSMENT.md)
- **SANDBOX:** /tmp/h-assessor/scratch-baseline-599f80ab  (tmux -L h-assessor, TMPDIR=/tmp/h-assessor)
- **STARTED:** 2026-07-19T02:50:35Z
- **Spawned-by:** admiral (HOLD-lift 02:48:03Z; keeper-restarted ~02:48:29Z → fresh start)
- **Mandate:** full S1–S7 + G1–G14, critic-until-dry, §4B teardown; fold hk-xrn8r rerun (real-vs-env, clean box).

## [2026-07-19T02:50:35Z] STARTUP · scaffolding
CMD: pin+rundir+env setup
OBSERVED: HEAD@src == PIN_SHA (599f80ab); /tmp/h-assessor clean (no prior h-assessor procs); shared /private/tmp/h distinct namespace.
ASSERT: pin matches, namespace isolated → PASS

## [2026-07-19T02:52:51Z] §4A PRE-FLIGHT + SANDBOX UP
CMD: preflight-4A.sh /tmp/h-assessor/scratch-baseline-599f80ab ; scratch-daemon up
OBSERVED: preflight fails=0 (all isolation PASS; benign WARN 6-worker-bin local-only). binary --version == 599f80ab (anti-stale-PASS §0.D). daemon RUNNING pid 21101, project dir == $SBX (dry-boot §4A.9 bind ✓), socket under SBX, boot log ⑤ bind line present, no error/panic.
ASSERT: §4A ALL PASS + binary-SHA==PIN + daemon reports SBX → SANDBOX HARD GATE **PASS**
ARTIFACT: /tmp/h-assessor/real-env-snapshot-baseline-599f80ab.txt ; scratch-daemon.log

## [2026-07-19T02:56:48Z] hk-xrn8r RERUN (quiet box, isolated ×3, default TMPDIR)
CMD: go test ./internal/daemon -run TestScenario_Hk6ynv4_SubscribeStream_EndToEnd -count=3
OBSERVED: 2 PASS / 1 FAIL — got [reviewer_launched run_started] want [run_started reviewer_launched]. INTERMITTENT on a QUIET, ISOLATED box → a REAL event-order race, NOT pure env-load. Test file unchanged in delta; subscribe-ordering is daemon (keeper/codexdriver delta cannot touch it).
ASSERT: real-vs-env = REAL-intermittent (not env-only). Product-vs-test + pre-existing-vs-regression → root-cause subagent. Disposition pending.
ARTIFACT: this block.

## [2026-07-19T02:56:48Z] PIN GUIDANCE (admiral 02:55:25Z)
HEAD 599f80ab->9152ea5c is TEST-ONLY (4 keeper scenario_*_test.go, 795 ins, ZERO product source, admiral diff-verified). PIN STAYS 599f80ab; baseline authoritative for 9152ea5c. Release SHA = 9152ea5c = 599f80ab + test-only.

## [2026-07-19T03:00:15Z] hk-xrn8r DISPOSITION — bucket (3) TEST-DEFECT, NON-GATING
Root-cause (evidence): daemon does NOT guarantee inter-event order on the live subscribe/observer stream — observer consumers dispatched per-emit via fresh goroutines (internal/eventbus/busimpl.go:650, documented busimpl.go:74-78). run_started & reviewer_launched emits race → [reviewer_launched run_started] is a LEGAL outcome. Only emit-ordered surface = durable JSONL (appended under lock BEFORE dispatch); real causality tool asserts over that file (internal/daemon/scenariotest/causality.go:44-47), NOT the socket. Test asserts strict order on the WRONG surface (subscribe_scenario_hk6ynv4_test.go:175-178).
Flake rates: 599f80ab 5/10 FAIL, a0591ba3 7/10 FAIL — same signature, PRE-EXISTING (worse at older baseline) → NOT delta-introduced. Delta doesn't touch bus/subscribe.
DISPOSITION: TEST-DEFECT-nongating, P2. Product delivers correctly; test over-asserts. Maps to admiral gating bucket (3). Fix: assert event SET on live stream + route ordering via causality.go. NON-GATING for the 599f80ab baseline. Needs a named test-fix owner (proposed to admiral).
ASSERT: hk-xrn8r does NOT block baseline → resolved NON-GATING.

## [2026-07-19T03:03:38Z] LT LEG (make core-loop-lt) — GREEN
CMD: make core-loop-lt LT_SCRATCH=/tmp/h-assessor/lt-scratch (runtime 9152ea5c = pin+test-only)
OBSERVED: MATRIX_JSON {"summary":{"green":1,"red":0,"pending":0,"skip":0},"gate":true,"all_green":true,"cells":[{"cell":"pi:local","verdict":"green"}]}. gap1 pass (pi tier1 ornith), gap3 pass (real HEAD change commit_landed), gap4 pass (workflow_mode=single), t10 pass (landed core-loop-proof-integ, git-verified main unchanged, target 5160326b->31f15609). Mid-run BATCH_ITEM ls-p7p failed structural (claude_exit_without_outcome) → daemon AUTO-REOPENED and the cell RESOLVED GREEN — dispatch/workloop.go (delta) recovery correct.
ASSERT: core dispatch loop end-to-end GREEN on a real pi agent, non-zero-on-any-red gate returned rc=0 → **LT PASS**. Exercises the 66-line workloop.go dispatch delta → no dispatch regression.
COVERAGE CAVEAT (Tier-1 non-gating): matrix ran single-mode pi:local only, NOT the full DOT graph. Rationale: (a) the delta a0591ba3..599f80ab does NOT touch the workflow-graph/DOT engine (keeper+codexdriver+claude-launch only); (b) the prior authoritative baseline covered DOT-attempt; (c) local ornith (weak same-model reviewer) cannot converge a multi-turn DOT round-trip — needs a stronger reviewer model (operator codex-minimal). DOT-graph covered by invariance; flagged, not silently dropped.
ARTIFACT: LT-core-loop.log

## [2026-07-19T03:05:25Z] CR LEG (cold review a0591ba3..599f80ab) — CLEAN
OBSERVED: verdict CLEAN. No P0/P1, no runtime-reachable core-loop/dispatch/secret-leak defect. Findings: F1 P2 (codexdriver ResidentSession.Close sets closed=true before queue drain → buffered submissions get ErrResidentClosed, defeats documented graceful FIFO drain; bounded, no hang/leak) — **LATENT**; F2 P3 (leaderDeferTextForHandoff dead code); F3 P3 (claude:local redundant global-trust write + no-op theme seed under CONFIG_DIR isolation — efficiency). Positives confirmed: dispatch wiring intact (workloop delta=field re-align + codex isolation guard wired end-to-end), no slot/lease leak, no lost-wakeup, codexdriver queue bounded+drains no-goroutine-leak, ResidentSession revival race-free, scrub no-leak, keeper T8/gauge-drop gating correct (suppress only CF!=nil && belowActThreshold else fail-defensive re-inject), partial config honored.
ASSESSOR VERIFICATION (claim reconciliation): CR's load-bearing "NewResidentSession has no production caller" INDEPENDENTLY CONFIRMED — grep: only resident.go (def) + resident_test.go; the internal/daemon "resident" hits are unrelated comments. → the entire codexdriver resident-session surface is SCAFFOLDING, inert in production at 599f80ab. F1 is genuinely LATENT/non-gating.
ASSERT: CR CLEAN; sole P2 latent (no production reach); production delta reduces to keeper + workloop field-realign + codex-isolation-guard + claude-launch-isolation + scrub — all confirmed correct. → CR PASS.
ARTIFACT: CR-FINDINGS.md

## [2026-07-19T03:13:46Z] XT LEG (adversarial break-testing) — XT-CONCERNS (no P0)
SURVIVED (executed, -race clean): codexdriver FIFO flood/close-race (cap=4 exact, ErrQueueFull, no loss/dup/panic); watchdog always-fail (backoff, Close<3s, no goroutine leak); codexwire malformed/string-id handshake (typed errors, no panic); claude CONFIG_DIR isolation (writes target isolated destCfg, real ~/.claude read-only).
FINDINGS: (4) P2 keeper gauge-drop gating claimed DELTA-ATTRIBUTABLE — ANALYTIC/UNVERIFIED, conflicts CR+my pre-restart verify+TestZJ1Y → UNDER ASSESSOR VERIFICATION. (7) P2 scrub value-concat leak — PRE-EXISTING at a0591ba3 (git show verified by XT), NOT delta. (5) P3 T8 residual TOCTOU AwaitModelDone→Clearing edge.
ASSERT: codexdriver/codexwire/isolation adversarially SOUND. Finding 4 must be resolved by executed verification before verdict (potential gating bucket-1). 
ARTIFACT: XT-FINDINGS.md

## [2026-07-19T03:15:59Z] FINDING 4 ADVERSARIAL VERIFICATION — REFUTED (not a defect; NOT gating bucket-1)
XT's analytic hypothesis: gauge-drop suppression drops a NEEDED defensive re-inject in the genuine busy-pane case.
ASSESSOR EXECUTED VERIFICATION (overrules the analytic synthesis):
- Predicate (step.go:431): gaugeDropped := ev.CF != nil && belowActThreshold(ev.CF); re-inject SUPPRESSED only when gauge READABLE AND below-act; KEPT on nil-CF (fail-defensive) OR at/above-act.
- Admiral's exact re-scoped case (gauge HIGH / nil-CF / clear-didn't-land): re-inject FIRES in all three → NEEDED re-inject PRESERVED. Matches my pre-restart verify (admiral-accepted PASS) + CR's independent read.
- The suppressed case (SAME session_id + below-act gauge + uncleared) is the DELIBERATE, TESTED exactly-one-clear behavior: TestZJ1Y_SelfServiceRestart_GaugeDrop_ExactlyOneClear (actionable_warn_self_service_zj1y_integration_test.go:314-323) asserts "want still exactly 1 /clear after the gauge dropped" (hk-1ryc no-double-restart; was 20, now 1). Finding-4's "fix" would REVERT this and reintroduce the 20-/clear storm.
- Spurious-low-gauge-while-uncleared sub-case: covered by (a) nil-CF fail-defensive, (b) the separate .precompact backstop RunForPrecompact (cycle.go:839) for the compaction-threshold overflow path, (c) fresh CF read per event + next-cycle resample.
VERDICT: Finding 4 REFUTED — intended+tested behavior, admiral's busy-pane case correctly preserved. At most a Tier-2 theoretical stale-gauge note, NOT delta-introduced regression. NON-gating. No stop-and-ping-BLOCK.

## [2026-07-19T03:15:59Z] FINDINGS 5 & 7 DISPOSITION
- Finding 5 (P3, T8 6zbg1 delta): in-cycle operator-attach re-check covers pollAwaitingHandoff (shell.go:323) but NOT the stepAwaitModelDone→stepEnterClearing edge (step.go:358-361) → residual narrow operator-attach clobber window. BOUNDED (clobbers one operator interaction, recoverable; not wedge/corruption). ANALYTIC. Disposition: Tier-1/P3 known-issue, non-gating, file found-by:assessor + name T8 owner to confirm+close the residual edge.
- Finding 7 (P2 scrub value-concat): greedy sk/pk/ghp value-regex leaks 2nd concatenated secret's tail. XT verified PRE-EXISTING at base a0591ba3 (git show) — NOT delta-introduced; the delta's hk-a5f1k key-classification change is SOUND. Disposition: secret-leak-class but PRE-EXISTING → non-gating for THIS 599f80ab baseline (admiral bucket-2 logic); file found-by:assessor P2 known-issue with named fix-owner (real leak, must not be lost).

## [2026-07-19T03:24:44Z] RESUME (keeper-restart) — final leg + verdict
Re-armed comms (join+drain, no new gating directives — pin guidance + gating map already held). $HARMONIK_AGENT==assessor, CWD==repo root. S6 watcher STILL LIVE (pid 24675, heartbeats current, daemon UP pid 21101, 0 hits). Scratch HEAD==pin. Skipped-as-complete: §4A/LT/CR/XT (all PASS/green in CAMPAIGN-STATE). REGRESSION-TREE.md was UNWRITTEN at cut → the pre-restart regression subagent (task ab54ac8e) completed post-restart and wrote it; my redundant full-tree re-run was STOPPED (would only add load noise).

## [2026-07-19T03:24:44Z] REGRESSION GREEN-TREE — GREEN-with-known-issues
OBSERVED: 5 target pkgs green outright (codexwire/codexdriver/workspace/sessioncapture/core); keeper green (1 ms-timer Heartbeat flake, 0/20 isolated); daemon whole-pkg hit 10-min load timeout, per-test isolation reliable. 6 reds ALL load/timing flakes, invariants intact. go build ./... CLEAN.
ASSESSOR VERIFICATION (the one deterministic red = potential gating bucket-1): TestThroughput_TenBeadsAtMaxFour 16-vs-10 run_started = legit re-dispatches under CPU starvation — every envelope DISTINCT run_id, envelope.run_id==payload.run_id (no double-emit). git diff a0591ba3..599f80ab -- internal/daemon/workloop.go read COLD by assessor: delta = (a) new codexRequireIsolationBoundary field + whole-literal alignment reformat, (b) fail-closed guard gated on deps.codexRequireIsolationBoundary (set iff HARMONIK_SUBSTRATE=codexdriver, unset in test → guard inert), placed BEFORE worktree setup, NEVER touches retry/emission path. => NOT a regression. Bucket (2)/(3): test encodes spare-CPU assumption.
ASSERT: no product regression, no gating bucket-1 → regression leg GREEN-with-known-issues. NON-gating.
ARTIFACT: REGRESSION-TREE.md, regression-tree.log

## [2026-07-19T03:24:44Z] CRITIC — DRY; VERDICT = PASS
Completeness critic dry: the sole load-bearing unverified claim (Throughput=regression?) executed+refuted; remaining gaps (DOT single-mode, codex SKIP, remote path, live context-fill) are PRE-EXISTING logged coverage caveats, not new modalities. All 5 legs in, all claimed-done reconciled, no P0, no gating bucket-1. ASSESSMENT.md (schema v2) written → VERDICT PASS.

## [2026-07-19T03:27:29Z] §4B TEARDOWN — CLEAN
1. S6 watcher (pid 24675) killed → gone. 2. scratch-daemon down (daemon pid 21101 stopped); leaked supervisor shim + daemon procs pkill'd → 0 scratch-baseline procs remain. 3. rm -rf /tmp/h-assessor + /private/tmp/h-assessor → gone. 4. git worktree prune removed ONE dangling registration (keeperreg-1, gitdir → deleted /tmp/h-assessor; pre-existing assessor-namespace residue) → real worktree list == 11 live FLEET worktrees, all UNTOUCHED (main + 10 agent-*). 5. No real $HOME/.harmonik/keeper (no RU-24 traversal leak). 6. find real repo -newermt start == empty outside rundir (nothing written outside sandbox). 
ASSERT: teardown CLEAN — sandbox fully reclaimed, fleet untouched.

## [2026-07-19T03:27:29Z] VERDICT POSTED + TERMINATING
Baseline PASS posted to admiral --topic gate (event 019f7868). ASSESSMENT.md (schema v2) is the deliverable. Assessor self-terminates: one verdict, not a standing loop. Admiral owns the epic->main PR + release call.
