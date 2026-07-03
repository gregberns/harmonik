# D2 Design — `failure_class` Placement on Outcome (Pass-4, phase-3-dot)

## Decision

**Framing A.** `failure_class` is a **top-level optional field on `Outcome`**, present only when `status = FAIL`, typed as the closed §8 enum `{transient, structural, deterministic, canceled, budget_exhausted, compilation_loop}`. Absent on `SUCCESS` / `RETRY` / `PARTIAL_SUCCESS`. When a handler emits `outcome_emitted` with `status = FAIL` and no `failure_class`, the daemon classifies via the existing HC-020 sentinel path (`ErrTransient` / `ErrDeterministic` / etc.) and **back-fills** the field before the edge cascade runs. Classification authority remains harmonik's (§8) — the field is a *carrier*, not a new authority surface.

## Framings considered

- **A. Top-level field on FAIL outcomes** — `Outcome.failure_class: enum | absent`. Machine-readable, cascade-readable, smallest delta.
- **B. Stuff into `notes`** — no schema change; `notes` carries a freeform sentence. Unparseable by cascade or audit tooling.
- **C. New `kind = failure` payload variant** — leverages EM-005a discriminator. Structurally clean but forces every FAIL outcome off `kind = default`, which is invasive (most FAIL outcomes today are `default`-kind).

## Rationale

- **B defeats G5 outright.** Pass-1 G5 ("failed-edge cascade routing on failure class") requires the cascade evaluator to read the class as a structured input. `notes` is freeform string with no schema; cascade cannot legally introspect it without inventing ad-hoc parsing. B is non-starter once G5 is in scope, which it is (D1 makes `failure_class` a permitted LHS).

- **C is structurally clean but invasive.** EM-005a's discriminator extension protocol exists precisely for cases like this. But: today essentially every FAIL Outcome rides on `kind = default`. Forcing them onto `kind = failure` means (a) every handler that emits FAIL must remember to set `kind`, (b) the `default` Outcome shape no longer covers the most common FAIL case, (c) consumers that read `payload.failure_class` need a different code path than consumers that read `Outcome.failure_class`. C is the right shape for *gate-decision* (D7) because gate decisions are genuinely a different Outcome class with rationale fields; it is the wrong shape for failure-class, which is a single enum that augments — not replaces — the existing FAIL surface.

