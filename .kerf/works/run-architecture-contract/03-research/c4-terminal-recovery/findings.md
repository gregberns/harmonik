# C4 research — Durable terminal and recovery composition

## Questions

1. Which stores are authoritative at each durable edge?
2. What ordering is already fixed by CQ-02 and JR-00?
3. What role may events play?
4. Which crash windows need explicit recovery owners?
5. Where may terminal effects be composed without adding a writer?

## Findings

### Authorities

- Queue state, intents, receipts, cleanup, and exact-ID status are owned by
  queue-model QM-050–QM-064 and the finalized CQ-02 contract.
- Run identity, durable Run records, terminal ledger ordering, and recovery
  classification are owned by execution-model EM-044–EM-046b and JR-00.
- Terminal Bead transitions are owned by beads-integration's adapter contract.
- Handler/session detection is owned by HC; daemon process recovery by PL.
- Events are observations under EV-INV-001. EV-015–EV-018 own append/fsync
  ordering; EV-021/EV-022 prohibit reconstructing state from JSONL.

### CQ-02 ordering is closed

Every queue replacement uses a normalized-name transaction: lock, snapshot
and generation check, private clone, validate/mutate, marshal exact bytes,
classify persistence, install only a durable selected result, advance volatile
generation, then Wake/observe. Results are `rejected`, `not_committed`,
`committed_durable`, or `commit_indeterminate`.

Final success requires intent-bound completed canonical bytes, an immutable
completion receipt, optional normal-path event attempt, exact-match CAS
cleanup, ownership release, and later release-marker/GC handling. Recovery
uses queue-owned facts only; it never mints a replacement identity or replays
an event. RAC must cite this sequence, not summarize it into a competing
transaction.

### Transition composition required by JR-00

The transition table must distinguish at least:

1. queue reservation;
2. Bead claim;
3. Run ID mint and durable Run record;
4. in-memory Run registration;
5. process/session launch;
6. mode result;
7. merge/terminal Run ledger;
8. terminal Bead close/reopen;
9. queue item/group completion or failure;
10. completion receipt and canonical cleanup;
11. Run-record cleanup;
12. restart reservation/session adoption/reconciliation.

For each, the design needs decision, effect, serialization, recovery, and
normative-owner columns plus observation timing. “Terminal owner” alone is
insufficient.

### Event boundary

A state transition may construct its event payload before persistence when
CQ-02 requires exact derived bytes, but may publish only at the owning
contract's observation edge. Append failure never rolls back durable queue,
Run/git, or Beads state. Restart never retries/synthesizes a missed terminal
event. Event consumers may detect divergence but cannot repair authority from
JSONL.

### Crash windows and task ordering

CQ-02's DAG fixes critical order:

```text
CQ-02I -> CQ-01 -> CQ-RECEIPT -> CQ-RUN-WAIT
    \          \                    /
     -> CQ-03 -> JR-01 -> CQ-04 -> JR-03 -> JR-04 -> WL-REC-01
```

The detailed prerequisites additionally include BR and WL gates. Receipt-aware
waiter and capable startup recovery must precede live receipt-producing
terminal composition. Adoption follows terminal/startup ownership and precedes
workloop recovery call-site migration.

## Risks, patterns, and decision status

- Existing live-pointer queue writers violate the future owner; RAC cannot
  route around CQ-02.
- Queue, Run, Beads, and event durability are not one cross-store transaction.
  Recovery classification, not rollback, composes them.
- Cleanup cannot release a queue name or Run handle while an intent/record
  remains unresolved.
- Events need a dedicated observation column to prevent accidental authority.

Use a terminal composer that calls existing owner ports in normative order and
records typed partial progress for recovery. It owns no store. No unresolved
blocker prevents design.
