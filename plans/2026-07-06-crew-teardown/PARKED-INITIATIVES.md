# Captain's crew — parked initiatives (teardown 2026-07-06)

Torn down to clear the deck before the admiral's new initiative. Fresh missions will
be written when we revive. This captures **what each crew was working on** so we can
come back to it. Nothing was in-flight at teardown (fleet was HELD, zero runs) — no
work was orphaned.

Scope: **captain's crew only** — jessica, duncan, stilgar, watch. Left running:
captain (self), admiral + its research crews (shannon, schmidhuber = the new initiative).

---

## jessica — daemon-reliability + remote concurrency lane
- **Charter:** highest-impact P1 daemon/worktree reliability findings (logmine iter-20);
  the throughput critical path — crash-loops + the worktree-create race are what idle-hang
  the fleet, esp. the remote box (gb-mbp).
- **Landed (CLOSED):** hk-lt091 (empty-HEAD worktree race), hk-rnkuy (crash-loop +
  daemon-death EventType), hk-qe736 (worktree-leak reaper), hk-gf59k (ledger-dep
  false-defer), hk-xkou8 (bounded sess.Wait reap-hang fix), hk-hs7ex (concurrency-split:
  local hard-cap 4 vs remote capacity), **hk-5qp7z (concurrent remote worktree-create
  race — the last critical-path item, CLOSED)**.
- **State at teardown:** effectively DRAINED/idle — her critical-path work is done.
- **Open thread to revive:** the gb-mbp ramp itself. Remote was validated to max_slots:6
  but the concurrent-create race work reverted it to max_slots:1 + `worker disable gb-mbp`.
  Reviving means: re-enable gb-mbp, concurrent re-validate on the fix, ramp 1→6→10
  (operator's 10-concurrent target). Backlog caveat she flagged: only ~4 safe self-contained
  beads in the top-60 ready — sustained remote saturation needs a real work supply.

## duncan — eval-metrics WS1 (token-parser tail)
- **Charter:** eval-metrics WS1 (epic **hk-9jdid**, cross-model run-log metrics plumbing) —
  extract per-run token totals for the codex + pi harness parsers, per-node attribution.
- **Landed (CLOSED):** WS1a (model_selected event), WS1b (sessiondata hook), WS1c
  (codex token parser), WS1d (pi token parser), **WS1e hk-eval-prog-per-node-attr-tqftl
  (per-node time+token attribution — CLOSED)**.
- **Open thread to revive:** **epic hk-9jdid is still OPEN** (assignee duncan). WS1's
  token/attribution children have landed; confirm whether the epic has remaining scope
  (WS2/WS3 metrics) or is ready to close on revive. Spec: `plans/2026-07-03-eval-program/`.

## stilgar — eval-metrics WS1 plumbing + keeper reliability
- **Charter (most recent):** eval-metrics WS1 plumbing (epic hk-9jdid) — B1 model-on-log,
  B2 sessiondata post-run hook; earlier, keeper reliability + sleep-teardown.
- **Landed (CLOSED):** hk-eval-prog-model-on-log-bh2o7 (B1), hk-eval-prog-sessiondata-hook-vmxrk
  (B2), hk-5266t (keeper doctor tmux-pane check — the recurring watch-wedge root cause),
  hk-xjr1n (sleep tears down scheduler/crons + keeper stop-hook), hk-qe736 (prior).
- **State at teardown:** FREE/idle-armed — between lanes.
- **Open thread to revive:** no owned open bead; was a free slot awaiting the next ranked lane.

## watch — always-on triage tier (infra, not a project lane)
- **Charter:** always-on Sonnet triage session (epic hk-8yh32) — consume bus + ops-monitor
  + crew status, record to ledger, escalate only actionable items to captain event-driven.
- **State at teardown:** healthy but on a known flaky loop — recurring idle-submit-wedge
  ~every 2h (root cause **hk-5266t** landed but needs a keeper-binary redeploy to take
  effect for live keepers; auto-recovery gap **hk-ghcqn**, P2 open). Captain was hand-nudging
  it. **Not a project initiative** — it's monitoring infra; re-establish fresh alongside the
  new fleet. Note: a wedged/absent watch does NOT blind the captain (captain's own Monitor
  watchers read the bus directly).

---

## Also torn down / noted
- **flywheel** tmux session — dead (Pi-harness superseded it); torn down.
- **Left alone:** `-default` spawn session (NEVER kill — fork-bomb risk), `ctx-watchdog`
  (keeper watchdog infra), captain, admiral, shannon, schmidhuber.
- **Paused-by-failure queue cruft** (crashrepro, gbmbp-*, leto-*, pi-*, sandbox-q, etc.)
  = pre-existing, left as-is.

## Reference for revival
- Lane history: `.harmonik/context/captain-lanes.md` (the "CURRENT TRUTH" blocks).
- Old mission files preserved under `.harmonik/crew/missions/{jessica,duncan,stilgar,watch}.md`.
