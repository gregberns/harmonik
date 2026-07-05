<!-- TIER: 2 (operational state, days cadence)
     LOADED BY: captain @ STARTUP Step 0b; NOT loaded by crews or implementers
     OWNER: captain, updated at session end (before HANDOFF.md) or on any crew/epic change
     DO NOT PUT HERE: standing behavioral rules (→ orchestrator-rules skill);
                      this-session salvage / run-id play-by-play (→ HANDOFF.md tier-1);
                      durable phase/locked decisions (→ project.yaml tier-3) -->

# Tier-2 context: captain lane registry + medium-term tracker (days cadence)
# Captain reads on every boot (STARTUP.md Step 0b) BEFORE re-deriving lanes.
# Stable across /clear cycles; verify every claim against live ground-truth at Step 2.

## ⭐⭐ OPERATOR DIRECTIVE 2026-07-04 ~18:10Z (via admiral, relayed to captain ~00:05Z) — LOCAL CAPPED 4, gb-mbp = THROUGHPUT CRITICAL PATH · expires: 2026-07-08
> Supersedes the 17:00Z throughput part-(2) (which said raise local 4→10). CORRECTION:
> - **LOCAL box is HARD-CAPPED at 4 concurrent** — NOT a disk artifact, do NOT raise. Do NOT `set-concurrency`
>   past 4, do NOT bump config max_concurrent past 4. (Captain reverted config.yaml 5→4 at ~00:05Z; live daemon
>   already ran 4. config.yaml is gitignored so the on-disk value is durable across restart.)
> - **The 5–10 concurrency target lives ENTIRELY on gb-mbp (remote).** gb-mbp safe re-validation is the **SOLE
>   throughput path = critical path**, not optional offload.
> - **SEQUENCE:** keep local 4 → root-cause the gb-mbp idle-hang/launch-gap + LAND the fix → serialized
>   (max_slots:1) quiet-window re-validate → drive concurrency to 10 ON gb-mbp → fill remote slots from the READY backlog.
> - **STATE (captain, ~00:10Z):** the idle-hang fix was NOT actually filed (workers.yaml note claimed it was) →
>   captain FILED it as **hk-xkou8** (concurrent agent_ready blind-spot + stale-detector stuck on sess.Wait at
>   max_slots>1; 1-slot is validated-good, failure is only at concurrent slots). Sequenced into jessica's
>   daemon-reliability lane LAST (internal/daemon → serializes with her work). gb-mbp stays **enabled:false**
>   until hk-xkou8 lands + a serialized quiet-window re-validate (fix-first; no quiet window while 3 local lanes run).
> - Manifest rollout gate **hk-ncg9m = CLOSED/landed** (its merge_build_failed note was the transient cache-wipe, cleared).

## ⭐⭐ CURRENT TRUTH (2026-07-05 ~04:08Z — keeper /clear resume; fleet reconciled, critical path intact)
> Lean keeper-restart resume. Daemon UP. Critical path intact. Nothing blocked on operator.
> (Committed immediately — a prior uncommitted edit was clobbered by a daemon worktree-merge checkout.)
>
> **CRITICAL PATH (direction-log 07-04 18:10Z + 17:00Z):** local HARD-capped 4; gb-mbp = throughput
> critical path. THE fix = **hk-hs7ex** (codename:concurrency-split, OPEN): split the single daemon gate
> into local hard sub-cap (4) + total admission ceiling (local + Σ worker.max_slots), hoisting SelectWorker
> to admission so remote isn't throttled by the local 4. Assigned **jessica**, sequenced NEXT after her
> in-flight **hk-up1pk** (max_slots>1 regression test) — both internal/daemon, SERIALIZED. On land →
> deploy daemon binary → cycle daemon (activates gb-mbp staged max_slots:6 + the hk-5266t keeper fix) →
> serialized quiet-window gb-mbp re-validate → drive to 10 concurrent on gb-mbp → fill remote slots.
>
> **LANES NOW:**
> | crew | lane | queue | state |
> |---|---|---|---|
> | jessica | daemon concurrency-split (CRITICAL PATH) | jessica-q2 | ACTIVE — hk-up1pk in-flight ~44min (VERIFIED healthy: steady reasoning heartbeats + agent_ready, not wedged; iterating on deterministic Kill-surviving-pane sim). **hk-hs7ex QUEUED NEXT.** Will flag terminal-fail vs merge. |
> | stilgar | sleep-teardown bug (hk-xjr1n) | stilgar-q2 | ACTIVE — hk-xjr1n in-flight (~healthy, heartbeat-confirmed), idle-armed watching. |
> | duncan | eval-metrics WS1 (WS1c+WS1d LANDED+CLOSED) | duncan-q2 | **HELD/idle-armed** — WS1e (per-node-attr) now unblocked (WS1b closed) BUT edits workloop.go = COLLIDES with jessica's hk-hs7ex. Was submit-wedged ("boot WS1e now" — wedge SAVED the collision); re-driven 03:48Z to hold, ACK'd. RE-TASK to WS1e the moment jessica's workloop.go lands. |
> | watch | triage tier | watch-q | 🔄 RESTARTED 04:07Z (session 12525305…). `watch-up` probe keys on watch's event-CONSUMER CURSOR advancing past pending events, NOT the registry — so STOPPING watch freezes the cursor forever (WRONG lever). RIGHT lever = RESTART: fresh session hits WATCH_RESTART_SUPPRESS_WINDOW=600 (10min boot suppress) + its fresh consumer drains the backlog → clears watch-up. Keeper NOT armed (hk-5266t broken until redeploy; watch holds no critical context). If it re-wedges before redeploy: `harmonik start crew watch` again, NEVER `crew stop`. |
>
> **NOTE:** WS3b feeders (hk-eval-prog-quality-feeders-k5bxl) COMPLETED success 03:47Z. Paused-by-failure
> cruft (crashrepro, gbmbp-*, leto-*, loadtest, paul-q, pi-*, sandbox-q, spread-pi) = pre-existing. gb-mbp
> stays enabled:false until the post-hk-hs7ex serialized re-validate. Local pinned 4.

