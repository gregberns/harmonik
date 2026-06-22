# Scenario Harness Spec-vs-Code Conformance Audit

**Bead:** hk-ffw3h  
**Date:** 2026-06-22  
**Spec audited:** `specs/scenario-harness.md` version **0.2.2** (front matter `version: 0.2.2`, `last-updated: 2026-05-14`)  
**Epic:** hk-i0tw (scenario harness implementation)  
**Auditor role:** VERIFY-AND-REPORT ONLY — no fixes applied.

---

## Scope

- **Normative spec:** `specs/scenario-harness.md` v0.2.2 (all §4 requirements, §5 invariants, §6 schemas, §7 lifecycle, §8 failure taxonomy, §10 conformance).
- **Implementation:** `internal/scenario/` (record types, failure taxonomy, fixture bootstrap, sensors) and adjacent artifacts: conformance scenario YAML files under `scenarios/`, conformance corpus test, twin wire types in `cmd/harmonik-twin-claude/wire.go`, harness CLI in `cmd/harmonik/main.go`.
- **Out of scope per task constraints:** `dot_cascade.go`, `workloop.go`, `daemon.go`, `stalewatch.go`, `reviewloop.go`.

---

## Per-Section Conformance Table

### §6.1 Record Shapes

| Spec record | Code location | Status | Note |
|---|---|---|---|
| `ScenarioFile` (12 fields) | `internal/scenario/scenariofile.go:173` | **CONFORMS** | All 12 fields present; `workflow_path`/`workflow_id` mutual exclusivity enforced in `Valid()`. Name regex `^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$` at `:149`. |
| `AgentOverride` | `internal/scenario/agentoverride.go:18` | **CONFORMS** | `binary` (required) + `args` (List\|None). `Valid()` enforces non-empty binary. |
| `FixtureSetup` | `internal/scenario/fixturesetup.go:23` | **CONFORMS** | `git_seed`, `files`, `skill_search_paths` — all List\|None/Map\|None; zero-value is valid. |
| `GitSeedOp` | `internal/scenario/gitseedop.go:64` | **CONFORMS** | 4-value `op` enum + `args` map; required-key table from §6.3 enforced in `Valid()` at `:85`. |
| `FileSeed` | `internal/scenario/fileseed.go:65` | **CONFORMS** | `encoding` (utf8\|base64\|""), `contents`, `mode` (octal ≤ 0777). Base64 decode-check and octal parse in `Valid()`. |
| `EventExpectation` | `internal/scenario/eventexpectation.go:37` | **CONFORMS** | `kind` (event_present\|event_absent), `type` (core.EventType), `payload_match` (Map\|None), `description`. |
| `WorkspacePredicate` | `internal/scenario/workspacepredicate.go:66` | **CONFORMS** | 5-value `kind` enum. `Expected *string` cleanly models String\|None. Per-kind §6.3 semantics enforced in `validate()` at `:98`: file_exists requires nil, git_ref_at rejects short-SHA, path-traversal and absolute paths rejected per SH-022. |
| `OutcomeExpectation` | `internal/scenario/outcomeexpectation.go:14` | **CONFORMS** | `outcome_status` typed as `core.OutcomeStatus` (SUCCESS\|FAIL\|RETRY\|PARTIAL_SUCCESS); `description` required. |
| `ScenarioResult` (12 fields) | `internal/scenario/scenarioresult.go:67` | **CONFORMS** | All fields present including `source_path` (v0.2 addition), `stdout_log_paths`/`stderr_log_paths` maps. Pass-iff-no-failure-class invariant enforced in `Valid()` at `:159`. |
| `AssertionResult` | `internal/scenario/assertionresult.go:65` | **CONFORMS** | 4-value `assertion_kind` enum (event_present\|event_absent\|workspace_state\|exit_code). `actual_value`/`expected_value` typed as `any` (JSONValue). |
| `SuiteResult` (7 fields) | `internal/scenario/suiteresult.go:65` | **CONFORMS** | `suite_id` typed `core.SuiteID`, `cadence_filter` enum. Suite-verdict invariant (pass iff all results pass, including vacuous empty-list case) enforced in `Valid()` at `:138`. |
| `CadenceTag` / `CadenceFilter` | `internal/scenario/cadencetag.go` | **CONFORMS** | 3-value `CadenceTag` + 4-value `CadenceFilter`. `Includes()` method at `:129` implements the SH-029 superset relation correctly (smoke⊂regression⊂nightly; all=nightly). |

