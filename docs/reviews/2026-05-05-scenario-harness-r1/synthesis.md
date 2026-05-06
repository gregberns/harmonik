# S07 Scenario Harness — R1 Integration Synthesis

**Date:** 2026-05-05
**Integrator:** foundation-author (R1 integration agent)
**Inputs:** implementer-r1.md, cross-spec-architect-r1.md, critic-r1.md
**Output:** specs/scenario-harness.md v0.1.0 (698 lines, 31 reqs, 5 invariants, 10 OQs) → v0.2.0 reviewed (891 lines, 36 reqs, 5 invariants, 13 OQs)

## Convergent themes — resolutions

### Theme 1 — Project-root + daemon coexistence
**Findings converged:** implementer B1; critic BLOCKER (one-daemon-per-project hidden assumption); critic BLOCKER (cross-spec contract conflict §4.4 SH-014 / SH-016 path-override surface).

**Resolution chosen:** hybrid (c) per the integration brief. SH-014 and SH-016 patched; new SH-016a declares the per-scenario synthetic project root at `<fixture-root>/<scenario-name>/project/`. The daemon's `.harmonik/`-rooted writes (PL-005 steps 0, 1, 3a, 6, 8) land inside the synthetic root by construction because the harness invokes the daemon entry-point with that working directory; no daemon-side path-override flag is required at v0.2. PL-001 (one-daemon-per-project) is satisfied because each scenario is a different project from the daemon's perspective. SH-017 was tightened to clarify "same composition-root function with a different working directory + different external configuration" without bypassing PL-005 steps. New OQ-SH-011 tracks the future need for an explicit daemon-side `--project-root` flag if CWD-based resolution proves insufficient.

**SH-NNN edits:** SH-014 (rewrite), SH-016 (added cleanup-between-suites note + `--fixture-root` flag), new SH-016a, SH-017 (clarified "same entry-point" + daemon-process-death detection), SH-INV-001 (updated surface-mutation enumeration to reference SH-016a).

### Theme 2 — Twin-binary identity
**Findings converged:** implementer B2; critic BLOCKER (HC-045 commit-hash); architect M-2.

**Resolution chosen:** SH-009 patched to defer to HC-043/HC-045 unchanged with explicit cite. SH-INV-003 sensor predicate tightened: a binary is a twin iff its absolute resolved path is under the configured twin-search-path prefix OR `agent_overrides` declared the binary AND the daemon's HC-043 commit-hash check passes against a registered twin entry. Name-only heuristics (`HasSuffix("-twin")`) explicitly forbidden. The conformance scenario for the sensor (a scenario referencing `/usr/bin/claude` MUST fail at pre-launch) is added to §10.2.

**SH-NNN edits:** SH-009 (HC-043/HC-045 binding + twin-search-path source precedence), SH-INV-003 (predicate tightening + conformance test), §10.2 SH-008-SH-011 obligation expanded.

**No OQ created** — the existing HC-036 + HC-043 + HC-045 surface is sufficient as a programmatic predicate; HC-036 alone is a naming convention but the predicate doesn't rely on it (it's path-prefix + hash-check based).

### Theme 3 — Per-run cancellation surface
**Findings converged:** implementer B3.

**Resolution chosen:** path (a) — declare per-scenario daemon-stop as the v0.2 cancellation mechanism. SH-026 patched to invoke the per-scenario daemon's `stop` RPC of PL-003a (graceful drain, escalating to SIGKILL on drain-timeout per ON-029). Because each scenario runs its own daemon (Theme 1), `daemon stop` halts the scenario cleanly without affecting any other scenario. Cancel-and-teardown wall-clock bound is now declared as `(N × HC-018) + ON-029-drain-timeout`. New OQ-SH-012 tracks the suite-mode efficiency post-MVH need for a per-run cancel RPC.

**SH-NNN edits:** SH-026 (rewrite for daemon-stop mechanism + bound), SH-015 (sub-step (d) added: `daemon stop` invocation).

## Patches applied (count by severity)

- **BLOCKER:** 12 applied / 0 deferred to OQ. (5 implementer + 7 critic — three pairs were the convergent themes; 7 unique BLOCKERs all addressed.)
- **MAJOR:** ~57 applied / 4 deferred / 0 disagreed. Deferred to OQ-SH-011, OQ-SH-012, OQ-SH-013, OQ-SH-005 (status-noted). The bulk of MAJORs were spec-mechanical fixes (cite repairs, verb tightening, missing schemas, ordering rules, equality semantics, sensor sharpening).
- **MINOR:** ~30 applied / ~10 skipped as duplicates or pure style. Skipped: redundant baseline `Axes:` line trim (style-preference); the §3 glossary nuance about "scenario" vs "scenario file" (acceptable as written); a few prose-vs-normative drift findings already absorbed by adjacent rewrites.

## New requirements / invariants added

