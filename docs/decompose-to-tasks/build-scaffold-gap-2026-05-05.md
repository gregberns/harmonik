# Build/Test Scaffolding Gap — Tracking-Bead Filing

**Date:** 2026-05-05
**Author:** scaffolding bead-authoring agent
**Source gap:** `docs/foundation/phase-1-readiness-gap-analysis.md` §B2 (BLOCKER), §B4 (MAJOR), §B5 (MINOR), §C2 (MAJOR)
**User clarification (2026-05-05):** local build + test must be fast and easy. **CI infrastructure is NOT needed.** Locked decision "agent-reviewer-every-commit" runs LOCALLY (lefthook hook + Makefile target) — not as a CI workflow.

## Existing scaffold-bead inventory

A live `br --json list --limit 0` scan against the corpus (878 issues at the time of this filing) found **zero** beads tracking the on-disk build/test scaffold artifacts named in the gap analysis:

- `Makefile`, `go.mod`, `.golangci.yml`, `lefthook.yml` — title-search returned 0 hits.
- `coverage-gate`, `forbid-import`, `agent-reviewer` (skill) — title-search returned 0 hits.
- `check-fast` / `check-full` — 0 hits in title; appears only in spec-prose references inside `quality-checks.md` text (not corpus tasks).

Adjacent matches that surface "build" or "infrastructure" but are NOT the scaffolding gap:
- `hk-63oh.62` — Cat 0 reconciliation taxonomy ("Infrastructure unavailable") — runtime taxonomy bead, not project-build infra.
- `hk-8mup.19` — PL-010 degraded-state on Cat 0 (runtime).
- `hk-hqwn.59.71` — `infrastructure_unavailable` event-row (runtime).
- `hk-sx9r.7` — commit-hash via build-time ldflags (related but specific to operator-NFR runtime stamping; does not author the Makefile).

Conclusion: **the scaffolding gap is unowned.** The bootstrap subset (291 beads, `scope:bootstrap`) presumes these files exist; none of them does, and none has a tracking task.

Classification: zero contract beads, zero implementation beads, zero scaffold-related beads of any kind for this surface.

## Gaps identified

Per the user's framing — "compile and run tests, fast and easy" — and the locked decisions in `build-practices.md` + `quality-checks.md`:

1. **`go.mod` + initial `go.sum` + `.gitignore` extension.** Foundation; nothing else can compile without it.
2. **`Makefile` with the three-tier gauntlet.** `build`, `test`, `check-fast`, `check`, `check-full`, `lint`, `agent-review`, `tools`. Local-only; CI parity preserved as future-proofing but not wired now.
3. **`.golangci.yml` v2** with the explicit linter list from `quality-checks.md` §Linter meta-runner + the **depguard component matrix** (per `subsystem-organization.md`) including the **PL-INV-002 LLM-SDK ban** in the daemon transitive closure.
4. **`lefthook.yml`** wiring local pre-commit (`make check-fast` + `make agent-review`), pre-push (`make check`), and commit-msg (Conventional Commits + required trailers).
5. **`scripts/coverage-gate.sh`** (or small Go tool) enforcing **95% core / 90% floor / <0.3% regression** per the locked decision.
6. **Test-helper scaffold** — `internal/testhelpers/` package + `test/{scenario,integration,crash}/` build-tagged stubs so `make check-full` doesn't fail on missing tag-gated paths. Keeps cluster A from reinventing helpers.
7. **`tools/forbid-import/`** — third-party allowlist enforcement (a protected rule file per `quality-checks.md` §Agent-enforceability item 5). Reinforces the depguard LLM-SDK ban transitively.
8. **`BUILDING.md`** — minimum dev-onboarding doc (clone → install tools → run tests). Makes the user's "fast and easy" criterion provable.

Explicitly **NOT** included (per 2026-05-05 user clarification):
- `.github/workflows/ci.yml` or any other CI runner config.
- Remote runners, deploy pipeline, GoReleaser wiring (release process is documented in `build-practices.md` §Release process but is post-MVH per "1.0 only after foundation complete").
- The `agent-reviewer` skill itself — covered by a separate operational-skills meta-epic per phase-1-readiness-gap-analysis §A4 (`p1-skill-agent-reviewer`); the `make agent-review` target here can stub-call until the skill bead lands.

## New beads filed

All under new meta-epic **`hk-pvcs`** ("Build/test scaffolding meta-epic — local Makefile, golangci, lefthook, coverage tooling"), labels `kind:meta-parent`, `phase:0`, `tag:meta`, `scope:bootstrap`. All children inherit `kind:workflow`, `phase:0`, `tag:meta`, `scope:bootstrap` (mandatory per mission), status `draft`, priority `P2`.

