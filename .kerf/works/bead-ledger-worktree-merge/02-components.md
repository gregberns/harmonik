# bead-ledger-worktree-merge — Components

> Pass 2 output for the `bead-ledger-worktree-merge` kerf work (spec jig).
>
> Created 2026-05-30. Decomposes the JSONL worktree-merge problem into five spec/implementation areas. Research inputs already in hand from `01-problem-space.md` (Report A: merge-driver; Report B: shared-DB). Design pass resolves the architecture choice.

## Component overview

| # | Area | Spec file(s) | Priority |
|---|------|-------------|----------|
| 1 | Git-level merge strategy | `specs/beads-integration.md` (new §BL-MRG) | Blocker — architecture choice |
| 2 | Worktree agent write surface | `specs/beads-integration.md` (amend BI-010, BI-011, BI-025e) | Blocker — new BI-010e |
| 3 | Daemon integration | `internal/daemon/workloop.go` + spec annotation | Follows C1 |
| 4 | Reconciliation extension | `specs/reconciliation.md` | Follows C1/C2 |
| 5 | Integration-branch coordination | `specs/beads-integration.md` (new §BL-IBC) | Can be stubbed if C1 covers it |

---

## Component 1: Git-level merge strategy

### Purpose
Replace the lossy `git checkout --theirs .beads/issues.jsonl` workaround with a correct, explicit merge contract. **Architecture choice:** union merge-driver (`harmonik beads-merge`) vs. shared SQLite via `BD_DB`.

### Design question (must be answered in Pass 4)
Does the merge-driver alone cover child-bead-spawn correctly, or does stale-at-fork make it insufficient?

- Merge-driver (Report A): ~40 LOC, union-by-ID, `updated_at` LWW. Solves: merge-conflict, child-bead-spawn. Does NOT solve: stale-at-fork (agent `br show <parent>` fails mid-run if parent was created on main after worktree fork). Integration-branch degrades with N-way merges.
- Shared-DB (Report B): `BD_DB=<main>/.beads/beads.db` env var, gitignore JSONL in worktrees. Solves: all four problems (merge-conflict, child-bead-spawn, stale-at-fork, integration-branch). More spec surface; open questions on orphan sweeps and multi-writer.

**Suggested design-pass answer:** merge-driver as Phase 1 (ship fast, closes data-loss bug), shared-DB as Phase 2 (closes stale-at-fork, unlocks child-bead-spawn at scale). C1 spec must cover both phases with a migration note.

### Requirements (what the spec must describe)

**BL-MRG-001.** Name the merge driver: `harmonik beads-merge`. Registration in `.gitattributes` as `merge=beads-union`. Driver command: `harmonik beads-merge %O %A %B %P`.

**BL-MRG-002.** Algorithm: union-by-ID from `{O, A, B}`. For each ID:
- Present in one side only (beyond ancestor) → take it (covers child-bead-spawn).
- Present in both A and B, equal → take either.
- Present in both and differ → pick row with larger `updated_at`.
- Optional: union `labels` + `dependencies` arrays (monotonic-additive; add as a config flag).

**BL-MRG-003.** Unresolvable conflicts (same bead closed on A, reopened on B within the same `updated_at` second) → emit a line to `.beads/merge-conflicts.log` for operator audit; do NOT fail the merge.

**BL-MRG-004.** Daemon MUST run `br sync --import-only` against its SQLite after any merge that touches `.beads/issues.jsonl`, to keep the DB consistent with the merged JSONL.

**BL-MRG-005.** (Phase 2 migration path) If `BD_DB` env is set, worktrees SHOULD skip JSONL entirely: the env var points at main's SQLite, and `.git/info/exclude` suppresses the worktree copy. When `BD_DB` is active, `BL-MRG-001`–`004` are no-ops (no JSONL in worktree to merge).

### Out of scope for this component
- Bead deletion (resurrect-from-other-side is accepted; operator can `br close` after).
- Schema migrations (last-writer-wins on whole row; merge-driver exits 0).
- Multi-machine synchronization.

---

## Component 2: Worktree agent write surface

### Purpose
Define precisely which `br` write operations are permitted from inside a worktree agent, and introduce a new write category (BI-010e) for agent-spawned child beads.

### Requirements (what the spec must describe)

**BI-010e (new clause, amend beads-integration.md).** Child-bead-spawn creates.
- An implementer agent MAY call `br create` inside a worktree to spawn child beads (e.g. research decomposition → task beads). This is a new write category alongside the existing `claim` write.
- Child-bead creates MUST include the parent bead ID as a `codename:` label (e.g. `codename:hk-<parent-id>`) so orphan-sweep reconciliation can identify them.
- The create MUST be idempotent if retried: agent SHOULD check `br list --label=codename:hk-<parent-id>` before creating, to avoid duplicates across retry.
- On merge, the union merge-driver (BL-MRG-002) naturally preserves child-bead creates; no additional daemon action required.

**BI-011 amendment.** Current BI-011: "no intra-run writes to the bead ledger (except claim and terminal transitions via adapter)." Amendment: permit child-bead creates (BI-010e) and parent-bead updates (labels, notes) as additional permitted intra-run write categories. Terminal transitions remain daemon-only (BI-010).

**BI-025e preservation.** Concurrent `br` invocations across processes continue to be permitted (SQLite WAL). No change required.

