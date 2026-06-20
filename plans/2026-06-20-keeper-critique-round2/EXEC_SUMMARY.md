# Keeper Critique Round 2 — Synthesis (current post-fix state)

**Date:** 2026-06-20. 7 critics on the post-fix tree (after `4128d760`/`3f88fae5`/`b43638ca`/`95135107` + daemon/captain-keeper redeploy). Reports `01`–`07`.

## Headline
The round-1 *logic* fixes shipped and hold (suite GREEN, coverage up to 76.3%, the "certifies-the-bug" smell is gone). The remaining risk has **shifted from cycle-logic to operations + the open loop**: the keeper now *decides* well but still *acts blindly*, and **the watcher that does the acting has nothing keeping it alive**.

## Findings, ranked (severity × blast-radius), with the smallest fix

| # | Finding | Critics | Sev | Smallest fix |
|---|---|---|---|---|
| **W1** | **Per-agent keeper has NO supervisor** → dies & stays dead (this is why the captain keeper needed a hand-relaunch today). The `ctx-watchdog` net *explicitly skips the captain*. | 06, 07 | **HIGH × FLEET** | `while…sleep 5…done` self-heal shim around the per-agent keeper launch (mirror `hk-keeper.sh`) |
| **W2** | **Automatic cycle still fire-and-forget** — `/clear`(`cycle.go:884`)+`/session-resume`(`:913`) injected, `cycle_complete`(`:919`) set without pane read-back. `await-ack` primitive exists but isn't called. | 01, 05, 07 | inject `AckLine(cycleID,"cycle")` before `/clear`, `AwaitAck` via an injectable capturer in the shared `runCycle` tail (covers `RunForPrecompact` too); abort instead of fake-complete |
| **W3** | **Liveness alarm nobody hears** — `session_keeper_ack_timeout` has ZERO consumers; a dead watcher emits nothing. | 06, 07 | watcher writes per-tick `<agent>.keeperalive`; ops-monitor/watchdog alarms on `keeper-dead:<agent>` + on new `ack_timeout` events |
| **W4** | **Crews have no keeper watcher.** (NOT "gauge not wired" — that's stale; gauges DO write. Only the watcher process is missing.) | 03 | step 6b in `crewstart.go HandleCrewStart` spawns `harmonik keeper --agent <crew>` (warn-only, NO `--respawn-cmd` first) + teardown in stop |
| **W5** | **`--project` defaults to `os.Getwd()` AND marker verbs skip `filepath.Abs`** (`gates.go:12-25`) — round-1 bug-2a root cause, **never removed**; relative/worktree CWD writes the wrong dir. Plus `set-dispatching` fails open (exits 0, no live-watcher check). | 04 | Abs-normalize `--project` for all marker verbs; warn when no live keeper for the resolved dir |
| **W6** | **PreCompact race unchanged** — trigger block (`watcher.go:765`) sits after the stale(`:662`)/foreign(`:711`) `continue`s → falls open to lossy native compaction in the keeper's two failure states. | 06 | hoist the precompact block above the `continue`s |
| **W7** | `--warn-pct/--act-pct` still misleading (defaults never applied, no tighten-only clamp, banner prints raw flag); typo'd verb falls through to watcher-mode (no `default:` in `main.go`). | 04 | defaults→0 + print effective band + clamp; add `default:` unknown-subcommand case |
| **D1** | **Identity collapse** — DEFER. `.sid` authoritative only on the happy path; the watcher fallback arms are sole-path for crews (no guaranteed `.sid`). Can't delete until crews always write `.sid`. | 02 | sequence AFTER W4 (crew watchers → crews write `.sid`) |

## Already being fixed (wave A, in flight)
hk-nbft (positionals), hk-0ouc (tmux leak), hk-zole (impure-path coverage), hk-uldg (wire await-ack into restart flow — the CLI/skill side of W2's manual path).

## Recommended wave-B sequence (avoids file collisions; depends on wave A landing first)
1. **W1 + W4 + W3** (deploy/supervision/liveness — touches launch scripts / `crewstart.go` / ops-monitor) — *top operational risk; do first.*
2. **W2 + W6** (watcher/cycle core — both in `watcher.go`/`cycle.go`; one agent to avoid the hand-copied-gate duplication).
3. **W5 + W7** (CLI hardening in `cmd/harmonik`/`gates.go`) — after hk-nbft merges.
4. **D1** identity-collapse — later, after W4.
