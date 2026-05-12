# MVH Reality Check

## Verdict

The codebase is significantly further from a demoable "add task → start daemon → watch it run" state than MVH_ROADMAP.md implies. The roadmap identifies 4 beads on the critical path (redaction wiring) and declares "these are the last normative gap." That is false at the demo-path level. Those 4 beads fix correctness of `Emit` (secrets-in-log), but `Emit` being correct does not make the daemon do anything. The daemon's `Start()` function currently constructs a `RedactionRegistry` and an `EventBus`, discards both (`_ = eventbus.NewBusImplWithRegistry(registry)`), and returns `0`. The binary builds, runs, exits immediately with code 0, and prints nothing. There is no main loop, no scheduler, no socket listener, no pidfile acquisition, no bead polling, no handler launch, no workspace creation wired at the composition root. The lifecycle package has all the individual building blocks (pidfile lock, orphan sweep, socket test fixtures, work queues, watcher), but none of them are called from `daemon.Start` or from `main`. The estimate from the roadmap — "4 implementer dispatches, sequential" — describes the time to fix `Emit` correctness, not the time to reach a runnable demo. A conservative estimate for the demo path is 3–5 weeks of focused implementation work.

---

## What works today (verified by reading or running)

- **Binary builds and passes `go build ./...` clean.** `cmd/harmonik/main.go` compiles. (`go build -v ./cmd/harmonik`)
- **All tests pass.** `go test ./...` — 15 packages, all green. (`go test ./...`)
- **`go run ./cmd/harmonik` exits 0.** No output, no error, immediate exit. Confirms the binary is a no-op at runtime.
- **EventBus (`internal/eventbus/busimpl.go`).** `NewBusImplWithRegistry`, `Emit`, `Subscribe`, `Seal`, `Drain` all implemented and tested. The JSONL append step inside `Emit` is an admitted stub (step 4 comment: "file path not yet threaded through daemon.Config").
- **JSONLWriter (`internal/eventbus/jsonlwriter.go`).** `OpenJSONLWriter`, `Append` (with fsync flag), `Close` all implemented. Not called from `busimpl.Emit` yet.
- **RedactionRegistry + middleware (`internal/handlercontract/redaction.go`, `redactionregistry.go`).** Implemented, wired at `daemon.Start` (hk-8i31.83 closed).
- **`brcli.Adapter`** — `ClaimBead`, `CloseBead`, `ReopenBead`, `ListBeadsByStatus`, `ShowBead` all implemented and invoke `br` subprocess. (`internal/brcli/terminaltransition_bi010.go`, `listbystatus_em031a.go`)
- **`workspace.CreateWorktree`** — calls `git worktree add -b` and is implemented. (`internal/workspace/createworktree.go:60`)
- **`lifecycle.AcquirePidfile`** — implemented. (`internal/lifecycle/pidfile.go:88`)
- **`lifecycle.WaitOwner`** — subprocess wait-owner pattern implemented. (`internal/lifecycle/spawnwait_pl014.go`)
- **`lifecycle.SpawnChildSysProcAttr`** — subprocess pgid wiring implemented. (`internal/lifecycle/spawndaemonchild_darwin.go`)
- **`handlercontract.Watcher` + `SpawnWatcher`** — the per-session NDJSON read-loop is fully implemented. (`internal/handlercontract/watcher_hc011.go:259`)
- **`handlercontract.AdapterRegistry`** — registry for per-agent-type adapters implemented. (`internal/handlercontract/adapterregistry_hc012.go`)
- **`WorkQueueSet`** — per-role buffered work channels implemented. (`internal/handlercontract/workqueue.go`)
- **`lifecycle.ReadyCriteria`** — struct and `Met()` logic implemented. (`internal/lifecycle/readystate_pl009.go`)
- **`lifecycle.StartupState`** — orphan-sweep flag tracking implemented.

---

## Concrete runtime gaps

