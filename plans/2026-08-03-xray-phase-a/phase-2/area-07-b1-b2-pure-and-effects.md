# Area 7 — B1/B2: computation welded to plumbing

**Classes:** B1 (pure logic embedded in an effectful declaration), B2 (effectful call inline in complex
logic). **Phase:** B. **Detector:** `heuristic`. **Lock unit:** file. **150 + 161 findings.**
Data: `data/area-07-b1-pure-in-effectful.json`, `data/area-07-b2-inline-effects.json`.

**This is the area with the highest research value in the directory, because phase B has never been
executed.** Four waves, all phase A. `funcseam`'s extraction tiers and the serial-extraction cost estimate
are exactly as unvalidated today as they were when they were written down. One executed B1 operation, timed,
is worth more than a thousand more A1 substitutions.

## What the detector computes

B1 reports the fraction of a function's top-level statements that touch **no effect**, with purity computed
to a fixpoint — a one-line wrapper around `os.ReadFile` is exactly as effectful as `os.ReadFile`, and most
code reaches the world through two or three such wrappers. A function at 65% pure statements is not one
tangled thing; it is a computation with plumbing wrapped around it, and **the pure part can leave today**:
no ports, no fixtures, no risk, testable the moment it lands.

B2 is the mirror image: high-complexity functions calling `os`, `net`, `exec`, `time.Now`, `fmt.Print*`
directly. Nearly every B1 site is also a B2 site.

## The candidates that matter

Ranked by complexity, all ≥40% pure:

```
  cx216  42% pure  harmonik.run                              cmd/harmonik/main.go:160
  cx158  73% pure  internal/daemon.runWorkLoop               internal/daemon/scheduler.go:484
  cx124  57% pure  harmonik.runBeadSubcommandIO              cmd/harmonik/run.go:114
  cx 93  57% pure  harmonik.runHarnessWithSigs               cmd/harmonik/harness.go:144
  cx 93  96% pure  internal/daemon.driveDotWorkflow          internal/daemon/dot_cascade_core.go:106
  cx 85  87% pure  internal/daemon.pasteInjectQuitOnCommit   internal/daemon/pasteinject.go:802
  cx 76  73% pure  internal/daemon.beadRunOne                internal/daemon/workloop.go:143
  cx 71  50% pure  internal/keeper.Watcher.Run               internal/keeper/watcher.go:1134
  cx 68  76% pure  internal/daemon.dispatchDotAgenticNode    internal/daemon/dot_cascade_core.go:1126
  cx 67  54% pure  harmonik.runCommsSendSubcommand           cmd/harmonik/comms.go:131
  cx 66  87% pure  internal/daemon.runAgentLaunch            internal/daemon/agentlaunch.go:341
  cx 62  54% pure  harmonik.runCommsRecvSubcommand           cmd/harmonik/comms.go:1282
  cx 62  60% pure  harmonik.runSubscribeSubcommand           cmd/harmonik/subscribe.go:59
  cx 58  84% pure  internal/daemon.SubscribeHub.HandleSubscribe  internal/daemon/subscribe.go:325
```

**Sort by purity × complexity, not by complexity.** The most valuable single target on this list is
**`internal/daemon.driveDotWorkflow` — complexity 93 at 96% pure.** Ninety-six percent of a
93-complexity function's top-level statements touch nothing in the world, and it is sitting inside the
daemon. Almost all of that can leave, today, and become testable without arranging a world.

By contrast `harmonik.run` at complexity 216 and 42% pure is the loudest number here and the worst first
task: it is `main`'s dispatcher, half of it is genuinely plumbing, and the payoff per unit of risk is the
lowest on the list.

Concentration by file (B1+B2 combined):

```
  14  cmd/harmonik/comms.go
  12  internal/eventbus/busimpl.go
   7  cmd/harmonik/keeper_enable_doctor_cmd.go
   6  cmd/harmonik/decisions_k4.go
   6  cmd/harmonik/handler.go
   6  cmd/harmonik/sync_assets_cmd.go
   5  cmd/harmonik/crew.go
   5  internal/keeper/watcher.go
```

## What to do

**B1 — extract the pure statements into their own function.** Pair this with `funcseam` from the organism
repo, which computes the exact signature of a statement run and refuses the ones that cannot be extracted
cleanly:

- **tier 0** — single entry, single exit. Extract these first; they are mechanical.
- **tier 1** — needs a caller-should-return protocol. Extract second, deliberately.
- **tier 2** — a `defer` or a loop-crossing `break`. **Never extract.** These mean restructure, not extract.

If the computed signature exceeds ~7 parameters and returns, **stop**: that is catalog B5, and the honest
move is to bundle the state into a type first, which is area 6. Do not extract a nine-parameter function
because the tool said a run was cohesive — a piece that cannot be understood alone defeats the only reason
to extract it.

**B2 — put the effect behind a port the caller supplies.** harmonik already does this well elsewhere:
`internal/runloop.RunPorts` is the established pattern, so this is applying an existing idiom unevenly
applied, not inventing one. Follow `RunPorts`' shape rather than designing a new port type per site.

**Do B1 before B2 at any given site.** Extracting the pure part first shrinks what the port has to cover,
and often reveals that the remaining effectful shell is thin enough not to need a port at all.

## Gate

```sh
go test -count=1 ./<edited pkg>/... ./<direct consumers>/...
```

Green before, green after — and for `internal/daemon` in particular, confirm the baseline first: several of
harmonik's ~30 known-failing tests are daemon integration tests, and a wave that cannot tell its own
breakage from the baseline has no gate.

**Exact post-condition available for B1**, and it is the reason to prefer B1 as the first phase-B
experiment: a pure extraction is a **refactoring in the strict sense** — same computation, new name. If the
extracted function is called from exactly the site it came from, with no reordering, the compiled package
should be identical or near-identical, and any behavioural difference is a bug in the extraction. Check
`go test -c` + `shasum`; if it differs, read the diff before assuming the tool was right.

Exit metric for phase B: **mean boundary width falls and tier-0 candidate count rises.** Re-run
`funcseam -list` before and after. Nobody has ever measured this delta across a real change, so record it
whatever it says — including if it does not move, which is what happened the first three times phase A's
exit metric was checked.

## What this area is really for

The project's sharpest open question is not "is there work to do" — there are 2,431 findings. It is
**whether executing a wave changes what the planner recommends.** Phase A's answer, measured three times,
is *not on its own*: it improves the graph's fidelity and leaves the recommendation invariant.

Phase B is the one that should move it, because unlike phase A it changes the *shape* of the graph rather
than adding star-topology vocabulary edges: extracting a pure function creates a new node with narrow ties,
and splitting a bundle deletes false ties. If a phase-B wave *also* leaves the recommendation invariant,
that is a much more serious result than phase A's, and it is worth knowing quickly.

**So: run one B1 operation, time it, and re-run `seam_finder` and `mq` on both sides.** That single
experiment closes the largest unmeasured claim in the project.
