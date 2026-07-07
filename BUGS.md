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

---

## Triaged / folded into corpus
*(none yet — move entries here with their `→ bead` / `→ corpus` markers once done)*
