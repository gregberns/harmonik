# 02 — Codex Execution Model inside harmonik

> Research pass for the 2026-07-20 codex-strategy realignment. Daemon/comms DOWN; grounded in code, git, beads, kerf.
> Pin: HEAD `49d7fde3` on branch `phase1-session-restart-substrate`. All `file:line` refs are against that tree.

---

## Bottom line

harmonik has **two entirely separate Codex code paths**, and the one that actually drives production implementer crews is **`codex exec` one-shot CLI** (built by `internal/daemon/codexlaunchspec.go`), *not* the structured JSON-RPC **app-server** driver in `internal/codexdriver`, which is opt-in only (`HARMONIK_SUBSTRATE=codexdriver`) and not wired into the default crew loop. Codex's own native sandbox does the real containment: harmonik launches `codex exec` under `sandbox_mode="workspace-write"` plus an explicit `sandbox_workspace_write.writable_roots` override, because on the installed codex-cli 0.142.0 the "no seatbelt" mode (`danger-full-access`) is silently **not honored** under ChatGPT auth. The load-bearing bug (hk-daegv) was that the `.git`-writable-roots fix first landed only on the app-server `thread/start`/`thread/resume` path — a path production never exercises — so codex's own `git commit` kept failing EPERM on the real `codex exec` runs until commit `49d7fde3` stamped the same writable roots onto the exec argv. A persistent, reconnectable **remote** app-server client (resident sidecar over ssh) is **partially built in-process but neither production-wired nor proven over ssh** — the resident client type exists, the supervised detached sidecar is HELD, and Phase-2 acceptance FAILED/HALTED on 2026-07-19.

---

## App-server vs codex-exec — which path runs where

Two distinct substrates, selected at different layers:

| Path | Package / entry | Invocation shape | When used |
|---|---|---|---|
| **codex-exec (one-shot CLI)** | `internal/daemon/codexlaunchspec.go` → `buildCodexLaunchSpec` | `codex exec --json …` / `codex exec resume <thread_id> …` | **Production implementer-crew harness** (`Harness="codex"`). Runs through the normal `handler.LaunchSpec` → tmux/SSH spawn path. |
| **app-server (JSON-RPC over stdio)** | `internal/codexdriver` (`driver.go`, `session.go`, `resident.go`), wire in `internal/codexwire` | `codex app-server` + `initialize` → `thread/start` / `thread/resume` NDJSON | **Opt-in only**: engaged solely when `HARMONIK_SUBSTRATE=codexdriver`. Not the default; intended for the *resident crew-orchestrator* experiment (Phase-2). |

**codex-exec is the production path — evidence:**
- The codex harness (`internal/daemon/codexharness.go:80-113`) converts a run into a `codexRunCtx` and calls `buildCodexLaunchSpec` — this is the crew harness the daemon dispatches.
- Initial turn argv: `codexlaunchspec.go:236` → `args = []string{"exec", "--json", "-c", ...}` then `-C <worktree> <seed>` (`:243`).
- Resume turn argv: `codexlaunchspec.go:230` → `args = []string{"exec", "resume", *rc.priorThreadID, "--json", "-c", ...}`. So **resume = `codex exec resume <thread_id>`**, a *fresh one-shot process* re-attaching by thread id, not a held connection.
- `codexcommit.go:20-25` states it plainly: "`codex exec --json` is one-shot run-to-exit (CompletionProcessExit) — there is no live REPL to re-prod and no second chance inside the same turn."
- Bead **hk-daegv** Ruling A (2026-07-19 22:17 UTC): *"the implement node ran via 'codex exec --sandbox workspace-write'"* — confirms live production runs use the exec path.

