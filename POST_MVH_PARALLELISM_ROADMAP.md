# Post-MVH Parallelism Roadmap

**Scope**: after MVH rows 1–11 are green (one bead, one worktree, one run at a time), the first optimization is concurrent throughput. This document is the plan for that phase. Not features. Not polish. Throughput.

---

## 1. What Blocks Parallelism Today

Audited against `60b6024`. Citations are absolute paths.

**1a. Single JSONL writer per daemon, serialized by a mutex.**
`internal/eventbus/jsonlwriter.go:29` — `JSONLWriter` holds a `sync.Mutex`. One writer is opened at `daemon.Start` and passed to the bus. Concurrent goroutines emitting high-frequency events queue behind this mutex. Not a correctness blocker (O_APPEND guarantees no interleave within PIPE_BUF) but a latency concentrator for F-class (fsync-boundary) events — each `fsync(2)` from one run's `run_started` stalls all other Emit callers. Under two concurrent runs this is a throughput hazard on the critical path.

**1b. `AdapterRegistry` has no concurrency guard.**
`internal/handlercontract/adapterregistry_hc012.go:40-43` — `sealed` and `adapters` are guarded by no mutex. `ForAgent` reads `sealed` and writes it on first call. Under concurrent run goroutines calling `ForAgent` concurrently, this is a data race. The registry is intended to be sealed once at startup and then read-only, but the seal transition itself is not atomic.

**1c. `busimpl.subscriptions` Drain is not per-run-coordinated.**
`internal/eventbus/busimpl.go:50-51` — the `wg sync.WaitGroup` in `busImpl` is a single process-level counter. When two bead runs are live and both dispatch async/observer consumers, `Drain` waits for ALL in-flight goroutines across both runs, not just the calling run's. A slow consumer from run A delays shutdown of run B. Not a crash, but it destroys fair termination semantics.

**1d. No `run_id` partitioning on the JSONL event envelope.**
`internal/core/event.go` — the in-process `Event` struct carries `Type` and `Payload`; there is no `run_id` envelope field. Per-spec the file is single-per-project (EV-020). Two concurrent runs emit `run_started` events that look identical to a downstream reader unless they parse the payload body. Blocks per-run observability before the reader does deep parsing.

