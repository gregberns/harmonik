# Integration Review — `credfence` (Credential & spend safety fence)

> Pass 6 integration. Cross-reference consistency check across all seven drafted spec changes AND the unchanged system specs they touch. Anchors were verified against the live `specs/*.md` files (not the drafts) wherever a draft links to an unchanged spec. Two control-points defects flagged in the pass-5 critical review were fixed before this pass; both are recorded in §Contradictions Found.

## Scope of this check

Drafts examined (7): `credential-isolation.md` (NEW), `cognition-loop.md`, `handler-pause.md`, `control-points.md`, `event-model.md`, `claude-launchspec.md`, `operator-nfr.md`.

Unchanged system specs read for contradiction/cross-ref validity: `claude-hook-bridge.md` (CHB-006/CHB-007 boundary), `execution-model.md` (FailureClass, §8.5), `queue-model.md` (QM-029b handler_paused), `process-lifecycle.md` (PL-018 LLM-free, PL-003a socket), `architecture.md` (§4.4 mechanism/cognition), `handler-contract.md` (§6.1 LaunchSpec, §4.7 secret redaction), `reconciliation/spec.md` (§4.4 wall-clock budget). These were inspected because the credfence seam (meter → event → control-point classification → handler-pause) and the credential scrub both reuse contracts owned by specs that are NOT being modified.

## Cross-Reference Checks Performed

Every inter-spec link in the seven drafts was resolved against the live target. Result column: OK = target anchor exists and the linked content is accurate; FIXED = a mislabel was corrected this pass.

| From (draft) | Link | Target verified | Result |
|---|---|---|---|
| credential-isolation §4.2 CI-003 / glossary | `[claude-hook-bridge.md §4.2 CHB-006]` (`ClaudeEnvVars` env-assembly) | live CHB-006 §4.2 "Required env-var schema" + `HARMONIK_SECRET_*` strip row | OK |
| credential-isolation §2.2 / CI-002 | `[claude-hook-bridge.md §4.2 CHB-007]` (forbidden-flag deny-list) | live CHB-007 §4.2 "Forbidden Claude flags" | OK |
| credential-isolation §4.1 CI-001 | `[cognition-loop.md §4.1 CL-001, §4.12 CL-100]` | live CL-001 §4.1, CL-100 §4.12 | OK |
| credential-isolation §8 / Appendix A.1 | `[claude-launchspec.md §4]` env-assembly; references table | live §4 `baseEnv` row + §4.2 step 5; references table is **§6** not §5 | FIXED (see C-3) |
| credential-isolation §4.4 CI-006 / §8 | `[operator-nfr.md §4.1 ON-004, §4.3]` | live ON-004 §4.1, §4.3 operator-control semantics | OK |
| credential-isolation §2.2 / §4 CI-007 | `[handler-contract.md §4.7]` (secret redaction, implied via CHB-006 `HARMONIK_SECRET_*`) | live HC §4.7 secret redaction | OK |
| claude-launchspec §4 baseEnv / step 5 | `[credential-isolation.md §4.1 CI-002, §4.2 CI-003, §4.2 CI-004]` | credential-isolation draft CI-002/003/004 | OK |
| claude-launchspec §6 references row | `[credential-isolation.md §4.1 CI-002, §4.2 CI-003]` | credential-isolation draft | OK |
| cognition-loop CL-090 exhaustion | `[event-model.md §8.4.3]` (producer set incl. cognition-loop) | event-model draft §8.4.3 row + INFORMATIVE note | OK |
| cognition-loop CL-090 meter feed | `[event-model.md §8.4.2]` (`budget_accrual` cost surface) | live event-model §8.4.2 (`cost_units`, `cost_basis`) | OK |
| cognition-loop CL-090 hard-halt | `[handler-pause.md §4 HP-012]` | live HP-012 §4 + handler-pause draft note | OK |
| cognition-loop §9 / CL-090 | `[control-points.md §4.5 CP-022]` (`scope` value) | control-points draft CP-022 (now incl. `handler_account`) | OK |
| cognition-loop §2.1/§2.2/CL-100 | `[credential-isolation.md §4.1 CI-001, §4.3 CI-005, §4.4 CI-006]` | credential-isolation draft | OK |
| handler-pause HP-012 note / §11a / §13 / A.4 | `[control-points.md §4.5 CP-022]`, `[cognition-loop.md §4.11 CL-090/090a]`, `[event-model.md §8.4.2/§8.4.3]` | drafts above | OK |
| handler-pause §11a | `HP-025` (submission-time validation) + `[queue-model.md §6.11a QM-029b]` | live HP-025 §4, live QM-029b (handler_paused, `-32018`) | OK |
| control-points CP-022 note | `[handler-pause.md §4 HP-012]`, `[cognition-loop.md §4.11 CL-090]`, `[event-model.md §8.4.3]` | drafts above | OK |
| control-points CP-023 (unchanged) | `[event-model.md §8.4]`, `[execution-model.md §8.5]` | live event-model §8.4, live execution-model FailureClass `budget_exhausted` | OK |
| event-model §8.4.3 INFORMATIVE | `[cognition-loop.md §4.11 CL-090]`, `[control-points.md §4.5 CP-022/CP-023]`, `[handler-pause.md §4 HP-012]` | drafts above | OK |
| operator-nfr ON-004b–g / ON-008a | `[credential-isolation.md §4.4 CI-006, §4.1 CI-001]`, `[cognition-loop.md §4.11 CL-090, §6]`, `[handler-pause.md §4 HP-012]`, `[control-points.md §4.7 CP-037]` | drafts + live CP-037 (config precedence) | OK |

