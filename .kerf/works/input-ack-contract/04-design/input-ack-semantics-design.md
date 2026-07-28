# Change Design — input-ack semantics

## Decision

Keep the OWNER model exactly as it stands (binary `Ack`, async acceptance) and
correct the consumer plus the five stale owner restatements. No replacement
enum is introduced; no new requirement ID is minted; nothing is renumbered.

## The contract, stated once

| Observation | Kind | Route |
|---|---|---|
| `Ack{Delivered}` | synchronous | driver handoff recorded; positive acceptance PENDING — await the correlated async terminal |
| `Ack{Rejected}` | synchronous | fail closed (RSM-025) |
| `agent_input_acked` (correlated) | asynchronous | positive acceptance — advance the pending transition |
| `agent_input_stale` (correlated) | asynchronous | fail closed (RSM-025) |

Totality: those four rows cover every input-seam observation. None is a silent
no-op (RSM-INV-002).

Correlation and consumption, on one `input_seq`:
1. consume the FIRST synchronous outcome exactly once;
2. after `Delivered`, the FIRST correlated asynchronous terminal wins;
3. drop only repeated or late observations for an already-resolved `input_seq`;
4. never drop the first `agent_input_acked` merely because `Delivered` was seen;
5. no second timer — the bound is AIS-INV-001's, owned by the AIS seam.

## Ownership (preserved, not moved)

- **AIS** owns: the `InputPort`, the `Ack` record, the async events, the timer,
  and the output-or-stale definition.
- **HC** restates the same seam contract at seam altitude.
- **RSM** owns ONLY reactor consumption and routing. It must not redefine the
  input timer, the port, the acceptance type, the stale terminal, or the event
  payload.

## Edits

### `specs/run-state-machine.md` (consumer)

- **RSM-027** amended in place. Bullet 2 (three-valued class) is replaced by two
  bullets — the binary synchronous outcome, then the async acceptance verdict
  with the totality statement. Bullet 3 gains the explicit `input_seq`
  consumption rule. Bullet 4 gains "MUST NOT add a second input timer". Bullet 5
  drops "never-confirmed `Degraded`" for the `Delivered` + `agent_input_stale`
  formulation.
- **RSM-024** resume-seed bullet reworded so a `Delivered` return alone no
  longer reads as resolving the seed, citing AIS-003 + AIS-004 + AIS-INV-001 as
  the composite authority (AIS-INV-001's headline terminal set, read alone,
  appears to say the opposite of its own body — see `review.md` F1).
- **RSM-INV-001, §1, §2, §13** inspected: none implies a positive synchronous
  ack and none mentions `Degraded`. Left untouched (the card scopes this to
  "wherever they imply", and they do not).
- Version `0.2.0` → `0.2.1`; new `## 14. Revision history` section (the spec had
  none) carrying one dated row.

### `specs/agent-input.md` (owner, leased prose only)

- §3 glossary `front-stop`, and **AIS-005**: the synchronous `Ack` becomes an
  earlier DELIVERY-HANDOFF observation; `agent_input_acked` is named as the
  positive acceptance and `agent_input_stale` as its absence. The front-stop
  rule (composes in front of, does not remove or weaken `agent_heartbeat` /
  commit-poll) is preserved.
- Version `0.1.0` → `0.1.1`; one revision row.

### `specs/handler-contract.md` (owner, leased prose only)

- The **HC-070**, **HC-056**, and **HC-057** "Front-stop composition" paragraphs
  get the same delivery-handoff correction, preserving each one's rule (does not
  gate first-work dispatch / does not satisfy the `agent_ready` gate / does not
  replace the heartbeat + silent-hang guard).
- Version `0.8.0` → `0.8.1`; one revision row. `status: reviewed` unchanged.

## Explicitly NOT changed

`Ack` record (either spec), AIS-003/AIS-004 bodies, HC-069, HC-INV-008,
AIS-INV-001, event payloads and registration (`event-model.md`), the bounded
window value, any production Go or test, the spec registry, `TASK-INDEX.yaml`,
any other task card.

## Risk

Low. The correction moves the consumer TOWARD the owner contract and toward the
already-shipped Go (`internal/handler/input_port.go`), so it removes a
contradiction rather than creating one. Rollback is a single-commit revert of
three spec files plus this work's artifacts.
