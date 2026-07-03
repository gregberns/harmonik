# reap — Integration Plan

**Work:** reap (Crash recovery & orphan remediation) · **Jig:** plan (v1)
**Inputs assembled:** `01-problem-space.md`, `03-components.md`, `04-research/{C1..C6}/findings.md`, `05-specs/{C1..C6}-*.md`, `change-spec-review.md`.
**Status of the change-spec critical-review must-fixes:** all six applied to the C1/C3/C4/C5/C6 drafts before this pass (see §6 "Carried-forward review items, now closed"). This integration plan ENCODES those resolutions as the cross-cutting contract so an implementing agent does not re-derive them.

This document describes how the six components connect, the order they must be built, the shared state and resources that cross component boundaries, the cross-cutting concerns no single component spec owns, and the integration-testing strategy. `SPEC.md` (the companion artifact) is the self-contained assembly an implementing agent reads first; this file is the rationale and the integration obligations behind it.

---

## 1. Component map (what connects to what)

Six components in three pairs, plus one standalone, all landing on the daemon boot path and the supervise/daemon process-lifecycle:

| Comp | One line | Spec home (new clause) | Bead |
|---|---|---|---|
| **C1** | Remove orphaned `.harmonik/worktrees/<run_id>/` dirs on boot | process-lifecycle **PL-006e** + event-model §8.7.14 | hk-9eury |
| **C2** | Durable run-ledger provenance so stuck `in_progress` beads actually reset | process-lifecycle **PL-006f** + PL-004 inventory | hk-9eury |
| **C3** | Reconcile dead-run `dispatched` queue items on boot | queue-model **QM-002c** + event-model §8.10.7 reason enum | hk-9eury |
| **C4** | Reap-on-exit + concurrent-spawn cap | process-lifecycle **PL-014b** + **PL-019(i)** + event-model §8.7 `spawn_cap_exceeded` | hk-xb5yi |
| **C5** | Single-flywheel-per-project lock | process-lifecycle **PL-002c** + operator-nfr §8 codes 25/24 + `CommandSupervise` | hk-li14r |
| **C6** | Boot-level restart backoff | process-lifecycle **PL-005c** + operator-nfr §8 code 26 + event-model §8.7 `daemon_boot_throttled` | hk-7t9g1 |

The three connection clusters:

