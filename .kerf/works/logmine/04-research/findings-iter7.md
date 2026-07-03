# logmine — Findings iter-7 (2026-06-16)

**Crew:** logmine · **Epic:** hk-mhmaw · **Queue:** logmine-q
**Window:** line-anchored from iter-6 high-water `019eceae-18a0-7952-ba99-6894b8cbd612`
→ **5073 events** (lines 28186–33258 of events.jsonl), 2026-06-16T04:26Z → 19:00Z UTC (~14.6h).
**Method:** 6-slice READ-ONLY fan-out over the frozen snapshot `/tmp/logmine-window.jsonl`
(whole file = window, no timestamp-string filter per F41a). Each slice classified prior `Fxx`
FIXED-confirm vs RECURRING; every finding anchored to event_id / sha / file:line.

---

## HEADLINE

**This is a DAEMON BATCH day (a keeper-redesign + flywheel/sentinel sprint), not a remote-substrate
sprint like iter-6.** 40 `run_started`, 22 `run_completed`, real DOT runs across multiple named crew
queues (paul-q, thufir-q). Throughput healthy. Health verdict: **GREEN-but-noisy, with two NEW P1
defects and one still-unfixed P1 incident.**

The three top items:
1. **F55 (P1, NEW) — the session-keeper is flooding the event log.** `session_keeper_operator_attached`
   = 2603 events = **51.3% of the entire window** (~5s poll-tick persisted as a durable typed event).
   This is the single biggest reason every logmine slice must line-anchor + frozen-snapshot.
2. **F56 (P1, NEW — root-causes iter-6 F53) — daemon restarts cancel in-flight DOT runs.** All 9
   `context cancelled during node "implement"` fire within ~3s of a `daemon_started` (8 restarts in
   14h). AND **F42/F44 (P1, RECURRING) — 7 beads' committed work is stranded on orphan `run/<id>`
   branches**, never merged, no auto-salvage. Salvageable now via cherry-pick.
3. **hk-cmry (P1, already filed by logmine 15:55Z) — daemon merge-rebase silently DROPS reviewed
   work.** This window supplies the anchors it was flagged NEEDS-FULL-ANCHORS for (see Wave-2 map).

Plus a live **FALSE-GREEN (F59)**: the captain boot HEALTHY-criterion "daemon up = `queue status ≠ 17`"
reads green even on a *paused-by-failure* queue; the boot-audit reports **main + remote-substrate +
~14 crew queues currently paused-by-failure**. Main is paused right now on **hk-tagp** (F61, remote
worker `agent_ready_timeout` 90s).

Cleanest domains: reconciliation + ledger-dep (21/21 clean, 15 deferrals all resolved, 0 deadlock),
the unreviewed-merge guard (0/22), the comms bus (210/210 unique event_ids, single captain, exemplary
HOLD→GREEN restart coordination), and disk (improved to 80% / 42 GiB — trend reversed two iters running).

---

## FINDINGS REGISTER (iter-7)