| Gap | Where | What's needed | Existing bead? | Est. size |
|---|---|---|---|---|
| `daemon.Start` is a no-op | `internal/daemon/daemon.go:40–53` | Wire all PL-005 steps: pidfile lock (`AcquirePidfile`), emit `daemon_started`, orphan sweep, socket bind, Cat 0 pre-check, git walk, bead query, in-memory model build, reconciliation dispatch, `daemon_ready` emission, then enter the main work loop | hk-8mup (open parent epic) | L |
| No main work loop / scheduler | Absent from codebase entirely | A goroutine that polls `br list --status ready`, claims each bead, dispatches a handler, and waits for completion; must call `brcli.ClaimBead`, `workspace.CreateWorktree`, handler `Launch`, then `brcli.CloseBead` | NONE | L |
| No concrete `Handler.Launch` implementation | `internal/handlercontract/handler_hc001.go` defines the interface; `internal/handler/` has only path-resolution and twin-launch verification utilities | A concrete type implementing `Handler` that calls `exec.CommandContext` to start `claude` (or the twin binary) with a `LaunchSpec`, connects progress stream stdout, and returns a `Session` | NONE | M |
| No `Session` implementation | `internal/handlercontract/session.go` defines the interface | Concrete `Session` wrapping `exec.Cmd` + `WaitOwner`, implementing `SendInput`, `Kill`, `Wait`, `Outcome` | NONE | M |
| No socket listener (PL-003) | `net.Listen` exists only in test fixtures (`lifecycle/testfixture_test.go:272`) | Production `net.Listen("unix", sockPath)` + request handler loop for `emit-outcome`, `claim-next`, `dispatch-status` agent wire-protocol messages | NONE | M |
| JSONL path not threaded into busimpl | `internal/eventbus/busimpl.go:114–116` ("deferred stub") | Pass JSONL file path through `daemon.Config`, open `JSONLWriter` in `daemon.Start`, inject it into `busimpl`; Emit calls `w.Append(redactedBytes, sync)` at step 4 | hk-8mup.63 (open, P2) | S |
| No `DeadLetterSink` implementation | `internal/handlercontract/watcher_hc011.go:62` defines the interface; `SpawnWatcher` panics on nil | A concrete type appending to `.harmonik/events/dead-letters.jsonl` | NONE | XS |
| No pidfile acquisition in `main` or `daemon.Start` | `lifecycle.AcquirePidfile` exists but is never called from production paths | Call `AcquirePidfile` in `daemon.Start` before socket bind | NONE | XS |
| No orphan sweep invoked at startup | `lifecycle.RunOrphanSweep` (tests reference it); not called from `daemon.Start` | Wire `RunOrphanSweep` into PL-005 step 3 in `daemon.Start` | NONE | S |
| No `daemon_started` event emission | Event type defined in `core/daemonevents_hqwn59.go`; never emitted from `daemon.Start` | Emit `daemon_started` payload after bus is sealed (PL-005 step 2) | NONE | XS |
| `daemon.Config` has no project dir, JSONL path, or socket path | `internal/daemon/daemon.go:18–22` | Add `ProjectDir string` (at minimum) to `daemon.Config`; all per-project paths derive from it | NONE | XS |
| `main.go` constructs nothing | `cmd/harmonik/main.go:run()` builds `policyEngine`, discards it, returns 0 | Call `daemon.Start(cfg)` with a real `Config` (projectDir from argv or cwd) | NONE | XS |
| No `Adapter` implementation for `claude-code` agent type | `AdapterRegistry` exists; no registered adapter | Concrete `ClaudeCodeAdapter` implementing `handlercontract.Adapter` (`DetectReady`, `DetectRateLimit`, `CleanExitSequence`, `RotateAccount`) | NONE | M |

---

## Suggested order of attack

1. **Fix `daemon.Config` and `main.go`** — add `ProjectDir string` to `Config`; have `main.go` read `os.Getwd()` (or a `--project` flag) and call `daemon.Start(cfg)`. Without this, nothing downstream can reference the `.harmonik/` directory. (~30 min)

2. **Wire JSONL path into busimpl (hk-8mup.63)** — thread the log file path through `daemon.Config`, open `JSONLWriter` in `daemon.Start`, pass it to `busimpl`. This is the only open non-post-mvh non-rollup bead; close it first to get durable event output before anything else. (~1 hr)

