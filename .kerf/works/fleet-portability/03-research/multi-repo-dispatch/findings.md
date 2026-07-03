# Research — C4. Multi-repo dispatch (adopt hk-3r3)

> Pass 3 (`research`) of `fleet-portability`. Component C4 per `02-components.md`. Verified
> against the live tree on 2026-06-13. This component is **adoption/documentation only** — it
> names a known boundary and adopts an existing bead; it does NOT design the cross-repo mechanism.

## Research questions

- RQ-C4.1 — Does the bead `hk-3r3` exist, and what exactly does it describe?
- RQ-C4.2 — Is the technical limitation real: are the daemon's worktrees rooted only at the supervised repo, so a bead touching another repo can't be landed?
- RQ-C4.3 — Is there ANY existing cross-repo mechanism (per-bead repo override, multi-repo config) today?

## Findings

### F-C4.1 — hk-3r3 exists, is OPEN, and precisely describes the gap (RQ-C4.1) — CONFIRMED

`br show hk-3r3 --json`:

- **ID:** hk-3r3 — **Status:** OPEN — **Type:** bug — **Priority:** 2 — **Area:** internal/daemon — **Created:** 2026-06-12 — **Related:** hk-2cx.
- **Title:** "daemon: cross-repo dispatch gap — daemon worktree can only land harmonik-repo changes; kerf-repo fixes must be applied out-of-band."
- **Description:** the harmonik daemon worktree can only land changes in the harmonik repo; a fix that belongs to a different repo (e.g. kerf) cannot be dispatched. Concrete case: the kerf-repo fix for hk-2cx had to be applied out-of-band and pushed directly to `/Users/gb/github/kerf` (commit 6fe78df), **unreviewed** by the harmonik review-loop. Proposed: a dispatch-target/repo mechanism so the daemon can route a bead to a configured non-harmonik repo (worktree + build gate + push in that repo).

This corroborates MEMORY ("kerf source is a separate repo — the harmonik daemon can only doc them, fix+push directly in the kerf repo"). **Adopt this bead as the named boundary; do not re-file.**

### F-C4.2 — Worktrees are rooted at the supervised repo only; cross-repo beads can't be landed (RQ-C4.2) — CONFIRMED

- `internal/daemon/claudelaunchspec.go:149-157` — `worktreeRootPath` is derived from the supervised project's `projectDir` via `workspace.WorktreeRootPath(deps.projectDir, workspace.NoWorktreeRootOverride())`, hard-rooted at `.harmonik/worktrees/` inside the supervised repo.
- `internal/daemon/daemon.go:38-45` — `Config.ProjectDir` is "the root directory of the harmonik project" — the single supervised repo, passed once at startup.
- `internal/daemon/claudelaunchspec.go:387,450-467` — `isHarmonikManagedWorktree()` requires the workspace path be a child of that worktreeRootPath. A bead touching a different repo cannot satisfy the prefix check.
- `internal/daemon/daemon.go:552` — `worktreeFactory(ctx, projectDir, runID, headSHA)` has **no repo-selection parameter**.

So the daemon is structurally single-repo. This also aligns with the spec: `process-lifecycle.md` and `workspace-model.md` (WM-013a lease lock at `<workspace_path>/.harmonik/lease.lock`) all scope the worktree surface to the one supervised `projectDir`.

### F-C4.3 — No cross-repo mechanism exists today (RQ-C4.3) — CONFIRMED

A grep for `repo_url` / `target_repo` / `cross_repo` / `repoPath` / `external.*repo` across `internal/` and `cmd/` finds nothing. `internal/core/beadrecord.go` `BeadRecord` carries only `BeadID, Title, Description, BeadType, Status, Labels, Edges, AuditTrailRef, Assignee` — **no repo field**. There is no per-bead repo override and no multi-repo daemon config. The out-of-band path (manual fix + direct push to the other repo, unreviewed) is the only option today.

## Patterns to follow

- This component contributes a **"known boundaries"** statement to the portability spec — a documentation pattern, like the OQ/known-limitation notes elsewhere in the corpus (e.g. process-lifecycle.md's OQ-PL-* entries). State the boundary, cite hk-3r3, document the out-of-band workaround.

## Risks / conflicts (flag for design)

1. **Scope discipline.** The temptation is to design the cross-repo dispatch mechanism here. DON'T — it's a separate, sizeable design (per-bead repo routing, per-repo worktree/build-gate/push, per-repo branch protection). C4's only job is to NAME the boundary and adopt hk-3r3. If the design pass wants the mechanism, spin a new kerf work; keep fleet-portability about deploy/coexistence.
2. **Unreviewed out-of-band fixes are a real risk the boundary should call out.** The hk-2cx precedent (kerf fix pushed unreviewed to commit 6fe78df) shows the workaround bypasses the review gate. The portability spec's boundary note should flag that cross-repo fixes currently escape review — a quality risk to track under hk-3r3, not to solve here.
