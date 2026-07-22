# 04 — Live worktree-create incident (gb-mbp, 2026-07-12 ~11:52Z)

Root-cause deep-dive on the CURRENT fleet-down incident. Bead **hk-2hfyt** (REOPENED),
worker **gb-mbp** disabled durable. READ-ONLY investigation.

## TL;DR

The verified failure signature this time is **not** what the code claims. The
run_failed summary for hk-thbbv (below) ends in `: <nil>` — meaning the daemon's
follow-up `git rev-parse HEAD` **exited 0 and returned empty stdout with a nil
error**, NOT that `git worktree add` left an un-checked-out worktree.

That single fact flips the diagnosis: **the worktree is almost certainly created
correctly; it is the daemon's SSH-wrapped HEAD *probe* that returns empty, so the
daemon falsely concludes "empty HEAD", tears the good worktree down
(`rm -rf` + prune + `branch -D`), and burns all 4 retries.** The
"concurrent remote create race" label in the error string is a misattribution —
it cannot be a concurrency race because the run fired at concurrency=1 with the
fleet quiet, and both `worktreeCreateMu` and `mergeMu` serialize the create.

## The ground-truth evidence

### The exact failure summary (events.jsonl, hk-thbbv, 2026-07-12T11:49:26Z)

```
worktree_create_failed: workspace: WorktreeCreationFailed:
git worktree add -b "run/019f5629-11b7-7bac-a801-c2cffb122140"
"/Users/gb/harmonik-worker/repo/.harmonik/worktrees/019f5629-...-c2cffb122140"
"3303712a09eef03fbf0c6fd66cbd2b52a735c6c4":
git worktree add exited 0 but HEAD did not resolve in "...019f5629-...-c2cffb122140"
(concurrent remote create race): <nil>
git output: (empty HEAD after git worktree add — hk-iaj1w)
```

The trailing **`: <nil>`** is the `%v` of `headErr` in the synthesized error at
`internal/workspace/createworktree.go:264-265`. `headErr == nil` means the probe
call did **not** error.

### Why `: <nil>` is decisive — trace the two code paths

`internal/workspace/createworktree.go:257`:

```go
head, headErr := resolveWorktreeHEADViaRunner(ctx, runner, worktreePath)
if headErr == nil && head != "" {
    return nil            // success
}
emptyHEADRace = true      // <-- we land here with headErr==nil, head==""
err = fmt.Errorf("git worktree add exited 0 but HEAD did not resolve in %q (concurrent remote create race): %v",
    worktreePath, headErr)   // headErr printed as <nil>
```

`resolveWorktreeHEADViaRunner` (`createworktree.go:146-152`):

```go
out, err := runner.Command(ctx, "git", "-C", wtPath, "rev-parse", "HEAD").Output()
if err != nil {
    return "", err
}
return strings.TrimSpace(string(out)), nil
```

So we reached `emptyHEADRace = true` via `headErr == nil && head == ""`. That is
**only** possible if `.Output()` returned `err == nil` (remote `ssh` process AND
remote `git` both exited 0) **and** stdout, after `TrimSpace`, was the empty
string. In other words: the SSH-wrapped `git -C <wtPath> rev-parse HEAD` produced
**no stdout at all and exited zero.**

This is a different failure than the daemon-side `resolveWorktreeHEADVia`
(`internal/daemon/pasteinject.go:506-521`), which distinguishes the two cases and
would have said "returned empty" — but the create-time check in `createworktree.go`
collapses "errored" and "empty-but-ok-exit" into the same `emptyHEADRace` branch,
which is why the log looks like a race when it is not.

### Concurrency is ruled out in code, not just by observation

- `CreateWorktree` holds `cfg.createMu` across the **entire** add+probe+retry loop
  (`createworktree.go:163-166`), and the daemon threads `deps.worktreeCreateMu`
  into it (`workloop.go:3599-3602`).
- The whole `ensureBaseOnWorker` + `wtFactory` sequence also runs inside
  `deps.mergeMu` (`workloop.go:3636-3668`).
- The incident fired at concurrency=1, fleet quiet.

So no sibling `git worktree add` / `fetch` was in flight. The `(concurrent remote
create race)` string is stale wording inherited from hk-iaj1w/hk-5qp7z and is
actively misleading triage.

### Base-reachability is already handled (and is NOT this bug)

`ensureBaseOnWorker` (`internal/daemon/codesync_rs_b8.go:133-151`) now fetches
`origin <sha>`, verifies with `git cat-file -t <sha>`, and — when the SHA is
absent (unpushed from box A) — **pushes the commit directly box-A→worker** over
SSH (`pushBaseToWorker`, lines 100-120; the hk-2hfyt residue jamis landed in
756f0c52). This is why the incident notes now say "base commit IS present
(cat-file = commit)". The 2026-07-07 root cause (unpushed base) is fixed; the
current failure survives it. hawat's own bead comment (2026-07-12 11:58Z)
confirms the scope is the *wrapped checkout no-op*, distinct from base-fetch.

