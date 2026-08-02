# Cross-Repo Dispatch

**Status:** implemented in `67581586a` (`hk-xfuc`).

The daemon can run a bead in a repository other than its project directory.
The operator must list that repository in the daemon safelist first.

## Configure a target repository

Add each permitted absolute path to `.harmonik/config.yaml`:

```yaml
daemon:
  allowed_repos:
    - /Users/gb/github/kerf
```

Then add `target_repo` to the bead's `## Branching` section:

~~~~markdown
## Branching

```yaml
target_repo: /Users/gb/github/kerf
```
~~~~

An empty `target_repo`, or one equal to the daemon project directory, is a
local run. An absent or empty `allowed_repos` list permits no cross-repo run.

The daemon compares the declared path and the safelist entry as exact strings.
It does not normalize paths or resolve symlinks. Use the same absolute spelling
in both places.

## Dispatch behavior

For an allowed cross-repo bead, the target repository is the active repository.
The daemon creates the worktree there. It resolves branching there. It runs the
escape and no-commit checks there. It merges and pushes there. The workflow also
asks the target repository whether a previous run already landed the bead.

The harmonik project directory remains the control directory. It owns the bead
ledger, daemon socket, queue files, event log, and workflow definition. The
project's protected-branch list does not apply to a target repository because
it names branches in the project repository.

If a bead names a repository that is not in `allowed_repos`, the daemon reopens
the bead with `CrossRepoUnsafeError`. Its reason names the missing safelist
entry. This check prevents an arbitrary bead body from selecting a path for Git
commands.

The contract does not define a cross-repository run on a remote worker.

## Implementation map

`internal/projectconfig/projectconfig.go` `AllowedRepos` reads the safelist.
`internal/daemon/workloop_runplan.go` `resolveRunPlanPlace` selects the active
repository and refuses an unsafe target. `internal/daemon/branching.go`
`CrossRepoUnsafeError` and `isInAllowedRepos` provide the refusal surface and
exact-match rule.

This document is explanatory. The normative contract is in
`specs/process-lifecycle.md` and `specs/operator-nfr.md`.
