# Spec Draft: beads-integration.md — BL-MRG + BI-010e + BI-011 amendment

**Pass 5 — 2026-05-30**
**Target file:** `specs/beads-integration.md`

---

## Changes

### 1. Amend BI-011 (line ~257) — add child-bead-spawn to permitted intra-run writes

**Current text (BI-011):**
```
#### BI-011 — No intra-run writes to Beads

Harmonik MUST NOT write per-node workflow transitions, outcome details, or fine-grained failure types to Beads. Intra-run state MUST live in the git checkpoint trail per [execution-model.md §4.4] and the JSONL event log per [event-model.md §6.2]. Writing every intra-run micro-transition to Beads is forbidden because it would thrash Beads's `blocked_issues_cache` and flood other Beads consumers.
```

**Replacement text:**
```
#### BI-011 — Permitted and prohibited intra-run writes to Beads

Harmonik MUST NOT write per-node workflow transitions, outcome details, or fine-grained failure types to Beads. Intra-run state MUST live in the git checkpoint trail per [execution-model.md §4.4] and the JSONL event log per [event-model.md §6.2]. Writing every intra-run micro-transition to Beads is forbidden because it would thrash Beads's `blocked_issues_cache` and flood other Beads consumers.

**Permitted intra-run write categories:**

| Category | Who issues | Write op | Constraints |
|----------|-----------|----------|-------------|
| `claim` | Daemon (pre-spawn) | `br update --status=in_progress` | Existing; routes through adapter per BI-012; idempotency per BI-029 |
| `child-bead-spawn` | Implementer agent (inside worktree) | `br create` | New (BI-010e); MUST include `codename:hk-<parent-id>` label; MUST check for existing child beads before creating |
| `parent-bead-label` | Implementer agent (inside worktree) | `br update` (labels or notes only) | Informational only; MUST NOT change status |

All other writes from inside a worktree run are prohibited. Terminal transitions (`br update --status=closed`, `br update --status=failed`, `br close`) from inside worktrees violate BI-010 and MUST NOT be issued by agent code.

**Failure contract.** If an agent issues a prohibited terminal write from inside a worktree, the daemon's post-merge `br sync --import-only` (BL-MRG-004) will re-import the union-merged JSONL, and the daemon's terminal-close via the §4.8 adapter runs on top. Net effect: the prohibited write MAY persist if the merge-driver's `updated_at` LWW happened to favor the worktree row. This risk is pre-existing (acknowledged in BI-010); it is documented here for explicitness.
```

---

### 2. Add BI-010e after BI-010d (~line 252)

**New clause to insert:**
```
#### BI-010e — Child-bead-spawn creates (agent-issued)

An implementer agent running inside a harmonik worktree MAY call `br create` to spawn child beads (e.g., a research bead decomposing into N task beads). Child-bead-spawn creates are permitted intra-run writes per BI-011.

**Constraints on child-bead creates:**

1. **Lineage label required.** Every child bead MUST carry the label `codename:hk-<parent-id>` (where `<parent-id>` is the parent bead's ID). This label is the orphan-sweep anchor for Cat-BL1 reconciliation; without it, discarded-run orphans cannot be mechanically identified.

2. **Idempotency check required.** Before issuing `br create`, the agent MUST check for existing child beads: `br list --label=codename:hk-<parent-id>`. If an expected child already exists (same title or equivalent), the agent MUST skip the create. `br create` has no native deduplication; the agent is the idempotency guard.

3. **Terminal transitions remain daemon-only.** Child beads start with status `open`. The daemon closes them via the normal terminal-transition adapter (BI-010). Agents MUST NOT call `br close` on child beads from inside a worktree.

4. **Merge safety.** The bead-ledger union merge-driver (BL-MRG-002) naturally preserves child-bead creates: a create is a new ID present in the worktree's JSONL but absent from main's — the driver's union algorithm takes it unconditionally (the "present in only one side beyond ancestor" branch). No additional daemon action is required at merge time for creates.

Tags: mechanism
```

---

### 3. Add new section §BL-MRG after BI-025e (~line 479+)

**New section to insert after BI-025e:**
```
### 4.x Bead-Ledger Merge Contract (BL-MRG)

> This section defines the git-level merge strategy for `.beads/issues.jsonl` in harmonik worktrees. It supersedes the lossy `git checkout --theirs .beads/issues.jsonl` workaround previously documented in HANDOFF.md.

#### BL-MRG-001 — Merge driver registration

`.gitattributes` MUST contain:
```
.beads/issues.jsonl merge=beads-union
```

`.git/config` (local to the repo) MUST configure the driver:
```
[merge "beads-union"]
    driver = harmonik beads-merge %O %A %B %P
    name = Bead Ledger Union Merge