No link in any draft resolves to removed or renamed content. No anchor was orphaned by these changes (all additions are new requirement IDs, new enum values, new producer-set entries, and new informative notes — nothing was deleted or renumbered, confirmed by each draft's revision-history "no existing requirement renumbered/reversed").

## Contradictions Found

Three were found; all three are resolved. The first two were caught by the pass-5 critical review and fixed at the start of this pass; finding them is itself evidence the integration discipline is working (the §6 type-block defect could only surface by reading the formal type definitions, not just the prose).

- **C-1 — control-points.md INTERNAL CONTRADICTION (BLOCKING, now FIXED).** CP-022 prose (§4.5) listed `scope ∈ {per_role, per_run, per_state, handler_account}`, but the formal `ENUM BudgetScope` block (§6.1.4), the `BudgetPayload` RECORD inline comment (§6.1.4), and the §7 example-YAML `scope:` field still listed only the original three values. An implementer reading the §6 type definitions would have rejected `handler_account` as invalid — defeating the whole point of the draft, which is to *land the enum value*. **Resolution:** added `handler_account` to the `ENUM BudgetScope` block (with an inline cross-ref to HP-012 and the CP-022 note), to the `BudgetPayload` RECORD comment, and to the §7 example-YAML `scope:` field. The enum value now lands in the type system, not in prose alone. The 0.4.1 revision-history entry and 05-changelog.md were updated to enumerate all four edit sites.

- **C-2 — control-points.md ↔ cognition-loop.md CROSS-SPEC OVERREACH (now FIXED).** The control-points CP-022 INFORMATIVE note asserted that the harmonik unified per-day spend cap (CL-090) **"is a `handler_account`-scoped Budget."** This claims CL-090 registers as a CP-022 `Budget` primitive instance. But a CP-022 Budget requires `resource ∈ {tokens, wall_clock_seconds, iterations}` (CP-022 / §6.1.4 `ENUM BudgetResource`), and CL-090 meters **USD/day**, which is not a representable `BudgetResource`. cognition-loop.md's own §9 coordination line makes only the narrower, correct claim: the exhaustion event's `scope` *value* maps to `handler_account`; it does **not** claim CL-090 is a registered Budget. The two drafts disagreed. HP-012 only needs the `scope`-value to discriminate the account-scoped (handler-fatal) variant from the per-run variant — it does not need a registered Budget. **Resolution (lower-risk, no `BudgetResource` change):** reworded the control-points note so CL-090 is **NOT** a registered CP-022 Budget; rather the cognition-loop meter is a cognition-loop-side mechanism that, on exhaustion, emits an account-scoped `budget_exhausted` event whose `scope` field carries the value `handler_account`. The control-points note now matches cognition-loop.md's claim exactly. The 0.4.1 revision-history entry and 05-changelog.md were updated to match. No `usd`/`cost` value was added to `BudgetResource` (that higher-risk change was rejected — not designed in pass-4 and not needed for the seam).

- **C-3 — claude-launchspec.md §-label mismatch (now FIXED).** The claude-launchspec draft correctly **placed** the new "Credential env deny-list / scrub" row in the live references table, which is **§6 (Cross-references)** — §5 is "Error taxonomy". But three prose references mislabeled it "§5 references table": the claude-launchspec draft's own 0.1.1 revision-history entry, the credential-isolation Appendix A.1 sibling note, and 05-changelog.md. Left unfixed, an implementer applying the credential-isolation amendments would look for a references table in §5 and not find one. **Resolution:** corrected all three prose references to "§6 Cross-references table." The row placement was already correct; only the labels were wrong.

No remaining contradictions. In particular, the `budget_scope = handler-account` (hyphen) wording in live HP-012 vs. the `handler_account` (underscore) enum value is **not** a contradiction — it is the field-name/field-value reconciliation explicitly handled by the CP-022 note, the HP-012 draft note, and the §13-RESOLVED entry: there is one field (`scope`), whose value is `handler_account`; HP-012's `budget_scope = handler-account` prose denotes that `scope`-value. All three drafts state this identically.

## Consistency Issues Found

- **Terminology — "credential env deny-list" vs "forbidden-flag deny-list".** Both the new spec and claude-launchspec take pains to keep these two deny-lists distinct (CI-002 vs CHB-007). Verified: every draft that names one names the distinction. No conflation. CONSISTENT.
- **Terminology — single `scope` field, not a parallel `budget_scope` field.** cognition-loop §9, control-points CP-022 note, handler-pause HP-012 note + §13-RESOLVED, and event-model §8.4.3 INFORMATIVE all state the same reconciliation (one field `scope`; HP-012's `budget_scope` prose is the field-value carrier). The event payload field is named `budget_scope?` in the event-model §8.4.3 row — this is the *event payload* field that carries the Budget `scope` value, which is exactly what the notes describe; the naming is intentional and consistent across all four drafts. CONSISTENT.
- **Terminology — "holder process" = the Pi cognition process.** credential-isolation CI-001 and cognition-loop CL-001/CL-100 agree the sole holder is the Pi process; the daemon and spawned `claude` children hold no credential. CONSISTENT.
- **Default-flip framing.** The Infinity→finite budget default (CL-090) and the credential scrub (CI-003) are both framed identically across cognition-loop, credential-isolation, and 05-changelog as *deliberate safer-default behavior changes* (not regressions), each justified by the 2026-05-30 incident, each requiring explicit operator opt-out for the prior behavior. CONSISTENT.
- **Conformance-count bookkeeping.** cognition-loop §7 conformance now reads "all six invariants" and lists acceptance scenario 6 (the unified-meter halt) — matching the added CL-INV-006 and the `BudgetState` §6 type row. The count was updated, not left stale. CONSISTENT.
- **`BudgetResource` enum is unchanged.** No draft adds `usd`/`cost` to `BudgetResource`; the USD meter lives entirely in cognition-loop (CL-090) and surfaces to control-points only as a `scope`-value classifier on the exhaustion event. This is the deliberate design boundary that resolves C-2 and keeps the control-points Budget primitive untouched in its `resource` axis. CONSISTENT.

## Cross-Reference Validity

All `[text](file.md §anchor)` links in the seven drafts resolve (table in §Cross-Reference Checks Performed). Verified in both directions:
- **Forward:** every link target anchor exists in the live spec or the sibling draft.
- **Reverse (no orphans):** every spec that credfence asks to carry a sibling note actually has a draft carrying it — claude-launchspec §4/§6 + revision-history (Appendix A.1/A.2 of credential-isolation), operator-nfr ON-004b/ON-008a (credential-isolation A.2), control-points/event-model/handler-pause (handler-pause A.4). No draft references a sibling amendment that does not exist; no sibling amendment is unreferenced by its requesting spec.
- The three §-label defects (C-3) that would have produced dangling references were fixed.

## Changelog Verification

05-changelog.md was checked entry-by-entry against the actual drafts after the C-1/C-2/C-3 fixes:
- **credential-isolation.md (NEW):** changelog lists CI-001..CI-007 + §5 invariants + §6 conformance + §8 coordination + Appendix A. Draft contains exactly these. MATCH.
- **cognition-loop.md:** changelog lists CL-090 rewrite, CL-090a, finite-default flip, exhaustion-event wiring, CL-090b/c/d, §2.1 broadening, CL-INV-006, `BudgetState` row, conformance scenario 6, three new `depends-on`. Draft contains all. MATCH.
- **handler-pause.md:** changelog lists HP-012-unchanged-plus-note, §11a, §13 item #3 RESOLVED, Appendix A.4. Draft contains all. MATCH.
- **control-points.md:** changelog (after this-pass edit) lists CP-022 prose + ENUM block + RECORD comment + §7 example-YAML enum extension, and the corrected INFORMATIVE note clarifying CL-090 is NOT a registered Budget. Draft now matches all four edit sites + the corrected note. MATCH (was a near-miss before C-1/C-2 — the changelog originally under-described the enum edit as prose-only and over-claimed "is this scope"; both corrected this pass).
- **event-model.md:** changelog lists §8.4.3 producer-set addition, optional `budget_scope`/`spent_usd`/`cap_usd`, optionalized `run_id`/`session_id`/`attempted_dispatch_cost`, INFORMATIVE producer-set note. Draft contains all. MATCH.
- **claude-launchspec.md:** changelog (after C-3 fix) lists §4 `baseEnv` + step-5 scrub note and the §6 Cross-references-table row. Draft contains all. MATCH.
- **operator-nfr.md:** changelog lists ON-004b–g + ON-004 "at minimum" extension + ON-008a. Draft contains all seven new IDs. MATCH.
- **Implementation-task anchors section:** the changelog correctly separates spec text from implementation tasks (`.gitignore`, pre-commit scan, `ClaudeEnvVars` scrub branch, regression test, scoped Pi-env builder, `supervise start` injection, unified meter feed, model-tier reads, dry-run, retry-budget fold) and maps each to its bead. These are exactly the non-spec-text items; none of them appears as normative requirement text in the drafts. MATCH.

Changelog is complete and accurate against the drafts after the three fixes.

## Final Assessment

After the three fixes (C-1 enum landing, C-2 cross-spec overreach, C-3 §-label), the spec corpus is coherent. The central credfence seam is a single closed loop that every touched spec describes identically:

> cognition-loop CL-090 unified meter (USD + max-runs) → emits `budget_exhausted{budget_scope=handler_account}` to the shared bus → event-model §8.4.3 registers the cognition-loop as a producer of this account-scoped variant → control-points CP-022 classifies the `handler_account` `scope`-value (now a real enum member, not prose-only) → handler-pause HP-012 (text unchanged) consumes it and pauses the `claude` handler type → cleared via the existing handler-resume surface (operator-nfr §4.3 / ON-008a).

The credential-isolation contract is orthogonal and self-contained: one deny-list constant (CI-002), one scrub boundary (CI-003 at `ClaudeEnvVars`/CHB-006, symmetric with the existing `HARMONIK_SECRET_*` strip), one assertion point (CI-004 at the substrate handoff), one scoped-injection rule (CI-005) with one carve-out (CI-005a attach), one injection source (CI-006), one committed-artifact invariant (CI-007). It reuses the existing CHB-006 env-assembly boundary rather than introducing a new one, so it adds no new subsystem seam. The only deliberate behavior changes (budget default-flip, credential scrub) are flagged as safer-defaults in every spec that mentions them, with explicit opt-out, justified by the incident. No unchanged spec is contradicted; no requirement is renumbered or reversed; the `BudgetResource` primitive is untouched. The corpus is ready for the Tasks pass.
