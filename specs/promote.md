# harmonik promote — spec

```yaml
---
title: harmonik promote
spec-id: promote
requirement-prefix: PR
spec-category: runtime-subsystem
status: reviewed
spec-shape: requirements-first
version: 0.1.0
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-06-13
depends-on:
  - operator-nfr
  - process-lifecycle
---
```

Bead: hk-pk3p1 (reconciles hk-gax8v, which is subsumed by this design).

### 4.a Subsystem envelope

Tags: mechanism

(a) Events produced: none (`promote` operates offline without a running daemon).

(b) Events consumed: none.

(c) Types introduced: none cross-subsystem.

(d) Handlers implemented: none (`promote` is a standalone CLI command, not a daemon handler).

(e) State owned: none persistent (uses a temporary git worktree, auto-removed on completion).

(f) Control points provided: none.

(g) NFRs inherited: `ON-055` (offline surface); `promote` does not abort in-flight runs per ON-INV-006.

(h) Boundary classification: all promote ops are `Tags: mechanism`; `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent`.

## Purpose

`harmonik promote` formalises two previously-manual workflows into a single race-safe command:

1. **push-mode** — the captain bypass-SOP: cherry-pick already-reviewed banked
   commits onto the target branch with a build gate and non-ff-safe push.
2. **PR-mode** — open a pull request from a head branch (default `integration`)
   to the target branch via `gh pr create`. Required when the target branch is
   protected.

The command operates without a running daemon and is safe to invoke concurrently
with a live daemon (it works in a temp git worktree, never the live main tree).

## Command surface

    harmonik promote <sha>...           push-mode
    harmonik promote --pr               PR-mode

### Flags

| Flag | Default | Description |
|---|---|---|
| `--project <dir>` | cwd / `$HARMONIK_PROJECT` | Project root |
| `--target <branch>` | `branching.yaml` `lands_on`, else `main` | Target branch |
| `--bead <id>` | — | Bead ID to stamp as `Harmonik-Bead-ID` trailer on cherry-picked commits; enables `harmonik reconcile` auto-close of the salvaged bead. If absent, promote auto-detects from the source commit subject's `(hk-xxx)` parenthetical. |
| `--pr` | — | PR-mode; mutually exclusive with positional SHA args |
| `--from <branch>` | `integration` | PR-mode: head branch |
| `--title <text>` | — | PR-mode: passthrough to `gh pr create` |
| `--body <text>` | — | PR-mode: passthrough to `gh pr create` |
| `--dry-run` | — | Print planned actions; mutate nothing |
| `--protect-branch <branch>` | — | Repeatable; operator override for protect list |

### Mutual exclusion

`--pr` and positional SHA arguments are mutually exclusive — passing both is an
argument error (exit 1).

## Protection gate (fail-closed, both modes)

1. Load `.harmonik/branching.yaml` via `internal/branching.Load(projectDir)`.
2. Resolve `target`: CLI `--target` > `branching.yaml` `lands_on` > `"main"`.
3. Resolve `protect_branches`: CLI `--protect-branch` flags > `branching.yaml`
   `protect_branches`.
4. **push-mode only**: if `target ∈ protect_branches` → exit 5 with message:
   `target "<b>" is a protected branch; use 'harmonik promote --pr' to open a
   pull request instead`.
5. PR-mode is always allowed regardless of `protect_branches`.

## Push-mode algorithm

Push-mode operates entirely inside a **temp git worktree** created from
`origin/<target>`. It never touches the live main working tree (safety on a
multi-agent box where the daemon may be mid-merge).

1. `git fetch origin <target>` — update the remote tracking ref.
2. `git worktree add --detach <tmpdir> <remote-tip>` — create a temp worktree at
   the fetched remote tip (detached HEAD).
3. For each `<sha>` in argument order: `git cherry-pick -x <sha>`. The `-x` flag
   records provenance in the commit message, matching the manual bypass-SOP. On
   conflict or error: `git cherry-pick --abort`, remove the temp worktree, exit 2.
   - **3a. Stamp bead trailer**: after each successful cherry-pick, resolve the
     bead ID — `--bead <id>` if provided, else auto-detect from the source
     commit subject's `(hk-xxx)` parenthetical. If a bead ID is found, run
     `git commit --amend --no-edit --trailer "Harmonik-Bead-ID: <id>"`.
     This trailer is what `harmonik reconcile` and the daemon orphan sweep use to
     detect subsumed beads (Cat 3c auto-close). Without it, a salvaged bead stays
     `in_progress` forever. The amend failure is non-fatal — push proceeds with a
     warning, but `harmonik reconcile` will not be able to auto-close the bead.
4. **Build gate**: if `go.mod` is present in the temp worktree:
   - `go build ./...`
   - `go vet ./...`
   On failure: remove temp worktree, exit 3 with the failing output.
5. **Race-safe push** (up to `maxPromotePushAttempts` = 3 attempts):
   - `git push origin HEAD:<target>` from the temp worktree.
   - On success: print the pushed tip SHA, remove temp worktree, exit 0.
   - On non-fast-forward (`[rejected]` / `non-fast-forward`):
     - `git fetch origin <target>` to update the remote tracking ref.
     - `git rebase origin/<target>` in the temp worktree to replay the
       cherry-picks onto the new remote tip.
     - On rebase conflict: `git rebase --abort`, remove temp worktree, exit 4.
     - Loop back to step 4 (build gate + push) with the rebased tree.
   - On non-recoverable push error: remove temp worktree, exit 4.
6. Remove the temp worktree (deferred cleanup; always runs).

**This retry loop is the key correctness improvement over the manual bypass-SOP**,
which had no retry and raced the daemon's own merges to the target branch
(ref: captain-deploy-non-ff-race incident, memory `reference_captain_deploy_nonff_race.md`).

## PR-mode algorithm

1. Verify `gh` is on PATH; exit 1 with a clear message if absent.
2. Resolve `target` (same as push-mode) and `head` (`--from`, default `integration`).
3. Run: `gh pr create --base <target> --head <from> [--title ..] [--body ..]`
4. Inherit `gh`'s stdout/stderr so the PR URL is visible to the operator.
5. **Never push `<target>` directly** in PR-mode.
6. Exit 0 on success, exit 1 on `gh` failure.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Argument / flag / config / tool-not-found error |
| 2 | Cherry-pick conflict |
| 3 | Build gate failed |
| 4 | Push failed (all retries exhausted or rebase conflict on retry) |
| 5 | Push-mode refused: target branch is protected |

## Implementation notes

- The temp worktree is created via `os.MkdirTemp` + `git worktree add --detach`.
  Cleanup is deferred so it runs even on early-exit paths (conflict, build failure).
- `mergeRunBranchToMain` in `internal/daemon/workloop.go` owns a similar
  build-gate + non-ff-safe-push sequence for daemon-managed bead merges. That
  function is daemon-internal and non-exported. The promote command re-implements
  the same two steps locally to avoid creating a public API for daemon internals.
  If drift becomes a problem, extract a shared `internal/gitops` helper.
- The `--protect-branch` operator-override flag mirrors the daemon's `--protect-branch`
  flag and exists for operator scripts that need to specify the protection list
  explicitly without a `branching.yaml`.