**app-server is opt-in and driver-resident:**
- `cmd/harmonik/substrate_select.go:75-81`: `selectSubstrate` returns the tmux substrate unless `os.Getenv(substrateSelectEnv) != "codexdriver"`; only `=="codexdriver"` builds `codexdriver.NewCodexSubstrate(opts)`.
- `substrateSelectEnv = "HARMONIK_SUBSTRATE"` (`substrate_select.go:31`); comment: "the structured Codex app-server driver … by explicit opt-in only" (`:22-30`).
- The driver speaks `initialize` (`session.go:775`), `thread/start` (`session.go:789`), `thread/resume` (`session.go:848`); the wire is JSON-RPC 2.0 NDJSON (`driver.go:1-6`). Server-held thread state survives child death and is re-attached via `thread/resume` on respawn (`resident.go:16`, `session.go:838-848`).
- **Reviewer always stays on tmux/Claude** regardless: `selectSubstrate` returns `reviewerSubstrate = tmuxSub` (`substrate_select.go:76,80`; rationale hk-qxvc2 at `:73`).

> The two paths **share nothing** at the sandbox layer — the writable-roots fix had to be applied *twice*, once per path (see below). This duplication is the root of hk-daegv.

---

## Sandbox — codex-native vs harmonik-imposed

### What Codex does natively (its partial sandbox)
Codex-cli owns the real containment (macOS seatbelt / Linux landlock). Its knobs:
- `sandbox_mode` ∈ {`read-only`, `workspace-write`, `danger-full-access`} — `workspace-write` permits writes only under the cwd + declared roots; `danger-full-access` disables the seatbelt entirely.
- `sandbox_workspace_write.writable_roots = [...]` — extra absolute paths writable under `workspace-write`.
- `approval_policy` / `-a` / `approval=never` — whether codex pauses to ask before exec/apply-patch.
- `--sandbox <mode>` (exec-only flag) and the global `-c key=value` config override, which works for both `exec` and `exec resume`.

**Key native quirk harmonik must work around:** on the installed **codex-cli 0.142.0 under ChatGPT auth, `danger-full-access` is NOT honored** — codex silently runs the effective `workspace-write` seatbelt anyway (hk-daegv, multiple assessor repros 2026-07-19; `substrate_select.go:254-262`, `codexdriver/driver.go:152-155`). So the only reliable containment override is `workspace-write` + explicit `writable_roots`.

### What harmonik imposes — codex-exec path (production)
`internal/daemon/codexlaunchspec.go`:
- Sandbox mode, initial turn: `-c sandbox_mode="workspace-write"` (`:236`).
- Sandbox mode, resume turn: same, `:230`. (`codex exec resume` rejects `--sandbox`, `--add-dir`, `-C`, so the uniform `-c` override is used — `:208`.)
- Writable roots: `-c sandbox_workspace_write.writable_roots=[<cwd>,<repo>/.git]` — built by `codexWritableRootsArg` (`:164-173`) from `codexExecWritableRoots` (`:152-162`), which appends the linked-worktree **git common dir** (`<repo>/.git`) because it holds `index.lock` and lives *outside* the worktree cwd (`:145-162`). Appended to argv at `:231-233` (resume) and `:237-239` (initial).
- Note: this path uses **`workspace-write`, not `danger-full-access`** — least-privilege, deliberately, because full-access isn't honored (`:230`,`:236`).
- Env hardening (`buildCodexEnv`, `:300-362`): strips `OPENAI_API_KEY`/`CODEX_API_KEY` and re-emits them empty (`:341-344`), forces `CODEX_HOME` (`:346-347`), guarantees `PATH` (`:335-339`).
- A **fail-closed billing guard** materializes `forced_login_method=chatgpt` and refuses to launch on unforced/API-key config (`:258-263`).

### What harmonik imposes — app-server path (opt-in)
`cmd/harmonik/substrate_select.go` (this is the file the task called "codexlaunchspec.go ~186-209"; the sandbox config actually lives in `codexSubstrateOptions`, `:221-271`):
- Launch args: `[]string{"app-server", "-c", ` + `sandbox_mode="danger-full-access"` + `}` (`:240`).
- Per-thread posture fields: `Sandbox: "danger-full-access"`, `ApprovalPolicy: "never"` (`:252-253`; constants `:130-133`).
- Writable-roots hook: `WritableRoots: codexWorktreeWritableRoots` (`:263`), which returns `[worktreeCwd, <repo>/.git]` (`:286-295`); the driver stamps these as `runtimeWorkspaceRoots` on **every** `thread/start` and `thread/resume` (`codexdriver/session.go:814-838`, `driver.go:146-168`).
- So the app-server path *claims* `danger-full-access` **and** ships writable-roots as belt-and-suspenders — comment: full-access is "harmless forward-intent for a codex build that does honor" it (`:261-262`).