| ID | P | Finding | Anchor | Class | Disposition |
|----|---|---------|--------|-------|-------------|
| **F55** | P1 | **keeper `operator_attached` event-log spam** — 2603 events = 51.3% of window; ~5s poll-tick persisted as durable typed event (1946/1997 gaps = 5s in top session). Per-session: 1998/`2d75d697`, 516/`014c567d`, 52/`ac2fdc6e`. Wastes I/O, bloats events.jsonl, forces line-anchoring. | first `019ecec3-a27c`; internal/keeper poll path | **NEW** (daemon/keeper) | FILE crew:stilgar bead — gate to attach/detach *edges* or sample ≥1/min |
| **F56** | P1 | **daemon restart cancels in-flight DOT runs** — all 9 `context cancelled during node "implement"` fire within ~3s of a `daemon_started`; the 15:55 triple cancels 3 concurrent runs at once. 8 restarts/14h (`binary_commit_hash:"unknown"`, incl. a `-c1→-c4` flap 17:25→17:44). **Root-causes iter-6 F53.** | run_failed `019ecec4`,`019ecf09`,`019ecf47`,`019ecf8a`,`019ed124-ca9d/caf8/cb0c`,`019ed1c9` vs daemon_started ts | **NEW/RECURRING** (daemon) | FILE crew:stilgar bead — restart-drain / in-flight grace before respawn; **upgrade F53 P3→P1** |
| **F42/F44** | P1 | **committed implementer work stranded on orphan `run/<id>` branches, no auto-salvage** — 7 beads with `Refs:` commits reachable ONLY from `refs/heads/run/<run_id>` (merge-base off origin/main verified): hk-7rmv(`8f8030b3`), hk-ar5y(`cd0158b1`), hk-psds(`e4846533`), hk-snjr(`555c33cd`), hk-t1wd(`d7443404`), hk-u0lv(`69044213`), hk-w2ow(`a02de62d`). | merge-base verify per bead | **RECURRING** (daemon) | ENRICH hk-3js5m; the 7 branches are salvageable via cherry-pick (captain/operator action) |
| **hk-cmry** | P1 | **daemon merge-rebase silently DROPS already-reviewed/committed work** (filed by logmine 15:55Z; the broken-main keeper-pkg compile P0). Today dropped hk-8prq's `.sid` GREEN; main self-healed by luck at 3125b68c. | bead hk-cmry; incident comms 15:45–16:25Z | **OPEN/UNFIXED** (daemon merge-integrity) | ENRICH hk-cmry with this window's anchors (see Wave 2); crew:stilgar |
| **F49** | P2 | **daemon merge commits omit `Reviewed-By:`/`Review-Verdict:` trailers** — 8/8 APPROVE'd+merged daemon beads carry 0 trailers; no daemon code writes the verdict to the commit (only reader `internal/workspace/reviewverdict.go`). Verdict lives only in events.jsonl. **RESOLVED iter-6 NEEDS-VERIFY → RECURRING (not benign).** | merge SHAs `69044213`/`aca28b0a`/`574cb797`; code grep | **RECURRING** (daemon merge-msg builder) | crew:stilgar bead — embed verdict in merge message |
| **F57** | P2 | **hard 20:00 per-node agentic timeout kills opus reviewers mid-`review_correctness`** → run_failed context-cancelled, no verdict, run discarded (no merge). Extends F53 into the reviewer node. | run `019ed1ad-77a3` (hk-t1wd) run_failed ln5044; paul comms ln5056 | **NEW** (daemon/agentic-timeout) | crew:stilgar bead — raise review-node ceiling or chunk review |
| **F58** | P2 | **daemon-spawned implementer injected onto comms bus under ad-hoc identity** — `from:"hk-t1wd-impl" to:"paul" topic:"blocker"` (15:40Z); only non-crew sender all window; registered a presence event. Escalation was correct + redundant, but an ephemeral worktree agent can inject into the coordination channel under an un-modeled name. | event ~`019ed0…`; presence `hk-t1wd-impl online=1` | **NEW** (comms identity/registry) | crew:stilgar bead — decide allow+register vs fail-closed |
| **F59** | P1 | **captain boot HEALTHY-criterion is a FALSE-GREEN** — criterion #6 "daemon up = `queue status` ≠ 17" returns 0 on a *paused-by-failure* queue ("up ≠ dispatching"). Boot-audit reports **main + remote-substrate + ~14 crew queues currently paused-by-failure**. | `docs/captain-boot-audit-2026-06-16.md` §B1 | **NEW** (captain skill + ops) | crew:liet doc fix (STARTUP.md health criterion) + **operator digest: paused-queue mass** |
| **F60** | P2 | **`STARTUP.md:319` comms-recv command is wrong** — `comms recv --follow --from captain` filters by *sender*, so the captain watches its own outbox (empty inbox). Should be inbox-watch (resolve identity, not `--from`). | `docs/captain-boot-audit-2026-06-16.md` §C1; STARTUP.md:319 | **NEW** (captain skill) | crew:liet dispatchable doc fix (after verifying the correct recv invocation) |
| **F61** | P2 | **hk-tagp remote gap7 e2e bead `agent_ready_timeout` (90s) — remote worker never responded; THIS paused the main queue.** | run_failed `019ececa-9ecf` timeout_ms:90000 | **NEW** (remote-substrate) | digest to captain; relate hk-eodo / remote-substrate lane |
| **F62** | P2 | **no-progress guard fails correct one-shot-complete beads** — documented in `docs/known-issues/no-progress-guard-fails-one-shot-beads.md` (commit `aec4d917`) but has NO tracking bead. Same DOT-completion-guard family as `a02de62d` hk-w2ow (advisory-RC exemption). | `aec4d917`; the known-issues doc | **NEW** (daemon DOT guard) | digest: recommend captain file a bug bead |
| **F63** | P2 | **`kerf next` blind to major lanes** — daemon-infra / remote-substrate / release-pipeline / flywheel-motion score ~0 (missing `bead_filter` clauses). "Re-org not durable until kerf bead_filters wired." | `docs/initiative-status-2026-06-15.md` §cross-cutting-2 | **RECURRING** (kerf-beta) | digest; recommend a bead_filter-wiring bead (kerf lane) |
| **F45** | P2 | **keeper `warn` fires BELOW `warn_pct`** — 8/80 warns at pct 27–29 < warn_pct 30. `belowWarnThreshold` Tokens-vs-Pct path persists. | `019eced3`,`019ed08d`,`019ed106`,`019ed108`,`019ed10a`×2,`019ed10b`,`019ed10c` | **RECURRING** (daemon/keeper) | hk-4zy9 is CLOSED → fold into the new keeper bead; NOT a band retune (operator HARD-NO) |
| **F46** | P3 | **keeper wants restart, never crisp-idle** — 2/3 `restart_now_blocked = not_crisp_idle`. | `019ed098`,`019ed108` | **RECURRING** (daemon/keeper) | fold into new keeper bead; relate hk-xjlq (captain restart-now) |
| **F47** | P2 | **`no_gauge` for captain** — 279 events, 100% `agent_name:captain` (217 stale + 62 foreign_session); 0 crew gauge; sweep `captain_sessions_skipped:0` (never sees a captain session to skip). Down from iter-6's 751 only because uptime/session shorter — captain still never holds a gauge. | no_gauge span; sweep payloads | **RECURRING/WORSENING** (daemon/keeper) | fold into new keeper bead; relate hk-tt9q (crew gauge), hk-mejt (rebind) |
| **F13** | P2 | **captain↔crew ownership/reopen round-trips** — ~11 exchanges; sharpest: **hk-t1wd stuck IN_PROGRESS, crew blocked on a captain-only `br update --status=open` ×3** because the daemon doesn't auto-reset per-node-timed-out beads. | comms log; hk-9ptu/hk-uvo8/hk-psds/hk-t1wd | **RECURRING** | root = hk-60t8 (daemon auto-reset-on-timeout); crew-self-reopen delegation = **captain decision** (contradicts crew no-terminal-write contract — do NOT self-edit) |
| **F40** | P3 | **`br exit 3` ShowBead burst at cold boot** — 37× at 10:44, 0 after; QM probes ~37 queued IDs while br DB briefly locked post-boot. Benign (skip-and-continue), noisy. iter-5 had F40 FIXED-confirm → cosmetic regression at cold-boot only. | `/tmp/hk-a3dc45482890-daemon.log:7-43` | **RECURRING-cosmetic** | digest; enrich the F40/ShowBead bead |
| **F43** | P2 | **gofumpt merge-gate non-convergence is back** — 4 `merge_build_failed` (gofumpt unformatted) + 3 fresh `fmt: auto-format` commits on main. iter-5's `8fbb79df` (gci-before-gofumpt) did NOT hold. Now auto-formats+commits rather than blocking, so converging not blocking. | `019ecee2`,`019ecee5`,`019ecf3a-0435/2079`; commits `dfbfa1c1`,`71789309`,`faddbc99` | **RECURRING** (daemon merge-gate) | crew:stilgar — reopen the "8fbb79df fixed it" claim; FIXED-confirm on *blocking*, residual *churn* cosmetic |
| **F41b** | P2 | **events.jsonl mixed-format timestamp** — top-level `timestamp_wall` now **uniformly Z (5070/5070)** (meaningful improvement); mixed format **moved to nested payload** (`lifecycle_transition.payload.transitioned_at` local offset, 2×). | top-level vs nested payload scan | **PARTIALLY FIXED** | recommend captain re-verify hk-umao; check nested-payload normalization |

