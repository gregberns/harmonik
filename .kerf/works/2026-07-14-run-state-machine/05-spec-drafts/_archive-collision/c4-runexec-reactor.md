# 05-Spec-draft slice / c4-runexec-reactor

> **This work drafts ONE spec** — `run-state-machine.md` (spec-id
> run-state-machine, prefix RX; see `05-changelog.md` and 00-decisions M3-D13:
> six components, one normative home, zero event-model amendment). This slice
> file is the per-component traceability map into that draft.

**Component:** C4 — the runexec machines (Dispatch + Run)
**Normative surface in `run-state-machine.md`:** RX-001 (pure core + depguard), RX-005 (Dispatch states), RX-006 (Run spine), RX-007 (timers-as-events), RX-008 (terminal state not out-param), RX-009..011 (the M2-1 input/ack contract); RX-INV-001/002
**Design doc:** `04-design/c4-runexec-reactor-design.md`
