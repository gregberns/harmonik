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

## ⭐⭐ CURRENT TRUTH 2026-07-11 ~06:35Z — PARALLEL 5-LANE posture; pi flagship DONE; Codex Option B KILLED

> **fmt-gate outage RESOLVED + PROVEN (2026-07-12 06:26–06:35Z):** daemon's merge-step auto-format
> commit used a `fmt:` subject the lefthook commit-msg gate rejected → drift-bearing merges failed
> fleet-wide. Fix hk-9k24q (`chore:` + `Trivial: true`, 1b97559a) on main; **daemon REDEPLOYED to
> d1fbf715** (live 06:25:54Z, pid 35591, last-good pinned 06:27, tag daemon-20260711-04). PROOF:
> hawat's remote wave landed 3/3 clean past the gate at 6-slot concurrency (7d0a4afa/0708c377/1a2bceb9)
> — remote-substrate now proven e2e. **Residual (hk-2jeel, P1, → yueh):** two CONDITIONAL sibling
> merge-path commits still fail the same gate — `commitResidualDelta` (soft-fail/work-loss) +
> `stripRunContextFromMerge` (hard-fail/merge-abort); fix inbound, needs a follow-up redeploy to go live.
> NOTE: daemon working-tree resets wipe UNCOMMITTED .harmonik/context edits — commit tier-2 updates.

**Operator priority order (2026-07-11, direction-log): run all 5 lanes IN PARALLEL, file-disjoint,
every non-conflicting slot full. IRON RULE — NEVER freeze the whole fleet for one bead; a stuck leg
reviews at its own pace (per-item gate), never a fleet-wide hold.**

| # | Lane | crew | queue | model | epic / state |
|---|---|---|---|---|---|
| 1 | **PI** | kynes | kynes-q | Opus | Flagship pi-hang **DONE** (hk-hcrvb CLOSED, deployed 59089968, prod-canary green) · pi-provider-switch **LANDED** (hk-m6uu2.*). Now on follow-up harness beads (hk-cdpxu empty-PATH). ✅ ready |
| 2 | **REMOTE** | hawat | hawat-q | Opus | remote-substrate + hardening (hk-rs-phase1-qfn1), 41/47 — buggy, heavy LOCAL testing. gb-mbp live re-enable OPERATOR-HELD (hawat does NOT flip). ✅ ready |
| 3 | **CODEX-AS-CREW** | piter | piter-q | Opus | epic hk-q3ovr. **Option B PERMANENTLY KILLED** (operator 07-11 hard-no — never revisit). Now: full kerf work `codex-app-server` — deep research on resident app-server orchestrator + can-it-retire-the-keeper. hk-l63b9 OPEN-PARKED until design names the path. |
| 4 | **QUALITY-ENFORCEMENT** | stilgar | stilgar-q | Opus | fail-closed gates (hk-clska line), 10/18. Currently hk-nwgj7 in-flight. ✅ ready |
| 5 | **COMMS-TEST-HARNESS** | yueh | yueh2-q | Sonnet | comms bus L0/L1/L2 tests. **B1+B2 operator-RATIFIED + dispatched**: B1=hk-8xspi (recv --agent own cursor), B2=hk-qw63o (idle --follow presence-beat) — removes a class of crew comms-wedges. ✅ ready |

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
