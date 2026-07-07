# BUGS.md — running defect capture (quality-system initiative)

> **Purpose (operator 2026-07-07):** a low-friction scratchpad where the admiral, captain, and crews
> jot down bugs/defects/friction they hit WHILE building the fleet, so nothing is lost. This is NOT the
> bead ledger — it is the pre-triage inbox. Periodically we triage these into beads (`br create`),
> classify severity (per `plans/2026-07-06-quality-system/07-assessor-severity-framework.md`), and fold
> the real ones into the growing test-validation system as regression corpus scenarios.
>
> **Discipline:** append a one-block entry the moment you hit something. Keep it terse. Don't stop to
> fix formatting. Fields: what broke · where (file/command/subsystem) · impact · workaround (if any) ·
> candidate test-lane it belongs to (dispatch / keeper / comms / sandbox / remote / review-loop / other).
> Mark `→ bead hk-xxxxx` once triaged; mark `→ corpus <scenario>` once a regression test exists.

---

## Open (untriaged)

### B1 — `comms recv --agent` silently drains 0 when a `--follow` watcher is armed
- **Where:** `harmonik comms recv` / eventbus cursor. **Subsystem:** comms.
- **What:** an armed `comms recv --follow` advances the consumer cursor first, so a subsequent
  `comms recv --agent <me>` returns nothing — looks like an empty inbox / a stall, but messages WERE
  delivered. Real usability trap: observers conclude "no messages" and mis-diagnose a stall.
- **Impact:** medium — causes false "comms is down / I have no work" conclusions; the operator flagged
  "there seems to be an issue with comms" partly from this shape.
- **Workaround:** observe the bus via `harmonik comms log --since <window> --json` (reads the ledger
  directly, cursor-independent) instead of `recv` when you just need to SEE traffic.
- **Candidate lane:** comms.

### B2 — comms presence ages out at ~120s → live crews show `stale`/offline in `comms who`
- **Where:** `harmonik comms who` presence TTL. **Subsystem:** comms.
- **What:** an idle-but-alive crew (armed `--follow`, not sending) drops out of `comms who` after ~120s;
  receiving does NOT refresh presence. A healthy crew reads as a ZOMBIE/offline.
- **Impact:** medium — false zombie signals drive unnecessary reconcile/restart churn (see the watch
  restart loop, B3).
- **Workaround:** confirm liveness via `tmux capture-pane` (pane-truth), not `comms who` alone.
- **Candidate lane:** comms.

