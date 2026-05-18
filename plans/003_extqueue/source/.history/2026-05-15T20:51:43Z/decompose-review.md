# extqueue — Decompose-Pass Review

## Findings

**Coverage (criterion 1, 6).** All six goals and decisions D1–D6 in `01-problem-space.md` map to at least one spec entry in the matrix. The Goal→Spec table is accurate.

**Style (criterion 3).** Requirements are written as post-conditions ("After this change, the spec describes…") with no prescriptive editing instructions. Good.

**Tier ordering (criterion 5).** Tier A (queue-model) → Tier B (the four edits) → Tier C/D is internally consistent. No cross-reference cycle observed.

**Under-scoping — `operator-nfr.md` is missing from the matrix.** This is the most consequential gap. The existing spec already touches the queue surface in normative ways that this work invalidates or extends:

- `ON-015 — Beads is the queue; overlay schema is harmonik's half` (§4.4) is the single normative statement that today equates the queue with Beads. v1 introduces an in-memory daemon queue *layered on top of* Beads. ON-015's framing has to be edited or qualified, or the two specs will contradict.
- `ON-008` drain sequence step (1) ("orchestrator stops pulling new tasks from the queue") and ON-009a `needs-attention` drain discipline both describe queue behavior whose semantics shift once the daemon queue is a separate object.
- `ON-013a` enumerates `enqueue` as an operator-command-dispatch goroutine; §4.1 ON-002 exit-code taxonomy lists `enqueue` as an operator command. The new `hk queue` subcommand family overlaps this surface and must reconcile (is `enqueue` retired, aliased, or renamed?).
- §8 exit-code category `2 queue-format-unsupported` (ON-016/017) refers to Beads overlay compat; the new `.harmonik/queue.json` schema gains a parallel N-1 obligation under §4.5 ON-018.

This is genuine under-scoping. `operator-nfr.md` belongs in Tier B as `edit`.

**Over-scoping risk — process-lifecycle socket section may already exist.** `02-components.md` entry 4 proposes a new socket at `.harmonik/control.sock` with length-prefixed JSON. `process-lifecycle.md` already specifies `.harmonik/daemon.sock` (mode 0600, PL-003) and JSON-RPC 2.0 over NDJSON (PL-003a). The decomposition should not introduce a second socket or a competing wire format. The Tier-B edit to process-lifecycle should be framed as "register `hk queue` subcommands as new JSON-RPC methods on the existing socket," not as a new transport.

**`control-points.md` minor touch.** ON-026 names "queue-empty re-query cadence" as a tunable knob owned by the CP config inventory. Once the daemon stops re-querying `br ready`, that knob is obsolete or repurposed. Worth a one-line note in the matrix; not critical.

**`claude-hook-bridge.md` / `handler-contract.md` correctly excluded.** HC-016's "work queue per agent role" is an orchestrator-side worker concept, not the dispatch queue — the §"No-spec-change checks" exclusion is correct.

## Required changes to `02-components.md`

1. Add a row to the spec change matrix:
   `| 8 | specs/operator-nfr.md | edit | Reconcile ON-015 "Beads is the queue" framing with the new in-memory daemon queue; clarify ON-008 drain step (1); reconcile `enqueue` operator-command surface with `hk queue` subcommands; extend N-1 compat (ON-018) to cover `queue.json` schema. |`
   Add a per-spec requirements subsection §8 mirroring entries 2–6 in style. Add to Tier B.

2. Rewrite entry 4 (`process-lifecycle.md`) socket bullet to reuse the existing `.harmonik/daemon.sock` and JSON-RPC 2.0 / NDJSON transport per PL-003 / PL-003a, registering the `hk queue` methods on it. Drop the `.harmonik/control.sock` and "length-prefixed JSON" proposals — they conflict with locked spec.

3. Update Goal→spec coverage table to add operator-nfr where appropriate (the "failures surface" and "state survives daemon restart" rows in particular).

## Optional improvements

- Add a one-line note to entry 4 or to the No-change checks about `control-points.md` ON-026 knob ("queue-empty re-query cadence") becoming obsolete.
- Entry 1 §"Persistence" could name the on-disk schema as participating in the §4.5 ON-018 N-1 window so the design pass is forewarned.
- Entry 5 events should explicitly note whether they ride the existing event-bus envelope (EV-016 source matrix) or require a new source registration — the design pass will need this either way.

## Verdict

**REQUEST_CHANGES.** Coverage and style are good and Tier ordering holds, but `operator-nfr.md` is materially under-scoped and the proposed socket in entry 4 collides with locked transport spec. Both are concrete and fixable.
