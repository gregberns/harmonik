# SESSION — auto-status (hk-zueat) design pass

## Current pass & progress
- Plan jig at **research** (advanced past problem-space). Design synthesis is COMPLETE and
  written to **`DESIGN.md`** (the deliverable). Work is **shelved**, awaiting **operator
  sign-off** before any impl.
- Captain-approved as DESIGN-ONLY (2026-06-12). Surfaced to captain via comms + br comments
  on epic **hk-cq1**.

## Decisions made this session
- Reframed the bead into **two separable axes**: node Outcome (SUCCESS/FAIL/RETRY/
  PARTIAL_SUCCESS) vs reviewer verdict (APPROVE/REQUEST_CHANGES/BLOCK).
- Recommended v1 = **deterministic, daemon-authoritative pre-reviewer/outcome gate**:
  FAIL-fast + bounce-to-REQUEST_CHANGES via a deterministic policy over already-computed
  build/scenario/commit signals + optional **daemon-validated** `.harmonik/auto_status.json`
  marker. **Never** auto-APPROVE/BLOCK. AR-006-clean, no new Outcome enum values.
- Carrier: **C1 engine-inspection primary**, **C2 post-run artifact fallback** (reuse
  review.json pattern), **C3 progress-stream deferred**.

## Open questions (the 6 decisions-needed → operator)
1. Approve the gate framing? (rec yes)
2. Carrier = C1 + C2, defer C3? (rec)
3. Handler marker = daemon-validated input, not self-report (EM-005c)? (rec yes)
4. Allow policy-derived REQUEST_CHANGES, or v1 **FAIL-axis-only** (smallest safe step)? (OPEN)
5. Attribute value shape: `auto_status="<policy|mode>"` vs bool+config? (OPEN)
6. Confirm no schema/enum bump? (expected yes)

## Suggested next steps (AFTER operator sign-off)
1. Apply the chosen scope. If FAIL-only: write the WG-041/WG-002 + execution-model.md §7 + (if
   C2) handler-contract.md §I **normative** deltas into `specs/` (do this in a true daemon lull
   or via the daemon queue — do NOT dirty main while crews run).
2. Lift the parser reject for `auto_status` (`internal/workflow/dot/parser.go:699`), add the
   `Node` attribute (sibling to `non_committing`, `ast.go:166` / `core/node.go`), and the
   deterministic policy eval at `dot_cascade.go:1078-1109` (outcome) /
   `reviewloop.go:656…947` (pre-reviewer).
3. Add tests incl. an AR-006 mechanism-tag assertion (the eval point must not call an LLM —
   cf. hk-31q sensor `internal/specaudit/ar006_mechanism_no_llm_test.go`).
4. File impl beads under epic hk-cq1; dispatch serially to liet-q.

## Reading order for a new session
1. This file → 2. `DESIGN.md` (full candidates + anchors + decisions) → 3. `01-problem-space.md`
4. Spec anchors: `specs/workflow-graph.md` WG-041/WG-002/WG-031; `specs/execution-model.md`
   §4.1/§7; `specs/handler-contract.md` §I.
5. Code anchors are inline in DESIGN.md §1.

---

## COMPLETED 2026-06-13 (crew feyd) — landed via daemon feyd-q beads

The 6 open questions were resolved into **4 LOCKED decisions** (unanimous 3-agent consensus,
captain-adopted; recorded on epic-bead **hk-zueat** comments):
- **D1** deny-side only (gate may FAIL/bounce, never auto-APPROVE/BLOCK).
- **D2** C1+C2 (keep C1 engine-inspection; add C2 `.harmonik/auto_status.json` daemon-validated,
  mirroring `ReadReviewVerdict`/`review.json`; defer C3).
- **D3** C2 = daemon-validated INPUT, not authoritative self-report (EM-005c/HC-059).
- **D4** FAIL-only v1 (FAIL+failure_class only; REQUEST_CHANGES-via-policy + reviewer-loop
  re-entry DEFERRED). Boolean `auto_status="true"` kept forward-compatible with future
  `auto_status="<policy-name>"`.

**Spec deltas landed** (combined v1-doc catch-up + v2) @ **cb90555a**: new rules **WG-053**
(workflow-graph.md, supersedes WG-041 reserved-block), **HC-068** (handler-contract.md §4.2a),
**EM-068** (execution-model.md §7.5) + EM-027 amendment + workspace-model.md §6.2/§4.7 +
examples/ catch-up. Independent fidelity-reviewer APPROVED (faithful to D1-D4); captain ruled
land-COMBINED + expand file scope.

**C2 carrier implemented** (serial feyd-q beads under hk-cq1): **hk-rlwb** (`ReadAutoStatusMarker`
reader/validator, `internal/workspace/autostatusmarker.go`) → **hk-kbne** (wire into
`runAutoStatusInspection`, C1+C2 deny-side OR, `dot_cascade.go`) → **hk-zwae** + **hk-aj80**
(re-point phantom `WG-041 §I.4` anchor → WG-053 in code/test comments).

NOTE: landed via the daemon queue, NOT `kerf finalize`/branch. Kerf status advanced to **ready**.
**hk-zueat** (this work's design/impl umbrella bead) is stale-open — recommended to captain for
close (intent fully realized). Reading-order note above references the OLD anchors (WG-041 §I /
liet-q); the authoritative final landing is this section.
