# Admiral — Major-Initiatives Registry

> **STANDING RULE (operator-mandated 2026-07-05) — PRE-DEPLOY E2E TEST GATE.** No daemon deploy ships without new end-to-end tests, added that deploy, that reproduce the changed behavior on a real launch path IN ISOLATION from the live daemon (never test on the primary daemon; green units are not the gate). Enforce every deploy. Canonical: orchestrator-rules §"PRE-DEPLOY END-TO-END TEST GATE" + `docs/daemon-redeploy.md` GATE 0 + admiral mission Hard bounds. Ties to the `codename:daemon-testbed` epic (hk-zk0v2) — that harness is what makes this gate cheap.

> **Admiral-owned.** The complete set of major initiatives + their status. This is the
> admiral's oversight anchor: every audit reconciles it against ground truth (captain-lanes.md
> tier-2 + `kerf next` + comms). It complements captain-lanes.md (which tracks *which crew is on
> which lane right now*); THIS file tracks *all the big rocks + which are active / on-deck / parked*.
>
> **Maintenance:** the admiral updates this each audit when an initiative's status changes (a new
> one appears in comms/operator asks, an active one lands, an on-deck one is staffed). Re-read on
> every admiral restart. Keep it SHORT — one line per initiative + status; detail lives in the
> bead/epic/plan it points at.
>
> **Status vocabulary:** ACTIVE (a crew/queue is working it now) · ON-DECK (next to staff, no
> blocker) · PARKED (**zero ready beads right now — a FACT, not a hold**; a parked KNOWN lane is
> self-authorizable to resume the instant ready work appears) · GATED (**held by a NAMED, DATED,
> OWNED, EXPIRING gate** — mirrors `lanes.json.gate`; absence of a live named gate means
> KNOWN/resumable, see orchestrator-rules §Autonomy) · DONE (landed; kept briefly for context).
>
> Last reconciled: 2026-06-25 ~06:24Z (admiral). PARTIAL UPDATE 2026-07-05 ~15:2xZ: added the
> MODEL-ROUTING / PROVIDER-SPREAD PROGRAM (operator, token-crunch). The 06-25 ACTIVE/ON-DECK rows
> below are 10 days STALE — full reconcile owed on the next hourly audit; treat MR1–MR3 + the
> quality-process kerf pilot as the current front line.

## ★ SINGLE FOCUS — QUALITY-SYSTEM (operator 2026-07-06): build the whole test/validation system

> Vet the daemon after changes BEFORE it replaces the live binary. Full synthesis + 4 scoping passes:
> `plans/2026-07-06-quality-system/` (00-SYNTHESIS is the map). Consolidate 4 overlapping efforts into
> ONE kerf work `codename:quality-system` (tranched, NOT a mega-epic). Build spine = daemon-testbed-design
> (Docker substrate → digital-twin agent seam → chaos generator). `remote-test-pyramid` (10/10) already
> built the runner-seam foundation — close it out. Process mandates: branch-per-epic gate (test+review+
> break-test at boundary), GATE-0 pre-deploy e2e, 24h rule (current binary; new build deploys only after
> passing the test system), testing-crew builds+tests in own worktree+integration branch → then main.

| Chunk (integration-branch epic) | Order | Proves |
|---|---|---|
| **core-loop-proof** | 1st, blocks all | real bead→queue→harness(correct model)→sandbox provider-comms→changes→DOT review-back, ×{claude,codex,pi}×{local,remote}. Closes bug-gaps 1/2/3/5. |
| **scripted-twin** ‖ **scratch-substrate** | after 1, parallel | Layer-1 deterministic twin + first scenario ‖ Layer-0 Docker substrate + disk dial |
| **adversarial-corpus** | after 2+3 | failure-corpus regression tests + Lane-1 XT break-testing |
| **chaos-generator** | last | Layer-2 LLM generator, judged by the real daemon |

**Org (APPROVED operator 2026-07-06):** NEW `assessor` agent-manifest = gate executor (spawned
per epic, admiral-directed, separate from captain-the-builder); admiral = gate authority, captain =
builder unchanged. Detail: `plans/2026-07-06-quality-system/04-testing-org-model.md`. Manifest AUTHORED +
independently reviewed → APPROVE (`.harmonik/agents/assessor/`); NOT yet wired to a launcher.

