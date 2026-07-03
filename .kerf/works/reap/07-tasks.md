# reap — Implementation Tasks

Breaks `SPEC.md` (assembled from `05-specs/C1..C6` + `06-integration.md`) into ordered, dependency-aware implementation tasks. **Every task maps to an already-created `codename:reap` bead** (reconciled against `br list --label codename:reap`, 2026-05-31). The four implementation beads are coarse (one bead spans multiple components), so several tasks share a bead — that is expected. Tasks with NO owning bead are flagged explicitly as **GAP** at the end (no beads are created in this pass, per the Tasks-pass instruction).

## Bead inventory (reconciled 2026-05-31)

| Bead | Type/Pri | Scope it owns | Components |
|---|---|---|---|
| **hk-9eury** | bug P1 | Make the boot sweep remediating + reconcile dispatched + reap worktrees | C1, C2, C3 (+ shared step-B wiring) |
| **hk-xb5yi** | bug P1 | Concurrent-spawn cap + reap-on-exit | C4 |
| **hk-li14r** | feature P2 | Single-flywheel-per-project lock | C5 |
| **hk-7t9g1** | feature P2 | Daemon restart backoff | C6 |
| **hk-a31od** | task P1 (`scenario-test`) | Boot-path scenario test | gates C1/C2/C3/C6 (blocks all 4 impl beads) |
| **hk-izs8s** | task P1 (`exploratory-test`) | Operator-CLI exploratory test | gates C4/C5/C6 (blocks all 4 impl beads) |