### B3 — `watch` crew re-stalls repeatedly with no self-healing path
- **Where:** watch session + ops-monitor. **Subsystem:** keeper / comms / lifecycle.
- **What:** the watch session goes stalled/down every ~5–25 min; ops-monitor fires escalating alerts
  (#2…#32, "no self-healing path") but nothing recovers it automatically — each recovery costs a manual
  captain restart, inverting the watch's purpose. Root cause historically = keeper can't inject a
  restart into a mangled tmux target (hk-5266t class) + no auto-recover hook.
- **Impact:** high (recurring toil + noise drowns real alerts).
- **Workaround:** captain hand-restarts (`harmonik start crew watch`, never `crew stop`).
- **Candidate lane:** keeper + comms.

### B4 — keeper `restart-now` / restart cycle glitches (this session's re-boot)
- **Where:** `harmonik keeper restart-now` / keeper watcher. **Subsystem:** keeper.
- **What:** the admiral keeper cycle glitched mid-restart this session (operator: "the keeper must have
  fucked up") — the admiral had to be re-booted manually via `harmonik agent brief`. Related filed
  defect `hk-pp1in`: restart-now aborts with `no_tmux_target` despite a healthy pane-bound watcher.
- **Impact:** high — a glitched keeper restart can strand a long-lived orchestrator (captain/admiral).
- **Workaround:** manual `harmonik agent brief --agent <name>` re-boot.
- **Candidate lane:** keeper. **→ partial bead hk-pp1in.**

### B5 — per-bead / DOT integration-branch targeting is dead code
- **Where:** `LandsOn` / `landTaskBranch` in `internal/daemon/workloop.go`. **Subsystem:** dispatch.
- **What:** work can't be directed to a specific integration branch; the daemon merges daemon-wide to
  the configured `lands_on` (default main). The designed per-bead/per-crew integration-target path is
  written but never wired into the live merge — dead code. This is WHY the Phase-1 build uses the
  C-model workaround (crew commits harness to its own branch, daemon executes the matrix only).
- **Impact:** high going forward (operator: CRITICAL) — blocks per-crew integration branches.
- **Workaround:** option C (crew commits harness directly to its integration branch in its own worktree).
- **Candidate lane:** dispatch. **→ bead hk-lgykq (P1); tested by hk-xke2i (known-RED matrix cell).**

### B6 — `harmonik start` only knows `captain|crew` (assessor must route via `crew start`)
- **Where:** `cmd/harmonik/start.go` hardcoded switch. **Subsystem:** lifecycle/launcher.
- **What:** `harmonik start assessor` is not a thing; the assessor spawns via `harmonik crew start
  assessor` (works because `ResolveType` resolves the bare type folder). Minor, but a launch-ergonomics
  gap once more manifest-typed roles exist.
- **Impact:** low. **Workaround:** use `crew start`. **Candidate lane:** lifecycle.

### B8 — re-tasking an idle remote-control crew via tmux paste wedges (input not submitted)
- **Where:** `tmux send-keys -l` into a crew's `--remote-control` pane. **Subsystem:** crew-launch / remote-control.
- **What:** a long directive pasted into an idle crew pane gets captured as a *collapsed paste*
  ("paste again to expand" → "Press up to edit queued messages") and the following `Enter` fails to
  submit it — the crew stays idle, the directive lost/queued-but-not-run. Hit this re-tasking shannon
  onto the comms-test lane; multiple retries with delays didn't reliably submit.
- **Impact:** medium — makes hand-re-tasking my own idle crews unreliable; wasted several cycles.
- **Workaround:** prefer a clean `harmonik crew start <name> --mission <file>` re-seed (the launcher
  seeds+submits reliably) over hand-paste; or keep directives short. schmidhuber woke fine with the
  same pattern, so it's a paste-timing race, not length alone.
- **Candidate lane:** keeper / lifecycle (crew-launch reliability).

### B7 — UBS pre-commit bug scanner requires bash ≥4.0; macOS ships 3.2 → hook errors every commit
- **Where:** UBS install / pre-commit path. **Subsystem:** tooling (not product).
- **What:** every `git commit` prints a UBS bash-version error before completing. Non-blocking (commit
  still succeeds) but noisy.
- **Impact:** low (dev ergonomics). **Workaround:** `brew install bash`. **Candidate lane:** other/tooling.

### B9 — fleet-wide simultaneous "context cancelled during node implement" wave, hits multiple queues
- **Where:** daemon DOT-workflow execution ("implement" agentic node). **Subsystem:** dispatch/daemon.
- **What:** at 2026-07-07T07:14:2x, ALL in-flight runs across MULTIPLE UNRELATED queues (jamis-q's
  hk-420yr.1/.2/.3 AND a different queue's hk-4hnvi/hk-9cqtm) failed within the same ~1.5s window, all
  with `dot: agentic node "implement" failed: context cancelled during node "..."`. A second, distinct
  wave hit yet another queue (yueh-q: hk-mpel5/vm8ym/7n6o7/x8fc6/n0wb0) ~90s later, same error shape.
  Daemon pid/socket were stable across both waves (no restart observed) — this isn't the daemon dying,
  it looks like something is issuing a cancel to in-flight agentic-node contexts fleet-wide, periodically.
- **Impact:** high if recurring — silently kills concurrent work across crews, forces resubmission,
  wastes the elapsed build time (my B1/B2/B3 were ~25min in when cancelled).
- **Workaround:** classify as transient, resubmit the failed batch. No root-cause fix identified yet.
- **Candidate lane:** dispatch (daemon-core) — worth a major-issue fan-out if it recurs a 3rd time.
- **UPDATE (same session, ~30min later):** jamis-q's B1/B2/B3 hit the IDENTICAL error again on the
  RESUBMITTED batch, all three failing within the same second (07:45:2x), after ~30min elapsed run
  time (first wave was ~25min elapsed). Two independent occurrences on two different bead sets, both
  cancelled at roughly the 25-30min mark of the "implement" node — this now looks like a **timeout**,
  not a random cancel-broadcast. Candidate root cause: an agentic-node or DOT-workflow context timeout
  set to ~25-30min that's too short for these tasks, OR a resource/concurrency reaper killing
  long-running contexts. Per crew-launch protocol (fails-twice rule), jamis is NOT re-dispatching a
  3rd time — escalated to captain via `--topic error`, awaiting instructions. Worth a major-issue
  fan-out per the fanout skill's "root cause refuted/recurred ≥2x" trigger.
- **ROOT CAUSE IDENTIFIED (2026-07-07, hk-8gixi):** The never-spawned reaper in `stalewatch.go`
  fires when `launch_initiated` was seen but `agent_ready` was NOT seen within
  `neverSpawnedReaperDefaultTimeout = 30 * time.Minute`. With all runs launched in the same dispatch
  wave, every run hits the 30-min deadline in the same 30s scan tick — explaining the ~1.5s window
  where ALL runs cancelled simultaneously. The root cause of `agent_ready` remaining unseen in a
  live-running implement node is a separate edge case (the hk-wths fix using `EmitWithRunID` in the
  `SetAgentReadyCallback` DOT cascade path was supposed to close this; the edge case that let B9
  slip through is not fully understood without runtime logs from the incident).
- **FIX (hk-8gixi):** Added `never_spawned_timeout=<seconds>` per-bead label override (mirrors the
  existing `stale_after=<seconds>` pattern). Operators can label long-running DOT beads with e.g.
  `never_spawned_timeout=7200` to raise the ceiling to 2 h without touching daemon config or
  triggering a live redeploy. The global `neverSpawnedReaperDefaultTimeout` (30 min) is unchanged —
  per-bead override only. Implemented in `internal/daemon/stalewatch.go`; new helper
  `beadNeverSpawnedTimeout()` reads the label; `checkRun` initialises `runStaleState.neverSpawnedTimeout`
  from the bead's `RunHandle.Labels`; both reaper checks use the per-run value. 8 unit tests in
  `stalewatch_neverSpawnedTimeout_hk8gixi_test.go` cover label parse, fallback, and the integration
  path end-to-end.

---

## Triaged / folded into corpus
*(none yet — move entries here with their `→ bead` / `→ corpus` markers once done)*

## hawat / hk-lgykq — 2026-07-06
- **ubs unusable on macOS default shell:** `ubs` hard-fails "requires bash >= 4.0 (you have 3.2.57)". macOS ships bash 3.2; must invoke via `/opt/homebrew/bin/bash /Users/gb/.local/bin/ubs`. The CLAUDE.md UBS quick-ref gives the bare `ubs` command, which no agent on stock macOS can run. Fix candidate: wrapper shim or doc note.
- **Latent bug found in situ (now fixed by hk-lgykq):** before this fix, a bead with a `## Branching target_branch: integration/X` directive was REBASED onto integration/X (baseBranch, reviewloop.go/workloop.go:~3668/4060) but MERGED into deps.targetBranch (main). Rebase base and merge target were inconsistent. The hk-lgykq wiring aligns them.
- **Stale integration branch stub:** `integration/lgykq-targeting` already existed (0 commits ahead of origin/main, checked out nowhere) from a prior hawat session; `git worktree add -b` fails on it. Had to `git branch -f ... origin/main` then `worktree add` without -b. Minor.

### B10 — worktree-create empty-HEAD race hits piter-q T1 beads (2nd failure, distinct from B9)
- **Where:** daemon workspace/worktree-create path. **Subsystem:** dispatch/daemon-core.
- **What:** `hk-4hnvi` and `hk-9cqtm` (piter-q, keeper-test-harden T1) both failed a second time (first
  was the B9 fleet-wide context-cancel wave) with `WorktreeCreationFailed: git worktree add exited 0 but
  HEAD did not resolve ... (concurrent remote create race)`. Referenced existing bead `hk-iaj1w`.
- **Impact:** blocks T1 dispatch a second time; two-strikes rule (crew-launch mission) means I escalate
  to captain instead of self-resubmitting again.
- **Workaround:** none identified by me; escalating to captain per mission (twice-failed → error topic).
- **Candidate lane:** dispatch (daemon-core) — see existing hk-iaj1w.

### B11 — bead marked `closed` (success) but its commit never landed on main (false-close)
- **Where:** daemon merge/close path, `lands_on`-default merge. **Subsystem:** dispatch/daemon-core.
- **What:** hk-420yr.4 (B1a, DOT verdict-parsing happy-path split) reported `run_completed` success and
  `br show` returned `status: closed` — but its commit (`63b5c2efe`, "B1a happy-path acceptance") is
  stranded on an orphaned `run/019f3bc3-f5d5-...` branch and is NOT an ancestor of `main`. The other 5
  of 6 sibling splits (hk-420yr.5/.6/.7/.8/.9) genuinely landed on `main` (confirmed via
  `git merge-base --is-ancestor`). This is the exact false-close failure class my own B2b sub-batch's
  reconcile-scanner test suite was written to guard against (hk-vbv3b/whru3 family) — ironic that B1a
  hit it live in the same session. Also note: per BUGS.md B5, none of these 6 splits landed on the
  intended `integration/subsystem-proofs` branch — the daemon's per-bead branch targeting is dead code,
  so ALL 6 merged straight to `main` (bypassing the mission's planned assessor-gated PR step).
- **Impact:** high — a bead can show `closed`/success while its code silently never merges; any
  automation trusting bead status without checking the merge commit will believe test coverage exists
  that doesn't. Also means "assessor gate at branch→main PR" was structurally bypassed here since there
  was no PR step at all.
- **Workaround:** jamis is re-authoring B1a's happy-path test content fresh and re-submitting rather than
  trying to recover/cherry-pick the orphaned commit (simpler, and re-proves it lands this time).
- **Candidate lane:** dispatch (daemon-core) — closely related to hk-lgykq (B5) and worth pairing with
  B2b's reconcile-scanner work as a regression case (false-close on a real repo, not just a fake).

### B11 — recurring daemon-restart silently reverts in-flight bead to open/unassigned, no run_failed event
- **Where:** daemon lifecycle / queue dispatch. **Subsystem:** dispatch/daemon-core.
- **What:** hk-5wadr (piter-q, keeper-test-harden T2) was silently reverted from dispatched->open,
  unassigned, THREE separate times across this session, each time coinciding with a daemon
  socket-down/restart window. No run_failed/run_stale event was ever emitted for these reverts (unlike
  the B9 context-cancel wave, which DOES emit run_failed) — the queue just reports "no queue active"
  after the daemon comes back, and `br show` shows the bead open with no failure note.
- **Impact:** crews must poll queue+bead state after every daemon restart and blind-resubmit reverted
  beads; there is no event to react to, so a `subscribe --types run_failed` watcher alone misses this
  class entirely (must diff queue state before/after each daemon blip instead).
- **Workaround:** after any daemon restart, re-check `br show <bead>` for beads that were `dispatched`
  and are now `open`/unassigned with no failure note, and resubmit.
- **Candidate lane:** dispatch (daemon-core) — restart/recovery should either resume the in-flight run or
  emit an explicit run_failed/requeued event, not silently drop it.
