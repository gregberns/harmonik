# MVH Roadmap

**Source of truth for "what needs to happen to ship MVH."** This file overrides the bead corpus when they disagree. Generated empirically by running and reading the code, not by querying bead labels.

## What MVH is

A single binary (`./harmonik`) you run in a project directory. It watches the bead ledger for tasks with status `ready`, picks one, creates a git worktree for it, spawns a Claude Code subprocess to do the work, watches the subprocess to completion, records the outcome to the bead, emits events to a JSONL log, and loops.

That's it. No daemonization, no concurrency across runs, no multi-project — those are post-MVH.

## What exists (verified `2026-05-12` at `60b6024`)

Individual building blocks, tested in isolation, **not wired together**:

| Surface | Where | Status |
|---|---|---|
| Composition root | `internal/daemon/daemon.go` | `Start()` constructs bus + registry, discards them, returns nil |
| Event bus | `internal/eventbus/busimpl.go` | Full `Emit` + sync/async/observer dispatch; JSONL append is a stub |
| JSONL writer | `internal/eventbus/jsonlwriter.go` | `Open`/`Append`/`Close` work; not connected to the bus |
| Redaction | `internal/handlercontract/redactionregistry.go` | HC-031 + HC-032 middleware wired through `Emit` |
| Bead CLI adapter | `internal/brcli/` | `ClaimBead`, `CloseBead`, `ListBeadsByStatus`, `ShowBead` shell out to `br` |
| Worktree create | `internal/workspace/createworktree.go` | Calls `git worktree add -b` |
| Pidfile lock | `internal/lifecycle/pidfile.go` | `AcquirePidfile` works |
| Subprocess pgid wiring | `internal/lifecycle/spawndaemonchild_darwin.go` | `SpawnChildSysProcAttr`, `WaitOwner` work |
| Orphan sweep | `internal/lifecycle/` | Code exists, never called from `Start` |
| Per-session watcher | `internal/handlercontract/watcher_hc011.go` | `Watcher` + `SpawnWatcher` (NDJSON read-loop) work |
| Per-role work queues | `internal/handlercontract/workqueue.go` | `WorkQueueSet` works |
| Ready-state criteria | `internal/lifecycle/readystate_pl009.go` | `ReadyCriteria.Met()` works |
| Adapter registry | `internal/handlercontract/adapterregistry_hc012.go` | Registry works, no concrete adapter registered |

`go build ./...` clean. `go test ./...` 15 packages green. `go run ./cmd/harmonik` exits 0 with no output.

## What's missing to make it run

Ordered by dispatch sequence. Each row is one implementer dispatch. Size: XS≤1h, S=1–3h, M=half-day, L=1–2 days.

| # | Task | Where | Size | Bead |
|---|---|---|---|---|
| 1 | Add `ProjectDir` to `daemon.Config`; `main.go` reads `os.Getwd()`/`--project`, calls `daemon.Start(cfg)` | `cmd/harmonik/main.go`, `internal/daemon/daemon.go` | XS | NEW |
| 2 | Open `JSONLWriter` in `daemon.Start`; inject into `busimpl`; `Emit` step 4 calls `Append(redacted, sync)` | `internal/daemon/`, `internal/eventbus/busimpl.go` | S | `hk-8mup.63` (filed) |
| 3 | Call `AcquirePidfile` in `Start`; emit `daemon_started` after bus is sealed | `internal/daemon/daemon.go` | XS | NEW |
| 4 | Wire `RunOrphanSweep` into `Start` (PL-005 step 3) before socket bind | `internal/daemon/daemon.go` | S | NEW |
| 5 | Production Unix socket listener — `net.Listen("unix", sockPath)` + request loop for `emit-outcome` / `claim-next` from agent subprocesses | `internal/daemon/` (new file) | M | NEW |
| 6 | Concrete `Session` wrapping `exec.Cmd` + `WaitOwner`: `SendInput`, `Kill`, `Wait`, `Outcome` | `internal/handler/` (new file) | M | NEW |
| 7 | Concrete `Handler.Launch` that `exec.CommandContext`s Claude Code (or twin) with a `LaunchSpec` and wires `SpawnWatcher` to the stdout pipe | `internal/handler/` (new file) | M | NEW |
| 8 | `ClaudeCodeAdapter` implementing `handlercontract.Adapter` (`DetectReady`, `DetectRateLimit`, `CleanExitSequence`, `RotateAccount`); register at `Start` | `internal/handler/` (new file) | M | NEW |
| 9 | `DeadLetterSink` — JSONL appender to `.harmonik/events/dead-letters.jsonl` | `internal/handlercontract/` (new file) | XS | NEW |
| 10 | **Main work loop** — goroutine in `Start`: poll `ListBeadsByStatus("ready")` → `ClaimBead` → `CreateWorktree` → `Launch` → `SpawnWatcher` → await `Watcher.Done` → `CloseBead` (or `ReopenBead` on failure) | `internal/daemon/` (new file) | L | NEW |
| 11 | Smoke test: end-to-end "one ready bead → workspace → claude-code stub run → bead closed → event appears in JSONL" | `test/` or `internal/daemon/` | M | NEW |

**Order matters.** 1–4 give observable side effects (file writes, event emissions) for debugging downstream work. 5–9 are the per-task machinery, parallelizable amongst each other once #1 is done. #10 is the integration point that consumes 5–9. #11 proves it.

Wall-time estimate at one implementer dispatch per row, with reasonable rebases: **2–3 weeks** of agent-clock if dispatches are clean; longer if integration #10 reveals interface mismatches.

## What we are NOT building for MVH

- Daemonization (detached process, signal handling beyond Ctrl-C). MVH stays foreground.
- Concurrent runs (one daemon per project, one in-flight task at a time).
- Reconciliation (covered by `internal/reconciliation/` plans, but its node-type lives post-MVH).
- Adapter rotation across multiple Anthropic accounts.
- Skill registry beyond a stub.
- Control-point policy registry beyond a stub.
- Spec-corpus sensors *unless* they're already failing — sensors are validation, not behavior.

## The bead corpus and why it's not the source of truth

`br ready --limit 0` shows 10 entries. 9 are either epics or `post-mvh`-labeled. Only **one** non-rollup non-`post-mvh` open bead exists: `hk-8mup.63` (JSONL wiring). The remaining 10 rows above have no bead at all. Filing beads for them is fine and traceable, but the corpus lagging this doc is the expected steady state — **update this doc first, then file beads if needed, not the other way around.**

