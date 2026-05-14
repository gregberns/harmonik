# Scenario Harness

```yaml
---
title: Scenario Harness
spec-id: scenario-harness
requirement-prefix: SH
status: reviewed
spec-shape: requirements-first
spec-category: runtime-subsystem
version: 0.2.2
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-05-14
depends-on:
  - architecture
  - handler-contract
  - event-model
  - workspace-model
  - execution-model
  - operator-nfr
  - process-lifecycle
---
```

## 1. Purpose

This spec defines harmonik's scenario harness: the end-to-end test rig that loads declarative scenario files, drives the production daemon stack against twin-handler binaries instead of real-model handlers, captures the resulting event-log JSONL plus workspace state, evaluates declared assertions, and emits a typed scenario verdict. It is normative for every component that participates in scenario execution — the harness binary itself, the assertion evaluator, the fixture lifecycle, and the conformance scenarios that prove a harmonik build can run end-to-end without network access.

The harness is a separate spec from the foundation 10 because its substance is a test-time orchestration layer with its own contracts (scenario-file shape, assertion vocabulary, verdict format, fixture-lifecycle invariants) that do not belong inside any single subsystem. It is load-bearing: per [docs/bootstrap.md §5 step 8] and [docs/goals/bootstrapping-self-building.md], the scenario harness is the regression net for self-build cycles — without it, a self-build cycle that subtly breaks orchestration cannot be caught before the next cycle compounds the breakage. The seed framing lives at [docs/subsystems/scenario-harness.md] and is superseded normatively by this spec.

## 2. Scope

### 2.1 In scope

- Scenario-file format (declarative YAML at v0.1; loaded without executing scenario code).
- Scenario-file location convention under `scenarios/`.
- Scenario loading and parsing semantics, including malformed-scenario behavior.
- Twin-binary substitution mechanism: handler config knob only, not a runtime branch.
- Same-code-paths-as-production invariant enforcement on the production-imported packages the harness invokes.
- Orchestration drive: harness drives the same orchestrator entry-point as `daemon` mode.
- Workspace fixture lifecycle (setup, isolation, teardown) atop [workspace-model.md §4.1, §4.3].
- Event-log capture: JSONL produced during the run is captured and read for assertion evaluation.
- Assertion vocabulary at MVH: event-presence predicate, event-absence predicate, workspace-state predicate, exit-code predicate.
- Per-scenario wall-clock timeout; failure class on exceedance.
- Failure taxonomy (§8) and its coupling to [execution-model.md §8] failure classes.
- Repeatability (deterministic on observable surface) given identical inputs.
- Cleanup invariants on success, assertion failure, scenario timeout, and harness internal error.
- Cadence support (smoke / regression / nightly via a tag declared on each scenario).
- Per-scenario verdict format (`ScenarioResult` record).
- Scenario composition (parameter matrix per scenario name).
- Twin-binary discovery: PATH prefix + scenario-declared absolute path; defined error class on missing.
- Concurrent-execution policy at MVH: sequential.
- Network-access policy: harness MUST run without external network access.
- Conformance scenario set the harness MUST execute to claim S07 conformance.

### 2.2 Out of scope

- The digital-twin binaries themselves (`claude-twin`, `pi-twin`) — owned by [handler-contract.md §4.8] for the parity contract and by per-handler specs (post-MVH) for behavior scripting. This spec consumes the twin surface; it does not specify it.
- The orchestrator graph machinery (workflow validation, edge cascade, transition emission) — owned by [execution-model.md §4.6, §4.10]. The harness drives the orchestrator; it does not implement it.
- CI scheduling, cron, GitHub Actions wiring — operator-domain scheduling, owned by `operator-nfr.md` once that spec defines a scheduling surface; this spec is normative for cadence tagging, not for who runs which cadence when.
- The conformance suite that uses real model calls — explicitly distinct from the scenario suite per [docs/goals/end-to-end-testability.md] and [docs/concepts/digital-twins.md]; that suite is post-MVH and is not S07.
- Scenario-authoring UX (editor support, scaffolding tools, scenario diff visualizers) — post-MVH operator-tooling concerns.
- Twin-conformance drift detection (real-vs-twin output comparison over time) — declared in scope of S07 by [handler-contract.md §4.8.HC-038], deferred from this v0.1 draft to a post-MVH revision (tracked at OQ-SH-008).
- Beads-integration verification at scenario level — beads writes are observable through the same JSONL surface this spec captures, but BI's per-write contracts are owned by [beads-integration.md §4.4].

## 3. Glossary

- **scenario** — a declarative test definition (a single YAML file under `scenarios/`) naming a workflow, twin-binary overrides, fixture inputs, expected events, expected workspace state, a wall-clock budget, and a cadence tag. (see §4.1)
- **scenario file** — the on-disk YAML form of a scenario, parsed into the `ScenarioFile` record of §6.1. (see §4.1, §6.1)
- **fixture** — the per-scenario filesystem and process state set up before orchestration starts and torn down after the verdict is emitted; includes the workspace seed, the event-log directory, and any twin-binary search paths. (see §4.4)
- **twin substitution** — the mechanism by which a scenario's `agent_overrides` field selects a twin-handler binary in place of a real-handler binary at workflow-load time, via the same `handler_ref` resolution path declared in [handler-contract.md §4.1.HC-003]. (see §4.3)
- **assertion** — a declared expectation in a scenario file evaluated against the captured observable surface (event log + workspace state + exit code) after orchestration completes. (see §4.6)
- **scenario result** — the record produced for every scenario after evaluation, capturing verdict, timings, failure class (when applicable), and pointers to the captured event-log and workspace-snapshot artifacts. (see §6.1)
- **conformance suite** — a separate test suite (post-MVH) that runs against real-model handlers; explicitly distinct from the scenario suite this spec governs. Naming overlap is acknowledged here so reviewers do not conflate the two.
- **cadence tag** — one of `smoke | regression | nightly`, declared on every scenario, that determines which scenarios run in which CI lane. (see §4.9)
- **harness verdict** — the terminal `ScenarioResult.verdict` value: one of `pass | fail | timeout | error`. `fail` means an assertion failed; `timeout` means the scenario exceeded its wall-clock budget; `error` means the harness itself or its orchestration drive failed before a verdict could be reached on the scenario's own terms. (see §6.1, §8)

## 4. Normative requirements

### 4.a Subsystem envelope

#### SH-ENV-001 — Envelope declaration

Envelope for the scenario-harness subsystem (S07) per [architecture.md §4.0 AR-053]. The harness is a test-time orchestration subsystem that drives the production daemon stack against twin-handler binaries; it is a bus consumer (observational replay reader per [event-model.md §4.5 EV-021]) and emits no cross-bus events, so the envelope's event-production element is `none` and the event-consumption element spans the full [event-model.md §8] taxonomy via the captured JSONL surface.

(a) Events produced: none. The harness emits no events on the cross-subsystem bus per §6.2; it produces only its own `ScenarioResult` / `SuiteResult` records on the harness CLI surface (per §6.1, SH-034).

(b) Events consumed:
  - All event types declared in [event-model.md §8] are consumed observationally via the captured JSONL event log per SH-020 (`event_present` / `event_absent` assertion evaluation). The harness reads the envelope of [event-model.md §6.1 Event] without redefining it; payload-shape interpretation for `payload_match` predicates is delegated to the owning event's §8 schema.
  - `run_completed` / `run_failed` payload field `outcome_status` per [event-model.md §8.1.8] — consumed by `OutcomeExpectation` per §6.1.
  - `workspace_merge_status` per [event-model.md §8.5.3] — consumed by the `scenarios/smoke/checkpoint-and-merge.yaml` conformance assertion per §10.1.
  - `agent_ready`, `agent_completed`, `agent_failed`, `outcome_emitted` per [event-model.md §8.3] — consumed by the §10.1 conformance scenario set.
  - `checkpoint_written` per [event-model.md §8.4] — consumed by the §10.1 checkpoint-and-merge scenario.

(c) Types introduced (cross-subsystem):
  | Type | `Tags:` | `Axes:` (if non-baseline) |
  |---|---|---|
  | `ScenarioFile` (§6.1) | mechanism | baseline |
  | `AgentOverride` (§6.1) | mechanism | baseline |
  | `FixtureSetup` (§6.1) | mechanism | baseline |
  | `GitSeedOp` (§6.1) | mechanism | baseline |
  | `FileSeed` (§6.1) | mechanism | `io-determinism=deterministic; idempotency=non-idempotent` (on-disk artifact) |
  | `EventExpectation` (§6.1) | mechanism | baseline |
  | `WorkspacePredicate` (§6.1) | mechanism | baseline |
  | `OutcomeExpectation` (§6.1) | mechanism | baseline |
  | `ScenarioResult` (§6.1) | mechanism | `io-determinism=deterministic; idempotency=non-idempotent` (on-disk artifact per SH-034) |
  | `AssertionResult` (§6.1) | mechanism | baseline |
  | `SuiteResult` (§6.1) | mechanism | `io-determinism=deterministic; idempotency=non-idempotent` (on-disk artifact per SH-034) |
  | `FailureClass` (§8 ENUM) | mechanism | baseline |

  These types are introduced by SH and observed at the harness CLI surface only; they are NOT bus-event payloads. Scenario-file consumption is internal to SH's loader (§4.1, §4.2); result-record emission is internal to SH's CLI driver (§4.13).

(d) Handlers implemented: none. The harness drives handlers via the twin-config-override mechanism of SH-008 against the production handler-resolution path of [handler-contract.md §4.1 HC-003]; it does not implement or host any handler class itself. Twin binaries are out of scope per §2.2 and owned by [handler-contract.md §4.8].

(e) State owned:
  - Per-suite ephemeral fixture root (`<fixture-root>/`) per SH-016 — operator-inspectable, not auto-deleted on suite completion.
  - Per-scenario synthetic project root (`<fixture-root>/<scenario-name>/`) per SH-016a — contains the scenario's `.harmonik/` tree, daemon socket / pidfile / events log, and worktree.
  - Per-scenario worktree (`<fixture-root>/<scenario-name>/workspace/`) per SH-013 — created via [workspace-model.md §4.1 WM-001]; lease-by-run rules of [workspace-model.md §4.3 WM-010] continue to apply.
  - Captured JSONL event log per SH-014 — written by the per-scenario daemon to the synthetic root's `.harmonik/events/events.jsonl` and read by the harness as the assertion surface.
  - Per-scenario captured stdout / stderr files per SH-020 (role-keyed `stdout_log_paths` / `stderr_log_paths` on `ScenarioResult`).
  - `workspace_snapshot_path` pointer per SH-015a — points at the in-place worktree directory; the harness does NOT copy or archive worktrees at teardown.
  - `ScenarioResult` and `SuiteResult` records per SH-034 — durably written to `<fixture-root>/<scenario-name>/result.json` and `<fixture-root>/suite.json`.

(f) Control points provided: none. The harness is a mechanism-tagged subsystem; its operations are not gates, hooks, guards, or budgets per [control-points.md §4.1]. Operator-policy surfaces consumed by the harness (`--cadence`, `--fixture-root`, `--twin-search-path`) are CLI flags, not control points.

(g) NFRs inherited / overridden:
  - Inherited: `ON-018` N-1 schema compatibility (cited in §6.3 schema-evolution prose for the future `ScenarioFile.schema_version` field per OQ-SH-007).
  - Inherited: `ON-029` drain-timeout escalation to SIGKILL (cited at SH-026 for the cancel-and-teardown wall-clock bound).
  - Overridden: none.

(h) Boundary classification per operation:
  | Operation | `Tags:` | Axes |
  |---|---|---|
  | `suite_load` (§4.2 SH-006) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `parse_scenario_file` (§4.1 SH-003) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `resolve_twin_binary` (§4.3 SH-009) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `fixture_setup` (§4.4 SH-012) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |
  | `synthesize_project_root` (§4.4 SH-016a) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |
  | `drive_orchestration` (§4.5 SH-017) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |
  | `capture_event_log` (§4.6 SH-020) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `evaluate_assertions` (§4.6 SH-021, SH-022) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `enforce_scenario_timeout` (§4.7 SH-026) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |
  | `fixture_teardown` (§4.4 SH-015) | mechanism | `llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent` |
  | `emit_scenario_result` (§4.13 SH-034) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |
  | `emit_suite_result` (§4.13 SH-034) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |

