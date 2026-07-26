# Research review

## Round 1 — REQUEST_CHANGES

The independent reviewer found four blockers:

1. durable artifact authority was unresolved;
2. Minted and Captured session timing had been conflated;
3. current parity and corrective target behavior shared one trace oracle;
4. `implementer_phase_complete` had no unambiguous meaning relative to quiescence.

Nonblocking cautions were to keep Handler watcher authority separate from Process lifetime ownership,
avoid speculative reviewer-progress evidence, and stage DOT/full remote repairs rather than folding them
into a behavior-neutral extraction.

## Resolution

- Durable files remain authoritative Workspace Model artifacts. Reviewer projection files transfer to the
  run-workspace control archive; class-F events remain routing/audit facts.
- Minted identity is durable before first work/ACK. Captured identity is observed after initial launch and
  becomes durable before resume/Retask.
- Current characterization and amended target traces are separate, with corrective behavior staged after
  the pure extraction.
- `implementer_phase_complete` is defined as logical work-result publication. The invisible phase-close
  barrier, not that event, authorizes advancement and workspace release.
- Handler Contract owns watcher authority/cardinality; Process Lifecycle owns phase cancellation, join,
  and resource lifetime.
- Reviewer budget retains shipped diff allowance plus bounded liveness grace and adds no speculative
  progress signal.
- DOT continuity and full remote behavior are follow-up consumers of reusable ports, not prerequisites for
  extracting the review-loop coordinator.

## Round 2

APPROVE. The independent reviewer confirmed all four blockers were resolved.
