# logmine — Wave 1 Findings (run 2026-06-09)

Crew **liet** · epic **hk-mhmaw** · queue **liet-q** · window **2026-06-08 → 2026-06-09 (last ~24h)**.

Method: 8 read-only sub-agents over distinct log slices (events.jsonl ×4, comms bus, daemon
stdout+supervisor, sub-agent transcripts, git churn+qa-scratch). Window ≈ 3,193 events,
473 comms messages, 83 commits, 70 worktree transcripts, 277KB daemon log. Every finding is
deduplicated across slices and anchored to a durable artifact (event_id / file:line / commit
sha). Triangulated (≥2 independent signals) findings are marked **[T]**.

## Register (prioritized)

| ID | Finding | Pri | Lane | Triangulated |
|----|---------|-----|------|--------------|
| F1 | Reconciliation detects but never repairs `bead_inprogress_queue_absent` → orphaned beads | P1 | stilgar (daemon) | [T] A2+A4+A1 |
| F2 | Daemon-restart cancellation inflates false-fails; landed beads re-dispatched + marked failed | P1 | stilgar (daemon) | [T] A1+A4+A5+A8 |
| F3 | `event_id` reused across paired pause events → violates N3 dedupe contract | P1 | stilgar (daemon/comms) | A4 |
| F4 | pause/resume CLI leaks flag token (`--queue`/`--help`) as `queue_name` | P1 | stilgar (daemon CLI) | A4 |
| F5 | Stale per-queue active-marker poisons a queue (`queue_already_active -32010`) | P1 | stilgar (daemon) | A5 |
| F6 | No `reconciliation_completed` event → stuck reconciliation undetectable | P2 | stilgar (telemetry) | A2 |
| F7 | `reviewer_launched` stopped emitting after a 06-08 redeploy (telemetry regression) | P1 | stilgar (telemetry) | A3 |
| F8 | `binary_commit_hash` always "unknown" + `daemon_config` omits workflow-mode/concurrency | P2 | stilgar (telemetry) | A4 |
| F9 | REQUEST_CHANGES fix-up w/ zero new commit dies as generic no_progress, obscuring cause | P2 | stilgar (daemon/review) | [T] A3+A1 |
| F10 | orphan-sweep `stale_intents_observed` grows unbounded (965→1012), never GC'd | P2 | stilgar (daemon) | A4 |
| F11 | Unguarded `--from` identity on comms → two-captains conflict froze work ~37min + mis-sends | P1 | stilgar (comms) + process | A5 |
| F12 | Comms delivery latency → false STALLED / stale-ceiling misreads (compounds hk-24xn1) | P2 | stilgar (comms) | A5 |
| F13 | Ownership-ambiguity round-trips; Gap-1 `--assignee` mirror not consistently consulted | P2 | process + telemetry | A5 |
| F14 | Diagnosis-method failure: hand-grep events.jsonl by run_id → 6 refuted root causes, ~18h burned | P1 | liet (doc/skill) | [T] A5+A8 |
| F15 | Real bead re-dispatched >2× as wedge canary, violating project rule (hk-w6y70 ×5) | P1 | liet (process/doc) | [T] A1+A5+A7 |
| F16 | CI Tier-2/Tier-3 lanes set `continue-on-error` to silence emails; reds still open | P1 | liet (CI) | A8 |
| F17 | Real-daemon smoke validation thrashes main trunk (6 commits + 3 cleanups net zero); no scratch lane | P2 | liet (workflow) | A8 |
| F18 | Worktree implementers reach for repo-root `.harmonik/` not worktree-local → "File does not exist" | P1 | liet (skill/launch-ctx) | A7 (28/70 transcripts) |
| F19 | Exit-137 SIGKILL on full daemon/scenario `go test` inside implementer budget | P2 | liet (skill/doc) | A7 (7/70) |
| F20 | Edit fails on aligned-unicode comment tables ("String to replace not found") | P3 | liet (tooling note) | A7 (2/70) |
| F21 | Log-noise classes crowd out diagnostics (347 / 142 / 83 / 66 occ) | P2 | stilgar (daemon) | [T] A4+A6 |
| F22 | Ops hygiene: 3 orphaned supervisor procs + live `-c6` vs on-disk `MAXC=8` drift | P2 | surface-to-captain | A6 |

