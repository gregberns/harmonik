# Keeper Critique — Round 2 (post-fix current state)

**Date:** 2026-06-20. Round 1 (`plans/2026-06-20-keeper-architecture-critique/`) diagnosed the keeper and we SHIPPED fixes. This round critically reviews the **CURRENT state** to find what's still wrong / newly wrong, so we implement the important ones.

## What shipped since round 1 (all merged to main + deployed on the running fleet)
- `4128d760` — restart-now direct/synchronous + flag-only + keeper→pane ACK injection (the stranded `89852bb3`).
- `3f88fae5` — Bug 3: stopped the automatic ACT-when-idle **loop** + **handoff-truncation-to-0** (`cycle.go`).
- `b43638ca` — `harmonik keeper await-ack` subcommand (agent-side ACK handshake PRIMITIVE; injectable PaneCapturer; `session_keeper_ack_timeout` event).
- `95135107` — band-drift integration tests → pinned 200k/215k/240k.
- Deployed: daemon + captain keeper restarted on the new binary; captain keeper now on corrected abs-token band.

## Known-still-open going in (verify + deepen; find MORE)
1. **Automatic cycle is STILL fire-and-forget.** Bug 3 fixed the loop+truncation, but `MaybeRun`/`runCycle`'s `/clear`+`/session-resume` are still pasted open-loop (no ACK read-back). The new `await-ack` primitive is NOT wired into the automatic cycle. **Is this the top remaining reliability hole?**
2. **`await-ack` is a primitive, not integrated** (hk-uldg) — nothing actually calls it on a real restart yet (manual or automatic; captain or crew).
3. **Single authoritative identity not done** — the watcher's fallback rederivation/latch heuristics remain (round-1 reports 02/05). `.sid` is authoritative via ReadCtxFile overlay but the watcher still has competing paths.
4. **Crews have NO keeper watchers** (gauge-not-wired-for-crews) — only the captain is protected.
5. **Positional args** inconsistent — doctor/enable/disable still accept them (hk-nbft, being fixed separately).
6. **Test gaps** — impure paste/respawn paths (hk-zole); integration suite leaks `*-flywheel` tmux sessions (hk-0ouc).

## Anchors
Code: `internal/keeper/{cycle,watcher,gauge,sessionid,tmuxresolve,injector,respawn,restartnow,awaitack}.go`; `cmd/harmonik/{keeper_cmd,keeper_enable_doctor_cmd}.go`. Round-1 synthesis: `plans/2026-06-20-keeper-architecture-critique/EXEC_SUMMARY.md` + `EXECUTION-WORKLOG.md`.

## Every agent
1. Read this brief + your anchors + the round-1 EXEC_SUMMARY (don't re-derive round-1 — build on it; find what's NEW or STILL broken).
2. Evaluate critically from your lens on the CURRENT code. Verify claims against code, not memory.
3. Write your report to `plans/2026-06-20-keeper-critique-round2/<your-file>.md`.
4. Return a ≤200-word summary: top finding, severity, one-line verdict, and whether it's worth implementing now.
