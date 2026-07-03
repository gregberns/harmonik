# C4 — Reap-on-exit + concurrent-spawn cap — Change Spec

**Component:** When the supervisor/daemon dies, kill its provenance-marked child `claude`+tmux tree (reap-on-exit); and enforce a hard upper bound on concurrently-spawned child sessions (spawn cap) that refuses the (cap+1)th spawn.
**Bead:** hk-xb5yi · **Goals:** G3, G4 · **Analysis gaps:** #4, #5
**Spec home:** `specs/process-lifecycle.md` §4.5 PL-019 (new sub-clause **PL-019i** reap-on-exit) + §4.5 PL-014 (new sub-clause **PL-014b** spawn cap); `specs/event-model.md` §8.7 (new `spawn_cap_exceeded` event).

---

## Requirements (carried forward from 03-components.md)

- **C4-R1** (reap-on-exit, supervisor) — When `Supervisor.Run` returns for any reason AND on `supervise stop`, the process MUST enumerate and kill (SIGTERM→bounded-wait→SIGKILL) every `claude` process and tmux session bearing THIS project's PL-006a provenance marker that it spawned, before returning. A child lacking the marker MUST NOT be killed.
- **C4-R2** (live-supervisor exclusion) — Reap-on-exit MUST honor PL-006d: it MUST NOT kill the live flywheel/coordinator session of a DIFFERENT live supervisor (sentinel + `kill(pid,0)`); it reaps only its own descendants.
- **C4-R3** (daemon side) — The daemon's spawned `claude` sessions are reaped by the next-boot orphan sweep AND, additionally, by a best-effort kill on the daemon's graceful-shutdown path (PL-011 drain). SIGKILL of the daemon relies on the next-boot sweep; documented, not a new requirement.
- **C4-R4** (spawn cap) — The daemon MUST enforce a hard maximum number of concurrently-live spawned child `claude` sessions (a safety ceiling, distinct from the operator-set `--max-concurrent` capacity gate at EM-049). The cap MUST have a finite default.
- **C4-R5** (spawn-cap refusal) — An attempt to spawn the (cap+1)th concurrent child MUST be REFUSED — the daemon MUST NOT launch it — and MUST emit a typed signal (`spawn_cap_exceeded`) rather than spawning silently.
- **C4-R6** (spawn-cap accounting) — The live-child count MUST decrement on child exit (watcher-goroutine reap per PL-014 / [handler-contract.md §4.3 HC-011] — the watcher-ownership + Wait/reap contract) so the cap is a live gauge, not a monotonic counter.
- **C4-R7** — Tests MUST cover: (i) supervisor exit kills its provenance-marked children, leaves unmarked ones; (ii) reap-on-exit skips a different live supervisor's session; (iii) (cap+1)th spawn is refused with the typed signal; (iv) child exit frees a cap slot.

## Research summary (from 04-research/C4)

The supervisor terminates only its DIRECT child (`buildCmd` sets `Setpgid:true`, `terminateChild` does SIGTERM→wait→SIGKILL, `supervisor.go:330-366`); tmux-substrate-hosted `claude` panes are NOT in the supervisor's PGID, so PGID-forwarded SIGTERM never reaches them — the incident's "8 live claude + a pi still spending" after the supervisor died. All kill/enumerate primitives already exist and are reusable on the exit path: tmux session enumeration+kill by `harmonik-<project_hash>-` prefix (`internal/lifecycle/tmux/orphansession.go:12-21`), handler/`br` subprocess kill (`SweepOrphanHandlers`/`SweepOrphanBr`), the PL-006a marker, and the PL-006d live-supervisor sentinel exclusion (`orphansweepactiveheld_pl007_test.go`). So C4-R1/R2 is "call the existing sweep primitives on a new trigger (the `Run`-return defer + `stop.go`), scoped to this project's set, skipping a live supervisor sentinel." For the spawn cap: the daemon spawns N child `claude` via the workloop gated ONLY by the operator-set `--max-concurrent` (EM-049, `workloop.go:478,595`); `RunRegistry.Len()` (`runregistry.go:79,94,102,117`) IS the live-child gauge (increment on `Register` at spawn, decrement on `Unregister` at watcher reap — a live gauge, exactly C4-R6). There is NO hard safety ceiling independent of `--max-concurrent`. Two flagged investigations carried into this spec as DECISIONS: (a) reviewer/resume spawns may bypass `RunRegistry` (`CreateReviewerWorktree` is a separate path) — the cap must count them; (b) at-cap behavior must be REFUSE+emit (not the at-max-concurrent sleep+retry). C4 couples to C5: reap-on-exit safety depends on the single-flywheel invariant (at most one daemon per project via the pidfile lock).

## Approach

Two independent additions sharing the PL-006a/PL-006d primitives.

### Part 1 — Reap-on-exit (PL-019i)

