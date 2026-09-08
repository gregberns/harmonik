# Pending operator decisions — restructure track

These are the calls a plan edit cannot make. Each has a recommended default so Phase 0
is NOT blocked. They gate real code movement, not scaffolding.

## R-D1 — Does the minimal core-vocabulary carry clear the GREEN spike bar? (TOP)
The plan opens the spine only if the Phase-1 measured spike shows the minimal vocab
carries few types, drags no `internal/core` type-family cycle, and needs a small shim.
This is answered by RUNNING the spike, not by deciding in advance. It is the single
decision that gates opening the spine port loop.
- **Default:** run the spike, report GREEN/AMBER/RED, decide then. Do not open the spine on hope.

## R-D2 — How does the legacy lint allow-list survive a `git mv`? (hard blocker)
The prior decomposition program named this unresolved. The plan now RESOLVES it with a
Phase-0 experiment (move one daemon-adjacent file, confirm no new legacy findings fire)
instead of restating a policy. Needs an operator nod that the experiment result is the
gate before the first daemon-adjacent move.
- **Default:** treat the Phase-0 experiment as the gate; block the first daemon-adjacent `git mv` until it passes.

## R-D3 — Keeper `presence` tentacle: route a vs b (from reconciliation)
Keeper reaches `internal/core` through `presence` (keeper→presence→eventbus→core), on
top of the known digest/dashboard cuts.
- **a (recommended):** invert `presence` into a keeper-declared consumer port, keeping
  Track B independent of the spine port order.
- **b:** gate keeper's move on the `eventbus`+`presence` spine ports landing first.
- **Default:** a — keeps keeper's standalone-binary track from serializing behind the spine.

## R-D4 — Promote the hermetic watched-to-fail unit layer to a hard per-port exit gate?
Review finding 7, outside the six assigned must-fixes. Currently a soft signal.
- **Default:** left soft for now; flagged. Operator's call whether hermetic unit coverage
  becomes a hard exit gate per port.

## Standing capacity risk (not a plan decision)
Both plans assume Codex/Pi crew throughput the fleet has not yet demonstrated. Stated as a
risk in both plans; it is an operator capacity call, not something a plan resolves.
