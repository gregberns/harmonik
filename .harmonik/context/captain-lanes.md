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

## ⭐⭐ CURRENT TRUTH 2026-07-12 ~11:48Z — PARALLEL 5-LANE; hk-thbbv UNBLOCKED (admiral-volume cancelled); iaj1w race = STALE WORKER (hk-zno2t)

> **daemon REDEPLOYED to 6442aaa0 (2026-07-12 ~11:28Z):** tag daemon-20260712-06, pid 60409,
> crashed_in_health_window=false, serving 46 queues. NOW LIVE (the delta over b25e9919): **hk-f9xzs
> 2476922e** (merge-step retry loop + preserve-APPROVE — the commit_gate traversal-cap fix that wedged
> hk-nxcvi/hk-thbbv; GATE-0 satisfied by its own 347-line integration test, run green in isolation),
> hk-ru45u (presence reason:refresh — killed 53% of log volume), + evalvol test beads.
> **GREEN broadcast → stilgar hold LIFTED (proving f9xzs via hk-nxcvi), yueh T2b/T3 unblocks on hk-thbbv land.**
> **hk-5z1f0 CLOSED-OUT as already-built:** its mechanism (150s remote agent_ready timeout + spawn
> semaphore) was ALREADY in b25e9919 (implementer kept no-committing — no code gap). Residual 2/10
> reviewer cold-start timeout at sustained-6 is empirical TUNING, DEFERRED to the operator-gated 6→10
> ramp (bead down-ranked P1→P3, deferral noted). gb-mbp RE-ENABLED (was briefly disabled to route the
> now-void local attempt).
> **hk-thbbv UNBLOCKED (11:44Z):** stilgar root-caused the -32015 to a STALE 'pending' item (run_id null)
> in the drained admiral-volume queue — EM-065 cross-queue scan counted it as live occupancy = permanent
> lock. admiral OK'd; captain ran `harmonik queue cancel admiral-volume` (REVERSIBLE, archived to .failed).
> Freed 3 P1s (hk-thbbv/hk-r9n2s/hk-rs1eh). Durable Class-D boot-reconcile fix = hk-bl4d6 (stilgar, queue-lane).
> **⚠️ FLEET-DISPATCH OUTAGE 11:52Z — gb-mbp DISABLED (durable+live), fleet routes LOCAL.** stilgar's 6 runs
> (hk-thbbv, hk-2i36s x2, hk-nxcvi x3) ALL died worker_name=gb-mbp at worktree-create empty-HEAD = **hk-2hfyt
> RECURRENCE**. ROOT CAUSE (stilgar decisive repro, SUPERSEDES both the earlier stale-worker read AND the
> gc/mutex hypotheses): base commit IS present on worker (fetched+cat-file), MANUAL `git worktree add` SUCCEEDS,
> only the daemon's **ssh-runner-WRAPPED checkout no-ops** (createworktree.go:257 resolveWorktreeHEADViaRunner
> returns empty). Not git/fs/base/concurrency. Disable is OPERATOR-PRE-AUTHORIZED (07-11 re-enable's own
> "re-disable if it recurs" condition fired). **hk-2hfyt REOPENED P1 → hawat** (remote-runner-wrapper fix, its
> real remote lane — UN-IDLES hawat). hawat's hk-rs-validate-remote-898a close (claimed remote validated) was
> INSUFFICIENT — didn't exercise wrapped-dispatch; fold a real assertion into hk-2hfyt acceptance. stilgar
> re-submitted hk-thbbv+hk-2i36s to a fresh LOCAL stilgar-q run (landing now). hk-zno2t (fetch-before-add) +
> hk-rpp5a stale-worker framing RETRACTED (kept as hygiene refs only). RE-ENABLE gb-mbp only after hk-2hfyt
> lands + quiet-window serialized (max_slots:1) re-validate. If hawat's first fix attempt fails → major-issue-fanout.
> NOTE: daemon working-tree resets wipe UNCOMMITTED .harmonik/context edits — commit tier-2 updates.
> `harmonik crew start` sets the keeper `.managed` marker but does NOT spawn the watcher — use
> `harmonik start crew` for a fully keeper-armed relaunch (stilgar/hawat currently watcher-less).

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
