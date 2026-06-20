# Asset Sync — propagating instruction updates into running projects

> Operator requirement (2026-06-20): "When harmonik is updated after running on a
> project, we need the instructions updated too — init isn't good enough. harmonik
> should SYNC the asset files, and those changes get distributed through the
> project (the instruction files)."
>
> Builds on 09 (assets ship via the embed FS). `init` scaffolds ONCE; this doc adds
> the ongoing UPDATE path: `harmonik sync-assets`.

## The problem init doesn't solve

`harmonik init` writes the embedded assets into a project a single time. After that,
the project's instruction files (`.claude/skills/*`, `AGENTS.md`, `.harmonik/context/*`)
are frozen at whatever binary version ran init. When the operator `go install`s a
newer harmonik whose embedded skills/templates have improved, **every already-running
project is stale** and there is no path to pull the improvements down. The patterns
we're consolidating now would themselves rot the moment they ship.

So the product needs a second verb alongside init: **`harmonik sync-assets`** —
reconcile a project's on-disk instruction files against the binary's embedded assets.
`init` becomes "sync into an empty project"; both share one apply-engine.

## The crux: not all assets sync the same way

Instruction files fall into three sync classes (mapping onto the three KINDS from 07).
The whole design lives in treating them differently:

| Class | Files | Who owns the content | Sync policy |
|---|---|---|---|
| **MANAGED** (kind B) | `.claude/skills/*` (incl. the new `orchestrator` standing-rules skill) | the PRODUCT — projects shouldn't edit | **Overwrite** from embed on update. Detect local drift (disk-hash ≠ lock-hash); on drift, don't clobber — emit `<file>.harmonik-new` + flag a conflict. |
| **MANAGED-REGION** (kind B+project) | `AGENTS.md` (router) | shared — universal block is product, deltas are project | Update only **marker-delimited regions** (`<!-- BEGIN harmonik:managed --> … <!-- END -->`); everything outside markers is the project's and is never touched. |
| **CONTENT-OWNED** (kind C) | `.harmonik/context/{project.yaml,captain-lanes.md,roadmap.md}`, `HANDOFF.md` | the PROJECT | **Never overwrite the body.** Sync may: (a) create a file missing entirely (from template), (b) refresh the **self-describing header region** (marker-delimited — same headers from 07), (c) REPORT when the template grew a new section the project lacks (additive suggestion, applied only with `--adopt-new-sections`). |

The self-describing TIER/LOADED-BY/OWNER headers from doc 07 do double duty: they ARE
the managed-region markers for the content-owned files. The header is synced; the body
is owned.

## Versioning — a manifest + a per-project lock

- **Embedded manifest** (in the binary): `asset-path → {version, sha256}`. Generated at
  build time from the embed FS (extend the existing `init_skills_sync_test.go` guard to
  also emit/verify the manifest). Bumps whenever an asset changes.
- **Project lock** `.harmonik/assets.lock`: `asset-path → {version, sha256}` recorded at
  the last init/sync. This is the "what we installed and at what hash" record that makes
  3-way reconciliation possible.

## The 3-way reconcile (per file)

```
embed_hash  = manifest[path].sha256      # what the binary ships now
lock_hash   = assets.lock[path].sha256   # what we last installed
disk_hash   = sha256(project file)       # what's there now (may have local edits)

embed == lock                      → up to date, skip
embed != lock & disk == lock       → FAST-FORWARD: safe to update (no local edits)  → apply
embed != lock & disk != lock       → CONFLICT: project edited a managed file
                                       → MANAGED: write path.harmonik-new + report
                                       → MANAGED-REGION/CONTENT: 3-way merge the managed
                                         region only; report if the region itself conflicts
embed has path, lock lacks it      → NEW asset (e.g. the orchestrator skill on first sync) → create
disk has path, embed lacks it      → project-authored → leave untouched
```

`harmonik sync-assets --dry-run` prints this classification table (the safe path is to
always dry-run first). `--apply` performs updates and rewrites `assets.lock`.
`--commit` additionally commits the result (see distribution).

## Distribution — how the change reaches the whole project

"Distributed through the project" = the updated instruction files are **tracked in git**
(skills, AGENTS.md are committed; the context bodies are project-owned but their headers
are tracked). So:

1. `sync-assets --apply` writes the files into the **main working tree**.
2. A commit (operator, or `--commit`) lands them on the project's default branch.
3. From there git distributes them to every clone and — critically — every daemon
   worktree picks them up on its next checkout. The fleet starts obeying the updated
   contracts without any per-crew action.

## Daemon-safety (LOAD-BEARING) — sync edits the main working tree

Writing skill/AGENTS files into the main working tree while the daemon is dispatching
trips the worktree-escape detector (`implementer_escaped_worktree`) and fails in-flight
beads (same hazard as any main-tree edit — known-workarounds). Therefore:

- `sync-assets --apply` MUST refuse (or auto-quiesce) when the daemon is actively
  dispatching. Gate on `harmonik queue status` + an in-flight-run check; require a lull
  or `--force`.
- Natural home: the **supervisor**. On a detected binary-version bump (running binary's
  manifest version ≠ `assets.lock`), `harmonik supervise` posts a comms/status notice to
  the captain ("assets N versions behind; M files have updates — run sync-assets"), and
  may **auto-apply only the FAST-FORWARD, MANAGED (skill) files during a quiescent
  window**, surfacing every CONFLICT and every content-owned change for human review.

## Trigger model (summary)

- **Manual:** `harmonik sync-assets [--dry-run|--apply|--commit]` — the post-`go install` step.
- **Semi-auto:** supervisor detects the version skew, notifies, auto-applies only safe
  skill fast-forwards in a lull.
- **Two guard layers, don't confuse them:** the in-repo `init_skills_sync_test.go`
  guards the BUILD (canonical `.claude/skills/` == embedded mirror); `sync-assets` guards
  DEPLOYED projects (embedded == on-disk). The manifest ties them together.

## How this lands in the codebase (slots into codename:productization)

The existing productization epic already targets "deployable on new projects." This is
its update-path sibling. New work:
1. Build-time **manifest** generation + verify (extend the sync-guard test).
2. `assets.lock` read/write + the **3-way reconcile engine** (shared by init and sync).
3. `harmonik sync-assets` command (`--dry-run/--apply/--commit/--force`).
4. **Managed-region markers** in AGENTS.md template + the context-file headers.
5. Supervisor **version-skew detection** + notify + safe auto-apply.
6. The `orchestrator` skill + context templates from 09 (the assets this all transports).

## Revised end-to-end model (one line)

`init` lays the three-kinds asset bundle down once; **`sync-assets` keeps it current**
across binary upgrades via a manifest + per-project lock + class-aware 3-way reconcile;
git + daemon-worktree-checkout distribute the result to the whole fleet; the supervisor
watches for skew. Behavioral contracts and state scaffolding become **versioned,
self-updating product features** instead of hand-copied per-repo files.