Lane key: **stilgar** = daemon/comms code (do not edit concurrently from liet-q — file + ping captain).
**liet** = docs/skills/process/CI that liet-q can dispatch without colliding with the daemon lane.

### Filed beads (Wave 2, 2026-06-09)

Daemon lane (`codename:logmine,crew:stilgar` — digested to captain, NOT dispatched by liet):
F1→hk-m3ydd · F2→hk-ly0hg · F3→hk-hggxx · F4→hk-i09r9 · F5→hk-qkahq · F6+F8→hk-mptxw ·
F7→hk-c73fs · F9→hk-m1wqp · F10→hk-cizvu · F11→hk-z0f02 · F12→hk-5xuvc · F21→hk-kqnay

liet lane (`codename:logmine,crew:liet` — dispatched to liet-q in Wave 3):
F13→hk-a4pq3 · F14→hk-ej8j7 · F15→hk-a0yyr · F16→hk-4mten · F17→hk-nk9pu · F18+F19+F20→hk-rpk5k

Wave-3 dispatch note: F14/F15/F17/F18 are clean liet-ownable doc/skill/process edits → dispatch first.
F16 (CI reds) and F13 (owner-in-events sub-item) carry daemon-lane risk → held for sequencing /
possible reroute to stilgar.

---

## Detailed findings

### F1 — Reconciliation detects but never repairs `bead_inprogress_queue_absent` [T] (P1, stilgar)
Reconciliation observes the dominant mismatch class every hour but has no repair path, so beads
sit orphaned in `in_progress` until a human salvages them with a cherry-pick.
- **Evidence:** 13/13 `reconciliation_mismatch_observed` in window are class `bead_inprogress_queue_absent`
  (ledger `in_progress`, queue `""`). Only **3** `queue_item_reconciled` events fired (reason
  `claim_write_lost`, beads hk-3kyh3/hk-y01k6/hk-rgxwd at 06:44:29). hk-w6y70 hit the mismatch on
  **4 consecutive hourly passes** (16:47, 17:12, 17:59, 18:50) without repair → salvaged manually
  via cherry-pick `a11338fb`. hk-5tg5o same pattern → salvaged `16a2cbd4`.
- **Blast radius:** 9 distinct beads (codex/captain crews); detection vastly outpaces repair.
- **Root cause (hypothesis):** a run wedges/crashes after claiming a bead (sets ledger `in_progress`)
  but the queue entry is lost; reconciliation detects but only the `claim_write_lost` path repairs.
  Other in_progress-orphans have no auto-repair → manual salvage.
- **Fix direction:** extend reconciliation to repair (re-queue or reset-to-open) every
  `bead_inprogress_queue_absent` it observes, not just `claim_write_lost`.

### F2 — Daemon-restart cancellation inflates false-fails [T] (P1, stilgar)
Rapid binary redeploys during the spawn-wedge firefight cancelled in-flight runs mid-node; the run
is marked `run_failed` even though the bead's commit already landed on main, then re-dispatched
(hitting noChange) and recorded as a *fresh* fail.
- **Evidence:** 14 of 35 `run_failed` are `dot: ... context cancelled` (12 `implement`, 2 `commit_gate`);
  every ctx-cancel timestamp matches a `daemon_started` within seconds (16:47:2x↔16:47:28, 17:58:50↔17:59:23,
  18:50:21↔18:50:54, 00:57:06↔00:57:09, 06:44:1x↔06:44:21). **~24 false-fail events across 12 beads**
  whose `Refs:` commit predates the fail (hk-3kyh3 failed 5× after commit `27034656`@06:22; hk-y01k6
  4× after `c0021bc4`@06:19; hk-w6y70 4× after `c69ead02`@17:04). 14 daemon restarts in 24h.
