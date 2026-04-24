# Crash-Recovery Adversary Review — execution-model.md v0.2

**Reviewer role.** Crash-Recovery Adversary (round 2).
**Target.** `/Users/gb/github/harmonik/specs/execution-model.md` v0.2 (909 lines, 55 requirements, 3 invariants, 7 OQs).
**Lens.** Pressure the spec against OS crashes, `kill -9`, partial writes, disk-full, FS-level races, external-git interference. Find requirements whose stated guarantees do not survive physical reality.
**Date.** 2026-04-24.

## Verdict summary

The spec's checkpoint-atomicity story is *mostly* sound thanks to EM-016's choice of `git update-ref` as the atomicity boundary and EM-017a's corrupted-checkpoint fallback. But several real crash-recovery gaps remain: (a) the "atomic sequence" language in EM-016 conflates git's reference-update atomicity with filesystem-level atomicity of the tree's *object* writes (loose objects can be partially-written); (b) sub-workflow expansion state is held only in daemon memory with no mapping from the spec on how EM-031a re-derives it post-crash; (c) the JSONL tail is nominated as divergence evidence but the spec never states what readers must do when that tail is torn mid-line (a common crash signature); (d) no requirement obliges the daemon to *preserve* dirty worktree state between crash and investigator dispatch, so RC-019's WIP-capture can fire against a post-cleanup worktree; and (e) EM-024's "tip of the task branch is the last durable checkpoint" claim does not survive external `git push --force` or local `git reset` by an operator, and no requirement bounds the damage radius. Fixes are small (six concrete add-ons named below); none require reopening a locked decision.

## Scenarios tested

### Scenario 1 — Crash between `git write-tree` and `git update-ref`

**Affected requirements.** EM-016 (atomic sequence), EM-017a (corrupted-checkpoint fallback), EM-018 (sibling file at canonical path), EM-023 (one commit per durable transition), EM-INV-001 (git is the reconstruction source).

**Failure mode.** EM-016 specifies a three-step sequence — `write-tree`, `commit-tree`, `update-ref` — and names the `update-ref` as the atomicity boundary. The claim holds for *reference visibility* but NOT for git's object database.

Concretely: if the daemon is killed after `write-tree` has begun streaming loose objects into `.git/objects/xx/yy…` and before `commit-tree` returns, the object directory can contain half-written blob, tree, or commit objects. A subsequent `git fsck` reports "bad object" (corrupted zlib stream). EM-017a's fallback does not fire because EM-017a only checks for missing-or-corrupted *sibling files at resolved transition IDs*; the orphan loose objects never get a trailer walk.

Worse: EM-023 is satisfied vacuously (no commit landed, so "exactly one commit per durable transition" holds), but the run is stuck because the transition never committed *and* the handler may have produced side effects. EM-010's baseline maps builder and merge nodes to `non-idempotent` — precisely the classes where partial-commit + lost-transition is most hazardous. A rerun after restart does not know whether the partial work on the worktree represents the crashed attempt or a legitimate work-in-progress state.

**Spec coverage.** Partial. EM-016 correctly names the atomicity boundary. EM-017a handles the "commit landed but sibling absent" case. Neither covers "objects written, commit never landed, handler side-effect occurred." Reconciliation §8.11a Cat 6b catches `git fsck` failure but only as an operator-escalate; no requirement in execution-model obliges the daemon to run `git fsck` at startup or after a suspected partial write.

