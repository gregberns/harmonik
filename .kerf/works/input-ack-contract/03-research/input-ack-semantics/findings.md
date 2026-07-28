# Research — input-ack semantics

## Pinned inputs (blob IDs at base `176d3fe87`)

| Blob | Path |
|---|---|
| `276c3c892356f95071e820e650b2a518e34090fe` | `specs/run-state-machine.md` (v0.2.0) |
| `b9825194a330826995ba4bf4edf985532c87d2f8` | `specs/agent-input.md` (v0.1.0) |
| `8983e2db0904e7c0bb878f4dfe6d40497c382420` | `specs/handler-contract.md` (v0.8.0) |

## Chronology — drift, or a later decision?

`git log -S` over the exact clauses:

| Clause | Landed in | Date |
|---|---|---|
| RSM-027 "three-valued acceptance class" (`Accepted`/`Degraded`) | `03eab1d29` — *RT0 land RSM spec + registry reservation* | 2026-07-14 |
| AIS-003 `There is NO acceptance "class"` (binary `Delivered`/`Rejected`) | `045e937b5` — *T1 land AIS spec + registry reservation (M2-1)* | 2026-07-15 |
| HC-070 `No acceptance "class"/tier` | `045e937b5` | 2026-07-15 |
| AIS glossary `front-stop` "positive acceptance signal" | `045e937b5` | 2026-07-15 |
| AIS-005 "an earlier positive acceptance signal" | `045e937b5` | 2026-07-15 |
| HC-056 "earlier, positive, per-input acceptance signal" | `045e937b5` | 2026-07-15 |
| HC-057 "earlier, per-input acceptance signal" | `045e937b5` | 2026-07-15 |
| HC-070 "earlier, positive, per-input acceptance signal" | `045e937b5` | 2026-07-15 |

**Conclusion.** The OWNER contract landed LAST (2026-07-15) and landed BINARY.
RSM's three-valued model is a stale consumer restatement written the day BEFORE
the owner spec existed — drift, not an open design choice.

The five "positive acceptance" sentences landed in the SAME commit as the binary
model, i.e. they are intra-commit residue of the pre-hook-sourced-ack framing,
not a later override. The AIS revision-history addendum row added by that same
commit (COORD c021, operator-ratified) says verbatim: *"the binary
`Delivered`/`Rejected` + async acked/stale model is unchanged."* No later commit
reopened either question, and no operator-approved decision endorses
`Accepted`/`Degraded` or a positive synchronous ack. **Proceed.**

## What the owner contracts already say (unchanged by this work)

- **AIS-003** — `Ack` carries a binary delivery outcome: `Delivered` (handed to
  the driver; acceptance verdict arrives asynchronously) or `Rejected` (protocol
  refusal, structured drivers only). "There is NO acceptance 'class' and no
  capability hierarchy." Positive acceptance is NOT an `Ack` outcome value.
- **AIS-004** — dual delivery: the synchronous `Ack` return AND a durable
  `agent_input_acked` event (or `agent_input_stale` on the timeout terminal).
- **AIS-INV-001** — bounded output-or-stale, `ClockPort`-measured; on the
  tmux/Claude path the awaited positive signal is the Claude-hook-bridge event
  (`outcome_emitted` / `agent_ready`), never a `capture-pane` scrape.
- **AIS §6.2 `Ack` record** — `Outcome DeliveryOutcome // …no acceptance
  "class", no capability hierarchy`; `Seq uint64`; optional `Token`.
- **HC-070 / HC §6.1** — same at seam altitude: `outcome : Enum -- {Delivered,
  Rejected}`.
- **HC-INV-008** — exactly ONE terminal per `SubmitInput`:
  `Ack{Delivered | Rejected}` OR an emitted `agent_input_stale`.

## Code cross-check (read-only; nothing changed)

`internal/handler/input_port.go` already implements the binary model:

    // DeliveryOutcome is the binary delivery outcome an Ack carries (AIS-003a…)
    // Positive acceptance is NOT a DeliveryOutcome value; it is the async
    // agent_input_acked event.
    type DeliveryOutcome int
    const (
        Delivered DeliveryOutcome = iota
        Rejected
    )

Grep over `internal/` and `cmd/` finds NO `Accepted`/`Degraded` ack outcome and
no consumer of a three-valued acceptance class. Every `Degraded` hit is an
unrelated identifier (`DaemonDegradedPayload`, `daemon_degraded`, the keeper's
`Degraded bool` on a clear-sent payload). Production code therefore AGREES with
the corrected specs and contradicted the pre-correction RSM-027.

## Blocked-on evidence (why this is P0)

- `.kerf/works/run-architecture-contract/04-design/c2-process-session-lifecycle-design.md`
  — "The exact Ack shape is intentionally not stated here: HC-070 and RSM-027
  contradict each other. `INPUT-ACK-CONTRACT-01` in C6 is a blocking coordinated
  owner-spec amendment."
- `.kerf/works/run-architecture-contract/change-design-review.md` Round 2 —
  "Proposed `INPUT-ACK-CONTRACT-01` owns the coordinated HC/RSM/AIS amendment;
  RAC states no replacement enum and cannot advance to spec drafting until the
  owner-spec decision is reviewed."
