# DESIGN — real auto_status (hk-zueat)

**Status:** design candidates, DESIGN-ONLY. Impl deferred, gated on operator sign-off.
**Lane:** liet / epic hk-cq1 / kerf work `auto-status`.
**Date:** 2026-06-12.

---

## 0. Key clarification (reshapes the whole design)

The bead asks to "derive a non-SUCCESS Outcome (e.g. FAILURE, REQUEST_CHANGES)." Research
shows those two examples live on **two different layers**, and conflating them is the main
source of design risk:

- **Axis A — node `Outcome.Status`** enum: `SUCCESS | FAIL | RETRY | PARTIAL_SUCCESS`
  (`internal/core/outcomestatus.go:12`). This is what every node emits. There is no
  `FAILURE` and no `REQUEST_CHANGES` value here.
- **Axis B — reviewer verdict**: `APPROVE | REQUEST_CHANGES | BLOCK`, consumed only by the
  **review loop** (`internal/daemon/reviewloop.go:1298-1371`). REQUEST_CHANGES lives here.

So "real auto_status" is really **two separable features**. The design treats them
separately and recommends scoping v1 deliberately.

---

## 1. Current state (anchored)

- `auto_status` is **reserved-and-rejected** at parse time today: declaring it is a hard
  ingest error (`internal/workflow/dot/parser.go:699`), per **WG-041** (`specs/workflow-graph.md:187`).
- `non_committing` is the only sibling axis today (commit-or-not). It is a bool on the AST
  `Node` (`internal/workflow/dot/ast.go:166`), parsed at `parser.go:683`, enforced at
  `internal/daemon/dot_cascade.go:1096`. A `non_committing` clean exit yields `SUCCESS`; the
  engine does **not** inspect any work-product or marker today (WG-041).
- The handler already emits a typed **NDJSON progress stream** (HC-007/HC-007a, 1 MiB line
  cap) whose **final `outcome_emitted` message carries the Outcome struct** (HC-008;
  `internal/handlercontract/progressstream_hc007.go:26`, watcher `watcher_hc011.go:433`).
  Unknown message types are ignored, so the stream is additively evolvable.
- The daemon **already reads a post-run artifact** for verdicts: `.harmonik/review.json` via
  `workspace.ReadReviewVerdict` (`internal/daemon/reviewloop.go:1229`,
  `internal/workspace/reviewverdict.go`), schema-validated, emitted as `reviewer_verdict`.
- The engine **already derives outcomes from deterministic, LLM-free signals**:
  - commit presence / HEAD advance (`internal/daemon/workloop.go:3826`),
  - `Refs:`-already-landed check (`workloop.go:3042`),
  - post-merge `go build ./... && go vet ./...` gate (`workloop.go:4127`, emits
    `merge_build_failed` + rollback),
  - pre-merge **scenario gate** `go test -tags=scenario` (`internal/daemon/scenariogate.go:78`,
    parses `--- FAIL`, fail-open on infra noise).
- **AR-006 compliance today:** zero LLM calls in the outcome-derivation path — all string/
  exit-code/git comparisons. The only LLM-capable hook is the pluggable `GateEvalFunc`
  (`internal/handler/gate_dispatch.go:124`), which is delegated, not hardcoded.

---

## 2. Design Question 1 — marker carrier

What carries the signal that drives a non-SUCCESS derivation?

| Candidate | Mechanism | New machinery | When available | Trust model |
|---|---|---|---|---|
| **C1-inspect** (engine work-product inspection) | daemon inspects build/test/scenario/commit signals it already computes | none for existing gates; new gate per new signal (lint, junit) follows scenario-gate pattern | post-run / at gate | **daemon-authoritative** (no handler trust) — best AR-006 + EM-005c fit |
| **C2-artifact** (post-run `.harmonik/auto_status.json`) | handler writes a structured status file; daemon reads it post-session | low — reuse `ReadReviewVerdict` pattern | after session closes | handler-supplied **input**, daemon validates against a deterministic policy |
| **C3-stream** (new `status_marker` NDJSON msg or reuse `outcome_emitted`) | handler emits a typed status message mid-run | medium — new watcher type + bus mapping (or extend outcome_emitted) | **during run** (before outcome_emitted) | handler-supplied; needed only if status must gate routing *during* the run |

