# 05 — Changelog (spec drafts → target specs)

> Pass 5 (Spec Draft). Signoffs waived (2026-07-14).

| Target spec file | Status | Change | Motivating design |
|---|---|---|---|
| `specs/run-state-machine.md` | **NEW** | Normative home for the daemon per-bead run lifecycle: the two pure reactors (Dispatch + Run) and their states/events/actions (RSM-001..009); the eight run-lifecycle ports and the cut line (RSM-010..012); the ClockPort determinism seam (RSM-013..014); the explicit merge queue + the true critical section (RSM-015..019); the factored terminal spine + elimination of the `runSucceeded` out-param (RSM-020..023); the bounded-liveness invariants RSM-INV-001/002 + the timer-stack bound + fail-closed + the `run_stale` home (RSM-024..026); consumption (not redefinition) of the agent-input `InputPort`/`Ack` seam (RSM-027); depguard enforcement (RSM-028); parity + test conformance (RSM-029..030). Reserves prefix `RSM` in `specs/_registry.yaml`. | 04-design (see collision note) |

## Notes
- **Prefix.** The spec reserves `RSM` (free in `_registry.yaml`; only `RS` is taken). The
  cross-cutting design doc's M3-D13 pinned the placeholder `RX`; `RSM` is the landed
  reconciliation (both free; RSM is clearer against the existing `RS` = replay-substrate).
- **No `specs/event-model.md` body change in this draft.** M3 introduces NO new durable events:
  it reuses the existing `agent_ready_timeout` + terminal family and consumes M2's run-scoped
  `agent_input_submitted/acked/stale`. The one event-model touch is a citation fix: the
  spec-orphaned `run_stale` (cited today to a non-existent event-model §8.12.1) is normatively
  HOMED in `run-state-machine.md` §8 (RSM-026). Repointing the `run_stale` code-comment citation
  and adding an event-model back-reference is an implementation task (07-tasks), not a spec-body
  rewrite.
- **`specs/agent-input.md` is NOT modified; it is CONSUMED and co-lands.** M3 consumes M2's
  ratified surface (authoritative bench spec, prefix **AIS**): `InputPort.SubmitInput(ctx,
  InputRequest) (Ack, error)` + `CloseInput` (AIS-001), the three-valued `Ack`
  {Accepted|Rejected|Degraded} (AIS-003), dual synchronous-return + emitted-event delivery
  (AIS-004), and the output-or-stale bounded-liveness guarantee (AIS-INV-001). RSM-027 consumes
  these; it defines no competing input/ack type and arms no input-ack timer. `run-state-machine.md`
  has a hard co-land dependency on `agent-input.md` (the `InputPort` it consumes).
- **Intermediate liveness tier surfaced** per the change-design review note: RSM-024 states the
  full timer stack (agent-input output-or-stale seed bound → ready sub-bound → post_ready_hang →
  absolute ceiling) explicitly, not collapsed.
- **(2026-07-14, supersedes the `{Accepted|Rejected|Degraded}` note above)** Per operator-ratified
  change-spec (COORD c019), the three-valued acceptance class is retired for the **binary
  acked/stale model**. The synchronous `Ack` (AIS-003) carries a delivery outcome only:
  `Delivered` (input handed to the driver; acceptance is confirmed asynchronously) or `Rejected`
  (synchronous protocol-level refusal, structured drivers only). Positive acceptance is the async
  `agent_input_acked` event, not an `Ack` class; the never-confirmed case reaches the
  `agent_input_stale` timeout terminal (the resume-hang fix). No `Accepted`/`Degraded` enum
  members. RSM-027 consumes this binary model uniformly across the tmux paste-driven path and the
  structured (Codex app-server) driver — not a per-driver tier.

## COLLISION NOTE (escalated to coordinator, 2026-07-14)
Two M3 design-doc sets collided on the shared bench `04-design/`. A concurrent agent ("Set-B")
overwrote the `04-design/` directory at 08:54 with its own docs (`00-decisions.md` M3-D1..D14 /
prefix RX, `runexec-design.md`, `ports-design.md`, `merge-queue-design.md`,
`liveness-parity-design.md`, `c1-clockport-design.md`, `c6-terminal-spine-design.md`,
`00b-review-resolutions.md`). This changelog's design traceability therefore points at the
`RSM-NNN` clause groups in the spec draft rather than at now-overwritten per-component filenames.
Set-B's docs are comprehensive but still carry TWO defects this pass corrected: (1) the M2
ownership direction (Set-B's M3-D11 framed M3 as the contract OWNER — backwards; the planner
confirmed **M2-1 owns, M3-4 consumes**); (2) the stale repo-local `.kerf/` mirror vocabulary
(`EvInputAck` / `Submit(ctx,InputMsg)(InputAck,error)` / `MsgID`) instead of the authoritative
bench AIS surface. Both corrections are applied to the SPEC DRAFT (the deliverable, RSM-027) and
to `00-decisions.md` M3-D11; Set-B's `runexec-design.md` / `ports-design.md` /
`liveness-parity-design.md` still need the same reconciliation. The coordinator must pick the
canonical design set and confirm the concurrent M3 agent is stood down before Integration.
