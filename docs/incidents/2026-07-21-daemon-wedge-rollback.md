# Incident + findings — 2026-07-21 daemon wedge, rollback to known-good

**Recorded by:** captain (keeper-restart resume session)
**Disposition:** service restored by rolling the live daemon back to the last-good tag. Issues below are documented for LATER review — NOT fixed this session (per operator: get something working first, don't fix inline).

> **CORRECTION (2026-07-22, post-assessor — this doc's original diagnosis was WRONG on two counts; do not trust the uncorrected claims below):**
> 1. **There was no crash.** The assessor (hk-msrpw) proved `daemon_shutdown` has zero emitters, so every restart records as `supervisor_revival cause=unexpected_exit`. The "two d59d5d32 exits" were both *deliberate* acts in our own comms stream (00:28 codexdriver restart, 02:01 the rollback itself). `hk-45pm7` was retracted by the admiral and dropped to P2 — NOT a defect.
> 2. **There was likely no "work-loop/crew-launch regression" (Issue 1 below is UNVERIFIED).** Crew-launch failing on d59d5d32 is best explained by **a964cbcb** ("isolate CLAUDE_CONFIG_DIR for claude:local", hk-8juwz) which lima live-proved breaks Claude auth ("Not logged in") and parks launches on a permissions modal — the modal it guarded against does not even render on claude v2.1.217. The stuck `main` queue I read as a "wedge" was a terminal `complete-with-failures` run (cleared by `queue cancel`, not a work-loop bug).
> **Real roll-forward target:** d59d5d32 **with a964cbcb reverted** (lima's candidate), NOT d59d5d32 as-is. Re-pin gated on the assessor's PASS on that reverted build.

## What happened

The live daemon (built from `d59d5d32`, branch `phase1-session-restart-substrate`) was **wedged**: it answered socket queries (`queue status`, `comms who/send`, `queue resume`/`pause` all ACKed) but its **work loop was dead** —

- `harmonik crew start <name>` returned a session_id and wrote the registry record, but **no tmux agent session was ever spawned** (boot-seed failed with "can't find session"). All crews (india, assessor) were down with only stale registry records.
- `harmonik queue resume main` reported "resumed" but the queue stayed `paused-by-failure`.
- The daemon pane was frozen at boot output (17:29:46); zero new log lines for any op.

**Fix applied:** rolled the daemon binary back to last-good tag **`daemon-20260718-01` (`894e2856`, "disable CrewIdleReaper")** via `docs/daemon-redeploy.md`. After respawn the new daemon **launched the admiral crew session in ~1s** and processed ops normally. Root cause is therefore somewhere in `894e2856..d59d5d32`.

## Issues to look at later (NOT fixed)

1. **Daemon work-loop / crew-launch regression in `894e2856..d59d5d32`.** Something in that range wedges the daemon's dispatch + crew-launch loop while leaving the socket responsive. Bisect before the phase1 branch (or `d59d5d32`) is redeployed. Candidate suspects in-range: the codex-first fence-drop (`hk-tckw3.1`), paste-inject buffer fix (`hk-9hvr0`), claude-reviewer tmux routing (`hk-qxvc2`), codex sandbox writable-roots (`hk-daegv`).

2. **Do NOT run crews on `HARMONIK_SUBSTRATE=codexdriver`.** It is untested and was never approved as the primary driver. The prior session left the daemon on codexdriver; that is reverted (rolled-back daemon is on the default claude/tmux substrate). Codex-as-driver stays behind the release gate.

3. **Release-rule violation risk (blocked).** The codex-first "Step-2 live proof" would have exercised unreleased codex work in production BEFORE the assessor/e2e gate. That is a release-rule violation — do not do it. `hk-tckw3.1` + `hk-9hvr0` remain OPEN; their code is only in the rolled-back `d59d5d32`, not in the running binary.

4. **macOS: `cp` of the Go binary breaks its code signature → SIGKILL (exit 137).** During the swap, `cp staged → /Users/gb/go/bin/harmonik` produced a binary the kernel killed on exec. Fix was `codesign --force --sign - /Users/gb/go/bin/harmonik`. The redeploy runbook should either build in place (`go install`) or codesign after any `cp`.

5. **~43 stale blocked queues had accumulated** (dead-crew + old canary/pi/gurney/frontline runs), all `paused-by-failure`/`paused-by-drain` with 0 pending/0 workers. Archived this session via `harmonik queue cancel`. Consider auto-reaping terminal/idle queues.

6. **`hk-8juwz` (pre-existing, known):** claude:local wedges on the theme/onboarding modal → `agent_ready_timeout`. The throwaway smoke bead `hk-tv6cg` hit this and paused the `main` queue.

7. **Branch divergence:** `phase1-session-restart-substrate` @ `d59d5d32` is 10 ahead / 2 behind `origin/main`, plus uncommitted tracked edits. Not touched. Note the running daemon binary (`894e2856`) is OLDER than both the branch and origin/main.

## Current good state (end of session)

- Daemon: `894e2856` (daemon-20260718-01), respawned by supervisor watchdog, socket healthy, work loop processing.
- Supervisor: running (restarted, `--watch-restart`).
- Admiral: back up, joined comms, running its boot loop.
- Queues: 0 blocked.
- Rollback point preserved: `/Users/gb/go/bin/harmonik.pre-rollback-20260721` (= `d59d5d32`).
