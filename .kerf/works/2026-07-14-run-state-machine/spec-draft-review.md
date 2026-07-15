# Spec-Draft Review — run-state-machine (M3)

> Independent reviewer (fresh context) round 1: REQUEST_CHANGES (single defect). Parent
> resolution round 2 documented here. Signoffs waived (2026-07-14).

## Round 1 verdict: REQUEST_CHANGES
Draft structurally excellent — all six change groups (C1-C6) covered as contiguous normative
RSM-001..030 + RSM-INV-001/002 clauses; normative voice clean; RSM prefix free; cross-refs
(replay-substrate, event-model, process-lifecycle, handler-contract, queue-model,
beads-integration, session-keeper) valid. ONE material defect: **RSM-027 consumed an imagined
M2 seam** — it cited `IN-001..IN-INV-003` / `Submit(ctx,InputMsg)(InputAck,error)` / `MsgID` /
`ErrInputUnsupported→observation-only`, which is the STALE repo-local `.kerf/` mirror. The
authoritative BENCH `agent-input.md` uses prefix **AIS**, `InputPort.SubmitInput(ctx,
InputRequest) (Ack, error)` + `CloseInput`, three-valued `Ack{Accepted|Rejected|Degraded}`, and
AIS-INV-001 output-or-stale. Every `IN-*` cross-ref dangled; the tmux path returns `Degraded`,
not `ErrInputUnsupported`; the binary acked/stale model dropped `Degraded`.

## Round 2 resolution: FIXED (parent)
RSM-027 rewritten to consume the authoritative AIS surface verbatim:
- `InputPort.SubmitInput(ctx, InputRequest) (Ack, error)` ([agent-input.md] AIS-001).
- Three-valued `Ack` honoured (AIS-003): `Accepted` advances; `Rejected` → fail-closed liveness
  edge (RSM-025); `Degraded` (written-but-unconfirmed, the interim tmux case) MUST NOT be treated
  as confirmation — the reactor keeps requiring an agent-derived signal and lets the liveness
  bound (RSM-024) terminate a never-confirmed Degraded submission. (This is precisely the
  resume-hang mechanism, now specced as the fix.)
- Dual synchronous-`Ack` + emitted `agent_input_acked`/`agent_input_stale` (AIS-004); correlation
  by the Ack's monotonic input-sequence id (no M3-minted MsgID).
- Bounded output-or-stale owned by the seam (AIS-INV-001); no M3 input-ack timer.
- RSM-024, §1, §2.2, §12 cross-refs, and the depends-on all repointed IN-*→AIS-*.
Grep-verified: zero residual `IN-*`/`InputMsg`/`InputAck`/`MsgID`/`ErrInput`/`Submit(`/
`input_ack_timeout` tokens in the draft; 17 AIS/InputPort/SubmitInput/Ack references present.

Reviewer's non-blocking notes carried: (a) RSM prefix vs design-doc RX = sanctioned
reconciliation (changelog Notes); (b) `agent-input.md` co-land dependency stated explicitly in
§12 + changelog; (c) intermediate liveness tier surfaced in RSM-024.

## HELD at spec-draft (not advanced) — coordinator arbitration required
A concurrent "Set-B" agent overwrote `04-design/` at 08:54 (its own M3-D*/RX doc set) while this
pass was mid-flight. The spec draft (the deliverable) is correct and self-consistent, but the
design-doc inputs are now two colliding sets, and Set-B's docs still carry the pre-correction M2
ownership + stale AIS vocab. Advancing to Integration would build on contested design ground and
race a live concurrent writer. Escalated to the coordinator to (1) pick the canonical design set,
(2) confirm the M2-owns + AIS corrections propagate into Set-B's runexec/ports/liveness docs, and
(3) stand down the concurrent M3 agent. Round-2 fix is complete; the pass is READY to advance the
moment the collision is arbitrated.
