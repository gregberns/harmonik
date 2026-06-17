# Cross-Repo Dispatch — Design Note

**Status:** gap documented; guard landed (hk-3r3). Full implementation is follow-up work.

**Bead:** hk-3r3 (child of hk-3js5m, codename:fleet-portability)

---

## Problem

The harmonik daemon creates per-bead worktrees rooted at `<projectDir>/.harmonik/worktrees/`. Every git operation — worktree add, merge, push — targets `projectDir`. A bead whose fix belongs in a *different* repository (e.g. the `kerf` CLI at `/Users/gb/github/kerf`) cannot be dispatched: the daemon would create the worktree in the wrong repo and the implementer agent would commit to the wrong tree.

**Concrete incident:** hk-2cx (kerf fix) was applied out-of-band with a direct push to `/Users/gb/github/kerf` (commit `6fe78df`), bypassing the review loop.

---

## Where the single-repo assumption is baked in

| Location | What it does |
|---|---|
| `workLoopDeps.projectDir` | Single directory threaded through all git ops |
| `workspace.CreateWorktree(projectDir, ...)` | Worktree rooted under `projectDir/.harmonik/worktrees/` |
| `resolveParentCommit(projectDir, ...)` | Resolves start_from SHA from `projectDir`'s git DB |
| `lockedMergeRunBranchToMain(projectDir, ...)` | Merges and pushes within `projectDir` |
| `checkMainWorkingTreeDirty(projectDir, ...)` | Escape-detection scoped to `projectDir` |
| `BeadRecord` struct | Carries no `repo_url`/`target_repo`/`repoPath` field |

The assumption permeates `beadRunOne` end-to-end; there is no partial-dispatch path today.

---

## Guard (landed, hk-3r3)

A bead can now declare its target repo in the `## Branching` section:

```markdown
## Branching

```yaml
target_repo: /Users/gb/github/kerf
```
```

When `target_repo` is set and differs from the daemon's `projectDir`, `beadRunOne` immediately reopens the bead with a `CrossRepoUnsupportedError`:

```
daemon: bead declares target_repo "/Users/gb/github/kerf" but daemon
supervises "/Users/gb/github/harmonik"; cross-repo dispatch not yet
implemented (hk-3r3) — apply fix out-of-band, see docs/cross-repo-dispatch.md
```

This converts the silent wrong-repo failure into a loud, actionable operator message.

---

## Proposed design for full cross-repo dispatch

### Contract

A bead with `target_repo: <abs-path>` is dispatched against the named repo instead of `projectDir`. All worktree, merge, and push operations target `<abs-path>`. The daemon must be configured to trust the named repo (a safelist prevents arbitrary path injection).

### Key decision points

1. **Repo safelist** — daemon startup reads an optional `allowed_repos` list from `.harmonik/config.yaml`. A bead whose `target_repo` is not in the list is reopened with `CrossRepoUnsafeError`. Prevents arbitrary path injection.

2. **Worktree root** — use `<target_repo>/.harmonik/worktrees/` as the worktree root for that run. The harmonik daemon only touches the target repo's worktrees; it never mixes worktrees across repos.

3. **Build gate** — the DOT gate (`go test ./...`) must run in `target_repo`, not `projectDir`. `beadRunOne`'s gate invocation must be parameterised on the active repo root.

4. **Merge and push** — the merge worker and push step both target the run-branch in `target_repo`. The `lockedMergeRunBranchToMain` mutex is per-repo (or at minimum per-`targetBranch` within a repo); concurrent cross-repo merges do not contend.

5. **Review loop** — the reviewer agent's AGENTS.md / CLAUDE.md must reference the target repo's conventions, not harmonik's. The agent-task.md injected into the worktree must reflect `workspace_path` = the target-repo worktree.

6. **`br` adapter** — `br` always targets the harmonik project ledger (`.beads/` under `projectDir`). Bead lifecycle transitions remain in `projectDir`; only the implementation worktree moves to `target_repo`. No change needed to the `br` adapter.

### Decomposition into follow-up beads

| Bead | Scope |
|---|---|
| hk-3r3-a | Add `allowed_repos` safelist to daemon config; parse and validate at startup |
| hk-3r3-b | Parameterise `workspace.CreateWorktree` / `WorktreePath` on an active repo root |
| hk-3r3-c | Thread `activeRepo` through `beadRunOne` (worktree, merge, push, gate) |
| hk-3r3-d | Update agent-task.md injection to use `activeRepo` for `workspace_path` |
| hk-3r3-e | Scenario test: daemon dispatches a kerf-repo bead and it lands in the kerf repo |

These are filed as sub-tasks under hk-3r3 (or a new epic) once the operator confirms scope.

---

## Workaround (today)

Apply cross-repo fixes out-of-band:

```bash
cd /Users/gb/github/kerf
git checkout -b fix/my-fix
# ... make changes ...
git commit -m "fix: my fix\n\nRefs: hk-XXXX"
git push origin fix/my-fix
# open PR manually or cherry-pick to main
```

Then close the bead manually via `br close hk-XXXX --reason "Applied out-of-band: <sha>"`.