### §8 FailureClass Enum

| Spec class | Code location | Status | Note |
|---|---|---|---|
| `scenario-load-failure` | `internal/scenario/failureclass.go:21` | **CONFORMS** | All 8 constants declared. |
| `twin-binary-not-found` | `failureclass.go:26` | **CONFORMS** | |
| `fixture-setup-failed` | `failureclass.go:31` | **CONFORMS** | |
| `orchestration-internal-error` | `failureclass.go:36` | **CONFORMS** | |
| `harness-internal-error` | `failureclass.go:42` | **CONFORMS** | |
| `assertion-failed` | `failureclass.go:47` | **CONFORMS** | |
| `scenario-timeout` | `failureclass.go:50` | **CONFORMS** | |
| `cleanup-failed` | `failureclass.go:55` | **CONFORMS** | |
| §8.0 precedence table | `failureclass.go:116` (`Precedence()`) | **CONFORMS** | Numeric rank 1–8 exactly matches spec table. `HigherPrecedenceThan()` at `:146` correctly resolves co-occurrence. `cleanup-failed` rank 8 (lowest) prevents verdict overwrite. |

### §5 Invariants / Sensors

| Invariant | Spec sensor | Status | Note |
|---|---|---|---|
| SH-INV-001 — no test-mode branches in production code | Corpus-grep (§5) | **CONFORMS** | Grep over `internal/` for forbidden tokens (`scenarioMode`, `isTest`, `isTwin`, `harnessMode`, `isFakeRunner`, `useStub`, `cfg.TestMode`, `HARMONIK_*_MODE` reads) — ZERO hits. `HARMONIK_WORKFLOW_MODE` in `internal/handler/claudehandler_chb006_024.go:309` is a WRITE to subprocess env, not a read for test-mode detection — not a violation. |
| SH-INV-002 — workspace fully reset on teardown | Post-suite process/lease/fd check | **GAP** | No post-suite sensor test exists checking (i) descendant process tree, (ii) worktree-lease registry, (iii) open fd scan. Data types for workspace path exist; sensor test absent. |
| SH-INV-003 — twin-only, never real-model handlers | Pre-launch path-prefix check | **GAP** | `AgentOverride.Binary` carries the path, but no code verifies the binary is under the configured twin-search-path prefix before launch. No scenario MUST-fail test for `/usr/bin/claude` is visible. |
| SH-INV-004 — identical verdicts on rerun | Nightly rerun-diff sensor (N≥10) | **GAP** | Determinism contract field set is defined on `ScenarioResult`. No rerun-diff test exists; nightly cadence. |
| SH-INV-005 — declarative-loadable | Suite-load corpus lint | **CONFORMS** | `ParseScenarioFile` (`:60`) enforces: `gopkg.in/yaml.v3` strict mode with `KnownFields(true)`, forbidden-tag deny-list (`!!python/object`, `!eval`, `!!binary`), 1 MiB size ceiling, 100 000-node ceiling, UTF-8 BOM rejection. |

### §4.1 Scenario File Format (SH-001 to SH-005)

| Req | Status | Code location | Note |
|---|---|---|---|
| SH-001 — YAML-only format | CONFORMS | `scenariofile.go:60` | ParseScenarioFile enforces YAML parse. |
| SH-002 — `.yaml` extension + `scenarios/` location | CONFORMS (extension); GAP (discovery) | `scenariofile.go:62` | Extension check present. Discovery from `scenarios/` dir is harness-CLI concern; no suite-load discovery code implemented. |
| SH-003 — declarative-loadable, size/node ceilings, no BOM | CONFORMS | `scenariofile.go:66-130` | All constraints enforced. |
| SH-004 — malformed → `scenario-load-failure` | CONFORMS | `scenariofile.go:125` (`sf.Valid()`) | `ScenarioFile.Valid()` returns false → caller maps to `FailureClassScenarioLoadFailure`. |
| SH-005 — name uniqueness + regex | CONFORMS (regex); GAP (suite-wide uniqueness) | `scenariofile.go:149` | Name regex enforced in `Valid()`. Suite-wide uniqueness requires suite-load pass not yet implemented. |