- **Blast radius:** 12+ beads; inflated fail counts, wasted slots, ownership confusion (see F13).
- **Fix direction:** (a) restart-during-flight should re-queue cleanly (pending), not `run_failed`;
  (b) pre-screen-for-landed-work at dispatch (already filed **hk-lhv8i** — verify/prioritize).
- **Note:** only **2** genuine no-work fails in window (hk-s2psr, hk-yag0s — zero matching commits,
  cancelled at the 06:44 restart before producing work). Clean re-dispatch candidates.

### F3 — `event_id` reused across paired pause events → N3 dedupe violation (P1, stilgar)
- **Evidence:** `operator_pause_status` emits both `pausing` and `paused` at the same/adjacent ms;
  in **4 of 5** transitions the two records share the SAME `event_id` (e.g. `019eac82-38b4` at
  13:11:15.891Z `pausing` AND .892Z `paused`; same for `019eac9b-d499`, `019eacc2-3488`, `019ead20-ce29`).
- **Impact:** any consumer honoring the NORMATIVE N3 dedupe-on-`event_id` contract drops the `paused`
  record → downstream views stick at "pausing".
- **Fix direction:** mint a distinct `event_id` per emitted event, not per operation.

### F4 — pause/resume CLI leaks flag token as `queue_name` (P1, stilgar)
- **Evidence:** across 10 `operator_pause_status` + 5 `operator_resuming`, `queue_name` = `--queue`
  (13×), `--help` (1×), `fwkeeper` (1×). 14/15 carry a flag literal instead of a real queue name.
- **Impact:** wrong queue attribution on pause/resume (effect may be cosmetic but target is wrong).
- **Fix direction:** arg-parse bug — read the flag's *argument*, not the flag token.

### F5 — Stale per-queue active-marker poisons a queue (P1, stilgar)
- **Evidence:** `12:20:17 controlpoints→captain`: queue 'cp' "POISONED — list shows empty but
  dry-run/submit returns `queue_already_active (-32010)`, stale active-marker"; broadcast advisory at
  12:22:37; recurred 17:19 (hk-tcenh submit blocked).
- **Impact:** queue becomes unusable; `queue status` and `queue submit` disagree; wasted round-trips.
- **Fix direction:** clear the per-queue active-marker when a run is killed/wedged (reconciliation or
  kill-path cleanup).

### F6 — No `reconciliation_completed` event (P2, stilgar/telemetry)
- **Evidence:** 47 `reconciliation_started` in window (33 hourly, 14 startup), **0** completion events;
  0 across all of events.jsonl. Results piggyback `daemon_orphan_sweep_completed`.
- **Impact:** a hung/crashed reconciliation is indistinguishable from a clean one — F1's stuck recon
  is undetectable from the log.
- **Fix direction:** emit `reconciliation_completed` with mismatch/repair counts.

### F7 — `reviewer_launched` stopped emitting after a 06-08 redeploy (P1, stilgar/telemetry)
- **Evidence:** 18 `reviewer_launched` vs 35 `reviewer_verdict`; **17 verdicts have no launch event**
  (e.g. run 019ea92f-8a2f, 019eac1c-9c33). Clean time boundary: last launch 06-08T11:13:28; first
  launch-less verdict 06-08T14:43:05 → extends to 06-09T12:25:22. Verdicts carry full payloads, so
  reviews DID run — this is telemetry loss, not a gate gap.
- **Impact:** verdict latency unmeasurable for 17 runs; review observability degraded.
- **Fix direction:** restore the `reviewer_launched` emit regressed by the 06-08 redeploy.

### F8 — `binary_commit_hash` "unknown" + `daemon_config` omits key flags (P2, stilgar/telemetry)
- **Evidence:** all 14 `daemon_started` carry `binary_commit_hash:"unknown"`; all 14 `daemon_config`
  carry only `{forbid_unprotected_default, target_branch}` — no workflow-mode / max-concurrent /
  auto-pull.
