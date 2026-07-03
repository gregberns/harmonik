# Watch-officer design — critic findings (paul, 2026-06-24)

Design under review: plans/2026-06-23-captain-wake-economy/design.md
Gate: reviewers WITH critics before any build (operator hard-req).

## Critic 1 — wake-reduction efficacy (adversarial) [agent aacabeaa] — VERDICT: REVISE
Core claim sound as intent, but NOT buildable as written: it presumes captain-wake triggers live in
skill prose; the two biggest wake sources are in daemon Go + a bash script the design leaves pointed at
the captain. As written → maybe 30-50% reduction, not ~90%.

### BLOCKERS
- **B1 — daemon wakes captain on EVERY agent_message{to=captain}.** internal/daemon/quiesce.go:638-654
  (handleAgentMessage) fires wakeSignal{captainWake:true} whenever payload.To=="captain" — ignores --wake
  AND sender. Pure Go, not skill prose. BUT: only nudges a PARKED captain (a.sleeping map, executeWake
  :661-667). A captain on an armed `comms recv --follow` Monitor is woken by DELIVERY to its filtered
  subscribe stream instead. Design names neither mechanism. FIX: per-sender, state which wake mechanism is
  disarmed + change senders at source; consider a sender-allowlist/--wake-bit on handleAgentMessage, or
  explicitly document the interlock only matters for parked captains.
- **B2 — ops-monitor still sends --to captain every 5m (biggest churn slice); design doesn't redirect it.**
  scripts/ops-monitor-check.sh:951-958 send_comms hardcodes --to captain (IMMEDIATE :982, ops-CRITICAL
  :999, DIGEST :1003); schedule every@5m. §6.2's filter ("operator+watch-officer only") would DROP ops
  IMMEDIATE alerts (daemon-down/paused-queue) OR leak ops churn. FIX: WE bead to repoint send_comms --to
  to watch-officer (config-driven target per O3/R1); watch officer re-escalates ops IMMEDIATE/CRITICAL.
- **B3 — captain filter can't express {operator ∪ watch-officer} union; O5 at risk.** comms recv already
  takes --from AND --topic (comms.go:1484-1489) — "singular --from gap" (WE5) is overstated. A single
  --from can't express the union; --from operator drops watch-officer, --from watch-officer drops operator,
  --topic escalation drops untopiced operator msgs. Operator msgs may carry NO fixed filterable handle (O5
  = "directly, never intercepted"). FIX: verify how operator msgs are addressed/topiced TODAY before
  picking; union expressible only if repeatable --from + operator always --from operator, OR shared
  reserved topic. Load-bearing for O5.

### MAJORS
- **M1 — epic_completed triple-wake.** daemon already wakes on epic_completed (quiesce.go:627-636) AND
  captain subscribes to it (STARTUP.md:506). Listing it IMMEDIATE at watch-officer = up to triple wake +
  triage latency. FIX: make epic_completed LEDGER-ONLY at watch-officer OR remove from captain's direct
  subscribe and route solely through watch-officer. Pick one.
- **M2 — BATCHED 30m digest recreates a poll (violates O2).** A 30m timed comms send to the captain = 48
  wakes/day push = poll loop by another name. Same for §5 heartbeat + §7 hourly liveness — each beat that
  reaches the captain's stream is a wake; heartbeat interval never defined. FIX: digest must be PULL not
  push (captain reads .harmonik/watch-officer/latest.json on its own idle), OR fold ONLY into next genuine
  IMMEDIATE (make that the ONLY behavior). Define heartbeat interval + justify it's not churn.
- **M3 — honest quantification.** 12-min tick (~120/day) already CE4-cheap (removing /loop removes
  invocation, savings smaller than raw count); comms churn (120-250) dominated by ops-monitor (B2,
  unredirected) + crew posts (fragile prose redirect, B1); epic_completed (5-10) must stay (M1 risks
  tripling). ~90% only if B2+M2 fixed. FIX: restate §10 as a per-source ledger w/ residual after CE4.