### §4.2 Suite Loading and Ordering (SH-006, SH-007)

| Req | Status | Note |
|---|---|---|
| SH-006 — suite-load phase (discovery + parse + validate) | **GAP** | No suite discovery or suite-load phase implementation. Data types ready; executor absent. |
| SH-007 — byte-lexicographic execution order | **GAP** | No ordering enforcement. Requires harness executor. |

### §4.3 Twin Substitution (SH-008 to SH-011)

| Req | Status | Note |
|---|---|---|
| SH-008 — handler-config override, not runtime branch | CONFORMS | SH-INV-001 token-set grep clean. |
| SH-009 — twin-search-path resolution (CLI flag > env > `twins/` default) | **GAP** | `BootstrapFixture` accepts caller-supplied `twinSearchPaths` slice but the precedence resolution loop (CLI flag `--twin-search-path`, env `HARMONIK_TWIN_SEARCH_PATH`, default `<repo-root>/twins/`) is absent from the harness CLI. |
| SH-010 — missing twin binary → `twin-binary-not-found` | N-A (data layer) | Failure class defined. Detection mechanism requires harness executor. |
| SH-011 — twin parity via HC §4.8 surface | N-A (data layer) | Observational; no code obligation in `internal/scenario/`. |

### §4.4 Workspace Fixture Lifecycle (SH-012 to SH-016a)

| Req | Status | Code location | Note |
|---|---|---|---|
| SH-012 — fixture setup (3 sub-steps) | CONFORMS | `fixturebootstrap.go:72` | `BootstrapFixture` covers (a) git init via `SynthesizeProjectRoot`, (b) event-log dir creation, (c) twin search paths captured. Partial-rollback documented as caller obligation. |
| SH-013 — isolated workspace per scenario | CONFORMS | `fixturebootstrap.go:21` (`ScenarioWorkspacePath`) | Path `<fixture-root>/<scenario-name>/workspace/` enforced. |
| SH-014 — isolated event-log dir | CONFORMS | `eventlogdir.go:12` (`EventLogRelPath`) | `.harmonik/events/events.jsonl` relative to synthetic project root by construction. |
| SH-015 — fixture teardown on all terminal paths | **GAP** | — | No teardown implementation visible in `internal/scenario/`. SH-015 sub-steps (a)–(e) unimplemented. |
| SH-015a — workspace snapshot (in-place, not copied) | CONFORMS | `sh015a_workspace_snapshot.go:25` | `WorkspaceSnapshotPath(name)` returns `<scenarioName>/workspace` (fixture-root-relative). No copy/archive. |
| SH-016 — per-suite ephemeral fixture root | CONFORMS | `fixtureroot.go:26` | `NewFixtureRoot` uses `os.MkdirTemp` under `os.TempDir()` or `parentDir`. Not auto-deleted. |
| SH-016a — per-scenario synthetic project root | CONFORMS | `synthprojectroot.go:33` | `SynthesizeProjectRoot` creates `<fixture-root>/<name>/project/` and runs `git init`. `ScenarioProjectRoot` path formula correct. |

### §4.5 Orchestration Drive (SH-017 to SH-019)

| Req | Status | Note |
|---|---|---|
| SH-017 — production daemon entry-point | **GAP** | No orchestration drive code in `internal/scenario/` or visible in `cmd/harmonik/main.go` for a `harness` subcommand. |
| SH-018 — no test-mode branches | CONFORMS | Covered by SH-INV-001 token-set scan. |
| SH-019 — daemon crash → `orchestration-internal-error` | N-A (data layer) | Failure class defined. Detection requires executor. |

### §4.6 Assertion Evaluation (SH-020 to SH-024)

