# Spec-Draft Review — `credfence`

> Autonomous self-review (no human reviewer present; user delegated all decisions per the assessment doc). Checks the pass-5 done-criteria from `kerf show credfence`. One round.

## Criteria check

| Criterion | Status | Note |
|---|---|---|
| One draft file per target spec, named to match | PASS | 7 drafts: credential-isolation.md (new), cognition-loop.md, handler-pause.md, control-points.md, event-model.md, claude-launchspec.md, operator-nfr.md — each maps 1:1 to `specs/`. |
| Existing-spec drafts contain the FULL updated file (not a diff) | PASS | The 6 modified drafts were produced by copying the live `specs/<file>.md` then applying surgical edits, guaranteeing all unchanged content is preserved verbatim. cognition-loop.md was hand-written and verified to contain §4.1–§4.12 in full (placeholder-grep = 0). |
| New-spec draft follows project conventions | PASS | credential-isolation.md uses the front-matter block, §1–§9 structure, `CI-###` requirement IDs, an Appendix-A amendment list, and a revision-history table — mirroring `handler-pause.md` / `claude-launchspec.md` cross-cutting-invariant pattern (research F4). |
| Every change-design target state reflected in draft text | PASS | C1/C2/C3 → CI-001..CI-007; C4 → CL-090 rewrite + CL-090a; C5 → CP-022 scope enum + event-model §8.4.3 producer + HP-012 note/§11a/§13-resolved; C6 → ON-004e/f + CL-090b; C7 → ON-004g + CL-090c/d. Verified by per-area traceability tables in each 04-design file. |
| No spec content added without a backing change design | PASS | Each added requirement traces to a 02-components row + a 04-design target-state. The implementation-task anchors (`.gitignore`, hooks) are explicitly NOT spec text (changelog §Implementation-task anchors). |
| No existing content accidentally removed/altered beyond the design | PASS | Copy-then-surgical-edit method; edits are additive (new requirements, notes, enum value, producer entry) or HP-012-unchanged-plus-note. The only intentional behavior changes (budget default-flip, scrub) are flagged in the changelog as deliberate safer-defaults. |
| Spec text is normative, not rationale | PASS | Drafts say "the system does X" (MUST/SHOULD). Rationale lives in 04-design. Informative notes are explicitly tagged INFORMATIVE / "(informative)". |
| Cross-references valid and consistent | PASS | The C4/C5 seam is closed-loop: CL-090 emits `budget_exhausted{budget_scope=handler_account}` ↔ event-model §8.4.3 permits cognition-loop producer ↔ control-points CP-022 `scope` enum classifies it ↔ handler-pause HP-012 consumes it. credential-isolation Appendix A ↔ claude-launchspec §4 note + operator-nfr ON-004b/ON-008a. All sibling §-anchors verified against the live files. |
| Draft filenames match target spec files exactly | PASS | See file list above. |
| 05-changelog.md accounts for every changed draft with traceability | PASS | Changelog covers all 7 drafts with target, status, what-changed, and motivating 04-design file. |
| Formatting consistent with existing specs | PASS | Modified drafts inherit the source files' formatting; the new spec follows the corpus front-matter + section conventions. |

## Validation / acceptance test beads (required before integration)

Two substantially-changed/introduced spec areas → 2 beads each:

- **credential-isolation.md (new):** scenario `hk-24d72` (spawned claude never receives a deny-list key — CI-003/CI-004a); exploratory `hk-96s75` (`supervise start` authenticates Pi from gitignored `.env` with no daemon leak — CI-006/CI-001).
- **cognition-loop.md (CL-090 unified meter):** scenario `hk-c7lxc` (unified meter halts on per-day cap AND on max-runs — CL-090/CL-090a); exploratory `hk-0p9so` (finite default + `FLYWHEEL_BUDGET_USD_PER_DAY=unlimited` opt-out — CL-090/ON-004c).

The control-points / event-model / handler-pause edits are additive amendments in service of the cognition-loop CL-090 seam (no standalone new operator surface); the cognition-loop scenario bead `hk-c7lxc` exercises the full meter→event→pause path end to end, so they are covered transitively rather than with separate beads.

## Verdict

APPROVE. All pass-5 criteria pass; the four required test beads are filed and labeled. Advance to Integration.
