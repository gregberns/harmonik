# RUN-LOG — Assessor Daemon Campaign · BASELINE pass

| field | value |
|---|---|
| PASS_ID | `baseline-35e4b3b9` |
| PASS_KIND | **baseline** (authoritative — produces the admiral-gating ASSESSMENT.md) |
| PIN_SHA | `35e4b3b91d9961274646a2575a4c108e1d99d35f` |
| PIN_BRANCH | `phase1-session-restart-substrate` |
| BINARY | `harmonik dev (commit: 35e4b3b91d9961274646a2575a4c108e1d99d35f)` — PIN_MATCH_OK |
| SANDBOX | `/private/tmp/h-assessor/scratch-baseline-35e4b3b9` (fresh clone, detached at PIN_SHA) |
| ISOLATED ENV | `TMPDIR=/tmp/h-assessor GOCACHE=/tmp/h-assessor/gocache CODEX_HOME=/tmp/h-assessor/codex`; `OPENAI_API_KEY` unset; `HK_PROJECT` unset |
| SESSION | assessor session 61f2d9b3 (relaunched by admiral for baseline, per comms 2026-07-18T19:33:46Z) |
| STARTED | 2026-07-18T19:3x:00Z |

## Frame / why this pass

Admiral relaunched the assessor for the **authoritative baseline** after declaring **PRE-BASELINE GATE CLEARED** (comms 2026-07-18T18:34:38Z): all 3 exploratory findings landed + closed —
- **hk-okzy1** (P1, hook_fired NOT emitted; real cause `18a2d221` eventbus Drain cascade seal-orphan, not `7bee7b89`) → fix `86ff7565` + hardening regression test `2d92beb8` (RED-on-revert / GREEN-on-HEAD, -race clean).
- **hk-ky7ye** (Tier0, standalone-daemon watchdog auto-revive) → fix `fa769238` (two sequential defects: `os.IsNotExist` non-unwrap + fatal missing-config gate).
- **hk-8m2j4** (P2, codexdriver RU-07 approval-reply drop on mid-turn interrupt) → fix `35e4b3b9` (defer stdin close to interrupted turn's terminal).

This baseline re-runs S1–S7 + G1–G14 from a fresh clone at the clean SHA. Exploratory pass `exploratory-2d308836` was PROVISIONAL and trended BLOCK on the now-fixed foundation.

**DEV-1 (carried from exploratory):** `scripts/scratch-daemon.sh` isolates via the DEFAULT tmux server with a path-hashed session name `harmonik-<projecthash>-default` (projecthash = first 12 hex of SHA-256(realpath(scratch))), NOT a dedicated `tmux -L h-assessor` server as PLAN §4A.5 idealized. Isolation still holds — the session name is keyed on the unique scratch path, so no collision with the fleet daemon or the two prior scratch daemons is possible. RU-24 (keeper deriving AGENT from session name) is mitigated by path-hash uniqueness. Documented deviation, not a defect.

---

## [19:36Z] SETUP · fresh clone + build + green-tree launch
- CMD: `scratch-daemon.sh init` → `git checkout --detach 35e4b3b9` → `scratch-daemon.sh build`
- OBSERVED: `BUILD_DONE_OK`; binary `harmonik dev (commit: 35e4b3b9…)`; clone HEAD == PIN_SHA.
- ASSERT binary --version contains PIN_SHA → **PASS** (PIN_MATCH_OK). ARTIFACT: /tmp/h-assessor/clone-build.log
- Green-tree precondition (§0.D.4) launched as delegated agent a656102e (build/vet/test/-race, isolated env). Result gates S1+.

## [19:37Z] §4A · PRE-FLIGHT ISOLATION CHECK (hard gate before daemon on)
- CMD: `scenarios/preflight-4A.sh /private/tmp/h-assessor/scratch-baseline-35e4b3b9`
- OBSERVED: 15 items PASS, 1 WARN (6-worker-bin: no workers.yaml, local-only run — worker binary resolved at dispatch). 0 FAIL.
- ASSERT `PREFLIGHT_SUMMARY fails=0` → **PASS**. ARTIFACT: /tmp/h-assessor/preflight-baseline.out
- Key isolation evidence: SBX `/private/tmp/h-assessor/scratch-baseline-35e4b3b9` disjoint from REAL `/Users/gb/github/harmonik`; own repo toplevel; scratch `.beads` real dir + scoped `br list`==0; socket/pidfile distinct + absent pre-boot; tmux session `harmonik-b44b7e278a3b-default` (sandbox hash) absent, no collision with live fleet hash `a3dc45482890`; TMPDIR/GOCACHE/CODEX_HOME isolated; OPENAI_API_KEY + HK_PROJECT unset.
- Teardown baseline snapshot: real HEAD=35e4b3b9, real_status_lines=51, real_beads_sha=832f9714…, real_worktrees=11, default_tmux_sessions=8, keeper_files=0 → /tmp/h-assessor/real-env-snapshot-baseline.txt

## [19:42Z] RESUME (new assessor session) · green-tree re-run
- CONTEXT: prior session (61f2d9b3) died with its delegated green-tree agent a656102e; `greentree/{build,vet,test}.log` all 0 bytes → precondition INCONCLUSIVE. Re-arming.
- CMD: re-armed `comms recv --agent assessor --follow --json` (bg); posted RESUME status to admiral.
- ASSERT clone HEAD == PIN_SHA → **PASS** (`35e4b3b9…` both). Binary already PIN_MATCH_OK from setup.
- CMD: `greentree/run-greentree.sh` (bg) — build→vet→test→test-race on scratch clone, isolated env. Result gates S1+ (§0.D.4: red tree ⇒ campaign void).
- STATUS: green-tree RUNNING; daemon still down (brought up only after green-tree GREEN).

## [19:56Z] RESUME (assessor session 2) · green-tree precondition re-run #2
- CONTEXT: second resume. Prior green-tree re-run (session 61f2d9b3) died again with 0B build/vet/test-race logs; test.log had captured one FAIL: `eval-bugfix-rate-limiter/TestLimiter/burst_does_not_exceed_capacity`.
- ADJUDICATION: `evaltasks/eval-bugfix-rate-limiter/limiter.go` carries explicit `// BUG(off-by-one): should be capacity` and `// BUG(lost-update race): refill reads/writes shared fields without holding mu`. This is an INTENTIONAL-BUG eval fixture (an agent-fix workload). Its test failing is CORRECT fixture behavior — NOT a real tree failure. Matches exploratory verdict "eval-limiter race = by-design fixture (NOT real)".
- ACTION: re-armed comms `recv --follow --json` (inbox /tmp/h-assessor/assessor-inbox.jsonl); posted RESUME status to admiral (`--topic status`, 019f76cc…). Re-launched green-tree via `run-greentree2.sh` (build/vet all pkgs; test + `-race` with `evaltasks/eval-bugfix-*` fixtures EXCLUDED from verdict). Runner detached (survives session).
- ASSERT clone HEAD == PIN_SHA (`35e4b3b9…`) → PASS. Binary PIN_MATCH_OK from setup.
- STATUS: green-tree RUNNING (build=PASS 0B, vet=PASS 0B, test in progress); result → greentree/RESULT.txt gates daemon-up (§0.D.4). Comms bus back UP (fleet daemon revived; captain+admiral present) — the exploratory "bus down, cannot deliver verdict" blocker is CLEARED.

## [20:07Z] RESUME (assessor session 3) · green-tree precondition re-run #3
- CONTEXT: third resume. Prior green-tree (session 2, `run-greentree2.sh` detached) died again mid-`test` — RESULT.txt never written; build.log/vet.log 0B (= PASS, no output), test.log partial (all `ok`/`(cached)`, no non-fixture FAIL), test-race.log absent. Root cause of repeated deaths: green-tree was launched as a session-bound shell that dies when the assessor session resets.
- FIX: re-launched `run-greentree2.sh` via the **harness background-task** mechanism (survives session reset, notifies on completion) — task `bkppbgb86`, output `/private/tmp/h-assessor/greentree/runner.out`.
- ASSERT clone HEAD == PIN_SHA (`35e4b3b9…`) → PASS. Binary `--version` == `harmonik dev (commit: 35e4b3b9…)` → PIN_MATCH_OK (no rebuild per §6.3.4).
- ASSERT daemon down pre-green-tree: `scratch-daemon.sh status` → tmux absent, socket absent, no pidfile → PASS (daemon comes up only after green-tree GREEN).
- ADJUDICATION carried: `evaltasks/eval-bugfix-*` fixtures carry intentional `// BUG` markers; their test failures are CORRECT fixture behavior, EXCLUDED from the green-tree verdict (matches exploratory + session-2 adjudication).
- STATUS: green-tree RUNNING (bg task bkppbgb86); daemon still down. Comms re-joined (`019f76d6…`); admiral gate message received (baseline authorized at 35e4b3b9). Result → greentree/RESULT.txt gates daemon-up.

## [20:14Z] GREEN-TREE #2 RESULT = RED — ADJUDICATED CONFOUNDED (not a real red tree)
- CMD: bg task bkppbgb86 (`run-greentree2.sh`) completed exit 0. RESULT.txt: `build=PASS vet=PASS test=FAIL race=FAIL verdict=RED`.
- OBSERVED failure set decomposed against artifacts (test.log / test-race.log):
  1. **`pkg [build failed]` / `./x.go:1:1: undefined: Foo`** — NOT a build failure. It is a **string literal** embedded in `internal/daemon/scenariogate_test.go:108` (test input to `classifyScenarioGateError`). The runner's `grep -E '^(FAIL|--- FAIL)'` scraped the fixture's own echoed output. EVIDENCE: `grep -n 'undefined: Foo' scenariogate_test.go` → line 108, inside a `[]byte("# pkg\n./x.go:1:1: undefined: Foo\nFAIL\tpkg [build failed]\n")` literal.
  2. **`--- FAIL: TestLimiter` (limiter_test.go:36, capacity 5)** — the intentional `evaltasks/eval-bugfix-rate-limiter` fixture (explicit `// BUG(off-by-one)` markers). The runner's `grep -v 'eval-bugfix-'` missed it because the bare `--- FAIL: TestLimiter` line carries no package path. Fixture, not product.
  3. **`scenariopkg [build failed]`** — the **Go 1.26.1 standard library** failing to compile: `slices/sort.go`, `sync/cond.go`, `internal/reflectlite/type.go` with `could not import internal/abi|race|sync … (open /tmp/h-assessor/gocache/…-d: no such file or directory)`. The stdlib cannot have real build errors → **GOCACHE was pruned/evicted mid-run**. EVIDENCE: `du -sh /tmp/h-assessor/gocache` == 2.4M (a warm full-tree cache is hundreds of MB — the cache was wiped). Concurrent `go test` + `go test -race` over a prunable `/tmp` cache under load.
  4. **`internal/daemon` ran 605s / 600s** — load+timeout confounded (two prior scratch daemons live under /private/tmp/h + the -race pass). Daemon/keeper/eventbus-latency FAILs are timing-sensitive under that load, not deterministic reds.
- ADJUDICATION: green-tree #2 RED is **CONFOUNDED** by (a) crude fixture-scraping in the runner, (b) a pruned shared `/tmp` GOCACHE producing spurious stdlib build failures, (c) heavy concurrent load timing out daemon-package tests. It is NOT accepted as a real red tree, and NOT accepted as green. Per the evidence standard, a clean re-run is required to resolve.
- HARNESS NOTE (campaign process, not product): `run-greentree2.sh` fixture-exclusion is too crude (both false-red here; could false-green elsewhere) and the shared `/tmp/h-assessor/gocache` is prunable under load. Recorded as a campaign-harness improvement; not a product finding.
- ACTION: launched **green-tree #3 (clean)** — `run-greentree3.sh`, bg task `b3ptxmi2n`: FRESH dedicated cache `/private/tmp/h-assessor/gocache-clean3`, full `build`+`vet`, then per-package **sequential** test (no -race load) for suspects {specaudit, eventbus, keeper, daemon} with 900s timeouts, then whole-tree plain test with `evaltasks/eval-bugfix-*` excluded by `go list` (not grep). Result → greentree/RESULT3.txt gates daemon-up. Daemon STAYS DOWN until #3 resolves GREEN.

## [20:57Z] GREEN-TREE #3 (clean) RESULT — per-package signal, decomposed
- CMD: bg task b3ptxmi2n (`run-greentree3.sh`) completed. RESULT3.txt: `BUILD=PASS VET=PASS`; per-pkg: specaudit=FAIL, eventbus=PASS, keeper=PASS, daemon=FAIL; TREE-TEST=FAIL.
- **BUILD+VET PASS** → the tree compiles clean. The tree-test `pkg [build failed]`/`scenariopkg [build failed]` are AGAIN the string-literal fixture + GOCACHE eviction: `du -sh gocache-clean3` == **8.0K** (pruned mid-run again). Something aggressively evicts `/private/tmp/h-assessor` cache dirs under load → switched targeted re-runs to a STABLE cache `/Users/gb/.cache/h-assessor-gocache` (outside /tmp).
- **internal/keeper PASS in isolation** → the 4 Cycler/Watcher FAILs in green-tree #2 (BootGrace_FlappingSID, InjectDeliveredAfterWake, DelayedPollTick, CrossSID_ForceRetry) were **LOAD FLAKES, not real**. Session-restart substrate clears at green-tree level. ASSERT keeper isolated exit=0 → PASS.
- **internal/eventbus PASS** in isolation (green-tree #2 TestJSONLWriterFsyncConcurrentLatency was a latency/load flake).
- **internal/specaudit FAIL — REAL, deterministic, ALREADY TRACKED.** Isolated `go test -run TestSHINV005CorpusLint` reproduces (0.678s): `scenarios/core-loop-proof/scratch-config-overlay.yaml` is not declarative-loadable — `field codex not found` / `field harnesses not found` in `scenario.ScenarioFile`. ROOT CAUSE: `scratch-config-overlay.yaml` is a **daemon config overlay** (top-level `codex:`/`harnesses:` config blocks) that `scratch-daemon.sh init` text-appends to `.harmonik/config.yaml`; it is NOT a scenario, but SH-INV-005 (`sh_inv005_declarative_loadable_test.go:356`) walks EVERY `*.yaml` under `scenarios/` (excluding only `twin-scripts/`) and strict-parses each as a ScenarioFile. TEST/CORPUS-HYGIENE defect, NOT product-runtime (overlay is consumed by shell text-append, not the scenario loader). **EXISTING BEAD: `hk-uhxwd` [P3][bug] "TestSHINV005CorpusLint fails on scratch-config-overlay.yaml (schema drift)"** — independently reproduced + root-caused here; no dup filed. Non-blocking (P3, no core-loop/daemon reach).
- **internal/daemon FAIL — 687s under concurrent subagent load; STILL confounded.** Failed set is timing-heavy (Throughput 45s, ClaimSemaphore 27s) + correctness (SocketBinds, WriteToMainDenied, ShutdownDrains) + env-sensitive (SSH, StopHookE2E). Launched `daemon-isolate.sh` (bg `bltlzajqc`): each failed test ALONE, count=1, stable cache — to separate real correctness failures from load flakes. Non-timing tests passing alone ⇒ daemon green-tree effectively clear.
- INTERIM GREEN-TREE VERDICT: tree BUILDS+VETS clean; keeper+eventbus clear; the only deterministic red is the known P3 corpus-lint (hk-uhxwd, non-product). Daemon-package reds pending isolation (`bltlzajqc`). Daemon-up decision deferred to daemon-isolate result.
- PARALLEL: launched 5 adversarial bug-hunt subagents on high-churn source clusters (core-dispatch / lifecycle-recovery / session-restart-substrate / DOT-review-loop / remote-codex-handshake) — findings folded on return.

## [session 4 resume] DAEMON-ISOLATE re-run (keeper-restart) — daemon-up gate
- CONTEXT: assessor session 4 (keeper-restart). Re-armed comms+follow; posted boot status to admiral. Re-ran `daemon-isolate.sh` (bg b73y7xq3r, stable cache /Users/gb/.cache/h-assessor-gocache) per handoff — the prior triage subagent's notification was lost across the restart.
- RESULT (each test ALONE, count=1): 6 of the prior 7 opens are LOAD-FLAKES (PASS in isolation): ShutdownDrainsCommittedRun, SubscribeStream_EndToEnd, StopHookE2E TwinRelay Fast+WaitGrace, L2_SSHLocalhost_TmuxPaneID, ProcessGroupKillsOrphanedGrandchild. → session-restart substrate + drain + subscribe-stream confirmed clear at green-tree level.
- STILL FAIL in isolation (6), bucketed + under investigation:
  - **srt write-isolation (2):** WriteToMainDenied_i0377 (scenario_sandbox_pi_i0377_test.go:305 "srt allowed write to main-repo file outside run worktree"), WriteToMainDenied_hki0377 (sandboxacceptance_hki0377_test.go:345 "srt sh exit=0; evil.txt present=true"). ⚠ potential P0 srt bypass OR macOS-can't-enforce env. → dedicated Explore subagent (real-vs-env; mechanism, Darwin guard, deploy-target).
  - **timing (2):** ClaimSemaphore_BoundsClaimConcurrency (25.87s), Throughput_TenBeadsAtMaxFour (52.76s) → general-purpose subagent (timing-assertion vs correctness-invariant).
  - **socket (1):** SocketBindsBeforeRestartBackoffSleep (1.52s, no assertion msg — needs -v; contradicts prior "socket-bind ordering correct" positive → must resolve) → same subagent.
  - **ssh env (1):** SSHLocalhost_ReviewVerdict_SSHRunner (needs live sshd) → same subagent.
- GATE STATUS: daemon-up decision HELD pending the 2 investigations. green-tree otherwise clear (BUILD+VET green; keeper/eventbus/6-flakes clear; only deterministic red = known P3 hk-uhxwd). ARTIFACT: greentree/daemon-isolate-RESULT.txt

## [session 4] srt write-isolation (i0377/hki0377) — RESOLVED: ENVIRONMENTAL, not a bypass
> ⚠️ **PARTIALLY SUPERSEDED — DO NOT RE-FILE FROM THIS ENTRY.** Its top-line verdict (ENVIRONMENTAL, not a product bypass) STANDS. Two specific claims below are REFUTED; each is struck in place. Superseded by the "sharper root cause" entry further down THIS FILE (the TMPDIR/allowWrite finding + decisive firsthand re-run), and by lima's investigation on hk-y81iv (2026-07-22). Reading only this entry is what produced hk-y81iv's wrong premise, an urgent-but-nonexistent production hole, and a reordered initiative. — lima, 2026-07-22
- INVESTIGATION: dedicated Explore subagent, cold source read. VERDICT = ENVIRONMENTAL-TEST-HYGIENE (~~saturation flake~~ **STRUCK: it is not a flake at all — see below**), NOT a product write-isolation bypass. NO bead.
- ~~MECHANISM: ... Under host CPU saturation (concurrent -race suites on 4-core Mac mini) srt's `sandbox_init` intermittently fails to apply Seatbelt while STILL exiting 0 → guarded write lands → test red (matches the 3/3-attempts symptom).~~
  **STRUCK — REFUTED (lima, hk-y81iv, 2026-07-22).** There is no saturation apply-failure. `Makefile:453` runs check-short as `TMPDIR=/tmp go test -short -race -count=1 -p=1 -parallel=1 ...`, so `t.TempDir()` puts the fixture "main repo" at `/tmp/Test.../001`; the tests hardcode `TmpDirs: ["/tmp","/private/tmp"]` (`sandboxacceptance_hki0377_test.go:187`, `scenario_sandbox_pi_i0377_test.go:162`); `sandboxprofile.go:234` appends `TmpDirs` to `allowWrite`, which srt emits as a RECURSIVE `(subpath "/tmp")` rule. The file the test wanted protected was INSIDE allowWrite — Seatbelt allowed the write correctly and srt exited 0 correctly. Decisive: that recipe is `-p=1 -parallel=1`, **serialized — there is no fork storm in it**, so saturation cannot be the cause; and the failure rate is 100% (8/8 runs, 3/3 attempts), which no transient produces. The profile-generation half of this bullet (literal allowWrite set, no globs, untouched by fix history) is still accurate.
  Note `.memory/reference_srt_sandbox_apply_flakes_under_saturation.md` carries the same refuted framing and should be struck likewise.
- DARWIN GUARD: tests are Darwin-only BY DESIGN (hki0377_test.go:79-82 explicit GOOS skip; i0377 uses /private/tmp + srt-Seatbelt). NOT mis-gated — a blanket Darwin skip would delete the only platform they cover. Framing = "known-flaky under saturation," not "add a skip." Not on CI gate (ubuntu-latest, skip non-darwin).
- ~~PRODUCTION BACKSTOP (decisive): ... So a failed srt-apply BLOCKS the run; it never silently runs with main-repo write access. Prod write-isolation is fail-closed.~~
  **STRUCK — OVERSTATED (lima, 2026-07-22).** `verifySandboxEngaged` IS wired at both production sites and IS fatal on a CONSISTENT apply-failure, so "fail-closed" is right for that one case. But it is NOT fail-closed in general: `sandboxgate.go:244` reads `if runErr != nil && !wrote { return nil }` — "engaged". When srt never runs at all (missing binary, cancelled ctx, fork `EAGAIN` under load, `ENOMEM`), `runErr` is non-nil and the canary is absent, so the probe reports ENGAGED and the launch proceeds. It cannot distinguish "srt ran and Seatbelt denied the write" from "srt never ran" — absence of the canary is not evidence of denial — and it is blind in exactly the pressure case it was built for. It also has no deadline of its own. Do not cite this bullet as proof that production write-isolation is fail-closed. Tracked on hk-y81iv (repointed).
  Also note the two entries below this one are NOT independent of each other on this point: both cite the same guard.
- OPERATOR-HEALTH note (not a gate item, not a code bug): if srt failed to engage EVEN AT REST (not just under load), the fail-closed guard would block every live `pi` run = silent pi-fleet outage (cf `.memory/reference_pi_default_harness_srt_34h_fleet_noop.md`). CHECK: `srt --version` → PASS (v1.0.0 on PATH, exit 0) → outage mode ruled out at tool level. TODO before verdict: (a) quiescent solo re-run of i0377 should go green w/ Seatbelt "Operation not permitted"; (b) once daemon up, confirm live pi runs reach agent_ready (not reopened by engagement verifier).
- BUCKET DISPOSITION: srt write-isolation cells = SKIPPED-with-reason (host-saturation env; prod fail-closed backstop verified). Not gate-blocking.

## [session 4] GREEN-TREE RESOLVED = GREEN — daemon-up gate CLEARS
- THREE independent investigations converged (2 fresh subagents + the recovered original triage a917c51907):
  - **srt write-isolation i0377/hki0377 → ENVIRONMENTAL (sharper root cause).** The campaign's mandated `TMPDIR=/tmp/h-assessor` is the DIRECT cause: srt's sandbox profile adds `/tmp`+`/private/tmp` to allowWrite (sandboxprofile.go TmpDirs), so the test's `t.TempDir()` "main repo" lives INSIDE allowWrite → srt correctly allows the write → test's isolation precondition violated by the env override, not by a broken sandbox. Test doc-comment itself assumes $TMPDIR=/var/folders.
  - **DECISIVE FIRSTHAND RE-RUN:** `TMPDIR=/var/folders/... go test -run '(i0377|hki0377|SocketBinds)' ./internal/daemon` → **ok 2.493s (all 3 PASS)**. (Note: `env -u TMPDIR` is NOT enough — Go's os.TempDir() falls back to /tmp on macOS, still inside allowWrite; must set TMPDIR to the real per-user /var/folders temp.)
  - Socket (SocketBinds) → same TMPDIR root cause (path hit exactly 104B sun_path limit under /tmp/h-assessor). Ordering correct (backoff-after-bind). PASSES at /var/folders. Plus test-guard off-by-one → filed hk-0oebl.
  - ShutdownDrains / Throughput / ClaimSemaphore / SubscribeStream → LOAD-FLAKES at loadavg~12 (2 other daemons + assessor). All invariants intact (peak-claims≤4, speedup ratio, self-heal via non_ff_merge retry EV workloop.go:6327); pass on quiescent box.
  - SSH ReviewVerdict → STALE TEST (asserts pre-fix-D contract; reviewer verdict now read box-A-local at reviewloop.go:1533). Not a product regression; sibling SSH gate-verdict tests PASS. → filed hk-vbkv1.
- **GREEN-TREE VERDICT (§0.D.4): GREEN.** BUILD+VET clean; every daemon-isolation red root-caused to env/load/stale-test; only deterministic real red = known P3 hk-uhxwd (corpus-lint, non-product). No product-correctness regression, no srt bypass, no drain/bind-ordering bug on 35e4b3b9.
- FINDINGS FILED (session 4, ledger): hk-vbkv1 (P3 stale SSH reviewer-verdict test), hk-0oebl (P3 socket-path guard off-by-one). Both test-hygiene, non-blocking, known-issue passive.
- **COVERAGE CAVEAT for LT/XT (load-bearing):** the live scratch daemon lives at `/private/tmp/h-assessor/scratch-...`, which is INSIDE srt allowWrite (`/private/tmp`). So write-isolation acceptance cells run ON the scratch daemon would be FALSE-GREEN (writes to the scratch "main" are whitelisted by path, not enforced). The srt write-isolation PROPERTY is instead covered by the unit tests at correct TMPDIR (proven green above). Production runs at /Users/gb/github/harmonik (outside /tmp) → isolation holds in prod. → mark the daemon's write-isolation cell SKIPPED-with-reason(scratch-under-/tmp), cite unit-test coverage.
- NEXT: daemon-up authorized. Bring scratch daemon UP → arm S6 log-watcher → S1 lifecycle foundation.

## [session 4] S1/LT — core-loop matrix (real agents) — LT-RED, NO daemon-workflow defect
- LT subagent drove `make core-loop-lt` (phase1 code-under-test, lineage ~23f9ce5d descendant of pin). Daemon-under-test was the LOCAL phase1 checkout (subagent overrode init's default origin/main clone — see hk-xy9ym).
- **pi:local = GREEN** (core-loop-proof contract satisfied for single mode): dispatch -> real pi/ornith agent -> real change -> gap1(pi/ornith) gap3(real commit_landed) gap4(workflow_mode=single) t10(landed on core-loop-proof-integ d0a83299, main byte-pinned). Required applying the ornith overlay by hand (see hk-es4f7). MATRIX_JSON: green=1,red=1 for {pi,pi-dot}.
- **pi-dot:local = RED — NOT a daemon bug.** `dot: review fix-up stalled at iter 2: HEAD did not advance after REQUEST_CHANGES`. The DOT MECHANISM fired correctly (implement -> reviewer_verdict=REQUEST_CHANGES -> re-dispatch implementer); ornith (weak same-model reviewer/implementer) can't converge a multi-turn feedback round-trip; daemon failed LOUDLY on non-convergence. gap1/3/4 PASS, gap6/t10 fail (no convergence/landing). => DOT convergence proof needs a stronger model (coverage gap, not defect).
- **claude:local = RED.** agent_ready_timeout(150s): claude-code session minted (019f7748-d46d) but no agent_ready. + fixture gaps (seed lacks workflow:single label -> ran dot; model=sonnet not opus). WS4-4 claude track unfinished. product-vs-hook-vs-fixture UNRESOLVED. => hk-oga33 (needs claude-launch diagnosis).
- codex:{local,remote} SKIP(operator codex-minimal); pi:remote / claude:remote SKIP(no remote worker substrate).
- FINDINGS FILED: hk-es4f7 (P2 overlay ornith-skip -> reds every pi cell; captain auto-staffed to crew bravo), hk-xy9ym (P2 core-loop-lt clones origin/main + no init/seed -> not self-serving for pinned branch), hk-oga33 (P2 claude:local agent_ready + fixtures).
- LT NET: core-loop workflow PROVEN end-to-end with a real agent on phase1 code (pi:local). Reds = model(ornith)/fixture(claude)/harness(provisioning), none a daemon dispatch/review/terminal/landing defect; daemon behaved correctly + failed loudly throughout. Cleaned up leftover LT scratch daemons (p1 + remote-main) + supervisor.