| Req | Status | Note |
|---|---|---|
| SH-020 — JSONL event log as assertion surface; stdout/stderr capture | **GAP** | No assertion evaluator. `EventLogPath`/`EventLogDir` helpers defined. `stdout_log_paths`/`stderr_log_paths` map fields on `ScenarioResult` defined but not populated. |
| SH-021 — four assertion kinds; dotted-path payload_match; evaluation order | **GAP** | Data types (`EventExpectation`, `WorkspacePredicate`, `AssertionResult`) complete. Evaluator absent. |
| SH-022 — workspace-state predicates inspect in-place snapshot | CONFORMS (struct validation) | `WorkspacePredicate.validate()` enforces path safety. Evaluation logic absent (execution layer). |
| SH-023 — no short-circuit; all assertions evaluated | **GAP** | Execution policy; requires evaluator. |
| SH-024 — log-read failure → `harness-internal-error` | N-A (data layer) | Failure class defined. |

### §4.7 Scenario Timeout (SH-025, SH-026)

| Req | Status | Note |
|---|---|---|
| SH-025 — `timeout_secs` ∈ [1, 7200], no default | CONFORMS | `ScenarioFile.Valid()` at `:306` enforces range. |
| SH-026 — timeout exceedance → cancel via `daemon stop` + `verdict=timeout` | **GAP** | Monotonic-clock cancellation not implemented. |

### §4.8 Repeatability (SH-027, SH-028)

| Req | Status | Note |
|---|---|---|
| SH-027 — identical inputs → identical contract field set | N-A (data layer) | Determinism contract field set defined on `ScenarioResult`. Enforcement is execution layer. |
| SH-028 — no external network access; Linux `unshare` / macOS `pf` sandbox | **GAP** | No network-sandbox mechanism implemented. |

### §4.9 Cadence Support (SH-029)

| Req | Status | Note |
|---|---|---|
| SH-029 — `cadence_tag` ∈ {smoke, regression, nightly}; `--cadence` filter superset | CONFORMS | `CadenceTag`, `CadenceFilter`, `Includes()` fully implemented. |

### §4.10 Matrix Expansion (SH-030)

| Req | Status | Note |
|---|---|---|
| SH-030 — parameter matrix; 1024 cell cap; synthetic name `<name>[k=v,...]`; `text/template` substitution | CONFORMS (cell cap); **GAP** (name synthesis + substitution) | `matrixCellCount` at `scenariofile.go:331` validates ≤1024 cells and rejects zero-length value lists. Name-expansion format (`<name>[k1=v1,k2=v2,...]` with byte-lex key order) and `text/template` parameter substitution not visible. |

### §4.11 Concurrency (SH-031)

| Req | Status | Note |
|---|---|---|
| SH-031 — sequential execution (one active scenario at a time) | **GAP** | Execution layer; no harness runner. |

### §4.12 Harness CLI (SH-032)

| Req | Status | Note |
|---|---|---|
| SH-032 — `harmonik harness` subcommand with 8 flags and 5 exit codes | **GAP** | No `harness` subcommand in `cmd/harmonik/main.go`. The word "harness" appears in `main.go` only in the context of `--default-harness` (agent-type selection — unrelated). |

### §4.13 Signal Handling and Result Emission (SH-033, SH-034)

| Req | Status | Note |
|---|---|---|
| SH-033 — SIGINT/SIGTERM graceful shutdown; double-SIGINT hard exit | **GAP** | Requires CLI (SH-032 unimplemented). |
| SH-034 — `result.json` per scenario + `suite-result.json` at suite root; `SuiteResult` to stdout | **GAP** | No emission code. Data types and path conventions (fixture-root relative) defined. |

### §10.1 Conformance Scenario Set

| Scenario file | Spec path | Status | Note |
|---|---|---|---|
| `smoke/twin-launch-and-ready.yaml` | `scenarios/smoke/twin-launch-and-ready.yaml` | CONFORMS | File present; parses via `ParseScenarioFile`; cadence=smoke; asserts `agent_ready`, `agent_completed`, `outcome SUCCESS`. `conformancecorpus_test.go:62`. |
| `smoke/checkpoint-and-merge.yaml` | `scenarios/smoke/checkpoint-and-merge.yaml` | CONFORMS | File present; cadence=smoke; asserts `checkpoint_written`, 2× `workspace_merge_status`, `commit_trailer_present(Harmonik-Run-ID)`, `outcome SUCCESS`. `conformancecorpus_test.go:68`. |
| `regression/twin-failure-classification.yaml` | `scenarios/regression/twin-failure-classification.yaml` | CONFORMS | File present; cadence=regression; asserts `agent_failed{error_category=ErrStructural}`, `event_absent(outcome_emitted)`, `outcome FAIL`. `conformancecorpus_test.go:74`. |
| Corpus parse test | `internal/scenario/conformancecorpus_test.go:48` | CONFORMS | `TestConformanceCorpus_SH101ScenariosParse` verifies all three files parse, have expected cadence, and declare ≥ minimum assertion counts. |