- **Trigger sites:** (i) a `defer` at the top of `Supervisor.Run` that fires on EVERY return path (ctx-cancel, Stop, crash-loop-return at `supervisor.go:279-296`, clean exit); (ii) `cmd/harmonik/supervise/stop.go` after the supervisor-PID kill.
- **Action:** enumerate this project's `harmonik-<project_hash>-` tmux sessions + provenance-marked `claude`/`br` subprocesses (reuse `SweepOrphanTmuxSessions` + `SweepOrphanHandlers` + `SweepOrphanBr`) and kill them SIGTERM→bounded-wait(5s, HC-018)→SIGKILL.
- **Scope decision (research RQ3):** **Option A — reuse the project-prefix sweep on exit**, relying on C5's single-flywheel invariant for safety (under the pidfile lock there is at most one daemon per project, so the prefix sweep cannot hit a sibling daemon's healthy children). The precise in-supervisor run_id tracking (Option B) is rejected because the in-memory set is lost on the SIGKILL-of-supervisor case that matters most.
- **Live-supervisor exclusion (C4-R2):** before killing the `harmonik-<project_hash>-flywheel` session, the reap MUST apply the PL-006d sentinel + `kill(pid,0)` check; a LIVE different supervisor's session is SKIPPED (incremented into `coordinator_sessions_skipped`). Reap-on-exit reaps only orphaned coordinator sessions, never a live one.
- **SIGKILL boundary (C4-R3):** the supervisor's own SIGKILL is uninterceptable; that case is covered by the next-boot orphan sweep (area A, already wired). The daemon's graceful-shutdown path (PL-011 drain) gets a best-effort reap of its spawned `claude`/tmux on the way down, closing the live-spend window between death and next boot. This is documented as relying-on-next-boot for SIGKILL, not a new requirement.

### Part 2 — Concurrent-spawn cap (PL-014b)

- **Gauge:** extend the live-child accounting. `RunRegistry.Len()` is the implementer-run gauge; the cap MUST count ALL spawned `claude` (implementer + reviewer + resume). **Decision (research RQ5/R1):** route reviewer/resume spawns through the same gauge — if `CreateReviewerWorktree`'s `claude` spawn bypasses `RunRegistry`, add a unified atomic live-child counter incremented at the OS-spawn boundary (the single point every `claude` launch passes) so the cap is a true blast-radius ceiling, not just an implementer-run ceiling.
- **Cap value:** a finite default `SPAWN_CAP` (recommend default tied to a small multiple of the per-daemon ceiling, e.g. `min(2 × PL-014a-derived-ceiling, HARD_FLOOR)` with a concrete `HARD_FLOOR` such as 16) — operator-overridable via config/env. The cap is a SAFETY ceiling, semantically distinct from `--max-concurrent` (operator throughput knob, EM-049) and from `credfence`'s per-day-USD/max-runs spend meter (cumulative cost limit). They compose: cap bounds INSTANTANEOUS fan-out; budget bounds CUMULATIVE spend; `--max-concurrent` throttles within the cap.
- **Enforcement (research RQ5/R2, the refuse-vs-sleep decision):** at-cap behavior is **REFUSE + EMIT + DO-NOT-RETRY-SPAWN** — distinct from the at-`--max-concurrent` behavior which SLEEPS+retries. When a spawn would exceed `SPAWN_CAP`, the daemon MUST NOT launch the child; it MUST emit `spawn_cap_exceeded{cap, live_count, run_id?, bead_id?}` and treat the dispatch as deferred (NOT a busy-spin; the item stays pending and is re-evaluated when a slot frees on a child exit, mirroring the `dispatch_deferred` shape but with the hard-ceiling semantics).
- **Decrement (C4-R6):** the live-child counter decrements on the watcher-goroutine reap (`Unregister` / `cmd.Wait()` per PL-014/HC-011) so the cap is a live gauge; a child exit frees a slot and re-enables dispatch.

### Spec text to add — PL-014b (process-lifecycle.md §4.5, after PL-014a)