- **Impact:** config-drift across restarts is invisible in the log — exactly the blind spot that fed
  the hand-grep diagnosis thrash (F14). Cannot verify the `--workflow-mode review-loop` pin post-rebuild
  from events.
- **Fix direction:** inject commit hash via ldflags; serialize workflow-mode + concurrency + auto-pull
  into `daemon_config`.

### F9 — REQUEST_CHANGES fix-up with no new commit dies as generic no_progress [T] (P2, stilgar)
- **Evidence:** run 019eac1c-9c33 (hk-zblnu) REQUEST_CHANGES → re-dispatch → `no_progress_detected`
  → `run_failed`. run 019eadd1-8528 (hk-cvg1j) REQUEST_CHANGES (`spec-field-name`) → re-dispatch →
  REQUEST_CHANGES again (same flag) → `no_progress_detected` → `run_failed`. All 6 `no_progress` are
  dot iter-2, `diff_hash_current == diff_hash_prior`.
- **Impact:** the real cause (implementer couldn't satisfy the reviewer; bead/spec lacked the field
  the reviewer expected) is hidden behind a generic no-commit failure class.
- **Fix direction:** distinct "review fix-up stalled" failure class so triage sees the reviewer flag.

### F10 — orphan-sweep `stale_intents_observed` grows unbounded (P2, stilgar)
- **Evidence:** 14 `daemon_orphan_sweep_completed`; `stale_intents_observed` rises monotonically
  965 → 1012 (+47) over window while every other counter stays 0. No retirement path.
- **Impact:** accumulating-state leak in the intent ledger; benign today, unbounded over time.
- **Fix direction:** GC retired intents (likely a sub-task of F1's reconciliation repair work).

### F11 — Unguarded `--from` identity on comms bus (P1, stilgar/comms + process)
- **Evidence:** `04:22:27 codexcrew→captain`: "my earlier broadcast 019eab71 was mis-sent under
  --from captain — disregard". 8h later the **two-captains** conflict: `12:35:32 captain→*`
  "there are TWO captains ... already given CONFLICTING orders (the CI hold-vs-push that landed
  dce82d03)"; freeze on daemon-restart / hk-togxq-deploy / force-push held ~12:35→13:12 (~37 min),
  resolved only by operator `13:09 SOLE-CAPTAIN` + `13:12 COMMAND: SECOND CAPTAIN TERMINATED`.
- **Impact:** `--from` is free-text with no per-session binding; two sessions can both claim a name and
  issue conflicting orders; froze irreversible work (incl. the P0 hk-togxq deploy) for ~37 min.
- **Fix direction:** per-session identity binding / warn when two live sessions claim one name
  (code); + a process note that operator arbitrates identity conflicts.

### F12 — Comms latency → false STALLED / stale-ceiling misreads (P2, stilgar/comms)
- **Evidence:** operator flagged comms unreliable twice (`05:58:43`); concrete misreads: `06:04:52`
  "hk-pj4b6 is NOT stalled — your STALLED read = comms delay"; `06:12-06:14` `-c1` ceiling was a
  "STALE read (comms latency)". 14 latency/OFFLINE/stale-read references; ≥3 caused a wrong steer.
  Bus drops ~10-30s on every daemon revive.
