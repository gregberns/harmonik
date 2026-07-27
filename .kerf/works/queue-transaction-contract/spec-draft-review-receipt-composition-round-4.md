# Spec-draft composition review — focused Round 4

**Verdict: REQUEST_CHANGES**

The newly authorized named-QueueStore regions, QM-061 correction, and
21-slice implementation DAG are internally consistent. One adjacent normative
owner seam was not included in the bounded edit and still describes the
removed singleton branch, so the empty-set-only `br ready` contract is not yet
composed end to end.

## Passing checks

- The execution-model semantic checker passes and confines changes to the
  exact eleven authorized regions, frontmatter version/date, and one qualified
  revision row. The evidence records all required named-fleet semantics:
  complete immutable QueueStore scan, all-queue duplicate pre-screen,
  deterministic eligible target, per-name submit/append, daemon-wide plus
  per-queue capacity, QM-067 cursor advance, and empty-named-set-only fallback.
- The execution-model glossary, EM-015f, EM-062 through EM-065, §6.5, §7.4,
  §9.3, and §10.1 agree on named queue/group identity. QM-061 agrees that the
  daemon/project QueueStore serializes submission while QM-027 occupancy is
  per normalized name.
- WL-03 uniquely owns pure fleet projection, `snapshotFleet`,
  `selectNextQueue`, steady-state source arbitration, and cursor advance.
  CQ-03 depends on WL-03, consumes its immutable selection, and owns
  reservation plus both capacity revalidations. CQ-CALLER-EAGER separately
  owns the file-disjoint eager caller, complete-fleet duplicate pre-screen,
  deterministic target, and mutation.
- `CQ-02.yaml` retains the exact 23-key schema and 69 crash rows, now with 21
  unique implementation slices. Every slice declares prerequisites, lease,
  writes, failing-first proof, accepted intermediate state, rollback boundary,
  and conflicts. The dependency graph is acyclic.
- The previously approved receipt, exact-ID status, independent waiter,
  startup recovery, cancellation, failure-pause transaction, terminal, and
  caller ownership spines remain intact. QM-052 and EM-015f still require the
  failed group plus `paused-by-failure` in one durable commit before
  observations. Optional group observations remain diagnostic-only,
  non-persistent, non-replayed, and non-authoritative; no global event slice
  was introduced.
- `git diff --check` passes.

## R4-C1 — Fallback owner clauses still target the removed singleton branch

The newly corrected §7.4 takes the fallback decision only when
`fleet.named_queues IS EMPTY`, and explicitly idles when a non-empty named set
has no eligible candidate. Two normative requirements outside the authorized
regions still point at the former singleton algorithm:

- EM-066 says the §7.4 ``queue IS None`` branch routes either to idle or
  `br ready`, although that branch no longer exists.
- EM-067 twice explains pause ordering relative to reaching the
  ``queue IS None`` branch, rather than the complete named-set-empty branch.
- The §10.2 EM-066/EM-067 pause-gate test says fallback is enabled with the
  opt-in flag unset. Under the current default, fallback is enabled only
  **with `--auto-pull` set**.

This is not merely stale terminology: EM-066/EM-067 own fallback gating and
§10.2 owns its conformance test. As written, an implementer can follow §7.4
and fail those clauses, or interpret the dangling singleton condition as
allowing `br ready` when named queues exist but are ineligible. The conformance
test also configures the opposite topology from the behavior it expects.

Required correction:

1. Amend only the affected EM-066 and EM-067 sentences to name the complete
   QueueStore named-set-empty branch and preserve the existing operator-pause
   semantics.
2. Amend the §10.2 pause-gate fixture to enable fallback with
   `--auto-pull`, and explicitly retain a non-empty-but-ineligible fleet case
   proving `br ready` is not consulted.
3. Expand the bounded checker, scope evidence, and the current qualifying
   revision row narrowly enough to cover those exact corrections.

No broader execution-model rewrite or reopening of the previously approved
receipt composition is required.
