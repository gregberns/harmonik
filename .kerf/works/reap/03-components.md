# reap — Decomposition (components & requirements)

Autonomous mode (delegated; no interactive user). Six components. Each requirement is concrete and testable, traceable to a Problem-Space goal (G1–G7) and/or an attached bead (hk-9eury reconcile+reap, hk-xb5yi spawn-cap+reap-on-exit, hk-li14r single-flywheel-lock, hk-7t9g1 restart-backoff). Grounded in `02-analysis.md` gaps #1–#7.

---

## C1 — Remediating worktree-directory reaping (sweep gap #1)
**One line:** Extend the boot orphan sweep to REMOVE orphaned `.harmonik/worktrees/<run_id>/` directories, not just clear their lease-locks.
**Goal:** G1. **Bead:** hk-9eury. **Analysis gap:** #1.

Requirements:
- C1-R1: On boot, for each `.harmonik/worktrees/<run_id>/` directory whose lease-lock is stale (per WM-013a) AND whose `run_id` is not re-attached to a live in-flight run rebuilt at PL-005 step 7, the sweep MUST `git worktree remove --force` (or equivalent prune + `RemoveAll`) the directory and prune the registered git worktree entry.
- C1-R2: A worktree whose `run_id` IS re-attached to a live run, OR whose lease-lock is held by a live process (flock probe `EWOULDBLOCK`), MUST NOT be removed.
- C1-R3: A worktree directory outside this project's `<repo>/.harmonik/worktrees/` root MUST NOT be touched (provenance by filesystem location + git-worktree registration under this repo).
- C1-R4: The count of removed worktree directories MUST appear as a new additive integer field (e.g. `worktree_dirs_removed`) on `daemon_orphan_sweep_completed`, N-1-tolerated per event-model §6.3.
- C1-R5: Removal errors (locked file, git failure) MUST be non-fatal: logged + summarized in the sweep's error field, never aborting `daemon.Start`.
- C1-R6: Re-running the sweep when no orphaned worktrees remain MUST be a no-op (idempotent); count = 0.