| Bead | Title |
|---|---|
| `hk-pvcs.1` | Initialize go.mod + initial go.sum + extend .gitignore |
| `hk-pvcs.2` | Author Makefile with three-tier check targets + build/test/lint/agent-review helpers |
| `hk-pvcs.3` | Author .golangci.yml with depguard component matrix + LLM-SDK ban (PL-INV-002) |
| `hk-pvcs.4` | Author lefthook.yml wiring local pre-commit + pre-push to make targets |
| `hk-pvcs.5` | Author coverage-gate script enforcing 95%-core / 90%-floor / <0.3% regression |
| `hk-pvcs.6` | Author test-helper scaffold (internal/testhelpers + test/{scenario,integration,crash} build-tagged stubs) |
| `hk-pvcs.7` | Author forbid-import tool — third-party allowlist enforcement (Tier 2 check) |
| `hk-pvcs.8` | Author BUILDING.md — minimum dev-onboarding doc (clone, tools, run tests) |

**Total: 1 epic + 8 child tasks = 9 new beads.** Within the 7–12 target range from the mission.

## Edges added

13 `blocks` edges (parent-child edges auto-created by `--parent hk-pvcs`):

- `hk-pvcs.1` (go.mod) blocks every other child — the Go toolchain must be able to resolve a module before any other target can compile, lint, or test.
  - `hk-pvcs.2 → hk-pvcs.1`
  - `hk-pvcs.3 → hk-pvcs.1`
  - `hk-pvcs.4 → hk-pvcs.1`
  - `hk-pvcs.5 → hk-pvcs.1`
  - `hk-pvcs.6 → hk-pvcs.1`
  - `hk-pvcs.7 → hk-pvcs.1`
  - `hk-pvcs.8 → hk-pvcs.1`
- `hk-pvcs.2` (Makefile) blocks every consumer of make targets — golangci/lefthook/coverage-gate/test-helpers/forbid-import/BUILDING all reference `make` invocations directly:
  - `hk-pvcs.3 → hk-pvcs.2`
  - `hk-pvcs.4 → hk-pvcs.2`
  - `hk-pvcs.5 → hk-pvcs.2`
  - `hk-pvcs.6 → hk-pvcs.2`
  - `hk-pvcs.7 → hk-pvcs.2`
  - `hk-pvcs.8 → hk-pvcs.2`

`br dep cycles` final state: **clean.** No cycles introduced.

## Open questions / surprises

1. **`go.work` multi-module decision is unresolved.** `subsystem-organization.md` describes a flat `internal/` tree which strongly suggests single-module is sufficient; multi-module (`go.work`) is only useful if separate semver lines are needed. Flagged as an OQ inside `hk-pvcs.1`'s description rather than a separate bead — defer to first-implementer judgment with single-module as the default.
2. **`scripts/coverage-gate.sh` exists in spec text but has no implementation reference.** `quality-checks.md` §Tier 2 names the script and `quality-checks.md` §Agent-enforceability item 5 lists it among "protected rule files," but the script is not specified beyond its targets. The 95% / 90% / <0.3% targets are LOCKED per STATUS.md "Decisions in force." Surprise: `quality-checks.md` §Deferred says "wait until testing methodology settles" — this contradicts the locked-targets statement. Flagged inside `hk-pvcs.5`'s description as a follow-up spec-edit candidate (NOT silently relax the gate; file a bead if the implementer hits the gap).
3. **`agent-reviewer` skill is not yet beaded.** `make agent-review` target (in `hk-pvcs.2`) and the lefthook pre-commit hook (`hk-pvcs.4`) both reference it; the skill itself is owned by phase-1-readiness-gap-analysis §A4's separate operational-skills meta-epic (`p1-skill-agent-reviewer`). Both `hk-pvcs.2` and `hk-pvcs.4` describe a "stub if the skill doesn't exist yet" fallback so the scaffold can land before the skill — no cross-epic dependency, no blocking.
4. **PL-INV-002 (LLM-SDK ban in daemon transitive closure) is normative but locked-decision-implicit.** It's referenced in `specs/process-lifecycle.md` and reinforced by `quality-checks.md` §depguard, but no bead in the existing corpus owns the depguard rule that mechanically enforces it. `hk-pvcs.3` cites this and inherits the responsibility — a follow-up could promote that to a stand-alone PL-cluster bead if the spec-author wants explicit linkage from `hk-8mup.*` to `hk-pvcs.3`.

## Compliance with mission constraints

- **All 9 new beads carry `scope:bootstrap`.** ✓
- **All 9 statuses are `draft`.** ✓ (Beads CLI default; verified via `br list`.) Zero `parked` / `awaiting-review`.
- **No code was written.** All beads are task-filing only. ✓
- **No specs were modified.** Spec-edit candidates flagged in the OQ list above. ✓
- **CI explicitly excluded.** `make agent-review` and lefthook are LOCAL; no `.github/workflows/` bead filed. ✓
- **Bead descriptions stay simple** — none reads like "complex CI matrix." ✓

## Next steps (caller decides)

- (Recommended) Decide on the `go.work` multi-module question above and either annotate `hk-pvcs.1` or close the OQ.
- (Optional) File the operational-skills meta-epic from phase-1-readiness-gap-analysis §A4 — the `agent-reviewer` skill is the natural near-term unblocker for `hk-pvcs.4`.
- (Recommended) Once `hk-pvcs.1` lands as code, the rest of the children become unblocked simultaneously and can land in parallel by independent agents.
