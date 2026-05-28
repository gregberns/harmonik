<!-- PP-TRIAL:v2 2026-05-28 main — v69. DOT proven end-to-end LIVE (simple+complex) via heavy QA; 5 blockers fixed, 1 dogfooded. Clean, all pushed. -->

Roadmap: [ROADMAP.md](ROADMAP.md). Cross-project rules: `~/.claude/CLAUDE.md`. Orchestrator rules: [docs/orchestrator-rules.md](docs/orchestrator-rules.md). Known workarounds: [docs/known-workarounds.md](docs/known-workarounds.md).

ROLE: You are the orchestrator. Delegate substantively. Keep the main thread minimal.

# Where we are (v69, 2026-05-28)

**Main clean, all pushed, in sync with origin.** This was a **heavy-QA session of the DOT live execution path** (per user directive: "HEAVILY test DOT simple→complex; log+resolve issues immediately; fix harmonik issues in subagents and continue testing"). The loop converged: **DOT now runs end-to-end live for both simple and complex topologies.**

## Headline: live DOT testing surfaced a cascade of bugs the stub tests missed

The v68 claim "DOT functional end-to-end" was true **only at the stub-handler test level**. The daemon DOT e2e tests use a `/bin/sh` stub handler that bypasses the real tmux paste/submit path and synthesizes outcomes — so **no real-agent end-to-end coverage existed.** The first live `harmonik run --workflow-mode dot` hung immediately, and each successive live run peeled back the next layer. Five distinct blockers, all now fixed + proven live:

| Bug (bead) | Commit | What it was |
|---|---|---|
| `hk-3qjwl` P0 | `704ddc0` | DOT dispatch pasted the agent prompt but never submitted it — missing the `waitAgentReady` gate the builtin paths have, so the paste hit Claude's welcome splash and the prompt sat unsent → `run_stale`. |
| `hk-kxygy` P0 | `a90980e` | CLI `harmonik graph validate` used a broken parser (`internal/workflowvalidator`) — passed **0/20** real `.dot` files incl. the canonical one. Re-pointed at the daemon's `internal/workflow/dot` parser. Now 20/20. |
| `hk-i1n7j` | `ceeae33` | merge-to-main `git rebase` aborts because the daemon's own `br` flush dirties tracked `.beads/issues.jsonl`. Added pre-rebase ledger discard. |
| `hk-aiw63` P0 | `5c8a8c3` | same class — Claude + `MaterializeClaudeSettings` dirty tracked `.claude/settings.json` every run. Generalized the pre-rebase cleanup to the existing `isHarmonikChurn` allowlist (`discardDirtyChurn`). |
| `hk-z03e8` P1 | `c0447af` | DOT classified terminal success by **inbound-edge topology** (forbidden by WG-021) → any topology richer than the exact review-loop shape was misclassified `run_failed`. Replaced with terminal-node-**identity** classification per WG-021/WG-022. **Fixed by dogfooding `harmonik run`** (North Star phase 2). |

Each was independently reviewed (agent reviewer or fresh-read + verification) before landing. Cascade engine routing (all 5 verdict routes) was separately proven; stale cap-bug `hk-i7yq8` closed via mutation proof.

## Proof artifacts (live runs, real claude agents, merged+pushed+bead-closed)
- **Simple** review-loop: `hk-3yz2d` → walked start→implementer→reviewer(APPROVE)→close→merge→push→CLOSED.
- **Complex** (review-loop + non-agentic `finalize` node): `hk-ha6z1` via `/tmp/dot-qa-complex1.dot` → same, through the extra node. This is the exact repro that failed pre-`hk-z03e8`-fix.
- **Dogfood**: `hk-z03e8` itself fixed via `harmonik run` (builtin review-loop, implementer→agent-reviewer APPROVE→auto-merge). harmonik run is now **proven reliable** (3 clean completions).

# Next work — DOT hardening beads (all filed this session, none block core function)

DOT works; these are reliability/observability/correctness gaps found by the gap-analysis comparing DOT dispatch vs the builtin paths. **All are in `internal/daemon/dot_cascade.go` except `hk-zhxqx`.** Dogfood them via `harmonik run` (now reliable) — but run dot_cascade.go ones **serially** (same file → conflicts under concurrency).

