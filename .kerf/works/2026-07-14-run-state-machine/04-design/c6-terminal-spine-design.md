<!-- RECONCILE 2026-07-15: id vocabulary. This design doc predates the RX->RSM rename and
     uses RX-* ids from the superseded Set-A draft lineage. RX numbering is NOT 1:1 with the
     normative RSM ids (verified: RX-INV-003 == RSM-INV-001; RX-020 != RSM-020). The normative
     spec specs/run-state-machine.md (RSM) is AUTHORITATIVE; treat RX-* here as historical only. -->

# 04-Design / c6-terminal-spine — the 4×→1 factoring slice (C6)

> Component slice doc. The full design lives in
> `c4-runexec-reactor-design.md` §4 (the `Run` machine's Gating→Merging→
> Finalizing tail — C4 and C6 were designed together); pins: `00-decisions.md`
> M3-D8, M3-D9.

**Decision summary:** the four open-coded merge/close blocks (workloop.go
`:3860–:3935`, `:4103–:4164`, `:5231–:5275`, `:5277–:5324`) and the
6×-duplicated close ladder become the ONE `Run` tail; the distinct ENTRY
conditions stay distinct events; exact summary/reason strings are preserved as
event data (parity). Answers to the decompose C6 questions:
(a) exit-0 auto-close vs agent_completed: research proved near-byte-identical
bodies (labels only) — one spine, two entry events, strings preserved;
(b) shutdown-drain stays a DISTINCT terminal edge (bgCtx + no sessiondata =
effector policy for that batch);
(c) `emitDone`'s captured state: success/summary/runTipSHA → Run terminal
state; `emitRunCompleted` → `ActEmitRunTerminal` (ctx-swap in the effector);
`sessiondata.Collect` stays a shell effect. `runSucceeded *bool` deleted —
the wrapper reads the terminal (M3-D8).

**Tasks:** RT6 (machine tail + L0), RT9 (the ×4 call-site unification +
out-param removal). The `hclifecycle.Machine` stays a projection (RX-020).
