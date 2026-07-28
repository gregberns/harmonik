# Components — input-ack-contract

One component. The change is a single coordinated amendment across three files;
splitting it would let the three specs land out of agreement, which is the exact
defect being corrected.

## C1 — input-ack semantics

| Spec | Role | Leased surface |
|---|---|---|
| `specs/agent-input.md` (AIS) | **owner** — port, `Ack`, async events, timer, output-or-stale | §3 `front-stop` glossary entry; AIS-005; version metadata; one revision row |
| `specs/handler-contract.md` (HC) | **owner** — seam-level restatement | HC-056 / HC-057 / HC-070 "Front-stop" paragraphs; version metadata; one revision row |
| `specs/run-state-machine.md` (RSM) | **consumer** — reactor consumption and routing only | RSM-027 (in place), RSM-024 resume-seed bullet, revision history |

Out of the component (explicitly): the `Ack` record itself, AIS-003/AIS-004
bodies, HC-069, HC-INV-008, AIS-INV-001, `event-model.md` payloads, all Go.
