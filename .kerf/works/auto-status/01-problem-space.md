# 01 — Problem Space: real auto_status (hk-zueat)

## Summary

Today `auto_status` is **reserved / not-accepted** on a workflow node (workflow-graph.md
WG-041). Non-SUCCESS Outcomes (FAILURE, REQUEST_CHANGES) are produced **only** by the
reviewer (human/LLM). We want the engine to **derive** a non-SUCCESS Outcome
**deterministically** — without invoking an LLM (AR-006 ZFC) — from either:
- **(a) work-product inspection** (e.g. test/lint/build results the engine already has), or
- **(b) a structured marker** embedded in the handler's output stream (e.g. `{"status":"FAILURE",...}`).

This is a **design pass** that produces candidates; implementation is deferred and gated
on operator sign-off.

## Goals

- Candidate(s) for the **marker carrier**: progress-stream NDJSON message type vs post-run artifact.
- Decide the **derivable set**: which Outcome values the engine can derive vs which must remain human.
- Decide the **reviewer-loop interaction**: does a marker-derived REQUEST_CHANGES re-enter the
  reviewer path or short-circuit it?
- Candidate spec deltas (non-normative, for review) to workflow-graph.md §4, handler-contract.md §I,
  execution-model.md §7.

## Non-goals

- Implementation (deferred; needs operator sign-off).
- Writing normative deltas into `specs/` or running `kerf finalize` (candidates only).
- Changing `non_committing` semantics — `auto_status` is an orthogonal node attribute
  (non_committing controls commit-or-not for SUCCESS; auto_status controls non-SUCCESS derivation).

## Constraints

- **AR-006 ZFC (load-bearing):** a mechanism-tagged evaluation point MUST NOT invoke an LLM.
  auto_status derivation must be deterministic (exit codes, structured markers, file presence) —
  no keyword/heuristic semantic judgment.
- **Backwards-compat:** auto_status is currently reserved; default behavior MUST be unchanged when
  the attribute is absent.
- Must fit the **existing Outcome enum** and the existing node-transition graph (dot workflow).

## Success criteria (for the design pass)

- A design doc presenting, per design question, ≥1 candidate (ideally 2 compared) with tradeoffs
  anchored to current code/spec (file:line / requirement-id).
- An explicit **decision-needed** list the operator can act on (go/no-go + which option).
- Captain can relay it to the operator without re-deriving context.

## Source

- Bead hk-zueat (deferred from attractor-parity v1 / hk-gv5n5).
- Captain directive 2026-06-12 (design-only, surface candidates + decision-needed).