## ⭐⭐ (SUPERSEDED) CURRENT TRUTH (2026-07-05 ~01:35Z — keeper /clear resume; duncan RE-STAFFED, watch recovered)
> Lean keeper-restart resume. Ground-truth reconciled: daemon UP, jessica-q2 + stilgar-q2 both active 1w
> (critical beads in flight, as claimed). Recovered watch (idle-submit-wedge → re-joined comms). RE-STAFFED
> the drained duncan onto eval-metrics WS1 parser tail. Fleet now 3/4 local active. Nothing blocked on operator.
>
> **CHANGES this resume:**
> - **duncan RE-STAFFED** (was DRAINED/free): eval-metrics WS1 epic hk-9jdid (its WS1a/WS1b landed+closed;
>   mirrored assignee stilgar→duncan). Dispatching WS1c hk-eval-prog-codex-tokens-fbhir (codexjsonlparser.go)
>   + WS1d hk-eval-prog-pi-tokens-sr316 (pijsonlparser.go) to duncan-q2 — file-disjoint parser-only, no
>   workloop.go (jessica collision guard held). WS1e (per-node-attr) HELD until jessica drains. Queue = duncan-q2.
> - **watch** re-joined comms after a submit-wedge nudge (back online 01:34Z).
>
> **LANES NOW (local 3/4 active):**
> | crew | lane | queue | state |
> |---|---|---|---|
> | jessica | daemon-reliability (DRAINED ~01:58Z) | — | **FREE** — **hk-xkou8 LANDED** (e0b02f77, bounds impl+rev sess.Wait in agent_ready-timeout paths = the gb-mbp concurrent-slot idle-hang fix; content-verified, CLOSED). ⚠️ RESIDUAL: merged code-only, NO max_slots>1 repro test (jessica filing a follow-up bead). Held for the gb-mbp re-validate phase. Landed prior: hk-rnkuy + hk-gf59k. |
> | stilgar | keeper reliability (DRAINED ~01:50Z) | — | **FREE** — **hk-5266t LANDED** (commit e5a1eed6, keeper doctor tmux-pane check for the zsh `:a` mangling — THE recurring watch-wedge root cause). NOTE: fix is on main but needs a daemon/keeper binary redeploy to take effect for LIVE keepers. RE-STAFF: WS1e (hk-eval-prog-per-node-attr-tqftl) is HELD until jessica's workloop.go drains; otherwise pull next file-disjoint ranked lane. Landed prior: hk-qe736. |
> | duncan | eval-metrics WS1 (RE-STAFFED 01:35Z off drained cache-reaper) | duncan-q2 | ACTIVE — epic hk-9jdid (WS1a/WS1b landed+closed; assignee mirrored stilgar→duncan). Dispatching WS1c hk-eval-prog-codex-tokens-fbhir (codexjsonlparser.go) + WS1d hk-eval-prog-pi-tokens-sr316 (pijsonlparser.go) as a wave — both P2/ready/no-blocker (verified won't insta-fail). File-disjoint parser-only, NOT workloop.go (jessica collision guard). WS1e (per-node-attr) HELD until jessica drains. Prior lane: hk-44ab2 + hk-whru3 both LANDED+CLOSED. |
> | watch | triage tier | watch-q | ⚠️ FLAKY — re-wedges ~every 20-30min (hk-5266t root cause, IN FLIGHT via stilgar). Recovered this resume (01:34Z re-join). Hand-nudge on each ops-monitor watch-stalled page (`send-keys C-u` + retype `comms join --name watch` + Enter). Does NOT blind you — Watcher-1 reads the bus directly. Stops once hk-5266t lands. |
>
> **NEXT STEP (02:02Z — BOTH critical-path fixes LANDED):** hk-xkou8 + hk-5266t both on main. jessica + stilgar DRAINED/free, held for the re-validate phase. Plan: let duncan's eval-metrics parsers (WS1c/WS1d) drain → **redeploy daemon in the lull** (activates hk-xkou8 daemon-fix + hk-5266t keeper-fix) → **serialized (max_slots:1) quiet-window gb-mbp re-validate** → drive to 10 concurrent on gb-mbp → fill remote slots from ready backlog. Admiral offline (self-authorize the KNOWN sequence). Milestone surfaced to operator 02:02Z. gb-mbp stays enabled:false until re-validate. Local pinned 4.
> **Paused-by-failure cruft** (crashrepro, gbmbp-val/-val2, leto-codex, leto-q, loadtest, paul-q, pi-q, pi-scav-2, sandbox-q, spread-pi) = pre-existing, leave as-is.

## ⭐⭐ (SUPERSEDED) CURRENT TRUTH (2026-07-04 ~23:32Z — keeper /clear resume; fleet self-recovered from a fleet-wide wedge)
> Captain resumed after a context /clear. A fleet-wide wedge (API hang stranding runs) hit ~23:22Z and
> the daemon self-recovered: it **bounced 23:27:33Z** (single clean restart, no crash-loop), swept 3 wedged
> runs; fresh runs healthy. All three crews re-adopted after the bounce and re-submitted their beads onto
> FRESH `-q2` queues (the old `-q` queues are paused-by-failure cruft — leave them). Verified live: three
> queues ACTIVE with `workers=1` each (dispatched + running). Nothing blocked on operator; watchers armed.
>
> **LANES NOW:**
> | crew | lane | queue | model | state |
> |---|---|---|---|---|
> | duncan | cache-reaper GOCACHE-wipe fix (hk-44ab2, P1) — re-tasked off wake-economy epic hk-var9b to this reliability bead | duncan-q2 | opus | ACTIVE — hk-44ab2 in-flight (worker live). hk-whru3 HELD (prior fail was a false-fail of this same reaper bug; re-sub after hk-44ab2 lands). |
> | jessica | daemon-reliability (logmine iter-20 P1 register) | jessica-q2 | opus | ACTIVE — hk-rnkuy (crash-loop + daemon-death EventType) in-flight; then hk-gf59k (ledger-dep false-defer). hk-lt091 (empty-HEAD worktree-create race) already off her queue. **hk-qe736 REASSIGNED to stilgar** (see below) — jessica told 23:49Z to SKIP it despite her mission file still listing it. |
> | stilgar | worktree-leak reaper (hk-qe736) — RE-TASKED after the bounce off its eval-metrics lane (epic hk-9jdid); took the worktree-reaper LOCKED-blind-spot fix instead | stilgar-q2 | opus | ACTIVE — hk-qe736 fix committed in worktree (force-remove git-locked agent worktrees older than N days; targets the 11-day locked agent + 58 stale), in merge-gate. Eval-metrics WS1 (B1/B2) is now UNSTAFFED — re-staff when a slot frees. |
> | watch | triage tier | watch-q | sonnet | ⚠️ FLAKY — recurring idle-submit-wedge (~every 20min): its loop types the next prompt but the /rc auto-submit never fires; captain must hand-nudge (`tmux send-keys … C-u` + retype + Enter). ops-monitor detects it (watch-stalled escalations) but there's NO auto-recovery, so each wedge COSTS a captain wake (inverts the watch's purpose). Root cause = **hk-5266t** (P1 open, unstaffed — keeper can't inject restart, mangled tmux target). Auto-recover gap = **hk-ghcqn** (P2, filed 07-05). Until one lands, expect to nudge the watch on each ops-monitor watch-stalled page. Captain's OWN Monitor watcher consumes the bus directly, so a wedged watch does NOT blind the captain. |
> | ops-monitor | ops probe | — | — | ONLINE (re-beats ~5min). session-hosted bg loop, no scheduler — verify liveness if it stays absent. |
>
> **COLLISION WATCH:** duncan (cache-reaper, hk-44ab2) and jessica (worktree-reaper, hk-qe736) both touch
> "reaper" code — different reapers (GOCACHE-wipe vs worktree-LOCKED-reap), likely file-disjoint, but verify
> before both land near-simultaneously. **Many paused-by-failure queues** (pi-q, leto-q/-codex, crashrepro,
> loadtest, gbmbp-val/-val2, sandbox-q, spread-pi, paul-q, pi-scav-2) = pre-existing cruft, left as-is; do NOT resume.

## ⭐⭐ (SUPERSEDED) CURRENT TRUTH (2026-07-03 ~17:35Z — cold-boot after keeper /clear; close-out + eval staffed)
> Captain cold-booted from a keeper-placeholder handoff; recovered real state from own pre-/clear
> comms (17:15Z) + boot digest. Daemon UP @ max_concurrent=10 (spawn_cap=20). Per operator direction-log
> 07-03 10:30Z: **captain OWNS the close-out** ("push it all onto the captain"); close-out sequences FIRST,
> eval-program (23 beads) after. Fleet healthy, 2 lanes + volume moving; nothing blocked on operator.
>
> **LANES NOW:**
> | crew | lane | queue | model | state |
> |---|---|---|---|---|
> | leto | CLOSE-OUT chain (P0, captain-owned) | leto-q | opus | ACTIVE — Step1 hk-1hgjr (reviewer local review_correctness ErrMalformed, the e2e keystone). Diagnosed: finalize read short-circuits to a NON-retrying local read → transient review.json truncation → ErrMalformed post-commit false-fail; only the finalize read needs the remote path's retry-until-valid (mirror hk-qts7r / 9860e8a2). Isolated fix agent in flight, then 2 independent worktree reviewers → ff-land. THEN hk-r4p0l (srt no-op for pi) → api-fix branch worktree-agent-a6e56ba9b90b2c320 → clean e2e sandboxed ornith run. Fix OUT-OF-DAEMON (self-review bootstrap trap). |
> | gurney | eval-program ws:problems | gurney-q | opus | ACTIVE — author 6 new HARD eval tasks under evaltasks/ per plans/2026-07-03-eval-program/05-problem-set-and-tools.md + a codename:eval-program bead each. FILE-DISJOINT from leto (evaltasks/ + beads only; NOT internal/daemon). |
> | (daemon) | evalvol volume | evalvol | — | ACTIVE/healthy — 14 self-contained eval beads (claude harness), 3 clean completions, 0 fails. Fills admiral's 10-concurrent volume target. |
> | thufir | scavenger (on-call) | thufir-q | opus | IDLE-armed. NOT staffed — box already ~10 concurrent (overload guard); hold codex/scavenger volume until close-out settles. |
> | watch | triage tier | watch-q | sonnet | ONLINE. admiral = oversight/operator. |
>
> **NEXT (staged, after WS4 land / close-out settles):** eval-program WS1 metrics — but WS1a/WS1b touch
> workloop.go = COLLISION risk w/ leto's finalize-read close-out work; hold WS1 until leto's workloop
> changes land, or route WS1c/WS1d (codex/pi jsonl PARSER token-extraction, file-disjoint) first.
> **DGX-SSH GATE (hk-eval-prog-dgx-ssh-x7tzo) likely UNBLOCKED:** operator told admiral "DGX login user is
> gb — I already added your key" — verify + un-gate ws:dgx when close-out done. **NOTE:** leto.md AND
> gurney.md are BOTH git-tracked → commit mission edits immediately (daemon worktree-checkout reverts
> uncommitted tracked-file edits under concurrent merges; hit twice this boot). Paused cruft queues
> (main/pi-q/sandbox-q/paul-q/leto-codex/pi-scav-2) = pre-existing, left as-is.

## ⭐⭐ OPERATOR DIRECTIVE 2026-06-30 (via admiral) — FRESH WAKE, STAFF 3 LANES · expires: 2026-07-04
> Fleet woke from a ~4-day sleep onto the security-fix daemon (7a9bf2e5, deploy daemon-20260630-01).
> Operator confirmed: **staff all THREE lanes below; REMOTE is the top priority.** This block
> SUPERSEDES every 2026-06-25 priority/lane block below (those directives are EXPIRED — treat as history).
> 1. **REMOTE-WORKER e2e proof** (`hk-nepva`, epic remote-hardening `hk-gx0dl`) — #1 PRIORITY.
>    Blocker `hk-t1t00` is CLOSED, so nepva is UNBLOCKED.
>    **OPERATOR TESTING CONSTRAINT (load-bearing):** do **VERY THOROUGH LOCAL testing** first via the
>    L0–L5 test pyramid / isolated test-daemon harness — testing that does **NOT require restarting the
>    live daemon**. **gb-mbp is UP and available for the live-remote portion** once local is solid.
>    Confirm routing via events.jsonl `run_started.worker_name=gb-mbp`, NOT daemon stderr.
> 2. **Pi-harness core build** (`hk-4rmj1`, codename:pilot) — the new OpenRouter-based implementer harness,
>    Phase-0 (mirrors codex). Operator-UNGATED now (was parked behind remote-reliable). Blocked-by `hk-1c16h`
>    (pilot B2) — verify that landed before dispatching B3; if not, staff B2 first.
> 3. **Keeper reliability** — `hk-u5tgh` (watchdog tmux-restart bypasses daemon → crews come back keeper-LESS)
>    + `hk-xxcv9` (crew boot doesn't auto-arm keeper). ⚠️ `hk-u5tgh` carries `hold:operator-design-decision`
>    — CHECK whether that design decision is settled (plans/2026-06-22-keeper-coverage-investigation/00-SYNTHESIS.md)
>    before dispatching it; `hk-xxcv9` (P2) is clean to staff now.
> Lanes run PARALLEL (file-disjoint). Stale `paused-by-failure` queues (main, paul-q, leto-codex) =
> pre-sleep cruft; reconcile, do NOT resume main.

## ⭐⭐ CURRENT TRUTH (2026-06-30 ~18:20Z — keeper-restart resume; codex UNBLOCKED, durable fix in flight)
> Lean keeper-restart resume. Fleet clean, 3 lanes moving; nothing blocked on operator.
> Cleared the recurring submit-wedge on BOTH gurney AND paul panes (stale unsubmitted directives;
> C-u + retype + Enter per §4.3). Persisted gurney's proof-run directive into its MISSION file
> (was only in-pane → would vanish on compact; gurney is at ~3%-until-auto-compact, keeper healthy).
>
> **LANES NOW:**
> | crew | lane | queue | model | state |
> |---|---|---|---|---|
> | gurney | remote-reliability follow-ups | gurney-q | opus | HEALTHY — IDLE-ARMED for hk-1s1or (launch-hang stall blind-spot, in-flight on gb-mbp ~40min) to land → then the 6 concurrent-under-load proofs (hk-icdz/3zij/d2z1/tzfw/xbpm/k0pz) that GATE the 4→8 bump. Proof-run sequence + HARD constraints (temp gb-mbp enable for proofs ONLY → revert enabled:false+slots:1 after; restart-landmine check; coordinate restart w/ leto B4) now DURABLE in gurney.md mission. |
> | leto | pi-harness Phase-0 (hk-94c3t) | leto-q | sonnet | HEALTHY — B4 (hk-mkcwg) in-flight ~62min, advancing normally. No action. |
> | paul | codex durable self-heal (hk-2pb79) | paulk-q | opus | hk-2pb79 RESOLVED per-incident (root = stale-WAL recurrence, NOT auth; removed 3.9MB ~/.codex/state_5.sqlite-wal; canary commits, HEAD→2e8c3c1). Captain APPROVED Option A: build per-launch stale-WAL self-heal INSIDE CodexHarness.LaunchSpec (codexharness.go/codexlaunchspec.go = paul's files, file-disjoint from gurney stalewatch/leto pi*.go, threshold via config), out-of-daemon + 2 reviewers + ff-land → close hk-2pb79. hk-u5tgh (keeper lane) CLOSED. |
>
> **CODEX OFFLOAD (operator additive directive, now unblocked) — SEQUENCED, not yet:** spawn the
> scavenger (thufir, hk-0kr4j) for backlog drain on codex tokens AFTER (a) paul lands the durable
> self-heal AND (b) gurney's proof+restart cycle settles. Rationale: gurney's imminent daemon restart
> kills codex mid-run → re-triggers stale-WAL; adding codex load now = "getting in the way" (operator
> said hold off if so). Run scavenger on now-robust self-healing codex post-restart.
> **CONCURRENCY 4→8: still GATED** on gurney's proofs (operator's call) — unchanged. Daemon UP.
> paused main/leto-codex/paul-q + active-queue paused-on-hk-0lwje = pre-sleep/known-P2 cruft, left as-is.

## ⭐⭐ CURRENT TRUTH (2026-06-30 ~18:08Z — keeper-restart resume; paul re-tasked to codex-unblock)
> Lean keeper-restart resume. Fleet clean, all 3 lanes moving; nothing blocked on operator.
> Verified all 4 crews ALIVE via pane-truth (comms-who absence was presence-aging, not zombies).
>
> **LANES NOW:**
> | crew | lane | queue | model | state |
> |---|---|---|---|---|
> | gurney | remote-reliability follow-ups | gurney-q | opus | HEALTHY — monitoring hk-1s1or daemon run (remote launch-hang stall blind-spot); triaged hk-q54s8 (✔); planning the 6 concurrent-under-load proofs (hk-icdz/3zij/d2z1/tzfw/xbpm/k0pz) that GATE the 4→8 bump. |
> | leto | pi-harness Phase-0 (hk-94c3t) | leto-q | sonnet | HEALTHY — B4 (hk-mkcwg) in-flight ~52m, 4th dispatch, advancing normally (B3 ran 89m; equally complex). No action. |
> | paul | codex-harness UNBLOCK (hk-2pb79) | paulk-q | opus | RE-TASKED 18:05Z from the DRAINED keeper lane (hk-xxcv9 + hk-u5tgh both landed/closed). Now diagnosing the systemic codex fast-fail (7-9s no-commit, regressed 06-26 04:00-05:28Z; leading hypothesis = expired codex auth post-4-day-sleep). Enacts the operator codex-offload directive (direction-log 12:00Z); local canary = the required re-canary. COLLISION GUARD: shared internal/harness w/ leto pilot → shared-file edits gated on captain. |
>
> **Why paul→codex (autonomous, KNOWN bead):** the handoff weighed only daemon-core candidates (collide w/ gurney) and kept paul idle. The disjoint ready lane it missed = the codex-harness blocker hk-2pb79, which gates the operator's explicit additive codex directive. Idle opus crew + P1 investigation bead = clean model-fit. hk-2pb79 fix is the prereq before any codex/scavenger backlog drain or re-canary (a re-canary now would just reproduce the failure).
> **CONCURRENCY 4→8: still GATED** (operator's call) on gurney's concurrent-under-load proofs — unchanged, surfaced. Daemon UP. paused main/leto-codex/paul-q = pre-sleep cruft, left paused (NOT main).
> **Note:** active queue is paused-by-failure on hk-0lwje (de-hardcode scheduled-comms-send --body bug, P2) — this is why ops-monitor shows stale (scheduled watch-pings broken). Known P2, daemon-core-ish (collides w/ gurney), left for now.

## ⭐⭐ CURRENT TRUTH (2026-06-30 ~17:30Z — keeper-restart resume; REMOTE HEADLINE CLOSED)
> Lean keeper-restart resume. **MILESTONE: remote-worker e2e HEADLINE proven + closed.**
> Captain reconcile-closed hk-nepva (live gb-mbp e2e, commit 2f07fd9e) + hk-qts7r (review.json
> race fix, 4 clean APPROVE reads, commit 9860e8a2) and closed the now-complete epic hk-gx0dl
> (its 3 pieces — gate-base-SHA hk-t1t00, verdict-consistency hk-qts7r, headline hk-nepva — all done).
> Cleared a misdirected unsubmitted "close hk-nepva and hk-qts7r" sitting in gurney's pane (gurney
> correctly refuses br close; closing validated out-of-daemon work is the captain's job).
>
> **LANES NOW:**
> | crew | epic/lane | queue | model | state |
> |---|---|---|---|---|
> | gurney | remote-reliability follow-ups (hk-gx0dl CLOSED) | gurney-q | opus | RE-TASKED + woke + scoping. Lead hk-1s1or (P1 remote launch-hang stall blind-spot — found scope: stalewatch.go suppresses stall once launchInitiatedSeen=true); triage hk-q54s8 (P1 deploy daemon-boot crash, DAEMON-CORE/risky) + hk-icdz concurrent-proof set (gates 4→8 bump). |
> | leto | hk-94c3t pi-harness Phase-0 | leto-q | sonnet | HEALTHY — B4 (hk-mkcwg) in-flight ~24m, advancing normally. No action. |
> | paul | hk-tswe0 (keeper/daemon defect cluster — DRAINED) | paulk-q | opus | IDLE-ARMED. No CLEAN file-disjoint ready lane (candidates daemon-core → collide w/ gurney). Correctly idle, NOT a missed-staffing-failure. Re-task only on a disjoint lane appearing. |
>
> **CONCURRENCY 4→8: still GATED** (operator's call). Headline proves remote works; the e2e-concurrent-
> UNDER-LOAD proofs (hk-icdz family) aren't done — gurney picking those up. Surfaced to operator.
> Watchers W1 (comms recv --follow) + W2 (epic_completed) re-armed. Daemon UP.

## ⭐ CURRENT TRUTH (2026-06-30 ~12:05Z — captain staffed the 3 lanes; all verified live)
> Cold boot after the ~4-day sleep. Daemon UP. Reconciled pre-sleep cruft: cleared 8 ghost crew
> records (adam, bob, gurney, kynes, leto, paul, stilgar, thufir); left `main` + `leto-codex` PAUSED
> (did NOT resume main); `paul-q` stuck paused-by-failure (1 failed item, resume wouldn't clear) so
> paul runs on a FRESH queue `paulk-q`. admiral online (operator).
>
> **3 LANES STAFFED + VERIFIED (comms-online AND working pane):**
> | crew | epic | queue | model | lane | state |
> |---|---|---|---|---|---|
> | gurney | hk-gx0dl | gurney-q | opus | REMOTE e2e proof (#1) — LOCAL L0-L5 pyramid + isolated test-daemon FIRST (no live-daemon restart), then live hk-nepva on gb-mbp (confirm worker_name=gb-mbp via events.jsonl). Blocker hk-t1t00 CLOSED. | ACTIVE |
> | leto | hk-94c3t | leto-q | sonnet | PI-HARNESS Phase-0 build (codename:pilot) — B3 hk-4rmj1 → B4-B9. B2 hk-1c16h CLOSED. UNGATED. | ACTIVE |
> | paul | hk-tswe0 | paulk-q | opus | KEEPER reliability — dispatch hk-xxcv9 (crew-boot auto-arm, clean P2) NOW. | ACTIVE |
>
> **HELD — operator design call (surfaced 2026-06-30):** hk-u5tgh (watchdog tmux-restart bypasses
> daemon → crews come back keeper-LESS). Captain confirmed synthesis NOT-settled — open question
> (item f): standalone ctx-watchdog as canonical crew governor vs flip crews to per-keeper auto-restart.
> Paul holds hk-u5tgh until operator decides; lane 3 runs hk-xxcv9 meanwhile.
>
> **PARKED (crews cleared on wake; known/resumable):** codex (hk-8dtyk — leto re-tasked to pilot),
> wake-economy (hk-var9b — MVP live, zero ready), scavenger (hk-0kr4j — thufir cleared).
> Note: gurney/leto `model:` in their missions = the CREW-orchestrator model; implementers still run
> sonnet-via-DOT. leto=sonnet with strict "escalate on ANY run_failed, don't self-classify".

## ⭐ OPERATOR-CONFIRMED PRIORITY SEQUENCE (set:2026-06-25 expires:~2026-06-29 — via admiral, survives /clear)
> The fleet ALREADY honors this — recorded so it survives a captain restart; NO reshuffle on read.
> Lanes run PARALLEL where slots + disjoint work allow. WIP-first is a TIEBREAKER, never serial gating.
> **This is a PRIORITY order, NOT a sequence: the #1 headline being blocked/gated NEVER idles #2–4.**
> If the remote-worker headline is parked behind a substrate/quiet-window gate, the OTHER lanes keep
> running — a single gated lane must never sequence or idle the rest of the fleet (orchestrator-rules
> §ANTI-IDLE). A lane is only idle on zero ready beads OR a named/dated/owned/unexpired gate.
> 1. **REMOTE-WORKER RELIABLE** — the headline (gurney STAGE-3 real e2e on gb-mbp). GOAL behind it:
>    once remote is PROVEN reliable, raise daemon `max_concurrent` 4→8 (remote adds the capacity).
>    **Do NOT bump concurrency now — GATED on remote-reliable.** Un-park gb-mbp + re-validate serialized
>    only after BOTH remote code bugs land (review.json read-retry = LANDED e4122ac9; worktree-HEAD race
>    = hk-iaj1w, gurney fixing offline).
> 2. **TOKEN-OPT** (epic hk-var9b / wake-economy) — currently ZERO ready beads (all blocked/deferred/in-flight)
>    = correctly idle, NOT a stall. Resume the moment ready file-disjoint work appears.
> 3. **CODEX** (leto pilot, queue leto-codex) — correctly filling idle local slots + offloads model cost.
> 4. **FLYWHEEL/FRAMEWORK** (admiral/captain framework: PLAN-v2 + stall-detector) — admiral drives;
>    artifact + skill edits, does NOT compete for daemon slots.

## ⭐ CURRENT TRUTH (2026-06-25 ~19:07Z — gb-mbp DISABLED again; fleet LOCAL; 2 lanes)
> **Live remote STAGE-3 (gb-mbp re-enabled 18:46Z for the headline) found TWO remote bugs that
> paused BOTH lanes — a RECURRENCE of the 2026-06-23T21:55Z concurrent-remote-failure pattern:**
> 1. 3/3 leto codex beads raced worktree-create on the worker → `git rev-parse HEAD returned
>    empty` (concurrent-worktree-HEAD race; proof beads hk-k0pz/xbpm/tzfw).
> 2. gurney hk-h106 → truncated remote `review.json` ErrMalformed (bead **hk-clrts**; root cause
>    diagnosed: `reviewverdict.go:126-139` does ONE cat-over-SSH, no retry/completion-wait).
>
> **RECOVERY (captain, this session):** disabled gb-mbp in workers.yaml (gitignored — persists, NOT
> committed), restarted daemon PID3214→28967 (brick-safe: config has `max_concurrent:4` +
> `liveness_no_progress_n:10`), routed LOCAL, resumed both queues. Execution+routing on gb-mbp WERE
> proven (commit a4ec1612) before the race surfaced — the headline's core proof holds.
>
> **CURRENT LANES (both LOCAL):**
> - **leto** (codex concurrency pilot, epic hk-8dtyk, queue leto-codex): ACTIVE — hk-vo31l + hk-dyqy
>   + hk-gx46b in implement LOCAL; standing lane, pulls file-disjoint clean P3/P2 when it drains.
> - **gurney** (remote-test-pyramid epic hk-6l941 / hardening hk-gx0dl, queue gurney-q): pivoted to
>   OFFLINE remote-bug fixes — hk-clrts (review.json, FIX-READY) + the worktree-HEAD race — fixed
>   out-of-daemon (isolated worktree→review→ff-land) + local L-series test layers.
> - **Headline hk-nepva (live remote e2e) PARKED — explicit un-gate condition:** un-park the
>   MOMENT (a) both remote bugs are landed on main (review.json read-retry = e4122ac9 LANDED;
>   worktree-HEAD race = hk-iaj1w) AND (b) a quiet window is available for the SERIALIZED
>   (max_slots:1) re-validation. When both hold, gurney RE-ENABLES gb-mbp and GOES — this is a
>   KNOWN ranked lane, not an operator escalation. This park is a substrate/quiet-window gate, NOT
>   "wait for captain to say go"; it must never sequence the whole fleet (leto + STAGE-1/local
>   gurney work run in parallel meanwhile).
> - watch = online, triaging correctly (escalated + confirmed recovery). admiral = oversight (no beads).
>
> ---
> ## (SUPERSEDED) CURRENT TRUTH (2026-06-25 ~06:28Z — COURSE CHANGE: remote test-hardening program)
> **Operator settled a new strategy (asleep now; admiral oversees, captain OWNS execution).**
> Stop chasing real-remote runs (feedback loop too slow). Instead build a **test pyramid
> (L0–L5)** that reproduces remote "separation" (filesystem / git-ref / tmux-process /
> SSH-transport) cheaply at rising fidelity. Daemon repointed **LOCAL** (gb-mbp DISABLED in
> workers.yaml, concurrency=4). Plan: `.harmonik/crew/designs/remote-test-strategy-plan.md`
> (+ `remote-iteration-impasse-plan.md` for moves ①②③, ④ skipped). **THIS block is authoritative;
> the lane table + directives below are PRE-COURSE-CHANGE and STALE.**
>
> **THE PROGRAM — kerf work `remote-test-pyramid`, epic `hk-6l941` (assignee gurney). Bead set filed + ranked:**
> - `hk-hd2w6` **L0** — runner-seam contract: add `daemon.Config.Runner`, thread to DOT gate/cascade,
>   add the 3 missing `…Via` read variants (gate-verdict `dot_gate.go:551/686` + `autostatusmarker.go:70`
>   are STILL bare `os.*` = unfixed hk-f3u6o class), + static no-bare-os.* audit + RecordingRunner contract test. **✅ LANDED 2026-06-25 (02da6...c940a8a5, DOT triple-review clean) — CLOSED.**
> - `hk-52xnr` **L1** (←L0) — twin harness on SEPARATED worker FS + Runner injection; deterministic verdict/gate-bug reproduction. **IN PROGRESS on gurney-q.**
> - `hk-8u2al` **L2** (←L1) ssh-localhost isolated · `hk-3q92c` **L4** (←L1) fault/chaos+replay ·
>   `hk-f10xl` **L5** (←L0) per-queue routing (move ②, gate `SelectWorker` workloop.go:2813) + scheduler property tests ·
>   `hk-yflqo` **L3** (←L2, P2, **LAST**) Docker/Lima containers + Linux-remote · `hk-o85ye` **move-③** (←L0, P2) bead-runs survive daemon restart.
> - `hk-t1t00` REWRITTEN (premise was wrong: affected-set is `headSHA..HEAD` per scenariogate.go:325-333, `HK_GATE_BASE_SHA` doesn't exist) — folded in, blocked-by L1.
>
> **OPERATING RULES (operator, this program):** all TESTING → low blast-radius, keep moving.
> Blocking bug mid-stream → SMALL fixes done DIRECTLY out-of-daemon (isolated worktree→review→ff-land),
> NOT the slow pipeline. Crews MAY use sub-agents but EVERY change reviewed. Review gate = multi-agent
> consensus of ≥2 DIVERSE agent types, NOT human signoff; split → admiral adjudicates.
>
> **Crew state:** gurney = remote-test-pyramid (LOCAL gurney-q; L0 landed, NOW running L1+L5 = 2 workers, M3 held).
> admiral = oversight + hourly watchdog (holds no beads). **watch = OFFLINE/wedged** (absent from comms who,
> pane frozen at "Sautéed 4m36s" w/ unsubmitted prompt @114k — flagged, NOT critical-path; captain armed
> direct on comms recv; let ops-monitor crew-fresh flag catch it). **paul DOWN — HELD** (wake-economy parked). All other
> initiatives PARKED behind this program (Pi gateway, codex-on-remote, de-hardcode-messages, wake-economy).
> hk-f3u6o LANDED on main (5999a39a) + CLOSED. gb-mbp re-enable is a later phase (L2/real-remote smoke), not now.
>
> **Staffing decision (RESOLVED 2026-06-25 on L0 land):** did NOT split to a 2nd crew. L5/M3 are children
> of gurney's epic hk-6l941, so a 2nd crew would collide on epic ownership + cost an Opus boot for marginal
> gain (daemon parallelizes either way at max-concurrent=4). Instead gurney fans out the file-disjoint
> unblocked layers into gurney-q: L1 (test-harness) ‖ L5 (daemon-routing) running concurrently; M3
> (hk-o85ye, runs-survive-restart) sequenced AFTER L5 since both touch internal/daemon (worktree-merge
> collision guard). L2/L4 unblock when L1 lands — gurney continues them on the same queue.

## active_lanes  (PRE-RESTART — STALE as of the 22:50Z restart; see CURRENT TRUTH block above)

> **DURABILITY NOTE:** this file MUST be COMMITTED, not left uncommitted-modified — an
> uncommitted tier-2 edit was clobbered 2026-06-24 when a worktree merge (a8d4591b) reset
> the working tree. Commit tier-2 changes the same session you make them.

> **OPERATOR DIRECTIVE 2026-06-24 (standing, via admiral):** the daemon must run **4
> concurrent instances always** on this box — "1-2 is not enough." Live `queue
> set-concurrency 4` is set. There is NO config field for default concurrency (reverts on
> restart), so the RESTART RECIPE must re-apply `set-concurrency 4` AND raise the spawn cap
> (HARMONIK_MAX_CONCURRENT_SESSIONS 8 → ~16) at the next daemon restart so 4 concurrent DOT
> triple-review runs don't hit spawn_cap_blocked (memory hk-vfeeo). Keep 4 file-disjoint
> lanes staffed.

> **RESTART-RECIPE MANDATORY STEP — hk-drygf deploy landmine (added 2026-06-24, captain):**
> hk-drygf LANDED on main (SHA a0af5152) made `liveness_no_progress_n` a **REQUIRED** config
> key, but live `.harmonik/config.yaml:63` has it COMMENTED OUT (`# liveness_no_progress_n: 10`).
> The running daemon + auto-revive use the OLD installed binary so it is NOT bricking now — but
> the moment this binary is `go install`ed and the daemon restarts (the SAME lull-deploy restart),
> `GovernorConfig()` fails loud and **the daemon REFUSES TO BOOT** until the key is set. BEFORE that
> restart: (1) set `config.yaml:63` to `liveness_no_progress_n: 10` (jamis RECOMMEND — enables the
> liveness detection axis at historical default, observe/emit-only, low risk) OR `: 0` (explicit
> keep-disabled). The VALUE is the operator's threshold-policy call by design; default to 10 unless
> operator says 0. (2) Fix the now-false `'all fields optional'` block-header comment.

> **OPERATOR DIRECTIVE 2026-06-23 (STANDING, via admiral, recorded 2026-06-24):**
> **TOKEN-OPTIMIZATION IS NOW THE PRIMARY PRIORITY — especially the CAPTAIN'S OWN token
> burn (ranks HIGHEST of all).** Reprioritize kerf-next ranking + crew staffing so
> token-burn-reducing work LEADS; token-opt WINS staffing/sequencing ties. Order:
> (1) **WATCH** initiative (captain's own wake-burn cut, hk-var9b) = TOP — sequence its MVP
> (coupled watch-standup + sender-redirect) ahead of other new work; (2) leanfleet
> token-efficiency (epic hk-itoc: restart-earlier, idle-restart, model-tiering); (3) keeper
> restart-earlier (hk-8hr1 — NOTE now CLOSED as no-op, band already on main); (4) per-crew
> model-fit; (5) codex re-prove→soak (ChatGPT-billed = throughput OFF the Anthropic budget).
> Other lanes MAY continue but yield ties to token-opt. Apply going forward.

| crew | epic_id / scope | lane (plain English) | queue | model |
|---|---|---|---|---|
| paul | hk-var9b / codename:wake-economy | **WATCH** (captain wake-burn cut) — TOP PRIORITY per token-opt directive. Design revised for operator's 4 rulings + the A/B follow-ups (WE1 = COUPLED watch-session-standup + sender-redirect MVP so captain never goes blind; scheduled-send = NATIVE comms-send action, bash-wrapper dropped). Critic gate on revised sequencing → build WE1-8. hk-8hr1 CLOSED (reconcile no-op, band already warn200K/act215K/force240K on main). hk-drygf governor HELD | paul-logmine | opus |
| jamis | hk-e3fy / daemon run_failed auto-reset | daemon: run_failed STILL strands bead in_progress on uncovered terminal paths (reviewer-pane-hang/verdict-silence) — auto-reset is partial post-c7062bb7; writing the real fix. `internal/daemon` | jamis-sh | — |
| gurney | hk-tef1s / codename:leanfleet | TOKEN-OPT (re-tasked from idle 2026-06-24): Mission front-matter `model:` field — make clean-drain crews explicitly Sonnet. Parser already exists (crewstart.go:494); WORK = markdown edits to `.harmonik/crew/missions/*.md`. hk-vfeeo (spawn-cap refuse-oversubscription) LANDED+CLOSED 7a8db433; live-resize half captured as backlog hk-omvan | gurney-q | sonnet |
| leto | epic hk-0639 codename:codex / Pi-design-on-hold | HELD for the Pi universal-model-gateway DESIGN crew (greenlit-pending-operator-go; brief plans/2026-06-23-pi-openrouter-harness). Codex soak CONCLUSIVE all shapes + ChatGPT-billing proven; no file-disjoint bead left to feed codex (gurney took the one available) | leto-ev | opus |

- **Fleet = daemon + captain + 4 work-crews + admiral (operator-engaged) + ctx-watchdog + ops-monitor.**
- Lanes file-disjoint: paul=`internal/keeper` + watch design, jamis=`internal/daemon`, gurney=`.harmonik/crew/missions/*.md` (markdown), leto=codex harness (LOCAL, held). HARD GUARD: gurney's daemon ledger follow-up hk-zgt4u is internal/daemon = collides with jamis — do NOT staff until jamis hk-e3fy lands.

### 2026-06-24 closures (reconciled this session)
- **hk-98jju (supervisor-revive epic) CLOSED:** daemon/supervisor auto-revive fix complete + live (watchdog-only mode decouples watchdog from the dropped flywheel; 5f18ba5e+d2b4f020; supervisor up). Operator DROPPED the flywheel keep-vs-remove scope → hk-zv6j3 CLOSED. Governor hk-drygf stays PARKED; decouple-idea captured as hk-qaqtl (low-pri, NOT dispatched). FLYWHEEL = dead cognition-loop, do NOT investigate; distinct from the live Pi gateway harness.

### 2026-06-24 admiral directive resolutions (operator-authorized)
- **Remote worker re-enable — APPROVED**, GATED on hk-92ih3 (paul) landing+verified. Then flip `workers.yaml` enabled:true + daemon restart + fix the stale re-enable comment (real gate = hk-scndr DONE + hk-92ih3, NOT hk-9a7rt) + raise spawn cap in that restart. Prove ONE remote DOT run on gb-mbp before scaling remote. Local stays 4.
- **Codex — UNPAUSED** (the "not daemon-runnable" framing was stale; ran e2e via daemon 2026-06-17). One LOCAL re-canary first (hk-n05u2 via leto). Codex bills ChatGPT, LOCAL-only (not on gb-mbp).
- **Supervisor / hk-drygf:** epic hk-98jju investigates (i) why `harmonik supervise` (the daemon auto-revive watchdog) dies on launch — REAL reliability gap, daemon has NO auto-revive now — and (ii) flywheel/sentinel keep-vs-remove. hk-drygf (governor-liveness) FOLDED in + stays HELD; do NOT apply FIX-A/FIX-B.
- **Supervisor-down alert spam:** mute the every-5m CRITICAL page only (hk-xr46t); the underlying check STAYS.

## Recently CLOSED / COMPLETED (2026-06-20 burst)

These epics are fully landed — moved out of the in-progress table. Verify against `br show <epic>` only if a regression is suspected.

| Initiative (plain English) | Epic / codename | Status |
|---|---|---|
| keeper-redesign — per-project config, zero hardcoded thresholds, actionable-warn self-restart handshake, hold/release co-working override, configurable hard-ceiling backstop, durable tmux↔session-id identity | `hk-gffc` | ✅ COMPLETE — ALL remaining beads landed (the earlier "operator-gated remaining" claim is stale; zero open) |
| captain-economy — slim captain boot (~81k→~55-60k tokens), Sonnet ops-monitor offload, comms `--wake` fix, per-crew `--model` | `hk-unjy` (CE1/CE4/CE5/CE6) | ✅ COMPLETE |
| tmux-session-organization — unified `harmonik-<hash>-*` namespace, agent+keeper window-nesting, window-granular restart, `supervise reap` | `hk-0v9e` | ✅ COMPLETE |
| doc-instruction-audit — three-kinds model (docs / skills incl. new `orchestrator-rules` / state tiers), AGENTS.md→router, new `harmonik sync-assets` + supervisor skew-notify | `hk-vk7b` | ✅ COMPLETE (Phase A+B); deferred follow-up `hk-fozq` (P3 supervisor auto-apply) |
| easy-start native launchers — native Go `harmonik start captain\|crew <name>`; bash `captain-launch.sh` retired; `captain respawn` self-heal; shared `agentlaunch` helper | `hk-kbjl`/`hk-bcd0`/`hk-sn4n`/`hk-z1rj` | ✅ CORE SHIPPED — integration paths (tmux-takes-effect, live `/clear`-injection) untested; see live-validation lane |
| remote-control session prefix — per-project RC session-label prefix (`hk-captain`) | `bf7d51f8` | ✅ CORE LANDED — 4 tail beads still open: `hk-dhe6` (epic), `hk-w8ex` (CLI parity test), `hk-25bg` (live 2-project validation), `hk-f4w7` (migration prompt) |

## LIVE FLEET STATE (snapshot 2026-06-20 ~22:30 — verify at boot)

> ⚠️ **STALE (2026-06-20) — SUPERSEDED. As of 2026-06-24: daemon UP & healthy (concurrency=4,
> spawn cap 16), NOT wedged; the `chani` session and the disk-90% firefight are over; the only
> live infra flag is `supervisor-down` (no daemon auto-revive) owned by jamis hk-f2j0o. Trust the
> active_lanes table + 2026-06-24 admiral directives above, not this block.**
>
> **Daemon is UP but WEDGED — clear before staffing crews.**
> - Main queue is `paused-by-failure` on `hk-tagp` (old remote e2e). Submit fresh named queues per lane; do NOT resume main.
> - Disk at **90% (≈19 GiB free; daemon paused dispatch at 2.5 GiB earlier and self-GC'd)**. `.claude/worktrees` = 3.1 GiB across **88 registered git worktrees** — worktree GC (lane L2) is a real prerequisite for reliable spawns.
> - Daemon's `br show` is erroring (exit 3) on a batch of beads — daemon-side; `br` from repo root is healthy (86 ready).
>
> **Other agents working NOW (coordinate, do NOT collide):**
> - **keeper polish** — an agent has uncommitted edits to `cmd/harmonik/captain.go` + `keeper_enable_doctor_cmd.go` and just landed the keeper "no-defaults" commit. → overlaps lanes **L7, L9**.
> - **remote-substrate e2e + disk firefighting** — the "chani" session owns the running daemon. → owns lane **L3** entirely; shares the daemon with **L1/L2** (merge/GC timing).

## Dispatch-ready lanes — captain → crew staffing (re-verified 2026-06-20)

One crew per lane (file-disjoint). Buckets: TEST=live-validate shipped code · BUILD=new code · BUGFIX · HYGIENE. Verify IDs at boot.

| # | Lane (plain English) | Bucket | Value | Epic / key beads | Ready? | Overlap |
|---|---|---|---|---|---|---|
| **L1** | **Daemon-reliability bugfixes** — kill the false-positives/strands that corrupt the orchestration signal | BUGFIX | **HIGH** | epic `hk-sfvc`; `hk-sj6a` (DOT review-phase freezes ~40m, no self-recover), `hk-53p3` (promote strands bead in_progress), `hk-ijtw` (review-gate false-pos sticks forever), `hk-gu3v` (crew-stale false-pos on active agent), `hk-vx1i`≡`hk-5zmz` (no_progress false-fire — DEDUP), `hk-g9zz` (rebase aborts on dirty worktree), `hk-guez` (cache-reaper races merge-build) | ✅ | merge-timing w/ chani |
| **L2** | **Disk / worktree GC** — reclaim space; 88 worktrees, 90% disk | HYGIENE | **HIGH** | `hk-ldzp` | ✅ | ⚠️ chani owns daemon — coordinate so GC doesn't race in-flight runs |
| **L3** | **Remote-substrate e2e proofs** — live-prove the remote path end-to-end | TEST | **HIGH** | `hk-4lrj` (DOT triple-review remote run lands on main — the only unproven variant), `hk-tagp`/`hk-h106` (agent_ready-over-tunnel proofs), 6× worktree-create proofs (`hk-icdz`/`3zij`/`d2z1`/`tzfw`/`xbpm`/`k0pz`), `hk-tyyy` (auto-scp binary), `hk-vjsv` (commit_gate stale-window loop) | ✅ | ⚠️ **IN-FLIGHT (chani)** — hand over / coordinate, don't re-staff |
| **L4** | **Fleet-state model + ZFC wind-down** — demote drain-oracle to a fact-tool, decisions back to captain | BUILD | **HIGH** | epic `hk-up4b` (supersedes `hk-rl4b`); `hk-pfr4` (GatherDrainFacts), `hk-kj7d` (delete auto-park tick), `hk-zqb3` (veto-on-execute), `hk-9mdz` (spec reword); **P2 gated behind design keystone `hk-9fvk`** → then `hk-8lne` (spec), `hk-gv04`/`hk-jay1` (aggregator + context-into-state) | P1 ✅ / P2 blocked on `hk-9fvk` | none |
| **L5** | **Flywheel wiring + validation** — wire `sentinel.Evaluate` into the tick, OBSERVE-only first, then staged ACT | BUILD+TEST | MED | epic `hk-0oca` (~28 beads); keystone `hk-y9fn` (FW1 config adapter) → FW2-6, AC1-5, BT1-6 tests, CD1-4 deploy, `hk-m8zqv` (4h soak) | ✅ keystone | none |
| **L6** | **rc-prefix tail** — finish CLI test, migration prompt, live 2-project validation | TEST+BUILD | MED | epic `hk-dhe6`; `hk-w8ex`, `hk-25bg`, `hk-f4w7`, `hk-kqra` | ✅ | none |
| **L7** | **Easy-start launcher validation** — integration tests + swap remaining doc examples to native | TEST | MED | epic `hk-q1ll`; `hk-dyqy` (doc swap) | ✅ | ⚠️ keeper tranche touches `captain.go` (in-flight keeper edits) |
| **L8** | **Codex-harness soak** — exercise the Codex implementer + verify ChatGPT billing each run | TEST | MED | epic `hk-0639`; `hk-cr31`/`y3g5`/`84c2`/`4cop`/`ngnv` | ⛔ operator-PAUSED 2026-06-19 | none |
| **L9** | **Crew keeper-gauge wiring** — wire the context gauge for crews on the live deploy | BUILD | MED | `hk-tt9q` | ✅ | ⚠️ keeper subsystem (in-flight) |
| **L10** | **Kerf binding + triage hygiene** — wire bead_filters, then `triage --ack` to restore drift detection | HYGIENE | MED | 63 untriaged; ~22 unwired works; baseline ~397 behind | ✅ | none |
| **L11** | **Doc-audit / security follow-ups** — close the tail; scrub plaintext API key | HYGIENE | LOW-MED | `hk-fozq` (asset-sync auto-apply); **P0: scrub any `*.env.txt` API key**; retention pruning | ✅ | none |
| **L12** | **Flaky-test stabilization** — deterministic-sort digest; fix watchdog regression; CI timeouts | BUGFIX | LOW-MED | `hk-z8fp` (Manifest.Digest map-order), `hk-7m39` (3 `TestDaemonWatchdog_*` RED on main), `hk-963f` (Tier-3 CI timeouts) | ✅ | none |
| **L13** | **leanfleet / token-burn efficiency** — restart-earlier, idle-restart, model-tiering | BUILD | LOW-MED | epic `hk-itoc`, `hk-bsdr` (analysis) | ✅ (research-heavy; partly superseded by L4) | none |
| **L14** | **Tooling capture (passive)** — aggregate emergent tooling patterns | HYGIENE | LOW | `hk-nlhys` — **CAPTURE-ONLY, do NOT formalize/dispatch** | n/a | none |

### START FIRST
**L1 (daemon-reliability, `hk-sfvc`)** — highest leverage: every bead is a false-positive/strand that corrupts the signal the whole fleet runs on; fixing them makes every other lane's dispatch trustworthy. All ready, no operator decision, distinct from the two in-flight areas (it's `internal/daemon/dot*` + reconcile). Dedup `hk-vx1i`≡`hk-5zmz`; coordinate merge timing with chani.
Runner-up: **L2 disk GC** — urgent (90% disk) but must coordinate with chani.

### DO NOT DISPATCH YET
- `hk-cyec` (fleet-state P4 desired-state reconcile loop) — operator HOLD.
- **L4 P2 code** (`hk-gv04`/`hk-jay1`/`hk-8lne`) — blocked behind design keystone `hk-9fvk`; operator said "nail the model first."
- `hk-1nwt` (paul ideation placeholder), `hk-nlhys` (L14) — capture-only / no-dispatch by design.
- **L3 (remote-substrate)** & **L7/L9 (keeper)** — owned by in-flight agents; coordinate before staffing.
- `hk-77x1` — Claude Code core RC regression, NOT harmonik-fixable; tracking note only.
- `hk-tagp` / main queue — paused-by-failure; do NOT re-dispatch to main.

### RESOLVED since last reassessment (do NOT re-open)
- `hk-538l` (remote review-loop e2e `agent_ready`) — **CLOSED**, proven end-to-end (`15ca1eb3`); only the DOT-mode variant (`hk-4lrj`, in L3) remains.
- `hk-5da7` / keeper `89852bb3` restart-now fix — **CLOSED on main** (`4128d760`). The earlier "unmerged, needs ACK verify + pct-vs-band operator decision" is RESOLVED; the active keeper-polish edits are separate follow-on work.

## Active operator directives (dated)

> **EXPIRY MECHANISM (admiral-framework, 2026-06-25 — ships now, independent of any ranker lock):**
> EVERY dated directive in this file MUST carry `expires:` AND an owner. **ON EXPIRY the DEFAULT is
> LAPSE → revert to the standing autonomous posture — NEVER a hold.** The admiral audit OWNS flagging
> an expired-but-present directive and either re-confirming with the operator or striking it. An
> expired block left in place is a FINDING (this is exactly how the 2026-06-19 scale-out block lapsed
> into a silent lean-park). A dated directive with no matching `direction-log.md` entry is also a
> FINDING. See `.harmonik/context/AGENTS.md` (forced-write/forced-read) + orchestrator-rules §Autonomy.

> set: 2026-06-19 · expires: ~2026-06-22 (3-day scale-out push — re-confirm or expire after the window)
> STANDING for EVERY captain across ALL restarts within the window. These OVERRIDE any
> stale "lean park / one-at-a-time / operator away" posture in a handoff. On conflict, THESE win.
>
> NOTE (2026-06-20): a TESTING / live-validation phase is IMMINENT ("where are we at?"). Much of the
> 2026-06-20 burst shipped as code but was never exercised live. Expect the scale-out directive to
> yield to a live-validation posture when the operator opens that window (operator decision §3.1 of the
> state-reassessment plan). Until then the scale-out directives below remain in force.

- **set:2026-06-19 expires:~2026-06-22** — ONE-AT-A-TIME IS RETIRED. Run multiple lanes/crews in parallel (file-disjoint). The prior "work one item at a time" directive is NO LONGER in effect.
- **set:2026-06-19 expires:~2026-06-22** — Scale OUT across many sessions over ~3 days. Lots of context budget is available, but do NOT run too much at once — stage lanes; don't blast the whole fleet up at once.
- **set:2026-06-19 expires:~2026-06-22** — The daemon MUST dispatch EVERY bead through DOT — the SONNET triple-review graph (repo-root workflow.dot == sonnet-triple-review). NEVER the opus DOT. NEVER single/no-review mode. Verify via run_started workflow_mode (STARTUP 5c grep).
- **set:2026-06-19 expires:~2026-06-22** — Captain ORCHESTRATES; it does NOT do the work and does NOT micromanage. Allocate + direct; crews own their own tasks. Once the fleet is running, the captain is mostly QUIET — occasionally check crews; use FRESH-CONTEXT sub-agents to verify crew work.
- **set:2026-06-19 expires:~2026-06-22** — Captain ensures PROCESS, not task content: (1) double-check crew work, (2) ensure reviewers are ACTUALLY used, (3) ensure work is ACTUALLY TESTED before integration. Set a ~30-min check-in loop; do not babysit between ticks.
- **set:2026-06-19 expires:~2026-06-22** — A DAEMON issue takes PRECEDENCE over everything else.
- **set:2026-06-19 expires:~2026-06-22** — EVERY session (captain + crews + flywheel + watchdog) MUST stay under 300k tokens of context. The built-in keeper has reliability issues. Run an INDEPENDENT Sonnet context-watchdog: a 30-min loop that idles between ticks, checks every session's context, and FORCE-restarts any that exceed the cap.
- **set:2026-06-19 expires:~2026-06-22** — Internet is FLAKY. The agent<->Anthropic API loop works, but other internet calls (WebFetch, gh, package downloads, SSH to the remote box) may fail. Propagate this caveat to EVERY crew, especially remote-substrate.

> Staffing order is in the dispatch table above (START FIRST → L1, runner-up L2). The remaining
> live-validation gaps (remote-node telemetry live-run, fleet auto-sleep round-trip, easy-start
> integration tests) are folded into lanes L3/L4/L7. The "remote-substrate live-validation first"
> ordering is RELAXED now that `hk-538l` is closed — L1 daemon-reliability leads.

## Operational caveats (still live)

- **STRANDED in_progress/open, pending `hk-53p3`** (do NOT raw `br close` — would reverse the locked beads-own-transitions decision): `hk-gu3v` (fix on main, in_progress) + `hk-nlio` (prior promote-salvage, open). Both auto-close once `hk-53p3` lands and `harmonik reconcile` runs.
- `hk-rty1` (P1): stranded in_progress one-liner (default→triple-review); needs split/reset to unstick.

## paused

- **codex (hk-0639)** — ⚠️ UNPAUSED 2026-06-24 (admiral/operator). LOCAL re-canary first (hk-n05u2 via leto), report before any soak. Codex bills ChatGPT, LOCAL-only.
- **gh-bugs** — only do beads that ALREADY EXIST and do NOT need GitHub (no gh access / flaky internet).