- **M4 — supervisor asset-skew sender unaddressed.** cmd/harmonik/supervise/assetskew.go:79-86 sends
  --from supervisor --to captain --topic status at boot. Low volume but a --to captain sender §6/§2 misses.
  Redirect or classify as allowed-direct.

### MINORS
- **m1 — watch-officer-as-crew:** idle queue may trip idle-restart/crew-stale. ops-monitor-check.sh:601
  excludes {captain,ops-monitor,ctx-watchdog,daemon,operator} from crew-stale but NOT watch-officer —
  WE7/WE8 must add watch-officer to that NON_CREW exclusion or it self-alerts as stale.
- **m2 — subscription_gap re-scan:** 256-slot drop-oldest could drop a burst of run_failed; re-scan
  recovers but adds latency to IMMEDIATE. State worst-case escalation latency under backpressure.
- **m3 — comms-send command-wrapper quoting:** `bash -c 'harmonik comms send ...'` in a JSON schedule is a
  quoting-bug magnet (cf. SSHRunner #{pane_id} truncation, wake pane-hash bugs). WE6 must include a quoting test.

### SOLID (keep)
- events.jsonl + forward cursor ledger (§3); reuse harmonik schedule (§7, gap real); §2 MAY/MUST boundary
  (O7); wake-bug-fix prereq genuinely landed (09c112c5). Note SKILL.md:606 flags pane-nudge wake is
  best-effort → don't rely solely on --wake; daemon interlock (B1) is the more reliable parked-captain wake.

BOTTOM LINE: do not build until B1-B3 resolved + M2 (pull-not-push) + B2 (ops redirect). Re-review after revision.

## Critic 2 — reuse-claim correctness [agent a0909cc] — VERDICT: APPROVE (3 mandatory non-structural revisions)
All "reuse X" claims CONFIRMED vs code. No BLOCK, no refuted structural claims. Riskiest concern resolves SAFE.
- §7 schedule primitive CONFIRMED incl. command-wrapper: daemon execs argv directly (no shell mangle), cmd.Dir=projectDir (scheduletick.go:233), PATH via os.Environ, `--` verbatim (schedule.go:155-158). Fire-and-forget (inner-send failure only on child stderr). action kinds exactly {command,spawn-crew} — comms-send GAP accurate.
- §6 comms filter gap CONFIRMED: `comms recv --from` singular (comms.go:1212-1215), recv has NO --to (identity=--agent); subscribe --from/--to/--topic apply to agent_message ONLY (subscribe.go:575-577,435-440), other types narrow by --types only. Repeatable --from fix = small CLI-local fan-out (recv per sender, dedup on event_id), no protocol change.
- §3 ledger CONFIRMED: events.jsonl + UUIDv7 (core/event.go); ScanAfter + `comms log --since` daemon-free; recv cursor contamination REAL (commscursor.go) → WO should use read-pure ScanAfter/comms log (advances NO cursor) + own watermark, needs no recv cursor at all.
- §5 component-liveness CONFIRMED SHIPPED on main (ops-monitor-check.sh: captain-liveness :197-206,677-688; tiers :849-854,961-1000; 5-min cooldown :99-102; per-crew keeper coverage :286,644-657 uses `ps` not pgrep on macOS).
- §8 crew launch/keepalive CONFIRMED SAFE — **the idle WO is NOT torn down by ANY path:** quiesce never kills sessions + not empty-queue-triggered (quiesce.go:430,775); orphan-sweep is tmux/PID-based, queue-depth not an input, exempts live crews (orphansweep.go:592-659,615 ↔ tmuxsubstrate.go:1383), boot-only; keeper has no idle teardown, cycle preserves session_id. model:sonnet injection CONFIRMED (crewstart.go:244,252→crewlaunchspec.go:120-122).
- §6/WE4 captain skill edit lines ALL 6 ranges ACCURATE; embedded-asset sync (TestSkillAssetsEmbedInSync, init_skills_sync_test.go:26) correctly required.
### REQUIRED BEFORE BUILD (critic 2):
1. Fix "~79 event types" → ~163 (eventtype.go; the :6 comment is itself stale). Substantive "contains every type WO needs" holds.
2. Re-scope WE7: adding a critical component is a ~4-SITE hand-edit (probe block + present/down derivation + signal-append + checks-map entry + CRITICAL_PREFIXES tuple), NOT a list-append.
3. Assign owners for 2 continuity caveats in §8/WE8: (a) HIGH keeper-for-crews statusLine gauge ships OFF on live deploy (keeper KNOWN DRIFT) → WE8 must require `keeper enable --yes-destructive` + verify `keeper doctor`, OR mandate token-light wake-tasks; (b) MED no in-daemon crew auto-respawn (crewstart.go:281-284) → after host reboot/kill nothing revives WO; name who respawns (likely ties to §5: ops-monitor escalates → captain respawns — state it).

## Critic 3 — constraints + restart-survival [agent a6f3848] — VERDICT: REVISE
Reuse story sound + durability crux (O6) HOLDS, but 1 blocker + 3 majors must be closed in the spec.
- **#2 BLOCKER — hardcoded thresholds (C4, = the HELD hk-drygf mistake).** Generic `schedule add` path is clean (parseInterval fails loud, no fallback, clock.go:14-27). BUT the design's cited precedents ops-monitor + ctx-watchdog bake `Interval:"5m"` as a GO LITERAL auto-registered on daemon startup w/ no override (opsmonitor_schedule.go:37, ctx_watchdog_schedule.go:46; watchdog config has only Enabled). If WE6/WE7 follow that precedent the 30m/1h become Go literals = exact hk-drygf error. FIX: every WO interval+target resolves via ResolveKeeperConfig-style config-or-fail-loud (keys e.g. watch_officer.digest_interval / .liveness_interval; absent→fail loud naming key + --example), register via `schedule add` from operator-edited config NOT a daemon ensureXSchedule Go-literal helper. Also: ops-monitor's own 5m/STALE=150/CAPTAIN_ABSENT=600/IMMEDIATE_COOLDOWN=1800 are pre-existing hardcodes — WE7 must not deepen that debt.
- **#3 MAJOR — O5 operator-direct: delivery airtight, FILTER is the failure mode.** Wake layer safe (quiesce.go:640-653 wakes captain on any to==captain). Delivery safe (per-agent cursor; WO's recv can't consume to=captain mail; MatchAgentMessage skips it). BUT captain's NEW filtered recv can silently DROP operator: --from singular, MatchAgentMessage (agent_message.go:33-44) exact case-sensitive ==, --topic filter drops untopiced (from=="") msgs. Captain scoped --from{operator,watch-officer} drops operator if from-value not byte-identical "operator" or a topic filter is on. FIX: enforce O5 at DELIVERY not FILTER — captain recv stays UNFILTERED (comms recv --agent captain, no --from/--topic), route CLIENT-SIDE in monitor logic (route on from/topic, dedupe event_id). Makes operator-drop structurally impossible + removes dependence on WE5 repeatable --from. If server-filter insisted for churn: never filter DIRECTED (to==captain) mail, only broadcast/status noise. Rewrite §6 item 2.
- **#4 MAJOR — O6 survives restart: GOOD, removing /loop from repo STARTUP.md PERSISTS.** Running captain reads REPO path .claude/skills/captain/STARTUP.md (start captain→ensureBootAssets→provisionSkills force=false SKIPS existing files, never clobbers; init_cmd.go:629). No launch/restart/wake path re-supplies stale embed. Past "didn't stick" = the instruction TEXT still said re-arm, not embed-clobber. Removing the text is the durable lever. TWO gaps: (a) editing repo STARTUP.md trips TestSkillAssetsEmbedInSync (init_skills_sync_test.go) → WE4 done-check MUST be `cp`-to-embed + `go test ./cmd/harmonik/`; `init --force` is the ONLY clobber path, never run vs a live captain. (b) /loop 12m re-arm is in 3+ PLACES: STARTUP.md:513-534, :489-491, WAKE step3 :722-723, SKILL.md:183-189/:399-400 → WE4 done-check MUST be `grep=0 "/loop 12m"` across STARTUP.md+SKILL.md (repo+embed=4 files) or a stale re-arm line silently decays O6.
- **#5 MAJOR — contra-evidence: WO is a NEW SPOF (the single biggest risk).** (a) keeper may NOT auto-launch for WO (memory reference_crew_start_no_auto_keeper_watcher) → WO keeper-less + silently dies → WE8 MUST gate launch on `keeper doctor watch-officer` (+ keeper enable --yes-destructive). (b) ALIVE-BUT-STALLED WO passes the §5 liveness check while buffering every crew escalation → captain starved silently. FIX: §5 WO-down detection must use ops-monitor's DUAL probe (comms-absence AND no-tmux) like it does for the captain, NOT last_seen alone, AND add a WO ESCALATION-CURSOR-ADVANCEMENT check (WO healthy only if its escalation cursor advances, not merely alive). (c) new latency tier: some IMMEDIATEs (daemon-down, crew-kill) should keep the existing daemon/ops-monitor→captain DIRECT path, BYPASSING the WO.
- **#1 MINOR — poll loop mostly clean.** WO loop = subscribe --follow (event) + re-scan on subscription_gap (gap-driven) OK; timers delegated to harmonik schedule (daemon 2s poll, not LLM loop) = correct long-heartbeat≠poll line. SMELL: §2 "WO is primary reader of latest.json" — if WO poll-reads it on a timer that's a poll loop. FIX: WO reacts to ops-monitor's existing [IMMEDIATE]/[DIGEST] comms event, reads latest.json only ON RECEIPT, never on its own timer. State in WE2/WE3.
- SINGLE BIGGEST RISK: WO = silent SPOF between crews and captain — alive-but-stalled it passes liveness while buffering escalations, starving the captain; design has no cursor-advancement check to catch it.

## CONSENSUS (all 3 critics in) → DESIGN VERDICT: REVISE (architecturally sound, not buildable as written)
Convergent must-fixes for the revision pass (BEFORE re-review + before any build):
1. WAKE MECHANISM IS IN CODE NOT PROSE (C1-B1/B2): redirect ops-monitor send_comms --to → watch-officer (config-driven); consciously handle the daemon QuiesceArbiter parked-captain interlock; enumerate+repoint ALL --to captain senders (ops-monitor, supervisor assetskew, crews).
2. O5 AT DELIVERY NOT FILTER (C1-B3 + C3-#3 CONVERGE): captain recv stays unfiltered, route client-side OR never filter directed mail — kills operator-drop + the {operator∪watch-officer} union at once.
3. NO PUSH-POLL / NO HARDCODED INTERVALS (C1-M2 + C3-#2 + C3-#1 CONVERGE): batched digest = PULL (captain reads latest.json on own idle) or fold-into-next-IMMEDIATE only; intervals config-or-fail-loud via `schedule add` from operator config, never Go literals.
4. SPOF HARDENING (C3-#5): dual-probe + escalation-cursor-advancement WO liveness; keeper doctor gate (WE8); keep daemon/ops-monitor→captain DIRECT for most-urgent IMMEDIATEs (epic_completed LEDGER-ONLY at WO per C1-M1 to avoid triple-wake).
5. WE4 DONE-CHECKS (C3-#4): grep=0 "/loop 12m" across STARTUP.md+SKILL.md (repo+embed=4 files) + embed-sync `go test ./cmd/harmonik/`.
6. C2 doc fixes: event count ~163 not ~79; WE7 = ~4-site hand-edit not list-append.
7. HONEST QUANTIFICATION (C1-M3): restate §10 per-source ledger w/ residual after CE4 (~90% only if ops redirect + pull-digest land).
NEXT SESSION: revise design.md per 1-7 → report captain + surface 4 operator decisions (name; comms-filter approach [leaning unfiltered+client-route per #2]; comms-send schedule-action [command-wrapper + quoting test]; default intervals [config-or-fail-loud]). Build WE1-8 via paul-logmine DOT ONLY after a clean re-review.
## Critic 3 — constraints + restart-survival [agent a6f3848] — PENDING (re-launch if lost)

---

## ROUND-2 critic pass (post-revision, on the changed sections) — paul, 2026-06-24

Revised design.md (7 consensus fixes applied) re-reviewed by 3 fresh adversarial critics, each code-grounded
on a fix-slice. RESULT: gate CLEAN — no blockers from any critic; architecture confirmed; only completeness
majors, all closed with each critic's OWN prescribed fix.

- **Critic A (code-redirect + efficacy) — REVISE, no blockers, 2 majors (BOTH APPLIED):**
  - M1 ops-monitor is a message-PARTITION not a target swap: send_comms() hardcodes --to captain and batches
    all 8 immediate signals into ONE message (ops-monitor-check.sh:951-959,966-982). Keeping daemon/supervisor/
    paused DIRECT while routing the rest to the WO requires splitting into TWO sends to TWO targets. §4 SPOF
    bypass DEPENDS on building this partition. → §6.1 item 1 + WE7 reframed; we COMMIT to the partition.
  - M2 crew status redirect omits an embedded asset: the crews' mandatory feed is crew-launch/SKILL.md:359,378
    (--to <captain_name> --topic status), embedded mirror cmd/harmonik/assets/skills/crew-launch/SKILL.md;
    editing trips TestSkillAssetsEmbedInSync; WE4 done-check covered only the 4 captain files. → §6.1 item 3 +
    WE7 now carry crew-launch/SKILL.md (repo+embed) redirect + its own cp+go-test+grep done-check.
  - CONFIRMED against code: daemon parked-captain interlock §6.2 (quiesce.go:640-667 wakes only sessions in
    a.sleeping; armed-recv captain woken by delivery), epic_completed ledger-only avoids triple-wake
    (quiesce.go:627-636 + STARTUP.md:506), O5-at-delivery §6.3, honest §10 (12/hr×24=120 ✓), 164 EventType
    consts (eventtype.go; :6 comment stale at ~79). minor: §10 "folded above" clarity → clause added.
- **Critic B (constraints + SPOF + config) — REVISE, no blockers, 1 major + 1 minor (BOTH APPLIED):**
  - MAJOR WE4 done-check too narrow: grep=0 "/loop 12m" passes while bare-/loop re-arm prose survives at
    STARTUP.md:490 ("the /loop health tick must be re-armed after /clear" — ACTIVE) + :513 (header). →
    broadened §6.4 + WE4 done-check to grep=0 "/loop" across the 4 files.
  - minor cursor-advance seam unnamed: reuse ops-monitor state.json prev_* consecutive-miss machinery
    (:121-155,255-278). → §5 + WE7 now name the seam.
  - CONFIRMED: Go-literal precedent real (opsmonitor_schedule.go:37, ctx_watchdog_schedule.go ~:46 Interval:"5m");
    parseInterval fails loud no fallback (internal/schedule/clock.go:14-27); ResolveKeeperConfig is a real
    copyable config-or-fail-loud (resolve_keeper_config.go:107-128,282-353); captain dual-probe exists
    (:197-206,677-688); NON_CREW :601 lacks watch-officer (single set-literal add covers :605 + :648); keeper
    enable --yes-destructive + keeper doctor is the real surface (keeper_cmd.go:965-988); /loop 12m = 4/3/4/3
    across exactly the 4 files; TestSkillAssetsEmbedInSync real + fires on repo-only edit.
- **Critic C (O5-delivery + reuse) — APPROVE, nothing to raise:**
  - Tried + FAILED to refute FIX #2: captain ALREADY runs the exact unfiltered `comms recv --agent captain
    --follow --json` (STARTUP.md:509) the design specifies → FIX #2 REMOVES the draft's filter, adds no
    machinery; operator-drop structurally impossible (delivery keys on to==agent, from/topic wildcards when
    unset: commsrecvhandler_nnwaa.go:138, comms.go:1304-1312, agent_message.go:33-44). WE5 rescope coherent.
  - ALL reuse claims hold verbatim post-rewrite: §3 read-pure ScanAfter (comms.go:567,726,661 vs recv-cursor
    commscursor.go); §7 schedule exec-direct argv + cmd.Dir=projectDir, action kinds {command,spawn-crew}
    only (scheduletick.go:232-233, schedule.go:155-161, types.go:40-42); §8 idle-WO-safe (quiesce.go:430,775;
    orphansweep.go:592-659; crewstart.go:244,252→crewlaunchspec.go:120-122; no in-daemon respawn :281-284).
  - nit (applied): §7 clock.go path → internal/schedule/clock.go.

### ROUND-2 CONSENSUS → DESIGN GATE: CLEAN (architecturally sound + buildable as written)
All round-1 and round-2 findings applied. No re-review item outstanding. The two REVISE verdicts were
completeness gaps closed with the critics' own prescribed fixes (no new design decisions), so a 3rd full
critic round on transcribed fixes = diminishing returns (operator token-economy mandate). NEXT: report captain
+ surface the 4 operator decisions (§12). Build WE1-WE8 via paul-logmine DOT ONLY after operator weighs in +
captain go.

---

## ROUND-3 (post operator-rulings deltas, 2026-06-24) — 3 fresh adversarial critics

Operator ruled the 4 §12 decisions; design.md changed §0 (name→watch), §6.1/6.2/6.3 (sender-redirect, filter
DROPPED), §7 (scheduled-send + per-setting descriptions), §11 (WE5 rescoped, WE6 updated). Round-3 critics
reviewed ONLY those deltas, verifying claims against code.

- **C1 — sender-redirect / O5 correctness → APPROVE (no blockers, no majors).**
  - LOAD-BEARING ground-truth VERIFIED TRUE: `comms recv --agent X` delivers ONLY to==X + broadcasts —
    MatchAgentMessage (internal/daemon/agent_message.go:33-44) via HandleCommsRecv
    (commsrecvhandler_nnwaa.go:200). The captain is NOT on a firehose; the no-filter ruling rests on a true
    premise.
  - O5 non-drop VERIFIED: operator→captain delivered; parked captain woken via handleAgentMessage:645-649 →
    executeWake:664-667; no filter exists to drop it.
  - SPOF bypass preserved IFF the partition is built (ops-monitor send_comms is one fn batching all immediates
    — §6.1 item-1 partition framing correct, dependency loudly flagged for WE7). §10 table holds.
  - NOTE (applied): §6.2/WE5 understated plumbing — executeWake/wakeSignal have NO wake-by-arbitrary-name
    branch (only captainWake bool + queueName); HandleDaemonWake:849 is the CLI path not the event path. WE5
    must add an agentName/target field to wakeSignal + executeWake branch. Localized, not a one-liner.

- **C2 — config fail-loud / descriptions → REVISE (2 blockers + 1 major, ALL applied).**
  - B1 (applied): the two description sinks (fail-loud error vs --example) are structurally separate with NO
    shared source in keeper (requiredKeeperValue has no description field; --example is a hand-written const
    keeper_config_example.go:36-86). WE6 must define ONE source — requiredWatchValue{KeyPath,Description,
    satisfied} (extends keeper shape) feeding BOTH error + --example, + a parity done-check test.
  - B2 (applied): copying ResolveKeeperConfig verbatim inherits a description-LESS error
    (KeeperConfigMissingError.Error() joins bare []string, resolve_keeper_config.go:116-128). Carrier must
    become []struct{KeyPath,Description} rendering `key — desc`; §7 calls out the divergence so it isn't
    copied away.
  - M1 (applied): third Go-literal schedule helper missed — seedGoalKeeperSchedule (init_cmd.go:841-869,
    Interval:"1h"), closest template to the watch 1h liveness ping; added to §7 MUST-NOT-copy list.
  - CONFIRMED clean: ruling-3 "identical config surface" holds (both wrapper + native action are a
    ScheduledJob in schedules.json, interval/target in watch config either way — no operator rewrite on
    upgrade). Command exec is direct argv, no shell (scheduletick.go:232-233, cmd.Dir=projectDir).

- **C3 — ruling-consistency / stale-ref sweep → REVISE (3 issues, ALL applied).**
  - §6.4 item 2 (was "unfiltered recv with client-side routing") → "plain recv, NO client-side routing".
  - WE4 (was "unfiltered recv + client-side routing") → "plain unfiltered recv (no client-side routing)".
  - WE4 done-check (was grep=0 "/loop 12m") → grep=0 "/loop" (matches §6.4, restores the round-2 broadening).
  - VERIFIED clean: zero stale name tokens (no "watch officer"/"WO"/"officer"); §1/§2/§4/§6.1-6.3/§9/§10/§12
    all express sender-redirect+new name; WE5/6/7 coherent; embed-sync done-checks intact.

### ROUND-3 CONSENSUS → DESIGN GATE: CLEAN
All round-3 findings were transcribed fixes (single-source description struct, wake-by-name plumbing note,
third Go-literal precedent, 3 stale-ref cleanups) — no architecture rejected. C1 APPROVE with the recv
ground-truth verified TRUE in code is the load-bearing confirmation that the operator's sender-redirect ruling
is sound and buildable. NEXT: report captain → on captain go, build WE1-WE8 via paul's queue DOT (hk-8hr1
precondition already met — landed on main).

---

## ROUND-4 — operator-follow-up re-scope (3 fresh adversarial critics, 2026-06-24)

Reviewing the 2 operator follow-ups folded into design.md: (A) couple watch-standup + sender-redirect into ONE
coordinated MVP rollout (never ship the redirect with no watch online); defer native scheduled-send + full
mutual-liveness + ledger polish to follow-on beads; (B) scheduled-send native-only, bash-wrapper DROPPED.
WE numbers kept STABLE (no renumber); WE7 narrowed to sender-redirect + basic watch-liveness, mutual-liveness
hardening moved to new WE9, WE10 = ledger polish.

- **C1 — coupling/sequencing correctness (a0762ec8) → REVISE (2 blockers + 1 major, ALL applied).**
  - B1 (applied, load-bearing): the "redirect is inert until the config flip" guarantee was ASSERTED, not
    constructed — §6.1 said the target "defaults to the watch" and §7 mandated config-or-fail-loud; neither
    yields a `captain` value at merge, so a merged-but-unflipped redirect would resolve to watch (redirect at
    merge time) OR fail loud (senders crash, dropping crew completions) — both = the blind-captain window
    follow-up A forbids. FIX: the two SENDER redirect-target keys `watch.status_target` + `watch.opsmonitor_target`
    DEFAULT to `captain` (explicit §7 exception, NOT fail-loud); config-or-fail-loud governs only the watch's OWN
    behavioral keys (intervals, escalation_target). Applied to §11 coupling guarantee + §6.1 item 1/3 + §7 (new
    exception bullet) + WE7 sub-bullet (+ done-check: un-set default resolves to captain).
  - B2 (applied): the "long heartbeat liveness fallback" replacing the removed captain `/loop` was hand-waved
    (no interval, no mechanism, no MVP bead). FIX: captain-liveness is now EXTERNALLY owned by the already-shipped
    ops-monitor captain-liveness probe (§5, component-liveness, no new bead); the captain runs NO self-`/loop` of
    any interval, so `grep=0 "/loop"` stays correct (not replaced by a longer-interval loop). Applied to §6.4
    item 2 (+ WE4 done-check asserts captain skill states liveness is ops-monitor-owned).
  - M1 (applied): §11 overstated that basic watch-down detection "closes the hazard" — it closes the no-watch +
    dead-watch windows but NOT alive-but-stalled (bus-pinned watch, fresh last_seen, frozen cursor passes basic
    process/tmux liveness while buffering escalations — §5/critic-3 #5b). FIX: honest framing — the residual is
    ACCEPTED for the MVP window ON CONDITION that WE9 (dual-probe + cursor-advancement) is the FIRST follow-on
    bead; ops-monitor captain-probe is the partial backstop until WE9. Applied to §11 + §1 diagram nit.
  - CONFIRMED clean: NO forward dependency of an MVP bead on a deferred bead (WE7 basic-liveness uses
    ops-monitor's existing escalate path, not WE6's schedule action; the WE6-EXTENDS-WE7-carrier relation is
    one-directional).

- **C2 — scope-split & code-claim integrity (a1ed8160) → APPROVE (no blockers/majors).**
  - Q1 carrier attribution CONSISTENT (WE7 introduces, WE6 extends) across §7/§11/§12; code TRUE:
    requiredKeeperValue has only keyPath+satisfied (resolve_keeper_config.go:266-269), Missing is []string
    bare-joined (:113,127), --example descriptions a separate const (keeper_config_example.go:36).
  - Q2 wrapper DROPPED cleanly — no surviving shippable wrapper option anywhere.
  - Q3 daemon QuiesceArbiter claims TRUE: handleAgentMessage wakes only on To==captainAgentName
    (quiesce.go:645-649); executeWake has only captainWake bool + queueName, NO wake-by-name (:661-675);
    parkAllSessions parks captain+crews by agentName (:466-475). "needs a new agentName field, not a one-liner"
    is accurate.
  - Q4 no orphaned schedule in the MVP — MVP is wholly event-driven; the hourly ping is wholly WE6/WE9.
  - Q5 ops-monitor partition fully in WE7 (MVP), nothing left in a follow-on.
  - NOTE (applied): ops-monitor line citations had drifted — real: send_comms :1005-1010 (was :951-959),
    8 signals :750-771 (was :696-717), join :1016-1036 (was :966-982), NON_CREW :655 (was :601), loops
    :659/:702 (was :605/:648). Content all TRUE; citations corrected in §6.1 item 1, §8 caveat, WE7.

- **C3 — full consistency / stale-ref sweep (a5b31eb6) → APPROVE (ZERO stale refs).**
  - Swept all 10 beads across every reference in §1–§12: every WE<n> reference matches its current §11 meaning;
    no phase contradictions (MVP {1,2,3,4,5,7,8} identical in table + build-order prose; follow-on {6,9,10}
    identical); no stale "WE1–WE8"/"8 beads" count; both dropped options (bash-c wrapper, client-side filter)
    appear only in explicit DROPPED framing; header + §12 both state the gate correctly; no orphans.
  - NIT (applied): §1 diagram L50 presented dual-probe+cursor-advance as baseline — appended
    "(basic watch-down MVP; dual-probe/cursor-advance = follow-on WE9)".

### ROUND-4 CONSENSUS → DESIGN GATE: CLEAN
2 APPROVE + 1 REVISE, every REVISE finding a transcribed fix (no architecture rejected) — same convergent-fix
pattern as rounds 1–3. C1-B1 was the load-bearing catch: the coupling guarantee is now CONSTRUCTED (default-safe
`captain` redirect target) not merely asserted, so the blind-captain window operator follow-up A forbids is
provably closed for the no-watch + dead-watch cases, with the alive-but-stalled residual honestly bounded by
making WE9 the first follow-on. Gate satisfied across 4 rounds. NEXT: report captain → on captain GO, build the
MVP group (WE1–WE5, WE7, WE8) via paul's queue DOT, then the follow-on group (WE6, WE9, WE10).