**Recommendation.** Add `EM-016a — Loose-object-write idempotency`: the daemon MUST either use `git hash-object -w --stdin` with an fsync before `commit-tree` (git's `core.fsyncObjectFiles=true` setting) OR use `git update-index --cacheinfo` + pack-file atomic rename.

Add an obligation at EM-031 that startup reconciliation MUST invoke `git fsck --no-dangling` (or equivalent object-database sanity check) before trusting the branch tip as the last durable state. Route failures to reconciliation Cat 6b (already defined in reconciliation §8.11a). Cross-link in §9.3 to that category.

An alternative design — batching multiple checkpoint writes into a single pack-file — would sidestep the loose-object problem entirely but requires changes to EM-016's atomic-sequence description. The loose-object + fsck path is the minimal-surface fix.

### Scenario 2 — Disk full during checkpoint commit

**Affected requirements.** EM-016 (atomic commit), EM-017a (fallback), EM-021 (evidence externalization), EM-025 (failed transitions MUST NOT commit).

**Failure mode.** Disk-full during `git write-tree` yields ENOSPC part-way through object writes. The daemon gets an error from git; the tree is partially materialized; no commit lands; no sibling file exists; the handler's work product sits in the worktree, potentially alongside a large `evidence/` directory per EM-021 that itself was only partly written.

The spec does not say what the daemon does. EM-025 addresses *handler-returned* failures, not *checkpoint-write* failures. If the daemon re-tries on freed space, it re-writes a different `transition_id` per EM-018a (new UUIDv7), which is correct — but nothing *prevents* the daemon from silently retrying indefinitely, and nothing tells it to drop the externalized-evidence orphans from the failed attempt. Over time, `.harmonik/transitions/` accumulates dead directories that no trailer references. The audit tool (EM-020a) will flag them as "sibling file not matching any trailer," routing each to reconciliation — a DoS on the reconciliation queue triggered by repeated disk-pressure events.

**Spec coverage.** Silent. ENOSPC is not in the failure taxonomy (§8 classes are `transient | structural | deterministic | canceled | budget_exhausted | compilation_loop`). EM-021 externalizes evidence but gives no GC rule.

**Recommendation.** Two adds.

1. Extend §8 failure taxonomy or §8.4 classification note: ENOSPC / EIO during checkpoint commit is classified as `transient` (retry with backoff once space freed) with a bounded retry cap, reclassifying as `structural` on cap exhaustion — mirroring §8.1 / §8.2's relationship.
2. Add `EM-021a — Orphan-evidence GC`: partially-written `.harmonik/transitions/<new_transition_id>/evidence/*` from a failed checkpoint attempt MUST be removed before any retry. A periodic sweeper MAY also reclaim these. Specify the sweeper's safety condition (only directories whose `<transition_id>` is not referenced by any trailer on any reachable commit).

### Scenario 3 — Corrupted JSONL tail used as divergence evidence

**Affected requirements.** EM-031 (state reconstruction uses git + Beads only; observational JSONL permitted for divergence evidence), EM-INV-001 (JSONL replay forbidden for state reconstruction), reconciliation §8.11a Cat 6b (JSONL unparseable past byte offset).

**Failure mode.** EM-031 says "Observational JSONL reads for divergence-evidence detection are permitted per [reconciliation.md §9.3a]." The referenced reconciliation §8.4 Cat 3 detector ("transition_event exists in JSONL but no corresponding transition-record file") *requires* parsing JSONL.

Crashes routinely tear the last line of a JSONL file (the writer was mid-write when `kill -9` hit; the file ends with a truncated JSON object and no newline). If the reader strictly parses and a truncated line at the tail triggers reconciliation Cat 6b ("JSONL unparseable past a byte offset"), the actual divergence signal is lost — every daemon crash masquerades as integrity corruption, every restart emits `operator_escalation_required`, and operators become desensitized to Cat 6b alerts. This defeats the purpose of EM-025's no-failure-commits design, which assumed crashes would be routine.

The spec does not tell readers how to handle a torn tail. The reconciliation Cat 6b detector ([reconciliation.md §8.11a]) is written as if any unparseable byte triggers the class; no tail-discrimination rule exists.

**Spec coverage.** Missing. EM-031 gestures at permitted use but does not state the parsing contract. [event-model.md §3.4] (cited in §9.3) owns fsync policy but is not yet drafted; the obligation on execution-model's side to say "readers of JSONL-for-divergence-evidence MUST tolerate a torn last record" is absent.

**Recommendation.** Add `EM-031b — JSONL tail-tolerance rule`: a consumer of JSONL for divergence-evidence purposes per EM-031 MUST treat an unparseable *final* line as "truncated tail" (discard and continue) rather than "file corrupted" (Cat 6b). An unparseable line anywhere but at the tail IS a Cat 6b signal. The boundary is "one trailing unparseable line, no trailing newline" vs "unparseable line followed by parseable lines." The distinction is mechanical and does not require cognition.

### Scenario 4 — Kill during sub-workflow expansion

**Affected requirements.** EM-034 (sub-workflow expands in place), EM-034a (node-ID namespacing), EM-035 (parent's checkpoint trail covers nested execution), EM-031a (active-run discovery).

**Failure mode.** EM-034 says "Expansion is keyed on the sub-workflow's version as resolved at workflow-load time" and that the load-time pin "survives until run terminal state." But the spec does not say *where* the pinned expansion is stored.

If expansion is only in daemon memory and the daemon is killed mid-run between the parent entering the sub-workflow node and the first nested-node checkpoint, the next startup has:

- a parent `Harmonik-Run-ID` trailer on the last checkpoint pointing at the *parent sub-workflow node*,
- no checkpoint for any expanded child node yet (EM-035 puts nested checkpoints on the parent branch, but none has landed),
- no record of *which version* of the sub-workflow was resolved.

The run is not re-startable without re-resolution, and re-resolution may produce a different expanded graph if the sub-workflow registry changed between crash and restart. EM-034 says the pin "survives" — but there is no durable pin to survive. EM-034b's acyclicity check runs at load-time, not at restart, so even validation obligations don't cover this case.

**Spec coverage.** Broken. EM-034's load-time-pin claim has no durable backing. EM-031a scans for active runs but cannot reconstruct the pinned sub-workflow version from git alone.

**Recommendation.** Add `EM-034c — Expansion pin is durable on the parent's sub-workflow entry checkpoint`: when a parent run enters a sub-workflow node, the *entry checkpoint* (required by EM-036's `sub_workflow_entered` event) MUST carry the resolved `sub_workflow_ref + version + resolved_workflow_id` in the transition record's `evidence` map under a reserved key (e.g., `evidence.sub_workflow_pin`). On restart, EM-031a reconstructs the pinned expansion by reading this key from the most-recent `sub_workflow_entered` transition record, NOT by re-consulting the registry. Registry updates between crash and restart therefore cannot alter the run's expansion. This also makes EM-034's "survives until run terminal state" claim machine-checkable.

### Scenario 5 — Beads unreachable during state reconstruction

**Affected requirements.** EM-031 (reconstruction uses git + Beads), EM-031a (active-run discovery queries Beads), EM-INV-005 (git wins on completion disagreement).

**Failure mode.** EM-031 and EM-031a both *require* Beads to classify active runs. If Beads is unreachable at startup (SQLite lock held, CLI missing, `br` hang beyond timeout — all real), the daemon cannot execute EM-031a's union ("Beads-linked runs ∪ branches whose tip carries no matching terminal-state bead"). Reconciliation §8.1 Cat 0 handles this at the reconciliation layer (halt + `degraded` status), but the execution-model spec doesn't cross-reference. A naive implementation reading only execution-model could attempt to proceed on git alone, silently classifying every bead-tied run as "no terminal-state bead found" and producing false reconciliations. This is a cross-spec integration hazard.

**Spec coverage.** Implicit. Reconciliation owns the Cat 0 halt, but execution-model's EM-031 / EM-031a read as if they assume Beads is always available. An implementer reading execution-model in isolation (a plausible scenario given the spec is designed to be cited by multiple downstream specs) could easily miss the Cat 0 gate.

There's also a subtler mid-run version: if Beads becomes unreachable *during* a run (not just at startup), the invariant EM-INV-005 ("git wins on completion disagreement") cannot be evaluated, because half of the comparison is unavailable. The spec's mid-run Beads-access pattern is left to [beads-integration.md] which has not drafted yet.

**Recommendation.** Add one sentence to EM-031a: "If Beads is unreachable at startup, active-run discovery MUST NOT proceed; the daemon MUST defer classification per the Cat 0 pre-check in [reconciliation.md §8.1] and enter `degraded` status per [process-lifecycle.md §8.2]." This is a cross-link, not a new obligation — but the absence of the cross-link is a trap for implementers. Also worth an open question on mid-run Beads unavailability (not a crash concern per se but adjacent enough to flag).

### Scenario 6 — Node that partially writes to worktree then dies (EM-017a/EM-018a/EM-023a adequacy)

**Affected requirements.** EM-017a (corrupted-checkpoint fallback), EM-018a (UUIDv7 uniqueness contract), EM-023a (durability decision procedure), EM-009/EM-010 (idempotency-class tags), reconciliation §8.3 Cat 2.

**Failure mode.** A `non-idempotent` builder node writes 80% of a multi-file edit to its worktree, then SIGSEGV's. No checkpoint commit landed (because no transition has been classified yet — the handler hasn't returned). The worktree contains dirty files; the task-branch tip is the *prior* durable checkpoint; the handler produced no `Outcome`. EM-023a's decision table sees no `transition_kind`/`outcome.status` pair to classify, so the transition is correctly "not durable." So far, the spec's invariants hold.

The gap: on restart, EM-031a's active-run discovery identifies the run as in-flight; reconciliation §8.3 Cat 2 classifies it (non-idempotent mid-flight) and dispatches an investigator. The investigator needs to decide: resume from the prior checkpoint? Reset? Reopen the bead? To decide correctly, it must see the dirty-worktree state.

Nothing in execution-model obliges the daemon to *preserve* that dirty state before the investigator runs. If the daemon's post-crash cleanup (EM-031, EM-031a, or any unrelated workspace-model operation) does a `git clean -fdx` or checks out the branch tip to "reset" the worktree, the evidence of partial work is gone. RC-019 obliges the investigator to capture WIP before a `reopen-bead` verdict — but that capture fires *after* the investigator has been dispatched against whatever worktree state survived. A pre-dispatch cleanup defeats the capture entirely.

**Spec coverage.** Absent from execution-model (reconciliation RC-019 requires the *investigator* to capture WIP before `reopen-bead`, but only *during* the investigation, not *before* the dispatch that spawns the investigator). If anything touches the worktree between crash and investigator launch, RC-019's capture fires too late.

**Recommendation.** Add one sentence to EM-031a: "Before dispatching any reconciliation workflow for an in-flight run, the daemon MUST NOT modify the run's worktree (no `git clean`, no `git checkout`, no branch switch). Worktree state at crash time is an investigator input." Cross-reference [workspace-model.md §5.9] to make the workspace-model side enforce the same read-only-until-investigator-ran rule.

### Scenario 7 — Branch force-pushed or reset by external git operation

**Affected requirements.** EM-024 (git always knows the last durable state of every in-flight run), EM-020 (transition records are immutable; history rewriting forbidden), EM-020a (audit tool detects integrity violations), EM-INV-001.

**Failure mode.** EM-020 forbids history rewriting *as a policy*. But nothing in the spec *prevents* an external actor from rewriting the task branch. Real sources of such rewrites:

- an operator runs `git push --force` after a manual fix;
- a pre-commit hook installed project-wide runs `git reset --soft HEAD~1`;
- a developer uses the wrong worktree and does `git reset --hard HEAD~5`;
- a CI system's auto-rebase rewrites the branch during a merge attempt.

EM-024 claims the task-branch tip IS the last durable checkpoint — but after an external force-push, the tip might be an unrelated commit or an older one from the run's own history. The run's state becomes silently incorrect until EM-020a's audit tool happens to run. There is no detector that fires *on branch-tip movement detected at startup or mid-run*; EM-020a is "post-hoc audit" and the spec gives no cadence.

If the force-push replaces the tip with an older run-owned commit, EM-031a's active-run discovery still finds the run, but state reconstruction lands on a stale checkpoint. Downstream handlers act on outdated state. The damage is silent and potentially irreversible.

**Spec coverage.** Weak. EM-020a exists but is a passive auditor; no active detection. Reconciliation §8.4 Cat 3 could absorb this, but the detection path ("branch tip differs from last-known-daemon-tip") is not stated anywhere.

**Recommendation.** Add `EM-024a — Branch-tip-advance monotonicity check`: on any branch-tip read that *follows* a prior daemon-observed tip for the same run, the daemon MUST verify that the new tip is a fast-forward of the prior tip (the prior tip is in the ancestor chain of the new tip). If not, the discrepancy MUST route to reconciliation Cat 3 (or a new Cat 3d "branch rewound externally"). The daemon SHOULD persist the last-observed tip per run in a small on-disk map under `.harmonik/run-tips/<run_id>` so the check survives daemon restart.

This is the storage cost of making EM-024's "tip IS the last durable state" claim defensible against external tampering. The storage is small (a SHA per run) and easy to reconstruct on first run-observation if lost. An alternative design — using git reflogs, which record the history of ref moves — has attractive properties (no new on-disk format) but is fragile under `git gc` and across shared repository hosts that don't preserve reflogs. The dedicated per-run tip map is simpler and more defensible.

## Requirements that held up well

- **EM-018a (transition_id UUIDv7 uniqueness).** The r1 critic's cross-run-collision finding has been addressed cleanly; cross-run merges and cherry-picks no longer collide, and the sibling-file path stays flat. No crash pressure breaks this. The decision to use UUIDv7 (time-ordered) instead of bare v4 also helps audit tools (EM-020a) scan in commit order.
- **EM-020 + EM-020a (immutability + audit).** The append-only discipline is crash-safe: an interrupted amend cannot corrupt prior records because amend is forbidden. Audit rules (a)–(d) cover the straightforward integrity-violation shapes: missing sibling, orphan sibling, duplicate transition_id across commits, schema-version disagreement. A crash during audit itself is benign (audit is a read-only scan).
- **EM-023a (durability decision procedure).** The mechanical `transition_kind × outcome_status` table means a crash-interrupted classification is deterministically re-derivable from the transition record; no state leaks into the decision, and the `PARTIAL_SUCCESS → partial_success=true` evidence flag preserves enough context to resume or reclassify correctly post-crash.
- **EM-025 (no failure commits at MVH).** Counter-intuitively, this *helps* crash safety: the spec has fewer commit-emission paths, each with an atomic `update-ref` boundary. A crash during a failed transition simply leaves the prior checkpoint as the tip; the failure event (per §8) is the only recovery signal needed, and JSONL loss of that event is survivable because reconciliation can reclassify from git + Beads state.
- **EM-033 (no workflow-level transactionality).** The explicit forbidding of prior-checkpoint rollback on later failure is crash-robust: prior commits remain durable regardless of crash timing in later nodes. Attempts to add transactionality would have created new crash-recovery windows (half-undone multi-commit rollbacks).
- **EM-INV-001 (git is the state-reconstruction source).** Holds under crash, provided the object-database sanity check from Scenario 1 is added. The invariant's framing is correct: JSONL-replay-for-state-reconstruction would have been catastrophic under torn-tail crashes.
- **EM-046 (context-restore is agent-scoped).** The `rollback_to_state_id = None` rule for context-restore prevents a class of crash-during-rollback ambiguity — the graph position cannot be mis-restored because it isn't being changed. A crash during context-restore resumes naturally from the source state.
- **EM-026 (reconciliation workflows emit verdict-only commit).** A crash during reconciliation leaves no mid-investigation durable state (by design), so post-crash re-classification sees the *outer* run in its original category — the bounded-recursion property from RC-INV-002 depends on this and holds.

## Hidden assumptions about crash safety

The spec quietly assumes all of the following; each is a latent hazard worth surfacing before finalize.

1. **Git's object-database writes are atomic.** They are not. Loose objects are written by `write → rename`, which *is* atomic per-object, but a multi-object write (tree + blobs + commit) is a sequence of independent renames. A crash in the middle yields a set of fully-written loose objects with an unfinished commit. Scenario 1 above. The specific failure under power-loss (not just SIGKILL) is worse: the ext4/APFS page cache may not have flushed the inode metadata when power is cut, so a "successfully renamed" loose object can vanish entirely on the next boot.
2. **The daemon has exclusive access to the project's git repository.** External git operations, operator scripts, and pre-commit hooks can mutate refs under the daemon's feet. Scenario 7 above. No requirement names "daemon has a lock on the repo" (and such a lock would be hostile to operators; the right answer is detection, not exclusion). The task branch naming convention in [workspace-model.md §5.8] — not yet drafted — may help by making daemon-owned branches visually distinguishable, but it does not prevent force-pushes.
3. **Beads is available whenever state reconstruction runs.** EM-031 reads as if Beads is always up; Cat 0 is a reconciliation concern, not visible from the execution-model reading path. Scenario 5. The one-sentence cross-link in the recommendation closes the trap.
4. **Sub-workflow resolution is idempotent across daemon restart.** EM-034's "load-time pin survives" claim assumes the registry state at restart matches the state at original load. No storage backs this. Scenario 4. The fix (pin in the entry-checkpoint transition record's evidence map) is minimal and uses existing infrastructure (EM-021 evidence externalization) so it costs nothing at the schema layer.
5. **JSONL readers parse strictly.** With `kill -9` during a write, the last line is torn. Without a tail-tolerance rule (Scenario 3), every daemon crash produces a Cat 6b false-alarm, pushing operators to intervene on what should be ordinary restart. This also defeats the purpose of EM-025's "no failure commits" decision, because each crash becomes an operator event.
6. **`fsync` happens somewhere.** The spec defers fsync to [event-model.md §3.4] for events and implicitly trusts git's own durability guarantees for commits. Git's default is `core.fsyncObjectFiles=false` on most platforms — a power-loss crash after `git update-ref` returned successfully can lose the commit's loose objects. A concrete obligation on the daemon to set `core.fsyncObjectFiles=true` (or use a batched `fsync` before emitting `checkpoint_written`) would close this. Worth tracking as an open question or cross-referencing to event-model §3.4 once that drafts.
7. **Evidence externalization under `.harmonik/transitions/<id>/evidence/*` inherits the commit's atomicity.** It does — because the evidence files are part of the tree — but EM-021 does not restate this, so a reader might assume they can be written outside the tree for speed. Worth a one-line clarification to prevent an implementer from "optimizing" by writing evidence to a sibling directory not in the tree.
8. **The reference-advance "atomicity boundary" covers the sibling file and trailers.** It does, because both are in the tree that `update-ref` points at — but EM-016's current phrasing ("atomicity boundary is the reference advance") invites a misreading that sibling-file write is a separate step. Reword to "the sibling file, work product, and trailers are all part of the tree written atomically by the reference advance."
9. **Worktree state is preserved between crash and reconciliation dispatch.** Scenario 6 above. Nothing in execution-model forbids a cleanup step that would destroy the investigator's evidence. Workspace-model will need to enforce the same read-only-until-investigator-ran discipline.
10. **Trailer parsing succeeds on commits with unusual footers.** EM-017 mandates trailers; EM-019 retrieval depends on them. `git interpret-trailers` has defined-but-subtle handling of commits whose body itself looks trailer-like (e.g., a commit with `Signed-off-by:` in its body that precedes the Harmonik trailers). Post-hoc audit (EM-020a) should probably use the trailer-block-only parse mode explicitly.

## Cross-spec implications

Three of the seven gaps (Scenarios 3, 5, 6) are fixable with a one-sentence clarification in execution-model plus a confirming obligation in the co-owning spec:

- Scenario 3 (JSONL torn tail): execution-model adds EM-031b; reconciliation §8.11a Cat 6b detector updates its detection rule to exclude the torn-tail case.
- Scenario 5 (Beads unreachable): execution-model cross-links EM-031a to reconciliation §8.1 Cat 0; no new reconciliation requirement needed.
- Scenario 6 (worktree preservation): execution-model adds one sentence to EM-031a; workspace-model §5.9 confirms the worktree-touch ban when it drafts.

The remaining four (Scenarios 1, 2, 4, 7) are execution-model-local fixes. EM-016a, EM-021a, EM-024a, and EM-034c sit naturally in §4.4, §4.4, §4.5, and §4.8 respectively. None crosses a subsystem boundary in a way that requires renegotiation of another spec's contract.

## Proposed amendments (consolidated)

| New ID | Section | Shape | Fixes scenario |
|---|---|---|---|
| `EM-016a` | §4.4 | Loose-object-write idempotency (fsync + startup fsck) | Scenario 1 |
| `EM-021a` | §4.4 | Orphan-evidence GC rule | Scenario 2 |
| §8 note | §8.1 | ENOSPC/EIO classified as `transient` with retry cap | Scenario 2 |
| `EM-024a` | §4.5 | Branch-tip-advance monotonicity check | Scenario 7 |
| `EM-031b` | §4.7 | JSONL tail-tolerance rule | Scenario 3 |
| `EM-034c` | §4.8 | Sub-workflow expansion pin durable on entry checkpoint | Scenario 4 |
| EM-031a sentence | §4.7 | Beads-unreachable cross-link + worktree-preservation rule | Scenarios 5, 6 |

Every addition is mechanism-tagged. None requires an `Axes:` line deviation beyond what EM-016/EM-018 already carry. Schema-version impact: zero (EM-034c uses the existing `evidence` map with a new reserved key; readers ignore unknown keys per §6.4).

---

**Reviewer note.** Seven concrete add-ons (EM-016a, EM-021a, EM-024a, EM-031b, EM-034c, one §8 classification note, and sentences in EM-031a) close the surfaced gaps. None reopens a locked decision; all fit within the current §4 grouping. Recommend round-3 integration before `status: reviewed`.

**Strongest single finding.** Scenario 4 (sub-workflow expansion pin is daemon-memory-only). The spec *asserts* the pin "survives until run terminal state" (EM-034) but provides no durable mechanism for it to survive; any daemon crash between sub-workflow entry and first nested checkpoint produces a non-restartable run. This is both (a) the easiest of the seven gaps to overlook at review, because the failure mode is silent rather than alarming, and (b) the most consequential for the workflow composition model, which [execution-model.md §4.8.EM-037] declares is harmonik's *only* composition mechanism.
