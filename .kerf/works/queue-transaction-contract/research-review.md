# Research review

## Round 1

- Reviewer: `/root/cq02_research_review`
- Scope:
  `03-research/queue-model/findings.md`,
  `02-components.md`, CQ-02, CQ-00 evidence, current queue spec and source
- Verdict: `REQUEST_CHANGES`

### Finding R1 — migration matrix and source symbol

The matrix omitted valid-legacy/invalid-canonical as a conflict state and did
not state the required preservation behavior precisely enough. Invalid
legacy/valid canonical must load canonical while retaining invalid legacy
unchanged. Valid divergent copies must fail closed. The current persistence
symbol is `internal/queue/persistence.go` `queuePath`, not `queueFilePath`.

Resolution:

- corrected the source symbol;
- added all legacy/canonical presence and validity rows;
- specified that valid legacy + invalid canonical preserves both unchanged,
  loads neither, and mutation-refuses `main`;
- specified that invalid legacy + valid canonical loads canonical and retains
  legacy unchanged;
- retained fail-closed behavior for valid divergent copies.

### Finding R2 — operation-specific indeterminate recovery

The generic read/compare/sync algorithm did not distinguish replacement,
unlink, and archive success candidates. Unlink expects absence. Archive
requires canonical absence plus the exact intended archive; both, neither, or
conflict must quarantine.

Resolution:

- split reconciliation into replacement, unlink, and archive classifiers;
- made canonical absence the unlink success candidate;
- required canonical absent plus exact intended archive identity for archive
  promotion;
- made both/neither/mismatch/third-state archive results quarantine;
- retained successful parent-directory sync as the durability boundary and
  classified later close failure as diagnostic only.

### Finding R3 — cancellation/archive writer inventory

The initial research covered shutdown and disk cancel but missed the complete
writer set: `HandlerAdapter.HandleQueueCancel`, `RunQueueCancel`,
bootstrap-time auto-archives, and `runBeadSubcommandIO`'s post-daemon
paused-failure archive.

Resolution:

- added a live-daemon operator-cancel persist/install/archive/clear protocol;
- made `RunQueueCancel` RPC-only and daemon-unreachable cancel fail closed;
- assigned bootstrap-time archive regions to the existing bootstrap migration;
- proposed a separate serialized inline-exit slice for the post-daemon archive;
- recorded all source symbols and archive result/recovery obligations.

### Finding R4 — DAG cycle and missing dependencies

The original “all caller migrations before JR-03” edge conflicted with the
maintenance-after-JR-03 edge. CQ-04 also lost its CQ-03 dependency and omitted
JR-01. Group activation and JR-03 both touch `workloop.go` and require explicit
serialization.

Resolution:

- replaced the informal graph with an exact acyclic graph;
- CQ-04 retains CQ-02I + CQ-03 and adds JR-01;
- group activation starts after CQ-03 and becomes a JR-03 predecessor;
- work-loop maintenance starts after CQ-03 + JR-03;
- inline exit starts after bootstrap + JR-03;
- JR-04 remains after CQ-04 + JR-03.

## Author resolution status

All four round-1 findings are incorporated in the research artifact. The
coordinator routed a focused round-2 confirmation. This pass review is
separate from CQ-02's final independent durability and composition reviews.

## Round 2

- Reviewer: `/root/cq02_research_review`
- Verdict: `REQUEST_CHANGES`
- Prior findings: R1, R3, and R4 resolved; R2 retained one archive-restart gap

### Finding R2.1 — timestamped archive identity is not reconstructible

Shutdown selects `.cancelled-<timestamp>`. After death between rename and
parent-directory sync, volatile result state is gone and several historical
archives may exist. The design could not assume restart knows “the intended
archive” or select the newest candidate.

Resolution:

- added a stable, queue-specific
  `.harmonik/queues/<name>.archive-intent` sidecar;
- the directory-durable intent records schema version, normalized name,
  parseable queue ID when available, source-byte digest, archive kind, and
  exact destination basename before rename;
- archive cannot start before intent durability;
- restart promotes only canonical-absent + exact intent-selected archive
  identity, resolves not committed only for exact source canonical +
  destination absence, and quarantines both/neither/corrupt/mismatch/third
  states;
- historical archives never substitute for the intent destination;
- after archive durability, intent removal and its directory sync must finish
  before owner clear or same-name admission;
- missing canonical + missing intent becomes the clean no-owner startup state
  only after startup successfully syncs `.harmonik/queues`.

## Author resolution status after round 2

R2.1 is incorporated. A focused round-3 confirmation was completed.

## Round 3

- Reviewer: `/root/cq02_research_review`
- Verdict: `REQUEST_CHANGES`
- Archive-intent protocol: approved

### Finding R2.2 — `main` clean-state classification ignored legacy evidence

The design called canonical-absent + intent-absent clean without considering
`.harmonik/queue.json`. Migration could recreate `main.json` while a main
archive intent was still resolving, or invalid/divergent legacy could be
silently bypassed.

Resolution:

- archive-intent classification now precedes migration and queue loading for
  every name;
- while a main intent exists, legacy is preserved unchanged and migration may
  not create `main.json`;
- after exact archive durability, absent legacy permits intent cleanup,
  equivalent legacy is durably removed as a duplicate before intent cleanup,
  and invalid/divergent legacy retains the intent plus mutation refusal;
- main reaches clean no-owner only after legacy absence or durable migration;
- historical archives alone remain nonblocking.

## Author resolution status after round 3

R2.2 is incorporated. A focused round-4 confirmation was completed.

## Round 4

- Reviewer: `/root/cq02_research_review`
- Verdict: `REQUEST_CHANGES`
- Legacy/intent ordering: approved except for one absence-durability cut

### Finding R2.3 — absent legacy may be an unsynced unlink

With a durable main archive intent, observed legacy absence could result from
an equivalent-legacy unlink whose `.harmonik` parent sync did not complete.
Removing intent could allow legacy resurrection after a later crash.

Resolution:

- any main-intent path that observes legacy absent now requires
  `fsync(.harmonik)` before intent removal, even if absent initially;
- equivalent legacy follows unlink → `fsync(.harmonik)` → remove intent →
  `fsync(.harmonik/queues)` → clear/admit;
- `.harmonik` parent open/sync failure retains intent plus mutation refusal;
- directory close after a successful sync is diagnostic only;
- the same ordering is explicit for shutdown, live operator cancel, and
  startup recovery.

## Author resolution status after round 4

R2.3 is incorporated. A focused round-5 confirmation was completed.

## Round 5

- Reviewer: `/root/cq02_research_review`
- Verdict: `APPROVE`

The reviewer confirmed the archive-intent protocol, main-legacy ordering, and
legacy-parent crash cut now compose without an unresolved recovery state.
Research may advance.