## Mechanism: why does the SSH-wrapped `rev-parse HEAD` emit empty stdout, exit 0?

`SSHRunner.Command` (`internal/lifecycle/tmux/runner.go:111-121`) builds:

```
ssh <opts> <host> -- 'git' '-C' '<wtPath>' 'rev-parse' 'HEAD'
```

Every token is single-quoted (`shellQuoteArg`, runner.go:103-105). OpenSSH does
NOT pass discrete argv — it space-joins the post-host operands into one string and
runs it via the worker's **login shell `$SHELL -c`** (documented in the SSHRunner
doc comment, runner.go:72-91). On the darwin worker `$SHELL` is zsh, which for a
non-interactive `-c` invocation sources **`~/.zshenv` only** (not `.zshrc`,
`.zprofile`, `.zlogin`).

Candidate mechanisms for "exit 0 + empty stdout", ranked:

1. **`git rev-parse` receives no effective rev argument → prints nothing, exits 0
   (STRONGEST fit).** `git rev-parse` with zero revs exits 0 and emits no output.
   `git rev-parse HEAD` where the `HEAD` token is dropped/absorbed produces
   *exactly* the observed signature (exit 0, empty stdout, nil err). Every other
   git failure mode for `rev-parse HEAD` (missing repo, unborn HEAD, bad object)
   exits **non-zero**, which would set `headErr != nil` — contradicting the `<nil>`
   we see. So the argument vector, as reconstructed by the worker's login shell,
   effectively lost/neutralized the `HEAD` rev. Something in the
   `~/.zshenv`-sourced non-interactive environment (an alias/function shadowing
   `git`, a `git` wrapper on the non-login PATH, or a `GIT_*` env var that makes
   `rev-parse` a no-op) is the prime suspect. This is the divergence from the
   MANUAL test, which the operator runs in an **interactive login** shell
   (full `.zshrc`/`.zprofile`, different PATH/aliases) — a materially different
   environment even on the same box. Note hk-2hfyt already established the two
   shells resolve *different git binaries* (non-login → homebrew 2.54.0;
   login → Apple 2.50.1), proving the environments genuinely diverge.

2. **The worktree add genuinely succeeded and HEAD *does* resolve manually, but the
   probe’s stdout is being swallowed by the transport/login-shell** (e.g. a
   `.zshenv` that redirects fd 1, emits then truncates, or a shell wrapper that
   eats stdout). Same net effect: the probe lies, the good worktree is destroyed.
   This is consistent with "manual `git worktree add` + manual `rev-parse` both
   succeed" while only the daemon path no-ops.

3. **`git worktree add` under the newer non-login git (2.54.0) writes a HEAD form
   the probe can’t read as a plain SHA** — possible but weaker: a broken/dangling
   HEAD makes `rev-parse HEAD` exit **non-zero**, which would surface as
   `headErr != nil`, not `<nil>`. The `<nil>` evidence argues against this.

Mechanisms (a) wrong-dir, (c) quoting-mangle, and (b) fs-not-settled from the task
brief are all weak here: paths are absolute with `-C`, tokens are individually
single-quoted (UUID paths carry no metacharacters), and a local APFS filesystem is
coherent across separate processes instantly, so a second SSH connection cannot
observe a "not-yet-settled" worktree at concurrency=1.

**Primary hypothesis:** the SSH-wrapped `git -C <wtPath> rev-parse HEAD`, executed
by the worker's non-interactive login shell sourcing only `~/.zshenv`, returns
empty stdout with exit 0 — either because the reconstructed command loses the
`HEAD` rev or because a shell/env shim swallows git's stdout. The worktree itself
is fine. The daemon misreads this as an empty-HEAD "race", destroys the worktree,
and exhausts retries — every attempt hits the same deterministic environment, so
retry cannot help (the 4x identical fast-fail is the fingerprint of a deterministic
env defect, not a race).

## Why the prior "fixes" don't stop it

- **hk-iaj1w / fb11aabd** added the post-add `rev-parse HEAD` validation +
  retry-on-empty-HEAD. It assumed empty-HEAD is a *transient concurrent* race, so
  its remedy is teardown + retry. Against a *deterministic* probe defect the retry
  loop just re-destroys a good worktree 4 times and fails louder. The fix
  introduced the very check that now produces the false negative.
- **hk-5qp7z / d92b16de** wrapped the add+probe loop in `createMu`
  (`createworktree.go:163-166`) to serialize concurrent creates. Correct for the
  real concurrent race, but orthogonal to a concurrency=1 deterministic failure —
  serialization does nothing when there is nothing to serialize.
- **hk-2hfyt / 756f0c52 (jamis)** fixed base-reachability (push box-A→worker).
  Necessary and now in place, but the failure survives a present base — it is
  downstream of the probe, not the base.

Net: all three fixes target *worktree creation*; the actual defect is in the
*post-create HEAD probe* under SSH wrapping. None of them looked at whether the
probe itself is trustworthy.

## Fix options

### Minimal / immediate (make the probe honest)

