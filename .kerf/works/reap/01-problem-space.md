# reap — Problem Space

**Work:** reap (Crash recovery & orphan remediation)
**Jig:** plan (v1)
**Source of truth:** `docs/flywheel/2026-05-30-lifecycle-feasibility-and-gaps.md` (the 2026-05-30 credit-burn incident assessment).
**Relates to:** hk-iuaed (imrest — post-mvh orphan-reset deferral, lifted here into a real implementation).

## Summary

On 2026-05-30 a flywheel daemon crash-looped through **10 boots in a day** (the last five inside ~14 minutes). The 14:56 boot's orphan sweep *observed* 951 stale intents, 6 beads stuck `in_progress`, and 24 stale `*-flywheel` tmux sessions — and **remediated zero of them**, because the stale-`in_progress` reset is post-mvh-deferred (hk-iuaed) and the sweep never reaped orphaned flywheel tmux sessions or worktrees. When the dead daemon finally stopped, **8 `claude` sessions and a `pi` process were still alive and still spending**, and a *separate* live `harmonik-pi` Pi was doing the same work (dual-orchestrator collision). This work makes the orphan sweep **actually remediating**, adds **reap-on-exit + a concurrent-spawn cap** so a dead daemon kills its children, adds **daemon restart backoff** to throttle the crash-and-re-pull multiplier, and adds a **single-flywheel-per-project lock** to prevent dual orchestrators. It is the failure-mode hardening tier of the three-work incident response (`credfence` = safety prerequisite, `pilot` = lifecycle build-out, `reap` = crash hardening).

## Goals

1. **Remediating orphan sweep.** On daemon boot, the sweep MUST actually *reset* stale `in_progress` beads owned by this project's daemon back to `open` (lift hk-iuaed / imrest from post-mvh-deferral into a wired-by-default production path), *kill* orphaned `harmonik-<project_hash>-flywheel`-prefixed tmux sessions, and *remove* orphaned run-worktrees — not merely observe and count them.
2. **Reconcile dispatched queue items on boot.** On unclean exit / next boot, queue items left in `dispatched` whose run did not survive MUST be reconciled (re-queued as `pending` or terminally failed per the existing queue/reconciliation rules) rather than stranded as permanent phantom in-flight.
3. **Reap-on-exit.** When the supervisor/daemon process dies (clean or crash), its spawned child `claude` + tmux sessions MUST be killed — a dead orchestrator MUST NOT leave live billable children.
4. **Concurrent-spawn cap.** A daemon/supervisor MUST enforce a hard cap on the number of concurrently-spawned child sessions so a defect cannot fan out unbounded paid sessions; exceeding the cap is refused, not silently allowed.
5. **Restart backoff.** Repeated daemon boots MUST be throttled by an exponential backoff so a crash-and-re-pull loop cannot produce 10 boots/day; the backoff window persists across process death (a durable last-boot marker, not in-memory state).
6. **Single-flywheel-per-project lock.** At most one flywheel orchestrator (daemon + its Pi/supervisor) MUST run per project; a second start attempt MUST be refused with a clear diagnostic, preventing the dual-orchestrator collision.
7. **Observability.** Every remediation action MUST be counted on `daemon_orphan_sweep_completed` (extending the existing additive-tolerated payload) and/or emit a typed event, so an operator can audit what the boot reaped.

## Non-goals