- **A is the smallest delta that closes G5.** A single optional top-level field, typed against the already-locked §8 closed enum (pass-3 SUMMARY "already-resolved" item #6), present-when-status-is-FAIL. EM-005's locked field set adds one row; no `kind` discipline disturbed; the daemon back-fills from sentinel when the handler omits it, preserving HC-020 as the authoritative classification path. EM-022 N-1 readability is satisfied because the field is *optional* — a v1-reader confronted with a v2 Outcome missing the field treats it as absent and falls back to sentinel-based routing.

- **Classification authority stays with harmonik (§8).** Pass-3 SUMMARY "already-resolved" item #13 explicitly: "Failure-class classification authority is closed in harmonik's favor." A handler MAY set `failure_class` as a hint (Attractor's `meta.failure_class` precedent), but the daemon's sentinel path is authoritative — if the handler sets `failure_class = transient` but raises `ErrStructural`, the daemon overrides to `structural` and logs the disagreement to reconciliation. D2 does not regress this; the field is a *transport surface* for the §8 enum, not a *re-opening* of who classifies.

- **EM-005 is the right home, not handler-contract.** Pass-3 SUMMARY item #4 locks Outcome's normative ownership in `execution-model.md §4.1 EM-005`. The schema bump lives in EM-005, not in HC-008.

## Implications for downstream decisions

### D1 (`failure_class` as edge-LHS) — UNBLOCKED → yes

With D2 = A, `failure_class` is a structured, typed, machine-readable field on `Outcome`. The cascade can legally route on it via expressions like `outcome.failure_class == "transient"`. D1's lean ("yes, sugar over Outcome meta") is now mechanically realizable. **D1 should land as yes.**

### D4 (edge-condition LHS whitelist) — UNBLOCKED

D2 adds one row to the whitelist of routable Outcome fields. The full Outcome-field whitelist is now: `status`, `preferred_label`, `failure_class` (FAIL-only), and `kind` (discriminator inspection if D7 lands). `suggested_next_ids` remains a *cascade input*, not an LHS-routable field (it's a list, not a scalar). `context_updates.*` LHS handling is D8's territory. **D4 can be written end-to-end once D5 picks the dialect.**

### D8 (`context_updates` typing) — INFORMED

D2 narrows D8's surface: because `failure_class` is now a first-class field, D8 does NOT need to accommodate "should `context_updates.failure_class` be a registered key?" The §8 enum has a dedicated carrier; `context_updates` carries everything *else*. D8's per-workflow registered-key lean is unchanged but its scope is smaller.

### C1 (`specs/workflow-graph.md`) — node-type Outcome-status legality table

The C1 catalog table gains a `failure_class` column showing which node-types may set the field. Per D3's 4-type catalog (`agentic | non-agentic | gate | sub-workflow`): all four MAY emit FAIL with `failure_class`. `sub-workflow` propagates verbatim per EM-036a (item #11) — so the parent observes the sub-workflow's terminal-node `failure_class` unchanged.

### EM-005 schema bump

EM-005 enum/field set is locked at v1 per pass-3 SUMMARY item #4. Adding `failure_class` is an **additive** change (new optional field), which per §6.4 is a non-breaking schema bump. Increment EM-005's schema_version from v1 → v2 with additive-bump rationale: "added optional `failure_class` field on FAIL Outcomes." N-1 readability holds because v1 readers see the field as unknown-and-optional, fall through to sentinel-based classification per HC-020.

Note this is **less disruptive than D3's EM-006 bump** (which was breaking due to enum collapse). D2 is purely additive.

## Trade-offs accepted

- **Two paths to the class value.** Handler-set (hint, advisory) and daemon-classified (authoritative). The daemon-back-fill rule must be explicit in EM-005 v2 prose so handlers don't assume their hint is final. Pass-5 spec-draft owns the prose.

- **The field is present-or-absent, not always-present-with-null.** Some style guides prefer "always present, nullable." We accept the optional-absent form because (a) the §8 enum has no natural null/zero-value, (b) absent-on-non-FAIL is a stronger structural signal than null-on-non-FAIL, (c) it matches `preferred_label`'s existing absent-or-string convention.

- **No `failure_class` on `RETRY` / `PARTIAL_SUCCESS`.** A retry that ultimately fails reclassifies via §8.1 (transient → structural on cap exhaustion) — that reclassification surfaces on the eventual terminal FAIL, not on intermediate RETRY outcomes. PARTIAL_SUCCESS is by definition not a failure. Accepted.

- **Sub-workflow propagation is verbatim.** A sub-workflow whose terminal node failed with `failure_class = budget_exhausted` propagates that class to the parent's cascade unchanged (EM-036a, item #11). The parent's edges may route on it — which is desirable but means parent workflows have visibility into nested failure detail. Acceptable; D18 confirms verbatim propagation is the v1 rule.

## Open follow-up questions

These are triggered by D2 but NOT closed by it:

1. **Handler-emitted vs. daemon-classified disagreement: log-only or escalate?** When the handler sets `failure_class = transient` and the daemon's sentinel classifies as `structural`, D2 says "daemon wins, log disagreement." Whether the disagreement also triggers a reconciliation Cat-6 entry is an open observability question for pass-5. Lean: log-only at v2; promote to reconciliation if the disagreement rate is non-trivial in practice.

2. **Should `compilation_loop` be settable by handlers, or daemon-only?** §8 says `compilation_loop` requires the daemon to observe the loop signature across multiple attempts — a single handler invocation cannot legitimately self-classify as `compilation_loop`. Pass-5 should state: handlers MAY set `{transient, structural, deterministic, canceled, budget_exhausted}` as hints; `compilation_loop` is daemon-only. (Mechanism: the daemon's classifier overrides any handler-set `compilation_loop` to `structural` and logs.)

3. **Event-stream alignment.** `failure_class` exists today as a payload field on `run_failed` events (pass-3 SUMMARY item #13). After D2, both Outcome.failure_class and run_failed.failure_class exist with the same enum. Pass-5 must declare them as the *same value* — the run_failed event's class is derived from the terminal Outcome's class. No new authority surface.

4. **D7 (gate-node Outcome payload) interaction.** D2 puts `failure_class` at the Outcome top-level; D7 (pending) puts gate rationale under `payload` with `kind = gate_decision`. A gate node that denies due to budget exhaustion would emit `status = FAIL`, `failure_class = budget_exhausted` (top-level), AND `kind = gate_decision`, `payload = {policy_id, decision_actor, ...}`. The two surfaces are orthogonal — top-level field carries classification, payload carries rationale evidence. Confirm in D7.

5. **N-1 reader fallback on missing field.** v1 daemons reading a v2 Outcome ignore `failure_class` and route via sentinel only. This is correct but means a v1/v2 mixed deployment routes FAIL outcomes inconsistently. Acceptable for MVH where no v1 corpus exists; flag for the §6.4 N-1 discipline note in pass-5.

---

## Footer — Reviewer note

Reviewer sub-agent was not dispatched in this pass; the Agent tool is not available in this thread. Per D2 brief, a **fresh-context re-read pass** was performed against (a) the decision sentence, (b) the rationale bullets, and (c) the downstream-implication sections. Re-read confirmed: D2 is additive (does not contradict pass-3 SUMMARY item #4 locking EM-005's existing field set, since additive bumps are explicitly permitted by §6.4); classification authority is preserved per item #13; the §8 enum is treated as closed per item #6. Open follow-up #2 (`compilation_loop` daemon-only) was added on re-read after noticing §8.1's loop-signature requirement implies single-invocation handlers cannot self-classify. No BLOCK-grade issues. Recommend a fresh-context reviewer agent run before pass-5 spec-draft begins.
