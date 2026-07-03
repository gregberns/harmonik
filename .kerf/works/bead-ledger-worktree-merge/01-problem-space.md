# bead-ledger-worktree-merge — Problem Space

**Status:** problem-space  •  **Jig:** spec  •  **Started:** 2026-05-21

## Summary

Harmonik worktrees inherit `.beads/issues.jsonl` (the bead ledger) as a tracked file. When agents inside a worktree run `br create`/`br update`, their local copy diverges from main. Concurrent runs landing on main make the JSONL non-FF on rebase. Today the daemon falls back to `git checkout --theirs .beads/issues.jsonl`, which silently discards every agent-side bead write.

This is currently tolerable because the daemon owns terminal transitions (BI-010) and re-asserts `close` post-merge. **It becomes intolerable the moment harmonik drives multi-bead workflows where one bead spawns child beads** (research bead → 5 task beads, agent-driven decomposition, etc.). Child-bead creates would vanish.

A parallel concern: when harmonik runs against an **integration branch** instead of main, the main-branch harmonik instance can't see beads created on the integration branch's JSONL — coordination across branches is undefined.

## Goals

- Worktree-created child beads MUST survive merge-to-main with their full state intact.
- Worktree bead writes MUST NOT silently disappear; conflicts MUST surface explicitly or be unioned correctly.
- Integration-branch dispatch MUST coordinate bead state with main-branch dispatch.
- Stale-at-fork problem (agent's `br show <id>` fails because their local DB was forked before the bead landed on main) SHOULD be fixed in the same architecture.

## Non-goals (this work)

- Replacing `br` (Dicklesworthstone/beads_rust) with a different ledger.
- Building a network-server mode for `br` (upstream explicitly rejected it).
- Multi-machine coordination of bead state (single-machine-multiple-worktrees is the scope).

## Constraints

- **`br` upstream behavior is fixed.** Cannot fork; can only configure via flags/env (`BD_DB`, `--db`) or wrap with our own tooling.
- **JSONL format:** ID-sorted, full-row-per-issue (NOT append-only). `br update`/`br close` rewrite rows in place. Each row has `updated_at` (natural last-writer-wins tiebreaker).
- **BI-010 / BI-011 / BI-025e** in `specs/beads-integration.md` already constrain the write surface. Any solution must either fit these or amend them with rationale.
- **Git as completion authority** (project-wide 4-store model): bead state participating in the same DAG as code is what makes merge-to-main an atomic close. We probably want to keep that property.
- **No server-mode daemon for `br`.** BI-003 rejected it.

## Success criteria

After this work:

1. Spec defines the worktree-bead-write story: who can write what to which ledger from which process, with idempotency rules and conflict resolution.
2. The `git checkout --theirs .beads/issues.jsonl` workaround is replaced by an explicit, correct merge strategy (and removed from `HANDOFF.md`).
3. A research bead in a worktree can create N child beads, those creates land on main intact, and a sibling harmonik instance immediately sees them.
4. Integration-branch dispatch (when used) has a documented bead-coordination story.
5. Reconciliation rules (specs/reconciliation.md) cover the new failure modes introduced by whichever solution is chosen.

## Spec areas likely affected

- `specs/beads-integration.md` (BI-010, BI-011, BI-025e — write surface, intra-run writes, concurrency).
- `specs/reconciliation.md` (new failure-mode categories).
- `internal/daemon/workloop.go` `mergeRunBranchToMain` (~workloop.go:1891-2036).
- `internal/workspace/agenttask_chb028.go:327` (worktree-spawn site).
- `.gitattributes` / `.gitignore` (if merge-driver or gitignore-JSONL approach wins).
- Possible new spec section for "agent-spawned child beads" (BI-010e if shared-DB approach wins).

---

## Research Pass — preliminary inputs (saved 2026-05-21)

Two parallel research sub-agents were dispatched to investigate solution candidates. Their reports are embedded here as research inputs for the decompose / research / design passes.

### Report A — Counter-proposal: keep JSONL, ship a custom git merge-driver

**Summary recommendation:** Ship a ~40-line custom git merge-driver. Union-by-bead-ID, last-writer-wins by `updated_at`. One bead, one PR, no kerf overhead.

**Findings:**

- JSONL is **NOT append-only.** `br update`/`br close` rewrite rows in place. JSONL is SQLite→file export sorted by ID, one full JSON object per issue. No duplicate IDs. Verified empirically against `.beads/issues.jsonl` (1541 rows, ID-sorted, no dupes).
- IDs globally unique (per `beads_rust` README).
- `br sync --flush-only` exports SQLite → JSONL idempotently; `--import-only` does the reverse; `--merge` does 3-way using `.beads/beads.base.jsonl` as ancestor with `--force`/`--force-newer-timestamp` tiebreakers.
- **No git merge driver is shipped upstream.** beads_rust README punts to manual line-editing + `--import-only`. The driver is ours to write.

**Algorithm (well under 50 lines):**

Register in `.gitattributes`: `.beads/issues.jsonl merge=beads-union`.  
Configure in `.git/config`: `merge.beads-union.driver = harmonik beads-merge %O %A %B %P`.

1. Parse `%O` (ancestor), `%A` (ours/main), `%B` (theirs/run-branch) as `map[id]row`.
2. For each ID in `union(O, A, B)`:
   - Present in only one of A/B beyond O → take it (covers child-bead-spawn case).
   - Present in both A and B and equal → take either.
   - Present in both and differ → pick row with larger `updated_at`. Optional second pass: union `labels` and `dependencies` (monotonic-additive in practice).
3. Write rows back ID-sorted to `%A`, exit 0.
4. Daemon runs `br sync --import-only` post-merge to refresh SQLite from merged JSONL.

**Cases not covered:**

(a) True semantic conflict on same field (A reopens, B closes same bead within seconds) — last-writer-wins picks one, other intent lost. Mitigation: agents almost never mutate beads they don't own; add `.beads/merge-conflicts.log` for operator audit.  
(b) Bead deletion — driver would resurrect deleted beads from the other side. Treat as out-of-band.  
(c) Schema migrations changing row shape — fall back to last-writer-wins on whole row.  
(d) Dependency-graph cycles introduced by union — accept and let `br dep cycles` flag post-merge.

**Conclusion:** Ship the merge-driver. The conflict class (child-bead-spawn) is the dominant case and is perfectly union-mergeable. Residual semantic-conflict tail is rare (daemon-owned lifecycle, single writer per bead in practice).

**Key citations:**
- `internal/daemon/workloop.go:1891-2036` — `mergeRunBranchToMain` (rebase fails on JSONL at line 1944).
- `HANDOFF.md:55` — current lossy `--theirs` workaround.
- `.beads/issues.jsonl` — confirmed ID-sorted, full-row-per-issue, 1541 rows.

---

### Report B — Architecture: shared DB via `BD_DB`

**Summary recommendation:** Point worktree agents at main's SQLite directly via `BD_DB=<main-repo>/.beads/beads.db` env var. Gitignore JSONL in worktrees. Solves merge-conflict AND stale-at-fork AND child-bead-spawn in one stroke.

**Candidate analysis:**

**(a) Gitignore JSONL in worktrees + out-of-band reconciliation.** Daemon snapshots `.beads/issues.jsonl` at worktree-create, post-merge diffs the worktree copy against the snapshot, parses new/modified bead lines, replays them against main's SQLite via `br create`/`br update`. Pros: agents stop fighting git over JSONL; daemon owns all main-side bead writes (consistent with BI-010). Cons: violates BI-011 ("no intra-run writes") in spirit; needs new write-op category and idempotency for `create`. Requires per-worktree `.git/info/exclude` injection (changing root `.gitignore` would un-track the file globally).

**(b) `br` shared DB via `BD_DB`.** Upstream rejects a network daemon (BI-003) but `br` supports `--db <path>` / `$BD_DB` / `$BD_DATABASE`. Worktree agents can point `br` at the **main repo's** `.beads/beads.db` directly. SQLite WAL serializes writes; BI-025e already affirms concurrent `br` invocations across processes are permitted. **Cleanest fix — no JSONL in the worktree at all.** Cons: agent `br sync` would write JSONL into main repo working tree (daemon doesn't expect this); OQ-BI-012 (multi-daemon-same-Beads) becomes relevant if N>1 harmonik instances.