- **Credential scrubbing & spend ceilings.** The API-key leak, the `os.Environ` passthrough, the daemon-side per-day-USD / max-runs budget, and the finite-default budget are `credfence`'s scope, NOT reap's. reap stops *live leaked children* mechanically (reap-on-exit + spawn cap); it does not own the credential boundary or the dollar meter.
- **The quiet/no-auto-pull daemon (S2), Pi-driven curated dispatch (S5/S6/S10), and pause/resume control (S9).** Those are `pilot`'s scope. reap does not change whether the daemon auto-pulls `br ready`; it only throttles *how often* a crashed daemon re-boots into that path (restart backoff) and prevents *two* of them running at once (single-flywheel lock).
- **Dry-run / plan-only daemon mode** (lost issue #10) — `pilot`.
- **Review-loop retry budget** (lost issue #7) — out of scope here; it is a spend-amplifier governed under `credfence`/`pilot`.
- **Re-architecting the in_progress claim semantics.** reap implements the imrest activity-marker reset (in_progress → open) but does NOT reopen the locked decision that close/reopen remain truth-claims; the BI-010d reset path already exists in spec.
- **New abstraction layers.** The BeadResetter / BeadCat3cCloser / ProvenanceChecker / MergeCommitScanner seams already exist in `internal/lifecycle/`; reap wires the production implementations into them, it does not invent a new sweep framework.

## Constraints

- **Spec-first.** Code matches the spec. The orphan-sweep contract lives in `specs/process-lifecycle.md` §4.5 (PL-006, PL-006a/b/c/d) and is already substantially written (stale-`in_progress` reset, Cat 3c close, coordinator-session exclusion PL-006d, additive event payload). reap's spec work is primarily (a) flipping the production wiring/default from "nil ⇒ skip" to "wired ⇒ remediate", and (b) adding the *new* normative contracts that have no spec home yet: reap-on-exit, concurrent-spawn cap, daemon-level restart backoff, and the single-flywheel-per-project lock. Those land in `specs/process-lifecycle.md` and/or `specs/execution-model.md`.
- **Provenance-gated, never blind.** Every kill/reset/remove MUST be gated on this project's PL-006a provenance marker (project_hash env var on Linux, PGID on darwin; tmux prefix `harmonik-<project_hash>-`). A candidate without a valid marker MUST NOT be touched. This is a hard safety invariant — the sweep runs against a multi-project machine.
- **Honor PL-006d.** The remediating tmux sweep MUST NOT kill a *live* coordinator/flywheel session owned by a running supervisor (sentinel + `kill(pid,0)` check). Reaping `*-flywheel` sessions means reaping *orphaned* ones (no live supervisor), not the live one.
- **Idempotent & crash-safe.** The bead-reset write is idempotency-keyed `<project_hash>:<bead_id>:reset:<daemon_start_ns>` and intent-logged per BI-030; re-running the sweep MUST NOT double-reset or corrupt state. Restart backoff and the single-flywheel lock MUST survive process crash (durable markers / fd-lifetime advisory locks that the kernel auto-releases, per PL-002a discipline).
- **MVH-layering reachability.** Today the reset path is unreachable in production because `ProvenanceChecker` is left nil and claim-intent provenance rules it out under MVH layering. reap MUST supply a production provenance signal (audit-log actor `project_hash`, OR the documented intent-log claim cross-reference) so the reset actually fires — closing the "observed 6, reset 0" gap.
- **darwin-first.** The dev/operator platform is darwin (no `/proc`); the PGID-based provenance fallback (OQ-PL-008) and `tmux list-panes` polling must work on macOS.
- **No regression of the live path.** The remediating sweep and reap-on-exit MUST NOT kill a *healthy* concurrent run or the operator's own Claude Code session; provenance + liveness checks are the guard.

## Success criteria (concrete, verifiable)

1. On boot, given N beads in `in_progress` owned by this project's prior daemon with no live run / no pending close-or-reopen intent / no merged commit, the sweep resets all N to `open` and `daemon_orphan_sweep_completed.bead_in_progress_reset == N` (the incident's "observed 6, reset 0" becomes "reset 6").
2. On boot, given M orphaned `harmonik-<project_hash>-flywheel` tmux sessions with **no** live supervisor (PL-006d sentinel absent or PID dead), the sweep kills all M; given a **live** supervisor session, the sweep skips it and increments `coordinator_sessions_skipped`.
3. On boot, given K orphaned run-worktrees (no live run, stale lease-lock per WM-013a), the sweep removes all K and the event payload reflects the count.
4. On boot, given a queue item in `dispatched` whose run did not survive the crash, the item is reconciled to a re-eligible (`pending`) or terminal (`failed`) state per the queue/reconciliation rules — never left permanently `dispatched`.
5. When the supervisor/daemon process is killed (SIGTERM, SIGKILL-then-next-boot, or clean stop), no spawned child `claude` process and no spawned tmux session bearing this project's provenance marker remains alive after the next boot's sweep completes (and reap-on-exit handles the interceptable cases before exit).
6. A daemon/supervisor configured with spawn cap C refuses to spawn the (C+1)th concurrent child and emits a typed cap-exceeded signal rather than spawning it.
7. Two rapid daemon boots within the backoff window: the second is delayed by the exponential backoff (verifiable via a durable last-boot timestamp and the boot-delay log/event); 10 boots in a fixed window is not reproducible under the default backoff.
8. A second `harmonik supervise start` (or daemon start) for the same project while one is live is refused with exit-code + diagnostic; the first keeps running undisturbed.
9. All of (1)–(8) are gated on PL-006a provenance: a resource from a *different* project's daemon present on the same machine is never reset/killed/removed (negative test).

## Affected spec areas

- **`specs/process-lifecycle.md`** — PRIMARY. §4.5 PL-006 (orphan sweep: stale-`in_progress` reset wiring + `*-flywheel` tmux/worktree reaping), PL-006a (provenance), PL-006b (BeadResetter production wiring), PL-006c (Cat 3c closer), PL-006d (coordinator exclusion); §PL-019 supervisor (reap-on-exit, spawn cap, supervisor lock vs single-flywheel lock); §PL-002 pidfile lock (single-flywheel-per-project); a NEW requirement for daemon-level restart backoff (durable last-boot marker).
- **`specs/execution-model.md`** — SECONDARY. The `dispatched`-item reconciliation on boot (EM-031a active-run reconstruction + queue-state reconcile), concurrent-spawn cap interaction with EM-049 capacity gate / `--max-concurrent`.
- **`specs/queue-model.md`** — referenced (QM dispatched→pending/failed reconciliation semantics); reap consumes the existing rules, may add the boot-time reconcile obligation cross-ref.
- **`specs/operator-nfr.md`** — referenced for exit-codes (single-flywheel-refused, spawn-cap-exceeded) and the durable state-marker file surface.
- **Code:** `internal/lifecycle/orphansweep*.go` (sweep wiring), `internal/daemon/` (orphan-sweep call site, `*-flywheel`/worktree reaping, dispatched reconcile), `internal/supervise/supervisor.go` + `cmd/harmonik/supervise/*` (reap-on-exit, spawn cap, single-flywheel lock, daemon restart backoff).

## Confirmation

User has delegated all decisions via the assessment doc + the desired 10-step lifecycle (HANDOFF-flywheel.md). The four attached beads (hk-9eury reconcile+reap, hk-xb5yi spawn-cap+reap-on-exit, hk-li14r single-flywheel-lock, hk-7t9g1 restart-backoff) define the scope and are treated as confirmed. Proceeding without further check-in per the delegation.