**1e. `daemon.Start` is a single linear goroutine; the work loop (row #10) is not yet wired.**
`internal/daemon/daemon.go:84` — `Start` runs synchronously to completion after emitting `daemon_started`. No goroutine pool, no scheduler, no in-flight run registry exists. Two concurrent runs require: (a) a goroutine per run, (b) a shared in-flight map protected by a mutex or sync.Map, (c) a polling loop that does not busy-spin. None of these are present.

**1f. `brcli.Adapter` SQLite contention serializes on `RunWithDBLockedRetry`.**
`internal/brcli/dblockretry.go` (referenced from `adapter.go:98`) — concurrent `ClaimBead` calls both shell out to `br update --claim`, writing to `.beads/beads.db`. The retry loop handles `BrDbLocked` exit codes per BI-025c. Under N concurrent claims this degrades to serial — correct, not crashing, but a throughput floor. At N=2 contention is minimal; at N>10 it becomes a scheduling bottleneck.

**Summary of real blockers:**

| # | Location | Risk | Verdict |
|---|---|---|---|
| A | `busimpl.go:50 wg` | Per-run Drain stalls on all runs' consumers | Fix before N>1 |
| B | `adapterregistry_hc012.go:40` | Data race on seal transition | Fix before N>1 |
| C | `jsonlwriter.go:29 mu + fsync` | Latency concentration on F-class | Fix before N>5 |
| D | `core/event.go` envelope has no `run_id` | Per-run filtering impossible | Fix for observability |
| E | `daemon.go:84` no goroutine pool, no run registry | Not wired at all | Foundation row |

---

## 2. Concurrency Model

**Recommendation: goroutine-per-active-bead inside one daemon process (model a).**

The daemon-per-project boundary is already locked in by the pidfile and socket paths in `internal/lifecycle/daemonpaths.go`. The question is whether concurrent beads run as goroutines inside that one daemon or as OS subprocesses.

STATUS.md's MVH-scope section: "All MVH code MUST be `run_id`-keyed, free of shared mutable state across runs." `run_id` keying is already in place — `workspace.WorktreePath` keys on `runID` (`worktreepath.go:98`), `brcli.terminalTransitionWrite` keys the idempotency key on `runID` (`terminaltransition_bi010.go:73`), the intent-log filename derives from the idempotency key. The foundation is goroutine-safe if the choke points above are fixed.

Subprocess-per-bead (model b) duplicates the bus, registry, JSONL writer, and brcli adapter per run, plus IPC overhead, for a feature (per-bead isolation) the worktree boundary already enforces (WM-003). More importantly, it pushes the "centralized controller" thesis toward Gas Town — multiple semi-autonomous processes each owning a slice of the queue. Explicitly against the user's standing position.

Conclusion: **model a** (goroutines). Fix the five blockers, add a run-registry map, add a goroutine per claimed bead. No re-architecture.

---

## 3. Ordered Work List

Rows in MVH_ROADMAP.md shape. Order is dependency-first: row N+1 can be dispatched after row N lands.

| # | Task | Where | Size | Unblocks |
|---|---|---|---|---|
| 1 | Add `run_id` field to `core.Event` envelope and propagate it through `busImpl.Emit` — every event carries the calling run's ID in the common envelope | `internal/core/event.go`, `internal/eventbus/busimpl.go` | S | D |
| 2 | Add `sync.RWMutex` to `AdapterRegistry`; guard `sealed` and `adapters` reads/writes with the RW pair so concurrent `ForAgent` calls are safe after startup sealing | `internal/handlercontract/adapterregistry_hc012.go` | XS | B |
| 3 | Split `busImpl.wg` into a per-subscription WaitGroup or a per-run-ID drainer; `Drain(ctx, runID)` signature addition — waits only for consumers tagged with that run | `internal/eventbus/busimpl.go` | M | A |
| 4 | In-flight run registry: `sync.Map` (or mutex-guarded map) in `daemon` package keyed by `run_id` → `*RunHandle`; `RunHandle` holds the watcher, worktree path, bead ID | `internal/daemon/runregistry.go` (new) | S | E |
| 5 | Work loop goroutine: replace single-shot `Start` with a polling loop that calls `ListBeadsByStatus("ready")`, claims first unclaimed, creates worktree, launches handler goroutine, registers in run registry; at most `cfg.MaxConcurrent` goroutines live simultaneously | `internal/daemon/workloop.go` (new) | L | E + #4 |
| 6 | `MaxConcurrent` field on `daemon.Config` default 1 (no behavioral change at MVH); next step sets it to N | `internal/daemon/daemon.go` | XS | #5 |
| 7 | Smoke test for N=2: two ready beads, `MaxConcurrent=2`, both close before `Start` returns; assert both events appear in JSONL with distinct `run_id` | `internal/daemon/` or `test/` | M | #5 + #1 |
| 8 | Bounded-worker bus: replace per-dispatch `go func()` in `busImpl` with shared worker pool (default 4 workers, operator-configurable per EV-014a note in `busimpl.go:39`); add `BusWorkerPoolSize` to `daemon.Config` | `busimpl.go`, `daemon.go` | M | C |
| 9 | Rate-limit `ClaimBead` concurrency: semaphore (buffered channel) in the work loop governing simultaneous `br update --claim` calls; prevents SQLite contention storm at N>5 | `workloop.go` | S | #5 |
| 10 | Per-run-id JSONL filter helper: `Filter(runID string)` on `JSONLWriter` returning an iterator over lines matching the given `run_id`; used by observability and tests | `jsonlwriter.go` | S | #1 |
| 11 | Throughput integration test: 10-bead queue at `MaxConcurrent=4`; assert all 10 close, wall-clock < 3× sequential baseline, no data races under `go test -race` | `test/` or `internal/daemon/` | M | #8 + #9 |

Row #5 (work loop) is the gate for all goroutine-per-bead behavior. Before it: preparatory. After it: hardening.

---

## 4. Throughput Metrics

- **Beads completed per minute** — primary throughput metric. `closed_bead_count / wall_seconds` over a rolling 60-second window. Emitted as `O`-class `daemon_throughput_sample` (new §8 row).
- **Claim-to-close wall time** — per-run latency, `run_started` → `run_completed`, keyed by `run_id`. P50/P95/P99 over last 100 runs. Aggregate by reading JSONL.
- **Worktree-create latency** — work-loop dispatch → `CreateWorktree` return. Payload field on `workspace_created`. P99 matters; `git worktree add` can stall on a large repo under index lock.
- **In-flight count** — `len(runRegistry)` at the moment a bead is claimed. Field on `run_started`. Saturation visibility.
- **SQLite lock retries per claim** — count of `BrDbLocked` retries per `ClaimBead`. Field on the claim event. Spikes signal contention.

**Where emitted:** all metrics are `O`-class events; they land in `events.jsonl`. Operator queries via `jq`. A future `harmonik stats` subcommand is out of scope.

---

## 5. What We Are NOT Doing in This Phase

- Multi-project (one daemon per project locked).
- Daemonization (`Start` stays foreground; no socket RPC, no detached process).
- Reconciliation (post-MVH by spec).
- Adapter rotation across Anthropic accounts (`ErrSingleAccountOnly` flag).
- ntm / tmux integration (gated on daemonization).
- Cross-machine centralized controller (Gas Town shape, against locked-in thesis).
- JSONL rotation (EV defers per OQ-EV-001).
- Operator pause/stop RPCs (signal-only at MVH).
- Workflow composition / node graphs (next scaling mechanism after N-concurrent-beads validated).

---

## 6. Anti-Patterns

**Do not introduce a worker-pool abstraction before the goroutine-per-bead shape is proven.** Row #8 adds a bounded worker pool to the bus — that is bus-internal, behind the existing `Emit` interface. The work loop itself should be one goroutine per active bead, not a pool fed by a queue of "work items." Conflating the two creates a nested-scheduler smell and makes the in-flight count opaque.

**Do not shard the JSONL file per run.** EV-020 mandates single-file-per-project. Sharding breaks observational replay, complicates dead-letter layout, and adds fd pressure. Key by `run_id` in the envelope (row #1).

**Do not add `MaxConcurrent` enforcement in the bus or the adapter.** The ceiling belongs in the work-loop scheduler (row #6). Pushing it down creates hidden coupling: the bus would need to know the scheduler's intent, which inverts the dependency.

**Do not use a global variable for the run registry.** The `sync.Map` must live as a field on a `Daemon` struct passed into `Start`. Package-level globals break concurrent tests and contradict the `run_id`-keyed mandate.

**Do not fix the `AdapterRegistry` race with a package-level `sync.Once`.** The registry is constructed per-`Start`. A package singleton would break concurrent tests. Use a per-instance `sync.RWMutex` on the struct (row #2).

**Do not launch the N=2 smoke test before rows #1 + #2 are merged.** Concurrent runs with a racy registry under `-race` produce non-deterministic failures that are hard to attribute. Race detector must be clean before any concurrent test is called a passing gate.

---

### Critical files

- `internal/daemon/daemon.go`
- `internal/eventbus/busimpl.go`
- `internal/core/event.go`
- `internal/handlercontract/adapterregistry_hc012.go`
- `internal/brcli/terminaltransition_bi010.go`