---

### FIXED-confirm this window (recurrence payoff)

| Finding | This-window evidence | Class |
|---|---|---|
| **F50** disk pressure | 80% used / **42 GiB free** (iter-6 88%/23; iter-5 94%/12) — trend reversed two iters | **FIXED-confirm** |
| agent-mail 18 GiB runaway log | `~/Library/Logs/agent-mail/` absent; whole `~/Library/Logs` = 8.1 MiB | **FIXED-confirm — root cause eliminated** |
| **F51b** CI revert tug-of-war | `scenario.yml` untouched in window (last `67ef8a6c`, 2 days stale); no new CI churn | **FIXED-confirm (quiescent)** |
| **F37/F51** spawn-semaphore never-spawned wedge | 0 never-spawned runs in the typed event stream (all 22 failed/stale run_ids have `run_started` + `implementer_phase_complete`). *Caveat:* daemon log recorded 1 `never-spawned reaper` fire on an hk-t1wd run (reaped correctly at 30m). Class effectively dormant. | **FIXED-confirm (events) / 1 log-only recovered** |
| **F48** remote-substrate worktree-isolation loss | 0 daemon-side worktree-missing/chdir events; the 2 transcript `is_error` are benign agent globs inside the correct worktree CWD. (Local batch = weak negative, but clean.) | **FIXED-confirm (0 occurrences)** |
| **F3 / F11** comms dedupe + identity | 210/210 unique event_ids; 1 captain (clean leave@16:30 + join@18:18, no overlap); 0 foreign-name sends by crews | **FIXED-confirm** |
| unreviewed-merge guard (hk-rssrg) | 0 unreviewed merges across 22 completions; 41 APPROVE / 10 REQUEST_CHANGES / 0 BLOCK | **FIXED-confirm** |
| **F38** reviewer-node run_stale false-positive | 0 reviewer-node run_stale (the one review failure = real 20:00 timeout, F57) | **FIXED-confirm** |
| **F1/F6** reconciliation + ledger-dep | 21/21 recon clean (closed=0, 2 benign `bead_inprogress_queue_absent` self-heals); 15 deferrals all blockers-closed; 0 deadlock | **FIXED-confirm** |
| **F52** daemon restart *cadence* | 8 restarts, benign deploy cadence, no crash-loop — BUT see F56: the restart *action* cancels in-flight runs. Cadence benign; consequence is not. | **BENIGN (cadence) / harmful (in-flight) → F56** |
| restart coordination (anti-finding) | 13 captain→`*` HOLD→GREEN broadcasts, 0 collision, 0 two-fixers race; paul stood down captain's manual fixer on self-heal | **GREEN — record as confirmation** |

