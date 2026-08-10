# Area 3 — A1: a literal spells out a value some constant already holds

**Class:** A1. **Phase:** A. **Detector:** `exact`. **Lock unit:** file. **1,373 findings over 276 files.**
Data: `data/area-03-a1-same-package.json`, `data/area-03-a1-cross-package.json`,
`data/area-03-a1-ambiguous.json`.

This is the class all four executed waves ran, so it is the only class with a calibrated cost: ~18 minutes
of fixed setup and verification per file, and effectively nothing per instance. A file with 112 instances
costs about the same as a file with 2. **Target by density or the money goes into the tail.**

## The partition that decides everything

The detector's precision is high and its findings are **not** a mandate. Split before doing anything:

| bucket | count | what it means | who acts |
|---|---:|---|---|
| same package, one declarer | **70** | free — no new dependency, no ambiguity | an agent, unsupervised |
| cross package, one declarer | **579** | the fix creates a package dependency | agent proposes, human approves |
| ambiguous (2+ packages declare the value) | **724** | one spelling, two vocabularies | human decides, per site |

The ambiguous bucket is **53% of the class**, and that is the single most important fact in this area.
`plan` will price all 1,373 as work if you let it.

## Why ambiguity is the hard half

The canonical case, from `refactor/catalog.md`: `cmd/harmonik-twin-claude/replaydriver.go` compares a
progress-stream message kind against `"handler_capabilities"`. That value is declared **twice** —
`handlercontract.ProgressMsgTypeHandlerCapabilities` (the type a handler emits on its progress stream) and
`core.EventTypeHandlerCapabilities` (the `event_type` the daemon stores after
`internal/handlercontract/watcher_hc011.go` translates one into the other).

Same eight characters, two vocabularies, one of them correct at any given site. **A bare literal cannot say
which one it means**, which is why the detector reports both candidates and names neither. An earlier
version of the catalog named `core` — and `core` is the wrong constant at that site. The naming heuristic
was wrong, silently, in the documentation, for two revisions.

So: an agent working the ambiguous bucket produces a *proposal per site* naming which concept it believes
the site means and why, and does not edit. That is a scout task, not a fix task, and it is the first real
test of the shared-schema claim in the README.

## Where the density is

Top files by A1 count (`data/area-03-a1-*.json`, group by `file`):

```
 112  cmd/harmonik-twin-claude/scenarios.go
  52  internal/queue/rpc.go
  49  cmd/harmonik/comms.go
  35  internal/keeper/watcher.go
  31  internal/lifecycle/startup_pl005_qm002.go
  30  cmd/harmonik/keeper_enable_doctor_cmd.go
  27  internal/hookrelay/hookrelay.go
  25  cmd/harmonik/init_cmd.go
  25  cmd/harmonik/main.go
  23  cmd/harmonik/smoke.go
  22  internal/workflow/dot/parser.go
```

**`cmd/harmonik-twin-claude/scenarios.go` is the largest single target and is very likely the wrong one.**
The twin binary imports no harmonik package at all — that is what makes every one of its literals a
cross-package finding, and there is a standing argument that the decoupling is deliberate: a test twin that
imported the code under test would stop being an independent check of the wire format. The previous
enumeration of the cross-package set (`refactor/04_cross-package-a1_2026-08-04.md` in the organism repo)
concluded *leave* for the great majority, and put the whole 1,113-instance cross-package wave at 0.5% of
total cut cost.

**Do not run the twin binary's 156 findings as a wave without an explicit operator decision.** The triage
rule `"deny_paths": ["cmd/harmonik-twin-claude/**"]` already exists from the first wave and should be
carried forward until someone overturns it.

## Cross-package: the shape that is actually worth fixing

Grouped by (referencing package → declaring package), the unambiguous cross-package set:

```
  76  cmd/harmonik              -> internal/core
  26  internal/daemon           -> internal/core
  25  cmd/harmonik              -> internal/lifecycle
  19  internal/keeper           -> internal/lifecycle/tmux
  19  cmd/harmonik              -> internal/scenario
  17  cmd/harmonik              -> internal/lifecycle/tmux
  16  cmd/harmonik-twin-claude  -> internal/core     <- deny (see above)
  15  cmd/harmonik              -> internal/schedule
  13  internal/eventbus         -> internal/core
```

**The `-> internal/core` rows are the cheap ones and the ones worth doing**: every package listed already
imports `core`, so referencing the constant adds no dependency at all — it converts an invisible tie into a
`VOCAB` edge for free. That is ~140 findings with the "creates a package dependency" objection removed by
inspection. Verify the existing import before treating any row as free; the check is one `grep` per file.

**Most of these are downstream of area 1.** Type `core.Event.Type` and a large share of the
`-> internal/core` set becomes a build error the compiler finds, with no detector and no triage. If both
areas are going to run, **area 1 first** — otherwise this area is hand-fixing what a type change would have
found.

## Gate

The exact one, and it subsumes the test run for this class:

```sh
go test -c -o /dev/null ./<pkg>          # before
shasum <compiled test binary>
# ...edit...
go test -c ./<pkg> && shasum             # must be byte-identical
go test -count=1 ./<pkg>/... ./<direct consumers>/...
```

A literal→constant substitution is a no-op after constant folding, so **the compiled package must be
byte-identical**. If it is not, the substitution changed behaviour and the finding was wrong.

Additionally, the phase exit metric: re-run `typegraph` and confirm new `VOCAB` edges appear between the
edited file and the declaring file, where there were none. Four waves have confirmed this decomposes
exactly — 174 substitutions produced 174 edges, 176 produced 176.

## What this class will not do, and stop expecting it to

Phase A joins every consumer to the shared constant **and no consumer to any other**. After two waves and
352 new edges, the co-changing *pair of consumers* still had zero edges between them. The topology is a
star, not a mesh. It turns out a two-hop path is enough for a community detector — but only once the
clusterer is weighted to care (`VOCAB` relatedness 0.5, landed in the organism repo) and only with a
complete Louvain. **A1 improves the graph's fidelity. On its own it does not change what the planner
recommends.** Run it for the compiler enforcement, not for the metric.