**The two paths disagree on sandbox mode**: exec = `workspace-write`+roots (what actually runs in prod); app-server = `danger-full-access`+roots (opt-in, unproven at scale).

---

## Local vs remote mechanics — where config reaches (or fails to reach) the process

**The codex binary lives ON the worker; only the command is streamed.** SSHRunner runs `ssh <host> -- codex exec …`; codex-cli must be installed on the worker (bead evidence: *"Observed codex-cli 0.142.0 on gb-mbp"*, hk-daegv). Nothing about the codex binary is shipped over the wire — only argv/env/cwd.

**codex-exec remote routing (production):** the daemon's work loop picks a worker per-run via `SelectWorker` and threads an `SSHRunner` (`workloop.go:744-765`, `:1123-1130`); the `handler.LaunchSpec` from `buildCodexLaunchSpec` is spawned through that runner. `WorkDir` sets cwd; on resume, `-C` is dropped (rejected by `exec resume`) and `WorkDir` alone sets the subprocess cwd (`codexlaunchspec.go:228-229,269`). Config reaches the process **inside the argv itself** (`-c sandbox_mode=…`, `-c …writable_roots=…`), so it travels with the ssh command uniformly local or remote — this is why the exec path's writable-roots fix is transport-agnostic.

**app-server remote routing (opt-in):** `codexWorkerRoutingRunner` (`substrate_select.go:101-210`) is a late-bound composition-root runner. `Command`/`CommandInDir` peek the live worker registry per-run (`:154-176`, `:195-210`): an enabled `ssh`-transport worker → `SSHRunner{Host, Opts:[ControlMaster=no,ControlPath=none]}`; otherwise LocalRunner. It **fails closed** (`requireBoundary`, `:107-116`): with no enabled ssh worker it returns a deliberately non-existent argv0 (`:123`) so `exec.Start` dies rather than run `danger-full-access` **unsandboxed on the daemon host**. The daemon-side guard mirrors this (`workloop.go:3610-3632`). `CommandInDir` (hk-czb11, `:178-210`) applies cwd *on the worker* (`cd … && exec …`) — an earlier bug set the LOCAL ssh process's `cmd.Dir` to the remote-only worktree path and fork/exec-ENOENT'd (fixed `942069eb`/`f8d3a42e`).

### The hk-daegv failure: where writable_roots did NOT reach the process
This is the precise known-bug mechanism the realignment cares about:

1. On codex-cli 0.142.0/ChatGPT auth, `danger-full-access` is ignored → effective `workspace-write`, sole writable root = worktree cwd.
2. A **linked worktree's git common dir** (`<repo>/.git/worktrees/<id>/` under `<repo>/.git`) is a **parent** of the worktree — outside the writable root. So codex's **own `git commit` EPERMs on `index.lock`**, *and* its `/bin/zsh -lc 'git …'` `exec_command` shell fails to spawn under the same seatbelt (hk-wwyse facet).
3. Codex then **exits 0 with no commit** — a silent no-op — and only the **daemon-side fallback** (`codexcommit.go` `ensureCodexRefsTrailer`, invoked from `workloop.go:5127-5140`, git run *outside* the sandbox) commits the applied patch to `run/<id>`.
4. **The dangerous part:** Fix B (commit `44831898`) added the writable-roots hook that stamps `runtimeWorkspaceRoots` — but **only on the app-server `thread/start`/`thread/resume` path**, which production *never exercises*. The real `codex exec` runs still shipped **no** `writable_roots`, so `.git` never reached the seatbelt and commits kept EPERM-ing (hk-daegv Ruling A, 2026-07-19 22:17).
5. **Fix** (`49d7fde3`, on HEAD): `codexExecWritableRoots` + `codexWritableRootsArg` stamp `-c sandbox_workspace_write.writable_roots=[cwd,<repo>/.git]` onto the **exec argv** too. Now both paths carry it.