### §6.4 Twin-Extension Wire Messages

| Message type | Spec fields | Status | Note |
|---|---|---|---|
| `twin_settings_loaded` | type, permissions_present, stop_hook_present, stop_hook_command (≤200 chars) | CONFORMS | `cmd/harmonik-twin-claude/wire.go:464`; truncation at 200 chars enforced. |
| `twin_hook_called` | type, hook_type, exit_code, duration_ms | CONFORMS | `wire.go:498`. |
| `twin_committed` | type, commit_sha, exit_code, duration_ms, stderr_excerpt | **GAP (doc)** | Implemented at `wire.go:530` (hk-8ys88) but NOT documented in spec §6.4. Minor documentation gap; no spec violation by the twin binary itself. |
| `twin_error` | type, reason | **GAP (doc)** | Implemented at `wire.go:557` but NOT documented in spec §6.4. Same category as above. |

---

## v0.2.0 → v0.2.2 Delta Assessment

The task note states hk-i0tw was scoped against v0.2.0. Additions since then:

| Addition | Version | Status |
|---|---|---|
| SH-015a (workspace snapshot mechanism — in-place, not copy) | v0.2.0 | CONFORMS — `sh015a_workspace_snapshot.go` |
| SH-016a (per-scenario synthetic project root) | v0.2.0 | CONFORMS — `synthprojectroot.go` |
| SH-032 (harness CLI surface) | v0.2.0 | **GAP** — not implemented |
| SH-033 (signal handling) | v0.2.0 | **GAP** — not implemented |
| SH-034 (result emission durability) | v0.2.0 | **GAP** — not implemented |
| §8.0 failure-class precedence table | v0.2.0 | CONFORMS — `Precedence()` / `HigherPrecedenceThan()` in `failureclass.go` |
| `ScenarioFile`: `workflow_path`/`workflow_id` split (was `workflow_ref`) | v0.2.0 | CONFORMS |
| `GitSeedOp`, `FileSeed` as distinct records | v0.2.0 | CONFORMS |
| `ScenarioResult.source_path`, relative `event_log_path`/`workspace_snapshot_path`, `stdout_log_paths`/`stderr_log_paths` | v0.2.0 | CONFORMS |
| §6.4 `twin_settings_loaded` + `twin_hook_called` | v0.2.2 | CONFORMS for the two declared types |

---

## Gaps Summary

Gaps are listed in descending severity. **Blocker** = required for any S07 conformance claim. **Major** = required for MVH but not on the critical path to a single scenario running. **Minor** = informational / nightly / documentation.

