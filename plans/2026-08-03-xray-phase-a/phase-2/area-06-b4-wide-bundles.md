# Area 6 — B4: structs that are namespaces pretending to be values

**Class:** B4. **Phase:** B. **Detector:** `exact` (>12 fields). **Lock unit:** the declaring file *and*
every consumer — this is the widest lock in the directory. **80 findings.**
Data: `data/area-06-b4-wide-bundles.json`.

## The set, by width

```
  63  internal/keeper.WatcherConfig            internal/keeper/watcher.go:206
  57  internal/keeper.CyclerConfig             internal/keeper/cycle.go:97
  44  internal/projectconfig.KeeperConfig      internal/projectconfig/projectconfig.go:553
  40  harmonik.ResolvedKeeperConfig            cmd/harmonik/resolve_keeper_config.go:173
  40  internal/daemon.Config                   internal/daemon/daemon.go:49
  39  internal/codexdriver.codexSession        internal/codexdriver/session.go:65
  33  internal/projectconfig.KeeperConfigPresence  internal/projectconfig/projectconfig.go:504
  31  supervise.Config                         cmd/harmonik/supervise/config.go:70
  31  internal/harness/shared.LaunchCtx        internal/harness/shared/launchctx.go:33
  29  internal/handlercontract.RunCtx          internal/handlercontract/harness.go:86
  29  internal/runloop.RunEnv                  internal/runloop/ports.go:260
  29  internal/workflow/dot.Node               internal/workflow/dot/ast.go:109
  27  internal/daemon.dotSubWorkflowRunner     internal/daemon/sub_workflow_runner.go:118
  26  internal/keeper.CycleState               internal/keeper/step.go:164
  24  harmonik.KeeperFlags                     cmd/harmonik/resolve_keeper_config.go:134
  24  internal/daemon.agentLaunchInput         internal/daemon/agentlaunch.go:146
```

**`workLoopDeps` is gone.** The 81-field bundle across 21 files that the organism repo's split analysis
named as *the* single load-bearing tie in the god-package no longer exists at `d5a12348f` — the only
surviving mentions are three doc comments in `internal/runmerge` and `internal/runlaunch` describing what
was moved out of it. Harmonik dissolved its worst B4 instance on its own, without this toolchain. That is
worth two things: the analysis that named it was correct, and **any number in this project citing
`workLoopDeps` is now stale** and should be re-derived rather than repeated.

## Why it blocks decomposition

**It is not a value, it is a namespace.** Every function taking `WatcherConfig` appears to depend on all 63
fields, so the symbol graph records coupling that does not exist, and **no cut through it looks cheap**.
This is the class most directly responsible for the planner failing to find seams: it manufactures dense
false ties in exactly the places a cut would otherwise go.

It is also the reason area 7's extractions come out too wide. Catalog entry B5 — "extraction boundary
exceeds ~7 parameters" — is B4 read backwards: past that width an extracted function cannot be understood
alone, which is the only reason to extract it. **Splitting a bundle is what makes the extraction it blocks
possible.** That is the one cross-file precedence relation this project has confirmed twice and which the
scheduler still cannot express (area 8, item 3).

## What to do

**One bundle at a time. Do not fan out this class.** The lock is the declaring file plus every consumer,
consumers overlap heavily (three of the top four are keeper configuration), and two concurrent B4
operations on related types will conflict.

The fix is **split by consumer, not by theme**:

1. For the chosen type, enumerate every function that takes it and record which fields each one actually
   reads. This is mechanical and is the whole analysis.
2. Fields that are always used together become a type. Fields used by exactly one consumer move to that
   consumer's own parameter list.
3. Each caller takes only its group.

**Verify: no consumer receives a field it does not reference.** That is a checkable post-condition, not a
taste judgement, and it is the reason to prefer split-by-consumer over split-by-what-sounds-related.

### Where to start

**`internal/keeper` — `WatcherConfig` (63) and `CyclerConfig` (57).** Two bundles, one package, and the
`internal/projectconfig.KeeperConfig` (44) / `ResolvedKeeperConfig` (40) / `KeeperFlags` (24) chain is
plainly the same configuration crossing three package boundaries and being re-widened at each. That chain
is the highest-value target here — five of the top fifteen entries are one concept — and it is one agent's
work, serially, not five.

`internal/projectconfig/projectconfig.go` is also **the head of the scheduler's critical path** (A2 → B4,
187 minutes), so it is the file where serial work is unavoidable regardless.

## Gate

The widest blast radius in the directory, and the tests are the only real signal.

```sh
go build ./...                       # every consumer must still compile
go test -count=1 ./internal/keeper/... ./internal/projectconfig/... ./cmd/harmonik/...
```

Confirm green before. `go vet ./...` matters more than usual here: catalog A5 (unkeyed composite literals)
is the specific hazard — a `Thing{a, b, c}` call site welded to field *order* breaks **silently** when the
struct is split or reordered, rather than loudly. **Run `go vet` on the pre-change tree and find the
unkeyed literals for the target type before touching it.** `go vet` catches these only for imported types;
same-package literals are the gap, and must be found by hand.

Exit metric: mean boundary width falls, and `funcseam`'s tier-0 candidate count rises in the affected
packages. That second one is the point of phase B and has never been measured across a real change.

## Honest state

**No phase-B operation has ever been executed on harmonik.** All four waves were phase A. The 50-minute
per-operation cost in the plan is invented — B4's marginal of 25 minutes per instance has no calibration
behind it at all. The first B4 operation someone times is worth more as a measurement than as a refactor.