**Status:** the exec-path fix is compiled into HEAD (`49d7fde3` is an ancestor of HEAD — verified), but **hk-daegv remains OPEN** pending a live `commit_landed=TRUE` proof on a remote worker. Treat "commits land on remote codex-exec" as **fixed-in-code, not yet field-verified**.

---

## Persistent-remote-app-server — built / planned / absent

The task's target ("run codex on a remote machine, point at it, stream work" under the app-server model) requires a **persistent, reconnectable JSON-RPC client holding a socket to a resident `codex app-server`**. Current state, decomposed:

| Capability | State | Evidence |
|---|---|---|
| In-process **resident client** holding one logical session across child deaths, lazy revive + `thread/resume` re-attach, bounded input queue | **BUILT (in-process only)** | `internal/codexdriver/resident.go` `ResidentSession` (`:1-40`); reconnect = respawn→`initialize`→`thread/resume` (`resident.go:16,114,279`; `session.go:838-848`). In-process test coverage (`resident_test.go`, `resume_internal_test.go`). |
| **Supervised detached sidecar** surviving daemon redeploy (own tmux session), watchdog liveness + per-turn deadline, backpressure (-32001), mid-session auth expiry | **PLANNED / HELD** | hk-160yb (OPEN, P2, "persistent supervised app-server sidecar … reconnect/thread-resume/backpressure/watchdog") re-files the swept hk-nzzos; hk-g0ror.2 CLOSED-as-dup with `[HELD]` — "do not staff build until Spike B verdict positive AND admiral gates Phase-2." |
| **Production wiring** of the resident client into the daemon composition root | **ABSENT (blocked)** | hk-8su2m OPEN: "wire ResidentSession + PreSpawn(cleanCodexStaleWAL) into daemon composition root" — not yet done. |
| **Real-ssh remote resume proven** end-to-end | **ABSENT / FAILED** | hk-g0ror.4 acceptance = **FAIL/HALT** (2026-07-19): crit-4 reconnect/resume "NOT REACHED (blocked upstream)"; "real-ssh resume still unproven." Route-into-boundary + guard-held PASSED; commits-land FAILED (hk-czb11). |

**Design exists, implementation does not.** `.kerf/works/codex-app-server/` holds a ratified-ish design (`spec.yaml status: tasks`; `01-problem-space.md`, `04-design/orchestrator-session-model-design.md`) whose central bet is that a resident server-side-thread session could **retire the keeper/compaction machinery**. But the whole Phase-2 line (epic hk-g0ror, OPEN) is gated: acceptance failed, the sidecar is HELD, and cutover is unwired.

**Net:** a persistent *remote* app-server client is **NOT built** in any production sense. The reconnect *primitive* exists in-process (`ResidentSession`); the supervised remote sidecar + real-ssh resume + daemon wiring are **planned/held/failed**. The production remote story today is `codex exec resume <thread_id>` — a fresh one-shot process per wake re-attaching by thread id over ssh, explicitly the *opposite* of a held connection (and "Option B `codex exec resume` per-wake re-invoke was permanently killed by operator decision 2026-07-11" as the *orchestrator* model — yet it remains exactly what the *implementer* harness does).

---

## Claude substrate — for contrast (1 paragraph)

