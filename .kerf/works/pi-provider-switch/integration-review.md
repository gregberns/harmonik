# Integration Review — `pi-provider-switch` (kerf pass 6)

**Reviewer:** independent adversarial review
**Scope reviewed:** 06-integration.md, SPEC.md, 05-specs/C1–C6, 03-components.md,
01-problem-space.md (incl. 2026-07-08 re-scope block).

## Overall verdict: READY-TO-ADVANCE

No blockers. The seam is coherent: one linear data-flow path, one shared contract
(the additive `RunCtx` tuple with zero-value = no-override), a clean build-order DAG,
and full success-criteria traceability. Interface names/types are consistent across
C1/C2/C3/C4 with no drift. The three gaps the integration author self-flagged are all
implementation-resolvable (details below), and the one genuinely safety-critical
predicate (the hk-pkugu harness gate) has a test backstop (C5 scenario 2 + adversarial
counterfactual) that fails loudly if an implementer gets it wrong. The SHOULD-FIX
items are cheap to close and can be closed during implementation; none blocks starting.

Counts: **0 BLOCKER · 3 SHOULD-FIX · 4 NIT**.

---

## Findings

### 1. [SHOULD-FIX] Fail-loud unknown-profile routing is a comment placeholder and lacks end-to-end test coverage
**Where:** C3 spec §3 (workloop wiring); SPEC.md §7.3; C3 acceptance criterion 5.
The wiring is written as `if profErr != nil { // route through the existing claim-time
error path ... }` — a comment, not a named mechanism. No spec identifies the concrete
claim-time error path or the resulting terminal bead state (fail? retry? errored
event?). The locked requirement (undefined profile → *fail loud, does NOT launch on an
empty/wrong tuple*) is exactly the silent-launch failure mode this work must prevent,
yet the only test is `TestResolvePiProfile_UnknownProfile_FailLoud`, which asserts the
*resolver returns an error* — it does NOT exercise the workloop wiring proving the bead
does not launch. This is the one place where mechanism is under-specified AND the
end-to-end behavior is untested.
**Fix:** Name the concrete claim-time error path (the function/branch the `profErr`
plugs into) and its terminal effect on the bead; add one test (or a C5 sub-case)
proving an undefined-profile bead does not reach `LaunchSpec`.

### 2. [SHOULD-FIX] Harness-gate predicate not pinned to the concrete constant; "pi family" wording is imprecise
**Where:** C3 spec §1 ("If agentType is NOT the pi family"); SPEC.md §3 decision 3,
§7.1; integration §Build-order step 0/4.
The gate is the load-bearing no-leak guard, but the spec never pins the predicate. The
codebase has a single concrete `core.AgentTypePi` constant (used at
`harnessregistry.go:64`, `workloop.go:4094/4837`, `dot_cascade.go`, and the hk-pkugu
tests) — there is no "family" of pi types, so "pi family" is misleading and invites a
wrong/over-broad predicate on the exact safety property being guarded.
**Mitigation that keeps this non-blocking:** C5 scenario 2 (no-leak + counterfactual)
fails if the predicate is wrong.
**Fix:** Replace "the pi family" with the concrete check `agentType == core.AgentTypePi`
throughout C3/SPEC.

