# Problem Space — standard-bead-dot

## Problem

Harmonik dispatches beads through a workflow mode. Historically the default was
`single` (no review) or `review-loop` (2-node implement↔review). The project now
has a landed **DOT engine** (codename `phase-3-dot`): a cascade driver and all node
types that can run an arbitrary per-bead process graph. The canonical such graph,
`standard-bead.dot` (start → implement → commit_gate → review → close /
close-needs-attention, with a review floor), should be the **live default** every
dispatched bead runs through — so every bead gets the full implement → gate → review
→ close cascade with a structural review floor (a bead can close only after APPROVE).

Two things were not yet normative / complete:

1. **Default wiring not specified.** The intent to make `dot` + `standard-bead.dot`
   the default was implemented in code (hk-30vlb) but never written into the system
   specs — the spec lagged the code (specs still asserted "default = single").
2. **Sub-workflow node dispatch not implemented.** The DOT engine has a
   `sub-workflow` node *type* and the supporting expansion/runner *types*, but the
   actual dispatch path is stubbed out (returns "out of scope").

## Current state (audit, 2026-06-11)

- **Default-flip: ALREADY LANDED in code** via hk-30vlb — `internal/daemon/moderesolve.go:101`
  tier-4 returns `WorkflowModeDot`; `cmd/harmonik/main.go:568` default is `dot`;
  `daemon.Start` requires an explicit `cfg.WorkflowModeDefault` (zero-value fail-closed);
  the review-loop floor (on embedded-graph load failure, never `single`) is in place.
  The operator's headline goal is therefore met in code; the spec must catch up.
- **Sub-workflow node dispatch: STUBBED** at `internal/daemon/dot_cascade.go:523`
  (`NodeTypeSubWorkflow` → failure "out of scope (separate bead)"). This is the one
  genuine remaining code gap. The expansion/runner types already exist.
- `standard-bead.dot` itself uses only landed node types (no sub-workflow node), so
  the default is independent of sub-workflow dispatch.

## Goal

1. Make the landed default-flip + the sub-workflow-dispatch contract **normative** in
   `specs/` (spec-first).
2. Implement sub-workflow node **dispatch** (the one code gap).
3. Verify the landed default conforms to the now-normative spec; remove stale text.

## Non-goals

- Re-implementing the DOT engine (landed) or the default-flip (landed via hk-30vlb).
