# 06 — Does Codex CLI natively support git worktrees under `workspace-write`?

*Research date: 2026-07-20. Codex versions verified locally: `codex-cli 0.144.5` (box-A, this machine). Worker reported at 0.142.0. Investigation was read-only: `codex --help`/`--version`/`sandbox --help`/`exec --help`/`features list` only — Codex never executed a task or edited files.*

---

## Bottom line

**No — Codex does not make `git commit` in a linked worktree "just work" under `workspace-write`, and there is no config key that turns it on.** Codex's default writable set under `workspace-write` is the working directory plus temp dirs (`/tmp`, `$TMPDIR`) — it does **not** auto-include a linked worktree's git *common* dir (`<main-repo>/.git`), which is where `index.lock`, `HEAD`, refs and the object store live. Worse, Codex actively **protects `.git` read-only even when it sits under a writable root**, and for a worktree — whose in-tree `.git` is a `gitdir:` pointer file — it resolves that pointer and protects the *target* read-only too. The **recommended / only** way to let Codex commit inside a worktree today is to grant the git common dir as an explicit writable root (`--add-dir <common-dir>` or `-c sandbox_workspace_write.writable_roots=[<worktree>,<common-dir>]`) — which is exactly what harmonik already does. Upgrading Codex does not fix this and on Linux may regress it. So harmonik's `codexGitCommonDir` injection is a **necessary workaround, keep it** (with one robustness refinement noted below).

---

## Native behavior + config surface

### Default writable roots under `workspace-write`

Documented default (OpenAI docs): *"The workspace includes the current directory and temporary directories like `/tmp`."* Plus `$TMPDIR` on macOS. There is **no** documented default that includes any `.git` directory, and nothing git-worktree-aware in the default writable set.

