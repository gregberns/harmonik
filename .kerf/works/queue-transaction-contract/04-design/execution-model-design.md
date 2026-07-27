# Execution-model named-queue consumption design

## Mode and boundary

Bounded consistency amendment. Queue-model continues to own queue identity,
lifecycle, QueueStore transactions, per-queue workers, and QM-067 arbitration.
Execution-model consumes those contracts only in the card-authorized regions.

## Target state

### Complete immutable fleet view

Every selection, eager-refill duplicate check, and orchestrator pre-submit
check begins with a complete immutable QueueStore named-set snapshot. A partial
or selected-name-only scan is invalid.

### Per-name submit

After the all-queue duplicate screen, an unoccupied normalized target permits
submit even if sibling names are live. An occupied target permits append only
to that target's appendable stream group; otherwise it rejects. Nothing
replaces a live target. QM-061 is corrected to state that QM-027 is per name.

### Dispatch selection and capacity

The §7.4 steady-state loop:

1. obtains one complete named-set snapshot;
2. uses `br ready` only when that set is empty and auto-pull is enabled;
3. filters to active queues with active groups, eligible items, and local
   capacity below `workers`;
4. idles if a non-empty fleet has no candidates;
5. enforces global `--max-concurrent`;
6. selects via QM-067 name-ordered round-robin;
7. advances the cursor on every selection; and
8. revalidates global and selected-queue capacity before claim.

Paused, completed, full, and otherwise ineligible queues do not block eligible
siblings.

### Eager refill

EM-062 chooses the lexicographically first eligible active stream queue with a
positive deficit after both capacity gates. EM-063 pre-screens candidate beads
against every queue in that same snapshot before the per-name append. This
deterministic refill target does not alter QM-067 dispatch ordering.

### Terminal group and observations

EM-015f is phrased per selected named queue. Terminalization mutates no sibling.
The existing normal-path `queue_group_completed` attempt, receipt authority,
and no-restart-synthesis clauses remain intact.

### Fallback-owner clauses and conformance fixtures

EM-066 and EM-067 replace only their stale `queue IS None` references with
`fleet.named_queues IS EMPTY`; all surrounding default, sealing, and pause
semantics stay byte-preserved. In §10.2, the pause fixture enables fallback
with `--auto-pull` set, and one adjacent fixture holds a non-empty wholly
ineligible fleet while a ready bead exists to prove `br ready` is never
consulted. The §9.3 map cites lifecycle as queue-model §8 and capacity as §9
QM-062 with QM-060/QM-066/QM-067.

## Ownership handoff

- `WL-03` owns extraction of fleet snapshot/candidate selection and QM-067
  cursor behavior from the workloop.
- `CQ-03` consumes that selection and revalidates both capacity gates while
  reserving one dispatch.
- `CQ-CALLER-EAGER` owns EM-062/EM-063 production adoption in
  `eagerRefillEval`.
- `CQ-01` owns per-name submit/append validation behind QueueStore.

Each card must name exact symbols, leases, and proofs; no two concurrent cards
may edit the same workloop region.

## Semantic-scope proof

The full-file execution draft must differ from baseline only in:

- frontmatter `version` and `last-updated`;
- glossary `active queue` and `queue group`;
- EM-015f and EM-062 through EM-065;
- §6.5 queue lifecycle;
- §7.4 steady-state queue selection/capacity;
- §9.3 queue-model dependencies;
- §10.1 queue dispatch conformance;
- the exact EM-066/EM-067 named-set-empty references;
- the exact §10.2 auto-pull pause token and adjacent nonempty-ineligible
  fixture; and
- exactly one qualifying revision row.

The proof requires complete QueueStore scan, empty-set-only `br ready`,
eligible active queue selection, both capacity gates, QM-067 cursor advance,
all-queue duplicate pre-screen, per-name submit, and QM-061 correction.

## Explicit non-changes

Run/workflow schemas, checkpoints, failure taxonomy, event payload schemas,
event-log durability, and non-queue control flow are unchanged.