**Recommendation:** **C1-inspect as primary** (it is daemon-authoritative, deterministic,
AR-006-clean, and most of the plumbing exists), with **C2-artifact as the handler-supplied
fallback** for signals the engine can't natively inspect (custom test runners, per-language
results) — reusing the proven `review.json` pattern. **C3-stream only if** a future need
requires status to influence routing *before* `outcome_emitted` (none identified now).
Critically: a handler-supplied marker (C2/C3) should be an **input the daemon validates via a
deterministic policy**, never an authoritative self-report — EM-005c keeps classification
authority with the daemon.

---

## 3. Design Question 2 — which values are derivable vs always-human

| Target value | Layer | Engine-derivable deterministically? | Basis |
|---|---|---|---|
| `SUCCESS` | Outcome | ✅ yes | git HEAD + exit code (already done) |
| `FAIL` (+ `failure_class`) | Outcome | ✅ yes | sentinel exit codes (HC-020), build/vet gate, scenario gate |
| `RETRY` / `PARTIAL_SUCCESS` | Outcome | ⚠️ handler-only today | no deterministic engine signal defined |
| reviewer `APPROVE` | verdict | ❌ semantic (cognition) | requires code-quality judgment — AR-006 forbids LLM at a mechanism eval point |
| reviewer `REQUEST_CHANGES` | verdict | ⚠️ only via a **codified deterministic policy** | e.g. `lint_errors>0 → REQUEST_CHANGES`; mechanism-legal *iff* the rule is a deterministic evaluator, not semantic judgment |
| reviewer `BLOCK` | verdict | ❌ semantic | same as APPROVE |

**Recommendation:** scope `auto_status` to the **deterministically-derivable set only**:
`SUCCESS` and `FAIL`+`failure_class` on the Outcome axis, and `REQUEST_CHANGES` on the
verdict axis **only when expressed as a deterministic policy over structured signals**.
**Never auto-derive APPROVE or BLOCK** (semantic → would violate AR-006 if LLM-driven, or be
unsafe if heuristic). No new Outcome enum values are required (reuse `FAIL`+`failure_class`).

---

## 4. Design Question 3 — reviewer-loop interaction

- A derived **FAIL** (Outcome axis) is **terminal** — it never enters the review loop (FAIL
  ends the run). No reviewer interaction needed.
- A derived **REQUEST_CHANGES** (verdict axis) **re-enters the existing loop naturally**: the
  verdict-routing switch (`reviewloop.go:1299`) treats all verdict sources identically —
  increment `iterationCount` (`:1363`), write `reviewer-feedback.iter-N.md`, `continue` at the
  loop top, re-dispatch the implementer with `--resume` (cap = 3, EM-015e). A marker/inspection
  source slots in at the **post-implementer-exit / pre-reviewer-dispatch** point
  (`reviewloop.go:656 … :947`).
- A derived **APPROVE that short-circuits** the reviewer would require a **new bypass flag**
  (an architectural addition, not a natural extension) — and removes the human/LLM judgment
  that is the reviewer's whole point.

**Recommendation:** allow a deterministic **pre-reviewer gate** that can (i) **FAIL-fast**
(terminal) or (ii) **bounce to REQUEST_CHANGES** (re-enter the implementer, *skipping* the
expensive reviewer dispatch for that iteration) based on C1-inspect / C2-artifact signals.
**Do NOT auto-APPROVE / short-circuit the positive path** in v1 — the reviewer stays the sole
APPROVE authority. This captures the cost win (cheap deterministic failures fail fast / bounce
without burning a reviewer) while preserving review integrity.

---