**(c) Custom git merge driver for `.beads/issues.jsonl`.** Same algorithm as Report A. Cons: must stay byte-exact with `br`'s JSONL schema (forwards-compat risk); doesn't help stale-at-fork (agent's worktree `.db` is stale for beads created on main mid-run). Upstream advocates `br sync --merge` instead.

**(d) Tag-and-extract before --theirs.** Daemon parses worktree JSONL pre-checkout, extracts new bead-IDs, applies via `br create` against main, then `--theirs` resolves. Like (a) minus gitignore. Same BI-011 caveat; fragile because updates (not creates) are also lost.

**(e) `br sync --merge` discipline.** Upstream-supported. Maintain `.beads/beads.base.jsonl` at worktree-create as ancestor; both sides call `br sync --merge --force` post-merge. Harmonik isn't using this surface today (no `beads.base.jsonl` in `.beads/`). The "follow `br`'s design" answer.

**Report B recommendation:** Candidate (b) primary, (e) as fallback. At worktree-create, daemon exports `BD_DB=<main>/.beads/beads.db` to agent environment AND gitignores `.beads/issues.jsonl` inside the worktree via `.git/info/exclude`. All agent `br` writes land in main's SQLite, serialized by WAL per BI-025e. Daemon owns JSONL re-export on main post-merge (single-writer flush). **Only candidate that also solves stale-at-fork.**

**Integration-branch dimension:** Candidate (b) gets SIMPLER, not harder, when target is an integration branch — agent `br` calls hit main-repo's `.beads/beads.db` regardless of which branch the worktree is on; child-bead creates immediately visible to sibling harmonik instance. Candidates (c)/(d) degrade with N-way merges.

**Open questions for design pass:**

1. Does `BD_DB` set to main-repo's DB cause br to write JSONL into *main's* working tree mid-run?
2. How does the daemon prevent agents from issuing terminal `br close` against main DB directly, bypassing the adapter? (Skill discipline only, or process guardrail?)
3. Should child-bead creates be new BI-010e write category with own idempotency key, or piggyback on `claim`'s key?
4. If a worktree's run fails and is discarded, do its child-bead creates persist in main DB as orphans? What sweeps them? (Cat-N reconciliation rule.)
5. Does dogfooded concurrent-claude (`--max-concurrent N>1`) need OQ-BI-012 multi-writer story resolved first?
6. Adopt `beads.base.jsonl` discipline as belt-and-suspenders fallback?

**Key citations:**
- `specs/beads-integration.md:190` (BI-010), `:257` (BI-011), `:479` (BI-025e), `:96` (BI-003).
- `internal/workspace/agenttask_chb028.go:327` — worktree-spawn site.
- `.gitignore:13-14` — DB gitignored, JSONL tracked.

---

## Divergence between reports

The two reports recommend **architecturally different** solutions:

| Dimension | Report A (merge-driver) | Report B (shared DB) |
|---|---|---|
| Localized | Yes (~40 LOC + .gitattributes) | No (spec amendment + worktree env injection + per-worktree gitignore) |
| Solves merge-conflict | Yes | Yes |
| Solves stale-at-fork | No | Yes |
| Solves child-bead-spawn | Yes (via union) | Yes (natively — single DB) |
| Solves integration-branch coordination | Partial (N-way merge degrades) | Yes (single DB regardless of branch) |
| Upstream alignment | Against advice | Within supported surface |
| Spec changes needed | None | BI-010e (new), BI-011 amendment, possibly BI-025e |
| Failure tail | Semantic conflicts on same field | Multi-writer concerns at N>1 |

## Path forward proposed (orchestrator note, 2026-05-21)

**Ship the merge-driver as a quick-win bead.** It closes the silent-data-loss bug TODAY and is compatible with whichever architecture wins the kerf design pass. Filed as a separate quick-win bead, pinned to this kerf work via `codename:bead-ledger-worktree-merge` label.

**Then complete this kerf work** through the standard passes (decompose → research → design → spec → tasks) to pick between merge-driver-only-forever vs migrating to shared-DB. Design pass should specifically answer: does child-bead-spawn introduce write patterns the merge-driver can't handle correctly?

## Next pass

`kerf status bead-ledger-worktree-merge decompose` — break this into the spec areas to revise and the open questions to answer.