## C2 — Durable provenance for the stale-`in_progress` reset (sweep gap #2)
**One line:** Give the bead-reset sweep a provenance signal that survives queue.json removal AND intent-log drain, so genuinely-stuck `in_progress` beads (the incident's "observed 6, reset 0") actually get reset.
**Goal:** G1. **Bead:** hk-9eury. **Analysis gap:** #2.

Requirements:
- C2-R1: The daemon MUST maintain a durable, project-scoped record of beads it has claimed-into-`in_progress` (a run-ledger or equivalent) that persists across SIGKILL and outlives both the wave-queue's `queue.json` (removed on completion per QM-003) and the BI-030 claim-intent file (drained by BI-031 recovery). This record establishes provenance independent of the existing transient signals.
- C2-R2: On boot, a bead in `br list --status in_progress` whose provenance is established by C2-R1's record (OR by the existing intent-log / queue-owned signals) and that satisfies NO reset-exclusion (a/b/c per PL-006) MUST be reset `in_progress → open` via `BeadResetter` with the `:reset:<daemon_start_ns>` idempotency key.
- C2-R3: A bead in `in_progress` whose provenance CANNOT be established as this project's (no ledger entry, no intent, not queue-owned, and `ProvenanceChecker` nil/false) MUST NOT be reset — the conservative-skip invariant of PL-006 is preserved (no blind cross-project reset).
- C2-R4: The `ProvenanceChecker` seam MUST remain the pluggable point for a future Beads audit-actor `project_hash` checker; the C2-R1 ledger is the MVH-grade provenance that does not depend on Beads exposing the audit actor.
- C2-R5: Reproduce-first: a test MUST reproduce the incident shape — bead `in_progress`, claim-intent drained, `queue.json` absent — and assert that WITH C2's ledger the bead is reset, and WITHOUT it (pre-fix) it is skipped. This locks the fix.
- C2-R6: The reset count MUST roll into `daemon_orphan_sweep_completed.bead_in_progress_reset` (existing field; no schema change).

## C3 — Dispatched queue-item reconciliation on boot (gap #3)
**One line:** On boot, reconcile `dispatched` queue items whose run did not survive — re-queue to `pending` or terminally fail — so no item is stranded as a phantom in-flight forever.
**Goal:** G2. **Bead:** hk-9eury. **Analysis gap:** #3.

Requirements:
- C3-R1: During `LoadQueueAtStartup` (PL-005 step 8a), for each `queue.json` item in status `dispatched`, the daemon MUST determine whether its run survived (re-attached live run at PL-005 step 7, OR an in-flight worktree+lease present, OR a claim-intent recovering it).
- C3-R2: A `dispatched` item whose run did NOT survive MUST be transitioned per the queue/reconciliation rules — to `pending` (re-eligible for re-dispatch) when no terminal evidence exists, OR to `failed` when reconciliation finds a terminal-failure signal (e.g., a `Refs:`-less worktree with no commit). It MUST NOT remain `dispatched`.
- C3-R3: A `dispatched` item whose run DID survive MUST be left `dispatched` and re-driven by the normal recovery path (no double-dispatch).
- C3-R4: The reconciliation MUST be idempotent across repeated boots and MUST NOT interfere with the C2 bead-reset exclusion (a) — a re-queued item's bead is the same one C2 may reset; ordering MUST ensure a re-queued (`pending`) item's bead is treated as queue-owned-and-recoverable, not double-handled.
- C3-R5: A typed observation (event or sweep-payload count, e.g. `dispatched_items_reconciled`) MUST record how many items were re-queued vs failed.
- C3-R6: A test MUST cover: (i) dead-run dispatched → pending; (ii) terminal-failure dispatched → failed; (iii) live-run dispatched → unchanged.

## C4 — Reap-on-exit + concurrent-spawn cap (gaps #4, #5)
**One line:** When the supervisor/daemon dies, kill its provenance-marked child `claude`+tmux tree (reap-on-exit); and enforce a hard upper bound on concurrently-spawned child sessions (spawn cap) that refuses the (cap+1)th spawn.
**Goal:** G3, G4. **Bead:** hk-xb5yi. **Analysis gaps:** #4, #5.

Requirements:
- C4-R1 (reap-on-exit, supervisor): When `Supervisor.Run` returns for any reason (ctx-cancel, Stop, crash-loop, clean exit) AND on `supervise stop`, the process MUST enumerate and kill (SIGTERM→bounded-wait→SIGKILL) every `claude` process and tmux session bearing THIS project's PL-006a provenance marker that it spawned, before returning. A child lacking the marker MUST NOT be killed.
- C4-R2 (reap-on-exit, live-supervisor exclusion): Reap-on-exit MUST honor PL-006d — it MUST NOT kill the live flywheel/coordinator session of a DIFFERENT live supervisor (sentinel + `kill(pid,0)` check); it reaps only its own descendants.
- C4-R3 (reap-on-exit, daemon side): The daemon's spawned implementer/reviewer `claude` sessions are reaped by the existing next-boot orphan sweep (area A) AND, additionally, by a best-effort kill on the daemon's own graceful-shutdown path (PL-011 drain) — closing the live-spend window between death and next boot. SIGKILL of the daemon (uninterceptable) relies on the next-boot sweep; this is documented, not a new requirement.
- C4-R4 (spawn cap): The daemon MUST enforce a hard maximum number of concurrently-live spawned child `claude` sessions (a safety ceiling, distinct from and independent of the operator-set `--max-concurrent` capacity gate at EM-049). The cap MUST have a finite default.
- C4-R5 (spawn cap refusal): An attempt to spawn the (cap+1)th concurrent child MUST be REFUSED — the daemon MUST NOT launch it — and MUST emit a typed signal (event, e.g. `spawn_cap_exceeded`) rather than spawning silently.
- C4-R6 (spawn cap accounting): The live-child count MUST decrement on child exit (watcher-goroutine reap per PL-014/HC-011) so the cap is a live gauge, not a monotonic counter.
- C4-R7: Tests MUST cover: (i) supervisor exit kills its provenance-marked children, leaves unmarked ones; (ii) reap-on-exit skips a different live supervisor's session; (iii) (cap+1)th spawn is refused with the typed signal; (iv) child exit frees a cap slot.

## C5 — Single-flywheel-per-project lock (gap #6)
**One line:** Assert at most one flywheel owner (daemon ⊕ supervised-Pi) per project; a second start is refused with a clear exit code + diagnostic, preventing the dual-orchestrator collision.
**Goal:** G6. **Bead:** hk-li14r. **Analysis gap:** #6.

Requirements:
- C5-R1: The work MUST define the canonical flywheel-ownership lock relationship between the existing daemon pidfile lock (PL-002) and the supervisor lock (PL-019c), such that a SECOND flywheel orchestrator for the same project cannot run concurrently with the first regardless of which layer (daemon vs Pi-supervisor) already holds ownership.
- C5-R2: A second attempt to bring up the flywheel for a project that already has a live owner MUST be REFUSED with a specific non-zero exit code (allocated from operator-nfr §8) and a diagnostic naming the live owner (PID + pidfile/lockfile path).
- C5-R3: The lock MUST be an fd-lifetime advisory `flock` (kernel-auto-released on crash, clean or unclean), per PL-002a discipline. POSIX `F_SETLK` is FORBIDDEN.
- C5-R4: The first (live) owner MUST be undisturbed by the refused second attempt — no state mutation, no signal, no config rewrite.
- C5-R5: A STALE lock (owner crashed, lock auto-released, marker file remains) MUST be reclaimable by a subsequent start without operator intervention (disambiguate via flock-acquire + `kill(pid,0)` probe, mirroring PL-002/PL-024).
- C5-R6: Tests MUST cover: (i) second start refused while first is live (exit code + diagnostic); (ii) start succeeds after first owner exits (stale lock reclaimed); (iii) first owner unaffected by refused second start.

## C6 — Boot-level daemon restart backoff (gap #7)
**One line:** Throttle rapid sequential daemon boots with a crash-aware exponential backoff backed by a durable last-boot marker, so a crash-and-re-pull loop cannot produce 10 boots/day.
**Goal:** G5. **Bead:** hk-7t9g1. **Analysis gap:** #7.

Requirements:
- C6-R1: The daemon MUST persist a durable, project-scoped last-boot record (timestamp + exit-disposition of the prior boot) that survives process death (file under the PL-005 file surface, e.g. `.harmonik/daemon.last-boot`).
- C6-R2: On `daemon.Start`, if the prior boot started within the backoff window AND exited abnormally (crash / non-clean), the daemon MUST apply an exponential backoff (delay before proceeding, or refuse with a retry-after diagnostic) keyed on the recent-boot count.
- C6-R3: The backoff MUST distinguish an operator-intended restart (prior clean shutdown) from a crash-loop (prior abnormal exit): a clean prior shutdown MUST NOT be throttled.
- C6-R4: The backoff envelope (base, cap, window) MUST have finite defaults and be operator-configurable (env/config), mirroring the supervisor backoff knobs.
- C6-R5: The applied backoff MUST be observable (log + event, e.g. `daemon_boot_throttled` with the delay and recent-boot count).
- C6-R6: The backoff state MUST be crash-safe and self-healing: a stale last-boot marker from long ago MUST NOT throttle a legitimate boot (window-bounded).
- C6-R7: A test MUST reproduce the 10-boots-in-a-window shape and assert that under the default backoff the Nth rapid crash-boot is delayed/refused, while a clean-shutdown restart is not throttled.

---

## Dependency graph

- **C2 depends on nothing structural** but its ledger (C2-R1) is the provenance source that **C3** (C3-R4 double-handling avoidance) and **C1** (C1-R2 live-run check) both consult for "is this run alive / owned." Build C2's durable run-ledger first; C1 and C3 read it.
- **C1, C3** are both inside the boot orphan-sweep / queue-load path (`RunOrphanSweep` + `LoadQueueAtStartup`) — they share the PL-005 startup sequence and the `daemon_orphan_sweep_completed` payload. Build together after C2.
- **C4, C5** are both on the supervise/daemon process-lifecycle side and share the PL-006a provenance enumeration + PL-006d live-supervisor exclusion. C5 (single-flywheel lock) is a prerequisite mindset for C4-R2 (reap-on-exit must know what "another live supervisor" means) but they are independently testable.
- **C6** is standalone (boot-time gate) but emits an event consistent with C4/C5's typed-signal convention.
- Safety ordering relative to sibling works: `credfence` (credential scrub + spend ceiling) is the prerequisite safety work and lands first; reap hardens failure modes and is independent of `pilot` (quiet daemon / Pi dispatch / pause).

## Interface summary (data flow & contracts)

| Boundary | Producer → Consumer | Contract |
|---|---|---|
| Durable run-ledger (C2-R1) | daemon claim path → C1/C2/C3 sweep | project-scoped record: `{bead_id, run_id, claimed_at, project_hash}`; survives SIGKILL, queue.json removal, intent drain |
| `daemon_orphan_sweep_completed` payload | sweep → operator/consumers | additive integer fields: existing `bead_in_progress_reset` + new `worktree_dirs_removed`, `dispatched_items_reconciled`; N-1-tolerated |
| `BeadResetter` / `ProvenanceChecker` seams | sweep → BI adapter | unchanged interfaces (`internal/lifecycle/orphansweepbeads.go`); reap supplies the C2 ledger as a new provenance source, ProvenanceChecker stays the future-audit-actor seam |
| Live-child gauge (C4) | spawn site → spawn gate | atomic live-count; increment on spawn, decrement on watcher reap (PL-014/HC-011); refusal at cap+1 → `spawn_cap_exceeded` event |
| Flywheel lock (C5) | daemon/supervisor start → second-start probe | fd-lifetime flock; held ⇒ exit-code + diagnostic; auto-released on crash |
| `.harmonik/daemon.last-boot` (C6) | prior boot → next `daemon.Start` | `{started_at, exit_disposition, recent_boot_count}`; window-bounded backoff input |
| PL-006a provenance marker | every spawn → every reap/kill | env var (Linux) / PGID (darwin) / tmux `harmonik-<project_hash>-` prefix; gates ALL kill/reset/remove in C1–C5 |
| PL-006d live-supervisor exclusion | sentinel+PID → C1/C4 reaping | `.harmonik/cognition/supervisor.sentinel` + `supervisor.pid` + `kill(pid,0)`; live ⇒ SKIP |

## Goal → component coverage

| Goal | Components |
|---|---|
| G1 remediating sweep (reset in_progress, kill flywheel tmux, remove worktrees) | C1 (worktrees), C2 (in_progress reset); flywheel-tmux kill already exists (area A) — verified, no new component |
| G2 reconcile dispatched on boot | C3 |
| G3 reap-on-exit | C4 (R1–R3) |
| G4 concurrent-spawn cap | C4 (R4–R6) |
| G5 restart backoff | C6 |
| G6 single-flywheel-per-project lock | C5 |
| G7 observability of remediation | C1-R4, C2-R6, C3-R5, C4-R5, C5-R2, C6-R5 (counts/events across all) |

Every goal maps to ≥1 component requirement; every requirement traces to a goal + bead + analysis gap. No requirement exists without a goal anchor.

## Review note

Per the jig's Review Criteria this decomposition should be checked by an independent reviewer (every goal→component mapped; no untraceable requirements; requirements concrete/testable). In this delegated single-pass run the coverage table above is the self-check; a fresh-context reviewer pass is recommended at the Change-Spec gate per the project review-gate rule. Proceeding to Research.
