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

## ⭐⭐ CURRENT TRUTH 2026-07-12 ~16:15Z — PARALLEL 5-LANE, ALL MOVING. daemon 77ea4295 (daemon-20260712-07) live.

> **Fleet CLEAN.** All 5 lanes verified pane-truth this boot (advancing spinners + idle-armed boxes).
> `comms who` staleness on watcher-less crews (stilgar/yueh/piter) is the B2 aging bug, NOT death.
>
> **Remote worktree-outage ROOT CAUSE SETTLED (admiral 3-deepdive, findings 04+05):** NOT a concurrent
> race, NOT a silent push-drop, NOT a stale-worker (all prior framings RETRACTED). The daemon's post-create
> `git rev-parse HEAD` probe (createworktree.go:257 `resolveWorktreeHEADViaRunner`) returns EXIT-0 + EMPTY
> stdout over the SSHRunner (non-interactive login-shell git-binary divergence) → daemon falsely declares
> empty-HEAD → `cleanupPartialState` rm -rf's a GOOD worktree → burns all 4 retries. The '~6 concurrency
> ceiling' = SSH serialization (no ControlMaster), not a resource wall — so hk-5z1f0 timeout-bump+semaphore
> is COUNTERPRODUCTIVE. **hawat DISPATCHED hk-2hfyt** = the fail-loud HONEST-PROBE fix (rev-parse --verify /
> test -e <wt>/.git before ANY teardown + harden remote git-invocation vs ~/.zshenv env divergence + fix the
> false 'concurrent race' error string). **WATCH: hk-2hfyt land.** hk-zno2t HELD as secondary base-hardening.
> **A-vs-B ruled B-tactical-NOW** (honest-probe lands under both); A = resident-worker REARCHITECTURE scoped
> as problem-space (folds into remote-substrate-phase2), **BUILD go/no-go is the OPERATOR's** (overlaps piter's
> codex resident-orchestrator thread). gb-mbp stays DISABLED until hk-2hfyt lands + serialized re-validate.
>
> NOTE: daemon working-tree resets wipe UNCOMMITTED .harmonik/context edits — commit tier-2 updates.
> stilgar/hawat/piter/yueh are watcher-less (`crew start` sets `.managed` but no watcher — use
> `harmonik start crew` for a fully keeper-armed relaunch). **piter nearing auto-compact (~4%)** — its beads
> are dispatched+durable, so a lossy compact costs only its "report beads" reminder; not urgent.

**Operator priority order (2026-07-11, direction-log): run all 5 lanes IN PARALLEL, file-disjoint,
every non-conflicting slot full. IRON RULE — NEVER freeze the whole fleet for one bead; a stuck leg
reviews at its own pace (per-item gate), never a fleet-wide hold.**

| # | Lane | crew | queue | model | epic / state |
|---|---|---|---|---|---|
| 1 | **FLEET-HARDENING / logmine** | kynes | kynes2-q | Opus | PI flagship DONE + fleet-hardening (hk-p006e keeper-arm + hk-fpjxi reaper) LANDED. Now owns **logmine epic hk-mhmaw** (recurring mine→document→file). iter-22 delivered (findings-iter22.md, 5 beads filed). Currently on **hk-q6nve** (agent-reviewer unregistered in worktree = root of 44% missing-trailer). ✅ |
| 2 | **REMOTE** | hawat | hawat-q | Opus | remote-substrate PROVEN e2e 3/3 + SUSTAINED-6 BANKED 10/10 on b25e9919. **6→10 ramp HELD**, gb-mbp live re-enable OPERATOR-HELD, and the worktree-race work is stilgar's internal/daemon zone (collision rule) → remote lane has NO dispatchable file-disjoint work right now. **UN-IDLED (11:55Z) → hk-2hfyt** (P1 REOPENED: remote-runner-wrapper checkout no-op — the gb-mbp fleet-outage bug; its REAL remote-lane work). Spinner advancing. Acceptance MUST include a real wrapped-dispatch worktree-create assertion (its prior hk-rs-validate-remote-898a close missed that path). gb-mbp stays DISABLED until this lands + serialized re-validate. watcher-less. ✅ |
| 3 | **CODEX-AS-CREW** | piter | piter-q | Opus | epic hk-q3ovr. Option B PERMANENTLY KILLED. **codex-app-server PHASE-1 COMPLETE + proven** (tap/serializer/reactor/twin + L0/L1/L2 green vs real corpus; T0-T5 closed). Verdict: layering cheap+sound, cost was ~all daemon/merge infra. **IDLE-ARMED on Decision #2 (Phase-2 go/no-go: hk-nzzos resident client+sidecar + Spike B) — SURFACED to operator, awaiting ruling.** |
| 4 | **REVIEW-LOOP + DISPATCH-LOCK + DAEMON-HYGIENE** | stilgar | stilgar-q | Opus | internal/daemon right-of-way. **hk-thbbv now UNBLOCKED** (admiral-volume cancel cleared the EM-065 phantom lock) → driving hk-thbbv (flagless REQUEST_CHANGES wedge fix) → hk-nxcvi; unblocks yueh T2b. Also owns: **hk-zno2t** (remote stale-worker fetch-before-add durable fix), **hk-2i36s** (wire ReapBranches periodic, re-dispatching), **hk-bl4d6** (Class-D boot-reconcile for drained-queue stale-pending). ✅ |
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