Tags: mechanism

### 4.1 Scenario file format

#### SH-001 — Scenario format is YAML at v0.1

Every scenario MUST be declared as a single YAML file conforming to the `ScenarioFile` schema in §6.1. Other formats (Go-as-code scenarios, DSLs, hybrid YAML+Go) are forbidden at v0.1; they are tracked as an open question (OQ-SH-001) for a post-MVH revision.

Tags: mechanism

#### SH-002 — Scenario files live under `scenarios/` at the repo root

Scenario files MUST be discovered under the repo-root directory `scenarios/` (recursive descent permitted; subdirectories MAY be used for grouping). The repo root is the directory returned by `git rev-parse --show-toplevel` invoked in the harness's working directory at startup. The harness MUST NOT load scenarios from any other location at MVH; absolute or workspace-relative scenario paths are forbidden as a security and reproducibility guard. The cross-platform file extension is `.yaml` (lower-case, byte-exact); files with any other extension (including `.yml`, `.YAML`) MUST be rejected at suite-load discovery time (without opening the file) and classified as `scenario-load-failure` per §8.1.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### SH-003 — Scenario file is declarative-loadable without executing scenario code

A scenario file MUST be parseable by a generic YAML loader plus the `ScenarioFile` schema validator (§6.1) without invoking any scenario-defined code, plugin, or external process. The harness MUST NOT support `!eval`-style YAML extensions, `!!python/object` constructors, or any tag that would execute embedded code during parse. The on-disk encoding MUST be UTF-8 without a byte-order mark; files with a BOM or any non-UTF-8 encoding MUST be rejected as `scenario-load-failure`. To bound resource exposure during parse, the harness MUST enforce a per-file size ceiling (default: 1 MiB) and a parsed-node count ceiling (default: 100 000 nodes) at suite-load; either ceiling overrun MUST be classified as `scenario-load-failure`. This is the structural property that lets review tooling and downstream agents reason about scenarios without running them.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### SH-004 — Malformed scenario files fail with `scenario-load-failure`

A scenario file that fails YAML parse, fails the §6.1 schema check, references an unknown workflow, or carries an unknown cadence-tag value MUST be classified as `scenario-load-failure` per §8. The scenario MUST NOT be partially executed; the harness MUST emit a `ScenarioResult` with `verdict=error` and `failure_class=scenario-load-failure`, and MUST move on to the next scenario in the suite.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### SH-005 — Scenario name uniqueness is repository-wide

Every scenario file MUST declare a `name` field that is unique across the entire `scenarios/` tree. Names MUST match the regular expression `^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$` (no slashes, no whitespace, no path-traversal sequences); names that do not match MUST fail with `scenario-load-failure`. Name collision after matrix expansion (per SH-030) is also a `scenario-load-failure`. The harness MUST detect duplicates at suite-load time, in which case the entire suite fails (not merely the duplicate scenarios) with `scenario-load-failure` carrying the conflicting paths. Name uniqueness is the addressing mechanism for cadence filtering, parameter expansion, and verdict reporting; collisions corrupt the surface.

Tags: mechanism

### 4.2 Suite loading and ordering

#### SH-006 — Suite-load is the discovery + parse + validate phase

The harness MUST execute scenario discovery, file parse, schema validation, and uniqueness check in a discrete `suite-load` phase before launching any orchestration. A suite-load that fails (any duplicates per SH-005, any parse errors, any schema errors) MUST abort the entire suite with a single suite-level error and MUST NOT execute any scenarios. Per-scenario load failures discovered during suite-load are still recorded as individual `ScenarioResult` entries with `verdict=error` so the operator sees the inventory.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### SH-007 — Scenario execution order is deterministic

Within a single suite invocation, the harness MUST execute scenarios in byte-lexicographic order of their `name` field (locale-independent UTF-8 byte comparison; not Unicode-collation order). Matrix-expanded synthetic names (per SH-030) participate in the same ordering. Determinism on order is required so that flaky-test bisection, log-diff comparison across runs, and crash-report localization are reproducible.

Tags: mechanism

### 4.3 Twin substitution

#### SH-008 — Twin substitution is a handler-config override, not a runtime branch

Twin-binary substitution MUST be expressed as a scenario-file `agent_overrides` map that is applied to the workflow's resolved handler configuration at workflow-load time per [handler-contract.md §4.1.HC-003]. The harness MUST NOT introduce any runtime branch in production-imported daemon, orchestrator, watcher, or adapter code that selects between real and twin handlers; the only mutation the harness performs on the production stack is at handler-config resolution. This requirement enforces [handler-contract.md §5.HC-INV-002] (twins are indistinguishable from real handlers to the daemon) at the harness boundary.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### SH-009 — Twin-binary discovery uses scenario path or PATH prefix

