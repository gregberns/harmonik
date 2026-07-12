<!-- TIER: 2 (operational state, days cadence)
     LOADED BY: captain @ STARTUP Step 0b; NOT loaded by crews or implementers
     OWNER: captain, updated at session end (before HANDOFF.md) or on any crew/epic change
     DO NOT PUT HERE: standing behavioral rules (→ orchestrator-rules skill);
                      this-session salvage / run-id play-by-play (→ HANDOFF.md tier-1);
                      durable phase/locked decisions (→ project.yaml tier-3) -->

# Tier-2 context: captain lane registry + medium-term tracker (days cadence)
# Captain reads on every boot (STARTUP.md Step 0b) BEFORE re-deriving lanes.
# Stable across /clear cycles; verify every claim against live ground-truth at Step 2.
# Keep this SHORT — one current-truth block. Superseded history is DELETED, not archived.

## ⭐⭐ CURRENT TRUTH 2026-07-12 ~08:15Z — PARALLEL 5-LANE; fmt-gate CLASS closed + daemon REDEPLOYED b25e9919; post-outage fleet-hardening in flight

> **fmt-gate outage + sibling CLASS fully RESOLVED; daemon REDEPLOYED to b25e9919 (2026-07-12 08:04Z):**
> tag daemon-20260712-05, pid 19214, health-window PINNED as last-good. NOW LIVE: hk-2jeel (fmt sibling
> `commitResidualDelta`/`stripRunContextFromMerge` — subjects proven vs validate-commit-msg.sh; GATE-0
> satisfied by direct hook verification since the committed Go test hk-r1v2n lagged), hk-1q7bt (no_gauge
> flood fix, kills 683/window), hk-p006e (crew-start `.managed` keeper-arm marker), hk-fpjxi (reaper),
> hk-hjvl4/1b348917 (dispatch-lock release), + the QA-EXECUTION-GATE workflow (implement→commit_gate→
> review→qa→close; admiral 019f551f). EMPIRICAL: 1b348917 did NOT release hk-thbbv's -32015 lock
> (stilgar verified post-deploy) → hk-eaxc5 status-gated skip still blocks hk-thbbv→yueh T2b.
> **Next systemic fix in flight: hk-f9xzs (merge stage discards passed APPROVE on retry → 31 full
> re-runs; = the commit_gate traversal-cap loop wedging hk-nxcvi) → stilgar.**
> NOTE: daemon working-tree resets wipe UNCOMMITTED .harmonik/context edits — commit tier-2 updates.
> `harmonik crew start` sets the keeper `.managed` marker but does NOT spawn the watcher — use
> `harmonik start crew` for a fully keeper-armed relaunch (stilgar/hawat currently watcher-less).

**Operator priority order (2026-07-11, direction-log): run all 5 lanes IN PARALLEL, file-disjoint,
every non-conflicting slot full. IRON RULE — NEVER freeze the whole fleet for one bead; a stuck leg
reviews at its own pace (per-item gate), never a fleet-wide hold.**

| # | Lane | crew | queue | model | epic / state |
|---|---|---|---|---|---|
| 1 | **FLEET-HARDENING / logmine** | kynes | kynes2-q | Opus | PI flagship DONE + fleet-hardening (hk-p006e keeper-arm + hk-fpjxi reaper) LANDED. Now owns **logmine epic hk-mhmaw** (recurring mine→document→file). iter-22 delivered (findings-iter22.md, 5 beads filed). Currently on **hk-q6nve** (agent-reviewer unregistered in worktree = root of 44% missing-trailer). ✅ |
| 2 | **REMOTE** | hawat | hawat-q | Opus | remote-substrate PROVEN e2e 3/3 (7d0a4afa/0708c377/1a2bceb9) **+ SUSTAINED-6 BANKED 10/10 clean on b25e9919** (2/10 first-pass reviewer cold-start timeouts both landed on retry = transient drag, not block). **6→10 workers.yaml ramp HELD** pending a reviewer cold-start notch. Now re-tasked → **hk-5z1f0** (P1: remote reviewer agent_ready_timeout under 6 concurrent slots over the tunnel = the residual ceiling). CAVEAT sent: hold reversetunnel.go + coordinate w/ stilgar before any internal/daemon file. gb-mbp live re-enable OPERATOR-HELD. watcher-less. ✅ |
| 3 | **CODEX-AS-CREW** | piter | piter-q | Opus | epic hk-q3ovr. Option B PERMANENTLY KILLED. **codex-app-server PHASE-1 COMPLETE + proven** (tap/serializer/reactor/twin + L0/L1/L2 green vs real corpus; T0-T5 closed). Verdict: layering cheap+sound, cost was ~all daemon/merge infra. **IDLE-ARMED on Decision #2 (Phase-2 go/no-go: hk-nzzos resident client+sidecar + Spike B) — SURFACED to operator, awaiting ruling.** |
| 4 | **REVIEW-LOOP + DISPATCH-LOCK** | stilgar | stilgar-q | Opus | RESTARTED 08:12Z (fresh ctx, keeper-marker set, watcher-less). Re-laned from stale gate-clear mission onto the systemic **hk-f9xzs** (merge discards APPROVE on retry = commit_gate traversal-cap loop) → **hk-thbbv** (flagless REQUEST_CHANGES wedge, still -32015 blocked via hk-eaxc5) → hk-nxcvi. Unblocks yueh T2b. internal/daemon right-of-way. ✅ |
| 5 | **COMMS-TEST + presence** | yueh | yueh2-q | Sonnet | comms bus tests. Main T2b/T3 chain still GATED on hk-thbbv (stilgar). **hk-r1v2n MERGED** (GATE-0 durability test — fmt-gate class now has permanent regression coverage). Now on **hk-ru45u** (presence join-only flood = 53% of ALL log; reason:refresh + single-emit/tick + leave-on-teardown). ✅ |

**Oversight (not counted as lanes):** watch (triage, watch-q) + admiral (strategy). Both up.
**admiral tmux pane = LIVE OPERATOR session** — do NOT send keystrokes or relaunch while operator is in it.

**DEPRIORITIZED — do NOT staff:** eval-program · flywheel (needs full re-assessment before any work) · dehardcode.

### Standing lane notes
- **internal/daemon collision rule:** stilgar has right-of-way; hawat holds workloop.go / reversetunnel.go
  beads until stilgar's daemon work lands.
- **gb-mbp live re-enable is OPERATOR-HELD** — remote lane does LOCAL testing only; do NOT auto-flip.
- **keeper-missing:hawat,piter,yueh** (ops-monitor flag) — spawned via `crew start` without an auto-keeper
  (durability gap, not urgent). yueh's B2 fix + arming keepers via `harmonik start crew` addresses it;
  arm a manual keeper watcher next lull if a crew nears context fill.
- **Paused-queue noise is EXPECTED cruft** — ~40 paused-by-failure canary/pi/gbmbp/quality/frontline queues
  + paused-queue:yueh-q (hk-1x8az dep-blocked on hk-thbbv). Suppressed by watch; do NOT resume.
- **Presence-staleness is not death:** crews aging out of `comms who` while their `--follow` watcher is
  killed are alive if pane-truth shows a spinner/idle-armed box (the B2 bug). Verify pane before reconciling.

### Open operator decisions (surfaced, non-blocking)
- **hk-0639** (Codex local-soak epic) — functionally done, open by charter; captain recommends CLOSE.
- **hk-4u1mb** (reviewer diff-budget) — conflicts with shipped heartbeat contract; operator leaning DEFER.
- Governor `liveness_no_progress_n`=10 (observe-only) — stands unless operator says 0.
