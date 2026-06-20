# I3 — Captain Restart Reliability: Failure-Mode Catalog

Investigated 2026-06-20 against repo HEAD `3f60cf23`, deployed binary `/Users/gb/go/bin/harmonik` (Jun 20 10:03, current). Live `keeper doctor --agent captain` run during this investigation.

**Verdict in one line:** The four headline restart gaps the operator likely remembers are all **FIXED on current main AND on the deployed binary**. The residual risk is **doc/operational drift**, not code defects: one wrapper not copied to the runtime tools dir, and several skill files citing stale flags/values that would mislead a captain hand-running keeper commands. Crews still have no keeper armed by default (unchanged, by design).

---

## Catalog

### FM-1 — Captain "quits-and-stays-dead" on a context warning  ✅ FIXED
- **Symptom:** captain launched bare (`claude --remote-control captain`, no `--session-id`); on a keeper WARN the old warn text told it to `/quit`; nothing respawned it. (memory: reference_captain_keeper_restart_gap)
- **Root cause:** no stable session-id for the keeper's clear→resume to `--resume`; fatal `/quit` in shared warn text; no respawn path.
- **Status: FIXED, deployed.**
  - `scripts/captain-tools/captain-launch.sh` mints a lowercase UUIDv4 `--session-id`, writes `captain.sentinel`+`captain.pid` (orphan-sweep exclusion), generates `captain-respawn.sh`, and arms the keeper with `--respawn-cmd` for dead-pane self-heal (hk-opuv, commit `c1816874`). The deployed copy at `~/.claude/captain-tools/captain-launch.sh` is **byte-identical** to source (`diff` EXIT=0), and `keeper doctor` reports `captain-tools … in sync`.
  - Warn text trimmed; the fatal `/quit` is gone (commits `9ac3a5cb`, `07ce9063`). The captain warn now says "keep working … run restart-now … keep the turn open." Both STARTUP.md §6 and SKILL.md §10 explicitly forbid self-`/quit`/self-exit ("NEVER exit or terminate your own session on a warn").
  - `--respawn-cmd` self-heal path exists in `internal/keeper/watcher.go` (`maybeRespawn`).
  - Live `keeper doctor --agent captain`: `managed` present, `.sid` present, SessionStart hook wired, `captain-launch.sh` in sync → the armed/survivable posture is real on this box.

### FM-2 — `session_id` flips on `/clear` (keeper goes blind)  ✅ FIXED
- **Symptom:** keeper bound to the launch id, but the id is re-minted at every `/clear`; keeper emits `session_keeper_no_gauge reason=foreign_session` and stops managing. (memory: reference_keeper_sessionid_flips_on_clear)
- **Root cause:** `--session-id` is DEAD after the first cycle; keeper must re-resolve the live id each cycle.
- **Status: FIXED, deployed.** The `.sid` single-writer channel is the primary identity (hk-8prq, `6894b4de`); `keeper-sessionstart-hook.sh` writes `.harmonik/keeper/<agent>.sid` post-`/clear`. `internal/keeper/watcher.go:798-808` re-reads the authoritative `.sid` before rejecting on a session-id mismatch and re-latches `.managed` when the live `.sid` is a valid UUIDv4 matching the gauge (commit `12853cc6` "re-resolve .managed from .sid in foreign-guard after external /clear"). Old heuristic-scrape stack deleted (hk-3391, `93f7000e`). The stale `cycle.go` comment that claimed "resume survives /clear" was corrected (`8d2ab9fa`). Foreign-session auto-recovery + `keeper rebind` also live (hk-mejt). Doctor confirms `.sid present and well-formed`.

