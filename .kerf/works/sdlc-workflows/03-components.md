# Pass 3 — Decompose

The unit of work is one workflow = one bead (fixture + pinning spec section + scenario test). The
21 workflows group into 5 components by landing dependency + risk. Each component is a coherent
batch; the dependency edges between components form a DAG (later components depend on the runtime
proof or capability beads delivered/confirmed by earlier ones).

```
C1 smoke-first ──> C2 NOW-remainder ──> [no further dep among NOW]
        │
        └─ confirms reviewer-commit channel ──> (C2 marquee fixtures)

C4 SOON ── depends on capability beads (hk-l8rpd / hk-sdnzj / hk-69asi / hk-karlz)
C5 DEMO ── D1 depends on C1+C2 (all-NOW); D2 depends on C4 capability beads
C3 epic ── parent of all (tracking only)
```

## C1 — Smoke-first batch (4 workflows, NOW, P1)

**Requirement.** Land, in order: `dual-review-consolidate`, `implement-review-fix`,
`plan-review-loop`, `security-review-loop`. Each gets a `.dot` fixture, a README subsection, and a
scenario test. `dual-review-consolidate` must be **smoked live first** to confirm a reviewer-class
node durably commits `reviews/reviewer-*.md` and the consolidate node reads it.

**Done means.**
- `implement-review-fix.dot` round-trips and its scenario test asserts APPROVE→close,
  2×REQUEST_CHANGES→APPROVE→close, BLOCK→close-needs-attention, cap-hit→fallback.
- `dual-review-consolidate.dot` round-trips; its scenario test walks
  implement→review_correctness→review_design→consolidate and asserts consolidate's APPROVE/
  REQUEST_CHANGES(cap=2)/BLOCK routing; a **live smoke run** confirms the commit-first brief
  discipline works (reviewer findings file committed and read by consolidate).
- `plan-review-loop.dot` and `security-review-loop.dot` round-trip; scenario tests assert the
  APPROVE / REQUEST_CHANGES-loop / BLOCK paths (terminals `plan-approved`/`plan-needs-attention`
  for the planning loop; `close`/`close-needs-attention` for security).

**Interfaces.** Consumes `specs/examples/review-loop.dot` as the topology baseline and the existing
scenario-test pattern (`scenario_reviewloop_full_*`). Produces the confirmed reviewer-commit
channel that C2's marquee fixtures rely on.

## C2 — NOW remainder (10 workflows, NOW, P1)

**Requirement.** Land the remaining 10 NOW workflows: `triple-review-consolidate`,
`two-reviewer-consensus`, `plan-review-finalize`, `spec-R1-R2-cycle`, `spec-citation-cleanup`,
`decompose-review-load`, `dependency-cycle-fix-loop`, `docs-sync`, `review-route-by-failure-class`,
`characterize-refactor-verify`.

**Done means.** Each `.dot` round-trips (Layer-1) and has a scenario test (Layer-2) asserting the
terminal reached + edge sequence for its characteristic paths:
- `triple-review-consolidate` — 3-reviewer spine → consolidate verdict routing (cap=3). Lands
  after C1 confirms the commit channel; the marquee fixture proper.
- `two-reviewer-consensus` — AND-of-verdicts consolidate (APPROVE iff both approve).
- `plan-review-finalize` — terminal-by-identity through the non-agentic `finalize_plan`
  intermediate (re-validates the hk-z03e8 path).
- `spec-R1-R2-cycle` — mixed `preferred_label` + `outcome.status` cascades; R2→integrate_r1 loop.
- `spec-citation-cleanup` — commit-gated author→reviewer handoff + nested fixer↔verifier sub-loop.
- `decompose-review-load` — `br`-as-implementer-node commit-gate chain.
- `dependency-cycle-fix-loop` — non-verdict custom labels `CYCLE`/`ACYCLIC` + `failure_class`
  structural routing + fix→check back-edge.
- `docs-sync` — custom `CODE_CHANGE` label routing back to an upstream implementer (WG-019).
- `review-route-by-failure-class` — `outcome.failure_class` branch fan (scenario test uses
  synthetic outcomes; live-branch coverage gated on `hk-1xsyu` stub, tracked as a test follow-up).
- `characterize-refactor-verify` — mid-graph back-edge re-entry leaving the oracle commit untouched.

**Interfaces.** The marquee fixtures (`triple-review-consolidate`, `two-reviewer-consensus`,
`decompose-review-load`'s consolidate-variant note) depend on C1's confirmed reviewer-commit
channel. No cross-dependencies among the rest.

## C3 — Parent epic (tracking)

**Requirement.** A single epic bead (`codename:sdlc-workflows`) that the 21 workflow beads and the
DEMO/SOON beads roll up under. Carries the FINAL corpus path, the smoke-first order, and the
capability-dependency map. No code; tracking only.

**Done means.** Epic exists; all 21 workflow beads reference it (parent/epic link); closed when the
14 NOW + 2 DEMO land and the 5 SOON beads are filed-and-blocked on their capability beads.

## C4 — SOON batch (5 workflows, P2, capability-blocked)

**Requirement.** File `green-build-merge-gate`, `regression-gate`, `release-with-rollback`,
`sentry-triage-faithful`, `quality-gate-policy`. Each bead **depends on** its capability bead and is
not dispatched until that lands.

**Done means.** Each bead is filed with its dependency edge:
- `green-build-merge-gate` → `hk-l8rpd`
- `regression-gate` → `hk-l8rpd`
- `release-with-rollback` → `hk-l8rpd`
- `sentry-triage-faithful` → `hk-l8rpd`, `hk-sdnzj`, `hk-69asi`
- `quality-gate-policy` → `hk-karlz`

When the capability lands, the bead becomes ready: land the `.dot` + scenario test (live where the
capability allows; otherwise a parse-only round-trip now + a gated live test).

**Interfaces.** Consumes the capability beads' node types (tool/shell, gate, per-node prompt,
non-committing agentic). Independent of C1/C2 except via shared dialect conventions.

## C5 — DEMO arcs (2 workflows, P1)

**Requirement.** Land `plan-to-shipped-now` (D1) and `plan-to-shipped-faithful` (D2) as showcase
fixtures + a scenario test walking the happy path + ≥1 escalation.

**Done means.**
- D1 — all-NOW topology with the in-session build gate (consolidate + docs_review briefs run
  `go build ./... && go test ./...` and BLOCK on red). Depends on C1 + C2 (composes their loops).
  Scenario test walks idea→plan→spec→tasking→implement→consolidate→docs→close + one BLOCK
  escalation.
- D2 — post-parity faithful arc with tool/gate/non-committing nodes. Depends on C4's capability
  beads (`hk-l8rpd`, `hk-sdnzj`, `hk-69asi`). Lands as a fixture + parse/scenario test once the
  capabilities ship; the north-star acceptance target.

**Interfaces.** D1 composes C1+C2 workflows; D2 composes C4 SOON forms + the marquee consolidate.

## Traceability (goal → component)

| Goal (pass 1) | Component(s) |
|---|---|
| Land 14 NOW fixtures + scenario tests | C1 (4) + C2 (10) |
| Land 5 SOON gated on capability beads | C4 |
| Land 2 DEMO arcs | C5 |
| Prove marquee runs today (brief discipline) | C1 (dual smoke) → C2 (triple/consensus) |
| Smoke-first order | C1 |
| Parent epic + proposed bead set | C3 |

No component requirement lacks a goal; no goal lacks a component. Dependencies form a DAG
(C1 → C2 marquee; C1+C2 → C5/D1; C4 → C5/D2; C3 tracks all).