## 5. Recommended v1 design (synthesized)

> `auto_status` becomes an optional agentic-node attribute that enables a **deterministic,
> daemon-authoritative pre-reviewer/outcome gate**. When set, after the implementer exits the
> daemon evaluates work-product signals it already computes (C1) plus an optional
> handler-supplied `.harmonik/auto_status.json` marker (C2) **through a deterministic policy**,
> and may emit `FAIL`+`failure_class` (terminal) or bounce the review loop to `REQUEST_CHANGES`
> (re-enter implementer). It never auto-emits `APPROVE`/`BLOCK`. Default behavior is unchanged
> when the attribute is absent.**

This is AR-006-clean (deterministic only), needs **no new Outcome enum values**, reuses the
`review.json` artifact pattern and the existing verdict-routing loop, and keeps the daemon as
the classification authority (EM-005c).

---

## 6. Candidate spec deltas (NON-NORMATIVE — for review, not for `specs/`)

- **workflow-graph.md §4 (WG-041 / WG-002):** lift the reserved-and-rejected status; define
  `auto_status` as an optional agentic-only attribute; its accepted value is a *policy
  reference / mode* (not `true`), and enumerate that it may yield `FAIL` (Outcome) or
  `REQUEST_CHANGES` (verdict) but never `APPROVE`/`BLOCK`. Update the WG-031 reserved-attribute
  table. Keep `non_committing` orthogonal.
- **handler-contract.md §I:** add the optional **`.harmonik/auto_status.json` artifact**
  schema (status + failure_class + optional structured signals) as a *handler-supplied input*,
  explicitly noting the daemon validates it via a deterministic policy and retains
  classification authority (cross-ref EM-005c). Optionally reserve a future `status_marker`
  progress-stream type but mark it out-of-scope for v1.
- **execution-model.md §7:** specify the engine-side derivation step: where in the dispatch /
  review-loop path the deterministic policy runs (`dot_cascade.go:1078-1109` for outcome,
  `reviewloop.go:656…947` for pre-reviewer), the deterministic-policy contract (inputs →
  {pass | FAIL+class | REQUEST_CHANGES}), and the AR-006 mechanism-tag on that eval point.

---

## 7. Decision-needed for operator (captain relays)

1. **Scope:** approve the "deterministic pre-reviewer/outcome gate" framing (FAIL + bounce-to-
   REQUEST_CHANGES; **no auto-APPROVE/BLOCK**)? — *recommended.*
2. **Carrier:** C1-inspect primary + C2-artifact fallback, defer C3-stream? — *recommended.*
3. **Handler trust:** confirm a handler-supplied marker is a daemon-**validated input**, not an
   authoritative self-report (EM-005c)? — *recommended yes.*
4. **REQUEST_CHANGES via policy:** allow deterministic-policy-derived REQUEST_CHANGES at all,
   or restrict v1 to the **FAIL axis only** (simplest, lowest-risk)? — *open; FAIL-only is the
   smallest safe first step.*
5. **Attribute value shape:** `auto_status="<policy-name|mode>"` vs a boolean + separate policy
   config — which fits author ergonomics? — *open.*
6. **Confirm no schema/enum bump** (reuse FAIL+failure_class, reuse REQUEST_CHANGES) — *expected
   yes per R1; confirm.*

---

## 8. Compliance / compat notes

- **AR-006 ZFC:** every derivation path above is deterministic (exit codes, git state,
  schema-validated artifact, policy evaluator). The mechanism eval point must be tagged and
  must NOT call an LLM — consistent with the new AR-006 sensor (hk-31q,
  `internal/specaudit/ar006_mechanism_no_llm_test.go`).
- **Backwards-compat:** attribute absent ⇒ identical to today. No change to `non_committing`.
- **Sequencing:** if the operator wants the smallest first step, ship Decision-4 = FAIL-only
  (C1-inspect), defer REQUEST_CHANGES-via-policy and C2-artifact to a v2.
