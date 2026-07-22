# 06 — Does the quality gate exercise the remote SSH path?

**Date:** 2026-07-12
**Verdict:** The QA gate is **real and firing**, but the **remote SSH bug class is structurally invisible to it**. Every path that would enter the remote (`rbc != nil`) branches in tests does so either through canned-output mocks that never spawn a shell, or through a real `ssh localhost` round-trip into the *operator's own fully-provisioned login shell*. Nothing reproduces a worker whose non-interactive login shell sources only a barren `~/.zshenv` and returns empty stdout for `git`.

---

## 1. Is the QA execution gate actually running? — YES, real, not theater

- The `qa` node is present in the live `workflow.dot` at repo root (committed `0adb6551`, "add QA execution gate after review in standard-bead workflow").
  - `workflow.dot` topology (header + node): `review APPROVE → qa`, and `qa` is the **sole inbound edge to `close`** (`qa APPROVE → close`). See the DOT header comment lines 19–27 and the `qa` node body. It is a `reviewer-class QA — EXECUTES the code + edge-case testing` node.
- It is firing in production: `.harmonik/events/events.jsonl` contains **21 `node_dispatch_requested` events with `payload.node_id == "qa"`**, the most recent at **2026-07-12T14:58:17Z** (`run_id 019f56a2-...`). So the gate is live and reached on real runs, not dead config.

Conclusion: the gate exists and executes. The problem is **what it executes against**, not whether it runs.

## 2. The core question — does the gate (or any test) exercise the remote SSH path with a faithful non-interactive login shell? — NO

### Where the remote code lives and how it's gated
- The remote branches are guarded on `rbc != nil` throughout `internal/daemon/workloop.go` (dozens of `if rbc != nil` / `if rbc == nil` sites, e.g. lines 3458, 3555–3595, 4240–4376). **Local runs (`rbc == nil`, the daemon on gb-mbp) never enter them** — they are byte-identical to the box-A-local path by design (NFR7).
- `SSHRunner` (`internal/lifecycle/tmux/runner.go:92`) tunnels each command as `ssh [opts] <host> -- <shell-quoted argv>`. Its own doc comment (lines 71–90) states OpenSSH space-joins the operands and runs them via the **remote LOGIN SHELL (`$SHELL -c`)** — this is exactly the surface where a barren `~/.zshenv` bites.
- The remote gate executes changed code via `/bin/sh -lc` (login shell): `internal/daemon/dot_cascade.go:1992` and `:2172` (`runner.Command(ctx, "/bin/sh", "-lc", …)`), precisely so the worker's PATH is loaded from its login-shell rc files.
- The bug-prone git probes route through the same runner seam: `resolveWorktreeHEADVia` at `internal/daemon/pasteinject.go:510` does `runner.Command(ctx, "git", "-C", wtPath, "rev-parse", "HEAD").Output()`; `ensureBaseOnWorker` / `pushBaseToWorker` / `fetchRunBranchBoxA` in `codesync_rs_b8.go` (lines 100, 133, ~210) all go through `tmux.CommandRunner`.
- The production worker config confirms the exposure: `.harmonik/workers.yaml:4` — *"claude auth via CLAUDE_CODE_OAUTH_TOKEN in the worker's `~/.zshenv`"*. The worker's env IS a non-interactive login shell sourcing `~/.zshenv` only. This is the exact shell the bug lives in.

### How tests reach (or fail to reach) those branches

There are two, and only two, ways the remote branches are exercised, and neither is faithful to the bug:

1. **Canned-output mock CommandRunners** (`RecordingRunner`, the L1 `hk52xnrPathRemapRunner`, and similar in `codesync_rs_b8_test.go`, `runner_seam_hkhd2w6_test.go`, `dot_cascade_tool_remote_env_hk230h_test.go`). These implement `CommandRunner.Command` and return a pre-baked `*exec.Cmd` / canned bytes. **They never spawn a shell at all**, so a "shell returns empty stdout because git isn't on PATH" condition simply cannot arise — the mock supplies whatever output the test author expected.

2. **Real `ssh localhost` scenario harness** (`scenario_ssh_localhost_hk8u2al_test.go` / `_l2_` variant, bead hk-8u2al). This does traverse a real `ssh → login shell → /bin/sh` round-trip (its stated purpose is catching the `#`-comment quoting bug, hk-fxy9/hk-538l). **But `localhost` is the operator's own machine**, whose login shell has git on PATH and a fully-provisioned environment. It therefore *cannot* reproduce empty stdout from a barren worker shell. The harness also pins `HandlerBinary: "/bin/sh"` with in-repo fixture scripts (lines 471, 529) — a controlled shell, not a bare worker `~/.zshenv`.

