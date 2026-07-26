# RUN-LOG — assessor RE-GATE · pin `d59d5d32`

| field | value |
|---|---|
| PASS_ID | `regate-d59d5d32` |
| PIN | `d59d5d325ab55e2b4e293f9ac0f49bda042e86bc` (branch tip `phase1-session-restart-substrate`) |
| PRIOR GATE | `fff3d937` → **BLOCK** (`runs/codex-substrate-validation/REGATE-VERDICT.md`) |
| AUTHORITY | operator standing directive 2026-07-18: keep testing, switch commits independently, do not wait on the admiral |
| STARTED | 2026-07-22 (assessor session, fresh wake) |

## Frame — why this pass

The prior BLOCK named **two INDEPENDENT defects** (explicitly "not a causal chain"), each with a prescribed remedy:

1. **hk-daegv** — codex 0.142.0 on the worker ignored `-c sandbox_mode="danger-full-access"` for the exec seatbelt (rollout showed `workspace-write` + `Operation not permitted`); codex's own `git commit` failed, `commit_landed:false`. Remedy: *adapt to the installed codex version, do NOT pin* ([[no-external-version-binding]]).
2. **hk-qxvc2** — `CLAUDE_CONFIG_DIR` never reached the remote claude process (`ps eww` on live pid 98702 showed zero `CLAUDE_CONFIG_DIR`); ssh does not forward client env, so `cmd.Env` set only the LOCAL ssh process. Claude read the shared `~/.claude.json`, wedged, `agent_ready_timeout` 150s → `run_failed`. Remedy: *propagate `LaunchSpec.Env` into the remote exec*.

**Both remedies have since landed** — so a fresh gate is warranted (only a fresh assessor PASS re-opens the prod codex reboot):
- `f907b702` forward env to remote ssh agents (hk-qxvc2/hk-okqyx) — introduces `handler.RemoteExecArgv` rewriting the launch to `env KEY=VAL … <binary> <args>` executed in place by the remote login shell.
- `44831898` + `49d7fde3` codex writable git-common-dir + `sandbox_workspace_write.writable_roots=[<worktree>, <repo>/.git]`, moving OFF `danger-full-access` to `workspace-write` — the adapt-not-pin remedy.
- `37651569` route claude reviewer onto tmux substrate; `2b921fde` valid per-run tmux input buffer name; `d59d5d32` local codex on LocalRunner + danger-full-access (hk-tckw3.1).

## Claimed-done reconciliation (assessor, direct read)

- **hk-qxvc2 remedy — MATCHES root cause.** `git show f907b702`: `handler.RemoteExecArgv(spec.Env, spec.Binary, spec.Args)` returns `name="env"` with `KEY=VAL` pairs preceding the binary; the commit body states the exact diagnosed mechanism ("ssh does not forward client env … `cmd.Env` only set the LOCAL ssh process"). Tests added on BOTH remote paths: `TestHandler_Launch_RemoteCwd_ForwardsEnvViaArgv_hkqxvc2` (asserts `CLAUDE_CONFIG_DIR` present AND positioned before the binary) and `TestSpawn_RemoteRunner_ForwardsEnvViaArgv_hkokqyx`.
- **hk-daegv remedy — MATCHES root cause + honors adapt-not-pin.** `git show 49d7fde3`/`44831898`: `codexExecWritableRoots` builds roots `[worktreeCwd, <repo>/.git]`, emitted as `-c sandbox_workspace_write.writable_roots=[…]` alongside `-c sandbox_mode="workspace-write"`. Rationale in-code: a linked worktree's git common dir lives OUTSIDE the worktree writable root, so codex's own `git commit` could not write objects/refs. No version pin introduced.
- **ATTRIBUTION CAVEAT (recorded up front):** local `codex-cli` is now **0.144.5** — the version the prior fix "was only verified on"; the BLOCK was caused by worker-side **0.142.0**. Fix AND version may both have changed. A Leg-B PASS must therefore record the WORKER's codex version, or the pass cannot distinguish "fix works" from "version drifted".

## Prior findings — outcome

All 8 P1/P2 findings from the `baseline-35e4b3b9` pass are **CLOSED**: hk-l5saf, hk-3hozm, hk-nddg1 (P1); hk-cjqyn, hk-9ngiv, hk-hi53s, hk-13ff4, hk-bzol4 (P2).

## [in flight]

- **Leg A (local)** — build/provenance, fixes-compiled-in, targeted + package tests, root-cause-match judgment. Delegated.
- **XT (exploratory)** — adversarial bug-hunt on the 6 new commits; priority angles: argv quoting/injection in the new `env KEY=VAL …` remote launch, **secret exposure via remote argv (`ps`-readable tokens)**, writable-roots over/under-grant + git-common-dir path derivation, tmux buffer-name collision, over-permissive local danger-full-access. Delegated.
- **Leg B (live decider) — RECON ONLY, not executed.** Determining how the prior Leg B was stood up (isolated vs prod worker path), the minimal isolated re-run, exact acceptance assertions, preconditions, and blast radius. Delegated. Standing note: do NOT touch prod `/Users/gb/harmonik-worker` or live fleet daemon (pid 22753).

## Findings this pass

- **hk-n64rw** [P3] CLI: unrecognized subcommand silently STARTS the daemon (+supervisor-watchdog) instead of erroring. `daemon` is not in the SUBCOMMANDS list; daemon flags are "used without a subcommand", so `harmonik daemon status` / `harmonik daemon --help` / any typo falls through to a start. Observed directly: emitted `harmonik daemon starting in …` + `supervisor-watchdog: started`, saved here only by the existing pidfile lock (pid 22753). Fix: dispatch unknown subcommands to a usage error.

## Refuted (checked, NOT filed)

- **Stale pidfile lock** — hypothesis that the pidfile was locked with no live daemon. REFUTED: daemon alive as pid 22753 (`/Users/gb/go/bin/harmonik --project /Users/gb/github/harmonik --no-auto-pull`), `lsof .harmonik/daemon.pid` shows it holding FD 5u. `pgrep -fl 'harmonik daemon'` missed it only because "daemon" is not in the cmdline. Live fleet healthy and undisturbed.