- **SH-015a** — Workspace snapshot mechanism (in-place worktree, `git` plumbing for ref predicates, no copy/archive).
- **SH-016a** — Per-scenario synthetic project root (Theme 1 resolution).
- **SH-032** — Harness CLI grammar at MVH (binary name, flag inventory, exit codes, output formats).
- **SH-033** — Signal handling and graceful shutdown (SIGINT/SIGTERM, exit codes 130/143, second-SIGINT escalation).
- **SH-034** — ScenarioResult durability and emission (per-scenario `result.json`, suite-level `suite-result.json`, error_detail format).
- **§8.0** Failure-class precedence table (resolves the three-way inconsistency at SH-015 / §7.1 / §8.8 noted by critic's consolidated finding).

No new SH-INV-NNN was added (SH-INV-001 through SH-INV-005 were all sharpened in place rather than supplemented).

## New open questions

- **OQ-SH-011** — Daemon-side `--project-root` flag (cross-spec coordination with PL).
- **OQ-SH-012** — Per-run cancellation RPC for suite-mode efficiency (cross-spec coordination with PL).
- **OQ-SH-013** — Network-sandbox mechanism on macOS (`pf` chosen as floor; alternatives possible).

OQ-SH-005 was demoted from "punt" to "watching brief" with a status note; its default-if-unresolved was promoted into SH-027's normative scope-carve-out paragraph.

## Cross-spec coordination findings

These findings are real but out of scope for this integration; they require future patches in other specs and are tracked here for the next coordinated patch wave.

| Target spec | Source review finding | What's needed | Tracking |
|---|---|---|---|
| process-lifecycle | implementer B3, critic concurrency BLOCKER | Per-run cancel RPC on the daemon socket (so multiple scenarios can share one daemon and cancel one without killing the daemon). | OQ-SH-012 |
| process-lifecycle | implementer B1, critic one-daemon-per-project BLOCKER, critic SH-014/SH-016 contract-gap | Optional `--project-root <path>` flag on the daemon entry-point so project-root resolution decouples from CWD. | OQ-SH-011 |
| handler-contract | architect M-2, implementer B2, critic SH-009 BLOCKER | None for v0.2 — HC-043/HC-045 already provide the surface SH-009 needs. SH absorbs the integration internally. (No HC patch required.) | (resolved at SH; no future HC edit pending) |
| handler-contract | implementer SH-INV-003 weakness | None — HC-043 hash check + path-prefix predicate are sufficient. (No HC patch required.) | (resolved) |
| handler-contract | architect non-finding side-note for HC-038 post-MVH drift | When SH adds drift detection in a post-MVH revision, HC-038 may acquire a reciprocal pointer. | (post-MVH; OQ-SH-008) |
| operator-nfr | architect M-3, m-3 | None — operator-nfr cited at depends-on; the §6.3 future-MUST and SH-026 cite are fine. | (resolved) |

## Disagreements (if any)

None. Every reviewer finding I touched was either applied as a patch, deferred to an OQ with explicit reasoning, or absorbed into an adjacent rewrite. A handful of MINOR findings were skipped as style-preference duplicates (e.g., critic's "drop redundant baseline Axes lines" — kept the explicit declarations because they aid readability of which requirements are truly baseline). One finding was reframed: critic's recommendation to "convert OQ-SH-005 to a Decision-Made and pull the carve-out into SH-027" was applied (SH-027 carries the carve-out normatively) AND the OQ remains, demoted to a watching-brief status, because the post-MVH question of whether the carve-out scales with twin growth is genuinely open.

## What v0.2 unblocks

- **Pilot decomposition** can begin against this version. Every BLOCKER is resolved; the spec is internally consistent (failure-class precedence table; lifecycle pseudocode aligned with §8 enum; schema records complete with `JSONValue`, `GitSeedOp`, `FileSeed`, split `workflow_path`/`workflow_id`).
- **Implementer skeleton** has every load-bearing decision pinned: project-root model (SH-016a), twin-search-path source (SH-009), cancel surface (SH-026 + daemon-stop), workspace-snapshot mechanism (SH-015a), CLI grammar (SH-032), signal handling (SH-033), result emission (SH-034), failure-class precedence (§8.0), per-kind predicate semantics (§6.3), payload-match grammar (SH-021).
- **Cross-spec architect's depends-on miss** is repaired (operator-nfr added).
- **Architect's cite audit** is fully repaired (M-1 WM-007→WM-019/WM-021; M-2 HC-043/HC-045 in SH-009; M-3 ON in depends-on; M-4 §10 components.md weak cite replaced with bootstrap.md + bootstrapping-self-building cites; m-1/m-2/m-3 minor citation precision applied).

**Remaining caveats:**
- The three new cross-spec OQs (OQ-SH-011, OQ-SH-012, OQ-SH-013) flag future coordination needs but do not block v0.2.
- The v0.2 cancellation model commits to per-scenario daemons; suite-mode efficiency is a known post-MVH lever (OQ-SH-012).
- macOS network-sandbox mechanism (`pf`) is declared as the floor but may need revisit if CI ergonomics suffer (OQ-SH-013).