### Failure contract
If an agent issues a prohibited write (terminal `br close`/`br update --status=closed` from inside a worktree), the daemon's post-merge `br sync --import-only` will overwrite it from the JSONL — which itself will be the unioned version from the merge. Net effect: terminal write from inside worktree MAY persist if the merge-driver's LWW happens to pick the worktree row. This is the pre-existing risk from BI-010; the spec must document it explicitly.

---

## Component 3: Daemon integration

### Purpose
Specify changes to `mergeRunBranchToMain` (workloop.go ~L1891-2036) required to implement C1 and C2.

### Requirements

**D3-001.** At worktree-create, daemon SHOULD set `BD_DB` to `<main-repo>/.beads/beads.db` in the agent subprocess environment IF the Phase 2 shared-DB path is chosen (BL-MRG-005). Otherwise no change at create time.

**D3-002.** `mergeRunBranchToMain` MUST NOT call `git checkout --theirs .beads/issues.jsonl`. With the merge-driver registered in `.gitattributes`, the driver runs automatically during rebase/merge. The explicit `--theirs` override suppresses the driver.

**D3-003.** After any merge that touches `.beads/issues.jsonl`, daemon MUST call `br sync --import-only` before invoking any further `br` operations (e.g. terminal-transition `br close`). This ensures the daemon's SQLite reflects the merged JSONL state.

**D3-004.** If `br sync --import-only` fails, daemon MUST treat it as a reconciliation event (`Cat-N: bead-ledger-import-failure`) rather than silently continuing. Emit a `bead_sync_failed` event to `.harmonik/events/events.jsonl`.

### Code sites
- `internal/daemon/workloop.go` `mergeRunBranchToMain` (~L1891, ~L1944 — the `--theirs` line).
- `internal/workspace/agenttask_chb028.go:327` — worktree-spawn env-injection site for Phase 2 `BD_DB`.

---

## Component 4: Reconciliation extension

### Purpose
Extend `specs/reconciliation.md` with new failure categories introduced by the merge-driver and child-bead-spawn patterns.

### New failure categories to add

**Cat-BL1: child-bead orphan.** A child bead (BI-010e) exists on main with `codename:hk-<parent-id>` label, but the parent run was discarded (no commit with `Refs: hk-<parent-id>` found in git). Investigator rule: emit `orphaned_child_bead` reconciliation event. Resolution: `br close <child-id> --reason="Orphaned: parent run <run-id> discarded"`.

**Cat-BL2: bead-ledger-import-failure.** Daemon received a `bead_sync_failed` event (D3-004). Investigator rule: attempt `br sync --import-only` again; if it still fails, emit `bead_ledger_corrupt` escalation for operator.

**Cat-BL3: merge-conflict-log has entries.** `.beads/merge-conflicts.log` is non-empty after merge. Investigator rule: parse log, surface conflicted bead IDs to operator, emit `bead_ledger_conflict_audit` event.

---

## Component 5: Integration-branch coordination

### Purpose
Define bead-state visibility when harmonik dispatches against an integration branch (target != main).

### Assessment
- **Merge-driver (Phase 1):** Integration-branch worktrees write to their own JSONL copy. When the integration branch merges to main, the N-way merge triggers the union driver for each bead file. Partial solution — sibling harmonik instances on main do NOT see integration-branch beads until the integration-branch lands. Child-bead creates are safe on merge but invisible during the run.
- **Shared-DB (Phase 2):** All agents regardless of target branch point at the same `beads.db`. Integration-branch coordination is a non-problem — all writes are immediately visible.

### Requirements (to be fully specified in design pass)

**BL-IBC-001.** Spec MUST define whether harmonik supports concurrent dispatch against both main and an integration branch simultaneously. If no (current assumed answer), state it explicitly; reconciliation rule Cat-BC1 applies (conflicting branch dispatch = operator error).

**BL-IBC-002.** Until Phase 2 shared-DB is adopted, spec MUST document the visibility gap (integration-branch beads invisible to main-branch instance during run) and note the mitigation: single-branch dispatch at a time.

---

## Research gaps

These questions are unresolved from the problem-space pass. Design pass (Pass 4) must answer them:

| # | Question | Blocking |
|---|----------|----------|
| R1 | Does `BD_DB` pointing at main's `.beads/beads.db` cause `br sync` to write JSONL into *main's* working tree mid-run? | Phase 2 decision |
| R2 | With N>1 concurrent harmonik instances all writing to shared DB, does SQLite WAL serialize correctly under sustained load? | Phase 2 decision |
| R3 | If a run fails and is discarded, what sweeps orphaned child beads (Cat-BL1)? Does the existing reconciliation investigator run automatically? | C4 design |
| R4 | Should the merge-driver union `labels` and `dependencies` by default, or only `updated_at` LWW on the whole row? | C1 implementation |

## Pass 3 research tasks

Given research is already substantially complete (Reports A and B in problem-space), Pass 3 should be lightweight:
- R1: empirical test — create a test worktree, set `BD_DB`, call `br sync`, inspect main working tree.
- R2: check SQLite WAL docs / beads_rust source for explicit multi-writer claims.
- R3: check existing reconciliation investigator in `specs/reconciliation.md` for whether it already covers orphan-style cases.
- R4: consult `br sync --merge` source/docs for how `labels` and `dependencies` are handled.

## Next pass

`kerf status bead-ledger-worktree-merge research` — investigate R1–R4 above, then advance to design.
