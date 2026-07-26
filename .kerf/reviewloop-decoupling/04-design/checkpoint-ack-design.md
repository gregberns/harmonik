# Session checkpoint, baseline, and protocol acceptance design

## Current state

Minted identity exists before work and CHB-023 orders capabilities/identity, durable context checkpoint,
then acceptance ACK. Captured identity appears only after launch and must be durable before resume/Retask.
Current review-loop code mixes interceptors, detached goroutines, sampled channels, fallback persistence,
and synthesized IDs; the checkpoint SHA also supplies the no-commit baseline.

## Target state

Amend EM-015d with policy-specific sequences.

Minted:

1. obtain identity before work;
2. persist through the chosen EM-023a checkpoint class;
3. return the exact checkpoint SHA as baseline;
4. only then send protocol acceptance ACK and permit first work.

Captured:

1. launch without a substitute identity;
2. observe the first authoritative native identity;
3. persist through the same checkpoint transaction;
4. return the new baseline;
5. require completion before resume/Retask.

Missing identity or persistence failure fails closed. Captured drivers have no CHB ACK unless their own
protocol defines one.

Pin the error matrix:

- absent capabilities or no common version is a typed protocol failure with no checkpoint-dependent ACK;
- checkpoint failure withholds ACK and first work;
- ACK write/finalization failure is terminal for the handshake;
- the selected negotiated version is an explicit transaction input and the same version is sent in ACK;
- identical committed mapping is idempotent, a conflicting mapping is a typed continuity failure, and
  working-tree-only state never counts as durable;
- event-projection failure cannot invalidate a committed mapping and is outside transaction success.

Define a cohesive location-explicit checkpoint transaction returning authoritative identity, checkpoint
SHA/baseline, idempotent-reuse status, and ACK disposition. It inspects committed state, reuses an
identical mapping, rejects conflicts, supports local/remote run workspaces, and never reconstructs the
baseline opportunistically.

The Minted critical sequence is exactly identity -> durable checkpoint/baseline -> ACK. Event projection
is outside transaction success. Pin crash states before/after checkpoint and ACK; no in-memory or
synthesized identity authorizes resume.

## Rationale

The policies differ in acquisition time but share a durability transaction and baseline result. This
removes channel/goroutine coupling without forcing Captured harnesses into a nonexistent pre-exec ACK.

## Traceability

- Component: C5; EM-012/015d/023a/025a/031, CHB-018/023, HC-006/006a/045c, HN-008/011.
- Tests: Minted crash cuts and ACK faults; Captured success/missing/persist failure; committed
  idempotency/conflict; capability/version mismatch and selected-version propagation; remote parity;
  returned baseline used by no-commit guard.