1. **Distinguish "probe errored / empty" from "add failed" and prove it against the
   real worktree before declaring empty-HEAD.** In `createworktree.go:257-266`,
   when `head == "" && headErr == nil`, do a direct existence/validity check
   through the same runner before tearing down — e.g. `git -C <wtPath> rev-parse
   --verify HEAD` (exit code is authoritative; `--verify` exits non-zero on a real
   unborn/broken HEAD) and/or `test -e <wtPath>/.git`. If the worktree is actually
   valid, treat the empty stdout as a **probe read failure**, not a create failure:
   do NOT `rm -rf` the worktree; either accept it or retry only the probe. This
   alone would have kept the good worktree and let the run proceed.

2. **Harden the remote git invocation against login-shell env divergence.** Run the
   probe (and ideally all worker git) with an explicit, environment-independent
   form: e.g. `sh -lc` replaced by a fixed absolute git path and cleared `GIT_*`
   env, or route through the same base64/`sh -c` idiom `remotematerialize.go` uses
   so the command string is not re-split by the interactive-vs-login shell. The
   goal: make the daemon's `rev-parse` reconstruct byte-identically to the manual
   one regardless of `~/.zshenv`.

3. **Fix the misleading error string** at `createworktree.go:264` — drop
   "concurrent remote create race" (it is provably wrong at concurrency=1) and
   include the raw probe stdout/exit so the next incident is diagnosable without
   log archaeology.

### Definitive diagnostic (run before committing a fix)

On the worker, reproduce the daemon's EXACT wrapped call vs the manual one and diff
the stdout:

```
ssh -o ControlMaster=no -o ControlPath=none <host> -- \
  'git' '-C' '<a-real-worktree>' 'rev-parse' 'HEAD'      # daemon path (non-login zsh)
```

vs an interactive login `git -C <worktree> rev-parse HEAD`. If the wrapped form
prints empty / a different binary / an env-shimmed result, the env-divergence
hypothesis is confirmed and points at the exact `~/.zshenv` line. hawat's
acceptance test MUST exercise the *wrapped* path (the prior hk-rs-validate-898a did
not), or it will keep passing while production fails.

## Would a resident worker-binary structurally eliminate this class?

**Yes.** This entire bug family — hk-iaj1w, hk-5qp7z, hk-fxy9 (tmux `#{}` comment
truncation), hk-gglt (python `-c` re-split), hk-z8ek (writes landing on box A),
hk-lt091 (ControlMaster serialization) — all stem from the same structural choice:
the daemon ships **individual git/shell commands over SSH, each reconstructed by
the worker's login shell**, with per-invocation quoting, per-invocation new TCP
connections, per-invocation `~/.zshenv` sourcing, and no shared process context.
Every one of these fixes is a patch on the *marshalling seam*.

A resident worker-binary (the daemon's own code running natively on the worker,
invoked over a structured RPC that carries a discrete argv vector and an explicit,
controlled environment) would:

- run `git worktree add` and `rev-parse HEAD` **in the same process, same cwd, same
  env, with `exec` argv** — no login shell, no `~/.zshenv`, no re-splitting, no
  quoting layer to mangle `HEAD` or swallow stdout;
- make the add and its HEAD-validation **atomic and co-located** (no second SSH
  connection, no cross-connection ambiguity);
- reuse the identical, already-correct LOCAL (`nil`-runner) code path on the
  worker, so "local is clean, remote is cursed" divergence disappears by
  construction.

The current SSHRunner seam guarantees the daemon's remote environment will *never*
byte-match the operator's manual test, so "manual works, daemon no-ops" will keep
recurring in new disguises. Patching the probe (fix options 1-3) stops *this*
incident; the resident-binary architecture is what removes the *class*.

## File:line index

- `internal/workspace/createworktree.go:146-152` — `resolveWorktreeHEADViaRunner` (`.Output()`, nil-err-empty-stdout path)
- `internal/workspace/createworktree.go:236-284` — add + HEAD-validate + retry loop; `:257` probe, `:264` synthesized (mis-labeled) error
- `internal/workspace/createworktree.go:163-166` — `createMu` full-loop serialization (hk-5qp7z)
- `internal/workspace/createworktree.go:213-227` — `cleanupPartialState` (rm -rf → prune → branch -D) that destroys the good worktree
- `internal/lifecycle/tmux/runner.go:72-121` — SSHRunner: login-shell re-split, per-token single-quote
- `internal/daemon/pasteinject.go:506-521` — daemon-side `resolveWorktreeHEADVia` (distinguishes empty vs error, unlike create-time)
- `internal/daemon/codesync_rs_b8.go:60-151` — `fetchBaseOnWorker` / `ensureBaseOnWorker` / `pushBaseToWorker` (base now guaranteed present)
- `internal/daemon/workloop.go:3592-3665` — remote `wtFactory`, mergeMu + createMu wiring, ensureBaseOnWorker call
- events.jsonl 2026-07-12T11:49:26Z (hk-thbbv, run 019f5629-11b7-...) — the `: <nil>` summary
```
