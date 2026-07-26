# Checkpoint and ACK findings

## Questions

1. Which specs own session identity, checkpoint durability, and protocol ACK?
2. What ordering is required, and is event publication part of success?
3. How do the current local, tmux, captured-ID, and remote paths differ?
4. What boundary would make the behavior atomic, testable, and reusable?

## Normative ownership

- `specs/execution-model.md` EM-015d owns implementer-session continuity; EM-023/EM-023a own durable checkpoint shape; EM-025a only constrains events that are emitted after a reference advance.
- `specs/claude-hook-bridge.md` CHB-023 specializes the sequence for Claude: receive/capture the session ID, persist it in git-backed `Run.context`, then return the connection-accept ACK.
- `specs/handler-contract.md` HC-009 and §7.2 own capability negotiation and `version_selected`.
- `specs/harness-contract.md` HN-008/HN-011 own `SessionIDMinted` versus `SessionIDCaptured`, including fail-closed behavior when a captured identity is absent.

For `SessionIDMinted`, the authoritative sequence is:

`authoritative session identity -> durable run-context checkpoint and exact baseline -> protocol ACK, when the selected harness protocol has one`

For `SessionIDCaptured`, the initial process must start before the native identity can be observed. Its
sequence is:

`initial launch -> native identity observation -> durable run-context checkpoint -> permit resume/Retask`

Captured persistence cannot gate initial work. Absence of a captured identity still fails closed before
resume/Retask, and no tracking UUID may be substituted.

Event publication is not a stage in this transaction. Existing projections may be emitted after checkpoint success, but observability failure must not alter durability or ACK correctness.

## Current implementation drift

`internal/daemon/sessioncontext_chb023.go` `persistClaudeSessionID` writes and stages a context file, commits it, and returns HEAD. Its idempotency check reads the working-tree file rather than committed state. A crash or commit failure after the write can therefore leave an uncommitted file that a retry incorrectly treats as durable.

Other gaps in `internal/daemon/reviewloop.go`:

- checkpoint and ACK failures are logged as non-fatal;
- a detached goroutine performs the work and the loop non-blockingly samples result channels, so it can miss a successful checkpoint and use the wrong baseline;
- a different existing session mapping is overwritten instead of producing a typed continuity conflict;
- the negotiated version is not carried explicitly to the ACK call;
- the direct tmux Claude path has no interception or pre-work checkpoint and persists only after execution;
- remote persistence is unsupported;
- `SessionIDCaptured` is kept only in memory and falls back to a tracking/minted UUID when capture is absent, contrary to HN-008;
- workloop and DOT do not durably preserve captured continuity, and DOT can resume with the tracking UUID.

The ad hoc context commit also lacks the Transition record and `outcome_status` required by EM-023a. The design must either use the normal checkpoint writer or explicitly define a specialized run-context checkpoint class.

## Error behavior to preserve or correct

| Condition | Required result |
|---|---|
| capabilities absent or no common version | typed protocol failure; no ACK |
| same committed mapping | idempotent success with exact baseline |
| different committed mapping | typed continuity conflict |
| working-tree-only mapping | not durable; retry persistence |
| checkpoint failure | fail launch/transition; no ACK |
| ACK failure | terminal handshake failure |
| captured identity absent | fail closed; never substitute tracking ID |
| event projection failure | does not invalidate committed mapping |
| remote execution | same guarantees at the explicit worker location |

## Boundary to use

Introduce an awaitable continuity-checkpoint transaction behind a narrow port. Its request should contain:

- run/workflow/phase/iteration identity;
- explicit execution location or runner;
- harness and session-ID policy;
- expected minted ID or observed captured ID;
- prior committed mapping, if known;
- optional negotiated protocol ACK capability.

Its single result should contain:

- authoritative session ID;
- checkpoint SHA;
- exact no-commit baseline SHA;
- whether a committed mapping was reused;
- ACK disposition (`not_applicable` or `sent`).

Required invariants:

1. Inspect committed state for idempotency.
2. Reject conflicting mappings.
3. Withhold ACK until checkpoint success.
4. Treat ACK failure as failure.
5. Make completion awaitable; do not use sampled channels.
6. Give local and remote adapters identical semantics.
7. Fail closed for missing captured identity.
8. Never let reviewer identity overwrite implementer continuity.
9. Keep event publication outside transaction success.

The pure part is the decision function over prior committed mapping, proposed identity, session policy, and protocol capability. It returns `reuse`, `checkpoint`, or a typed conflict plus the required ACK disposition. Filesystem, git, remote execution, and wire ACK are adapters driven by that decision.

## Spec conflicts and amendments

1. HN-008 and production use caller-minted Claude identity, while HC-045c/CHB-008/CHB-023 describe handler-minted identity. Match the production topology: caller-minted identity, with capabilities confirming it when a wrapper protocol exists.
2. For Minted drivers, generalize CHB-023 to require durability before the selected driver's first-work
   boundary; `version_selected` applies only to handshake-based handlers. For Captured drivers, require
   durability after native observation and before resume/Retask.
3. Define whether the continuity commit is a standard EM-023a checkpoint or a specialized checkpoint class.
4. State continuity for both Minted and Captured policies, including captured fail-closed behavior and durability before resume/Retask.
5. Require explicit execution placement for remote continuity checkpoints.

## Tests needed

- crash/failure after write and after stage but before commit;
- uncommitted-file false idempotence;
- checkpoint failure withholds ACK;
- ACK failure is terminal;
- exact checkpoint-before-ACK trace;
- no goroutine/result-channel race;
- negotiated version propagation;
- local/remote behavioral parity;
- captured identity absence fails closed;
- captured mapping is durable before Retask;
- DOT uses the captured ID rather than tracking UUID;
- concurrent agent git activity is serialized safely;
- checkpoint output conforms to the chosen EM durability class.