> **PL-014b — Concurrent-spawn safety cap**
>
> In addition to the per-daemon concurrency ceiling of §PL-014a (an FD-budget-derived throughput ceiling) and the operator-set `--max-concurrent` capacity gate ([execution-model.md EM-049]), the daemon MUST enforce a hard SAFETY cap on the number of concurrently-LIVE spawned child `claude` sessions — a blast-radius ceiling that bounds instantaneous fan-out so a defect (e.g., a crash-and-re-pull loop, a runaway dispatch) cannot launch unbounded paid sessions. The cap MUST have a finite default and MUST be operator-configurable via the daemon config / environment.
>
> The cap MUST count ALL spawned `claude` sessions — implementer runs, reviewer runs, and resume sessions — not only `RunRegistry`-tracked implementer runs; a spawn path that bypasses `RunRegistry` (e.g., reviewer-worktree creation) MUST still be counted against the cap (a unified live-child counter incremented at the OS-spawn boundary). The live count MUST be a LIVE gauge: incremented at spawn, decremented when the child exits (the watcher-goroutine reap per §PL-014 / [handler-contract.md §4.3 HC-011] — the daemon owns exactly one watcher goroutine per session and that watcher's `Wait`/reap is the decrement point; HC-024a covers the socket-EOF variant); it MUST NOT be a monotonic counter.
>
> When a spawn would bring the live count above the cap, the daemon MUST REFUSE the spawn — it MUST NOT launch the child — and MUST emit `spawn_cap_exceeded` (per [event-model.md §8.7]) carrying `{cap, live_count, run_id?, bead_id?}`. Refusal is distinct from the §PL-014a / EM-049 at-capacity behavior (which sleeps and retries): a refused spawn's work item is deferred without launching, and re-evaluated when a child exit frees a slot. The daemon MUST NOT busy-spin on a refused spawn.
>
> The cap is independent of and composes with: the `--max-concurrent` throughput throttle (which operates BELOW the cap), the §PL-014a FD-budget ceiling, and the credential/spend governance of `credfence` (the per-day-USD / max-runs cumulative spend meter — a different gate that reap does NOT own). The cap bounds CONCURRENT live children; the spend meter bounds CUMULATIVE cost.
>
> Tags: mechanism
> Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

### Spec text to add — PL-019i (process-lifecycle.md §4.5 PL-019, new lettered clause)

> **(i) Reap-on-exit.** When the supervisor process exits via any interceptable path — `Supervisor.Run` returning (ctx-cancel, `Stop`, crash-loop termination, or clean exit) and on `harmonik supervise stop` — the process MUST enumerate and kill every `claude` process and tmux session bearing THIS project's provenance marker (per PL-006a: the `harmonik-<project_hash>-` tmux prefix and the env/PGID subprocess marker) that it is responsible for, BEFORE returning. The kill MUST use SIGTERM followed by SIGKILL after a bounded interval (5s per [handler-contract.md §4.4 HC-018]). A process or session lacking this project's provenance marker MUST NOT be killed.
>
> Reap-on-exit MUST honor the live-supervisor exclusion of §PL-006d: it MUST NOT kill the live flywheel/coordinator session (`harmonik-<project_hash>-flywheel`) of a DIFFERENT live supervisor (sentinel present + recorded PID responds to `kill(pid, 0)`); such a session is SKIPPED. Reap-on-exit reaps only its own orphaned descendants. The safety of reusing the project-prefix enumeration on the exit path relies on the single-flywheel-per-project invariant of §PL-019(c) and the daemon pidfile-lock invariant of §PL-002 (at most one daemon + at most one supervisor per project), so the prefix sweep cannot reach a sibling owner's healthy children.
>
> The DAEMON's spawned implementer/reviewer `claude` sessions are reaped by (1) the next-boot orphan sweep of §PL-006 (the authoritative reaper, covering the uninterceptable SIGKILL case) AND (2) a best-effort reap on the daemon's graceful-shutdown drain path (§PL-011) that kills the daemon's spawned `claude`/tmux on the way down, closing the live-spend window between a clean death and the next boot. SIGKILL of the daemon is uninterceptable and relies solely on the next-boot sweep; this is the documented boundary, not a separate requirement.

### Spec text to add — event-model.md §8.7

Add `spawn_cap_exceeded` (class O, daemon-core, observability+audit) with payload `{cap, live_count, run_id?, bead_id?, refused_at}` to the §8.7 daemon-lifecycle event taxonomy (next free §8.7.x number). N-1 tolerated.

## Files & changes

| File | Change | Why |
|---|---|---|
| `specs/process-lifecycle.md` | Add PL-014b + PL-019(i); changelog row. | Normative spawn-cap + reap-on-exit. |
| `specs/event-model.md` | Add `spawn_cap_exceeded` §8.7.x row. | New event registration. |
| `internal/supervise/supervisor.go` | Add a `defer reapOnExit()` at the top of `Run` (fires on all returns); reapOnExit enumerates + kills project-prefix tmux/subprocesses honoring PL-006d. | Reap trigger (supervisor). |
| `cmd/harmonik/supervise/stop.go` | After the supervisor-PID kill, invoke the same reap (project-prefix sweep + PL-006d skip). | Reap trigger (stop verb). |
| `internal/daemon/daemon.go` (PL-011 drain) | Best-effort reap of spawned `claude`/tmux on graceful shutdown. | Close the clean-death window (C4-R3). |
| `internal/daemon/workloop.go` (~line 595 spawn site) | Add the `SPAWN_CAP` gauge check alongside the `>= maxConcurrent` check; refuse+emit `spawn_cap_exceeded` when over cap; do-not-retry-spawn. | Spawn-cap enforcement. |
| `internal/daemon/runregistry.go` (or new `spawncounter.go`) | Unified atomic live-child counter incremented at the OS-spawn boundary (implementer+reviewer+resume), decremented on watcher reap. | Live gauge covering all spawn paths. |
| `internal/workspace/createworktree.go` (reviewer spawn) | Increment/decrement the unified live-child counter on reviewer `claude` spawn/exit. | Cap counts reviewer spawns. |
| `core/...` | `SpawnCapExceededPayload`. | Event payload. |

## Acceptance criteria

- **AC1 (C4-R7 i):** A supervisor with two spawned `claude` panes (one provenance-marked for this project, one marked for a DIFFERENT project) exits → the marked-for-this-project pane is killed, the other-project pane survives.
- **AC2 (C4-R7 ii / C4-R2):** Reap-on-exit with a LIVE different supervisor's `harmonik-<project_hash>-flywheel` session present (sentinel + live PID) → that session is SKIPPED, not killed.
- **AC3 (C4-R3):** On `harmonik supervise stop`, all this-supervisor's `claude`/tmux children are gone after the command returns.
- **AC4 (C4-R3 daemon drain):** On daemon graceful shutdown, the daemon's spawned `claude`/tmux are best-effort killed; on SIGKILL they are reaped by the next-boot sweep (assert via a follow-on boot).
- **AC5 (C4-R7 iii / C4-R5):** With `SPAWN_CAP=C` and C live children, the (C+1)th spawn is REFUSED (no `claude` launched), and `spawn_cap_exceeded{cap=C, live_count=C}` is emitted; no busy-spin.
- **AC6 (C4-R7 iv / C4-R6):** After a live child exits, the gauge decrements and a previously-refused spawn becomes launchable.
- **AC7 (reviewer/resume counted):** A reviewer-worktree `claude` spawn counts against the cap (the cap is reached by implementer+reviewer combined).
- **AC8 (cap ≠ max-concurrent):** With `--max-concurrent` LOWER than `SPAWN_CAP`, throughput is throttled by max-concurrent (sleep+retry); with `--max-concurrent` HIGHER, the cap refuses+emits at C — the two behaviors are observably distinct.

## Verification

```bash
go test ./internal/supervise/... -run 'Run|Stop|ReapOnExit|Crash' -count=1
go test ./internal/daemon/...    -run 'SpawnCap|Workloop|RunRegistry|Reap|Drain' -count=1
go test ./internal/lifecycle/tmux/... -run 'OrphanSession|Provenance' -count=1
```

Manual: start a supervisor + daemon spawning `claude` panes; `kill -TERM` the supervisor; confirm `tmux ls` shows no `harmonik-<project_hash>-` sessions and `pgrep claude` shows none for this project. Set `SPAWN_CAP=1`, dispatch 2 beads, confirm one is refused with `spawn_cap_exceeded`.

## Error handling & edge cases

- **Reap during reap** (re-entrant) — the `defer` fires once per `Run`; `stop.go`'s reap and the defer are idempotent (killing an already-dead session is a no-op).
- **PGID-escape handlers (OQ-PL-011)** — a handler that internally `setsid`s escapes the marker and cannot be reaped; out of conformance, documented hazard, same as the existing sweep.
- **Cap counter leak** — a spawned child whose watcher goroutine dies without `Unregister` would leak a slot; the watcher-reap (HC-011 / HC-011a panic-recovery, which converts a wedged/panicked watcher to `agent_failed` and still reaps) + the next-boot gauge reset (the counter is in-memory, reset on boot) bound this.
- **Cap vs deferred dispatch** — a refused spawn must NOT mark the bead `needs-attention` (that is a review-loop termination, not a spawn refusal); the work item stays pending and re-dispatches on slot-free.

## Migration / backwards compatibility

Additive. Default `SPAWN_CAP` is finite; an operator who needs higher fan-out raises it explicitly. Reap-on-exit only kills provenance-marked descendants, so it cannot regress a multi-project machine. The `spawn_cap_exceeded` event is N-1 tolerated.

## Test beads

- **Scenario:** AC5/AC6 (spawn-cap refuse + slot-free) IS the scenario test. CLI under test: `harmonik daemon` with `SPAWN_CAP` set. Lifecycle state: cap reached, (cap+1)th dispatch attempted. Observable terminal condition: `spawn_cap_exceeded` event in `.harmonik/events/events.jsonl` + no `claude` process spawned for the refused run.
- **Exploratory:** AC3 (`harmonik supervise stop` reaps the child tree) IS the exploratory test — operator-invoked command, observable side-effect: no `harmonik-<project_hash>-` tmux sessions remain.
- See the shared test-bead block in `C6-boot-backoff-spec.md` §Test beads for the filed bead IDs.