There is **no test anywhere** that: (a) provisions a worker (or localhost account) with a deliberately barren `~/.zshenv` lacking git/PATH; or (b) injects a mock CommandRunner that *simulates a non-interactive login shell returning empty stdout / exit 127 for git*. Grep for `zshenv|zshrc|login shell|non-interactive|empty stdout|-lc` across the test corpus yields only doc-comment mentions and the security/quoting fixtures — none feed empty git output into the daemon's state machine.

### What the gate actually runs against in production today
The daemon on gb-mbp runs **local** (`rbc == nil`) for the overwhelming majority of runs (of the last 20 `run_started`, only 2 carried remote markers). So when the qa node "executes the changed code," it runs `/bin/sh -c` on box A with a normal environment — never the remote `/bin/sh -lc` over SSH into a barren shell. The remote branches only run against the real gb-mbp worker in production, where the bug actually bit — and there is no automated gate replicating that condition.

## 3. Verdict — structurally invisible, and why

**YES, the remote bug class is structurally invisible to the current quality gate.**

The coverage boundary is precise:

- **The seam is exercised; the failure mode is not.** Tests drive the `CommandRunner` seam extensively, but only in two fidelities — canned mocks (no shell) and real `ssh localhost` (a *good* shell). The specific adversarial condition — *a non-interactive login shell that sources only a barren `~/.zshenv`, so `git` is not on PATH and the command yields exit 127 + empty stdout* — is never materialized in any test.
- **Local runs skip the code entirely.** The daemon's default local path is `rbc == nil`; the remote branches are compiled but never entered by the gate on a normal run.
- **The one place empty stdout is guarded is incidental, not systematic.** `resolveWorktreeHEADVia` (pasteinject.go:519) does reject an empty `rev-parse HEAD`, but that guard exists only because `.Output()` also errors on exit 127. Other git probes over the same seam (codesync fetch/push, base-SHA/HEAD comparisons) are not systematically tested to *fail closed* when the remote shell returns empty — which is exactly how "the daemon destroys good state" happens.

### Minimal test infrastructure that would make these bugs visible

1. **A `BarrenShellRunner` mock CommandRunner** (unit-level, fast, no network): for the git/HEAD/verdict probe commands, returns *empty stdout with exit 0* and, in a second mode, *exit 127 (command not found) with empty stdout* — simulating a login shell where git isn't on PATH. Wire it as `rbc.sshRunner` in a `rbc != nil` workloop test and assert the daemon **fails closed** (aborts the run / preserves the worktree / does NOT merge, reset, or destroy state) on empty/absent git output. This is the highest-leverage, lowest-cost fix and catches the whole class at the seam.

2. **A negative-fidelity localhost e2e** (extends the hk-8u2al harness): run `ssh` into a *sandboxed login shell whose `~/.zshenv` is deliberately barren* (e.g. `env -i` + a temp `ZDOTDIR`/`HOME` whose `.zshenv` sets no PATH), so a real `ssh → $SHELL -lc 'git …'` returns empty/127. Assert the same fail-closed invariant end-to-end. This catches PATH/rc-sourcing regressions the pure mock can't model.

3. **A destructive-transition invariant test**: assert as a golden property that *no empty/short git-probe result can drive a state-destroying transition* (worktree teardown, branch reset, merge-to-main). This turns "empty stdout → destroy good state" into a compile-time-adjacent contract rather than a latent runtime hazard.

Item 1 alone would have surfaced the reported production bug; items 2–3 harden against recurrence.

---

### Key file:line evidence
- `workflow.dot` (repo root) — `qa` node, header lines 19–27; committed `0adb6551`.
- `.harmonik/events/events.jsonl` — 21 `node_id:"qa"` dispatches, latest 2026-07-12T14:58:17Z.
- `internal/lifecycle/tmux/runner.go:71–120` — `SSHRunner`; remote LOGIN SHELL delivery documented.
- `internal/daemon/dot_cascade.go:1992, 2172` — remote gate runs `/bin/sh -lc`.
- `internal/daemon/pasteinject.go:510, 519` — HEAD probe over the runner seam; incidental empty-guard.
- `internal/daemon/codesync_rs_b8.go:100, 133` — `pushBaseToWorker` / `ensureBaseOnWorker` over the seam.
- `internal/daemon/workloop.go` — `rbc != nil` gating (3458, 3555–3595, 4240–4376); local == byte-identical (NFR7).
- `internal/daemon/scenario_ssh_localhost_hk8u2al_test.go` — real `ssh localhost` harness = operator's own good login shell; `/bin/sh` fixture handler.
- `.harmonik/workers.yaml:4, 8–11` — gb-mbp worker, `transport: ssh`, auth via worker `~/.zshenv`.
