<!-- DRAFT — proposed replacement for .harmonik/context/captain-lanes.md
     (startup-doc revamp Stage 2 companion, per 02-cutover §2.5 + 03-operator-decisions.md).
     AUTHORITATIVE (read first, in order): 03-operator-decisions.md (OVERRIDES all), then
     02-cutover-and-open-questions.md, then 00-SYNTHESIS.md.
     GOVERNING OVERRIDE — PRINCIPLES, NOT RULES: this file LEADS with the snapshot principle
     (00-SYNTHESIS §6: L12-34 KEEP reconciled, L36-604 CUT — git is the archive). The five
     restored items below are guardrails UNDER that principle, not a re-injected checklist:
     - COMMIT-TIER-2-IMMEDIATELY named in the CONTRACT block — pointer only; the rule itself
       (stage the specific path, never `git add -A`; commit in the same action as the edit)
       now lives at orchestrator-rules §CWD/commit discipline. Restating it here would recreate
       the closed-category list this pass exists to remove.
     - LOAD-BEARING REVERSAL: the draft had kept only the lint-debt half of the fail-closed
       hooks redeploy-gate dependency; the `.claire/worktrees/agent-*` placeholder-purge half is
       restored below — dropping it silently reproduces the 07-11 fleet-wide merge outage
       (flipping fail-closed with 4 stale placeholders still failing `make check`).
     - Hook-mitigation recovery pointers carried forward: backup at
       `.harmonik/context/hook-mitigation-backup/` (+ `lefthook install` to reinstall), and the
       orphaned `git stash` (settings.json drift) — a live hazard with no current owner.
     - The "keeper-missing" dated item now carries `expires:` + an owner (context/CLAUDE.md
       forced-write requires both for any dated item).
     - hk-j0p1r (PR #30) credited alongside hk-y20d2 for the stdin fix deployed at 59089968.
     Banner removed on deploy; deploys in the SAME action as direction-log.md + lanes.json
     (cutover step 2.3), committed immediately, specific paths only. -->

<!-- TIER: 2 (operational state, days cadence)
     LOADED BY: captain, via the manifest wake step (`harmonik agent brief` reads project.yaml
                -> lanes.json + this file -> direction-log.md); NOT loaded by crews or implementers
     OWNER: captain, updated at session end or on any crew/epic change
     CONTRACT: ONE current-truth block, hard cap ~60 lines, REPLACE IN PLACE — an update DELETES
     what it supersedes, never annotates ("SUPERSEDED"/"LIFTED" prose is banned). Every dated item
     carries `expires:` + an owner; on expiry the default is LAPSE, not a hold (admiral audit owns
     re-confirming or striking). COMMIT-TIER-2-IMMEDIATELY: stage this specific path and commit in
     the same action as the edit — canon at orchestrator-rules §CWD/commit discipline; uncommitted,
     a replace-in-place rewrite is the only copy of current truth (a8d4591b reset the tree once). -->

# Tier-2 context: captain lane registry + medium-term tracker (days cadence)

## ⭐⭐ CURRENT TRUTH 2026-07-11 ~06:20Z — PARALLEL 5-LANE posture; pi flagship DONE; Codex Option B KILLED

**Operator priority order (2026-07-11, direction-log): run all 5 lanes IN PARALLEL, file-disjoint,
every non-conflicting slot full. IRON RULE — NEVER freeze the whole fleet for one bead; a stuck leg
reviews at its own pace (per-item gate), never a fleet-wide hold.**

| # | Lane | crew | queue | model | epic / state |
|---|---|---|---|---|---|
| 1 | **PI** | kynes | kynes-q | Opus | Flagship pi-hang **DONE** (hk-hcrvb CLOSED, deployed 59089968 — hk-j0p1r/PR #30 alongside hk-y20d2 for the stdin fix, prod-canary green) · pi-provider-switch **LANDED** (hk-m6uu2.*). Now on follow-up harness beads (hk-cdpxu empty-PATH). ✅ ready |
| 2 | **REMOTE** | hawat | hawat-q | Opus | remote-substrate + hardening (hk-rs-phase1-qfn1), 41/47 — buggy, heavy LOCAL testing. gb-mbp live re-enable OPERATOR-HELD (hawat does NOT flip). ✅ ready |
| 3 | **CODEX-AS-CREW** | piter | piter-q | Opus | epic hk-q3ovr. **Option B PERMANENTLY KILLED** (operator 07-11 hard-no — never revisit). Now: full kerf work `codex-app-server` — deep research on resident app-server orchestrator + can-it-retire-the-keeper. hk-l63b9 OPEN-PARKED until design names the path. |
| 4 | **QUALITY-ENFORCEMENT** | stilgar | stilgar-q | Opus | fail-closed gates (hk-clska line), 10/18. Currently hk-nwgj7 in-flight. **Redeploy-gate dependency:** fail-closed goes live ONLY once BOTH halves clear — main's containedctx/contextcheck lint debt AND the 4 stale `.claire/worktrees/agent-*` placeholder dirs purged (dropping either half reproduces the 07-11 fleet-merge outage, mitigated by removing the premature primary-repo hook shims — backup at `.harmonik/context/hook-mitigation-backup/`, reinstall via `lefthook install`). stilgar also owns reconciling an orphaned `git stash` (settings.json drift) left by that mitigation — live hazard, currently ownerless. ✅ ready |
| 5 | **COMMS-TEST-HARNESS** | yueh | yueh2-q | Sonnet | comms bus L0/L1/L2 tests. **B1+B2 operator-RATIFIED + dispatched**: B1=hk-8xspi (recv --agent own cursor), B2=hk-qw63o (idle --follow presence-beat) — removes a class of crew comms-wedges. ✅ ready |

**Oversight (not counted as lanes):** watch (triage, watch-q) + admiral (strategy). Both up.
**admiral tmux pane = LIVE OPERATOR session** — do NOT send keystrokes or relaunch while operator is in it.

**DEPRIORITIZED — do NOT staff:** eval-program · flywheel (needs full re-assessment before any work) · dehardcode.

### Standing lane notes
- **internal/daemon collision rule:** stilgar has right-of-way; hawat holds workloop.go / reversetunnel.go
  beads until stilgar's daemon work lands.
- **gb-mbp live re-enable is OPERATOR-HELD** — remote lane does LOCAL testing only; do NOT auto-flip.
- **keeper-missing:hawat,piter,yueh** (ops-monitor flag) — spawned via `crew start` without an
  auto-keeper (durability gap, not urgent). yueh's B2 fix + arming keepers via `harmonik start crew`
  addresses it; arm a manual keeper watcher next lull if a crew nears context fill.
  `expires: 2026-07-13T00:00:00Z` · owner: captain (re-confirm or strike per the LAPSE rule).
- **Paused-queue noise is EXPECTED cruft** — ~40 paused-by-failure canary/pi/gbmbp/quality/frontline
  queues + paused-queue:yueh-q (hk-1x8az dep-blocked on hk-thbbv). Suppressed by watch; do NOT resume.
- **Presence-staleness is not death:** crews aging out of `comms who` while their `--follow` watcher is
  killed are alive if pane-truth shows a spinner/idle-armed box (the B2 bug). Verify pane before reconciling.

### Open operator decisions (surfaced, non-blocking)
- **hk-0639** (Codex local-soak epic) — functionally done, open by charter; captain recommends CLOSE.
- **hk-4u1mb** (reviewer diff-budget) — conflicts with shipped heartbeat contract; operator leaning DEFER.
- Governor `liveness_no_progress_n`=10 (observe-only) — stands unless operator says 0.