3. **Add `daemon_started` emission + pidfile acquisition** — call `AcquirePidfile` (single-instance guard), then call `bus.Emit(ctx, "daemon_started", ...)`. These are the first observable side effects. (~1 hr)

4. **Wire socket listener** — add `net.Listen("unix", sockPath)` production code (the pattern exists in `testfixture_test.go`); create a minimal request handler that accepts `emit-outcome` from agent subprocesses. (~2–3 hrs)

5. **Implement concrete `Session` and `Handler.Launch`** — create a `claudeCodeSession` wrapping `exec.Cmd`+`WaitOwner`+progress-stream pipe, and a `claudeCodeHandler.Launch()` that calls `exec.CommandContext`. Wire `SpawnWatcher` to the progress stream immediately after launch. (~4–6 hrs)

6. **Implement `DeadLetterSink`** — a one-file JSONL appender. (~30 min)

7. **Build the main work loop** — a goroutine in `daemon.Start` that calls `brcli.ListBeadsByStatus("ready")`, iterates results, calls `CreateWorktree`, calls `handler.Launch`, calls `SpawnWatcher`, awaits `Watcher.Done`, then calls `brcli.CloseBead` or `ReopenBead`. This is the highest-complexity step. (~1–2 days)

8. **Wire orphan sweep** — call `lifecycle.RunOrphanSweep` at PL-005 step 3 before socket bind. (~1 hr)

"Add task → run → processes" requires completing all 8 steps.

---

## What the bead corpus has wrong

The MVH_ROADMAP.md critical-path section is narrowly accurate (those 4 beads do gate correct `Emit` semantics) but its "Worked Example" (steps 4–5) invokes functionality — "daemon finds the ready bead via its Beads-CLI skill integration", "daemon launches a Claude Code sub-agent against a task branch" — for which no wiring exists at `daemon.Start`. The roadmap's implicit claim that closing 4 beads reaches MVH is false.

The one surviving non-rollup open bead without `post-mvh`:

| Bead | Current label | Should be MVH because... |
|---|---|---|
| hk-8mup.63 | P2/open, no `post-mvh` | JSONL path threading is a prerequisite for any durable event output; without it every `Emit` silently discards to the stub. |

No `post-mvh` beads appear to cover the main work loop, the concrete `Handler.Launch`, or the socket listener. These gaps have no beads at all — they are not labeled incorrectly, they simply don't exist in the corpus.

---

## What I tried to run, what happened

```
$ go build ./...
(no output — clean build)

$ go vet ./...
(no output — clean vet)

$ go run ./cmd/harmonik
(no output, exits 0 immediately)

$ go build -v ./cmd/harmonik
github.com/gregberns/harmonik/cmd/harmonik

$ go test ./...
ok  github.com/gregberns/harmonik/internal/brcli     71.172s
ok  github.com/gregberns/harmonik/internal/core      1.627s
ok  github.com/gregberns/harmonik/internal/daemon    (cached)
ok  github.com/gregberns/harmonik/internal/eventbus  1.127s
ok  github.com/gregberns/harmonik/internal/handler   4.225s
ok  github.com/gregberns/harmonik/internal/handlercontract  2.649s
ok  github.com/gregberns/harmonik/internal/lifecycle 18.739s
ok  github.com/gregberns/harmonik/internal/operatornfr  1.904s
ok  github.com/gregberns/harmonik/internal/workspace 31.947s
(+ 6 more — all green)

$ br list --status open --json | python3 -c "..."
Total open: 50
Open without post-mvh: 5  (4 are rollup epics; 1 is hk-8mup.63 JSONL wiring)
```

The daemon binary builds and runs. It does exactly nothing: `daemon.Start` creates two objects, discards them, and returns nil. `main` calls `run()`, gets exit code 0. No file is written, no socket is opened, no bead is read. The codebase has high-quality individual components (brcli, workspace, watcher, workqueue, pidfile, orphan sweep) that are tested in isolation, but none of them are connected to each other or to the entry point.