### False-fail verification (slice 1, git-triangulated)

False-fail rate **~38% (5/13)** — much lower than iter-6's ~100%, because this window's failures left
genuinely *unmerged* work (F42 stranded) rather than direct-committed sprint work.
- **Landed (false-fail):** hk-0z5x(`a90ead24`), hk-9mr2(`759e0a61`/`9e687d9e`), hk-9ptu(`b3641d51`/`dfbfa1c1`, after 4 dispatches), hk-l8mv(`574cb797`/`02a7d451`), hk-owz1(`2a1960d8`/`40cdec41`).
- **True-fail / stranded (F42):** hk-7rmv, hk-ar5y, hk-psds, hk-snjr, hk-t1wd, hk-u0lv, hk-w2ow — all on orphan `run/` branches.
- **True-fail (no commit anywhere):** hk-tagp (F61, remote worker timeout).

### Cross-slice discrepancy noted (for next iter)
Run `019ed1ad-77a3` (hk-t1wd) was read as a **reviewer node context-cancelled at ~20m** (slice 2) AND
as a **never-spawned reaper fire at 30m12s** (slice 5, daemon-log line). hk-t1wd was re-dispatched
multiple times → these are likely different run_ids/log-lines conflated. Event-stream scan (slice 1)
found `run_started` for all 22 in-window run_ids. Net: spawn-leak class is dormant; the timeout/cancel
story (F56/F57) is the real one. Re-verify run_id attribution iter-8.