```

The daemon MUST ensure the git config entry is present at startup (auto-configure if absent). `.gitattributes` is repo-tracked and will be present after merging the implementing PR.

Tags: mechanism

#### BL-MRG-002 — Driver algorithm (union-by-ID)

`harmonik beads-merge` MUST implement union-by-ID merge:

1. Parse `%O` (ancestor), `%A` (ours/main), `%B` (theirs/run-branch) as `map[id]row`.
2. For each ID in `union(O, A, B)`:
   - Present in only one of A or B beyond O → take it (covers child-bead-spawn creates and deletes).
   - Present in both A and B and equal → take either.
   - Present in both and differ → pick row with larger `updated_at`.
3. **Array field union.** For `labels` and `dependencies` arrays, perform set-union of both A and B values (not LWW on the whole array). Rationale: `br sync --merge` treats these fields as opaque LWW; concurrent label/dependency additions on both sides would drop one side. The driver must compensate with explicit union. Note: label removals on one side are respected if the removal moves the ID entirely off that side; simultaneous add-on-A + remove-on-B of the same label is resolved by including the label (additive-bias).
4. Write rows back ID-sorted to `%A`, exit 0.
5. On any semantic conflict per BL-MRG-003, emit a log line and continue (never exit non-zero from this driver).

Tags: mechanism

#### BL-MRG-003 — Semantic conflict logging

A semantic conflict occurs when the same bead exists in both A and B (beyond O), both differ from O, and both differ from each other — with the resolution being non-obvious (e.g., same bead closed on A, reopened on B within the same `updated_at` second). For such cases:

- Pick A (ours/main) as the winning row.
- Append a line to `.beads/merge-conflicts.log`:
  `<iso8601-timestamp> CONFLICT bead=<id> field=status a=<A_value> b=<B_value> resolution=took-ours`
- Exit 0 (never block the merge for a semantic conflict).

The reconciliation investigator reads `.beads/merge-conflicts.log` to surface audit items per Cat-BL3 (reconciliation/spec.md §8.BL3).

Tags: mechanism

#### BL-MRG-004 — Post-merge SQLite refresh

After any rebase or merge that touches `.beads/issues.jsonl`, the daemon MUST call:
```
br sync --import-only
```
in the main repo's working directory, before any subsequent `br` operations (e.g., the terminal-transition `br close` for the completed bead). This ensures the daemon's SQLite reflects the union-merged JSONL state.

If `br sync --import-only` fails, the daemon MUST emit `bead_sync_failed` to `.harmonik/events/events.jsonl` and route to Cat-BL2 (reconciliation/spec.md §8.BL2) rather than silently continuing.

Tags: mechanism

#### BL-MRG-005 — Removal of `mergeRebaseAutoResolveBeadsLedger` workaround

With BL-MRG-001 in effect, `internal/daemon/workloop.go:mergeRebaseAutoResolveBeadsLedger` MUST be removed. That function's `git checkout --theirs .beads/issues.jsonl` override suppresses the registered merge driver and reintroduces lossy behavior. The driver runs automatically during `git rebase` and `git merge` when `.gitattributes` is configured per BL-MRG-001.

Tags: mechanism

#### BL-MRG-006 — Phase 2 migration path (informative)

Phase 2 enables full shared-DB mode where worktree agents point `BD_DB` at main's `beads.db`. Phase 2 resolves the stale-at-fork problem (agent's `br show <parent>` may fail if the parent bead was created on main after the worktree was forked). Phase 2 requirements (not yet normative; tracked as a follow-up bead):

- Daemon MUST set `BD_DB=<main-repo>/.beads/beads.db` in the worktree agent subprocess environment.
- Daemon MUST set `BR_LOCK_TIMEOUT=5000` (5 s) alongside `BD_DB`. Rationale: SQLite WAL mode has `busy_timeout=0` by default — concurrent write contention causes immediate `SQLITE_BUSY` failure without a retry window. 5 s is sufficient for the sparse concurrent write pattern in practice.
- Agents MUST NOT call `br sync` with Phase 2 active (daemon owns the flush cycle; `br sync --flush-only` from an agent would write JSONL into main's working tree mid-run).
- BL-MRG-001–005 are no-ops for Phase 2 worktrees (no JSONL in worktree to merge).
- Phase 2 does not affect beads whose worktrees use Phase 1 (the driver); mixed-mode is safe since Phase 2 worktrees have no JSONL to conflict.

Tags: informative
```

---

## Changelog entry (`05-changelog.md`)

```
## beads-integration.md

- **BI-011** (amended): Renamed "No intra-run writes" → "Permitted and prohibited intra-run writes". Added permitted-write table with `claim` (existing), `child-bead-spawn` (new BI-010e), and `parent-bead-label` (new). Added explicit failure contract for prohibited terminal writes from inside worktrees.
- **BI-010e** (new): Child-bead-spawn creates. Defines constraints: lineage label, idempotency check, terminal-transition prohibition, merge-driver preservation guarantee.
- **§BL-MRG** (new section, 6 clauses): Normative bead-ledger merge contract. Defines merge-driver registration (BL-MRG-001), union-by-ID algorithm with array-field union (BL-MRG-002), semantic conflict logging (BL-MRG-003), post-merge SQLite refresh (BL-MRG-004), removal of `--theirs` workaround (BL-MRG-005), Phase 2 migration path (BL-MRG-006, informative).
```