A scenario's `agent_overrides[role]` value MUST resolve to a twin binary by one of two mechanisms: (a) an absolute path declared in the scenario, or (b) a name resolved against a configured twin-binary search-path prefix delivered to the harness at startup. The harness MUST NOT perform unrestricted `$PATH` lookup for twin binaries; resolution mirrors the in-repo handler-binary rule at [handler-contract.md §4.10.HC-042]. The twin-search-path source is, in precedence order: (i) the harness CLI flag `--twin-search-path <dir>`; (ii) the environment variable `HARMONIK_TWIN_SEARCH_PATH`; (iii) the in-tree default `<repo-root>/twins/`. Resolved twin paths remain subject to [handler-contract.md §4.10.HC-043] commit-hash verification and [handler-contract.md §4.10.HC-045] launch-rule conformance unchanged; the harness performs no carve-out from the daemon's pre-launch hash check. A twin binary that fails the hash check MUST fail with `verdict=error` and `failure_class=twin-binary-not-found` (binary present but version-mismatched is a discovery failure for the harness's purposes).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### SH-010 — Missing twin binary fails the scenario with `twin-binary-not-found`

If a scenario's `agent_overrides` references a twin binary that does not exist (absolute path missing on disk, or name not resolvable against the search-path prefix), the harness MUST emit a `ScenarioResult` with `verdict=error` and `failure_class=twin-binary-not-found` BEFORE attempting to launch the orchestration drive. The error MUST carry the unresolved name and the search paths consulted.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### SH-011 — Twin parity is the parity surface declared in HC §4.8

The harness consumes the parity surface declared at [handler-contract.md §4.8] (HC-035 through HC-040). The harness MUST NOT inspect the twin's internal state, log file shape, or process tree as a means of evaluating scenario assertions; observability of twin behavior for assertion purposes MUST go through the same handler-contract progress-stream / event-bus surface as real handlers. Process-tree inspection at fixture teardown for the SH-INV-002 sanity check (live-process accounting) is a hygiene operation distinct from behavioral observation and is permitted; it MUST NOT influence scenario verdicts.

Tags: mechanism

### 4.4 Workspace fixture lifecycle

#### SH-012 — Fixture setup precedes orchestration

For every scenario, the harness MUST execute a fixture-setup phase before invoking the orchestration drive of §4.5. Fixture setup MUST: (a) create a fresh workspace conforming to [workspace-model.md §4.1.WM-001] and [workspace-model.md §4.2 branching model], seeded from the scenario's declared `fixture_setup` instructions; (b) create an isolated event-log directory; (c) prepare the twin-binary search path. Fixture-setup failure MUST classify as `fixture-setup-failed` per §8 and MUST NOT proceed to orchestration. On partial-success failure (any of (a), (b), (c) succeeds before a later step fails), the harness MUST execute fixture teardown (per SH-015) best-effort against the partially-constructed fixture before recording the failure; teardown errors during partial-rollback are appended to `error_detail` but do NOT change the failure class (it remains `fixture-setup-failed`, never `cleanup-failed`).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### SH-013 — Each scenario uses an isolated workspace

Each scenario's workspace MUST be disjoint from every other scenario's workspace and from the operator's working tree. "Disjoint" means: no two scenarios' canonical workspace paths share a prefix and no symlink under one workspace resolves to a path under another. The harness MUST place each scenario's workspace at `<fixture-root>/<scenario-name>/workspace/` (with the matrix-expanded synthetic `<scenario-name>` for SH-030 cells); this naming convention guarantees disjointness given SH-005 name uniqueness. Workspaces MUST be created under a per-suite ephemeral root (see SH-016) and MUST NOT reuse any path from a prior suite invocation. Lease-by-run rules of [workspace-model.md §4.3.WM-010] continue to apply inside the harness; the harness does not bypass them. The harness MUST verify no path-collision at suite-load (a naming-convention lint that fails fast on collision).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### SH-014 — Each scenario uses an isolated event-log directory

Each scenario MUST capture its events into a dedicated JSONL file under the scenario's synthetic project root (per SH-016a), NOT into the operator's `.harmonik/events/events.jsonl`. Because each scenario runs against a per-scenario synthetic project root, the daemon's normal `.harmonik/events/events.jsonl` write target lands inside the synthetic root by construction — no path-override surface is mutated in production code. The harness MUST NOT read from, write to, or otherwise mutate the operator's `.harmonik/` tree (the `.harmonik/` directory under `git rev-parse --show-toplevel`); reads of the daemon socket, pidfile, or event log of an unrelated operator daemon are explicitly forbidden.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### SH-015 — Fixture teardown runs on every terminal path

The harness MUST execute fixture teardown on every terminal scenario path (pass, fail, timeout, error). Teardown is run-to-completion best-effort: a failure in any sub-step MUST NOT halt the remaining sub-steps; all errors are accumulated and reported. Teardown MUST execute the following sub-steps in order: (a) terminate any still-live handler subprocesses spawned by the orchestration drive, honoring [handler-contract.md §4.4.HC-018] cancellation bounds (per-handler T_cancel ceiling; total subprocess-cleanup wall-clock is bounded by `N × HC-018-ceiling` where N is the live-subprocess count at teardown entry); (b) release any worktree leases per [workspace-model.md §4.3.WM-013b]; (c) close the event-log file (fsync then close); (d) stop the per-scenario daemon via the `daemon stop` RPC of [process-lifecycle.md §4.2.PL-003a] (graceful drain, bounded by the daemon's drain-timeout per [operator-nfr.md §4.7]); (e) record a `workspace_snapshot_path` recording obligation (NOT a termination action) — see SH-015a for the snapshot mechanism. Teardown is idempotent (calling it twice is a no-op on already-completed sub-steps). On any sub-step failure, teardown classifies as `cleanup-failed` per §8.8; verdict-downgrade follows the precedence table at §8.0.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

#### SH-015a — Workspace snapshot mechanism (new in v0.2)

The `workspace_snapshot_path` recorded in `ScenarioResult` MUST point at the per-scenario worktree directory in-place (i.e., the same path created at SH-012 fixture-up). The harness MUST NOT copy, archive, or otherwise relocate the worktree at teardown; SH-016's "fixture root is not auto-deleted" rule preserves the worktree for post-hoc inspection. `workspace_state` predicates (SH-022) that resolve `git_ref_at` or `commit_trailer_present` MUST inspect the worktree's `.git/` directly via `git` plumbing commands; predicates that resolve `file_exists`, `file_contents_equal`, or `file_contents_match` MUST inspect the worktree's working files directly. The snapshot is captured AFTER (a)-(d) of SH-015: lease release and event-log close happen first, but the worktree files and refs are NOT modified by the teardown sub-steps themselves (any merge-back to integration occurred during orchestration per [workspace-model.md §4.5.WM-019], BEFORE teardown).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### SH-016 — Per-suite ephemeral fixture root

The harness MUST create a per-suite ephemeral fixture root (under an OS-temp directory by default, or under a configured override via `--fixture-root <path>`) and place all scenario fixture state inside it. The harness MUST NOT delete the fixture root automatically on suite completion — operators inspect it for debugging; deletion is operator-driven (via an explicit `harmonik harness clean` subcommand or manual `rm -rf`). Subsequent suite invocations create their own fresh fixture roots; prior fixture roots accumulate until the operator deletes them. The fixture-root path MUST be reported in suite-level output (`SuiteResult.fixture_root`).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### SH-016a — Per-scenario synthetic project root (new in v0.2)

Each scenario invocation MUST run against an ephemeral per-scenario synthetic project root at `<fixture-root>/<scenario-name>/project/` (with the matrix-expanded `<scenario-name>` for SH-030 cells). The synthetic project root is the working-directory the harness uses when invoking the daemon entry-point of SH-017. Because the daemon's startup sequence per [process-lifecycle.md §4.2.PL-005] writes `.harmonik/daemon.pid`, `.harmonik/daemon.sock`, `.harmonik/daemon.instance-id`, and reads `.harmonik/daemon.state` relative to its working directory, those writes land inside the synthetic root by construction; the operator's `.harmonik/` tree is untouched (SH-014). Each synthetic root contains: (i) a fresh git repository (`git init` plus the `fixture_setup.git_seed` ops); (ii) an empty `.harmonik/` skeleton minted by the daemon at PL-005 step 0; (iii) the per-scenario Beads SQLite store at `.harmonik/beads.sqlite`; (iv) the per-scenario event log at `.harmonik/events/events.jsonl` (matching SH-014). One daemon-per-project per [process-lifecycle.md §4.1.PL-001] is satisfied because each scenario IS a different project from the daemon's perspective — each scenario starts a fresh daemon at `<fixture-root>/<scenario-name>/project/` and stops it at teardown (per SH-015 sub-step (d)). PL-005 step 8 reconciliation runs against the freshly-seeded fixture state and is a no-op by construction (no prior runs, no store divergence); the harness MUST NOT skip the step. Synthesizing the project root via working-directory assignment is the v0.2 mechanism; whether a daemon-side `--project-root` flag is needed for future flexibility is tracked at OQ-SH-011.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

### 4.5 Orchestration drive

#### SH-017 — Harness uses the production orchestrator entry-point

The harness MUST drive scenarios by invoking the same daemon entry-point and the same orchestrator startup sequence as production `daemon` mode per [process-lifecycle.md §4.2.PL-005]. "Same entry-point" means the same composition-root function in the same Go package, invoked with a different working directory (the per-scenario synthetic project root per SH-016a) and a different external configuration (handler-config overrides per SH-008). The harness MUST NOT bypass the daemon, MUST NOT instantiate the orchestrator directly outside the production composition root, and MUST NOT skip startup-sequence steps (PL-005 steps 0–9 all run; reconciliation in step 8 is a no-op against fresh fixture state). The harness MUST detect daemon process death (e.g., via the daemon's child-process exit on the harness's own process tree) and classify a daemon crash as `orchestration-internal-error`. This requirement is the load-bearing enforcement of the "same code paths as production" design principle.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### SH-018 — No test-mode branches in production-imported packages

Production-imported packages (the daemon, the orchestrator, the agent-runner, the workspace manager, the hook system, the policy engine, the event bus, any handler implementation) MUST NOT contain conditional branches keyed off "is this a test?" / "is this scenario mode?" / "is this a twin?" / "is this a harness invocation?". The harness applies the production stack with two and only two surface mutations: handler-config overrides (§4.3) and working-directory assignment to the per-scenario synthetic project root (§4.4 SH-016a). Forbidden token set (canonical list, also enforced by §5 SH-INV-001's grep): `scenarioMode`, `isTest`, `isTwin`, `harnessMode`, `isFakeRunner`, `useStub`, `if agent_type == "*-twin"`, `HasSuffix("-twin")`, `cfg.TestMode`, and any environment variable matching `HARMONIK_*_MODE`. The reviewer obligation to reject PRs introducing such branches is a §10.2 conformance test obligation, not a runtime requirement.

Tags: mechanism

#### SH-019 — Orchestration internal error fails with `orchestration-internal-error`

If the orchestration drive returns an error from the daemon's startup, dispatch, or shutdown that is not a scenario-attributable failure, the harness MUST classify the scenario as `verdict=error` with `failure_class=orchestration-internal-error`. "Scenario-attributable failure" is the closed set: (i) a handler error surfaced as `agent_failed` per [handler-contract.md §4.6.HC-024]; (ii) a twin-scripted failure surfaced as the same; (iii) a gate denial surfaced as a node-level outcome; (iv) any condition the orchestrator's edge-cascade resolves into a terminal `run_failed` event. Anything else (daemon crash detected via process-exit, RPC error from the daemon socket, panic in the orchestrator goroutine, daemon entered `degraded` state per [process-lifecycle.md §4.4 PL-010] mid-scenario, store-divergence detected by reconciliation mid-scenario) is `orchestration-internal-error`. The original error MUST be captured verbatim in `ScenarioResult.error_detail` (a non-empty operator-readable string with at minimum the underlying `err.Error()` and any captured panic stack). This class is a harness-or-daemon defect, NOT an assertion failure; review escalation is automatic.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 4.6 Event-log capture and assertion evaluation

#### SH-020 — Harness reads the captured JSONL event log as the assertion surface

After orchestration completes (either by normal terminal-state emission or by timeout cancellation), the harness MUST read the captured JSONL event log and evaluate each declared assertion against it. The read MUST conform to [event-model.md §4.5.EV-021] observational-replay rules: the harness is performing observational replay; output is advisory for state but authoritative for "what events were observed during this scenario." The harness MUST NOT reconstruct state from JSONL; declared `expected_workspace` predicates inspect the workspace tree directly per SH-022. The harness MUST also capture each handler subprocess's stdout and stderr to per-scenario files at `<fixture-root>/<scenario-name>/logs/<role>-{stdout,stderr}.log` for operator debugging; these files are not part of the assertion surface (SH-021 assertions do not read them) but their paths MUST be reported in `ScenarioResult` (see §6.1).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### SH-021 — Assertion vocabulary at v0.1 is four kinds

The harness MUST support exactly four assertion kinds at v0.1: (a) `event_present` — an event of declared `type` and (optionally) declared payload-field-equals predicates was emitted; (b) `event_absent` — no event of declared `type` matching declared predicates was emitted during the scenario's wall-clock window; (c) `workspace_state` — a declared filesystem or git-ref predicate over the post-orchestration workspace tree holds; (d) `exit_code` — the run-level terminal-emission event payload (`run_completed{status=...}` or `run_failed{status=...}` per [event-model.md §8.1.8]) carries a declared `outcome_status` value matching the [execution-model.md §4.1 EM-005] `Outcome.status` enum.

`payload_match` keys MUST use dotted-path grammar (e.g., `error.category` addresses the `category` field of a top-level `error` object); array indices use bracket form (`items[0].id`). Equality is value-equal under JSON canonical types (numbers compare by numeric value: `1 == 1.0`; strings compare byte-equal in NFC normalization; booleans by identity; null by identity); shallow-merge semantics — declared keys MUST appear in the actual payload with equal values, but the actual payload MAY contain additional unmatched keys.

The wall-clock window for `event_absent` is `[fixture-setup-completion, terminal-event-emission OR timeout-cancellation-completion]`. On timeout the window's right edge is the moment the harness completes orchestration cancellation per SH-026; events emitted after cancellation completion are not observable to assertions. Absence-window confidence depends on scenario duration; this is acknowledged operator-side and not a determinism defect.

`assertion_results` ordering: declared assertions are evaluated in the union order of `expected_events` (declaration order), then `expected_workspace` (declaration order), then `expected_outcome` (single entry if declared); `AssertionResult` records appear in the same order in `ScenarioResult.assertion_results`. The harness MUST NOT short-circuit (per SH-023).

Composite predicates, cross-event ordering predicates, and other assertion kinds are tracked at OQ-SH-006 for a post-MVH revision.

Tags: mechanism

#### SH-022 — Workspace-state predicates inspect the captured snapshot

The `workspace_state` assertion kind MUST be evaluated against the per-scenario worktree tree in-place (per SH-015a) — NOT against a copy or archive. The harness MUST NOT mutate the workspace during evaluation. Predicates MUST address files by repo-relative path; absolute-path predicates are forbidden so the same scenario is portable across operator machines. Symlinks under the worktree that resolve outside the worktree (path traversal) MUST be rejected at predicate-evaluation time as `assertion-failed` (the operator's filesystem state is not part of the contract). Per-kind interpretation of the `expected` field is declared at §6.3.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### SH-023 — Assertion failure produces verdict `fail` with `assertion-failed`

When any declared assertion fails to hold, the harness MUST set `ScenarioResult.verdict=fail` and `failure_class=assertion-failed`. The result MUST carry one `AssertionResult` record per declared expectation (§6.1), distinguishing which assertions passed and which failed. The harness MUST NOT short-circuit on first assertion failure — every declared assertion MUST be evaluated so the operator sees the full failure picture. Even on `verdict=timeout` (per SH-026) the harness MUST evaluate all declared assertions best-effort against the partial event log (assertions whose evaluation is impossible due to missing data are recorded with `passed=false` and an explanatory `error_detail`-style note); the verdict remains `timeout`.

> RATIONALE: Full evaluation is a deliberate cost-vs-debuggability tradeoff. Scenarios are short by construction (the SH-026 timeout bounds scenario duration) and full evaluation is cheap; partial-evaluation on timeout preserves the operator's ability to localize the timeout cause.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### SH-024 — Event-log capture failure escalates to `harness-internal-error`

The harness's read of the captured JSONL is governed by [event-model.md §6.2]'s post-fsync torn-tail rule: a torn tail at the end of the file (well-formed lines followed by a partial trailing record after the last fsync) MUST be skipped silently per EV §6.2 — this is normal post-fsync behavior, not a corruption signal. The harness MUST classify the scenario as `verdict=error` with `failure_class=harness-internal-error` (NOT as `assertion-failed`) when, and only when, the log read fails for one of: (i) the event-log directory or file does not exist; (ii) a permissions error prevents reading; (iii) a JSON-parse error occurs at a position other than the post-fsync tail (mid-file corruption); (iv) a disk-full or other I/O error during read; (v) an `EV-011a bus_overflow` event is observed in the captured log (events were shed during the scenario, defeating assertion completeness). The harness MUST NOT silently treat absent events as "not emitted" when the log itself is corrupt or incomplete.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 4.7 Scenario timeout

#### SH-025 — Every scenario declares a wall-clock budget

Every `ScenarioFile` MUST declare a positive `timeout_secs` field (integer seconds, range `[1, 7200]`; values outside the range MUST fail at scenario-load time per SH-004). The deadline MUST be measured against a monotonic clock (e.g., Go's `time.Now()` returning a `Time` whose monotonic component is preferred for comparison) so that wall-clock regressions during NTP corrections do not cause spurious timeouts; wall-clock timestamps are recorded in `ScenarioResult.started_at` / `completed_at` (RFC 3339 with millisecond precision in UTC) for operator-facing display only. There is no harness-default budget; explicit declaration prevents accidental long-runners from masquerading as fast scenarios.

Tags: mechanism

#### SH-026 — Timeout exceedance produces verdict `timeout` with `scenario-timeout`

When a scenario's orchestration drive does not reach a terminal state within `timeout_secs` (measured per SH-025 from fixture-setup completion to the orchestrator emitting a terminal `run_completed` / `run_failed` event), the harness MUST cancel the orchestration by invoking the per-scenario daemon's `stop` RPC of [process-lifecycle.md §4.2.PL-003a] (graceful drain, escalating to SIGKILL of agent subprocesses on drain-timeout per [operator-nfr.md §4.7 ON-029]). Because each scenario runs against its own per-scenario daemon (per SH-016a), `daemon stop` halts the scenario's run cleanly without affecting any other scenario. The harness MUST then execute fixture teardown per SH-015 and emit a `ScenarioResult` with `verdict=timeout` and `failure_class=scenario-timeout`. Per-handler cancellation MUST honor the bounded-cancellation contract of [handler-contract.md §4.4.HC-018]; the harness's overall cancel-and-teardown wall-clock bound is `(N × HC-018-ceiling) + ON-029-drain-timeout`, where N is the live-handler count at timeout. Suite-mode efficiency (a per-scenario cancel that does NOT terminate the daemon) is post-MVH and tracked at OQ-SH-012.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

### 4.8 Repeatability and determinism

#### SH-027 — Identical inputs MUST produce identical observable verdicts

Given identical inputs (the same harmonik build, the same twin binaries, the same scenario file, the same `fixture_setup` seed), the harness MUST produce identical values for the determinism contract field set on every run. The contract field set is: `ScenarioResult.verdict`, `ScenarioResult.failure_class`, the ordered list of `(AssertionResult.passed, AssertionResult.assertion_kind)` tuples, and the multiset of distinct event types observed in the captured JSONL. Byte-identity of the captured JSONL is NOT required (UUIDv7 event IDs, wall-clock timestamps, PIDs, and `daemon_instance_id` per PL-005 step 0 drift on every run); semantic identity of the contract field set is.

Scope carve-out: Twin binaries that exercise the wall-clock heartbeat mode of [handler-contract.md §4.6.HC-026a] (rather than the scripted scenario-mode carve-out) are exempt from this determinism rule. Such scenarios MUST be tagged `cadence_tag=nightly` and are explicitly excluded from the SH-INV-004 sensor's rerun-diff check. Determinism for the verdict and failure-class fields applies to all other scenarios unconditionally.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### SH-028 — Harness MUST NOT depend on external network access

Every scenario MUST execute without any non-loopback outbound network call from the harness, the per-scenario daemon, the orchestrator, the watchers, the agent-runner, or the twin binaries. Permitted: connections to `127.0.0.0/8` and `::1` (loopback, including the daemon's own UNIX-domain or local-TCP RPC socket and any local SQLite-over-network if used); connections to AF_UNIX sockets within the synthetic project root; filesystem-only operations. Forbidden: any connection to a non-loopback IPv4/IPv6 address; outbound DNS to a non-loopback resolver. Twin binaries explicitly do not call models per [docs/concepts/digital-twins.md]; the harness MUST verify that no scenario can succeed by accident with network access.

Verification mechanism floor: on Linux, the harness CONFORMANCE LANE MUST execute scenarios inside a network namespace created via `unshare(CLONE_NEWNET)` with only the loopback interface up, ensuring non-loopback outbound calls fail at the kernel; on macOS, the conformance lane MUST run under a `pf` packet-filter ruleset that drops all non-loopback egress (mechanism floor; declared `darwin-only` and tracked at OQ-SH-013 if mechanism choice changes). A scenario whose run produces a non-loopback connection attempt detectable by the sandbox MUST fail with `verdict=error` and `failure_class=harness-internal-error`. The §10.2 conformance lane MUST run with the sandbox enabled.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 4.9 Cadence support

#### SH-029 — Every scenario declares a cadence tag

Every `ScenarioFile` MUST declare `cadence_tag` ∈ `{smoke, regression, nightly}`. The harness MUST support a `--cadence <tag>` CLI flag whose accepted values are `smoke`, `regression`, `nightly`, and `all`. The cadence-superset relation:

| `--cadence <tag>` value | Includes scenarios with `cadence_tag` |
|---|---|
| `smoke` | smoke |
| `regression` | smoke, regression |
| `nightly` | smoke, regression, nightly |
| `all` (or flag omitted) | smoke, regression, nightly |

A `--cadence` filter that resolves to an empty scenario set (no matching scenarios) MUST emit a `SuiteResult` with `suite_verdict=pass` and an empty `results` list (vacuously true), and the harness exit code MUST be 0. There is no harness-default cadence assignment for scenario authoring; explicit declaration is the discipline.

Tags: mechanism

### 4.10 Scenario composition (parameter matrix)

#### SH-030 — Scenarios MAY declare a parameter matrix

A `ScenarioFile` MAY declare a `matrix` field: a map of parameter-name to list-of-values. The harness MUST expand the scenario into one execution per cell of the cartesian product, capped at 1024 cells per scenario (a scenario whose matrix produces more cells MUST fail at scenario-load time as `scenario-load-failure`). Synthetic per-cell `name`s MUST render as `<scenario-name>[<k1>=<v1>,<k2>=<v2>,...]` with parameter keys in byte-lexicographic order and values formatted using their YAML scalar form. Parameter substitution uses Go's `text/template` syntax (delimiters `{{` `}}`); only field-substitution is supported (no conditionals, no loops, no function calls beyond identity). Unknown parameters, unresolvable templates, or matrix-cell name collisions (per SH-005) MUST fail at scenario-load time per SH-004.

Tags: mechanism

### 4.11 Concurrency policy

#### SH-031 — Scenarios run sequentially

The harness MUST execute scenarios sequentially: at most one scenario's full lifecycle (per SH-016a synthetic-project-root creation through SH-015 fixture teardown completion) may be active at any time. "Active" means: between the start of a scenario's fixture-up and the completion of that scenario's teardown sub-step (e), no other scenario MAY have any sub-step running. Pre-fetching scenario N+1's fixture state in parallel with scenario N's teardown is forbidden at v0.1. Reconciliation workflows internal to a single scenario (per PL-005 step 8 dispatching against fixture state) are not "concurrent scenarios" and are unaffected by this requirement. Concurrent multi-scenario execution is tracked at OQ-SH-002; the trigger is a measured suite-wall-clock pain point, not a speculative parallelism win.

Tags: mechanism

### 4.12 Harness CLI surface (new in v0.2)

#### SH-032 — Harness CLI grammar at MVH

The harness binary is `harmonik harness` (a `harmonik` subcommand). The MVH CLI surface is:

| Flag / argument | Type | Default | Purpose |
|---|---|---|---|
| `--cadence <tag>` | enum | (omitted = `all`) | Cadence filter per SH-029 |
| `--scenario <path>` | string (repeatable) | (none) | Run a single scenario file (or repeated for a subset) |
| `--fixture-root <path>` | string | OS-temp directory | SH-016 fixture-root override |
| `--twin-search-path <path>` | string | `<repo-root>/twins/` | SH-009 twin-search-path override |
| `--list` | flag | false | Print discovered scenarios + cadence; no execution |
| `--dry-run` | flag | false | Suite-load + matrix expansion only; no orchestration |
| `--output <format>` | enum (`human` \| `json`) | `human` | `SuiteResult` output format on stdout |
| `--verbose` | flag | false | Operator-facing progress log to stderr |

`SuiteResult` MUST be written to stdout in the chosen format at suite end; harness-internal log messages MUST go to stderr. Exit codes:

| Exit code | Meaning |
|---|---|
| 0 | `SuiteResult.suite_verdict = pass` |
| 1 | `SuiteResult.suite_verdict = fail` (one or more scenarios failed) |
| 2 | Suite-load aborted (per SH-006: any duplicate, parse error, schema error) |
| 3 | Harness-internal error (panic, unrecoverable I/O failure outside scenario boundaries) |
| 130 | Operator interrupt (SIGINT) |

Two concurrent `harmonik harness` invocations against the same operator project are permitted (each creates its own per-suite ephemeral fixture root); they MUST NOT contend for any shared resource since both run against synthetic project roots per SH-016a.

Tags: mechanism

#### SH-033 — Signal handling and graceful shutdown (new in v0.2)

The harness MUST handle SIGINT and SIGTERM by attempting graceful shutdown: cancel the currently-running scenario per SH-026 (treating it as a timeout-equivalent — `verdict=error`, `failure_class=harness-internal-error`, `error_detail` indicating operator interrupt), execute SH-015 teardown, write a partial `SuiteResult` to stdout containing the results of completed scenarios plus the interrupted scenario's error verdict, and exit with code 130 (SIGINT) or 143 (SIGTERM). If a second SIGINT arrives during graceful shutdown the harness MUST exit immediately (exit code 130) without further teardown — operator-driven escalation overrides the cleanup invariant.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

### 4.13 Result emission (new in v0.2)

#### SH-034 — ScenarioResult durability and emission

Every scenario's `ScenarioResult` MUST be written to disk at `<fixture-root>/<scenario-name>/result.json` immediately after the scenario completes (before the next scenario begins). The aggregate `SuiteResult` MUST be written to `<fixture-root>/suite-result.json` AND emitted to stdout per SH-032 at suite completion. `SuiteResult.suite_verdict` is `pass` iff every `result.verdict==pass`; any non-pass result (including `fail`, `timeout`, or `error`) implies `suite_verdict=fail`. If the harness crashes after a scenario's `result.json` write but before the suite-level emission, the per-scenario files allow operator reconstruction; the harness MAY (post-MVH) provide a `harmonik harness reconstruct <fixture-root>` subcommand for this. `ScenarioResult.error_detail`, when present, MUST be a non-empty operator-readable string carrying at minimum the underlying `err.Error()` string and (where applicable) the captured panic stack.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

## 5. Invariants

#### SH-INV-001 — No test-mode branches in production code

For every package imported by the harness from the production tree (daemon, orchestrator, agent-runner, workspace manager, hook system, policy engine, event bus, handler implementations), there MUST be ZERO conditional branches keyed off "is this a test?" / "is this scenario mode?" / "is this a twin?" / "is this a harness invocation?". The harness configures the production stack via two explicit surface mutations: handler-config override (SH-008) and working-directory assignment to a per-scenario synthetic project root (SH-016a). All other behavior is identical to production by construction.

Sensor: a corpus-grep at the harness conformance test layer (§10.2) over the production-imported package set, with the following layered checks:

1. **Token-set grep (case-insensitive):** any of `scenarioMode`, `isTest`, `isTwin`, `harnessMode`, `isFakeRunner`, `useStub`, `cfg.TestMode` matches a fail.
2. **Regex pattern:** `if\s+.*[Tt]est|[Tt]win|[Ss]cenario|[Hh]arness.*Mode` matches a fail.
3. **Suffix-test pattern:** `HasSuffix\([^,]+,\s*"-twin"\)` and `agent_type\s*==\s*"\*?-twin"` match a fail.
4. **Environment-variable pattern:** any read of `HARMONIK_*_MODE` env var name in production code matches a fail.

Excluded from the grep: unit-test packages (per HC-035 in-process fakes carve-out) and the harness's own packages. Periodic review of the token-set is required as a §10.2 obligation; new test-mode tokens discovered in a violator PR are added to the grep on accept.

Tags: mechanism

#### SH-INV-002 — Workspace state is fully reset on teardown

After SH-015 teardown completes, no live process spawned by the scenario (including handler subprocesses, `br` CLI subprocesses spawned by the daemon per [process-lifecycle.md §4.1.PL-006], any twin-spawned grandchild processes orphaned by twin exit) remains, no worktree lease is still held by the scenario's `run_id`, no event-log file descriptor is still open. "No live process" is evaluated by enumerating the daemon process's full descendant tree at teardown completion (using `getppid()`-walk on Linux/Darwin, marker env var `HARMONIK_RUN_ID` per PL-006a for cross-confirmation); zombies (reaped but not yet wait-ed) are tolerated. The harness MUST guarantee these on every terminal path including `fail`, `timeout`, `error`. Sensor: a post-suite assertion at the harness conformance test layer (§10.2), evaluated AFTER the last scenario's teardown completes AND BEFORE the SuiteResult is written, inspects (i) the descendant process tree (no PID with a `HARMONIK_RUN_ID` env var matching any executed scenario's run_id), (ii) the worktree-lease registry (per-fixture-root scan: no `<workspace>/.harmonik/lease.lock` files held by an executed run_id), (iii) open file descriptors (`lsof`-equivalent check that no event-log file under the fixture root remains open). Residual processes / leases / fds fail the suite.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=non-idempotent

#### SH-INV-003 — Harness operates only on twin binaries, never on real-model handlers

Every handler binary launched during scenario execution MUST be a twin per [handler-contract.md §4.8.HC-035]; the harness MUST refuse to launch a real-model handler. The "is a twin" predicate is: a binary is a twin iff its absolute resolved path is under the configured twin-binary search-path prefix per SH-009 (item (b)) OR the scenario's `agent_overrides` declared the binary AND the daemon's HC-043 commit-hash check passes against a registered twin entry. Name-only heuristics (`HasSuffix("-twin")`) are NOT sufficient and MUST NOT be used by the sensor; the predicate is purely path-prefix-based plus HC-043 hash verification. Sensor: a pre-launch check at handler-config resolution (§4.3) verifies the predicate; a scenario whose resolved configuration would launch a binary failing the predicate MUST fail with `verdict=error` and `failure_class=harness-internal-error`. Conformance test for the sensor: a scenario whose `agent_overrides` references `/usr/bin/claude` (or any binary outside the search-path prefix) MUST fail at the pre-launch check, NOT at orchestration time.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### SH-INV-004 — Scenarios produce identical observable verdicts on rerun

Given identical inputs, the harness MUST produce identical values for the SH-027 determinism contract field set on every run. This invariant is the enforcement surface of SH-027; it spans the harness, the daemon, the orchestrator, and the twin binary set, because non-determinism in any of them defeats it. Sensor: a nightly cadence task reruns the `regression`-cadence subset (excluding `nightly`-cadence wall-clock-mode scenarios per SH-027's carve-out) N times where N≥10, and diffs the determinism contract field set across runs. The cadence pinned at sensor execution time MUST be `regression` (the sensor MUST NOT silently fall through to a smaller smoke set). Any divergence across the N runs fails the nightly task.

Tags: mechanism

#### SH-INV-005 — Scenario files are declarative-loadable without code execution

Every scenario file MUST be loadable by a generic YAML parser plus the §6.1 schema validator, with no plugin, no eval, no code-execution YAML tags. Sensor: a corpus lint at suite-load time runs every scenario file through `gopkg.in/yaml.v3` in strict mode with `KnownFields(true)` and a forbidden-tag deny-list (rejecting `!!python/object`, `!eval`, `!!binary` constructors carrying executable, custom-loader directives, anchors that reference unbound aliases); any rejected file is a suite-load failure. The parser is pinned to this implementation at v0.1; future revisions MUST justify a parser change as a foundation amendment per [architecture.md §4.6].

Tags: mechanism

## 6. Schemas and data shapes

### 6.1 Scenario file and result records

```
RECORD ScenarioFile:
    name              : String                                -- repo-wide unique per SH-005; byte-lex order drives execution per SH-007
    description       : String                                -- one-line operator-facing description
    workflow_path     : String | None                         -- DOT workflow file path (repo-relative within synthetic project root); MUTUALLY EXCLUSIVE with workflow_id; exactly one MUST be set
    workflow_id       : String | None                         -- workflow_id per [execution-model.md §4.1]; MUTUALLY EXCLUSIVE with workflow_path
    agent_overrides   : Map<String, AgentOverride>            -- key: agent role declared by the workflow; value: twin-binary selector
    fixture_setup     : FixtureSetup                          -- workspace seed instructions
    expected_events   : List<EventExpectation>                -- event_present / event_absent assertions
    expected_workspace: List<WorkspacePredicate>              -- file/git-ref predicates
    expected_outcome  : OutcomeExpectation | None             -- exit_code (run-level terminal-emission outcome_status) assertion
    timeout_secs      : Integer                               -- per SH-025: positive integer in [1, 7200]
    cadence_tag       : Enum (smoke | regression | nightly)   -- cadence-filter membership per SH-029
    matrix            : Map<String, List<String>> | None      -- parameter expansion per SH-030 (max 1024 cells)
```

`agent_overrides` keys MUST match agent roles declared in the resolved workflow. A scenario referencing an undeclared role is a `scenario-load-failure` (caught at scenario-load when the harness loads the workflow definition for the first time).

`workflow_path` resolves to a DOT file in one of: (i) `<synthetic-project-root>/<workflow_path>` (seeded by `fixture_setup.files` or `fixture_setup.git_seed`); (ii) `<repo-root>/scenarios/_workflows/<workflow_path>` for shared workflows referenced across scenarios. The harness MUST attempt (i) first, then (ii); resolution failure is `scenario-load-failure`.

```
RECORD AgentOverride:
    binary            : String                                -- absolute path or twin-search-path-relative name; subject to SH-009 + HC-043 hash check
    args              : List<String> | None                   -- additional CLI args; MERGE SEMANTICS: appended to the production composition root's default args (no replacement)
```

```
RECORD FixtureSetup:
    git_seed          : List<GitSeedOp> | None                -- optional list of git ops to seed the workspace tree
    files             : Map<String, FileSeed> | None          -- optional path → file-seed map applied to the synthetic project root before orchestration
    skill_search_paths: List<String> | None                   -- additional skill search paths injected into LaunchSpec per [handler-contract.md §6.1 LaunchSpec]
```

```
RECORD GitSeedOp:
    op                : Enum (commit | branch | tag | checkout)
    args              : Map<String, String>                   -- per-op fields; see §6.3
```

```
RECORD FileSeed:
    encoding          : Enum (utf8 | base64)                  -- contents encoding; default utf8
    contents          : String                                -- file contents as utf8 string OR base64-encoded bytes per encoding
    mode              : String | None                         -- octal POSIX file mode (e.g., "0755"); default "0644"
```

```
RECORD EventExpectation:
    kind              : Enum (event_present | event_absent)
    type              : EventType                             -- one of the §8 types per [event-model.md §8]
    payload_match     : Map<String, JSONValue> | None         -- optional dotted-path-keyed field-equals predicates per SH-021; shallow-merge against actual payload
    description       : String                                -- operator-facing label for the assertion
```

```
RECORD WorkspacePredicate:
    kind              : Enum (file_exists | file_contents_equal | file_contents_match | git_ref_at | commit_trailer_present)
    path              : String                                -- repo-relative path within the worktree; absolute and traversal forbidden per SH-022
    expected          : String | None                         -- per-kind interpretation declared at §6.3
    description       : String                                -- operator-facing label
```

```
RECORD OutcomeExpectation:
    outcome_status    : Enum (SUCCESS | FAIL | RETRY | PARTIAL_SUCCESS)  -- matches the run-level terminal-emission event payload's outcome_status per [event-model.md §8.1.8]; semantic value matches [execution-model.md §4.1 EM-005] Outcome.status
    description       : String
```

```
RECORD ScenarioResult:
    scenario_name           : String                           -- ScenarioFile.name (or expanded matrix name per SH-030)
    source_path             : String                           -- repo-relative path to the originating scenario file (operator triage)
    started_at              : Timestamp                        -- RFC 3339 UTC wall-clock at fixture-setup start (millisecond precision)
    completed_at            : Timestamp                        -- RFC 3339 UTC wall-clock at teardown completion (millisecond precision)
    verdict                 : Enum (pass | fail | timeout | error)
    failure_class           : FailureClass | None              -- §8 enum; absent iff verdict=pass
    assertion_results       : List<AssertionResult>            -- one per declared assertion; empty only if scenario terminated before §7.1 step 4 entered (e.g., scenario-load-failure, twin-binary-not-found, fixture-setup-failed)
    event_log_path          : String                           -- fixture-root-relative path to the captured JSONL
    workspace_snapshot_path : String                           -- fixture-root-relative path to the post-teardown worktree (per SH-015a)
    stdout_log_paths        : Map<String, String>              -- role -> fixture-root-relative path of captured stdout per SH-020
    stderr_log_paths        : Map<String, String>              -- role -> fixture-root-relative path of captured stderr per SH-020
    error_detail            : String | None                    -- non-empty operator-readable string when verdict=error or when cleanup-failed appended; format: "<class>: <err.Error()>" plus optional stack on panic
```

```
RECORD AssertionResult:
    assertion_kind    : Enum (event_present | event_absent | workspace_state | exit_code)
    description       : String                                -- copied from declared assertion
    actual_value      : JSONValue                             -- captured observable
    expected_value    : JSONValue                             -- declared expectation
    passed            : Bool
```

```
RECORD SuiteResult:
    suite_id          : UUID                                  -- UUIDv7 generated at suite invocation
    started_at        : Timestamp                             -- RFC 3339 UTC, millisecond precision
    completed_at      : Timestamp                             -- RFC 3339 UTC, millisecond precision
    fixture_root      : String                                -- absolute path to the per-suite ephemeral fixture root per SH-016 (operator-facing)
    cadence_filter    : Enum (smoke | regression | nightly | all)
    results           : List<ScenarioResult>
    suite_verdict     : Enum (pass | fail)                    -- pass iff every result.verdict==pass; any non-pass result implies fail
```

`FailureClass` is the enum declared in §8. `JSONValue` is the JSON value type (one of: null, boolean, number, string, array of JSONValue, object with string keys mapping to JSONValue) — the same value type produced by Go's `encoding/json` `interface{}` decode and consumed by `EventExpectation.payload_match`, `AssertionResult.actual_value`, `AssertionResult.expected_value`.

### 6.2 Co-owned event surface

This spec emits no cross-bus events of its own. The harness is a consumer (observational replay reader) of every event type declared in [event-model.md §8] and produces only its own `ScenarioResult` / `SuiteResult` records on the harness CLI. No bus event types are added to the §8 taxonomy by this spec.

### 6.3 Per-kind interpretation tables and schema evolution

#### `WorkspacePredicate.expected` interpretation

| `kind` | `expected` interpretation |
|---|---|
| `file_exists` | `expected` MUST be absent (`None`); presence of the file at `path` is the predicate. |
| `file_contents_equal` | `expected` is the literal byte-equal contents of the file at `path` (UTF-8 string; for binary use `file_contents_match` against a base64-decode regex). |
| `file_contents_match` | `expected` is a Go `regexp` (RE2) pattern matched against the file contents at `path` (multi-line mode off by default). |
| `git_ref_at` | `expected` is the target the ref at `path` (interpreted as `refs/...` per `git`'s rules) MUST resolve to. Form: a 40-char hex SHA-1 (full commit hash) OR another ref name (resolved transitively); short-SHA forms are forbidden for portability. |
| `commit_trailer_present` | `expected` is the trailer key (e.g., `Harmonik-Run-ID`); the predicate matches if the HEAD commit at `path` (interpreted as a ref name) has at least one trailer with that key. Trailer values are NOT matched at v0.1; key-only matching is the contract. |

#### `GitSeedOp.args` interpretation

| `op` | required `args` keys | meaning |
|---|---|---|
| `commit` | `message`, optional `parent`, optional `ref` (default: `HEAD`) | Create an empty or `files`-applied commit on the given branch. |
| `branch` | `name`, optional `from` (default: `HEAD`) | Create a branch pointing at `from`. |
| `tag` | `name`, optional `target` (default: `HEAD`) | Create a lightweight tag. |
| `checkout` | `ref` | Move HEAD to `ref`. |

#### Cadence supersets

(See SH-029 table for the operator-facing form.)

#### Schema evolution

`ScenarioFile.schema_version` is omitted at v0.1 — every scenario file is an implicit `version: 0.1.0` per the spec's front matter. Once the harness is mature enough that scenario files are exchanged across harmonik releases, a `schema_version` field will be added with the N-1 readable contract per [operator-nfr.md §4.5]; the v0.1 harness reading a future-dated `schema_version` MUST refuse the file with `scenario-load-failure` (forward-incompat: silent coercion is a defect). Tracked at OQ-SH-007.

### 6.4 Twin-extension wire messages

The canonical twin binary (`cmd/harmonik-twin-claude`) emits two additional progress-stream message types beyond the core HC-007 catalog. These are **additive**, **optional**, and **twin-emitted only** — the daemon treats unknown type strings as forward-compatible no-ops per [handler-contract.md §6.4]; real-model handlers do NOT emit them. Implemented by bead **hk-e66ht** (settings reader + Stop-hook caller).

Both messages are NDJSON-framed (HC-007a: one JSON object per line, terminated by 0x0A, max 1 MiB per line). They appear on the handler-to-daemon progress stream alongside the HC-007 core messages.

#### `twin_settings_loaded`

Emitted **once at startup** after the twin attempts to read `<worktree-path>/.claude/settings.json`. Provides observability into whether the settings file was found and what Stop-hook configuration it contained. Emitted unconditionally (even when the file is absent or unreadable — field values reflect the result of the attempt).

| Field | Type | Description |
|---|---|---|
| `type` | `string` | Always `"twin_settings_loaded"`. |
| `permissions_present` | `bool` | `true` when `dangerouslyAllowedPermissions` key is present in the parsed settings JSON; `false` otherwise (file absent, parse error, or key not found). |
| `stop_hook_present` | `bool` | `true` when at least one Stop hook command was found in `hooks[].Stop[].command`; `false` otherwise. |
| `stop_hook_command` | `string` | First Stop hook command found, truncated to 200 characters. Empty string when `stop_hook_present` is `false`. The 200-char ceiling is a log-readability guard; full command is available in the settings file on disk. |

Assertion scenarios MAY use `event_present` with `type: twin_settings_loaded` and payload predicates to verify that the twin read a specific settings configuration during a run.

#### `twin_hook_called`

Emitted **after each hook execution** triggered by a script cue. At hk-e66ht scope, `hook_type` is always `"Stop"` (the only hook type the twin executes); the field is declared as `string` rather than enum for forward extensibility to future hook types (`PreToolUse`, `PostToolUse`, etc.).

A non-zero `exit_code` does NOT cause the twin to exit — the daemon-side outcome handler decides what to do with the code; the twin continues its scripted execution sequence.

| Field | Type | Description |
|---|---|---|
| `type` | `string` | Always `"twin_hook_called"`. |
| `hook_type` | `string` | Hook type executed. Currently always `"Stop"`. Extensible for future Claude Code hook types. |
| `exit_code` | `int` | OS exit code returned by the hook subprocess. `0` = success; non-zero = hook indicated failure. |
| `duration_ms` | `int` | Wall-clock duration of the hook execution in milliseconds, measured from subprocess fork to wait completion. |

Assertion scenarios MAY use `event_present` with `type: twin_hook_called` and `payload_match` predicates to verify that a Stop hook was called and received an expected exit code during a run.

## 7. Protocols and state machines

### 7.1 Scenario-execution lifecycle

The harness executes every scenario through a fixed five-step lifecycle. Failure transitions are explicit; every transition emits an entry into the `ScenarioResult` record.

```
FUNCTION run_scenario(scenario):
    -- step 1: load (occurs in the suite-load phase per SH-006; included here for the per-scenario contract)
    -- (suite-load already validated; this branch is a defensive guard)
    IF scenario_file_invalid(scenario):
        RETURN ScenarioResult(verdict=error, failure_class=scenario-load-failure)

    -- step 2: fixture-up (per SH-016a, creates the synthetic project root + per-scenario daemon)
    IF twin_binaries_unresolved(scenario.agent_overrides):
        RETURN ScenarioResult(verdict=error, failure_class=twin-binary-not-found)
    err = fixture_setup(scenario)
    IF err != nil:
        partial_rollback_teardown(scenario)        -- per SH-012; best-effort, classification stays fixture-setup-failed
        RETURN ScenarioResult(verdict=error, failure_class=fixture-setup-failed)

    -- step 3: orchestrate (deadline measured against monotonic clock per SH-025)
    deadline = monotonic_now() + scenario.timeout_secs
    (orch_err, deadline_exceeded) = orchestration_drive(scenario, deadline)
    -- order-of-checks: deadline_exceeded is checked FIRST so a coincident orch_err on a timeout
    -- routes to scenario-timeout (the cause is the timeout) rather than orchestration-internal-error.
    IF deadline_exceeded:
        cancel_orchestration_via_daemon_stop()     -- per SH-026; bounds N×HC-018 + ON-029 drain
        partial_log = read_event_log_best_effort() -- per SH-023, evaluate assertions on partial log
        results = evaluate_all_assertions(partial_log, workspace_snapshot, partial=true)
        return_after_teardown(verdict=timeout, failure_class=scenario-timeout, assertion_results=results)
    IF orch_err != nil AND not_classified_as_assertion_outcome(orch_err):
        return_after_teardown(verdict=error, failure_class=orchestration-internal-error)

    -- step 4: assert (full-eval on non-timeout)
    log = read_event_log()
    IF log_unreadable_per_SH024(log):
        return_after_teardown(verdict=error, failure_class=harness-internal-error)
    results = evaluate_all_assertions(log, workspace_snapshot)
    IF any(not r.passed for r in results):
        return_after_teardown(verdict=fail, failure_class=assertion-failed, assertion_results=results)

    -- step 5: fixture-down + verdict (per §8.0 precedence table)
    err = fixture_teardown(scenario)               -- idempotent: prior return_after_teardown already ran teardown
    IF err != nil:
        IF verdict_already_set: keep prior verdict; append cleanup-failed to error_detail
        ELSE:                   verdict=error, failure_class=cleanup-failed
    RETURN ScenarioResult(verdict=pass, assertion_results=results, ...)
```

The `return_after_teardown` helper invokes fixture teardown per SH-015 before returning. Teardown is idempotent (per SH-015's run-to-completion contract); a second teardown invocation in step 5 after `return_after_teardown` already ran one is a no-op. Verdict retention follows the §8.0 precedence table — teardown failure NEVER overwrites a prior `fail`, `timeout`, or `pass` verdict; it appends to `error_detail` only.

> INFORMATIVE: Every branch above maps to a §8 failure class. The §8 enum is exhaustive over the lifecycle's failure transitions; this property is verified by the §10.2 conformance obligation for SH-001..SH-031.

## 8. Error and failure taxonomy

The harness defines the following failure classes. Every class names: detection rule, default response, escalation path, and (where applicable) the [execution-model.md §8] failure-class analog. Adding a new failure class is a foundation amendment per [architecture.md §4.6]; orphan harness errors that do not fit a current class are recorded under `harness-internal-error` with `error_detail` carrying detail until the pattern justifies a new class.

### 8.0 Failure-class precedence (new in v0.2)

When two or more failure classes co-occur on a single scenario, the highest-precedence class determines the recorded `failure_class`; lower-precedence classes are appended to `error_detail`. Precedence (highest first):

1. `harness-internal-error` — the harness itself is broken; cannot trust any other classification.
2. `orchestration-internal-error` — the daemon stack is in an unknown state; supersedes scenario-attributable failures.
3. `scenario-load-failure` — pre-orchestration, never coexists with later classes.
4. `twin-binary-not-found` — pre-orchestration, never coexists with later classes.
5. `fixture-setup-failed` — pre-orchestration, supersedes cleanup of partial fixture.
6. `scenario-timeout` — supersedes assertion outcomes (a timeout means assertions are evaluated on partial data).
7. `assertion-failed` — observational; the run completed cleanly.
8. `cleanup-failed` — never overwrites a prior verdict (`pass`, `fail`, `timeout`); is appended to `error_detail` only.
9. (no failure — `verdict=pass`).

The §7.1 lifecycle pseudocode and §8.8 default response apply this table consistently. SH-015's "downgrade to error only if no prior failure class has higher precedence" wording resolves against this table: `cleanup-failed` (8) is lower precedence than every prior verdict-bearing class, so it never causes a downgrade.

### 8.1 `scenario-load-failure`

- **Detection.** YAML parse failure, §6.1 schema check failure, unknown workflow ref, unknown cadence tag, parameter-expansion failure, duplicate scenario name.
- **Default response.** Record `ScenarioResult(verdict=error, failure_class=scenario-load-failure)`; do not execute the scenario.
- **Escalation.** Suite-load aborts when ≥1 scenario fails to load; operator review.
- **EM analog.** None — pre-orchestration; never reaches the daemon.

### 8.2 `twin-binary-not-found`

- **Detection.** A scenario's `agent_overrides` references a twin binary that does not exist (absolute path missing, name unresolvable).
- **Default response.** `ScenarioResult(verdict=error, failure_class=twin-binary-not-found)`; do not execute orchestration.
- **Escalation.** Operator review (twin-binary build / install issue).
- **EM analog.** Conceptually adjacent to [execution-model.md §8.2 `structural`] (a different plan is required), but classified at the harness layer because the daemon never sees it.

### 8.3 `fixture-setup-failed`

- **Detection.** Fixture-setup phase (SH-012) returns an error: workspace creation failed per [workspace-model.md §4.1.WM-003], git seed ops failed, file writes failed, fixture-root unreachable.
- **Default response.** Run fixture teardown best-effort; emit `ScenarioResult(verdict=error, failure_class=fixture-setup-failed)`.
- **Escalation.** Operator review (filesystem / git environment problem).
- **EM analog.** Harness-local; never reaches the daemon.

### 8.4 `orchestration-internal-error`

- **Detection.** The orchestration drive returns an error from daemon startup, dispatch, or shutdown that is NOT classifiable as a scenario-attributable failure (assertion failure, timeout, twin scripted failure routed through `agent_failed`).
- **Default response.** Capture the error verbatim in `error_detail`; emit `ScenarioResult(verdict=error, failure_class=orchestration-internal-error)`.
- **Escalation.** Automatic — this is a harness or daemon defect; review escalation is the harness's job.
- **EM analog.** Adjacent to [execution-model.md §8.2 `structural`] when the underlying error is `ErrStructural`; but the routing layer here is the harness, not the orchestrator's cascade.

### 8.5 `assertion-failed`

- **Detection.** ≥1 declared assertion in the scenario evaluates to `passed=false` per SH-023.
- **Default response.** `ScenarioResult(verdict=fail, failure_class=assertion-failed)`; full `assertion_results` list populated.
- **Escalation.** Operator review (production regression OR scenario over-specification).
- **EM analog.** None — assertions are observational; the run itself may have completed cleanly.

### 8.6 `scenario-timeout`

- **Detection.** Orchestration drive does not reach a terminal state within `timeout_secs` wall-clock seconds per SH-026.
- **Default response.** Cancel orchestration via the daemon's normal cancel surface; run fixture teardown; emit `ScenarioResult(verdict=timeout, failure_class=scenario-timeout)`.
- **Escalation.** Operator review (test bug, twin script bug, orchestrator hang).
- **EM analog.** Adjacent to [execution-model.md §8.4 `canceled`] — the cancel path uses the same surface; but the harness-side classification is `scenario-timeout` because the cancel was harness-initiated.

### 8.7 `harness-internal-error`

- **Detection (closed list).** (i) event-log unreadable per SH-024 (mid-file corruption, missing file, permission error, disk-full, observed `bus_overflow` event); (ii) real-model handler attempted launch per SH-INV-003; (iii) outbound non-loopback network call attempted per SH-028; (iv) harness panic / Go runtime error inside the harness binary; (v) operator interrupt (SIGINT/SIGTERM) per SH-033; (vi) failure to start the per-scenario daemon (e.g., binary not found, fork failure) per SH-017's daemon-process-death detector. Any failure not on this closed list and not classified by §8.1–§8.6 or §8.8 MUST be recorded under §8.7 (vi-fallback) until the pattern is added explicitly to the list above by foundation amendment.
- **Default response.** Best-effort fixture teardown; emit `ScenarioResult(verdict=error, failure_class=harness-internal-error)` with `error_detail` populated (per SH-034).
- **Escalation.** Automatic — the harness is broken, not the production code.
- **EM analog.** None — entirely harness-local.

### 8.8 `cleanup-failed`

- **Detection.** Fixture teardown (SH-015) sub-step fails: workspace removal blocked, lease release failed, child process zombie remains, daemon-stop RPC error, event-log close error.
- **Default response.** Per §8.0 precedence: if a prior verdict (`pass` / `fail` / `timeout`) is already set, keep it and append `cleanup-failed: <error>` to `error_detail`. If no prior verdict is set (rare — implies the scenario reached teardown directly without verdict assignment), emit `ScenarioResult(verdict=error, failure_class=cleanup-failed)`.
- **Escalation.** Operator review (often a pidfile or worktree-lease bug surfacing through the harness rather than production).
- **EM analog.** None.

## 9. Cross-references

### 9.1 Depends on

- **[architecture.md §4.1]** — four-axis classification; every requirement above carries `Tags:` and (where applicable) `Axes:` per the rules declared there.
- **[architecture.md §4.9 AR-INV-007]** — centralized-controller principle; the harness invokes the production daemon's composition root and does NOT instantiate orchestration logic outside it (SH-017, SH-018).
- **[architecture.md §4.6]** — amendment protocol; adding a §8 failure class or changing the §10.1 conformance scenario set is a foundation amendment.
- **[handler-contract.md §4.1.HC-003]** — handler selection is config-level; SH-008's twin-substitution mechanism MUST go through this surface.
- **[handler-contract.md §4.4.HC-018]** — bounded cancellation; SH-026 timeout cancellation uses this contract per-handler.
- **[handler-contract.md §4.6.HC-026a]** — heartbeat scenario-mode carve-out; the harness consumes the carve-out to produce byte-reproducible event streams (with a bound per SH-027).
- **[handler-contract.md §4.8 HC-035..HC-040]** — twin parity surface; SH-011 declares the harness consumes this surface and does not extend or bypass it.
- **[handler-contract.md §4.10.HC-042, HC-043, HC-045]** — handler-binary launch rules and commit-hash check; SH-009 mirrors them for twin-search-path discovery and applies HC-043 unchanged.
- **[handler-contract.md §5.HC-INV-002]** — twins indistinguishable from real handlers; SH-INV-001 is the harness-side enforcement of this principle.
- **[handler-contract.md §5 HC-INV-006]** — exactly-one terminal event per session; the §10.1 `regression/twin-failure-classification.yaml` conformance scenario is the harness-visible enforcement surface.
- **[event-model.md §4.5.EV-021]** — observational replay rules; SH-020 declares assertion evaluation as observational replay.
- **[event-model.md §6.1 Event]** — envelope shape consumed by SH-020 / SH-022; the harness reads the envelope without redefining it.
- **[event-model.md §6.2]** — JSONL torn-tail, post-fsync-tail, and read-recovery rules; SH-024 cites them for the event-log capture failure class.
- **[event-model.md §8]** — event taxonomy; every `EventExpectation.type` value resolves into the §8 enum.
- **[event-model.md §8.1.8]** — `run_completed` / `run_failed` payload's `outcome_status` field; `OutcomeExpectation` matches against this value.
- **[workspace-model.md §4.1.WM-001]** — workspace primitive consumed by SH-012 fixture setup.
- **[workspace-model.md §4.2 Branching model]** — branching contract that fixture-setup-created workspaces MUST honor.
- **[workspace-model.md §4.3.WM-010, WM-013b]** — lease-by-run rules and lease release on terminal transitions; SH-013 / SH-015 cite them.
- **[workspace-model.md §4.5.WM-019, WM-021]** — squash-merge contract and `workspace_merge_status` emission; the §10.1 `smoke/checkpoint-and-merge.yaml` conformance scenario verifies these.
- **[execution-model.md §4.1 EM-005]** — `Outcome.status` enum (carried as `outcome_status` per [event-model.md §6.1] and §8.1.8) consumed by `OutcomeExpectation`.
- **[execution-model.md §8]** — failure classes the harness maps its own classes against in §8.
- **[operator-nfr.md §4.5]** — N-1 readability compat-window; cited in §6.3 schema-evolution prose for the future `schema_version` field.
- **[operator-nfr.md §4.7 ON-029]** — drain-timeout escalation to SIGKILL; SH-026 cites for cancel-and-teardown bound.
- **[process-lifecycle.md §4.1.PL-001]** — one-daemon-per-project; SH-016a satisfies by treating each scenario as its own ephemeral project.
- **[process-lifecycle.md §4.1.PL-006, PL-006a]** — orphan sweep + `HARMONIK_RUN_ID` env-var marker; SH-INV-002 sensor uses these.
- **[process-lifecycle.md §4.2.PL-003a]** — daemon socket RPC inventory; SH-026 invokes `stop` for cancel.
- **[process-lifecycle.md §4.2.PL-005]** — daemon startup sequence; SH-017 declares the harness uses the same entry-point.

### 9.2 Reverse dependencies

> INFORMATIVE: Reverse dependencies are computed on demand from the foundation corpus. Populated at finalize.

### 9.3 Co-references (read-only consumption)

- **[docs/concepts/digital-twins.md]** — concept seed for the twin pattern; informative context for SH-009 / SH-011.
- **[docs/goals/end-to-end-testability.md]** — G07 goal that motivates this spec; informative.
- **[docs/goals/bootstrapping-self-building.md]** — G06 self-build cycle whose regression net this spec provides; informative.
- **[docs/bootstrap.md §5 step 8]** — the MVH sequencing entry that names S07; informative.
- **[docs/subsystems/scenario-harness.md]** — the seed-status subsystem doc; this spec supersedes it normatively.

## 10. Conformance

### 10.1 Conformance profiles

**Core MVH.** An implementation conforming to Core MVH MUST pass every requirement SH-001 through SH-034 and every invariant SH-INV-001 through SH-INV-005. No requirement is deferred at MVH.

**Conformance scenario set.** A harmonik build claims S07 conformance only if its harness can load and execute the following representative scenarios (located at `scenarios/<path>` per SH-002) and produce `verdict=pass` for each:

- **`scenarios/smoke/twin-launch-and-ready.yaml`** — a minimal one-node workflow that launches a single twin, observes `agent_ready` per [handler-contract.md §4.9.HC-039], emits `agent_completed`, and reaches a clean terminal state. Asserts `event_present(agent_ready)`, `event_present(agent_completed)`, `exit_code(SUCCESS)` (the `OutcomeExpectation.outcome_status` form).
- **`scenarios/smoke/checkpoint-and-merge.yaml`** — a workflow that drives one node to terminal success, emits a checkpoint per [execution-model.md §4.4.EM-016], and merges the task branch per [workspace-model.md §4.5.WM-019] (squash-merge contract) emitting `workspace_merge_status` per [workspace-model.md §4.5.WM-021]. Asserts `event_present(checkpoint_written)`, `event_present(workspace_merge_status{status=pending})`, `event_present(workspace_merge_status{status=merged})`, `workspace_state(commit_trailer_present, Harmonik-Run-ID)`.
- **`scenarios/regression/twin-failure-classification.yaml`** — a scenario where the twin is scripted to crash without `outcome_emitted`, exercising the `ErrStructural` classification per [handler-contract.md §4.6.HC-024] and the exactly-one-terminal-event invariant of [handler-contract.md §5 HC-INV-006]. Asserts `event_present(agent_failed{error_category=ErrStructural})`, `event_absent(outcome_emitted)`, `exit_code(FAIL)`.

These three scenarios are the v0.1 conformance floor. Adding to the set is a foundation amendment per [architecture.md §4.6]; removing requires the same.

**Post-MVH extensions.** Composite assertion predicates (OQ-SH-006), concurrent scenario execution (OQ-SH-002), Go-as-code scenarios (OQ-SH-001), schema versioning of scenario files (OQ-SH-007), twin-conformance drift detection (OQ-SH-008) are additive extensions; none is required to claim Core MVH conformance.

### 10.2 Test-surface obligations

During bootstrap (before `testing.md` exists) test obligations are named in prose. Each requirement's test obligation:

- **SH-001 — SH-007 (scenario format + suite load).** Schema-validation unit tests covering the §6.1 record shape; corpus tests on the conformance scenario set verifying every file parses; negative tests covering each malformed-scenario class (parse error, schema mismatch, unknown workflow, unknown cadence, duplicate name, parameter-expansion failure, name-regex violation, `.yml` extension, BOM-encoded file, oversize file, oversize matrix). Reviewers MUST reject any production-package PR introducing a forbidden test-mode branch per SH-018; the corpus-grep sensor of SH-INV-001 is the mechanical check.
- **SH-008 — SH-011 (twin substitution).** Integration tests verifying that handler-config resolution applies `agent_overrides` correctly; corpus grep at the harness conformance test layer per the SH-INV-001 token-set / regex / suffix / env-var checks; an HC-043 hash-mismatch injection test verifies SH-009's hash-check binding (a stale twin fails as `twin-binary-not-found`); SH-INV-003 conformance: a scenario whose `agent_overrides` references `/usr/bin/claude` (or any binary outside the search-path prefix) MUST fail at the pre-launch check with `harness-internal-error`; the conformance scenario set's `smoke/twin-launch-and-ready.yaml` covers the live path.
- **SH-012 — SH-016a (fixture lifecycle).** Crash-injection tests killing the harness between fixture-up and orchestration-start, verifying that the per-suite ephemeral fixture root remains for operator inspection AND the operator's `.harmonik/` tree is not mutated; partial-fixture-rollback tests (forcing failure between sub-steps (a)/(b)/(c) of SH-012) verify classification stays `fixture-setup-failed`; SH-016a synthetic-project-root tests verify the daemon's `.harmonik/` writes land inside the synthetic root and that one-daemon-per-project (PL-001) is satisfied; OS-process-tree assertion at suite end (after the last teardown but before `SuiteResult` emission) verifies SH-INV-002.
- **SH-017 — SH-019 (orchestration drive).** Cross-package tests verifying that the harness's daemon entry-point and the production `daemon` mode entry-point are the SAME composition-root function (Go function-identity check, NOT byte-for-byte); daemon-process-death injection tests verify SH-019 routes to `orchestration-internal-error`; degraded-state injection (forcing PL-010 mid-scenario) verifies the same routing.
- **SH-020 — SH-024 (assertion vocabulary).** Unit tests over each of the four assertion kinds (`event_present`, `event_absent`, `workspace_state`, `exit_code`) covering positive AND negative cases, including dotted-path payload-match, shallow-merge semantics, NFC-normalized string equality, and per-kind `WorkspacePredicate.expected` semantics from §6.3; torn-tail tolerance test verifies post-fsync torn tails are accepted; mid-file-corruption injection test verifies SH-024 escalates to `harness-internal-error`; `bus_overflow` injection test verifies the same.
- **SH-025 — SH-026 (timeout).** Scenario timeout tests: a deliberately-hanging twin scenario verifies monotonic-clock cancellation, fixture teardown completion within `(N × HC-018) + ON-029` bounds, partial assertion evaluation per SH-023, and `verdict=timeout` emission; signal-during-timeout-handling test verifies SH-033's second-SIGINT escalation.
- **SH-027 — SH-028 (repeatability + offline).** Determinism tests: rerun the `regression`-cadence subset N≥10 times and diff the SH-027 determinism contract field set; SH-INV-004 sensor pinned at `regression` cadence; network-sandbox tests verify SH-028 (a probe scenario that attempts an outbound non-loopback call MUST fail with `harness-internal-error`); on Linux the network-namespace mechanism is verified by inspecting that no non-loopback interface is reachable from the harness process tree.
- **SH-029 — SH-030 (cadence + matrix).** Filter unit tests for `--cadence` flag covering the supersets per SH-029's table; empty-result handling (filter resolves to zero scenarios → `suite_verdict=pass`, exit 0); matrix-expansion unit tests verify the cartesian-product naming, byte-lex parameter ordering, and Go `text/template` substitution; matrix-cell-name-collision test verifies suite-load failure.
- **SH-031 (sequential).** Suite-execution tests verify at most one scenario's lifecycle is active at any time; "active" boundary test verifies that scenario N+1's fixture-up does NOT begin before scenario N's teardown sub-step (e) completes.
- **SH-032 — SH-034 (CLI + result emission).** CLI-grammar unit tests cover each flag and exit-code mapping; signal-handling tests verify SIGINT/SIGTERM produce a partial `SuiteResult` and the documented exit codes; per-scenario `result.json` durability tests verify that crash-after-result-write preserves the per-scenario file.

Migration to `[testing.md §<layer>]` cross-references occurs within one revision cycle once testing.md lands; this obligation is tracked in OQ-SH-009.

### 10.3 Excluded conformance claims

- This spec does NOT grant conformance over: the twin binaries themselves (owned by [handler-contract.md §4.8] for parity, and by per-handler specs for behavior); orchestrator dispatch correctness (owned by [execution-model.md]); workspace lease correctness (owned by [workspace-model.md]); event JSONL correctness (owned by [event-model.md]).
- This spec does NOT guarantee any performance bound on suite wall-clock duration; cadence design (smoke / regression / nightly) is the operator's mechanism for managing duration.
- This spec does NOT cover the conformance suite that uses real model calls; that suite is post-MVH and is not S07.

## 11. Open questions

#### OQ-SH-001 — Hybrid YAML + Go-as-code scenario format

Question: At v0.1 the scenario format is YAML-only. Should a Go-as-code scenario format be added later for scenarios that need programmatic control flow (e.g., parameter-driven branching beyond the matrix expansion of SH-030)? If yes, how do declarative review properties (SH-INV-005) compose with code-driven scenarios?
Owner: foundation-author
Blocks: SH-001, SH-003 (the YAML-only floor) — additive, not blocking the v0.1 contract
Default-if-unresolved: YAML-only at v0.1; revisit when ≥3 in-tree scenarios cannot be expressed cleanly under the matrix-expansion mechanism.

#### OQ-SH-002 — Concurrent scenario execution

Question: At MVH the harness runs scenarios sequentially (SH-031). When does parallel execution become worthwhile, and what isolation guarantees does it require (per-scenario fixture root and per-scenario daemon are the obvious answers; per-scenario daemon collides with [process-lifecycle.md §4.1.PL-001] one-daemon-per-project unless we treat each scenario as its own ephemeral project)?
Owner: foundation-author
Blocks: SH-031 widening
Default-if-unresolved: sequential at MVH; revisit when measured suite wall-clock is operator-painful.

#### OQ-SH-003 — Twin-binary distribution model

Question: Twin binaries are referenced by absolute path or by the configured search-path prefix per SH-009. Should they live in-tree alongside the harmonik repo (same release cadence as the daemon, twin-parity easier to enforce) or be vendored separately (twins as their own release stream, harmonik depends on a pinned version)?
Owner: foundation-author
Blocks: nothing in the v0.1 contract; affects build / release tooling design downstream
Default-if-unresolved: in-tree alongside the harmonik repo for MVH; same release cadence as the daemon.

#### OQ-SH-004 — Auto-generated scenarios from improvement-loop signals

Question: Per [docs/subsystems/scenario-harness.md] open question 5: should the improvement loop (S09, post-MVH) auto-generate scenarios when it observes new failure patterns in production, closing the loop "production failure → regression scenario → never again"? If yes, what is the contract: does S09 emit a `ScenarioFile` directly, or a higher-level intent that a separate authoring agent translates?
Owner: improvement-loop spec author (post-MVH)
Blocks: nothing at MVH (no S09 yet)
Default-if-unresolved: not in v0.1; defer until S09 lands.

#### OQ-SH-005 — Twin-introduced non-determinism

Status note (v0.2): the v0.1 default-if-unresolved was promoted to normative text in SH-027's "scope carve-out" paragraph. This OQ remains open as a watching brief on whether wall-clock-mode scenarios outgrow the nightly-cadence carve-out (e.g., whether reviewers find the carve-out leaks non-determinism into observable verdicts despite the cadence isolation).
Question: Does the SH-027 wall-clock-mode carve-out remain sufficient as the twin set grows post-MVH, or do we need a richer determinism contract (per-twin-mode determinism profiles)?
Owner: foundation-author
Blocks: SH-027 widening
Default-if-unresolved: keep the carve-out as written in SH-027; revisit if ≥3 wall-clock-mode scenarios demonstrate practical determinism issues.

#### OQ-SH-006 — Composite and ordering assertion predicates

Question: At v0.1 the harness supports four assertion kinds (SH-021). Reviewers and scenario authors may need composite predicates (`A AND B`, `A OR (B AND C)`) and cross-event ordering predicates (`A precedes B`, `A within N seconds of B`). When and how do these enter the surface? Is the right shape a richer per-assertion DSL or a separate predicate-graph YAML structure?
Owner: foundation-author
Blocks: SH-021 widening
Default-if-unresolved: four kinds at v0.1; widen post-MVH when ≥5 scenarios across the conformance set demonstrate the need.

#### OQ-SH-007 — Scenario-file schema versioning

Question: At v0.1 there is no `schema_version` field on `ScenarioFile`; the spec's front matter version is the implicit version. When the scenario format gains a new field that older harness versions cannot ignore, the N-1 readable contract per [operator-nfr.md §4.5] kicks in. What is the migration path from v0.1's no-version file to v0.2's versioned file?
Owner: foundation-author
Blocks: nothing at MVH
Default-if-unresolved: a missing `schema_version` is treated as `0.1.0` by future harness versions; a future spec revision adds the field as REQUIRED with a default-on-absence shim for v0.1 files. v0.1/v0.2 harness MUST refuse a `schema_version` that is forward-incompat (per §6.3); silent coercion is a defect.

#### OQ-SH-008 — Twin-conformance drift detection

Question: Per [handler-contract.md §4.8.HC-038], twin-vs-real drift detection is scoped to S07 post-MVH. What is the workflow's shape: a separate cadence (`real-conformance`) that runs the same scenarios against real handlers and diffs the observable surface? A periodic capture of real-handler output stored alongside the twin script as a parity-reference file?
Owner: foundation-author (post-MVH)
Blocks: nothing at MVH (drift detection is post-MVH per HC-038)
Default-if-unresolved: separate post-MVH cadence; reference-file capture is the leading candidate; concrete shape lands in a v0.2 revision.

#### OQ-SH-009 — Migrate test-obligation prose to testing.md references

Question: Section §10.2 currently names test obligations in prose. The template §10.2 expects cross-references to `[testing.md §<layer>]` once testing.md lands.
Owner: foundation-author
Blocks: none (MVH prose obligations are in place)
Default-if-unresolved: Keep prose obligations; migrate within one revision cycle after testing.md is finalized.

#### OQ-SH-010 — Failure-class extension protocol

Question: §8 defines eight failure classes. Adding a class requires a foundation amendment per [architecture.md §4.6]. Is that the right friction level, or should new harness-internal sub-classes be addable by spec revision without amendment (the harness has more local-discretion latitude than the production daemon)?
Owner: foundation-author
Blocks: §8 extensibility
Default-if-unresolved: foundation amendment for new classes at v0.1; revisit if the class list grows past ≈12.

#### OQ-SH-011 — Daemon-side `--project-root` flag (new in v0.2)

Question: SH-016a synthesizes a per-scenario project root by setting the daemon's working directory at process start; the daemon's `.harmonik/`-rooted writes land under the synthetic root by construction (CWD-relative resolution). Does the daemon need a normative `--project-root <path>` flag in [process-lifecycle.md §4.2.PL-005] to make project-root resolution explicit (rather than CWD-coupled), and if so what's the migration path?
Owner: foundation-author (cross-spec coordination with PL)
Blocks: nothing in v0.2 (CWD-based mechanism is sufficient for the harness today)
Default-if-unresolved: keep CWD-based mechanism; surface a coordination-patch finding for PL if a future use-case (e.g., daemon-as-supervisor) requires explicit decoupling.

#### OQ-SH-012 — Per-run cancellation RPC for suite-mode efficiency (new in v0.2)

Question: SH-026 cancels via `daemon stop`, which terminates the per-scenario daemon entirely (acceptable because each scenario uses its own daemon per SH-016a). When suite-mode efficiency becomes operator-painful (OQ-SH-002 trigger), a per-run cancel RPC on the daemon socket would let multiple scenarios share a daemon and cancel one without affecting others. Should PL add such an RPC, what's its signature, and how does it interact with the SH-031 sequential rule?
Owner: foundation-author (cross-spec coordination with PL)
Blocks: post-MVH suite-mode efficiency improvements; not blocking v0.2
Default-if-unresolved: stay with `daemon stop` per scenario at v0.2; coordinate with PL when the parallelism trigger fires.

#### OQ-SH-013 — Network-sandbox mechanism on macOS (new in v0.2)

Question: SH-028 declares a `pf` packet-filter mechanism on macOS as the conformance-lane sandbox floor. Is `pf` the right mechanism, or should the harness use an alternative (e.g., a Go-level dial-time interceptor, an `ip6fw`-equivalent, a containerized sandbox)? Linux's `unshare(CLONE_NEWNET)` is settled.
Owner: foundation-author
Blocks: SH-028 conformance-lane portability
Default-if-unresolved: `pf`-based sandbox on macOS at v0.2; revisit if `pf` proves operationally fragile (e.g., admin-rights requirement breaks CI).

## 12. Revision history

| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-05-05 | 0.1.0 | foundation-author | Initial draft per `docs/subsystems/scenario-harness.md` seed + `docs/bootstrap.md` §5 step 8 framing. Authored as peer to the 10 existing reviewed specs. SH prefix reserved in `specs/_registry.yaml`. v0.1 declares YAML-only scenario format, four-kind assertion vocabulary, three-tag cadence (smoke / regression / nightly), eight-class failure taxonomy, sequential execution, and a three-scenario conformance floor. |
| 2026-05-05 | 0.2.0 | foundation-author | R1 review integration. Inputs: `docs/reviews/2026-05-05-scenario-harness-r1/{implementer,cross-spec-architect,critic}-r1.md`. Three convergent themes resolved: (1) per-scenario synthetic project root (new SH-016a) reconciles SH-014's no-mutation rule with SH-017's production-daemon-entry-point requirement and PL-001's one-daemon-per-project; (2) twin-binary identity (SH-009 + SH-INV-003) now defers to HC-043 commit-hash check unchanged with a path-prefix predicate that excludes name-only heuristics; (3) per-run cancellation surface uses `daemon stop` per-scenario at v0.2 with OQ-SH-012 tracking suite-mode efficiency post-MVH. New normative requirements: SH-015a (workspace-snapshot mechanism), SH-016a (synthetic project root), SH-032 (CLI surface), SH-033 (signal handling), SH-034 (result emission/durability). New §8.0 failure-class precedence table. Cite repairs: WM-007→WM-019/WM-021 in §10.1, HC-043/HC-045 in SH-009, EV §8.1.8 + EM-005 alignment for `OutcomeExpectation`, AR-INV-007 for centralized-controller, ON-029 for drain-timeout, PL-006/PL-006a for SH-INV-002 sensor. `operator-nfr` added to depends-on. Schema fixes: split `workflow_ref` into `workflow_path`/`workflow_id`; new `GitSeedOp` and `FileSeed` records; `JSONValue` defined; per-kind `WorkspacePredicate.expected` table at §6.3; `ScenarioResult.source_path` + relative `event_log_path` / `workspace_snapshot_path` + role-keyed stdout/stderr capture maps. Three new OQs (OQ-SH-011/-012/-013); OQ-SH-005's default-if-unresolved promoted into SH-027 normatively. Status: draft → reviewed. |
| 2026-05-14 | 0.2.2 | foundation-author | Add §6.4 "Twin-extension wire messages" enumerating `twin_settings_loaded` and `twin_hook_called`. These two additive, optional, twin-emitted message types were implemented by hk-e66ht (settings reader + Stop-hook caller) but were not documented in this spec. §6.4 declares field names, types, and semantics matching `cmd/harmonik-twin-claude/wire.go` exactly. Front matter `version` advanced to 0.2.2 and `last-updated` to 2026-05-14. No requirement IDs added (descriptive section only); no existing content changed. Refs: hk-1encw, hk-e66ht. |
| 2026-05-06 | 0.2.1 | foundation-author | Backfill patch closing F-pilot-SH-4 (sh-pilot.md §7) per the discipline v0.10 §3.2 §4.a envelope grandfather carve-out FROZEN decision (SH is the 11th spec, drafted post-AR-053-2026-04-24, and is NOT in the grandfathered set `{EM, HC, CP, WM, PL, RC, EV}`). **Front matter:** added `spec-category: runtime-subsystem` per [architecture.md §4.0 AR-052]; `last-updated` advanced to 2026-05-06. **New §4.a Subsystem envelope with SH-ENV-001** declaring the eight envelope elements of [architecture.md §4.4 AR-013] per [architecture.md §4.0 AR-053] using the reserved `SH-ENV-NNN` requirement-ID range. (a) events produced = none (SH §6.2 framing); (b) events consumed = the [event-model.md §8] taxonomy via observational replay reader per SH-020 with explicit calls-out for `outcome_status` (EV-§8.1.8), `workspace_merge_status` (EV-§8.5.3), agent-state events (EV-§8.3), and `checkpoint_written` (EV-§8.4); (c) types introduced = `ScenarioFile`, `AgentOverride`, `FixtureSetup`, `GitSeedOp`, `FileSeed`, `EventExpectation`, `WorkspacePredicate`, `OutcomeExpectation`, `ScenarioResult`, `AssertionResult`, `SuiteResult`, `FailureClass` (CLI-surface types, not bus payloads); (d) handlers implemented = none (twin substitution drives via HC-003); (e) state owned = per-suite ephemeral fixture root + per-scenario synthetic project root + worktree + captured JSONL + stdout/stderr capture + workspace-snapshot in-place pointer + `ScenarioResult`/`SuiteResult` durable records; (f) control points = none; (g) NFRs inherited = ON-018 (schema compat) + ON-029 (drain timeout); none overridden; (h) boundary classification = 12 operations spanning `suite_load`, `parse_scenario_file`, `resolve_twin_binary`, `fixture_setup`, `synthesize_project_root`, `drive_orchestration`, `capture_event_log`, `evaluate_assertions`, `enforce_scenario_timeout`, `fixture_teardown` (best-effort axis), and the `emit_scenario_result` / `emit_suite_result` durability operations. No other content changed; no requirement IDs renumbered; no IDs retired; no OQs added or closed. Status remains `reviewed`. F-pilot-SH-4 self-flag transitions from "class lane" to "resolved by spec patch." |