**PHASE PROGRESS (admiral drives; captain builds one phase at a time):**
- **Phase 1 — core-loop-proof + gate bootstrap: KERF TASKED, BUILD STARTING.** Captain ran full kerf cycle
  (problem-space→analysis→tasks) scoped to core-loop-proof ONLY → **epic `hk-hcrvb` + 9 tranched tasks**
  (T1 skeleton → T2 assert-lib → gap tasks T4-8 → T9 gate), deps wired, committed+pushed `ef24bcaf`.
  `integration/core-loop-proof` branch up. Acceptance = top-5 gaps in `02-bug-corpus`.
  **BUILD MODEL DECISION (admiral, option C, 2026-07-06):** the 'own worktree + integration branch → then
  main' rule has NO daemon support (daemon merges daemon-wide to main; per-bead LandsOn/landTaskBranch path
  is DEAD CODE). Chose C: alia orchestrator commits harness code directly to `integration/core-loop-proof`
  in its own worktree, uses the daemon ONLY to EXECUTE the matrix (the SUT), never to merge harness — avoids
  the self-fix bootstrap trap. Option B ELEVATED by operator 2026-07-06 to CRITICAL-going-forward:
  per-bead/DOT integration-branch targeting (the dead LandsOn/landTaskBranch path) filed **hk-lgykq (P1,
  daemon-reliability, known-issue+found-by:admiral)** — the FIRST assigned-known-issue (proceed on the C
  workaround, but on a funded fix track, not parked). Directed: (a) core-loop-proof harness ADDS an assertion
  that a bead directed to integration branch X lands on X (REDs today = known-issue evidence); (b) captain
  staffs a crew to fix hk-lgykq after T1 skeleton lands (daemon-core; must pass the gate it enables). See the
  assigned-known-issue lifecycle in `07-assessor-severity-framework.md`. alia staffing now on C.
  **T2 assert-lib landing green = the trigger to hand schmidhuber the Phase-2 Layer-1 twin chunk.**