---

## LANE ROUTING (Wave 2/3)

- **crew:stilgar (daemon/comms code — digest to captain, do NOT dispatch from logmine-q):**
  F55 (operator_attached spam, P1, NEW bead), F56 (restart cancels in-flight runs, P1, NEW bead),
  F42/F44 (enrich hk-3js5m, P1), hk-cmry (enrich w/ anchors), F49 (NEW bead, merge-trailer), F57 (NEW
  bead, review-node timeout), F58 (NEW bead, implementer-on-bus), F43 (reopen fmt-convergence claim),
  consolidated keeper bead (F45/F46/F47 + handoff_timeout abort — hk-4zy9 is CLOSED).
- **crew:liet (docs — dispatchable to logmine-q):** F60 (STARTUP.md recv command fix) + F59-doc
  (boot HEALTHY-criterion false-green text) — both touch the **captain skill**, so **greenlight with
  captain before dispatch** (audit-recommended but it is the captain's own operating contract).
- **captain / operator digest (no dispatch):** F59 paused-queue mass (operator-urgent), F61 (hk-tagp
  remote timeout), F62 (file no-progress-guard bug bead), F63 (kerf bead_filter wiring), F40 (enrich
  ShowBead bead), F13 crew-self-reopen delegation question (contradicts crew no-terminal-write contract).
- **DIGEST-ONLY (CI, do NOT touch .github):** F51b confirmed quiescent — no action.

## FINDING → BEAD MAP (Wave 2, filed 2026-06-16)

| Finding | Bead | Lane label | P | Note |
|---|---|---|---|---|
| F55 keeper operator_attached spam | **hk-2yvx** | codename:keeper-redesign | P1 | captain-routed (paul) |
| F56 restart-cancels-in-flight + F42 stranded-work | **hk-86eh** | codename:no-progress-guard | P1 | captain-routed (thufir); 6-bead salvage list w/ run/ branch refs |
| F49 merge commits omit review trailers | **hk-dyim** | crew:stilgar | P2 | RESOLVED iter-6 NEEDS-VERIFY→RECURRING |
| F57 review-node 20:00 agentic timeout | **hk-4p2h** | crew:stilgar | P2 | |
| F62 no-progress guard fails one-shot beads | **hk-f1uh** | codename:no-progress-guard | P2 | doc existed, no tracking bead (now tracked) |
| F45/F46/F47 keeper-gauge defects | **hk-jgzg** | codename:keeper-redesign | P2 | hk-4zy9 closed-but-unfixed; NOT a band retune |
| F59/F60 captain STARTUP.md doc fixes | **hk-t8ew** | crew:liet | P1 | **HELD** pending captain greenlight (edits captain contract) |
| F42/F44 (enrich) | hk-3js5m (comment) | — | — | xref hk-86eh |
| hk-cmry rebase-drop (enrich) | hk-cmry (comment) | — | — | anchors added (hk-8prq run 019ed0ef…) |

**Digest-only (no bead):** F58 (implementer-on-bus, needs-design), F61 (hk-tagp remote agent_ready_timeout — relate hk-eodo), F63 (kerf bead_filter wiring), F40 (ShowBead boot burst, cosmetic), F43 (fmt-convergence now self-healing), Refs-without-colon (cosmetic), F41b (partial fix — re-verify hk-umao), F13 (root = existing hk-60t8).

> **Window:** line-anchored from iter-6 high-water `019eceae-18a0-7952-ba99-6894b8cbd612` → 5073 events, 04:26→19:00Z 2026-06-16.
> high-water: 019ed1ce-b7c3-769f-a5fd-005f77bca6a2  (2026-06-16T19:00:46Z agent_presence — last line of the frozen iter-7 window; next daily run resolves THIS event_id to its line and slices forward, per F41a)
</content>
</invoke>