### FM-3 — Keeper-restart re-hydration (does the captain re-read mission/handoff?)  ✅ FIXED (intentional design)
- **Symptom risk:** after a keeper-driven `/clear`+`/session-resume`, does the captain re-ground correctly?
- **Status: WORKS by design.** Resume re-runs `/session-resume <agent>` on the SAME session id, so the captain re-executes the full STARTUP.md boot (Steps 2–6) and treats HANDOFF as INTENT-only, re-deriving live state. SKILL.md §10 + STARTUP.md "On resume after a restart-now cycle" both enforce: re-drain comms, re-ground via STARTUP, re-arm watchers (keeper arming survives; `comms recv --follow` and the `/loop 12m` tick must be re-armed). The "Handoff RE-CLASSIFY" rule (ON-060) prevents a resumed captain from re-surfacing already-autonomous items. Crew re-hydration is durable via the `br show <epic> --assignee` mirror + named-queue keeps draining (keeper SKILL.md § Crew-restart re-hydration). No live gap.

### FM-4 — await-ack / VERIFIED restart (hk-uldg)  ✅ LANDED
- **Symptom risk:** a fired `restart-now` is assumed to land even if the keeper is dead/wrong-pane.
- **Status: LANDED + deployed.** `harmonik keeper await-ack` exists (hk-uldg, `b43638ca`); restart-now wired to inject `[KEEPER ACK <nonce>] received restart` BEFORE the gated `/clear` (`909c25b0`; `internal/keeper/restartnow.go:125-141` injects ACK at step 4, `/clear` at step 5). For SELF restart the external wrapper `scripts/captain-tools/keeper-restart-verified.sh` fires restart-now, parses `nonce=rn-…`, and runs `await-ack --kind restart` as a separate process surviving the `/clear`. For CREW restart the captain runs `await-ack --agent <crew>` directly (SKILL.md §10). The AUTOMATIC ACT cycle is explicitly out-of-scope (companion hk-vpnp) and relies on its internal handoff-nonce poll — that is a known, documented boundary, not a regression.

### FM-5 — Captain out-of-band deploy non-ff race  ⚠️ LIVE (mitigated by procedure)
- **Symptom:** a captain cherry-pick push to `main` during a daemon merge fails the daemon bead non-ff and can wedge the daemon (stale `refs/heads/main`). (memory: reference_captain_deploy_nonff_race)
- **Status: PRODUCT FIX still open (`hk-svieq`, daemon fetch+rebase+retry on non-ff).** Not a restart-cycle bug, but it surfaces during the captain's LULL-DEPLOY duty which often runs right after a restart. Mitigation is procedural only: deploy in a true lull + `git merge --ff-only origin/main` after push. **Fix direction:** land `hk-svieq` so the daemon rebase-retries instead of failing the bead.

### FM-6 — Crews still have NO keeper armed by default  ⚠️ LIVE (known, by design)
- **Symptom:** a crew filling to ~200k tokens wedges (pane stops accepting keystrokes); no auto-clear fires. (keeper SKILL.md § KNOWN DRIFT; known-workarounds.md:64)
- **Root cause:** the gauge ships OFF; `keeper enable … --yes-destructive` is an explicit, deferred operator step. Captain is armed; crews are not.
- **Status: LIVE by design.** Workaround is manual `crew stop` + `crew start` (re-hydrates from `--assignee` mirror; named queue keeps draining). Confirm per-agent state with `keeper doctor --agent <crew>`. **Fix direction:** arm crew keepers in an operator-supervised session (hk-ekap1/hk-njetn) — out of scope for captain restart.

### FM-7 — Idle captain/crew doesn't wake on a bare comms send  ⚠️ LIVE (operational)
- **Symptom:** an idle session at its prompt does not wake on `comms send`; needs a tmux pane nudge. (memory: reference_crew_wake_and_context_clear)
- **Status: LIVE operational reality.** STARTUP.md §6 "Idle-crew wake" already documents the `tmux send-keys` nudge + verify-via-capture-pane. Not a restart-cycle defect; it affects post-restart re-tasking. No code fix expected (architecture of remote-control panes).

---

## Documentation / operational drift (the real residual risk)