- **Phase 2 — deterministic testbed: PLANNED + independently reviewed + HARDENED.** Build-plan =
  `plans/2026-07-06-quality-system/06-phase2-plan.md` (scripted-twin ‖ scratch-substrate → twin-replay +
  corpus port). Adversarial review → NEEDS-FIXES; 6 fixes applied: (a) C3 scenario re-scoped to the LOCAL
  same-file merge-race — hk-5qp7z/hk-lt091 are REMOTE worktree-create races, wrong target for a local twin;
  (b) fixtures live at repo-root `scenarios/<group>/` (NORMATIVE), NOT `internal/scenario/`; (c) named the
  twin-injection seam (`agent_overrides` SH-008–011); (d) dropped miscited hk-nlhys from C5; (e) Docker
  substrate is Linux — validates the disk-dial mechanism, not darwin-fidelity; (f) entry precondition made
  testable (chunk-1's assertion package path + green CI). Twin/scenario seam ALREADY EXISTS in-tree; only
  chunk-3's Dockerfile is greenfield. Cannot BUILD until Phase-1 core-loop-proof merges (hard dep).
- **assessor gate mechanics — DESIGNED + wire-up PLANNED.** Severity framework =
  `07-assessor-severity-framework.md` (MAJOR→BLOCK all actions vs MINOR→open `found-by:assessor` known-issue
  bead; per-action merge|release|redeploy matrix; ledger in the deploy-readiness report). Wire-up plan =
  `08-assessor-wireup-plan.md`: BASIC SPAWN NEEDS ZERO CODE (`harmonik crew start assessor` resolves the type
  folder). Two must-do-before-first-gate gaps, both pure spec/config: (A) author `specs/assessor-handoff-schema.md`
  (branch/gate/commit/found_by_sources frontmatter — crew schema doesn't fit); (B) branch-scope the block query
  (beads have no branch field → today's `found-by:*` query is fleet-wide; file + query with `--label <epic_id>`).
- **assessor → REMEDIATION LOOP — DESIGNED (operator deliverable 2026-07-06).** `09-remediation-loop-design.md`:
  how assessor findings (blocking + assigned known-issues like hk-lgykq) get ROUTED → TRACKED → DRIVEN to
  closed as a SYSTEM, not admiral hand-routing each gate. Core: (1) the finding bead IS the fix unit (no
  shadow bead); (2) a new `remediation:blocking`/`remediation:assigned` label pair encodes disposition at
  file-time → `kerf next` ranks it into the captain's normal backlog (admiral does ZERO routing); (3) closure
  gate = the fix must flip its known-RED harness cell green + add a repo-root `scenarios/` regression (ratchet,
  not oscillate); (4) 2-cycle escalation bound so remediation can't silently wedge a held epic. Wires in at
  Phase-1's first epic boundary alongside `07`/`08`. `hk-lgykq` = the first end-to-end acceptance trace.
- **⭐ OVERNIGHT PUSH LANE STRUCTURE (operator 2026-07-07, max-parallelism 9h window).** Program map =
  `10-program-map.md`. Answer to 'is one crew all we can parallelize' = NO. FIVE concurrent lanes now:
  (1) **alia** → core-loop-proof (dispatch, critical path) — T1-T4 landed+GREEN; runs gap tasks T5-T8/T10
  via INTERNAL sub-agents (NOT fanned to crews — they all edit core-loop-assert.jq = same-file race,
  captain's correct catch). (2) **hawat** → hk-lgykq (dead integration-branch-targeting fix; dogfoods
  against alia's T10 known-RED cell). (3) **schmidhuber** → Phase-2 twin (epic/scripted-twin, ACTIVE —
  fired by T2-green; Layer-1 scripted twin + local same-file merge-race scenario). (4) **captain crew** →
  comms-test (`12-comms-test-design.md`, epic codename:comms-test-harness — ZERO substrate dep, highest
  parallel candidate; T4 = surface B1/B2 semantics to operator, don't self-fix). (5) **captain crew** →
  keeper-test (`11-keeper-test-design.md`, epic codename:keeper-test-harden — T1 fixes hk-pp1in + watch
  re-stall). Token reality: Claude cap ~98% → orchestrators SONNET, build workers CODEX. **gb-mbp remote
  RE-ENABLE authorized** (admiral, 2026-07-07): blocker hk-5qp7z proven-fixed → cycle to +6 (=10 daemon
  slots) at a clean lull — the biggest throughput lever. Staged: Lane 4 (lightweight subsystems: DOT
  verdict-parse, promote/reconcile, br-adapter) + Lane 5 (substrate-bound rows) per the program map.
  BUGS.md (repo root) = live defect-capture inbox (8 entries). shannon = idle spare (paste-wedge, B8).
- **Phase 3 — adversarial-corpus + chaos-generator: admiral to plan while captain builds Phase 2.**
- Keeper defect filed: `hk-pp1in` (restart-now aborts no_tmux_target despite healthy pane-bound watcher).

## MODEL-ROUTING / PROVIDER-SPREAD PROGRAM (operator 2026-07-05 — NEW, token-crunch-driven)
> NOTE 2026-07-06: MR1-3 are now EXERCISE epics for the quality-system above — build them, then validate
> the binary through the test harness before deploy (operator's workstream 3).

> Context: Anthropic tokens running LOW. Immediate stopgap already directed to captain (flip daemon
> worker path off Claude → Pi/DeepSeek). These three are the DURABLE program that makes token-spread a
> first-class capability instead of a manual switch. #3 IS shannon's routing research (verifier-cascade,
> `plans/2026-07-05-model-selection/`) elevated to BUILD. Ranked by token leverage; captain scopes + kerfs.

| # | Initiative (plain English) | Depends on / connects to | Status | Notes |
|---|---|---|---|---|
| **MR1** | **Crew can run on Codex or Pi (not just Claude)** — a crew orchestrator session can be backed by a non-Claude harness; cuts orchestration-tier Claude burn, not just the worker tier | builds on ON-DECK Codex-on-remote + Codex-vetting; crew-launch harness selection | **ON-DECK → captain to scope bead/kerf** | Operator 2026-07-05. Independent of MR2/MR3; directly cuts the standing crew/orchestrator Claude burn. |
| **MR2** | **Multiple models/providers in Pi concurrently** — Pi is daemon-global ONE model today (deepseek). Enable using the local DGX (ornith) AND OpenRouter models AT THE SAME TIME so both substrates stay full | = the GATED "Pi universal-model-gateway" below, EXPANDED to concurrent-multi-provider. Gate ("not before remote proven") is now effectively MET (gb-mbp concurrent PROVEN) → recommend LIFT | **GATE-LIFT CANDIDATE → captain to scope** | Operator 2026-07-05. Prereq for good MR3 routing (need multiple live providers to route between). |
| **MR3** | **Daemon dispatch-time model selection + configurable default skew** — the daemon picks the right model per task; default skew is operator-tunable. Default: fill Pi+local-DGX FIRST, then spread across Claude+Codex; when Claude tokens run low, CUT Claude off automatically | = shannon's verifier-cascade routing (`plans/2026-07-05-model-selection/`, thread 5) elevated to BUILD; needs MR2 (multi-provider) to route across; the objective merge-gate is the quality backstop | **BUILD (from research) → captain to kerf** | Operator 2026-07-05. Highest token leverage — this is the durable form of today's manual "cut Claude" switch. The "cut when low" auto-throttle is the acute win. |

## TOP / ACTIVE (being worked now)

| Initiative (plain English) | Codename / key beads | Status | Notes |
|---|---|---|---|
| **Remote test-hardening program** — make the remote substrate thoroughly + cheaply testable WITHOUT real remote servers. Full test pyramid L0–L5 + impasse-plan moves ①②③ (skip ④). **#1 PRIORITY** (operator 2026-06-25, runs autonomously overnight) | kerf work `remote-test-pyramid` · epic **hk-6l941** · L0 **hk-hd2w6** (ready) → L1 **hk-52xnr** (blocked-by L0) on **gurney-q** LOCAL · plan: `designs/remote-test-strategy-plan.md` | **ACTIVE — dispatching** | Fleet REPOINTED LOCAL (gb-mbp disabled, daemon restarted, concurrency=4). FILED 06:23Z; **gurney re-tasked + adopting L0**. Seam-map found 3 more bare verdict reads (`dot_gate.go:551/686` + `autostatusmarker.go:70`) = unfixed remainder of the hk-f3u6o class → folded into L0. Order: L0 runner-seam contract + L1 separated-FS twin FIRST → L2 isolated-ssh → L4 fault/chaos → L5 scheduler-props (move ② per-queue routing) → **L3 LAST = DOCKER containers (NOT Lima VMs, operator 06:10Z) + Linux-remote support** (OrbStack daemon currently DOWN — start before L3; Linux-target + concurrent/multi-remote scheduler are real design gaps → captain to scope an L3 research kerf-work). Captain owns kerf planning+dispatch (local queue); admiral oversees + hourly unstick-watchdog. Operating rules: small bug fixes done DIRECT out-of-daemon, sub-agents OK w/ review, review=multi-agent consensus NOT human signoff. |
| **Wake-economy / token-optimization** — cut the captain's own wake + token burn; token-opt is the **#1 standing priority** (2026-06-23 operator directive, RE-CONFIRMED 2026-06-25; captain's own burn ranks highest) | `codename:wake-economy` · hk-var9b / hk-8yh32 / WE10 / hk-we-soak1/2 | **ACTIVE — but UNSTAFFED (at-risk)** | MVP all 7 WE beads CLOSED, watch-tier cutover LIVE. Soak/polish follow-on (hk-we-soak1/2, hk-we10, hk-8yh32) is ready + NOT staffed — paul (the crew) is DOWN. Directed captain 3× to re-staff (01:11/01:55/02:05). Escalate to operator if still idle next audit. |
| **Remote-worker validation** — distribute bead-work to the gb-mbp macOS SSH box; prove one full triple-reviewed run lands over the tunnel | hk-h106 (hostname proof) · hk-4lrj (triple-review capstone) · hk-f3u6o (reviewer consistency) · hk-t1t00 (durable HK_GATE_BASE_SHA) | **ACTIVE — last-mile** | priority #2. Routing/launch/implement/commit_gate ALL PROVEN green on gb-mbp. Gate-loop root cause was STALE worker origin/main (NOT cold cache) inflating the affected-set → fixed. ONLY remaining: remote-reviewer verdict consistency (run 3 hit reviewer agent_ready_timeout). Crew **gurney** (epic hk-gx0dl, queue gurney-remote) owns the last-mile: ROOT-CAUSED + reproduced the reviewer bug (daemon reads verdict from its LOCAL path; reviewer wrote it on gb-mbp). **hk-f3u6o LANDED on main 5999a39a (2026-06-25 ~05:30Z, OUT-OF-DAEMON per operator)** — isolated implementer mirrored gurney's diagnosis (runner-routed verdict + budget-sentinel reads, nil→local fallback) + fixed 3 sibling read sites (builtin review-loop + APPROVE merge-trailer); built, full daemon suite green, independently reviewed+APPROVED, fast-forward push. Bead OPEN (no daemon auto-close) → captain to reconcile/close on resume. **hk-t1t00 = HOLD/needs-rework** (premise OK in shell-script terms but a separate fix; deferred pending operator strategy decision). FLEET HELD pending operator remote-test-strategy decision. |

## ON-DECK (next to staff, no blocker)

| Initiative (plain English) | Codename / key beads | Status | Notes |
|---|---|---|---|
| **Codex-on-remote** — run the Codex implementer harness on gb-mbp (not just Claude); verify e2e launch + ChatGPT billing over the tunnel | (no bead yet — captain creates) | **ON-DECK** | Operator-requested this session. Sequenced AFTER the Claude remote ladder lands. Prereq: confirm ChatGPT/codex auth exists on gb-mbp. Watch reviewer-pin footgun hk-2jxqg. Lives only in comms until the bead exists. |
| **Codex-vetting (local)** — dedicated crew, DELIBERATELY serialized one-bead-at-a-time, quality-assess how Codex performs on THIS repo | epic hk-0639 (soak) · fed by hk-fgy9o (test-uplift) · local re-canary hk-n05u2 first | **ON-DECK** | Operator-requested this session. Lives only in comms until staffed. |
| **Daemon-reliability** — kill the false-positives/strands that corrupt the orchestration signal | epic hk-sfvc · hk-7xgu4 (iter-2 implementer thrash, burns 60-90min slots + blocks queue) · hk-u5tgh (restart leaves crews keeper-less) | **ON-DECK** | Lane 4. file-disjoint: internal/daemon. Highest fleet leverage. |
| **Watchdog session rename** — rename the mislabeled "flywheel" tmux session (it's the daemon-revive watchdog) to `-supervisor` | (captain scoping bead) | **ON-DECK** | Directed 2026-06-25. Load-bearing contract string (reaper matcher + spawn-exclusion move in lockstep). |
| **Standing test-daemon harness** — a separate worktree/clone running an ISOLATED test daemon pinned to the remote: run a test against it, submit issues to the main daemon. = MOVE ① (scratch-clone), NOT the skipped move ④ (two daemons on the SAME repo dir). The fast-loop unblock that makes remote bugs cheap to iterate | move ① in `designs/remote-iteration-impasse-plan.md` + `remote-test-strategy-plan.md` · captain scoping kerf-work + bead set | **ELEVATED → BUILD** (operator 2026-06-25 ~21:29Z, via admiral; directive comms 019f00af) | Operator clarified the long-misread "two daemons" idea = THIS (isolated scratch-clone test daemon), promote from scope-only side-quest to BUILT standing harness in the remote lane; accelerates the last-mile, does not compete. **DURABLE + REUSABLE — operator wants the framework KEPT and reused broadly (e.g. running BATCHES of tests through an isolated test daemon), NOT a throwaway scaffold.** Re-runnable on demand: scratch worktree/clone, own socket+tmux namespace → run test(s) → feed issues to main daemon. Staff gurney or a dedicated scratch-clone crew (restart authority on the SCRATCH daemon only). |

## GATED / WAIT (held by a NAMED, DATED, OWNED, EXPIRING gate — mirrors `lanes.json.gate`)

> "PARKED" (zero ready beads now) is a FACT and is NOT listed here — a parked KNOWN lane is
> self-authorizable to resume. ONLY a lane held by a named/dated/owned/expiring gate belongs in
> this section. On gate expiry the DEFAULT is LAPSE → revert to autonomous; the admiral audit
> owns re-confirming or striking an expired gate.

| Initiative (plain English) | Codename / plan | Status | Gate (owner · reason · expires) |
|---|---|---|---|
| **Pi universal-model-gateway** — universal model/provider gateway harness | `plans/2026-06-23-pi-openrouter-harness/` | **GATED** | operator · not before remote-worker proven reliable · expires 2026-07-09 |
| **De-hardcode-messages** — remove hardcoded message strings | — | **PARKED** | Bundled with Pi; same gate. |
| **Hot-reconfigure + concurrent/multi-worker dispatch** — change daemon remote/local config WITHOUT restart, run local + remote AT THE SAME TIME, and (phase 2) multiple remotes | routing LANDED: hk-f10xl (`Queue.LocalOnly`/`WorkerTarget`) · live-toggle: hk-xjbvi (OPEN, daemon-reliability) · multi-remote: research (`plans/2026-06-20-remote-node-telemetry-autoscale/phase2/`) | **ELEVATED into ACTIVE remote lane** (operator 2026-06-25 ~21:29Z, via admiral; directive comms 019f00af) | 3 parts: (1) **concurrent local+remote routing = LANDED today (hk-f10xl)** — per-queue `local_only`/`worker_target` gate the single `SelectWorker` call (workloop.go:2928-2936); one local queue + one remote queue dispatch concurrently. REMAINING = a LIVE e2e proof + the (2) live worker on/off toggle hk-xjbvi (OPEN, restart-only today) — **TOP of parked, FIRST to pull next (operator 21:xxZ)**. (3) multi-remote N-worker = still V1 single-worker (`ErrTooManyWorkers`); **BOTTOM of parked, explicitly NOT a rush (operator)**. Captain scoping the live-validation + toggle into the remote lane. |

## DONE recently (context only — verify with `br show` if regression suspected)

- **leanfleet** (`hk-itoc`) — token-efficiency campaign. CLOSED.
- **Codex local soak** (`hk-0639`) — harness proven 5/5 e2e under load, ChatGPT-billed. Epic still OPEN by open-ended-soak charter (operator's close-or-keep call).

## Operator-pending decisions (admiral surfaces; operator settles)

- **hk-4u1mb** (reviewer diff-budget) — conflicts with shipped hk-sj6a heartbeat contract; captain recommends progress-aware discriminator. Operator leaning DEFER.
- **Governor threshold** (`liveness_no_progress_n`) = 10 (observe/emit-only). Operator's policy call; 10 stands unless operator says 0.
- **Close hk-0639 soak epic?** Functionally done; OPEN by charter. Operator's close-or-keep call.

## Audit marker 2026-07-08 ~21:34Z (admiral)
- **v0.5.0 now gates on THREE, all tracked+staffed:** (1) A5 flip, (2) dispatch-fix PR #26, (3) [done] Pi.
- **Pi core-goal (hk-fdbhf): CLOSED in-daemon.** In-daemon canary GREEN (run 019f4365-e7cb, commit e3c66024) under the narrow no-$HOME warm_read. Operator core goal PROVEN in-daemon. hk-fdbhf.1 (startup-hang) re-scoped P3 follow-up. Captain reported milestone to operator.
- **PR #19/#25 dispatch-down (my directive → captain → chani): EXECUTED FAST + CORRECT.** Captain git-verified ground-truth, filed epic hk-5gmkd, pivoted chani (Pi closed). chani: deterministic repro grounded on current main → fix landed → **PR #26 (OPEN/MERGEABLE, git-verified) supersedes #19, closes #25**, commit 60e2f1a9 removes permissions.allow at both write-sites + inverts the regression test. E2E-gate regression-test discipline honored. CI in flight.
- **A5 (gurney, PR #24):** -timeout=20m cleared the daemon 10m panic; sole remaining red = cmd/harmonik (signature-less, both runs). Still converging.
- **LOCKED-DECISION DEFENDED again:** captain refused read-all warm_read, held narrow no-$HOME set (secret-exfil safety). OPTION-A intact.

## Audit marker 2026-07-08 ~22:33Z (admiral)
- **ALIGNED + MOVING, no drift.** All three v0.5.0 gates progressing hard since 21:34.
- **PR #26 (dispatch-down):** SUBSTANCE GREEN + independent adversarial review PASSED. Spec-drift caught + reconciled (WM-040a flipped MUST-inject → MUST-NOT, commit a7245148; hk-53y35 marked superseded). scenario Tier-3 x2 + hooks green. Sole red = cmd/harmonik check-short flake INHERITED from main (0 files under cmd/ touched) = the SAME bug #24 fixes. Correctly HELD ready to rebase onto post-#24 main. DONE pending human merge.
- **PR #24 (A5): ROOT CAUSE FOUND + FIXED (git-verified head 477c7994).** Not a deadline, not OOM — TestWorkflowModeDot_ValidRefPassesFlagParse (+sibling) reached runBeadSubcommand's $TMUX self-wrap syscall.Exec, vanishing the go-test process on the tmux-less CI runner (signature-less FAIL, ~218 t.Parallel orphaned). Fix stubs runBeadSelfWrapExec; VALIDATED locally under exact CI env (env -u TMUX -race -p=1 -parallel=1). **1st Tier-2 STEP-green landed (pass 23m56s); 2nd run pending** for the 2× gate. Second recurrence of this class (b67db8ee was TestRunTmuxEnvSet) → captain to file P3 test-guard.
- **Sequencing correct:** #24 lands (2× STEP-green) → boot stilgar co-verify → merge #24 → chani rebases #26 → merge #26 → captain flips continue-on-error (A5) → redeploy through PRE-DEPLOY E2E GATE → cut v0.5.0.
- **E2E-gate honored:** #26 carries an isolated E2E dispatch-past-modal canary. Pre-deploy E2E gate on the redeploy still OWED (captain not yet at deploy).
- **No new initiative, no locked-decision reversal.** watch-stalled ops alerts (#33/#34) continue — watch/operator lane, ops-monitor auto-nudges; not admiral drive.

## Audit marker 2026-07-08 ~23:33Z (admiral)
- **BOTH release-critical PRs LANDED. A5 LIVE.** Verified against ground truth (`git log origin/main`):
  - **PR #24 (A5) MERGED** — 364f0400 on main. check-short flake root-caused ($TMUX self-wrap syscall.Exec) + fixed; 2× STEP-green co-verified real by captain.
  - **PR #26 (dispatch-consent) MERGED** — 9120aa62 on main, hk-5gmkd CLOSED, closes #25 (auto), supersedes #19. Fully green post-rebase (SIGINT flake cleared once #24 in base = confirms diagnosis).
  - **A5 FLIP CONFIRMED REAL** — ci.yml `check (Tier 2)` job has NO continue-on-error (line-48 continue-on-error is the non-blocking `hooks` job — verified, not a miss). check-short now merge-blocking.
- **hk-to22v CLOSED** against 364f0400 (manual-PR route; captain closed after gurney's flag). Not admiral-driven — noted the gap.
- **REMAINING TO CUT v0.5.0:** (1) redeploy live daemon through the PRE-DEPLOY E2E GATE (dispatch-past-modal canary — the #26 fix must be proven live before cut); (2) close superseded PR #19 (still OPEN, won't auto-close). Directed captain on both this audit.
- **OPTION-A intact; E2E-gate enforced on the coming redeploy.** No drift, no locked-reversal.

## Audit marker 2026-07-09 ~00:33Z (admiral)
- **SELF-CORRECTION (load-bearing):** my 23:33 "A5 flip CONFIRMED REAL" was WRONG/incomplete. Dropping `continue-on-error` is necessary but NOT sufficient — captain found `check (Tier 2)` is not a REQUIRED status check in branch protection, so PR #27 is red-but-mergeable (UNSTABLE+MERGEABLE). True fail-closed A5 needs the check marked REQUIRED (repo-admin gh-api setting). A5 is NOT yet fail-closed. Lesson: verify the required-check, not just the yaml flag.
- **PR #19 CLOSED** (superseded-by-#26) — my directive executed. Verified state=CLOSED.
- **hk-88c29 flake fix LANDED** — PR #28 merged to main (1f985633, verified `git log`). The intermittent TestHandler_Launch_NilHandlerSpec_StdinNotClosed -race flake that #27 surfaced is fixed; no longer gates the cut.
- **Captain's revised cut order (endorsed):** land hk-88c29 [DONE] → set `check (Tier 2)` REQUIRED in branch protection → merge A5-flip PR #27 → redeploy → PRE-DEPLOY E2E dispatch-past-modal canary → cut v0.5.0.
- **AUTHORIZED captain** to set the required-check via gh api — it is correct EXECUTION of the locked A5 precondition (KNOWN lane, admiral's call), not a new decision or locked-reversal. Informed operator (branch-protection = repo governance).
- OPTION-A intact; E2E gate still enforced. watch-stalled alerts (#37/#38) ongoing — watch/operator lane.

## Audit marker 2026-07-09 ~01:33Z (admiral) — AT THE CUT GATE
- **THE E2E GATE PAID OFF — caught a real ship-blocker.** Captain ran the PRE-DEPLOY dispatch-past-modal canary; GATE 0 FAILED: a *committed* `.claude/settings.json` permissions.allow block (11 tools, aab94ed2) still tripped the Claude 2.1.205 consent modal → agent_ready hang. #26 removed the daemon INJECTION but not the COMMITTED block → necessary-but-insufficient. Without the gate, v0.5.0 would have shipped with dispatch still dead. Vindicates the standing enforcement.
- **Fixed + re-verified:** PR #29 (de-commit permissions.allow; daemon runs --dangerously-skip-permissions, devs keep pre-approval via gitignored settings.local.json) merged to main @ **e59beef8** (git-verified head). GATE 0 re-ran PASS: scratch daemon @ 651f00aa → agent_ready 2.3s + marker, live daemon untouched.
- **A5 now GENUINELY fail-closed:** #27 dropped continue-on-error + branch protection CREATED with `check (Tier 2)` as REQUIRED status check (strict=false, enforce_admins=false — minimal-friction, preserves captain self-merge/emergency bypass). Not cosmetic.
- **Redeploy DONE + verified:** live daemon on e59beef8, deploy tag **daemon-20260708-01** (git tag --points-at e59beef8 = confirmed), rollback binary intact, dispatch resumed live.
- **Merge train complete:** #24 (A5 CI) + #26 (injection) + #28 (hk-88c29 flake) + #27 (continue-on-error) + #29 (committed-block) all on main; #19 closed; hk-88c29 + hk-5gmkd + hk-to22v closed.
- **ESCALATED to operator: final GO to tag v0.5.0.** All gates green + verified. Cut = release/outward-facing/hard-to-reverse = operator's call, not admiral self-auth. Recommended GO. Captain holding for the word.
- **Deferred (non-blocker):** supervisor asset-skew (10 files/6 conflicts) — captain correctly NOT blind-applying (AGENTS.md managed-region regression risk). gurney2-q paused-by-failure on a premature A5 bead dispatch — HARMLESS (nothing merged), leave paused.

## Audit marker 2026-07-09 ~02:13Z (admiral) — keeper-restart resume; v0.5.0 SHIPPED, teardown IN FLIGHT
- **v0.5.0 CUT + PUBLISHED (DONE).** Operator gave GO 01:54Z → captain cut+published 01:56Z. Tag `v0.5.0` → e59beef8 (git-verified: local tag + `git tag --points-at origin/main`), GitHub release live (not draft): https://github.com/gregberns/harmonik/releases/tag/v0.5.0. Release notes finalized incl. dispatch consent-gate section (#26 injection + #29 committed-block + GATE-0-caught-it). Deploy tag daemon-20260708-01 also on e59beef8; live daemon serving. Release chain COMPLETE. Captain reported to operator; nothing outstanding on the release.
- **WIDE TEARDOWN SWEEP directed 02:10Z (operator-authorized, post-v0.5.0 idle pause).** Admiral issued 4-phase safe-sequence directive to captain (PHASE 0 quiesce daemon+crews+stalled watch → PHASE 1 commit meaningful untracked work → PHASE 2 worktree sweep → PHASE 3 prune 1339 merged branches → PHASE 4 paused-queue debris). Captain ACTIVELY EXECUTING: pane shows Phase-1 commit-untracked-work sub-agent running (live spinner) — captain alive + on-task (presence-stale in comms who, pane-truth confirms). This directive was issued ~2min before the keeper cycled me; on resume it is progressing correctly.
- **ALIGNED + MOVING — no drift, no escalation.** Captain keeper doctor GREEN (live-watcher + managed + pane live — cold-start watch clean). watch-stalled ops alerts (#41, >1147m) ongoing but Phase-0 teardown stands the watch session down anyway — not admiral-drive. No new-initiative ranking, no locked-decision reversal pending. Operator needs nothing from admiral this cycle.

## Audit marker 2026-07-09 ~02:18Z (admiral) — TEARDOWN COMPLETE + one decision made
- **Teardown sweep COMPLETE (all 5 phases), verified via captain report.** P0: dispatch PAUSED + 11 crew/watch sessions stood down (captain/admiral/-default/ops-monitor kept). P1: 4 grouped commits pushed → main e59beef8..**cd2f7501** (release notes, 3 kerf works, quality-enforcement + world-models plans, crew/context state); .subscribe.jsonl gitignored. P2: worktrees 14→2 (12 removed clean). P3: local branches 1882→547 (1335 merged deleted, 542 unmerged WIP kept, remote refs pruned). P4: 29 paused-by-failure queues LEFT+noted (no daemon-live purge path — queue cancel is daemon-dead-only; archivable in a future daemon-down window). 3 branches refused safe -d, left in place (benign).
- **DECISION (admiral's own, non-destructive, KNOWN work):** `hk-release-prep` worktree held uncommitted release-infra (.goreleaser.yaml, RELEASING.md, release-preflight.sh, Makefile/main.go) on no branch → directed captain to PRESERVE on a new `release-infra` branch + push (discarding real infra work was the only lossy path; it extends the v0.5.0 release automation). Then remove the worktree. NOT an operator escalation.
- **Fleet cleanly QUIESCED (deliberate post-v0.5.0 end-state).** Resume dispatch with `harmonik supervise resume` when re-standing the fleet. Relayed the full teardown milestone + my one decision to operator (informational — nothing needed). Admiral idle/oversight-armed; comms watch re-armed.

## Audit marker 2026-07-09 ~03:33Z (admiral) — keeper brief-inject defect → captain, redeploy HEAD
- **NEW TRACKED INITIATIVE — keeper-inject-survives-timeout (operator-authorized, ACTIVE via captain).** Root-caused live: admiral.cycle cyc-...000015 phase=aborted reason=handoff_timeout — keeper drove the restart but the handshake timed out before `agent brief --wake keeper-restart` injected; agent booted wake=fresh, blind (operator had to hand-run brief). NOT a stale-binary issue (running keeper newer than the Jul-4 fix d80e6141) — a live handoff-timeout defect. Beads (codename:keeper-inject-survives-timeout, all P1 OPEN, git/br-verified): **hk-fi78d** brief-inject survives handoff_timeout · **hk-ypuym** operator-initiated restart uses same ack→/clear→brief sequence · **hk-w2sel** daemon test-harness exercises real handshake + asserts brief lands on ack-timeout (blocks-wired behind both). Captain ACKed + owns end-to-end (implement+test+redeploy).
- **REDEPLOY HEAD directed (operator ask).** Live daemon at e59beef8 is 6 commits behind HEAD (all docs/chore, no code delta) — operator wants us running off latest binary going forward. Captain to build HEAD + swap local daemon via docs/daemon-redeploy.md. **PRE-DEPLOY E2E TEST GATE enforced** — harness (hk-w2sel) must be green before the fix-bearing swap.
- **ALIGNED + MOVING — no drift, no escalation.** Active work = operator-directed this hour (highest value by definition). Captain online, beads filed+wired correctly, comms watch alive (3 followers). ops-monitor watch-down/watch-stalled alerts (#7/#44) are the intentional-teardown artifact (watch down by design), NOT an incident — clear on re-stand. No locked-decision reversal, no new-initiative ranking pending. Operator needs nothing this cycle beyond awaiting the captain's redeploy tag.