Claude runs on `substrate=tmux` and is the default (`selectSubstrate` returns `tmuxSub` unless opted out; reviewer is *always* tmux). A Claude agent is a **long-lived interactive TUI process** — `claude --remote-control <label> --session-id <uuid>` — living in a **tmux pane on the host** (or on a worker over ssh, but still a pane, not a subprocess pipe). It is driven by **hooks** (hook-relay signals `agent_ready`, etc.) and **re-tasked while alive** by bracketed-paste into the live pane; because the context window grows, it needs the **keeper** to run handoff→`/clear`→`/session-resume` before the pane overflows (`.kerf/works/codex-app-server/01-problem-space.md`). Contrast with codex-exec: no pane REPL, no paste re-task, no keeper — codex is spawned, runs one turn to exit (`CompletionProcessExit`), and any next turn is a brand-new `codex exec resume` process; codex has **no TUI/paste path** (`codexharness.go:130`), and its sandbox/containment is codex-native seatbelt rather than tmux/hook mediation.

---

## Classification table

| # | Requirement / behavior | Tag | Basis |
|---|---|---|---|
| 1 | Production implementer crews run via **`codex exec` one-shot**, not app-server | ACTUAL-LIMITATION | `codexharness.go:80`, `codexlaunchspec.go:236`, hk-daegv Ruling A |
| 2 | App-server driver is **opt-in** (`HARMONIK_SUBSTRATE=codexdriver`), off by default | SELF-IMPOSED-CONSTRAINT | `substrate_select.go:31,75-81` |
| 3 | Reviewer is **always** tmux/Claude even on the codex path | SELF-IMPOSED-CONSTRAINT | `substrate_select.go:76,80` (hk-qxvc2) |
| 4 | codex-exec runs under **`workspace-write`**, not `danger-full-access` | ACTUAL-LIMITATION | `danger-full-access` not honored on 0.142.0/ChatGPT (hk-daegv); `codexlaunchspec.go:230,236` |
| 5 | Writable-roots must include **`<repo>/.git`** or codex's own commit EPERMs | ACTUAL-LIMITATION | `codexlaunchspec.go:145-162`; hk-daegv |
| 6 | Writable-roots fix reached only the app-server path first, missing exec (the bug) | ACTUAL-LIMITATION (now fixed-in-code) | Fix B `44831898` app-server-only → exec fix `49d7fde3`; hk-daegv Ruling A |
| 7 | Sandbox config travels **inside the argv** (`-c …`), so it is transport-agnostic on exec | ACTUAL behavior (design property) | `codexlaunchspec.go:230-239` |
| 8 | App-server `danger-full-access` requires an **enabled ssh worker boundary**; fail-closed else | SELF-IMPOSED-CONSTRAINT | `substrate_select.go:107-176`, `workloop.go:3610-3632` (hk-5h759) |
| 9 | codex binary is **installed on the worker**, command streamed over ssh | ACTUAL-LIMITATION | hk-daegv ("codex-cli 0.142.0 on gb-mbp") |
| 10 | Daemon-side commit **fallback** covers codex's sandbox-blocked commits | SELF-IMPOSED-CONSTRAINT (safety net) | `codexcommit.go`, `workloop.go:5127-5140` |
| 11 | Persistent **remote** app-server client / supervised sidecar | PLANNED (HELD) | hk-160yb OPEN, hk-g0ror.2 HELD, hk-8su2m unwired |
| 12 | In-process resident reconnect + `thread/resume` primitive | BUILT (in-process, unproven over ssh) | `resident.go`; hk-g0ror.4 crit-4 NOT REACHED |
| 13 | Real-ssh remote **resume** proven end-to-end | ABSENT / FAILED | hk-g0ror.4 FAIL/HALT 2026-07-19 |
| 14 | Resident session could **retire the keeper** (central design bet) | ASSUMPTION (unverified) | `.kerf/works/codex-app-server/01-problem-space.md`; not yet tested |
| 15 | Claude = long-lived tmux TUI, hook-driven, keeper-managed | ACTUAL (contrast) | problem-space doc; `codexharness.go:130` |

**UNVERIFIED flags:** #14 (keeper-retirement) is an untested design hypothesis. #6's "fixed" status is code-verified (on HEAD) but **not field-verified** — hk-daegv stays OPEN pending a live `commit_landed=TRUE` remote proof. The exact installed codex-cli version on any given worker is a moving target (no-external-version-binding); 0.142.0 is the version in the hk-daegv repros.
