# Spec Changelog — `credfence` (Credential & spend safety fence)

Maps each `05-spec-drafts/` file to its target in `specs/`, the change status, what changed, and the motivating change design. All edits to existing specs are additive or surgical extensions; no existing requirement is reversed (the budget default-flip and the credential scrub are deliberate safer-default behavior changes called out below).

## New Specs

### credential-isolation.md (NEW)
**Target:** `specs/credential-isolation.md`
**Motivated by:** `04-design/credential-isolation-design.md` (components C1/C2/C3)
**What:** New cross-cutting invariant spec (requirement prefix `CI`). Defines:
- **CI-001 sole holder** — exactly one process (the Pi cognition process) MAY hold a credential env deny-list key; the daemon and every daemon-spawned `claude` child MUST NOT.
- **CI-002 credential env deny-list** — single source of truth `{ANTHROPIC_API_KEY, ANTHROPIC_AUTH_TOKEN, CLAUDE_CODE_OAUTH*}`, named distinctly from the CHB-007 forbidden-flag deny-list.
- **CI-003 scrub** at the single `ClaudeEnvVars`/CHB-006 env-assembly boundary (symmetric with the `HARMONIK_SECRET_*` strip; no scatter of `env -u`); **CI-004** substrate handoff is an assertion point; **CI-004a** named regression test.
- **CI-005 scoped injection** (allow-list base, not blanket `os.Environ()` union) across all launch paths; **CI-005a** attach-path carve-out (operator's own shell is exempt).
- **CI-006** `supervise start` injection source (gitignored `.env` read only by `supervise start`; precedence operator-export > `.env` > fail-closed).
- **CI-007** no credential value in committed artifacts or event payloads.
Plus §5 invariants, §6 conformance, §7 OQ, §8 cross-spec coordination, Appendix A (the sibling notes it asks claude-launchspec and operator-nfr to carry).

## Modified Specs

### cognition-loop.md (MODIFIED)
**Target:** `specs/cognition-loop.md`
**Motivated by:** `04-design/cognition-loop-design.md` (component C4 + C6/C7 notes)
**What:**
- Rewrote **CL-090** from a Pi-turns-only "substrate spend" kill-switch into a **unified spend meter** summing Pi turns AND daemon-spawned `claude` cost (consumed from the existing `budget_accrual` event, [event-model.md §8.4.2]) against one shared per-day cap. Added cost-attribution, eventual-consistency, finite-default, and exhaustion-event paragraphs.
- Added **CL-090a** — per-day max-runs ceiling (counts class-F `run_started`; loss-proof backstop; resets on the same day boundary as the USD cap).
- Flipped the default per-day cap from the code's inert `Infinity` to a **finite default** (recommended 20 USD), with explicit operator opt-out for unlimited — a deliberate safer-default behavior change.
- Wired exhaustion to emit `budget_exhausted{budget_scope=handler_account}` so the existing handler-pause policy ([handler-pause.md] HP-012) halts dispatch (the C4/C5 seam).
- Added informative **CL-090b** (operator model-tier knobs), **CL-090c** (retries draw the max-runs ceiling — no third budget surface), **CL-090d** (dry-run/plan-only mode).
- Broadened §2.1 budget bullet to "both layers"; added a credential-isolation in-scope/out-of-scope note; added **CL-INV-006**, the `BudgetState` §6 type row, conformance scenario 6; added `credential-isolation`/`handler-pause`/`control-points` to `depends-on`; noted in OQ-CL-004 that the `budget_exhausted{handler_account}` event goes to the shared bus.

### handler-pause.md (MODIFIED)
**Target:** `specs/handler-pause.md`
**Motivated by:** `04-design/handler-pause-control-points-event-model-design.md` (component C5)
**What:**
- **HP-012 text UNCHANGED.** Added a clarifying note mapping `budget_scope = handler-account` to the Budget primitive `scope` value `handler_account` ([control-points.md CP-022]) and naming the cognition-loop unified meter as the producer.
- Added **§11a** — the end-to-end budget-exhaustion hard-halt path (meter → `budget_exhausted{handler_account}` → HP-012 pauses `claude` handler → `budget-paused` → existing handler-resume clears).
- Marked **§13 deferred item #3 RESOLVED** (the `budget_scope` field gap is closed by the control-points `scope`-enum extension).
- Added **Appendix A.4** listing the control-points / event-model / cognition-loop sibling amendments.

### control-points.md (MODIFIED)
**Target:** `specs/control-points.md`
**Motivated by:** `04-design/handler-pause-control-points-event-model-design.md` (component C5)
**What:**
- Extended **CP-022**'s Budget `scope` enum from `{per_role, per_run, per_state}` to `{per_role, per_run, per_state, handler_account}` — in the CP-022 prose (§4.5), the `ENUM BudgetScope` block (§6.1.4), the `BudgetPayload` RECORD comment (§6.1.4), and the §7 example-YAML `scope:` field (additive enum value; CP-001 single-primitive invariant and CP-005 per-Kind table unchanged).
- Added an INFORMATIVE note explaining the `handler_account` scope (per-handler-account ceiling / daily-quota), its handler-fatal mapping to HP-012, and the `scope`-vs-`budget_scope` field-name reconciliation. This resolves handler-pause §13 deferred item #3. The note explicitly clarifies that the cognition-loop unified per-day cap ([cognition-loop.md CL-090]) is NOT a registered CP-022 Budget instance (USD/day is not a representable `BudgetResource`); rather its exhaustion *event* carries `scope = handler_account` as a classifier that HP-012 reads — matching cognition-loop.md's narrower claim and avoiding cross-spec overreach.

### event-model.md (MODIFIED)
**Target:** `specs/event-model.md`
**Motivated by:** `04-design/handler-pause-control-points-event-model-design.md` (component C5)
**What:**
- Amended **§8.4.3 `budget_exhausted`** producer set to add `cognition-loop (flywheel)`; added optional `budget_scope`, `spent_usd`, `cap_usd` payload fields and made `run_id`/`session_id`/`attempted_dispatch_cost` optional for the account-scoped (run-agnostic) variant.
- Added an INFORMATIVE producer-set note distinguishing the per-run (agent-runner S04) variant from the account-scoped (cognition-loop) variant. Governed by the §6.4 additive-field rule + §4.6 amendment protocol. Event class O and mechanism tag unchanged.

### claude-launchspec.md (MODIFIED)
**Target:** `specs/claude-launchspec.md`
**Motivated by:** `04-design/claude-launchspec-design.md` (component C2 note)
**What:**
- Added a credential-scrub note to the §4 `baseEnv` field-table row and to the env-assembly step (step 5): env assembly removes the credential env deny-list keys ([credential-isolation.md CI-002/CI-003]) symmetric with the `HARMONIK_SECRET_*` strip; the substrate handoff is an assertion point (CI-004).
- Added a "Credential env deny-list / scrub" row to the §6 Cross-references table, kept distinct from the existing CHB-007 forbidden-flag-deny-list row. No existing requirement changed.

### operator-nfr.md (MODIFIED)
**Target:** `specs/operator-nfr.md`
**Motivated by:** `04-design/operator-nfr-design.md` (components C3, C6, C7)
**What:**
- Extended the **ON-004** "at minimum" config-inventory list to name the credfence knobs.
- Added six config-inventory entries: **ON-004b** credential injection source ([credential-isolation.md CI-006]); **ON-004c** per-day USD cap (finite default); **ON-004d** max-runs ceiling; **ON-004e** Pi model tiers (tier-3 judgment defaults Sonnet, Opus opt-in); **ON-004f** single daemon `claude` baseline default (hot-reload SHOULD-not-MUST, surface left to implementation); **ON-004g** `--dry-run` plan-only mode.
- Added **ON-008a** — the §4.3 operator-surface note: `supervise start` injects the credential from the scoped source; the `budget-paused` reason is surfaced and cleared via the existing handler-resume. All additive.

## Cross-component consistency notes
- **C4/C5 seam is consistent end to end:** cognition-loop CL-090 emits exactly the `budget_exhausted{budget_scope=handler_account}` that event-model §8.4.3 now permits the cognition loop to produce, that control-points CP-022's `scope` enum now classifies, and that handler-pause HP-012 (unchanged) consumes to pause the `claude` handler.
- **Single deny-list source of truth:** `credential-isolation.md` CI-002 is the one normative definition; claude-launchspec and operator-nfr reference it rather than restating the set.
- **Deliberate safer-default behavior changes (not regressions):** the budget default-flip (Infinity → finite, CL-090) and the credential scrub (CI-003) intentionally change default behavior because the prior behavior was the 2026-05-30 incident vulnerability; operators who want unlimited spend or key passthrough must opt in explicitly.

## Implementation-task anchors (not spec text)
The following are implementation tasks the spec text references but does not itself contain (filed/derived in pass-7):
- The `.gitignore` rule for `.env`/`*.env` and the pre-commit secret-scan hook (anchored by CI-007; the deny-list CI-002 is the scan's input). hk-pbs1u.
- The `ClaudeEnvVars` scrub branch (CI-003) and the named regression test (CI-004a). hk-f2nm1, hk-4g32m.
- The scoped Pi-env builder across `shim.go` / supervisor / (carve-out) `attach.go` (CI-005). hk-uiu98.
- The `supervise start` `.env` injection (CI-006). hk-fo9zz.
- The daemon-side unified meter feed + finite default + max-runs (CL-090/CL-090a). hk-k3f8g, hk-60csa.
- The `FLYWHEEL_MODEL_TIER*` reads + daemon-baseline knob (ON-004e/f). hk-rljho, hk-c5oxy.
- The `--dry-run` daemon mode (ON-004g) and the retry-budget folding into max-runs (CL-090c). hk-cebjc, hk-c1ah6.
