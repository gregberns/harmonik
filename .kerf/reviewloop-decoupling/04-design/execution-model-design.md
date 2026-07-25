# Review-cycle execution semantics design

## Current state

Execution Model owns review-loop behavior but differs from the driver: it routes on diff-hash equality
instead of HEAD advancement, omits shipped `review_fixup_stalled`/`fixup_stalled`, does not define
flagless-`REQUEST_CHANGES` normalization, and does not treat the continuity checkpoint SHA as the initial
work-product baseline. Reserved context state is not durably maintained and some cancellation paths omit
cycle completion.

## Target state

Define the pure projection:

`CycleState + Observation -> CycleDecision{next state, ordered cycle intents, terminal result?}`.

State contains iteration/cap, authoritative implementer continuity ID, prior work-product HEAD, diff
hashes as evidence, raw verdict, and exact initial baseline. Observations are validated phase result,
resulting HEAD/work-product advancement, diff hash, verdict, cancellation, or typed failure. The kernel
contains no git, filesystem, event emitter, clock, process, network, goroutine, cleanup, or checkpoint I/O.

Amend EM-012/015d/015e and grammar:

- reserve and durably maintain `last_iteration_head_sha`; keep diff hash as evidence;
- define initial no-commit baseline as the exact continuity-checkpoint SHA;
- exclude daemon control commits from work-product;
- route unchanged resumed HEAD to `fixup_stalled` without a reviewer;
- preserve raw flagless `REQUEST_CHANGES` but normalize routing to approval;
- route actionable request-changes below cap to the next iteration, at cap to `cap_hit`, and BLOCK
  immediately;
- reuse the authoritative implementer continuity identity for every resume while selecting a fresh
  handler and harness identity for every reviewer launch;
- add `fixup_stalled` to completion grammar while retaining historical `no_progress`;
- require exactly one terminal cycle decision after cycle entry;
- define `implementer_phase_complete` as logical result, with advancement still gated by C4 close.

Terminal routing remains explicit: `approved` is success; `cap_hit`, `blocked`, and `fixup_stalled` are
needs-attention outcomes; daemon/phase/verdict `error` follows the existing failure/needs-attention ladder.
No pure decision directly performs bead mutation.

Decision/conformance coverage is exhaustive for initial no-work, resumed stall, approve, flagless approve,
request-changes retry/cap, block, malformed/missing verdict, phase error, and cancellation. Properties cover
monotone bounded iteration, at most one reviewer per iteration, exactly one terminal decision, stable
implementer continuity, fresh reviewers, durable restart state, and checkpoint-baseline use.

## Rationale

HEAD advancement matches shipped routing and separates work-product truth from diagnostic diff evidence.
A value-only kernel makes the table exhaustive while leaving all effects with their owners.

## Traceability

- Component C1; EM-012/015d/015e, grammar, conformance.
- Evidence: execution, checkpoint, event, and phase-lifetime research.
- Goals: deterministic policy, durable restart state, controlled extraction.
