# Research — Component B: codex-exec sandbox posture (D3)

> Source (do NOT re-run — cite): `plans/2026-07-20-codex-strategy-realignment/research/05-commit-ownership-and-sandbox-mechanism.md`
> and `.../research/06-codex-worktree-support.md`; DECISIONS §D3 (+ D2-direction, findings valid);
> `_plan.md` §3b/§4.

## Questions
1. What is the TRUE mechanism of the codex commit/shell failures under `workspace-write`?
2. Does `danger-full-access` dissolve BOTH the commit failure AND the `exec_command` denial?
3. What survives of commit `49d7fde3`, and what becomes dead?
4. What is the residual live-verification debt?

## Findings (with evidence — research/05 & 06)
- **F1 — the failure is a kernel/Seatbelt filesystem EPERM, NOT an approval denial** (research/05
  finding #4): errors are `Unable to create '…/.git/worktrees/…/index.lock': Operation not
  permitted` / "unable to index file … updating files failed" (`LEG-B.md:64-68`,
  `LIVE-GATE-yankee.md:75`) — because a **linked worktree's `.git` common dir lives OUTSIDE the
  `workspace-write` writable root**. `approval_policy=never` was already in force, so codex *ran*
  the command; it failed at the FS layer.
- **F2 — the SAME sandbox blocks codex's own `exec_command` shell steps** (tests, `git status`,
  multi-step git) — research/05 finding #3; the commit-fallback does NOT rescue these. This is the
  exec-facet.
- **F3 — `danger-full-access` removes the seatbelt entirely** → no writable-root restriction,
  full FS + exec (D3 §"dissolves the entire commit/worktree/shell-step problem"; research/06
  reconcile: writable_roots is **inert/moot** on any danger-full-access path). With no seatbelt
  there is nothing to deny either the `.git` write OR the `exec_command` spawn. **High confidence,
  reasoned; live-verification owed** (_plan §4a). (Caveat: the literal `CreateProcess` string in
  one log is a *Windows* API name; harmonik runs macOS/Linux where the denial is a bare EPERM —
  either way, no seatbelt ⇒ no denial.)
- **F4 — `49d7fde3` disposition = SUPERSEDE, not revert** (research/05 finding #5; _plan §4b):
  KEEP the `-c sandbox_mode` override mechanism (real bug fix — `codex exec resume` rejects
  `--sandbox`/`--add-dir`/`-C`, only `-c` accepted on resume); CHANGE the value
  `workspace-write` → `danger-full-access` on BOTH branches (`codexlaunchspec.go:230,236`);
  DELETE only the now-dead writable-roots derivation (`codexExecWritableRoots`,
  `codexWritableRootsArg`, and the app-server twin `codexWorktreeWritableRoots`/`codexGitCommonDir`,
  `substrate_select.go:286-320`). Do NOT `git revert` the commit wholesale.
- **F5 — live-verification debt:** research/05 finding #12 flags the danger-full-access self-commit
  path is coded but **not yet live-verified on deployed codex 0.142.0**. The end-to-end proof
  (Pass §5 acceptance / Step 2) retires this debt. No external-version binding: don't pin 0.142.0.

## Patterns to follow
- Mirror the existing hk-daegv test shape: a `TestBuildCodexLaunchSpec` asserting
  `danger-full-access`, no `--sandbox`, no `writable_roots` on both the initial and resume argv.
- Leave the app-server `codexHeadlessSandbox` constant (`substrate_select.go:131`, already
  `danger-full-access`); only its `WritableRoots` hook + helpers are removed.

## Risks / conflicts
- The dissolution of the exec-facet is **reasoned, not yet live-proven** — flagged as the go/no-go
  gate in sequencing. If a bead does NOT clear locally, fan out on the failure BEFORE hardening.