### 3. [SHOULD-FIX] Model-coalescing detection offered as an unresolved either/or, and one of the two options muddles resolver responsibility
**Where:** C3 spec §3; SPEC.md §7.3 ("a small `hasSingleModelLabel(...)` helper …
alternatively have `resolvePiProfile` return alongside a flag and let the caller
coalesce").
The observable precedence is fully locked (tier-1 `model:` wins, else `profile.Model`),
so this is implementation-resolvable — but the two offered mechanisms are not
equivalent in design quality: `resolvePiProfile` inspects `profile:` labels, not
`model:` labels, so having it return a model-coalesce flag couples it to a concern it
doesn't own. Leaving the either/or open invites the muddier option.
**Fix:** Collapse to the `hasSingleModelLabel(beadLabels) bool` helper (mirrors
`resolveModelField`'s exactly-one test; false for both 0 and >1 labels → coalesce to
`profile.Model`). Drop the return-flag alternative.

### 4. [NIT] C5 export-seam names are speculative
**Where:** C5 spec §Research/§Files (`ExportedResolvePiProfile`, and
`ExportedNewHarnessRegistryWithPiProfiles` "if needed"); SPEC.md §9.
Test-only plumbing in `export_test.go`; names can be settled at implementation with no
contract impact. Genuinely implementation-resolvable — not a spec hole.

### 5. [NIT] "Concurrent A→OpenRouter / B→ornith" is proven by construction, not by a concurrency test
**Where:** re-scope directive #1; C5 scenario 1 (two beads driven sequentially "in one
test"); integration §4 (stateless-per-launch, no shared mutable state).
The hermetic test proves per-bead *independence* (each bead resolves its own tuple),
and §4 argues true concurrency safety from statelessness. That is adequate, but the
word "concurrent" in the criterion is discharged by the statelessness argument + the
live operator canary, not by a parallel-goroutine test. Worth an explicit one-line note
in C5 that concurrency is established by construction (no shared mutable state) rather
than exercised, so the claim isn't over-read.

### 6. [NIT] C2 raw→typed mapper extension is hand-wavy ("find the existing mapper")
**Where:** C2 spec §Config structs ("Find the existing raw→typed mapper for
`PiHarnessConfig` in `projectconfig.go` and extend it"); SPEC.md §6.
Resolvable, but naming the concrete mapper function/line (as the rest of the spec pins
line refs) would remove a small search step and a drift risk (map not copied).

### 7. [NIT] `resolvedProfile` "non-zero" test not pinned
**Where:** C3 spec §3 ("if `resolvedProfile` is non-zero"); SPEC.md §7.3.
`PiProfileConfig` is all-string (comparable), and C2 guarantees a resolved profile has
non-empty provider/model/api_key_env, so `resolvedProfile != (PiProfileConfig{})` is
correct and can never false-positive. Fine to leave, but stating the comparison removes
ambiguity.

---

## Checks that passed clean

- **Traceability (check 1):** SPEC §12 maps all 6 success criteria + both 2026-07-08
  re-scope directives + all 4 goals to component(s) → change-spec section. Every
  criterion has a home; spot-checked #2 (fail-closed/no-leak → C4 §8 + §11.2/§11.4),
  #3 (byte-identical default → C6/§10), #5 (two wire formats → C5/§9). No orphan
  criterion.
- **Interface consistency (check 2):** RunCtx tuple field names/types identical across
  layers — C1 public `Provider/APIKeyEnv/APIKeyFile/BaseURL/API` (string); C3
  `claudeRunCtx` unexported `provider/apiKeyEnv/apiKeyFile/baseURL/api` (string) →
  projected `Provider: rc.provider` etc.; C4 reads `rc.Provider…` fallback `h.provider…`;
  C2 `PiProfileConfig.Provider/Model/APIKeyEnv/APIKeyFile/BaseURL/API`. No spelling or
  type drift.
- **No contradictions (check 3):** precedence option (b), atomic wire-format triple,
  byte-identical zero-value default, and value-opacity (shape-only, no allowlist) are
  stated identically in 03-components, C2/C3/C4, integration §Cross-cutting, and SPEC
  §3/§11.
- **Integration concerns (check 4):** build-order DAG (C1→C3, C2→C3, C1→C4, {C3,C4}→
  C5/C6) is acyclic and matches across integration §Order and SPEC §4; the
  hk-pkugu-before-C3 prereq is called out as a load-bearing manual gate; the single
  RunCtx seam and zero-value invariant are the one shared contract; error propagation
  (aggregated `PiConfigMissingError` at C2, fail-loud at C3) is described (see finding
  #1 for the C3 wiring gap).
- **SPEC faithfulness (check 5):** SPEC.md is a faithful assembly — it restates C1–C6
  and the cross-cutting invariants without adding requirements or changing the locked
  decisions. The §12 traceability table is new assembly scaffolding, not a new
  requirement.

---

## Blocker summary
None.
