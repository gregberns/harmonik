# Integration — input-ack-contract

## Cross-spec consistency after the amendment

| Claim | AIS | HC | RSM |
|---|---|---|---|
| `Ack.outcome` is binary `Delivered \| Rejected` | AIS-003, §6.2 record | HC-070, §6.1 record | RSM-027 bullet 2 |
| `Delivered` = driver handoff, not acceptance | AIS-003, AIS-005, §3 glossary | HC-070, HC-056, HC-057, §6.1 | RSM-027 bullets 2/3, RSM-024 |
| positive acceptance = async `agent_input_acked` | AIS-003, AIS-004, §8 taxonomy | HC-070, §6.4 | RSM-027 bullet 3 |
| absence of acceptance = `agent_input_stale` | AIS-INV-001 | HC-INV-008, §6.4 | RSM-027 bullets 3/6, RSM-INV-001 |
| correlation by `input_seq` | AIS-003b, §6.2 `Seq` | HC-070, §6.1 | RSM-027 bullet 4 |
| ONE timer, owned by the AIS seam | AIS-INV-001 | HC-INV-008 (seam restatement) | RSM-027 bullet 5 — consumer MUST NOT add a second |

No spec now asserts a positive synchronous ack and no spec now names an
`Accepted` or `Degraded` outcome.

## Downstream consumers unblocked

- `ARCH-01` (run-architecture contract) — its C2 design deferred the Ack shape
  pending this decision; it can now cite one reviewed vocabulary.
- `PS-01` (phase FSM) and `BR-00` — both list a reviewed
  `INPUT-ACK-CONTRACT-01` as a prerequisite.

## Code alignment

No production change is required or permitted. `internal/handler/input_port.go`
already ships the binary `DeliveryOutcome`; the corrected specs describe the
code that exists. Nothing in `internal/` or `cmd/` referenced the removed
three-valued model.