| # | Spec req | Code location (gap) | Severity | Description |
|---|---|---|---|---|
| G-01 | SH-032 | `cmd/harmonik/main.go` | **blocker** | `harmonik harness` subcommand absent. The 8 CLI flags (--cadence, --scenario, --fixture-root, --twin-search-path, --list, --dry-run, --output, --verbose) and 5 exit codes are unimplemented. The harness is not operable as a standalone command. |
| G-02 | SH-017 | (not present) | **blocker** | No orchestration drive: code that invokes the daemon entry-point (same composition root as `daemon` mode) with per-scenario working directory and handler-config overrides does not exist. |
| G-03 | SH-015 | (not present) | **blocker** | Fixture teardown not implemented. SH-015 sub-steps (a) handler subprocess termination, (b) lease release, (c) event-log fsync+close, (d) daemon stop RPC, and idempotency guarantee are all absent. |
| G-04 | SH-006, SH-007 | (not present) | **blocker** | Suite-load phase absent: no recursive `.yaml`-only discovery under `scenarios/`, no duplicate-name detection, no byte-lexicographic ordering of execution. |
| G-05 | SH-020 to SH-024 | (not present) | **blocker** | Assertion evaluation engine absent. No evaluator for the four assertion kinds (event_present, event_absent, workspace_state, exit_code), no dotted-path payload-match walker, no no-short-circuit runner, no JSONL reader with torn-tail handling. |
| G-06 | SH-034 | (not present) | **blocker** | Result emission absent. No code writes `<fixture-root>/<name>/result.json` or `<fixture-root>/suite-result.json`, nor emits `SuiteResult` to stdout. |
| G-07 | SH-033 | (not present) | **blocker** | Signal handling absent (depends on G-01 CLI). No SIGINT/SIGTERM handler; no graceful-shutdown path; no double-SIGINT hard-exit. |
| G-08 | SH-026 | (not present) | **major** | Timeout enforcement absent. No monotonic-clock deadline, no `daemon stop` cancellation call, no partial assertion evaluation on timeout path. `timeout_secs` is validated in `ScenarioFile.Valid()` but never consumed by an executor. |
| G-09 | SH-028 | (not present) | **major** | Network-access sandbox absent. No `unshare(CLONE_NEWNET)` on Linux, no `pf` packet-filter ruleset on macOS. Non-loopback outbound connections are undetected. |
| G-10 | SH-009 | `cmd/harmonik/main.go` (not present) | **major** | Twin-search-path precedence loop not wired. `BootstrapFixture` accepts caller-supplied `twinSearchPaths` but the CLI-flag → env-var → `<repo-root>/twins/` precedence resolution that produces this slice is absent from any harness startup path. |
| G-11 | SH-030 | (not present) | **major** | Matrix name-expansion and text/template substitution absent. `matrixCellCount` validates the 1024 cap correctly, but the synthetic name format `<name>[k1=v1,k2=v2,...]` (byte-lex key order) and `{{.paramName}}` template substitution are not implemented. |
| G-12 | SH-INV-002 | (not present) | **major** | Post-suite sensor test absent. No code checks (i) descendant process tree for `HARMONIK_RUN_ID`-marked processes, (ii) worktree-lease registry for held leases, (iii) `lsof`-equivalent for open event-log file descriptors, after the last teardown and before `SuiteResult` emission. |
| G-13 | SH-INV-003 | (not present) | **major** | Pre-launch twin-path check absent. No code verifies at handler-config resolution time that the resolved binary is under the configured twin-search-path prefix (or is a hash-checked registered twin per HC-043). The must-fail scenario for `/usr/bin/claude` is untested. |
| G-14 | SH-INV-004 | (not present) | **minor** | Nightly rerun-diff sensor absent. No test reruns the `regression`-cadence subset N≥10 times and diffs the `(verdict, failure_class, assertion_kind, event-type multiset)` field set across runs. |
| G-15 | §6.4 (doc gap) | `cmd/harmonik-twin-claude/wire.go:530,557` | **minor** | `twin_committed` (hk-8ys88) and `twin_error` implemented in `wire.go` are not documented in spec §6.4 (v0.2.2). The spec states §6.4 matches `wire.go` exactly; it does not match the two extra types. Spec documentation needs a v0.2.3 patch to add these or confirm they are intentionally out of scope. |

---

## Final Verdict

**GAPS FOUND: 15**

The `internal/scenario/` data layer — record types (all 12 §6.1 records), the 8-class FailureClass enum with precedence table, and the fixture bootstrap infrastructure — **fully conforms** to spec v0.2.2. The conformance scenario YAML files and corpus parse test also conform. SH-INV-001 (no test-mode branches) is satisfied by the production code corpus.

The implementation gaps are concentrated in the **execution layer**: the `harmonik harness` CLI subcommand (G-01), the orchestration drive (G-02), fixture teardown (G-03), suite-load discovery/ordering (G-04), the assertion evaluation engine (G-05), result emission (G-06), and signal handling (G-07) are all absent — 7 blocker gaps that together mean no scenario can be loaded, driven, or reported on via the harness today. The remaining gaps (G-08 through G-15) are major/minor and are subordinate to the blockers. All v0.2.0 data-model additions (SH-015a, SH-016a, §8.0 precedence table, schema field splits) are correctly implemented.