- **`hk-upcjj` (P1)** — DOT dispatch passes `eventCh=nil` to `pasteInjectQuitOnCommit`, disabling heartbeat-staleness + launch-verification kills (a hung pane recovers only via the long wall-clock backstop — the class that bit us repeatedly). **SUBTLETY:** the per-run tap channel (`tapCh`, captured by the `hk-3qjwl` agent_ready gate) is already consumed by `waitAgentReady` (`newChanAgentEventSource(tapCh)`); a channel can't be drained twice. Check how the builtin path (`workloop.go:~1638`) shares one tap between `waitAgentReady` and the quit-on-commit heartbeat monitor — may need a tee/fan-out or a second tap. Don't naively reuse the drained channel.
- **`hk-5e9yj` (P1)** — no-progress detector (EM-015e) missing in the DOT cascade (builtin has the diff-hash compare at `reviewloop.go:~557`). Medium effort.
- **`hk-d0aqq` (P1)** — DOT reviewer node never emits the `reviewer_verdict` event (builtin emits it at `reviewloop.go:~852`). Mechanical/observability — **safe dogfood candidate.**
- **`hk-9v5yo` (P2)** — DOT path hard-errors on noChange instead of subsumed-close (builtin: `workloop.go:~1806`).
- **`hk-mvjs4` (P3)** — DOT omits `implementer_phase_complete` event. Mechanical/observability — **safe dogfood candidate.**
- **`hk-zhxqx` (P1)** — `TestMergeToMain_NoWorkAgentMainAdvanced` is RED on main: the `hk-cwxow` fix (noChange+main-advanced → subsumed-close) has **regressed** on the **builtin** path — bead REOPENED instead of closed, false-positive `non_ff_merge`. Verified failing on pristine HEAD (not caused by this session's merge fixes). This is the only RED in the DOT/merge test surface.

Pre-existing (carried from v68): `hk-1xsyu` (P2, daemon e2e for non-APPROVE routes — note: real reviewers can't be forced to REQUEST_CHANGES/BLOCK, so this MUST stay a stub-handler test), `hk-karlz` (P2, daemon gate evaluator — gate nodes error until it lands, so gate topologies can't run live yet).

# Files to open first
1. `internal/daemon/dot_cascade.go` — DOT cascade driver. `dispatchDotAgenticNode` (agent_ready gate + the `eventCh=nil` of `hk-upcjj`), `dotTerminalNodeIsSuccess` (the `hk-z03e8` fix), the terminal branch.
2. `internal/daemon/workloop.go` — `mergeRunBranchToMain` + `discardDirtyChurn` + `isHarmonikChurn` (the merge fixes); `TestMergeToMain_NoWorkAgentMainAdvanced` regression (`hk-zhxqx`).
3. `internal/daemon/reviewloop.go` / `workloop.go` builtin dispatch — the parity reference for the remaining DOT gaps.
4. `specs/examples/review-loop-finalize.dot` (added by the `hk-z03e8` fix) + `/tmp/dot-qa-complex1.dot`, `/tmp/dot-qa-complex2.dot` (QA complex fixtures; validate clean via `harmonik graph validate`).

# Caveats
- Full `go test ./internal/daemon/` is RED from pre-existing failures (`hk-zhxqx` + v68's `hk-o4vjp`, `hk-yn29b`, a StaleWatcher hang). Use `-run` filters. The DOT/merge/cascade subset is GREEN (22 pass) except `hk-zhxqx`.
- **Worktree-churn fact (now handled):** every run dirties tracked `.claude/settings.json` (Claude + `MaterializeClaudeSettings`) and `.beads/issues.jsonl` (`br` flush). The merge tolerates `isHarmonikChurn` before the rebase. Don't be alarmed by a dirty `.beads`/`.claude` mid-run.
- **Cruft to clean up someday (low priority):** ~43 git stashes accumulated (mostly agent stash-compare artifacts; a few may be real prior-session WIP — inspect before `stash clear`). Several stale `.harmonik/worktrees/run-*` + orphaned `run/*` branches.
- Live DOT testing of **non-APPROVE routes** (REQUEST_CHANGES/BLOCK/cap-hit) is NOT feasible with real agents (can't force a real reviewer's verdict) — those belong in stub-handler daemon-e2e tests (`hk-1xsyu`).

# Translations glossary
- **DOT mode** — workflows as Graphviz `.dot` graphs; daemon walks node→edge→node via `driveDotWorkflow` (`internal/daemon/dot_cascade.go`).
- **agent_ready gate** — `waitAgentReady`; the daemon must wait for the agent's REPL to be input-ready before pasting+submitting the launch prompt (else the Enter is eaten by the splash). Was missing in DOT (`hk-3qjwl`).
- **isHarmonikChurn / discardDirtyChurn** — allowlist of tracked files the daemon/agent dirty every run (`.beads/issues.jsonl`, `.claude/*`, `.harmonik/`); discarded before the merge rebase.
- **dogfood** — using `harmonik run` to land harmonik's own dev work (canonical loop, North Star phase 2). `hk-z03e8` was fixed this way.
- **terminal disposition** — success vs needs-attention; per WG-021/WG-022 keyed to terminal node **identity** (`close`=success, `close-needs-attention`=attention), NOT edge topology (the `hk-z03e8` bug).

# No hard blockers. Standing directive: on /session-resume, CONTINUE — don't ask "shall I". Next action: dogfood the safe DOT parity beads (`hk-d0aqq`, `hk-mvjs4`) via `harmonik run`, then tackle `hk-upcjj` (mind the channel-sharing subtlety) and `hk-zhxqx`.