- **Boot orphan-sweep / queue-load cluster (C1 + C2 + C3).** All three run inside the PL-005 deterministic startup sequence: C2's ledger and the pre-sweep `DiscoverActiveRuns` set are produced at step 3, C1's worktree reap consumes them at step 3, C2's bead-reset runs at step 4.5, C3's dispatched reconcile runs at step 8a. They share two pieces of state: the **pre-sweep active-run set** (§2.1) and the **`daemon_orphan_sweep_completed` payload** (§2.2).
- **Process-lifecycle / supervise cluster (C4 + C5).** Both ride the PL-006a provenance marker + PL-006d live-supervisor exclusion. C5's single-flywheel invariant is the *safety precondition* that makes C4's project-prefix reap-on-exit safe (under the lock there is at most one flywheel owner per project, so a prefix sweep on exit cannot reach a sibling owner's healthy children). They share the **operator-nfr §8 exit-code registry** with C6 (§2.3).
- **Standalone gate (C6).** The boot-backoff gate is self-contained but slots into the same PL-005 step list as C5 (step 1.5 vs 1.6, §2.4) and shares the exit-code registry obligation with C5 (§2.3). It also *defines* the `daemon_generation` counter (the `boots.jsonl` monotonic generation) that C2's ledger rows and C5's lock marker both reference (§2.5).

## 2. Shared state & resources crossing component boundaries

### 2.1 The pre-sweep active-run set (C1 ⟷ C3) — the single survive-check source

This is the integration's load-bearing shared resource and the resolution of review must-fix C1/C3.

- **Producer:** the daemon, at **PL-005 step 3**, calls `lifecycle.DiscoverActiveRuns` (EM-031a; `internal/lifecycle/activerun_em031a.go:314`) ONCE. `DiscoverActiveRuns` builds the active-run set from the Beads non-terminal query + the git task-branch-tip scan ALONE — it does NOT require the step-7 in-memory model rebuild (which `internal/lifecycle/orphansweepbeads.go:50-58` documents is not wired as a distinct phase at MVH). This is the same lightweight-pre-sweep pattern the daemon already uses for queue-provenance (`internal/daemon/daemon.go:705-741` does a raw `queue.Load` before `LoadQueueAtStartup`).
- **Consumer 1 (C1, step 3):** PL-006e clause (ii) — a worktree's `run_id` in the pre-sweep set ⇒ NOT removed.
- **Consumer 2 (C3, step 8a):** QM-002c survive-check (i) — a `dispatched` item's `run_id` in the *same* pre-sweep set ⇒ left `dispatched`. The set is threaded into `LoadQueueAtStartup` rather than rebuilt at step 7, so C1 and C3 read ONE active-run set against ONE filesystem snapshot — no two divergent sets racing.
- **Degradation:** on `ErrBeadsUnavailable`, the set falls back to the git-branch-tip scan alone; a worktree/item with a live task-branch tip is treated as re-attached (conservative skip). This matches `DiscoverActiveRuns`'s documented `ErrBeadsUnavailable` contract.
- **Why this matters at integration time:** C1's PL-006e and C3's QM-002c BOTH originally said "the EM-031a re-attached set re-attached by PL-005 step 7" while also asserting C1 "runs before socket bind at step 3" — mutually exclusive, since the step-7 set does not exist at step 3. The integration contract is: **one `DiscoverActiveRuns` call at step 3 feeds both.** The daemon.go wiring (§3 build step B) makes the single call and passes the result into both `RunOrphanSweep` (C1) and `LoadQueueAtStartup` (C3).

### 2.2 The `daemon_orphan_sweep_completed` payload (C1 + C2 + C3 + the existing PL-006d field)

Additive-only integer fields on ONE event (event-model §8.7.14), N-1 tolerated per EV-029:

| Field | Owner | Meaning |
|---|---|---|
| `bead_in_progress_reset` | existing (PL-006) / C2 widens the source | beads reset `in_progress → open` |
| `coordinator_sessions_skipped` | existing (PL-006d) | live-supervisor sessions the sweep skipped (C4 reap-on-exit also increments) |
| `worktree_dirs_removed` | **C1 (PL-006e)** | orphaned worktree dirs removed |
| `dispatched_items_reconciled` (split `requeued`/`failed`/`completed`) | **C3 (QM-002c)** | dead-run dispatched items reconciled |

Integration obligation: a SINGLE `daemon_orphan_sweep_completed` is emitted once after the sweep+reconcile complete; the C3 dispatched-reconcile summary may be a separate sub-count surfaced on the same event or on the boot-reconcile summary structure (`internal/queue/...`), but the payload schema bump is additive-only — no field is renamed or removed (consumer-stability per event-model §6.3 N-1).

### 2.3 The operator-nfr §8 exit-code registry (C5 + C6) — ORDERED obligation

This is the resolution of review must-fix C5/C6 exit-code allocation. The central registry (`internal/operatornfr/exitcode.go`) currently declares codes 0–23; code 25 (`ExitCodeSupervisorRunning`) is a LOCAL const in `cmd/harmonik/supervise/start.go:23` NOT in the registry, and there is no `supervise` `CommandName` in `internal/operatornfr/commandcodes.go` (only daemon/attach/enqueue/status/pause/stop/upgrade/list/runner). Allocating 24 (C5) and 26 (C6) while leaving a hole at 25 produces a non-contiguous registry that `VerifyCommandExitCodeSets` cannot resolve. The integration pass MUST perform these steps **in this order** (this is a normative integration obligation, recorded so finalize/implementation does it deliberately rather than silently):

1. **Absorb code 25** `supervisor-already-running` into operator-nfr §8 (the value already used at `start.go:23`; PL-019(c)'s "PL-INTERIM pending ON absorption" note resolves here). Add the §8 row + ON-001/ON-003 catalog entry. Registry stays contiguous (0–25) before 24 is inserted.
2. **Add a `CommandSupervise` `CommandName`** + `CommandExitCodeSet` in `commandcodes.go` (covering `supervise` / `supervise start`), declaring codes 0/17/24/25 used on that surface. Without it, no set can declare 24/25 for the supervise surface and AC6/AC7 cannot pass.
3. **Add code 24** `flywheel-already-running` (C5) to §8 + to `CommandSupervise` and to `CommandDaemon` (the `--flywheel` daemon boot).
4. **Add code 26** `daemon-boot-throttled` (C6) to §8 + to `CommandDaemon`.

After steps 1–4, `VerifyCommandExitCodeSets()` MUST resolve 24, 25, AND 26 to §8 entries and the registry is contiguous 0–26. **Separate, non-blocking:** `start.go:19`'s LOCAL `ExitCodeDaemonDown=17` semantically collides with the registry's code-17 `multi-daemon-target-missing`; the integration pass SHOULD reconcile the dual meaning of code 17 but this does NOT gate 24/25/26 registration. (Recorded; do not silently rewrite — file-discipline.)

### 2.4 The PL-005 startup-step labels (C5 + C6) — disambiguation

Resolution of review must-fix C5/C6 step-1.5 collision. Both C5 and C6 insert a gate between PL-005 step 1 (pidfile lock) and step 3 (orphan sweep). Distinct labels:

- **Step 1.5 — flywheel-lock acquire** (C5, PL-002c): when flywheel topology, acquire `.harmonik/flywheel.lock` immediately after the pidfile lock, before any state write.
- **Step 1.6 — boot-backoff gate** (C6, PL-005c): after step 1.5, compute the recent-abnormal-boot count and delay-or-refuse, before the orphan sweep.

Integration obligation: the final merged PL-005 step list MUST land BOTH labels together so the numbering is unambiguous. If only one of C5/C6 lands first, step 1.5 is reserved for C5 and the boot-backoff gate stays at 1.6 (never re-uses 1.5). The change-spec-review CF-2 flagged PL-005 renumbering vs the sibling `pilot` work (which also touches PL-005's auto-pull step); the final integration with `pilot` must verify no integer-step renumbering — reap uses fractional 1.5/1.6 insertions precisely to avoid renumbering existing integer steps.

### 2.5 The `daemon_generation` counter (C6 defines; C2 + C5 reference)

C6's `boots.jsonl` defines a monotonic `daemon_generation` (max prior generation + 1) per boot. C2's run-ledger rows carry `daemon_generation` (which boot wrote the claim) and C5's flywheel-lock marker records `started_at`+`pid` (and may carry the generation for diagnostics). Integration obligation: the generation counter is OWNED by C6's `bootlog.go`; C2's `runledger.go` and C5's `flywheellock.go` READ it (a single source). If C6 is not yet merged, C2/C5 use a daemon-start nanosecond timestamp as the generation surrogate until C6 lands (documented degradation, not a divergence — the ledger's exclusion ordering is the gate regardless).

### 2.6 The PL-006a provenance marker + PL-006d live-supervisor exclusion (C1 + C4 + C5)

The structural safety invariant shared across the kill/reset/remove components. PL-006a (env var on Linux / PGID on darwin / tmux `harmonik-<project_hash>-` prefix) gates every PROCESS and TMUX kill (C4 reap-on-exit, C5 lock scope). C1's worktree-dir removal uses a DIFFERENT provenance boundary — filesystem location + git-worktree registration under `<repo>/.harmonik/worktrees/` (PL-006e path-gate) — because a worktree dir is filesystem-resident, not a process. PL-006d (`.harmonik/cognition/supervisor.sentinel` + live-PID probe) excludes a live coordinator session from both the boot sweep (existing) and C4's reap-on-exit (new trigger, same primitive). Integration obligation: C4's reap-on-exit reuses the existing `internal/lifecycle/tmux/orphansession.go` enumeration + `SweepOrphanHandlers`/`SweepOrphanBr` + the PL-006d sentinel skip; it does NOT introduce a parallel enumeration. The `coordinator_sessions_skipped` count is shared with the boot sweep's payload field (§2.2).

## 3. Integration order (build sequence)

Derived from the §03 dependency graph + the shared-state contracts above. Safety-prerequisite ordering relative to siblings: `credfence` (credential scrub + spend ceiling) lands FIRST as the safety prerequisite; reap is independent of `pilot` (quiet daemon / Pi dispatch / pause). Within reap:

- **Step A — C2 run-ledger + C6 boot-log (the durable-marker foundation).** C2's `runledger.go` and C6's `bootlog.go` define the two durable JSONL markers and the `daemon_generation` counter (C6 owns it, C2 reads it). Build together because C2's ledger rows reference C6's generation. Wire the claim-time append (C2) at `runregistry.go:94` and the boot-row append (C6) early in `daemon.Start`. No behavior change yet (markers written, not yet consumed).
- **Step B — the pre-sweep `DiscoverActiveRuns` call + payload plumbing (the shared survive-check).** In `daemon.go` (~705-741, the existing pre-sweep block), add the single `DiscoverActiveRuns` call at step 3 alongside the existing `queue.Load`; thread the result into both `RunOrphanSweep` (for C1) and `LoadQueueAtStartup` (for C3). Add the additive payload fields to `DaemonOrphanSweepCompletedPayload` and the boot-reconcile summary. This is the wiring that §2.1 and §2.2 depend on; it precedes C1/C3 logic.
- **Step C — C1 worktree reap + C2 bead-reset widening (boot sweep remediation).** C1's `WorktreeReaper` (consumes the step-B active-run set + lease-lock liveness + C1's sidecar-preservation via `internal/workspace/crashevidence.go`). C2 widens the ownership gate in `orphansweepbeads.go` with the `LedgerProvenanceSet` from step A. Both land in `RunOrphanSweep`.
- **Step D — C3 dispatched reconcile (queue-load remediation).** `reconcileDeadRunDispatchedItems` after `reconcileDispatchedItems` in `startup_pl005_qm002.go`, consuming the step-B active-run set + the `crashevidence.go` evidence classification (which C1's sidecar-preservation keeps alive — §5.1 ordering).
- **Step E — C5 flywheel lock + the ordered exit-code obligation (§2.3).** `flywheellock.go` (modeled on `pidfilelock.go`); acquire at PL-005 step 1.5 (daemon) / before sentinel writes (supervise). Perform the §2.3 ordered §8 registration (25 → `CommandSupervise` → 24) FIRST so the lock's exit-24 refusal resolves.
- **Step F — C6 boot-backoff gate (§2.4) + code 26.** `bootbackoff.go`; insert the step-1.6 gate after C5's step-1.5 gate; append code 26 to §8 (after step E's contiguous 0–25). PL-011 drain writes `exit_disposition=clean`.
- **Step G — C4 reap-on-exit + spawn cap.** Reap-on-exit reuses C5's single-flywheel invariant + the PL-006a/PL-006d primitives; the spawn cap adds the unified live-child counter at the OS-spawn boundary (covering implementer + reviewer + resume). Land after C5 because C4-R2's "another live supervisor" safety relies on C5's invariant.

Steps A and B are prerequisites for everything; C–G are largely parallel after them, with the C5-before-C4 and step-E-before-step-F exit-code-registry ordering as the only hard inter-step constraints.

## 4. Cross-cutting concerns (not owned by any single component spec)

- **Logging.** Every remediation action (remove/reset/reconcile/kill/refuse/throttle) logs per operator-nfr §4.9 ON-035. Removal/kill errors are non-fatal: appended to the sweep error accumulator + logged, never aborting `daemon.Start` (the PL-006 best-effort posture). C5/C6 refusals are pre-event-bus on the supervise path (a refused start has no event bus) and `daemon_startup_failed{failure_mode=...}` on the daemon path after PL-005 step 0.
- **Configuration.** New operator-tunable knobs, all with finite defaults: `SPAWN_CAP` (C4, default a small multiple of the PL-014a ceiling with a `HARD_FLOOR` such as 16), the boot-backoff envelope `HARMONIK_BOOT_BACKOFF_{BASE,CAP,WINDOW,CEILING}` (C6, defaults 1s/60s/10m/5). The flywheel lock (C5) is gated on a `--flywheel` flag/config; a plain non-flywheel daemon acquires none of it. No knob defaults to unbounded (the incident's `budget.ts=Infinity` anti-pattern is explicitly avoided — though the dollar meter itself is `credfence`'s, not reap's).
- **Error propagation across boundaries.** A `DiscoverActiveRuns` `ErrBeadsUnavailable` at step B degrades the survive-check to git-branch-tips for BOTH C1 and C3 (one degradation, consistent across consumers). A `ShowBead` failure in C3's survive-check leaves the item `dispatched` (conservative, retried next boot). A ledger/boot-log append failure logs and proceeds with prior-behavior provenance (never a wrong reset). The cap counter is in-memory (reset on boot); a leaked slot is bounded by the watcher-reap (HC-011/HC-011a) + next-boot reset.
- **Idempotency & crash-safety.** Every boot-path action is idempotent across repeated boots: worktree removal of an absent dir is a no-op (C1); the bead-reset is idempotency-keyed `<project_hash>:<bead_id>:reset:<daemon_start_ns>` (C2); a reconciled item is `pending`/`failed`/`completed` (out of the dispatched scan, C3); the flywheel lock + boots.jsonl survive process crash via fd-lifetime advisory locking + durable JSONL (C5/C6).
- **Provenance is the universal gate.** No kill/reset/remove proceeds without this project's PL-006a marker (processes/tmux) or the PL-006e path-gate (worktree dirs). A resource bearing a DIFFERENT project's marker is never touched (the negative test, success-criterion 9). This is the hard multi-project-machine safety invariant and it is asserted in every component's ACs.

## 5. Known cross-component ordering dependencies (must hold at runtime)

### 5.1 C1 sidecar-preservation gates C3's terminal-failure read
C1 (PL-006e, step 3) removes bare-stale worktrees BEFORE C3 (QM-002c, step 8a) reads "worktree present, no `Refs:` commit" as `dead_run_failed` evidence. C1's clause (iii) PRESERVES a worktree carrying an unprocessed session sidecar (WM-003a / `internal/workspace/crashevidence.go`) until step-8 Cat-3 reconciliation. That preservation is the guard that keeps C3's `dead_run_failed` evidence alive: a worktree C3 needs to classify as terminal-failure has an unreconciled sidecar, so C1 does not remove it at step 3. Integration obligation: C1's sidecar-preservation MUST land with or before C3, and C3's classification reads against the post-C1-sweep filesystem.

### 5.2 C2 exclusion (a) does not double-handle C3's re-queued bead
The orphan sweep's stale-`in_progress` reset (C2, step 4.5) EXCLUDES queue-dispatched beads (exclusion (a) of PL-006). So a bead whose queue item is still `dispatched` at sweep time is NOT reset by C2; C3 (step 8a) then re-queues that item to `pending` on the SAME boot. No second sweep pass is needed and no double-handling occurs (C3-R4). Integration obligation: C2's exclusion (a) reads the step-B pre-sweep `queue.Load` dispatched set (already wired at `daemon.go:705-741`); C3 runs after C2.

### 5.3 C5-before-C4 safety precondition
C4's reap-on-exit reuses the project-prefix tmux/subprocess enumeration on the exit path. This is only safe because C5's single-flywheel invariant (under the pidfile lock + flywheel lock) guarantees at most one flywheel owner per project — so the prefix sweep cannot reach a sibling owner's healthy children. Integration obligation: C5 lands before (or with) C4; C4's spec text explicitly cites the §PL-002 + §PL-019(c)/§PL-002c invariant as its safety basis.

### 5.4 Step-1.5-before-step-1.6 and exit-code 25-before-24-before-26
§2.4 and §2.3 above. The flywheel-lock gate (1.5) precedes the boot-backoff gate (1.6); the §8 registry absorbs 25 + adds `CommandSupervise` before adding 24, then 26.

## 6. Carried-forward review items, now closed

The change-spec critical-review surfaced six must-fix items; all are resolved in the C1/C3/C4/C5/C6 drafts and encoded as integration contracts here:

1. **C1/C3 ordering contradiction (blocking)** → resolved via §2.1: ONE `DiscoverActiveRuns` call at PL-005 step 3 (Beads + git-ref scan, independent of the step-7 model) is the shared survive-check source for both C1's worktree reap and C3's dispatched reconcile. Matches the daemon.go:705-741 pre-sweep `queue.Load` pattern.
2. **PL-005 step-1.5 label collision (blocking-for-integration)** → resolved via §2.4: 1.5 = flywheel-lock (C5/PL-002c), 1.6 = boot-backoff (C6/PL-005c); distinct labels, both land together.
3. **Exit-code 25 gap / non-contiguous registry (blocking)** → resolved via §2.3: ordered obligation absorb-25 → add-`CommandSupervise` → add-24 → add-26; `VerifyCommandExitCodeSets` resolves 24/25/26.
4. **C1 `crashevidence.go` anchor wrong (must-fix)** → corrected to `internal/workspace/crashevidence.go` (WM-003a) in C1 PL-006e (iii) and Files table, and in C3's evidence-classification references. (Note: the file DOES exist, in the workspace package, not lifecycle — anchor now package-qualified throughout.)
5. **Event-class / field-name precision (must-fix)** → (a) C3 now cites event-model §8.10.7 explicitly and notes the class-F enum extension on `queue_item_reconciled.reason`; (b) C4's gauge-decrement now cites HC-011 (watcher-ownership + Wait/reap) not HC-024, with HC-011a panic-recovery for the leak case and HC-024a noted for the socket-EOF variant; (c) `coordinator_sessions_skipped` confirmed existing (process-lifecycle.md:333, PL-006d) — no change, noted.

## 7. Integration testing strategy

- **Scenario test (hk-a31od)** — the boot-path end-to-end on the twin substrate: a single `harmonik daemon` boot that simultaneously (a) removes an orphaned worktree (C1), (b) resets a stuck `in_progress` bead with drained intent + absent queue.json + a ledger row (C2), (c) reconciles a dead-run `dispatched` item to pending/failed/completed (C3), and (d) is throttled when crash-looping within the window (C6). Asserts the terminal `.harmonik/events/events.jsonl` carries `daemon_orphan_sweep_completed{worktree_dirs_removed, bead_in_progress_reset}`, `queue_item_reconciled{reason=dead_run_*}`, and `daemon_boot_throttled{action}` with expected counts. This test exercises §2.1 (shared active-run set), §2.2 (shared payload), §5.1/§5.2 (ordering) together.
- **Exploratory test (hk-izs8s)** — `harmonik supervise stop` reaps the child tree (C4: no `harmonik-<project_hash>-` tmux / spawned `claude` remains); a second `harmonik supervise start` while a flywheel daemon is live is refused with exit 24 + a stderr diagnostic naming the live owner (C5); a crash-looping `harmonik daemon` is refused with exit 26 + retry-after (C6). Exercises §2.3 (exit codes), §2.4 (step ordering), §5.3 (C5-before-C4 safety).
- **Per-component unit/seam tests** — each component's `05-specs` Verification block (table-driven, clock/seam-injected); the reproduce-first tests (C2-AC1, C6-AC1) assert both the with-fix and without-fix branch in one table-driven test, locking the fix.
- **Negative / cross-project test** — success-criterion 9: a resource bearing a DIFFERENT project's PL-006a marker (or a worktree outside the path-gate) is never reset/killed/removed across C1–C5. One shared negative fixture exercised by each component.
- **Registry conformance** — `VerifyCommandExitCodeSets()` is a unit gate run after the §2.3 ordered add; AC6 (C5) / AC7 (C6) depend on it passing with 24/25/26 contiguous.

## 8. Integration completeness self-check

| Success criterion (01-problem-space §Success criteria) | Component → change-spec section |
|---|---|
| 1 — reset N stuck in_progress (observed-6→reset-6) | C2 PL-006f / AC1 |
| 2 — kill M orphaned flywheel tmux, skip live | existing PL-006d (verified) + C4 PL-019(i) reap-on-exit / AC2 |
| 3 — remove K orphaned worktrees | C1 PL-006e / AC1 |
| 4 — reconcile dispatched → pending/failed | C3 QM-002c / AC1-AC2 |
| 5 — no live billable child after death | C4 PL-019(i) + next-boot sweep / AC3-AC4 |
| 6 — refuse (cap+1)th spawn + typed signal | C4 PL-014b / AC5 |
| 7 — throttle rapid boots; 10/window not reproducible | C6 PL-005c / AC1 |
| 8 — second flywheel start refused (exit + diagnostic) | C5 PL-002c / AC1 |
| 9 — provenance-gated (cross-project never touched) | all components, shared negative fixture / §4 |

Every success criterion traces to a component and a change-spec section; every change-spec section traces to a requirement and a goal (the §03 coverage table). No contradiction remains between component specs after the six must-fix resolutions. `SPEC.md` assembles these without adding requirements or changing decisions.