These do not break the cycle but will mislead a captain who hand-runs keeper commands after a restart:

1. **`keeper-restart-verified.sh` NOT deployed to the runtime tools dir.** It exists only at `scripts/captain-tools/keeper-restart-verified.sh`; `~/.claude/captain-tools/` has `captain-launch.sh` but **not** the verified-restart wrapper. SKILL.md §10 line 696 references it as `scripts/captain-tools/keeper-restart-verified.sh` (repo path), which is reachable from the repo, but the keeper SKILL.md "Quick reference" (line 447) also cites the repo path — consistent. NOTE: `captain-launch.sh` does NOT route restart through this wrapper, so a captain's automatic restart-now is NOT externally verified unless someone explicitly runs the wrapper. **Fix direction:** either copy the wrapper into `~/.claude/captain-tools/` and have `captain-launch.sh` wire restart-now through it, or accept that the automatic-cycle verification is hk-vpnp's job.

2. **Stale keeper-band values in captain skills.** STARTUP.md §6 (lines 335/338) and SKILL.md (line 221) still say `--warn-pct 30 --act-pct 35`. The ACTUAL `captain-launch.sh` uses `--warn-abs-tokens 200000 --act-abs-tokens 215000`, and the keeper SKILL.md correctly notes pct flags are **inert on [1m] windows** (abs caps always win). The pct values are harmless-but-wrong if a captain hand-relaunches the keeper. **Fix direction:** update STARTUP.md/SKILL.md to cite `--warn-abs-tokens 200000 --act-abs-tokens 215000`.

3. **Positional-arg examples now rejected (flag-only, hk-nbft).** `harmonik keeper doctor captain` / `restart-now captain` / `enable captain` now exit 2 ("flag-only; use --agent"). The skill PROSE/examples mostly use `--agent` correctly, but keeper SKILL.md lines 286, 430, 433, 436 and 258 show positional `<agent>` forms (`keeper doctor <agent>`, `keeper enable <agent>`, and a `--warn-pct 25 --act-pct 30` example at 436). A captain copy-pasting these gets exit-2/inert-flag surprises. **Fix direction:** rewrite those examples to `--agent <name>` + `--warn-abs-tokens/--act-abs-tokens`.

4. **Memory reference_captain_keeper_restart_gap is stale-but-correct-in-conclusion.** It still describes the bare-launch `/quit` death and cites old line numbers (`injector.go:14-16`, `watcher.go:445-454`); those line numbers have drifted, but its FIX section (use `captain-launch.sh`, no self-`/quit`) matches current reality.

---

## Summary table

| FM | Failure mode | Status |
|----|--------------|--------|
| FM-1 | Captain quits-and-stays-dead (no --session-id) | ✅ FIXED + deployed (captain-launch.sh, no self-/quit) |
| FM-2 | session_id flips on /clear → keeper blind | ✅ FIXED + deployed (.sid re-resolve, watcher.go:798-808) |
| FM-3 | Re-hydration after keeper clear | ✅ Works by design (STARTUP re-ground + RE-CLASSIFY) |
| FM-4 | await-ack / VERIFIED restart (hk-uldg) | ✅ Landed (ACK before /clear; external wrapper for self) |
| FM-5 | Out-of-band deploy non-ff race | ⚠️ LIVE — product fix hk-svieq open; procedural mitigation only |
| FM-6 | Crews have no keeper armed | ⚠️ LIVE by design — manual crew stop/start workaround |
| FM-7 | Idle session doesn't wake on comms send | ⚠️ LIVE operational — tmux nudge documented |
| Drift-1 | keeper-restart-verified.sh not in ~/.claude tools | ⚠️ doc/deploy gap |
| Drift-2 | Stale --warn-pct/--act-pct in captain skills | ⚠️ doc drift (cosmetic; pct inert on 1M) |
| Drift-3 | Positional-arg keeper examples now exit-2 | ⚠️ doc drift |