- **Impact:** agents act on stale state; compounds **hk-24xn1** (daemon doesn't wake on submit — filed).
- **Fix direction:** lower delivery latency / surface delivery-confirmation; verify hk-24xn1 status.

### F13 — Ownership-ambiguity round-trips (P2, process + telemetry)
- **Evidence:** ≥4 "whose bead is this?" exchanges (`16:08`, `16:50`, `12:13`) over hk-w6y70 /
  hk-xdxws / hk-kbqto / hk-3kyh3. Wedge/fail events don't attribute the bead to a crew; the captain
  skill's Gap-1 `--assignee` mirror was not consistently consulted.
- **Fix direction:** carry owning-crew in run/fail/wedge events; reinforce the Gap-1 mirror habit.

### F14 — Diagnosis-method failure: hand-grep by run_id → 18h burned [T] (P1, liet doc/skill)
The headline meta-finding. The spawn-stall root cause flip-flopped **4+ times** (phantom → orphan →
CPU-contention → disk-full ENOSPC → flock → "no agent_ready" → actual: oh-my-zsh update prompt +
tapCh race), each "confirmed" then overturned.
- **Evidence:** captain's own `07:29:50` message: "root cause has flip-flopped 4x ... the flips are
  driven by METHOD: hand-grepping events.jsonl by run_id gives FALSE NEGATIVES." ~79 correction-flavored
  messages; re-attribution commit `eb12eb6b`; 4 sequential daemon fix attempts in git
  (`ec30b225`→`7cc0f0bc`→`5282816c`→`7641f648`→`53ead2aa` tapCh race) + postmortem `505ad77e`.
  Wall-clock ≈ the whole 15:00→09:00 window (~18h).
- **Fix direction:** formalize the "Major-issue fan-out protocol" into a doc/skill — on a MAJOR wedge,
  fan out 10-15 agents on DISTINCT angles + ≥2 adversarial verifiers that can OVERRULE a wrong
  synthesis; NEVER hand-grep events.jsonl by run_id (use `harmonik subscribe --json` / structured
  queries). This logmine pipeline is itself part of the answer.

### F15 — Real bead re-dispatched >2× as wedge canary [T] (P1, liet process/doc)
- **Evidence:** T2 **hk-w6y70** used as the spawn-wedge canary across multiple restarts: wedging at
  15:01, 16:45, 17:01 ("WEDGED AGAIN"), 17:31, then 17:38 ("NOT dead — both reached
  implementer_phase_complete, SLOW ~16min"). 5 transcripts / 4 commits (A7). Violates AGENTS.md
  "never re-dispatch a bead >2× without investigation".
- **Fix direction:** use a throwaway trivial smoke bead as the daemon canary, not a real
  captain-implementation bead; codify in orchestrator rules.

### F16 — CI lanes neutered with `continue-on-error`; reds hidden (P1, liet CI)
- **Evidence:** `dce82d03` added Tier-2 (`make check`) + Tier-3 (scenario) GitHub Actions lanes →
  `0ff24aff` set "continue-on-error on both lanes to stop operator failure-emails (interim)".
- **Impact:** CI is currently non-blocking; the underlying test reds are still open and masked.
- **Fix direction:** fix the reds, then remove `continue-on-error` so the gate blocks again.

### F17 — Real-daemon smoke validation thrashes main trunk (P2, liet workflow)
- **Evidence:** add/remove cycles `3a62c1fe`+`cf739cc1`→`8ff23042`; `6a6fcb7c`+`64a2a167`→`14661961`;
  `c438efda`+`4efcce1c`→`8083d2c6` — 6 smoke commits + 3 cleanups net to zero code on main.
- **Fix direction:** a smoke scratch lane / non-trunk verification path so concurrency-fix validation
  doesn't churn trunk history.

### F18 — Worktree implementers reach for repo-root `.harmonik/` (P1, liet skill/launch-ctx)
- **Evidence:** "File does not exist" in **28/70** transcripts, 38 occ; clearest: an implementer
  Read `/Users/gb/github/harmonik/.harmonik/agent-task.md` (shared main-repo file) instead of its
  worktree-local `.../worktrees/<id>/.harmonik/agent-task.md`. Self-recovered but one wasted cycle each.
- **Fix direction:** one-line note in the implementer launch context / agent-task header — "your files
  live under your worktree CWD; the repo-root `.harmonik/` is a different tree." (cf. worktree-cwd-slip memory.)

### F19 — Exit-137 SIGKILL on full daemon/scenario `go test` (P2, liet skill/doc)
- **Evidence:** `Exit code 137` in **7/70** transcripts, tied to `go test ./internal/crew/...` and
  scenario/daemon suites that boot real daemons and exceed the per-command timeout.
- **Fix direction:** reinforce in the implementer skill: run targeted fast gates, not full
  daemon-booting suites, inside the implementer budget (cf. scenario-test-authoring memory).

### F20 — Edit fails on aligned-unicode comment tables (P3, liet tooling note)
- **Evidence:** 2 transcripts / 3 occ; both tried to Edit the `bead_closed (EM-052 §4.12.6)` block
  with em-dash/box-drawing + aligned columns; "neither \uXXXX form matched". Self-recovered.
- **Fix direction:** Read exact bytes before Editing aligned-unicode tables, or anchor on a short
  ASCII substring. Minor.

### F21 — Log-noise classes crowd out diagnostics [T] (P2, stilgar)
- **Evidence:** `/tmp/hk-daemon.log`: `quit-on-commit: launch-heartbeat-timeout suppressed` ×347,
  `mergeRunBranchToMain: uncommitted changes in project working tree` ×142, QM-002b
  `bead_inprogress_queue_absent` ×83, `review.json absent (non-fatal)` ×25; events.jsonl:
  `session_keeper_no_gauge` ×66 from a `keeper-dogfood` test session on a fixed 125s cadence.
- **Impact:** real diagnostics are buried; `grep -i error|failed` returns ~50 mostly-benign hits.
- **Fix direction:** demote/sample these WARN classes; gate dogfood keeper spam behind a test flag.

### F22 — Ops hygiene: orphaned supervisor procs + concurrency drift (P2, surface-to-captain)
- **Evidence:** 3 live `hk-daemon-supervise` processes (PIDs 4456, 53986, 53991 — only 1 expected),
  racing the pidfile lock on every daemon exit; live daemon runs `--max-concurrent 6` while on-disk
  `/tmp/hk-daemon-supervise.sh:14` sets `MAXC=8` (stale running process vs reconstructed script).
- **Action:** NOT a code bead — surface to captain/operator; touching the live daemon/supervisor is
  stilgar/operator lane. Reap to one supervisor; re-launch if 8-wide is intended.

---

## Counter-findings & already-tracked (note, do NOT file)

- **Review gate is holding:** 0 unreviewed merges in window; all 28 closed beads carry an APPROVE
  verdict (A3). The 06-01→06-02 review-loop-default-outage class did NOT recur.
- **Implementer fleet is healthy** (A7): genuine command-failure rates low (≤4 `is_error`/transcript);
  big raw `FAIL`/`exit 17`/`stale` keyword counts are mostly spec-doc prose + implemented error-path
  strings, NOT agent mistakes. Do not over-index on raw keyword tallies.
- **Ledger-dep gating is functioning, NOT a deadlock this window** (A1+A2): 46 deferrals on real
  task→task deps (not bead→open-epic); blockers later landed. The open-epic parent-child deadlock
  class was NOT observed. Minor: deferrals re-emit identically across re-submits (no dedup) — P3 noise.
- **No daemon crashes/panics/ENOSPC** in the 277KB log (A6); `SIGKILL/SIGSEGV/FAIL` strings are
  scenario-test fixtures (`hk-cun4l-exit_*`). The pidfile-collision storm A6 found is dated **06-01**
  — out of window, historical; the supervisor backoff handled it correctly.
- **Already filed (verify, don't duplicate):** hk-lhv8i (pre-screen landed work at dispatch),
  hk-24xn1 (daemon doesn't wake on submit), hk-tcenh (daemon infinite-wedge budget bug).
- **Open design decision (note):** `docs/qa-scratch/hook-wakeloop-proto.md` — Stop-hook wake-loop has
  a no-graceful-interrupt blocking constraint; doc recommends persistent-daemon + polling instead.
- **Reusable gotcha:** `t.Parallel()` on tests touching `~/.claude.json` causes lock contention
  (fixed `27034656` by removing t.Parallel) — fold into test-writing guidance.
</content>
</invoke>
