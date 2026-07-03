# C4 — Multi-repo dispatch — Design (doc-only adoption of hk-3r3)

> Pass 4 (`design`) of `fleet-portability`, component **C4**. This component is doc-only:
> it NAMES the cross-repo dispatch limitation as a known fleet boundary and ADOPTS the existing
> open bead `hk-3r3`. It designs NO mechanism — building cross-repo dispatch is an explicit
> NON-GOAL (`01-problem-space.md` §Non-goals) and a separate work.

## Current state
The daemon lands only changes to the repo it supervises. Worktrees are rooted at
`<projectDir>/.harmonik/worktrees/` (`internal/daemon/claudelaunchspec.go` `WorktreeRootPath`);
a bead touching another repo cannot satisfy the managed-worktree prefix check. There is no
per-bead repo override and no multi-repo config — confirmed: no `repo_url` / `target_repo` /
`repoPath` field on the bead record. Cross-repo fixes (e.g. a kerf-repo fix surfaced while
supervising the harmonik repo) are applied out-of-band today. This limitation is filed as the
open bug bead **hk-3r3** but is not stated as a portability boundary anywhere normative.

## Target state
The portability narrative explicitly STATES the single-supervised-repo boundary and references
hk-3r3 as the tracked gap, documenting the out-of-band path until that bead lands. hk-3r3 is
adopted under `codename:fleet-portability`. NO requirement ID, NO code, NO new spec file.

## Rationale / requirements traceability
- Satisfies G4 (`01-problem-space.md`): "cross-repo dispatch gap documented & adopted" and SC6
  ("cross-repo boundary is named"). The mechanism itself is out of scope by Non-goal.
- Research `03-research/multi-repo-dispatch/findings.md` confirmed the worktree-prefix constraint
  and the absence of any per-bead repo field — there is nothing to relax, only to name.

## Mechanism
- `06-integration.md` §6 carries the boundary note.
- `07-tasks.md` T20 is the documentation bead that lands the note + adopts hk-3r3.
