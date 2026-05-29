<!-- PP-TRIAL:v2 2026-05-28 main — v72. BOTH feature areas DONE: all 13 SDLC fixtures + README, and ALL Track-2 attractor-parity (6 code + 7 test + sidecar). THREE daemon concurrency/loop bugs found, fixed, reviewed, merged: hk-68pvl (worktree race), hk-kuxxl (wave PID-aliasing — EMPIRICALLY VALIDATED), hk-isq02 (review-loop iter-2 resume-ready). Concurrent --wave runs are now SAFE. Main clean @ d0fe0bc, 0/0 origin, build green. START HERE = pull new work; --wave is usable again. -->

Roadmap: [ROADMAP.md](ROADMAP.md). Cross-project rules: `~/.claude/CLAUDE.md`. Orchestrator rules: [docs/orchestrator-rules.md](docs/orchestrator-rules.md). Known workarounds: [docs/known-workarounds.md](docs/known-workarounds.md). Dispatch loop: skill `harmonik-dispatch` + AGENTS/CLAUDE.md §"Daily loop".

ROLE: You are the orchestrator. Delegate substantively. Keep the main thread minimal. If a bead fails twice, dispatch an investigator sub-agent (don't re-dispatch).

# Where we are (v72, 2026-05-28)

**Main clean @ `d0fe0bc`, `0/0` with origin, build green, `go test ./internal/workflow/...` + daemon review-loop suites pass.** Both major work areas the session targeted are COMPLETE, and three daemon bugs that were silently corrupting concurrent/iterated runs are fixed + reviewed + merged.

## What landed this session (v71 → v72)
- **Area 1 — SDLC fixtures: 100%.** All 13 NOW fixtures (`hk-o52fm.1–.14` minus the SOON/DEMO `.15–.21`) + the README consolidation (`hk-9w9y5`). Recovered the marquee `hk-o52fm.5` (triple-review-consolidate) from a dangling commit and de-collided clashing `TestTRC_`/`trc*` test names vs `.6`.
- **Area 2 — Track-2 attractor-parity: complete.** T0 spec (`hk-jyqxe`) + 6 code beads (`hk-l8rpd` tool/shell node, `hk-55zv2` goal/param, `hk-m5lmo` role, `hk-sdnzj` inline prompt, `hk-q8nqr` model/effort, `hk-69asi` non-committing node) + 7 test beads (`hk-cucz6/qpbpc/156il/mca0b/xp9j7/4bn9o/9ohjf`) + sidecar (`hk-9t892`).
- **Three daemon bugs — all fixed, independently reviewed (APPROVE), merged:**
  - `hk-68pvl` (`4abfafd`) — worktree removed out from under a live implementer → false `no_commit`. Fix: deferred `forceTeardownSession` gates removal on session teardown (LIFO).
  - `hk-kuxxl` (`81e661b`) — `--wave` PID-aliasing: slash-bearing tmux window handle (`session:bead/i1`) misresolves to the active pane, so when a fast sibling's pane exits, slow siblings see an aliased-dead PID → false `no_commit`. Fix: resolve PID via slash-free `%NNNN` pane ID. **EMPIRICALLY VALIDATED** (7-bead 3-wide wave, 6/6 clean, zero no_commit).
  - `hk-isq02` (`1109502`) — review-loop iteration-2 implementer never readies (`agent_ready_timeout`) because `claude --resume` doesn't re-fire `SessionStart` (the only `agent_ready` source on the tmux substrate). Fix: synthetic-`agent_ready` fallback grace for iter≥2 when `implWatcher==nil`.

## Key operational lessons (see memory + below)
1. **`harmonik run` needs `$TMUX`.** If the orchestrator session isn't inside tmux, `harmonik run` exit-1's on `$TMUX is not set` and spawns NO daemon. Workaround: wrap in `tmux new-session -d -s harmonik-run -c <repo> "harmonik run ... --notify-stream 2>&1 | tee /tmp/harmonik-<batch>.log; echo HARMONIK_RUN_EXITED_\${PIPESTATUS[0]} >> ..."`, then Monitor the tee'd log + events.jsonl. (Distinct from the v71 stale-`queue.lock` exit-1, which DID spawn a daemon.)
2. **`--wave` concurrency is SAFE again** (post hk-kuxxl/hk-68pvl). Residual caution: sibling beads cloned from the same test template can pick identical test-fn names → collide at MERGE under `--wave` (serial surfaces it in-loop). For template-family fixtures, serial is still cleaner.
3. **Review/verify sub-agents that run `git checkout` must use `isolation: worktree`** — a non-isolated reviewer left main on a stray branch this session; the orchestrator's commits then landed off-`main` and `git push origin main` said "up-to-date" while HEAD was ahead. Tell-tale: push up-to-date but `git rev-list --left-right --count origin/main...HEAD` shows you ahead → check `git branch --show-current`.
4. **Daemon may leave local `main` behind origin** after a run (per-bead it merges + pushes to origin but the local checkout can lag, sometimes with staged churn). Reconcile with `git fetch && git reset --hard origin/main` once the daemon has EXITED (never mid-run).

# Remaining / next work (nothing blocking)
- **P3 attractor-parity v2 backlog:** `hk-9j49t` (per-tool-node `transient_exit_codes`), `hk-gv5n5` (real `auto_status` work-product status-derivation), `hk-1xzg3` (normative `model_stylesheet` selector >2 tiers), `hk-tksed` (`tool_command_completed` observability event).
- **P3 hardening follow-up:** `hk-82jwm` — strengthen the `hk-68pvl` regression test to assert the PRODUCTION defer ordering (reviewer's non-blocking note).
- **Pre-existing, unrelated (not introduced this session):** RED test `TestMergeToMain_NoWorkAgentMainAdvanced` (`hk-zhxqx`); dep cycle `hk-11xkn ↔ hk-iuaed`; `TestReviewLoopBridge_CHB009` drift; real-claude-spawn env-gated tests.
- **Hygiene:** several stale agent worktrees under `.claude/worktrees/` and `.harmonik/worktrees/` + run/ branches accumulated; safe to prune when convenient. `kerf next` showed a large untriaged/external-drift backlog — a `kerf triage --ack` pass is overdue but low-priority.

# Dispatch discipline (unchanged — see AGENTS.md)
Rebuild (`go install ./cmd/harmonik`) → pre-screen beads → launch in background (inside tmux per lesson #1) with `--notify-stream` → arm a Monitor on the tee'd log + `.harmonik/events/events.jsonl` → on failure: failed-once = re-dispatch next batch, failed-twice = STOP + investigator sub-agent. `--wave` is now safe for `--max-concurrent > 1`.

# Translations glossary
- **fixture** — a `specs/examples/<name>.dot` workflow example + its `internal/workflow/scenario/<name>_test.go`.
- **Track-2 / attractor-parity** — the DOT-graph capability set bringing harmonik's `.dot` dialect to spec parity (tool/shell nodes, goal/param substitution, per-node role/prompt/model/effort, non-committing nodes).
- **no_commit (false)** — `no_commit_during_implementer ... iteration 1 exit=0`: the daemon recorded no commit though the implementer did/should have worked. Root causes this session were hk-68pvl + hk-kuxxl, both now fixed.
- **review-loop iteration-2** — when iter-1 implementer commits and the reviewer returns REQUEST_CHANGES, the daemon launches an iter-2 implementer to address it (was broken by hk-isq02, now fixed).

# No hard blockers. Both stated areas DONE; parallel runs proven safe. Next action: pick new work from `kerf next` / the P3 backlog, or take on the deferred items above. `--wave` is usable again.