- Source: [Sandbox — ChatGPT Learn](https://learn.chatgpt.com/docs/sandboxing) ("the agent can read files, edit within the workspace, and run routine local commands inside that boundary"; workspace = cwd + temp dirs).
- The one worktree-aware behavior Codex *does* have is **trust inheritance**, not writability: *"When working in a git worktree, Codex resolves the primary repository working directory and checks whether that root is trusted. If the main project is trusted, all its worktrees inherit that trust decision."* That suppresses repeated trust prompts — it is a completely different facet from sandbox write access and does **not** grant write to the common dir.

### `.git` is protected read-only *inside* a writable root (the load-bearing fact)

OpenAI's docs state (verified via the docs full-text dump, [learn.chatgpt.com/docs/llms-full.txt](https://learn.chatgpt.com/docs/llms-full.txt)):

> "`.git` is protected as read-only whether it appears as a directory or file." … "If `.git` is a pointer file (`gitdir: ...`), the resolved Git directory path is also protected as read-only."
>
> "`.agents` and `.codex` directories are protected as read-only when they exist as directories. Protection is recursive."

This is the crux. A linked worktree's cwd contains a **`.git` *pointer file*** (`gitdir: <main>/.git/worktrees/<id>`). Even if you make the worktree cwd writable, Codex reads that pointer, resolves it, and re-marks the resolved gitdir read-only. So naïvely adding the worktree (or the repo root) as a writable root is *defeated* by the carveout — `git commit` still hits EPERM on `index.lock` / objects. This confirms the underlying problem stated in the task is real and by-design, not incidental.

**Why harmonik's approach still works (macOS seatbelt):** harmonik names the git **common dir itself** (`<repo>/.git`) directly as a writable root, not its parent. The re-protection rule keys off `<writable_root>/.git`; when the writable root *is* `<repo>/.git`, there is no `<repo>/.git/.git` to re-protect, so the carveout doesn't fire and the seatbelt grants write. That is the mechanism that makes `49d7fde3` work.

### Config keys under `sandbox_workspace_write` (full surface)

From [Configuration reference — ChatGPT Learn](https://learn.chatgpt.com/docs/config-file/config-reference):

| Key | Type | Meaning |
|---|---|---|
| `writable_roots` | `array<string>` | Additional writable roots when `sandbox_mode = "workspace-write"`. |
| `network_access` | `boolean` | Allow outbound network in the sandbox. **Default off.** |
| `exclude_slash_tmp` | `boolean` | Exclude `/tmp` from writable roots. |
| `exclude_tmpdir_env_var` | `boolean` | Exclude `$TMPDIR` from writable roots. |

**There is no `include_git_dir`, no `git_writable`, no worktree-aware writable-roots key.** The only lever is `writable_roots` (add paths) — there is no lever to *relax* the `.git` read-only carveout for the current worktree. (That missing lever is precisely what open issue #14338 asks for — see below.)

### CLI / `-c` surface (verified from local `--help`)

- `-s, --sandbox <read-only|workspace-write|danger-full-access>` — top-level and on `exec`.
- `codex exec --add-dir <DIR>` — *"Additional directories that should be writable alongside the primary workspace."* Repeatable. This is the officially-blessed equivalent of injecting a writable root.
- `-c sandbox_workspace_write.writable_roots=[...]` — dotted `-c` override (value parsed as TOML). This is the form harmonik uses.
- `-C, --cd <DIR>` — set the working root.
- `codex sandbox …` subcommand exposes `--sandbox-state-readable-root`, `--allow-unix-socket`, `--log-denials` (useful for diagnosing seatbelt denials) but **no** writable-git flag.

**Official recommended way to run Codex in a worktree:** the docs describe the *desktop-app* worktree flow (Handoff between Local and Worktree, "Create branch here") but give **no CLI recipe** for committing inside a linked worktree under `workspace-write`. In practice the community answer is `--add-dir`/`writable_roots` pointed at the git dir — the same workaround harmonik implements.

---

## Version story

**No Codex version makes worktree commits "just work" under `workspace-write`, and upgrading is not a fix — on Linux it can be a regression.**

- The changelog ([learn.chatgpt.com/docs/changelog](https://learn.chatgpt.com/docs/changelog)) has worktree entries only for the **desktop app** (Handoff/Sync, "Built-in worktree support", 2026-02/03) — none touch the CLI `workspace-write` permission model or `.git`/writable-roots behavior.
- The one substantive sandbox change in the relevant window is a **regression** for Linux: per [issue #14338 — "Allow writable gitdir for current worktree in sandboxed workspace-write mode"](https://github.com/openai/codex/issues/14338) (**OPEN, unresolved**), commit `dcc4d7b` / **PR #13453** (~2026-03-08) changed the Linux **bubblewrap** sandbox to **force-mount `.git` and the resolved gitdir read-only *after* writable roots are applied.** The reporter explicitly notes `--add-dir` cannot fix it because the read-only remount happens last, and that the only escapes are `danger-full-access` or per-command escalation.
- Feature flags confirm nothing native landed: `codex features list` shows `codex_git_commit` = **removed** and `use_linux_sandbox_bwrap` = **removed** (neither is an available knob).
- Related open sandbox bugs on Windows: [#9313](https://github.com/openai/codex/issues/9313) (`.git` entries under writable roots not denied) and [#32168](https://github.com/openai/codex/issues/32168) (Windows split writable roots break) — evidence the writable-roots/`.git` interaction is still in flux across platforms.

**Implication for harmonik's two versions (0.142.0 worker / 0.144.5 box-A):** both post-date the March bubblewrap change. On **macOS seatbelt**, naming `<repo>/.git` a writable root works (as harmonik relies on). On **Linux bubblewrap** (if any remote worker is Linux), the post-#13453 force-remount can defeat even a direct-`.git` writable root — so upgrading/relying on native behavior is *not* a safe path there. **Recommendation: do not chase a Codex version that "just works"; none exists, and Linux trends the wrong way.**

---

## harmonik's current workaround: keep / replace / supersede

**Recommendation: KEEP — it is a necessary workaround, not redundant, and nothing native supersedes it.**

- `codexGitCommonDir` (`cmd/harmonik/substrate_select.go:306-313`) and `codexExecWritableRoots` (`internal/daemon/codexlaunchspec.go:152-162`) inject `[<worktree>, <repo>/.git]` as `sandbox_workspace_write.writable_roots`. That is functionally identical to the officially-supported `codex exec --add-dir <common-dir>`, so it is *aligned* with Codex's blessed mechanism — just expressed via `-c`.
- There is **no** codex-native option (config key or flag) that makes this unnecessary. Issue #14338 is the feature request for one, and it is open/unbuilt.
- **Robustness refinement (optional, worth a bead):** the current derivation is string path-munging — it assumes the `.harmonik/worktrees/` layout and appends `/.git`. That is correct for harmonik's fixed layout and degrades gracefully (returns `""` off-layout), but it will silently miss the real common dir for any non-default worktree root, a submodule, or a custom `GIT_DIR`. The robust source of truth is `git -C <worktree> rev-parse --git-common-dir` (absolute). Consider switching to that where a git call is cheap; keep the string fallback for remote POSIX paths where shelling out is awkward. Not urgent — current code works for the deployed layout.
- **Reconcile with `danger-full-access`:** recent commit `a0619c1c` "force sandbox danger-full-access at launch" and `44831898` "writable git-common-dir so codex commit lands" appear to touch overlapping ground. If a given launch path already runs `danger-full-access`, the `writable_roots` injection on *that* path is moot (full-access has no writable restriction). Worth confirming the two paths (interactive vs `exec`) don't both apply — but the writable-roots injection remains correct and necessary for any path that stays in `workspace-write`. **This is a harmonik-internal reconciliation, not a Codex question.**

---

## exec / shell facet (running tests, `git status`)

**Native answer: spawning subprocesses IS allowed under `workspace-write` — that is the mode's entire purpose.** Docs: *"Codex can read files, make edits, and run commands in the working directory automatically."* So `git status`, test runners, etc. are expected to run. A blanket "cannot spawn a process" is **not** the documented behavior.

**On the observed `"CreateProcess Operation not permitted"`:** UNVERIFIED, but two things stand out:

1. **`CreateProcess` is a Windows API name.** On macOS seatbelt / Linux bubblewrap the denial string would be a bare `EPERM` / "Operation not permitted" without `CreateProcess`. If that exact string was seen, it most likely came from a **Windows sandbox** context (see Windows issues #9313, #32168) — not the macOS/Linux path harmonik normally runs. Flag for the operator to confirm which host produced it.
2. **Most likely it is the *same* writable-root problem, not a subprocess ban.** A spawned `git status`/test step that tries to write a lockfile or metadata **outside** the writable roots (again: the worktree's resolved gitdir) will be denied — and the failure surfaces at the child process, looking like "the subprocess was blocked." Remedy is the same as for commit: ensure the git common dir is a writable root, and keep `/tmp` writable (`exclude_slash_tmp` default = off; do **not** set it true).

**Config remedies to try for the exec facet (if it recurs on macOS/Linux):**
- Confirm `/tmp` and `$TMPDIR` remain writable (defaults) — many tools write scratch there; excluding them breaks subprocesses.
- Add the git common dir writable root (already done) so git subprocesses can write locks.
- Use `codex sandbox --log-denials <cmd>` to capture the *exact* macOS seatbelt denial and see which path was refused — this will disambiguate "process spawn blocked" vs "write outside writable root."
- If genuinely inherent to the seatbelt for a needed path, the only documented escapes are `network_access=true` (for network), per-command approval escalation, or `danger-full-access` — there is no finer subprocess-allow knob.

*Confidence: subprocess-spawn-is-allowed = DOCUMENTED. The specific `CreateProcess` denial's root cause = UNVERIFIED (needs `--log-denials` on the actual failing host).*

---

## Classification table

| Claim | Class | Basis |
|---|---|---|
| `workspace-write` defaults writable = cwd + `/tmp` + `$TMPDIR` | ACTUAL-LIMITATION (documented) | Docs: "workspace includes the current directory and temporary directories like /tmp" |
| Codex auto-includes a worktree's git common dir in writable roots | **ASSUMPTION (FALSE)** — it does not; the operator's surprise is understandable but Codex genuinely doesn't | Docs list no such default; issue #14338 exists precisely because it doesn't |
| `.git` (dir *or* `gitdir:` pointer target) is force-protected read-only even under a writable root | ACTUAL-LIMITATION (documented, by design) | Docs llms-full.txt: ".git … protected as read-only … resolved Git directory path is also protected" |
| There is a config key to make worktrees writable natively | ASSUMPTION (FALSE) | config-reference lists only writable_roots / network_access / exclude_slash_tmp / exclude_tmpdir_env_var; #14338 requests the missing knob |
| Naming `<repo>/.git` *itself* as a writable root bypasses the `.git` carveout on macOS seatbelt | ACTUAL-BEHAVIOR (verified in harmonik prod: 49d7fde3/44831898 land commits) | Carveout keys on `<writable_root>/.git`; when the root *is* `.git` there's no child to re-protect |
| Upgrading Codex makes worktrees "just work" | ASSUMPTION (FALSE) — no such version; Linux regressed post-PR #13453 | Changelog has no such entry; issue #14338 open against current line |
| harmonik must inject the common dir as a writable root | ACTUAL-LIMITATION (necessary workaround) | No native alternative; `--add-dir` is the same thing |
| harmonik deriving the common dir by string-munging the worktree path | SELF-IMPOSED-CONSTRAINT | Chosen for remote POSIX-path safety; `git rev-parse --git-common-dir` is the robust alternative |
| `workspace-write` blocks all subprocess spawns | ASSUMPTION (FALSE) | Docs: mode explicitly runs commands; denials are writable-root scoped |
| The `CreateProcess Operation not permitted` denial | UNVERIFIED | `CreateProcess` = Windows API string; needs `--log-denials` on the failing host to root-cause |

---

## Sources

- [Sandbox — ChatGPT Learn](https://learn.chatgpt.com/docs/sandboxing) (redirected from developers.openai.com/codex/concepts/sandboxing)
- [Configuration reference — ChatGPT Learn](https://learn.chatgpt.com/docs/config-file/config-reference)
- [Codex full docs dump — llms-full.txt](https://learn.chatgpt.com/docs/llms-full.txt) (`.git` read-only quotes)
- [Codex changelog](https://learn.chatgpt.com/docs/changelog)
- [Issue #14338 — Allow writable gitdir for current worktree in sandboxed workspace-write mode (OPEN)](https://github.com/openai/codex/issues/14338)
- [Issue #9313 — Windows: .git entries under writable roots not denied](https://github.com/openai/codex/issues/9313)
- [Issue #32168 — Windows split writable roots break](https://github.com/openai/codex/issues/32168)
- Local read-only: `codex-cli 0.144.5`; `codex exec --help` (`--add-dir`), `codex sandbox --help` (`--log-denials`, `--sandbox-state-readable-root`), `codex features list` (`codex_git_commit`=removed, `use_linux_sandbox_bwrap`=removed)
- harmonik source: `cmd/harmonik/substrate_select.go:286-313` (`codexWorktreeWritableRoots` / `codexGitCommonDir`); `internal/daemon/codexlaunchspec.go:145-173` (`codexExecWritableRoots` / `codexWritableRootsArg`); commits `49d7fde3`, `44831898`, `a0619c1c`
