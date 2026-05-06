# SH Pilot r1 — Decomposition-Quality Review

`reviewer-version: 1` — drafted 2026-05-05 by the decomposition-quality reviewer subagent against `docs/decompose-to-tasks/sh-pilot.md` v0.1.0 + `docs/decompose-to-tasks/sh-pilot-data.yaml` and `specs/scenario-harness.md` v0.2.0, applying the method in `docs/decompose-to-tasks/pilot-review-protocol.md` v0.2 §3.2 against `docs/decompose-to-tasks/discipline.md` v0.9.

> Scope: per protocol §3.2 method this review samples 10–15 beads (weighted toward complex), evaluates description fidelity to the source spec, evaluates coalesce/multi-step decisions, and flags missing-coalesce / over-split smells. Severity: BLOCKER / MAJOR / MINOR. Lane: `local` (this pilot's application error) / `class` (the discipline rule has a bug or gap).

---

## 1. Sampling plan

Weighted sample of 14 beads:

- **Coalesce candidates** (3): SH-015 + SH-015a (teardown + snapshot pair); SH-016 + SH-016a (fixture-root + synthetic-root pair); SH-025 + SH-026 (timeout declaration + exceedance pair). The pilot's §7 F-pilot-SH-1 explicitly evaluated all three; reviewing the decisions.
- **Multi-step umbrella candidates** (2): SH-015 (5 ordered teardown sub-steps); SH-INV-001 (4-layer corpus grep). Both presented as single beads with sub-bullets — verify §2.2 F8b shared-function-body tiebreaker fires correctly.
- **Sensors / invariants** (2): `sh-inv-001`, `sh-inv-005` — verify §2.5 four-source enumeration and term-use coverage.
- **Schema beads** (2): `sh-schema.scenario-file` (anchor, 11 fields), `sh-schema.workspace-predicate` (per-kind interpretation table, 4 fields).
- **Error-taxonomy bead** (1): `sh-error.taxonomy` — verify (c.2) consumer→owner direction and 8-class threshold under (c) single-table form.
- **Random first-class beads** (4): `sh-005`, `sh-018`, `sh-021` (highest fan-out cross-spec convergence), `sh-028` (network-sandbox + cross-platform mechanism floor).

Total: 14. Coverage and Reference reviewers handle their own remits per §3.1 / §3.3.

---

## 2. Per-bead findings

### 2.1 Coalesce candidates (per pilot §7 F-pilot-SH-1)

#### `sh-015` + `sh-015a` (teardown + workspace snapshot)

**Spec text.** SH-015 §4.4 declares the 5-step teardown protocol on every terminal path. SH-015a §4.4 declares "the `workspace_snapshot_path` recorded in `ScenarioResult` MUST point at the per-scenario worktree directory in-place" plus the inspection protocol via `git` plumbing vs working files.

**Pilot decision.** Two separate beads. F-pilot-SH-1 records: test 1 partially fires; test 2 (anchor + clarification) fires; test 3 does NOT fire — SH-015a's predicate semantics are independently testable and consumed by `sh-022` workspace-state evaluation.

**Reviewer assessment.** The split is sound. SH-015a is not a clarification of SH-015 sub-step (e); it is the *mechanism* by which the recording obligation in (e) is fulfilled, AND it is the substrate for SH-022 / `sh-schema.workspace-predicate` consumers. The pilot's edge graph supports this — `sh-015a` is the target of edges from `sh-015` (sub-step (e)), `sh-022`, and `sh-schema.scenario-result` (`workspace_snapshot_path` field). A coalesced single bead would force SH-022's predicate-evaluation work to depend on the entire teardown protocol's contract, which is not the actual code path.

No finding.

#### `sh-016` + `sh-016a` (fixture-root + synthetic project root)

**Spec text.** SH-016 §4.4 declares the per-suite ephemeral fixture root (operator inspects, not auto-deleted). SH-016a §4.4 declares the per-scenario synthetic project root at `<fixture-root>/<scenario-name>/project/` — the working-directory mechanism that lets the daemon write into the synthetic root by CWD construction.

**Pilot decision.** Two separate beads. F-pilot-SH-1 records: test 1 fires (both about fixture-root containment); test 2 fails (SH-016a is a peer rule, not a clarification); test 3 fails.

**Reviewer assessment.** The split is sound. SH-016a is the *load-bearing* surface mutation cited by SH-014, SH-017, SH-018, SH-031, SH-032, SH-INV-001, SH-INV-002 — the entire "two-and-only-two surface mutations" architecture turns on this bead. SH-016 is the *operator-facing* container (debugging convenience, no-auto-delete). Different consumers, different tests. Coalescing would erase the two-surface-mutation architectural distinction.

No finding.

#### `sh-025` + `sh-026` (timeout declaration + exceedance)

**Spec text.** SH-025 §4.7 declares the `timeout_secs` field shape + monotonic-clock measurement. SH-026 §4.7 declares the cancellation protocol (`daemon stop` RPC + ON-029 drain + per-handler HC-018 bound + teardown via SH-015 + verdict emission).

**Pilot decision.** Two separate beads. F-pilot-SH-1 records: test 1 fails (schema-shape vs behavioral protocol); test 2 fails; test 3 fails.

**Reviewer assessment.** The split is sound. SH-026 has cross-spec edges to HC-018, PL-003a, ON-029, plus intra-spec edges to SH-015, SH-016a; SH-025 has just the SH-004 intra-spec edge. Coalescing would conflate a field-shape rule with a multi-cross-spec cancellation protocol. The §2.3 test 1 ("single shape OR single code path") clearly fails.

No finding.

### 2.2 Multi-step umbrella candidates

#### `sh-015` — 5 ordered teardown sub-steps (a)–(e)

**Spec text.** SH-015 enumerates 5 sub-steps in order: (a) terminate handler subprocesses honoring HC-018; (b) release worktree leases per WM-013b; (c) close event-log file (fsync+close); (d) `daemon stop` RPC per PL-003a; (e) record `workspace_snapshot_path` per SH-015a.

**Pilot decision.** Single bead with sub-bullets. F-pilot-SH-1 records F8b shared-function-body tiebreaker fires.

**Reviewer assessment.** The §2.2 three-AND test:
1. **≥3 steps:** YES (5 steps).
2. **Independently testable:** PARTIAL. (a) is a process-tree termination test; (b) is a lease-registry scan; (c) is fsync verification; (d) is a daemon-stop RPC test; (e) is a path-recording check. Each is in principle independently testable, but the spec body declares "Teardown is run-to-completion best-effort: a failure in any sub-step MUST NOT halt the remaining sub-steps; all errors are accumulated." This is the canonical F8b shared-function-body shape — the steps form a single linear best-effort sequence in any plausible Go implementation (`func teardown() { errs := []; errs = append(errs, terminateHandlers()); errs = append(errs, releaseLeases()); errs = append(errs, closeLog()); errs = append(errs, daemonStop()); errs = append(errs, recordSnapshot()); return aggregate(errs) }`).
3. **Umbrella loses meaning when stripped:** PARTIAL. The five-step ordering is documented in spec §4.4 SH-015 itself; the umbrella description ("Fixture teardown runs on every terminal path") is meaningful without the steps, but the steps are operationally load-bearing.

Discipline §2.2 F8b worked example for EM-016 (`writeTree → commitTree → updateRef`) is the textbook precedent. SH-015's shape is an even stronger case: the spec EXPLICITLY frames it as a single error-accumulating function ("all errors are accumulated and reported"). Single-bead-with-sub-bullets is correct.

The `sh-015` description includes all five sub-steps with their cross-spec edges (HC-018, WM-013b, PL-003a, ON-029, SH-015a). Cross-spec edges from `sh-015` are correctly emitted to each cited target.

No finding. Pilot's F8b application is correct.

#### `sh-inv-001` — 4-layer corpus grep sensor

**Spec text.** SH-INV-001 §5 enumerates 4 layered sensor checks: (1) token-set grep; (2) regex pattern; (3) suffix-test pattern; (4) env-var pattern.

**Pilot decision.** Single sensor bead. The 4 layers are sub-bullets in `sh-inv-001`'s description.

**Reviewer assessment.** §2.2 multi-step rule technically applies only to §4 protocol requirements; sensors are governed by §2.5 ("one bead per `<prefix>-INV-NNN`"). The 4-layer enumeration is a sensor implementation detail (one grep tool, four invocation modes), not a protocol step list. Single-bead is correct per §2.5.

No finding.

### 2.3 Sensor / invariant beads

#### `sh-inv-001` — production-code test-mode-branch grep

**Spec text.** SH-INV-001 sensor body anchors at: SH-008 (handler-config override surface mutation), SH-016a (working-dir surface mutation), SH-018 (the canonical forbidden-token list).

**Pilot edges.** `sh-inv-001 → sh-008`, `sh-inv-001 → sh-016a`, `sh-inv-001 → sh-018`.

**Reviewer assessment.** §2.5 source 3 (sensor-body inline cites) — body explicitly anchors at all three. Edges fire correctly. §10.2 conformance-auditor persona analysis is N/A (SH §10.2 does not declare a per-persona block; the §10.2 obligations are organized by req-group). Source 1 (conformance-group prose) and source 2 (persona bundling) yield empty for SH-INV-001 — the §10.2 prose for SH-008..SH-011 (the twin substitution group) does not name SH-INV-001 inline. Source 4 (invariant-body term-use) — body does not term-use any §6 schema directly.

No finding. Sensor description is a real verification mechanism (corpus grep with 4 layered patterns), not a restatement of the invariant.

#### `sh-inv-005` — declarative-loadable suite-load lint

**Spec text.** SH-INV-005 sensor body cites: §6.1 schema validator (term-defined by `sh-schema.scenario-file`); SH-001/SH-003 implicitly (the YAML-load and declarative-loadable rules being enforced); SH-006 implicitly (suite-load phase); `[architecture.md §4.6]` for parser-change foundation amendment.

**Pilot edges.** `sh-inv-005 → sh-001`, `sh-inv-005 → sh-003`, `sh-inv-005 → sh-006`, `sh-inv-005 → sh-schema.scenario-file`, `sh-inv-005 → ar-020`.

**Reviewer assessment.** §2.5 source 4 (F-em-r1-MAJ-1, invariant-body term-use) — body uses "§6.1 schema validator" (term-defined by `sh-schema.scenario-file`) and "foundation amendment per [architecture.md §4.6]" (term-defined by `ar-020`). Both fire correctly. Source 3 (sensor-body inline cites) — SH-001/SH-003 are not inline-cited in the sensor body verbatim, but they are the YAML-load and declarative-loadable contracts the lint enforces. The pilot treats them as predecessors via the consumer→owner pattern — defensible per §2.5 source 4 (term-use of "scenario file"/"declarative-loadable" concepts owned by SH-001/SH-003).

**Finding F-decomp-SH-1 (MINOR, `local`).** The `sh-inv-005 → sh-001` and `sh-inv-005 → sh-003` edges are weakly justified by §2.5's four sources. SH-INV-005's body literally says "Every scenario file MUST be loadable by a generic YAML parser plus the §6.1 schema validator" — "scenario file" and "loadable" are not §4-req-owned defined terms in the §3.1 step 5 sense; they are general concepts. The §6.1 schema validator is the only term-defined cite. SH-006 (suite-load phase) similarly is implicit. The pilot is over-gating these edges (which is "never wrong" per F4), but reviewers may want to verify these are intentional vs accidental scope-broadening. **Lane: `local`** — discipline rule applied conservatively (over-gate per F4); no class issue. Justification: §2.5 source 3 + 4 do permit conservative emission; the spec's body language is ambiguous on whether it's a "term-use" vs an "operational scope" cite.

### 2.4 Schema beads

#### `sh-schema.scenario-file` — 11-field RECORD

**Spec text.** §6.1 declares 11 fields: `name`, `description`, `workflow_path`, `workflow_id`, `agent_overrides`, `fixture_setup`, `expected_events`, `expected_workspace`, `expected_outcome`, `timeout_secs`, `cadence_tag`, `matrix`. Resolution rules + load-failure modes per SH-002/003/004/005.

**Pilot description.** Bead description enumerates all 11 fields, each with type and a brief constraint pointer (e.g., `timeout_secs (Integer, [1, 7200] per SH-025)`). Cross-references to SH-002/003/004/005 included.

**Reviewer assessment.** Field list is complete (11/11 match). Type information is preserved (String, String|None, Map<String, AgentOverride>, etc.). Constraint cross-references (SH-005 byte-lex, SH-007 ordering, SH-025 range, SH-029 enum, SH-030 cells, etc.) are present.

No finding. Schema description is faithful to spec §6.1.

#### `sh-schema.workspace-predicate` — 4-field RECORD + per-kind table

**Spec text.** §6.1 declares 4 fields: `kind` (Enum file_exists | file_contents_equal | file_contents_match | git_ref_at | commit_trailer_present), `path`, `expected`, `description`. §6.3 `WorkspacePredicate.expected` interpretation table provides per-kind semantics.

**Pilot description.** Bead description enumerates all 4 fields with the `kind` enum and notes per-kind `expected` semantics from §6.3, including: file_exists (None — presence is the predicate); file_contents_equal (literal byte-equal UTF-8); file_contents_match (Go RE2 multi-line off); git_ref_at (40-char SHA-1 OR ref name, short-SHA forbidden); commit_trailer_present (key-only at v0.1).

**Reviewer assessment.** Field count matches; per-kind interpretation correctly inlined per discipline §2.11(c.1) (§6.3-payload-co-located-with-§4-row pattern, applied here as §6.3-table-co-located-with-§6.1-record). The bead correctly absorbs the §6.3 table content as bead description rather than minting a separate bead per discipline §6.3 prose-as-doc treatment.

No finding.

### 2.5 Error-taxonomy bead

#### `sh-error.taxonomy` — 8-class single-table form

**Spec text.** §8 declares 8 sub-sections (§8.1–§8.7 + §8.8) plus §8.0 precedence table. Threshold per §2.11(c) is 11+ for one-bead-per-row; SH has 8.

**Pilot decision.** Single umbrella bead per §2.6 single-table form. `sh-error.taxonomy` description enumerates all 8 classes with their detection rules + EM analogs + §8.0 precedence ordering.

**Reviewer assessment.** §2.11(c) threshold (8 < 11) correctly triggers single-table form. Description includes all 8 classes verbatim. §8.0 precedence table is folded into the description (highest-first ordering). EM analogs for `twin-binary-not-found` (~ EM §8.2 `structural`), `orchestration-internal-error` (~ EM §8.2 `structural`), `scenario-timeout` (~ EM §8.4 `canceled`) are preserved.

**Edge direction (per §2.11(c.2)).** Pilot data: every consumer §4 req that cites a sentinel name (`scenario-load-failure`, `twin-binary-not-found`, `fixture-setup-failed`, `orchestration-internal-error`, `assertion-failed`, `scenario-timeout`, `harness-internal-error`, `cleanup-failed`) edges TO `sh-error.taxonomy` (consumer → owner): `sh-002`, `sh-003`, `sh-004`, `sh-005`, `sh-006`, `sh-009`, `sh-010`, `sh-012`, `sh-015`, `sh-017`, `sh-019`, `sh-022`, `sh-023`, `sh-024`, `sh-026`, `sh-028`, `sh-033`. Total: 17 inbound edges. The taxonomy bead's only outbound edge is to `ar-020` (foundation-amendment protocol per §4.6). This matches the v0.9 (c.2) anti-pattern guard exactly: `<req> → <spec>-error.taxonomy`, never inverse. Direction is correct.

No finding. The pilot has internalised the v0.9 (c.2) lesson cleanly.

### 2.6 Random first-class beads

#### `sh-005` — name uniqueness

**Spec text.** "Every scenario file MUST declare a `name` field that is unique across the entire `scenarios/` tree. Names MUST match `^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`. Name collision after matrix expansion (per SH-030) is also a `scenario-load-failure`. The harness MUST detect duplicates at suite-load time, in which case the entire suite fails (not merely the duplicate scenarios)."

**Pilot description.** Verbatim restatement of all four normative clauses (uniqueness, regex, matrix-expansion participation, suite-load detection + entire-suite-fail).

**Reviewer assessment.** Description fidelity is complete. Cross-spec edges: none required; intra-spec edge to `sh-error.taxonomy` (consumer for `scenario-load-failure`) correctly emitted.

No finding.

#### `sh-018` — no test-mode branches in production-imported packages

**Spec text.** "Production-imported packages... MUST NOT contain conditional branches keyed off 'is this a test?' / 'is this scenario mode?' / 'is this a twin?' / 'is this a harness invocation?'. The harness applies the production stack with two and only two surface mutations: handler-config overrides (§4.3) and working-directory assignment to the per-scenario synthetic project root (§4.4 SH-016a). Forbidden token set (canonical list, also enforced by §5 SH-INV-001's grep): `scenarioMode`, `isTest`, `isTwin`, `harnessMode`, `isFakeRunner`, `useStub`, `if agent_type == \"*-twin\"`, `HasSuffix(\"-twin\")`, `cfg.TestMode`, and any environment variable matching `HARMONIK_*_MODE`. The reviewer obligation to reject PRs introducing such branches is a §10.2 conformance test obligation, not a runtime requirement."

**Pilot description.** Verbatim restatement of all clauses including the full forbidden-token list.

**Reviewer assessment.** Description fidelity is complete. Edges: `sh-018 → sh-008` (handler-config override surface mutation 1) and `sh-018 → sh-016a` (working-dir surface mutation 2) per the "two-and-only-two" load-bearing terms — correctly emitted as term-use edges per §3.1 step 5. The reverse edge `sh-inv-001 → sh-018` is also emitted (sensor blocks-on impl that defines its forbidden-token list) per §2.5 source 3 sensor-body inline cite.

**Re F-pilot-SH-7 (pilot-flagged).** The pilot worried about SH-018 + SH-INV-001 token-list duplication. The §2.10 sensor-as-separate-bead rule + §2.5 F12 sensor↔impl one-way correctly forbid coalescing. Pilot keeps them separate; SH-INV-001 sensor blocks-on SH-018. This is the right call.

No finding.

#### `sh-021` — assertion vocabulary (highest fan-out)

**Spec text.** §4.6 SH-021 declares 4 assertion kinds (event_present, event_absent, workspace_state, exit_code), each with non-trivial substantive content: dotted-path payload-match grammar, NFC-normalized strings, shallow-merge semantics, event_absent window definition, ordering, no-short-circuit, post-MVH composite tracking via OQ-SH-006. The `exit_code` kind specifically cites `[event-model.md §8.1.8]` (terminal events) and `[execution-model.md §4.1 EM-005]` (`Outcome.status` enum).

**Pilot description.** Full description preserves all four assertion kinds with their constraints, plus the dotted-path / NFC / shallow-merge / window / ordering / no-short-circuit clauses, plus the OQ-SH-006 forward-reference.

**Pilot edges.** `sh-021 → sh-schema.event-expectation`, `sh-021 → sh-schema.workspace-predicate`, `sh-021 → sh-schema.outcome-expectation`, `sh-021 → sh-schema.scenario-file`, `sh-021 → sh-023`, `sh-021 → sh-026`, `sh-021 → em-005`, `sh-021 → em-schema.outcome-status`, `sh-021 → ev-events.run-completed`, `sh-021 → ev-events.run-failed`. Total: 10 edges.

**Reviewer assessment.** This is the single highest-fan-out bead in SH (10 edges, 4 cross-spec). Description fidelity is complete; edge derivation is sound:
- 3 schema edges to the assertion's own RECORD types — correct per §3.1 step 4.
- 1 schema edge to `sh-schema.scenario-file` — correct (assertion vocab consumed via the parent record).
- 2 intra-spec edges to `sh-023` (no-short-circuit) and `sh-026` (event_absent window's right edge on timeout) — correct per §3.1 step 5 term-use.
- 2 EM edges (`em-005` for the EM-005 `Outcome.status` enum cite; `em-schema.outcome-status` for the OutcomeStatus enum value type) — both correct per §3.1 step 4 type-cite.
- 2 EV edges (`ev-events.run-completed` and `ev-events.run-failed` for the §8.1.8 terminal-emission events) — correct per §2.11(d.2) EV-row-bead-as-canonical-home.

No finding. This is the model worked example of the four-source edge-derivation rule firing cleanly on a behaviorally-rich requirement.

#### `sh-028` — no external network access (cross-platform mechanism floor)

**Spec text.** §4.8 SH-028 declares a non-loopback-forbidden contract for harness/daemon/orchestrator/watchers/agent-runner/twin binaries, with a Linux floor (`unshare(CLONE_NEWNET)`) and a macOS floor (`pf` packet-filter). Detection of non-loopback connection → `harness-internal-error`. §10.2 conformance lane MUST run with sandbox enabled.

**Pilot description.** Faithful restatement of all clauses including the cross-platform mechanism floor and the OQ-SH-013 forward-reference for macOS `pf` mechanism revisitation.

**Pilot edges.** `sh-028 → sh-error.taxonomy` (cites `harness-internal-error`).

**Reviewer assessment.** Description fidelity is complete. Note that the `unshare(CLONE_NEWNET)` and `pf` mechanism details are operational implementation knowledge co-located in spec §4.8 itself (not in §6 or §10.2); per discipline §2.4 default, the test obligation is absorbed into this req bead. SH-028 has no cross-spec inline cites.

No finding. (Could plausibly have an extracted "network-sandbox harness" test-infra bead per §6 of pilot — but pilot's F-pilot-SH-6 explicitly defers this until shared usage is observed; consistent with BI's `bi-test.crash-harness` extraction-on-observed-sharing pattern.)

---

## 3. Missing-coalesce smell check

Per protocol §3.2 step 3: "Check for clusters of requirements that share the same prefix and could plausibly have been coalesced but were emitted as separate beads."

Pilot reports **0 §2.3 coalesces** — first time in the corpus (per §1 spec-version preface; HC had 0; BI had 3; AR had 2; EM had several typed-alias clusters that explicitly did NOT coalesce per F-em-r1-MIN-8). This warrants explicit verification.

### 3.1 Candidates examined

The pilot's F-pilot-SH-1 explicitly evaluated 3 candidates (all rejected). Beyond those, I scan for additional clusters:

#### Candidate A: `sh-002` + `sh-003` + `sh-004` + `sh-005` (scenario-load-failure family)

**Surface signal.** All four §4.1 reqs cite `scenario-load-failure` and emit edges to `sh-error.taxonomy`.

**§2.3 three-AND test.**
1. **Single shape OR single code path:** FAILS. SH-002 is a discovery rule (file extension + path); SH-003 is a parse-mechanism rule (UTF-8, no-eval, size cap); SH-004 is the failure-classification umbrella; SH-005 is a name-uniqueness rule. Each has a distinct code path (discovery vs parse vs schema-check vs uniqueness-scan) producing the same failure class.
2. **Anchor + clarifications:** PARTIAL. SH-004 could plausibly be the anchor (it declares the failure-class umbrella). But SH-002, SH-003, SH-005 are not clarifications of SH-004 — they are *triggers* that map TO SH-004's failure class. Distinct work.
3. **Split reduces to "see anchor":** FAILS. Each of SH-002/003/005 has substantive constraint shape (regex, size cap, parse-tag deny-list) that does not reduce to "see SH-004."

**Verdict.** Coalesce does NOT fire. BI's analogous example is BI-025a..e — five separate `br` invocation reqs that all share the `br` invocation surface but address orthogonal concerns (exit code, JSON, timeout, stderr, concurrency). Discipline §2.3 worked example explicitly rejects coalescing this cluster. SH-002..SH-005 follow the same pattern.

No finding.

#### Candidate B: `sh-009` + `sh-010` (twin-binary discovery + missing-binary failure)

**Surface signal.** Both §4.3 reqs cite `twin-binary-not-found`; SH-010 is the negative-path failure for SH-009.

**§2.3 three-AND test.**
1. **Single shape OR single code path:** PARTIAL. SH-009 is the discovery resolution algorithm (search-path + HC-043 hash); SH-010 is the failure emission rule (verdict=error, error MUST carry unresolved name + search paths consulted). They share the discovery surface but the failure-emission carries distinct payload requirements.
2. **Anchor + clarifications:** SH-009 could be the anchor; SH-010 is the "what happens when discovery fails" clarification.
3. **Split reduces to "see anchor":** PARTIAL. SH-010's substantive content ("error MUST carry unresolved name and search paths consulted") is a payload-shape requirement, not just a "see SH-009" pointer.

**Verdict.** Two-of-three fires; per the pilot's F-pilot-EM-5 worked precedent (typed-alias clusters need all 3 to fire), coalesce does NOT fire. The pilot's choice to keep them separate is consistent with the cumulative discipline.

**Finding F-decomp-SH-2 (MINOR, `local`).** Worth flagging for synthesis: this is a borderline case. SH-009 + SH-010 are tightly bound semantically (the SH-009 discovery rule's negative path *is* SH-010). Reviewers may judge this as a missed coalesce. However, the cumulative discipline (post-EM/BI/AR pilots) sets the conservative bias; the pilot's choice to keep separate is defensible and matches BI's BI-025a..e pattern. **Lane: `local`** — discipline applied conservatively; no class gap.

#### Candidate C: `sh-INV-001` + `sh-018` (forbidden-token list)

**Surface signal.** Both name the same canonical forbidden-token set inline.

**§2.3 three-AND test.** §2.5 explicitly forbids merging invariants with §4 reqs ("MUST NOT be merged into the §4 requirement beads they constrain"). The §2.3 coalesce rule does not apply across the §4/§5 boundary.

**Verdict.** Hard rule: coalesce CANNOT fire across §4/§5. Pilot correctly keeps them separate (F-pilot-SH-7 documents the analysis). No finding.

#### Candidate D: `sh-032` + `sh-033` + `sh-034` (CLI surface family)

**Surface signal.** All three are the new-in-v0.2 CLI / signal-handling / result-emission family added in §4.12 / §4.13.

**§2.3 three-AND test.**
1. **Single shape OR single code path:** FAILS. SH-032 is the CLI grammar (flags + exit codes); SH-033 is signal handling (SIGINT/SIGTERM → graceful shutdown); SH-034 is per-scenario `result.json` durability + `SuiteResult` emission. Distinct code paths.
2. **Anchor + clarifications:** No anchor structure; three peer rules.
3. **Split reduces to "see anchor":** FAILS.

**Verdict.** Coalesce does NOT fire. No finding.

### 3.2 Verdict on zero-coalesce result

The pilot's zero-coalesce result is **defensible**. SH's §4 structure exhibits a high degree of behavioral / mechanistic distinction between adjacent reqs:

- §4.1 (scenario file format) groups 5 reqs each addressing a distinct error trigger.
- §4.3 (twin substitution) has 4 reqs with 4 distinct concerns: surface-mutation rule, discovery resolution, failure emission, parity surface adoption.
- §4.4 (workspace fixture lifecycle) has 7 reqs each with a distinct mechanism (setup phase, isolated workspace path, isolated event-log path, teardown protocol, snapshot mechanism, ephemeral fixture root, synthetic project root).
- §4.6..§4.13 each contain 1-5 reqs that are mostly peer rules without anchor-and-clarification structure.

This is in contrast to BI (which had 3 explicit anchor-clarification clusters: BI-008 + BI-008a, BI-010 + BI-010a + BI-010b, etc.) and AR (which had a verbatim cross-reference req AR-035 collapsing to AR-026's notes line per §2.1a).

SH's authoring style — peer rules with cross-reference edges rather than letter-suffixed clarification chains — does not produce coalesce-eligible clusters. The pilot's conservatism is appropriate.

**No missing-coalesce finding above MINOR severity.** Borderline case F-decomp-SH-2 above logged for transparency.

---

## 4. Over-split smell check

Per protocol §3.2 step 4: "Check for multi-step protocols of 2 steps, or step beads whose descriptions could be sub-bullets in their parent."

Pilot reports **0 §2.2 multi-step splits** — also noteworthy. SH has 36 §4 reqs; the §7.1 lifecycle pseudocode names 5 lifecycle steps; SH-015 has 5 ordered teardown sub-steps; SH-INV-001 has 4 layered sensor checks; SH-INV-002 has 3 inspection axes; SH-021 has 4 assertion kinds. None split into step beads.

### 4.1 Candidates examined

#### Candidate E: `sh-015` 5-step teardown protocol

Already analysed in §2.2 above. F8b shared-function-body tiebreaker fires correctly. Single bead with sub-bullets is right per the EM-016 worked example precedent.

#### Candidate F: §7.1 lifecycle pseudocode (5 steps)

**Spec text.** §7.1 enumerates 5 lifecycle steps: load → fixture-up → orchestrate → assert → fixture-down + verdict.

**Pilot decision.** Not minted as an umbrella bead; per pilot §1, "the §7.1 lifecycle pseudocode is §7-prose owned by the §4 reqs it summarises (per §3.1 §7-prose no-edge rule)." The §4 reqs (SH-001..SH-034) collectively implement the lifecycle.

**Reviewer assessment.** §3.1 explicitly excludes §7 prose / pseudocode / state-machine prose from edge generation (F-em-r1-MIN-3). The corollary is that §7 lifecycle prose does NOT get its own bead either — it's a *summary* of the §4 reqs that own the actual contracts. The pilot's choice is correct.

No finding.

#### Candidate G: `sh-021` 4 assertion kinds

**Spec text.** SH-021 enumerates 4 assertion kinds. Each is an independently buggy/testable predicate evaluation surface.

**§2.2 three-AND test.**
1. **≥3 steps:** YES (4 kinds).
2. **Independently testable:** YES — `event_present` evaluation, `event_absent` window evaluation, `workspace_state` predicate evaluation, `exit_code` payload-match evaluation are each independent.
3. **Umbrella loses meaning when stripped:** PARTIAL. The umbrella ("Assertion vocabulary at v0.1 is four kinds") is meaningful as a vocabulary scope assertion; the kinds themselves are the substantive content.

**Reviewer assessment.** This case is closer to the §2.2 three-AND-fires threshold than SH-015. The 4 kinds are NOT a sequential protocol; they are a vocabulary enumeration. §2.2 was designed for sequential protocols ("contains a numbered step list"); applying it to a parallel-vocabulary list is a category mismatch.

The pilot's decision to keep this as a single bead with kinds-as-sub-bullets is correct: each kind has its own schema bead (`sh-schema.event-expectation`, `sh-schema.workspace-predicate`, `sh-schema.outcome-expectation`) which carries the per-kind work. The SH-021 bead is the *integration* of the four kinds plus the global rules (no short-circuit, ordering, NFC). Splitting into 4 step beads would force each step-bead to re-cite the global rules.

No finding. Pilot's choice is correct.

#### Candidate H: `sh-INV-001` 4-layer corpus grep

Already analysed in §2.2. §2.5 governs sensors (single bead per invariant); the 4 layers are sensor-implementation sub-bullets, not step beads.

No finding.

#### Candidate I: `sh-INV-002` 3-axis inspection (process tree, lease registry, fds)

**Spec text.** §5 SH-INV-002 sensor inspects 3 axes: (i) descendant process tree, (ii) worktree-lease registry, (iii) open file descriptors.

**Pilot decision.** Single sensor bead with 3 axes as sub-bullets; predecessors `sh-015`, `sh-013`, `sh-014`, `sh-016`, `pl-006`, `pl-006a` cover the term-use and source-3 cites for each axis.

**Reviewer assessment.** §2.5 governs sensors — single bead per invariant. The 3 axes are sensor sub-tasks, not separable invariants. Correct.

No finding.

### 4.2 Verdict on zero-multi-step result

The pilot's zero-multi-step result is **defensible**. SH's behavioral surface is dominated by:

- Single-rule §4 requirements with cross-spec edges (no internal step lists).
- One sequential protocol (SH-015's 5-step teardown) that fires F8b shared-function-body tiebreaker.
- One vocabulary enumeration (SH-021's 4 assertion kinds) that is not a sequential protocol.
- One pseudocode lifecycle (§7.1) explicitly excluded by §3.1 §7-prose no-edge rule.
- Sensor multi-axis enumerations governed by §2.5 (single-bead-per-invariant).

This is not the same as BI/PL/ON whose §4 reqs (BI-031, BI-030, PL-005, ON-027) declared explicit "step 1, step 2, step 3..." protocols with independent failure modes per step. SH's §4 simply does not contain that shape (the closest, SH-015, fires F8b).

**No over-split finding.**

---

## 5. Description-fidelity sweep — across full sample

Description fidelity check per protocol §3.2 step 2 Question 1 ("Does the description match what the spec says? Does it leave out any normative requirement? Does it overstate?"):

Across the 14 sampled beads, descriptions are uniformly high-fidelity to the spec body. Specific observations:

- **Quantitative limits preserved:** SH-003's 1 MiB / 100 000 nodes; SH-005's regex; SH-025's [1, 7200]; SH-030's 1024 cells; SH-032's exit codes; SH-INV-004's N≥10. All present.
- **Forbidden-token lists preserved verbatim:** SH-018's full 9-element list; SH-INV-001's 4-layer pattern enumeration. All present.
- **Cross-spec cite paths preserved:** SH-015's `[handler-contract.md §4.4.HC-018]`, `[workspace-model.md §4.3.WM-013b]`, `[process-lifecycle.md §4.2.PL-003a]`, `[operator-nfr.md §4.7]` — all four cited in description.
- **OQ forward-references preserved:** SH-001 cites OQ-SH-001; SH-021 cites OQ-SH-006; SH-026 cites OQ-SH-012; SH-028 cites OQ-SH-013; SH-031 cites OQ-SH-002; SH-016a cites OQ-SH-011. All present.
- **Carve-outs preserved:** SH-027's heartbeat-mode wall-clock carve-out; SH-INV-001's unit-test-package + harness-own-package exclusions; SH-INV-004's `nightly`-cadence exclusion. All present.

One narrow concern:

**Finding F-decomp-SH-3 (MINOR, `local`).** `sh-019`'s description preserves the closed set of scenario-attributable failures (handler `agent_failed`, twin-scripted failure, gate denial, edge-cascade `run_failed`) but condenses the "anything else" enumeration to "(daemon crash via process-exit, RPC error, panic, `degraded` state per [process-lifecycle.md §4.4 PL-010] mid-scenario, store-divergence)" — losing the spec's explicit "store-divergence detected by reconciliation mid-scenario" granularity (the pilot says "store-divergence" without the "detected by reconciliation mid-scenario" qualifier). MINOR cosmetic; an implementer reading the bead in isolation would still need to consult the spec for the full reconciliation-detected qualifier. **Lane: `local`** — pilot tightening; no class issue.

---

## 6. Findings summary

### 6.1 By severity

| Severity | Count |
|---|---|
| BLOCKER | 0 |
| MAJOR | 0 |
| MINOR | 3 |

### 6.2 By lane

| Lane | Count |
|---|---|
| `local` | 3 |
| `class` | 0 |

### 6.3 Findings list

| ID | Severity | Lane | Bead(s) | Concern |
|---|---|---|---|---|
| F-decomp-SH-1 | MINOR | `local` | `sh-inv-005` | `sh-inv-005 → sh-001` and `sh-inv-005 → sh-003` edges weakly justified by §2.5 four sources. Conservative over-gate per F4; defensible. |
| F-decomp-SH-2 | MINOR | `local` | `sh-009` + `sh-010` | Borderline coalesce candidate (anchor + negative-path clarification). Pilot keeps separate, consistent with BI-025 cumulative discipline. |
| F-decomp-SH-3 | MINOR | `local` | `sh-019` | Description condenses spec's "store-divergence detected by reconciliation mid-scenario" to "store-divergence." Cosmetic tightening. |

### 6.4 Verdict on zero-coalesce / zero-multi-step

- **Zero §2.3 coalesces — VERIFIED DEFENSIBLE.** The pilot evaluated 3 candidates explicitly; reviewer verified 4 additional candidates (scenario-load-failure family, twin-discovery + failure pair, INV-001 + SH-018 cross-§4/§5, CLI family). All 7 candidates correctly fail the §2.3 three-AND test under the cumulative post-EM/AR/HC discipline. SH's authoring style (peer rules with cross-reference edges, not letter-suffixed clarification chains) does not produce coalesce-eligible clusters.
- **Zero §2.2 multi-step splits — VERIFIED DEFENSIBLE.** The pilot evaluated SH-015 explicitly (F8b fires). Reviewer verified 5 additional candidates (§7.1 lifecycle pseudocode, SH-021 vocabulary, SH-INV-001 sensor layers, SH-INV-002 inspection axes). The §7-prose no-edge rule + §2.5 sensor-single-bead rule + the F8b shared-function-body tiebreaker correctly fire across all candidates.

The "zero-zero" result is not a smell here; it reflects SH's shape (mostly peer rules without anchor-clarification or sequential-protocol structure that the §2.3/§2.2 rules were designed to handle).

---

## 7. Out-of-scope notes

- **F-pilot-SH-4 (§4.a envelope absence):** Per the review brief, this is being handled by a separate discipline-patch agent. No reviewer action.
- **F-pilot-SH-5 (`[event-model.md §6.2]` cite):** Pilot self-flags as MINOR `local` for the Reference reviewer's lane. Not in decomposition-quality scope.
- **F-pilot-SH-6 (test-infra deferral):** Pilot self-flags. Decomposition-quality concurrence: SH-028's network-sandbox is a plausible future extraction once the Linux + Darwin paths produce shared tooling. Pilot's defer-until-shared-usage-observed pattern matches BI's `bi-test.crash-harness` precedent. No finding.

---

## 8. Synthesis input — recommended lane assignments

Per protocol §4.1 four-probe triage, the 3 MINOR findings are all `local`:

1. **F-decomp-SH-1, F-decomp-SH-2, F-decomp-SH-3** — all MINOR cosmetic / borderline; per protocol §4.2 "MINOR findings skip triage; they are cosmetic" — all stay in the pilot-patch lane at author's discretion.

No class-lane findings from this reviewer. The decomposition-quality dimension is clean for SH r1.

---

## 9. Method audit

- Spec read top-to-bottom (lines 1–891 of v0.2.0).
- Pilot read top-to-bottom (lines 1–276 of v0.1.0).
- Pilot data YAML examined for edge derivation in detail (804 lines).
- Discipline §2.1–§2.6, §2.11(c), §2.11(c.1), §2.11(c.2) re-read against findings.
- Sample size: 14 beads (target 10–15) — coverage: 3 coalesce candidates + 2 multi-step candidates + 2 sensors + 2 schemas + 1 taxonomy + 4 random first-class.
- Missing-coalesce smell scan: 7 cluster candidates evaluated.
- Over-split smell scan: 6 candidates evaluated.

End of report.
