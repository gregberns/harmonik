# C4 — Multi-repo dispatch — spec-draft (doc-only adoption; NO normative spec text)

> Component C4 produces NO new normative requirement and NO new spec file. Per `02-components.md`
> §C4 and `04-design/` (C4 is a "doc-only adopt hk-3r3" component), C4's deliverable is a
> "known boundaries" statement, not a requirement. This draft file exists for structural
> completeness and records WHERE the boundary text lands.

## Target: portability narrative + scope cross-references (no requirement ID)

The drafted boundary text (to be appended at finalization to the relevant spec scope sections —
NOT as a numbered requirement):

> **Known boundary — single supervised repo.** The daemon lands only changes to the repo it
> supervises: worktrees are rooted at `<projectDir>/.harmonik/worktrees/`; a bead touching another
> repo cannot satisfy the managed-worktree prefix check; no per-bead repo override exists (no
> `repo_url` / `target_repo` / `repoPath` field on the bead record). Cross-repo fixes are applied
> out-of-band. Tracked as open bead **hk-3r3** (adopted under `codename:fleet-portability`);
> designing the multi-repo dispatch mechanism is a separate work.

## Where this is reconciled
- `06-integration.md` §6 — the boundary note + that it is doc-only (no code, no requirement ID).
- `07-tasks.md` T20 (bead) — the documentation task that adopts hk-3r3.

## Why no requirement
The cross-repo dispatch mechanism is an explicit NON-GOAL of this work (`01-problem-space.md`
§Non-goals). C4 only NAMES the boundary and adopts the existing tracking bead; it adds no
spec obligation that code must satisfy. There is therefore nothing to draft as a `MUST` clause.