Existing bead dependency wiring (verified): hk-a31od and hk-izs8s each BLOCK all four implementation beads — the test beads must be satisfied before an implementation bead closes (the test-gate per the jig's Validation/Acceptance Tests motivation: hk-37zy8 / hk-aievp / hk-ry3be). This is the correct direction; no rewiring needed.

---

## Tasks

Tasks are grouped by the `06-integration.md` build steps A–G. Task IDs are local (`T1`..`Tn`) for the DAG; each names its owning bead.

### Step A — durable markers (foundation)

**T1 — Run-ledger marker + claim-time append (C2 ledger surface).** → bead **hk-9eury**
- **Build:** new `internal/lifecycle/runledger.go` — append-only `.harmonik/run-ledger.jsonl` reader/writer behind a `RunLedger` seam; row `{bead_id, run_id, claimed_at, daemon_generation, project_hash}`; prune-on-terminal-state; generation tagging. Wire the claim-time `fsync`'d append at `internal/daemon/runregistry.go:94` (`Register`), BEFORE the `br` claim.
- **Spec ref:** SPEC §4 C2 PL-006f (ledger surface, write ordering, lifecycle); `05-specs/C2`.
- **Deliverables:** `runledger.go`, `runregistry.go` edit, `runledger_test.go`.
- **Acceptance:** C2-AC4 (crash between append and claim → harmless row, pruned); ledger append precedes claim; fsync'd.
- **Deps:** none (foundation).

**T2 — Boot-log marker + `daemon_generation` counter (C6 boots.jsonl).** → bead **hk-7t9g1**
- **Build:** new `internal/lifecycle/bootlog.go` — append-only `.harmonik/boots.jsonl` reader/writer/prune behind a `BootLog` seam (injectable clock); row `{boot_id, started_at, daemon_generation, exit_disposition}`; the monotonic `daemon_generation` counter (max prior + 1) OWNED here. Append `unknown` early in `daemon.Start`; the PL-011 drain updates to `clean`.
- **Spec ref:** SPEC §4 C6 PL-005c (boots log) + §2.5 (generation ownership); `05-specs/C6`.
- **Deliverables:** `bootlog.go`, `daemon.go` start-row append + PL-011 drain update, `bootlog_test.go`.
- **Acceptance:** C6-AC4 (SIGKILL leaves `unknown`, counted abnormal); generation monotonic; clean-disposition write on drain.
- **Deps:** none (foundation). **Note:** T1's ledger row reads this generation counter — if T2 is not yet merged, T1 uses a daemon-start-ns surrogate (SPEC §2.5); the DAG keeps T2 as a soft predecessor of T1's generation field (advisory A1 from integration-review).

### Step B — shared survive-check + payload plumbing

**T3 — Pre-sweep `DiscoverActiveRuns` call + payload field plumbing (the C1⟷C3 shared survive-check).** → bead **hk-9eury**
- **Build:** in `internal/daemon/daemon.go` (~705-741, the existing pre-sweep block), add ONE `lifecycle.DiscoverActiveRuns` call at PL-005 step 3 alongside the existing raw `queue.Load`; thread the resulting `ActiveRunSet` into BOTH `RunOrphanSweep` (for C1/T5) and `LoadQueueAtStartup` (for C3/T6). Add the additive integer fields `WorktreeDirsRemoved` (C1) + `dispatched_items_reconciled` split (C3) to `core/DaemonOrphanSweepCompletedPayload` and the boot-reconcile summary; map them in `ToPayload()`. Handle `ErrBeadsUnavailable` → degrade to git-branch-tips for both consumers.
- **Spec ref:** SPEC §3.1 (one pre-sweep set) + §3.2 (one payload); `06-integration.md` §2.1, §2.2; `05-specs/C1` Approach survive-check note, `05-specs/C3` Decision 1.
- **Deliverables:** `daemon.go` edit, `orphansweep.go` payload-field add, `core` payload struct edit, wiring test.
- **Acceptance:** the single `DiscoverActiveRuns` result is observably the same set consumed by C1 and C3 (one call, two consumers); `ErrBeadsUnavailable` degradation exercised.
- **Deps:** none structurally, but precedes T5 and T6 (they consume its outputs).

### Step C — boot sweep remediation (C1 + C2)

**T4 — Worktree-dir reaper seam + production impl (C1).** → bead **hk-9eury**
- **Build:** new `internal/lifecycle/worktreereap.go` (or `internal/workspace/orphansweep.go` extension) — `WorktreeReaper` seam + production impl: `git worktree remove --force --force` → `os.RemoveAll` fallback → `git worktree prune`; the `realpath`-prefix path-gate; the lease-lock + pre-sweep-`DiscoverActiveRuns` liveness check; the sidecar-preservation check via `internal/workspace/crashevidence.go` (WM-003a). Invoke after `SweepStaleLeaseLocks` in `RunOrphanSweep`.
- **Spec ref:** SPEC §4 C1 PL-006e; `05-specs/C1`; event-model §8.7.14 `worktree_dirs_removed`.
- **Deliverables:** `worktreereap.go`, `orphansweep.go` call-site edit, `crashevidence.go` consumption, spec edits (process-lifecycle PL-006e + event-model §8.7.14), `worktreereap_test.go`.
- **Acceptance:** C1-AC1..AC7 (remove K stale, preserve live/in-set/sidecar, path-gate rejects outside, non-fatal errors, idempotent second boot).
- **Deps:** T3 (consumes the pre-sweep active-run set + payload field).

**T5 — Widen the in_progress-reset ownership gate with the run-ledger (C2).** → bead **hk-9eury**
- **Build:** in `internal/lifecycle/orphansweepbeads.go`, add a `LedgerProvenanceSet` owner-set input read from T1's ledger; widen the ownership gate (~line 90-130) to include it; keep the exclusion ordering (a/b/c) UNCHANGED. Wire the `RunLedger` reader into `RunOrphanSweep` at `daemon.go`; prune ledger rows on the Cat-3c close path.
- **Spec ref:** SPEC §4 C2 PL-006f (consumption + lifecycle); `05-specs/C2`; process-lifecycle PL-006 sixth-bullet + PL-004 inventory edits.
- **Deliverables:** `orphansweepbeads.go` edit, `daemon.go` wiring + prune, spec edits, `orphansweepbeads_test.go` additions.
- **Acceptance:** C2-AC1 (reproduce-first: ledger-present→reset, ledger-absent→skip, one table-driven test); C2-AC2 (cross-project ledger → not reset); C2-AC3 (exclusions still gate); C2-AC5 (post-close prune); C2-AC6 (no new payload field).
- **Deps:** T1 (ledger), T3 (sweep wiring + the pre-sweep sets exclusion (a) reads).

### Step D — queue-load remediation (C3)

**T6 — Dead-run dispatched-item reconcile pass (C3).** → bead **hk-9eury**
- **Build:** in `internal/lifecycle/startup_pl005_qm002.go`, add `reconcileDeadRunDispatchedItems` after `reconcileDispatchedItems` (~line 169); classify NON-surviving items by the C3 evidence table (merge→completed / `Refs:`-less-worktree→failed / no-artifact→pending) reusing `MergeCommitScanner` + `internal/workspace/crashevidence.go`; persist-before-emit; emit `queue_item_reconciled` with the new reasons; record the `dispatched_items_reconciled` summary split. Thread the T3 pre-sweep `ActiveRunSet` + worktree/lease discovery into `LoadQueueAtStartup`.
- **Spec ref:** SPEC §4 C3 QM-002c; `05-specs/C3`; execution-model EM-031a cross-ref + event-model §8.10.7/§6.3 reason-enum extension (class F unchanged).
- **Deliverables:** `startup_pl005_qm002.go` edit, `daemon.go` call-site inputs, `internal/queue/...` summary, spec edits (queue-model QM-002c, execution-model, event-model), tests.
- **Acceptance:** C3-AC1 (dead-run→pending), AC2 (terminal→failed), AC2b (merged→completed, not requeued), AC3 (live-run in pre-sweep set→unchanged, no event), AC4 (idempotent second boot), AC5 (summary split), AC6 (non-interference with C2 exclusion (a)).
- **Deps:** T3 (shared survive-check set), T4 (C1 sidecar-preservation keeps `dead_run_failed` evidence alive — `06-integration.md` §5.1 ordering), T5 (C2 exclusion (a) non-interference — `06-integration.md` §5.2).

### Step E — single-flywheel lock + ordered exit-code registry (C5)

**T7 — Exit-code registry ordered obligation: absorb 25 → `CommandSupervise` → add 24 (+ reserve 26).** → bead **hk-li14r** (the 24/25/`CommandSupervise` parts); the 26 add is bead **hk-7t9g1** (see T9).
- **Build:** in `internal/operatornfr/{exitcode.go,commandcodes.go}` and `specs/operator-nfr.md` §8, perform IN ORDER: (1) absorb code 25 `supervisor-already-running` into §8 + the `ExitCodes` registry + ON-001/ON-003 catalog; (2) add a `CommandSupervise` `CommandName` + `CommandExitCodeSet` (codes 0/17/24/25); (3) add code 24 `flywheel-already-running` to §8 + `CommandSupervise` + `CommandDaemon`.
- **Spec ref:** SPEC §3.4 + §4 C5 "Ordered §8 obligation"; `06-integration.md` §2.3; `05-specs/C5` "Ordered integration obligation".
- **Deliverables:** `exitcode.go`, `commandcodes.go`, `operator-nfr.md` §8 edits, `exitcode_test.go` / `VerifyCommandExitCodeSets` test.
- **Acceptance:** C5-AC6 (`VerifyCommandExitCodeSets` passes with 24 AND 25 resolvable, registry contiguous, no hole at 25).
- **Deps:** none structurally; MUST precede T8 (the lock's exit-24 refusal) and T9's code-26 add (which appends after a contiguous 0–25).

**T8 — Flywheel lock acquire + refusal + stale reclaim (C5).** → bead **hk-li14r**
- **Build:** new `internal/lifecycle/flywheellock.go` (modeled on `pidfilelock.go`) — `ProbeFlywheelLock` `flock(LOCK_EX|LOCK_NB)` + marker `{pid, layer, started_at, project_hash}` + PL-002a/PL-024 stale-reclaim. Acquire at PL-005 **step 1.5** in `daemon.go` (when `--flywheel`) and BEFORE the supervisor lock + state writes in `cmd/harmonik/supervise/start.go`. Refuse held-by-live with exit 24 + a stderr diagnostic naming the owner.
- **Spec ref:** SPEC §4 C5 PL-002c; `05-specs/C5`; process-lifecycle PL-002c + PL-019(c) amend + PL-004 inventory.
- **Deliverables:** `flywheellock.go`, `daemon.go` + `start.go` acquire, spec edits, `flywheellock_test.go`.
- **Acceptance:** C5-AC1/AC1b (cross-layer refusal exit 24 + diagnostic), AC2 (stale reclaim), AC3 (first owner byte-identical), AC4 (flock auto-release, no `F_SETLK`), AC5 (plain non-flywheel daemon unaffected).
- **Deps:** T7 (exit-24 resolvable). Acquire at step 1.5 — precedes the T9 step-1.6 gate.

### Step F — boot-backoff gate (C6)

**T9 — Boot-backoff gate + code 26 (C6).** → bead **hk-7t9g1**
- **Build:** new `internal/lifecycle/bootbackoff.go` (reusing the `supervisor.go` `backoffWithJitter`) — compute `k` from T2's boots.jsonl, delay-vs-refuse decision. Insert the gate at PL-005 **step 1.6** in `daemon.go` (after T8's step-1.5 flywheel-lock gate, before the sweep); delay or exit 26 + emit `daemon_boot_throttled`. Append code 26 `daemon-boot-throttled` to operator-nfr §8 + `CommandDaemon` (AFTER T7's contiguous 0–25). Add the `daemon_boot_throttled` event (event-model §8.7).
- **Spec ref:** SPEC §4 C6 PL-005c + §3.3 (step 1.6) + §3.4 (26 after 25); `05-specs/C6`.
- **Deliverables:** `bootbackoff.go`, `daemon.go` gate, `exitcode.go`/`commandcodes.go` code-26, spec edits (process-lifecycle PL-005c + PL-005-step-list 1.5/1.6 + PL-011, operator-nfr §8 code 26, event-model `daemon_boot_throttled`), `bootbackoff_test.go`.
- **Acceptance:** C6-AC1 (reproduce-first: delay for k<CEILING, refuse exit 26 for k≥CEILING, both branches), AC2 (clean not throttled), AC3 (self-healing prune), AC5 (observable), AC6 (gate at 1.6, after pidfile + flywheel gate, before sweep), AC7 (`VerifyCommandExitCodeSets` passes with 24/25/26 contiguous).
- **Deps:** T2 (boots.jsonl), T7 (registry contiguous 0–25 before 26), T8 (step-1.5 gate precedes step-1.6).

### Step G — reap-on-exit + spawn cap (C4)

**T10 — Reap-on-exit on supervisor `Run`-return + `supervise stop` (C4 part 1).** → bead **hk-xb5yi**
- **Build:** add a `defer reapOnExit()` at the top of `internal/supervise/supervisor.go` `Run` (fires on all returns); reapOnExit enumerates this project's `harmonik-<project_hash>-` tmux sessions + provenance-marked `claude`/`br` (reuse `internal/lifecycle/tmux/orphansession.go` + `SweepOrphanHandlers`/`SweepOrphanBr`), SIGTERM→5s→SIGKILL, honoring the PL-006d live-supervisor skip. Same reap invoked in `cmd/harmonik/supervise/stop.go` after the PID kill. Add a best-effort reap on the daemon PL-011 drain.
- **Spec ref:** SPEC §4 C4 PL-019(i); `05-specs/C4` Part 1; process-lifecycle PL-019(i).
- **Deliverables:** `supervisor.go` defer, `stop.go` reap, `daemon.go` drain reap, spec edit, `supervisor_test.go` additions.
- **Acceptance:** C4-AC1 (marked killed, other-project survives), AC2 (live different-supervisor skipped), AC3 (`supervise stop` clears child tree), AC4 (daemon drain best-effort, SIGKILL→next-boot sweep).
- **Deps:** T8 (C5 single-flywheel invariant is the safety basis for the project-prefix reap — `06-integration.md` §5.3).

**T11 — Concurrent-spawn cap: unified live-child gauge + refuse-and-emit (C4 part 2).** → bead **hk-xb5yi**
- **Build:** new `internal/daemon/spawncounter.go` — a unified atomic live-child counter incremented at the OS-spawn boundary (implementer + reviewer + resume), decremented on watcher reap (HC-011/HC-011a). In `internal/daemon/workloop.go` (~595), add the `SPAWN_CAP` check alongside the `>= maxConcurrent` check: over-cap → REFUSE (do not launch), emit `spawn_cap_exceeded`, defer the item (no busy-spin). Increment/decrement on the reviewer spawn in `internal/workspace/createworktree.go`. Add `spawn_cap_exceeded` event (event-model §8.7).
- **Spec ref:** SPEC §4 C4 PL-014b; `05-specs/C4` Part 2; process-lifecycle PL-014b + event-model `spawn_cap_exceeded`.
- **Deliverables:** `spawncounter.go`, `workloop.go` cap gate, `createworktree.go` counter, `core` payload, spec edits, tests.
- **Acceptance:** C4-AC5 ((cap+1)th refused + `spawn_cap_exceeded`, no busy-spin), AC6 (child exit frees slot), AC7 (reviewer spawn counts), AC8 (cap behavior distinct from `--max-concurrent`).
- **Deps:** none structurally (independent of T10); shares bead hk-xb5yi.

### Test tasks (the gate)

**T12 — Scenario test (boot-path end-to-end).** → bead **hk-a31od** (`scenario-test`, EXISTS, open)
- **Build:** a twin/real-claude scenario test: one `harmonik daemon` boot that simultaneously removes an orphaned worktree (C1), resets a stuck `in_progress` bead with drained intent + absent queue.json + ledger row (C2), reconciles a dead-run `dispatched` item to pending/failed/completed (C3), and is throttled when crash-looping in the window (C6). Asserts `.harmonik/events/events.jsonl` carries `daemon_orphan_sweep_completed{worktree_dirs_removed, bead_in_progress_reset}` + `queue_item_reconciled{reason=dead_run_*}` + `daemon_boot_throttled{action}` with expected counts.
- **Spec ref:** SPEC §6; `06-integration.md` §7.
- **Acceptance:** the bead's terminal JSONL conditions assert true on the twin substrate.
- **Deps:** T4, T5, T6 (C1/C2/C3) + T9 (C6). **Gate:** hk-a31od already BLOCKS hk-9eury and hk-7t9g1 — those impl beads may not close until T12 passes.

**T13 — Exploratory test (operator-CLI surface).** → bead **hk-izs8s** (`exploratory-test`, EXISTS, open)
- **Build:** `harmonik supervise stop` reaps the child tree (no `harmonik-<project_hash>-` tmux / spawned `claude` remains — C4); a second `harmonik supervise start` while a flywheel daemon is live is refused exit 24 + a stderr diagnostic naming the live owner (C5); a crash-looping `harmonik daemon` is refused exit 26 + retry-after (C6).
- **Spec ref:** SPEC §6; `06-integration.md` §7.
- **Acceptance:** the bead's CLI side-effects + exit codes assert true.
- **Deps:** T8, T9, T10, T11 (C4/C5/C6 CLI surfaces). **Gate:** hk-izs8s already BLOCKS hk-xb5yi, hk-li14r, hk-7t9g1 — those impl beads may not close until T13 passes.

---

## Dependency graph (DAG)

```
T1 (runledger) ─────────────┐
T2 (bootlog/generation) ┄┄┄┄┤(soft: T1 reads generation)
                            │
T3 (pre-sweep set+payload) ─┼──► T4 (worktree reap, C1) ──┐
                            ├──► T5 (in_progress reset, C2)┤
                            └──► T6 (dispatched reconcile, C3) ◄── T4 (sidecar), T5 (exclusion-a)
T7 (exit-codes 25→CmdSupervise→24) ──► T8 (flywheel lock, C5) ──► T10 (reap-on-exit, C4)
T7 ──► T9 (boot-backoff+code26, C6) ◄── T2 (bootlog), T8 (step1.5 before 1.6)
T11 (spawn cap, C4)  [independent]
T4,T5,T6,T9 ──► T12 (scenario test, hk-a31od)
T8,T9,T10,T11 ──► T13 (exploratory test, hk-izs8s)
```

No cycles. Critical path: T2/T7 → T8 → T9 → T13, and T1/T3 → T4/T5 → T6 → T12.

**Parallelization:** {T1, T2, T3, T7, T11} have no inter-dependencies and can start concurrently. After T3: {T4, T5} parallel. After T7: {T8 then T9} sequential (step-1.5-before-1.6 + 25-before-26); T10 after T8. T6 joins T4+T5. The two test tasks (T12, T13) are the final gate.

## Coverage check (every SPEC section → task)

| SPEC section | Task(s) |
|---|---|
| §3.1 pre-sweep `DiscoverActiveRuns` set | T3 |
| §3.2 shared sweep payload | T3 (fields), T4/T6 (counts) |
| §3.3 step labels 1.5/1.6 | T8 (1.5), T9 (1.6) |
| §3.4 ordered exit codes 25→24→26 | T7 (25/24), T9 (26) |
| §4 C1 PL-006e | T4 |
| §4 C2 PL-006f | T1, T5 |
| §4 C3 QM-002c | T6 |
| §4 C4 PL-014b | T11 |
| §4 C4 PL-019(i) | T10 |
| §4 C5 PL-002c + §8 codes | T7, T8 |
| §4 C6 PL-005c + code 26 | T2, T9 |
| §6 testing | T12, T13 |

Every SPEC section and every component AC (C1-AC1..7, C2-AC1..6, C3-AC1..6, C4-AC1..8, C5-AC1..6, C6-AC1..7) is assigned to at least one task.

---

## Task → bead reconciliation (and GAPS)

Every task above maps to an existing `codename:reap` bead. The four implementation beads are coarse; the mapping is many-tasks-to-one-bead, which is expected and not a gap. Reconciliation result:

- **hk-9eury** (C1+C2+C3): T1, T3, T4, T5, T6 — fully owned.
- **hk-xb5yi** (C4): T10, T11 — fully owned.
- **hk-li14r** (C5): T7 (the 24/25/`CommandSupervise` portion), T8 — owned.
- **hk-7t9g1** (C6): T2, T7 (the code-26 portion is attributed to hk-7t9g1; T7's body is shared between hk-li14r and hk-7t9g1), T9 — owned.
- **hk-a31od** (scenario test): T12 — owned.
- **hk-izs8s** (exploratory test): T13 — owned.

### GAPS (noted, NOT filed — per the Tasks-pass instruction)

1. **Shared exit-code registry obligation (T7) spans two beads.** T7's ordered §8 work (absorb 25 → add `CommandSupervise` → add 24, then later code 26) is a single cross-cutting integration action that hk-li14r (C5, needs 24/25) and hk-7t9g1 (C6, needs 26) BOTH depend on. There is no dedicated "exit-code registry integration" bead. **Recommendation (for the orchestrator, not actioned here):** implement T7 once under hk-li14r (it lands the 25-absorption + `CommandSupervise` + 24), and have hk-7t9g1's T9 append code 26 on top — sequence hk-li14r before hk-7t9g1 so the registry is contiguous. No new bead is strictly required (the work fits inside the two existing feature beads), but if the orchestrator prefers an explicit seam, a `chore` bead "operator-nfr §8: absorb 25, add CommandSupervise, codes 24/26" would isolate it. **This is the only genuine task→bead boundary ambiguity; it does not block — it is a sequencing note.**

2. **Sibling-work dependency on `credfence`/`pilot` is out-of-bead by design.** The SPEC notes reap is independent of `pilot` and that `credfence` lands first as the safety prerequisite; no reap bead owns that cross-work sequencing (correctly — it is an orchestrator-level ordering, captured in `01-problem-space.md` Non-goals and `06-integration.md` §3, not a reap task). Not a gap in the reap task list; recorded for completeness.

3. **`daemon_generation` source if hk-7t9g1 lands after hk-9eury** (integration-review advisory A1): T1 (hk-9eury) reads the generation counter T2 (hk-7t9g1) owns. If hk-9eury merges first, T1 uses a daemon-start-ns surrogate per SPEC §2.5. No bead owns this sequencing note; it is a soft-dependency the orchestrator should honor (prefer hk-7t9g1's T2 before hk-9eury's T1, or accept the documented surrogate). Not a missing bead — a build-order note.

No task lacks an owning bead. The single boundary item (GAP 1) is a shared-task-across-two-beads sequencing note, resolvable within the existing beads. No beads were created in this pass.
