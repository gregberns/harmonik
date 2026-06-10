# A3 — Ad-Hoc Tooling Inventory & Formalization Recommendations
**Date:** 2026-06-10  
**Analyst:** read-only sub-agent  
**Sources:** captain-tools/, scripts/, hk-nlhys, comms log (24h), known-workarounds.md, orchestrator-rules.md, memory files, `harmonik --help` tree

---

## Status key
- **EXISTS** — already a first-class harmonik command/flag/event
- **PARTIAL** — exists but incomplete or requires manual ceremony around it
- **MANUAL** — still entirely a hand-run script or undocumented ritual

---

## Pattern × Status × Recommendation Table

| # | Pattern (plain English) | Current Status | What EXISTS today | Recommended harmonik surface | Proposed bead title | Priority |
|---|-------------------------|---------------|-------------------|------------------------------|---------------------|----------|
| 1 | **Daemon supervisor / auto-revive** — keep the daemon alive across crashes with backoff, strip API creds, tee to log | PARTIAL | `scripts/hk-keeper.sh` (canonical, checked-in); `harmonik supervise start/stop/status/restart` subcommand exists | `harmonik supervise` already has the verb surface; gap: `hk-keeper.sh` is still the preferred launch path because `supervise` lacks credit-burn guard (`env -u ANTHROPIC_*`) and the non-`harmonik-*`-prefixed tmux session name (so orphan sweeps don't kill it). Close the gap: add `--strip-api-key` flag + configurable tmux session name to `harmonik supervise start`. Retire `/tmp/hk-daemon-supervise.sh` (ephemeral, reconstructed at least once after deletion). | `harmonik supervise: add --strip-api-key and --session-name to close gap with hk-keeper.sh` | P2 |
| 2 | **Tmux orphan reap** — detect and kill dead `harmonik-*-flywheel` sessions left by killed/crashed daemons, to unblock `tmux new-window` launch stalls | PARTIAL | Daemon reaps its own COMPLETED runs; `hk-4l7zs` filed (P1 boot-reap extension). Manual reap SOP documented in hk-nlhys. | `harmonik supervise reap` verb (or `harmonik daemon reap-orphans`): enumerate sessions matching `harmonik-<12hex>-flywheel`, verify `pane_dead=1` AND session predates the live daemon's start time, kill survivors, emit a `tmux_orphan_reaped` event per session. Boot reconciler (hk-5pg37) should call this on startup. Preserves: `harmonik-pi`, `hkdkeeper`, `kerf`. | `harmonik supervise reap: boot-time + on-demand tmux orphan reap (extends hk-5pg37)` | P1 |
| 3 | **Captain crew-status polling** — dispatch a haiku sub-agent per crew to read its transcript digest and return ≤5-line status, so captain never reads multi-MB JSONLs inline | MANUAL | `~/.claude/captain-tools/crewlog.sh` (ad-hoc, outside the repo). `harmonik crew list` shows registered crew but no health/transcript data. | `harmonik crew status <name>` (or `--all`): reads last N turns of the crew's transcript JSONL (path derivable from session_id in crew registry), emits a compact digest on stdout. Integrate with `harmonik crew list --json` so orchestrators can poll without a separate script. | `harmonik crew status: compact transcript digest for captain status polling` | P2 |
| 4 | **Bypass-SOP banked-commit deploy** — during a daemon outage: author on isolated worktree → independent reviewer → merge-tree gate with real exit codes → self-serialized cherry-pick to main | MANUAL | No harmonik command. Crews coordinate via `comms`. The cherry-pick steps, build verification, and non-ff guard are fully hand-run. Comms log shows this SOP executed 5+ times in the last 24h across 3 crews. | `harmonik promote <sha> [--verify-build] [--no-ff-guard]`: given a reviewer-blessed commit SHA on a worktree branch, runs the merged-tree gate, cherry-picks to main, pushes. Emits a `bypass_sop_land` event. Makes the "banked commit" pattern repeatable without per-crew shell improvisation. See also hk-gax8v (harmonik promote for integration→main). | `harmonik promote: daemon-independent banked-commit cherry-pick with build gate` | P1 |
| 5 | **Wake-watcher (comms auto-injection)** — out-of-process poller that injects incoming `harmonik comms` messages into an idle Claude pane via `tmux send-keys`, with idle-gate, dedup, and pending queue | MANUAL | `scripts/hk-wake.sh` (complex, 200+ lines, checked into repo). No harmonik verb. Crews must launch this by hand per agent identity + tmux target. | `harmonik crew wake <name>` (or `harmonik crew start ... --wake-watcher`): start `hk-wake.sh` in the background bound to the crew's registered tmux target. The wake-watcher logic is self-contained enough to run as a daemon subprocess. Emitting a `wake_injected` event would make injection observable. | `harmonik crew wake: first-class wake-watcher launch bound to crew registry` | P2 |
| 6 | **Scenario-test worktree authoring** — because daemon 30-min cap kills scenario beads before commit, author via uncapped worktree sub-agent + targeted `go test -tags=scenario` gate + cherry-pick | MANUAL | `scripts/scenario-gate.sh` exists (affected-package scope, fail-open). No harmonik verb for "run this bead without the time cap." The workaround requires manual worktree management. | `harmonik run --no-timeout` flag (or `--timeout 0`) for known long-running beads, plus a `scenario` workflow mode that runs `scenario-gate.sh` as the commit gate instead of the standard gate. Alternatively: raise or remove the 30-min cap for beads labeled `scenario`. | `harmonik run: --no-timeout / extended budget for scenario-test beads` | P2 |
| 7 | **Smoke-scratch lane** — real-daemon validation in a throw-away temp project so smoke commits don't litter main trunk | PARTIAL | `scripts/smoke-scratch.sh` exists (checked-in). No `harmonik smoke` first-class command. Crews must remember to use this script rather than dispatching against main. | `harmonik smoke [--timeout 20m]`: wraps `smoke-scratch.sh`, enforces the scratch-project pattern, emits a `smoke_pass`/`smoke_fail` event to the daemon event stream so orchestrators can gate a deploy on smoke outcome without parsing log output. | `harmonik smoke: first-class command wrapping smoke-scratch lane with event emission` | P2 |
| 8 | **Deploy-verify gate** — after `go install` + keeper revive, verify the daemon is (a) up, (b) running the right binary hash, (c) dispatching with correct `--workflow-mode` | MANUAL | `harmonik supervise status` shows process state. No binary-hash or workflow-mode verification. Orchestrator-rules.md documents the manual checklist (rebuild → poll queue status → verify reviewer events fire). | `harmonik supervise verify [--expect-workflow-mode review-loop]`: checks live daemon PID matches the installed binary's mtime, emits `deploy_verified`/`deploy_drift` event. Pairs with #7 (smoke gate). | `harmonik supervise verify: post-deploy binary + workflow-mode drift check` | P2 |
| 9 | **Dispatch-guard / pre-screen scout** — check whether a bead's work already landed on main before dispatching, via `git log --grep "Refs: <id>"` AND code artifact search | PARTIAL | `harmonik queue dry-run` validates ledger deps and double-dispatch. Does NOT check git history for already-landed work. Orchestrator-rules.md has a manual bash snippet. Filed as hk-lhv8i. | `harmonik queue dry-run` should also run the `git log --grep "Refs: <id>"` check (hk-lhv8i) and report `already_landed` items in its output, so the submit path has a single pre-flight gate. | `harmonik queue dry-run: already-landed check (hk-lhv8i) — add Refs: git-log gate` | P1 |
| 10 | **Concurrent concurrency change** — `harmonik queue set-concurrency N` exists but is undocumented in top-level help; crews hand-edit `--max-concurrent` in the supervisor script for persistence | PARTIAL | `harmonik queue set-concurrency` verb exists (hk-ohiaf, commit 1cc2b88e). `harmonik supervise` has no `--max-concurrent` persistence surface. | Expose `harmonik supervise set-concurrency N` that (a) calls the live RPC AND (b) rewrites the keeper/supervisor config so the new value survives revives. | `harmonik supervise set-concurrency: persist concurrency change across daemon revives` | P3 |
| 11 | **Credit-burn guard** — strip `ANTHROPIC_API_KEY`/`ANTHROPIC_AUTH_TOKEN` from daemon environment so spawned claude bills the subscription, not the API credit pool | PARTIAL | `scripts/hk-keeper.sh` strips them. `/tmp/hk-daemon-supervise.sh` strips them. `harmonik supervise start` does NOT strip them (gap noted in #1). | Fold into `harmonik supervise start` as the default (no flag required — subscription billing is always the right default). Emit a `credit_burn_risk` warning event if `ANTHROPIC_API_KEY` is set in the daemon's environment at boot. | `harmonik supervise: always strip ANTHROPIC_API_KEY at daemon boot (credit-burn guard)` | P1 |
| 12 | **Two-captains / sole-captain enforcement** — protocol for resolving which of two concurrent captain sessions is authoritative; currently purely verbal via comms bus | MANUAL | `harmonik comms who` shows online agents. No ownership/authority concept. Comms log shows two-captains conflicts resolved via broadcast on 2026-06-09. | `harmonik crew claim-captain [--force]`: write a durable captain token to `.harmonik/captain.lock` (analogous to pidfile); second session attempting `claim-captain` gets a rejection with the current holder's identity. `harmonik comms who --captain` shows the lock holder. | `harmonik crew: captain-lock preventing two-captains authority conflicts` | P3 |
| 13 | **Session-keeper context watcher** — monitors Claude context %, injects warn at 80%, triggers handoff/clear/resume at 90% | PARTIAL | `harmonik keeper` command exists with `enable`/`doctor`/`set-dispatching`/`clear-dispatching` verbs. `scripts/keeper-statusline.sh`, `keeper-stop-hook.sh`, `keeper-precompact-hook.sh` are checked-in hook scripts. Gap: `harmonik keeper enable` still requires manual `--yes-destructive` flag and manual session naming; no crew-auto-wire. | `harmonik crew start ... --keeper`: auto-enable the keeper for newly started crew sessions. Removes the manual `harmonik keeper enable` ceremony per crew. | `harmonik crew start: --keeper flag to auto-wire keeper on crew session start` | P2 |
| 14 | **Cross-queue double-dispatch dedup** — bead dispatched to two queues simultaneously is not caught (hk-a11re open) | MANUAL | Per-queue `bead_already_dispatched` guard exists. Cross-queue dedup is absent. Manual workaround: don't dispatch the same bead to two queues. | `harmonik queue submit` should check all active queues for the bead before accepting. Emit `bead_already_dispatched` even on cross-queue collision. Close hk-a11re. | `harmonik queue submit: cross-queue double-dispatch dedup (close hk-a11re)` | P2 |
| 15 | **Daemon event monitoring via raw JSONL grep/Python** — crews hand-grep `events.jsonl` with `run_id` filters, producing false negatives (documented in hk-nlhys + logmine F14) | PARTIAL | `harmonik subscribe --json` is the correct authoritative path. `hk-nlhys` explicitly flags hand-rolled event greps as dangerous. Orchestrator-rules.md says "NEVER hand-grep events.jsonl by run_id — use jq". | Add `harmonik events <run_id>` query command (wraps `jq 'select(.run_id=="<id>")'` on events.jsonl with proper field extraction) so crews have a safe one-liner that avoids the false-negative trap. | `harmonik events <run_id>: safe per-run event query replacing dangerous hand-greps` | P2 |

---

## Summary by priority

| Priority | Patterns | Key theme |
|----------|----------|-----------|
| **P1** | 2 (orphan reap), 4 (promote/bypass-SOP), 9 (dry-run already-landed), 11 (credit-burn guard) | Safety + unblock daily loop |
| **P2** | 1, 3, 5, 6, 7, 8, 13, 14, 15 | Captain UX, scenario tests, observability, crew lifecycle |
| **P3** | 10, 12 | Ergonomics / nice-to-have |

---

## What is already well-covered (no new bead needed)

- `harmonik subscribe` — typed event stream replaces ad-hoc JSONL tails (DONE, just needs to be used)
- `harmonik crew start/stop/list` — crew registry exists
- `harmonik queue pause/resume/cancel` — lifecycle verbs exist (minor name flakiness tracked in hk-4kuvj)
- `harmonik keeper` — session-keeper watcher + hook scripts exist; gap is only the auto-wire UX
- `scripts/scenario-gate.sh` and `scripts/hk-keeper.sh` — checked-in canonical scripts; gaps are harmonik-verb wrappers, not reimplementation
- `scripts/validate-commit-msg.sh` and `scripts/secret-scan.sh` — commit-time gates already integrated via lefthook; no harmonik verb needed

---

## Sources consulted

- `/Users/gb/.claude/captain-tools/crewlog.sh`
- `/tmp/hk-daemon-supervise.sh`
- `/Users/gb/github/harmonik/scripts/` (all 12 scripts)
- `br show hk-nlhys` + `br comments list hk-nlhys`
- `harmonik comms log --since 24h` (106KB, 24h of crew comms)
- `/Users/gb/github/harmonik/docs/known-workarounds.md`
- `/Users/gb/github/harmonik/docs/orchestrator-rules.md`
- Memory files: `reference_harmonik_daemon_supervisor`, `reference_captain_crew_status_polling`, `reference_scenario_test_authoring`, `project_emergent_tooling_capture`, `reference_review_loop_default_outage`
- `harmonik --help`, `harmonik queue --help`, `harmonik comms --help`, `harmonik crew --help`, `harmonik keeper --help`, `harmonik supervise --help`, `harmonik subscribe --help`
