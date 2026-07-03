# logmine — Iteration 5 Findings (run 2026-06-14)

Crew **logmine** · epic **hk-mhmaw** · queue **logmine-q** · window **2026-06-13 ~19:00 →
2026-06-14 15:03 local** (continues `findings-iter4.md`; high-water `019ec259`). F-numbering
continues at **F42**.

**Method:** 6 read-only parallel sub-agents (NOT worktree-isolated), one per slice — (1)
failures/wedges + transcripts, (2) reconciliation/ledger-dep, (3) review loop, (4) daemon
lifecycle/keeper/queue, (5) comms bus, (6) daemon stdout + git churn + qa-scratch + disk.
Window = **4,324 events** (lines 21860→26183 of events.jsonl), frozen snapshot at
`/tmp/logmine-window.jsonl`, resolved by **line-anchoring the iter-4 high-water event_id**
(per F41a — the fix held: this run saw the FULL window, not iter-4's ~10%). **[T]** =
triangulated across ≥2 independent slices.

## Headline — commit_gate traversal-cap silently STRANDS committed work (F42) [P1]

The material finding of iter-5. Three beads exhausted the DOT `commit_gate→implement`
deterministic-fail cap (`traversal_cap=3`, `standard-bead.dot:148-151`,
`core/edgecascade.go:180`) and were routed to `close-needs-attention`. Their implementer
**commits exist but were VERIFIED not on origin/main** (`git merge-base --is-ancestor` =
false; each reachable only from its `run/<run_id>` worktree branch), never merged, never
re-dispatched in-window:

| bead | dangling sha | subject | on origin/main? |
|---|---|---|---|
| hk-4mten | `214189c2` | fix(ci): go test -short reds; continue-on-error while lint backlog remains | **NO** |
| hk-cogb | `020a6935` | chore: whole-repo gofumpt+gci format pass (make fmt x2) | **NO** |
| hk-dk5j | `caa281db` | chore(fmt): whole-repo gofumpt@v0.7.0+gci format pass — clears fmt-check skew | **NO** |
| hk-jqpr | `8fbb79df` | fix(fmt): run gci before gofumpt so fmt converges in one pass | **YES** (manual re-dispatch `019ec68c`) |

All 4 run_failed share ONE class: `dot: traversal cap hit at node "commit_gate"`
(events `019ec4bc/519/567/688`). All 4 fired `queue_paused reason=group_failure fail_count=1`.
The daemon **paused the group and did NOT auto-re-dispatch** the failed bead — only hk-jqpr
got a manual re-dispatch. The other 3 sit lost. **F42 is the lost-work class of this window.**

**Trigger (F43):** the pre-merge fmt-check gate (`5af82e47`, added this window) keeps failing
the whole-repo fmt-pass beads (hk-cogb/dk5j) because **fmt didn't converge in one pass** — the
exact bug `8fbb79df` (gci-before-gofumpt) fixed. Self-referential: the fmt-fix beads were
killed by the fmt-gate. Churn chain on main: `5af82e47`→`8fbb79df`→`785fb7b4` ("fmt-check
re-dirtied"). Since `8fbb79df` is now on main, the convergence trigger is **likely resolved**;
hk-cogb/dk5j may now be moot (verify main is fmt-clean). hk-4mten is distinct — its commit_gate
FAIL was `go test -short` reds (the subject of `214189c2`), not fmt.

**The DURABLE defect is F42, not F43:** commit_gate cap-exhaustion discards committed work with
no salvage and no auto-re-dispatch, independent of which deterministic gate triggered it.

---

## Register (prioritized)

| F# | Finding | Sev | Lane | Anchor | Status |
|----|---------|-----|------|--------|--------|
| F42 | commit_gate traversal-cap strands committed work (3 beads, dangling worktree shas, no salvage/re-dispatch) | **P1** | stilgar (hk-3js5m) | standard-bead.dot:148-151; ev 019ec4bc/519/567; merge-base verified | NEW [T] |
| F43 | pre-merge fmt-check non-convergence is the deterministic FAIL for fmt beads (self-referential) | P2 | stilgar (hk-3js5m) | 5af82e47→8fbb79df→785fb7b4 | NEW (trigger likely fixed by 8fbb79df) [T] |
| F44 | failed bead not auto-re-dispatched; queue paused on group_failure, 3/4 stranded | P2 | stilgar (hk-3js5m) | 4× queue_paused 019ec4bc/519/567/688 | NEW |
| F45 | keeper warn EMITTED BELOW warn_pct (24/79 at pct 24-29% < 30%) — emit-condition bug, NOT a band retune | P2 | stilgar (hk-4zy9, watcher.go) | warn payloads {pct:24,warn_pct:30} | NEW [T] |
| F46 | one captain session warn-nagged 60×/~9h, restart wanted but never crisp-idle (5/7 restart_now_blocked=not_crisp_idle) | P2 | stilgar (hk-4zy9 / hk-xjlq) | session 8f68d96f | RECURRING (signal-as-designed) |
| F47 | keeper no_gauge for captain chronic: 612 (603 stale + 9 foreign_session); gauge can't read captain ctx | P2 | stilgar (hk-mzdm closed) | no_gauge span 19:00→21:53Z | RECURRING — enrich/re-open |
| F48 | keeper handoff_timeout abort: cycle ...4 double-fired 18s after ...3 on session d4ef9e15 (precompact+warn collision) | P2 | stilgar (hk-mzdm closed) | cycle_aborted 02:57:02Z; 2× precompact_blocked | RECURRING [T] |
| F49 | F41b RECURS despite hk-umao CLOSED: events.jsonl still mixes Z (keeper, 2551) vs offset (eventbus, 1773) | P2 | stilgar (hk-umao closed) | jq Z/offset split over window | RECURRING — enrich/re-open |
| F50 | disk pressure worsening: 94% / 12Gi free (was 91%/17Gi iter-4) — −5Gi, +3pp/day; ENOSPC risk | P2 | ops/operator | df /System/Volumes/Data | RECURRING/worsening [T] |
| F51 | run_stale=13 RECURRING (6@launch_initiated=spawn-semaphore hk-4l7zs; 6@node_dispatch=commit_gate DOT-loop pre-cap; 1 self) | P3 | stilgar | run_stale payloads | RECURRING (overlaps F42) [T] |
| F12/F13 | run_stale slow-recovery flag→retract + assignee-collision round-trips — well-handled but ~4× captain↔crew "wedged?→not" cycles | P3 | logmine (orchestrator-rules) | comms trace | RECURRING (codify wait-ceiling) |
| F34 | auto-memory MEMORY.md index 26,200B > 24,986B cap (partial load) | P3 | logmine | wc -c MEMORY.md | RECURRING — self-fix this run |
| — | CI tug-of-war: revert 67ef8a6c re-applies continue-on-error that 11b761af removed (scenario.yml) | P3 | ci (hk-963f/hk-8387) | 67ef8a6c | tracked; DO NOT touch .github/workflows |

## FIXED-confirm this window (the recurrence payoff)

- **F1** reconciliation full-payload: 27/27 `{examined,reset,closed}` complete; examined=12, reset=0, closed=0. **F6** ledger-dep: 0 deferrals, no hk-dv8qv deadlock. Slice 2 fully CLEAN.
- **hk-rssrg unreviewed-merge = 0** [T×3] (reviewer_launched↔verdict↔run_completed↔bead_closed all 40/41 reconciled). **PL-031** REQUEST_CHANGES→APPROVE converged (1 cycle, hk-loz5). DOT review-gate holds (41/41 carry `workflow_mode:dot`). verdict-absent salvage class NOT recurring.
- **F38** run_stale reviewer-node false-positive: 0 stale on a reviewer node (all early-phase). **F37** -c4 spawn mitigation: active_run_count max 2, 0 spawn_failed/escape/no_progress.
- **F3** comms dedupe (165/165 unique event_id), **F11** identity (165/165 carry --from, single captain). Comms HEALTHY, zero mechanical friction.
- **F40** ShowBead exit-3, **F29** context.json force-add, **F33** tree pollution, **F30** test-prose-in-daemon-log (0 in-window): all FIXED-confirm.
- **F5** daemon lifecycle STABLE: 0 restarts, 0 backoff, 0 orphan-sweeps in-window (daemon up the whole window).

## Finding → bead map (Wave 2)

| Finding | Bead | Action |
|---|---|---|
| F42/F43/F44 commit_gate cap strands work | **hk-3js5m** (OPEN, crew:stilgar, infra-stability) | ENRICH comment (3 dangling shas + salvage/re-dispatch gap); digest to captain |
| F45/F46 keeper warn-below-threshold + 60×/9h nag | **hk-4zy9** (OPEN, keeper-warn-fix) | ENRICH comment; NOT a band retune (operator HARD-NO) |
| F47/F48 keeper no_gauge/handoff_timeout | **hk-mzdm** (CLOSED) | ENRICH comment + recommend re-open to captain |
| F49 timestamp mixed-format persists | **hk-umao** (CLOSED) | ENRICH comment + recommend re-open to captain |
| F50 disk worsening | — | digest to captain (operator action: go clean -cache / worktree prune) |
| F12/F13 wait-ceiling-before-wedged | **NEW codename:logmine,crew:logmine** | file dispatchable doc bead (orchestrator-rules) — HOLD dispatch if gate suspect |
| F34 MEMORY.md over limit | — | logmine non-token self-fix this run |

## Pipeline note

iter-5 validated the F41a line-anchored cursor: the window was the full 4,324 events (vs iter-4's
~10% under-window). The recurrence payoff was large this run — 14 priors confirmed FIXED — and
the new P1 (commit_gate cap strands work) was caught by triangulating slice-1's "dangling sha"
claim against an independent `git merge-base --is-ancestor origin/main` check on the main thread.

> high-water: 019ec829-2fb1-791c-af0c-80707ae55b6a  (2026-06-14T15:03:22Z queue_group_completed — last event in the frozen line-anchored window; next daily run resolves THIS event_id to its line and slices forward, per F41a)
