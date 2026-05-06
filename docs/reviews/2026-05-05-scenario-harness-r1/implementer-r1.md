# S07 Scenario Harness v0.1 — Implementer R1 Review

**Date:** 2026-05-05
**Reviewer:** implementer
**Scope:** specs/scenario-harness.md (698 lines, 31 reqs, 5 invariants, 8 error classes, 9 schema records, 10 OQs)

## Summary

The spec gives a Go author a clear conceptual frame: scenarios are YAML, twin substitution is a config-level override, the harness uses the production daemon entry-point, and the assertion vocabulary is small and bounded. The five-step `run_scenario` pseudocode in §7.1 makes the happy and unhappy paths readable. Schema records are mostly typed; the failure taxonomy in §8 is well-named and lines up with the §4 emit sites. As a starting point for a `cmd/scenario-harness/` skeleton plus an `internal/harness/` package set, this is implementable.

The hard problems are concentrated in three places. **First**, the spec asserts both "use the production daemon entry-point as `daemon` mode does" (SH-017) and "MUST NOT mutate the production `.harmonik/` tree" (SH-014) without saying how those reconcile when the daemon's startup sequence (PL-005, PL-002, PL-002a, PL-003) is hard-wired to write `.harmonik/daemon.pid`, bind `.harmonik/daemon.sock`, mint `.harmonik/daemon.instance-id`, sweep `.harmonik/reconciliation-locks/`, and read `.harmonik/daemon.state`. Either the harness creates a per-scenario `.harmonik/` tree under the fixture root (and "production daemon entry-point" means "the same composition-root function with a different project root") or it doesn't, and the answer changes the implementation surface materially. **Second**, twin-binary discovery, search-path configuration, and the "is this binary a twin?" predicate referenced by SH-INV-003 are all gestured at without a normative resolver or twin-marker contract — the discovery rule per SH-009 references HC-042 by analogy but doesn't actually pin the search-path source (CLI flag? env var? config file?). **Third**, several requirements rely on contracts that don't exist in any cited spec: `cancel_orchestration()` for SH-026, the workspace-snapshot capture mechanism, the network-sandbox enforcement of SH-028, and the `assertion_results` round-trip when the run terminates pre-assertion.

Top priorities for the integrator: pick the project-root model for the harness (almost certainly: per-scenario synthetic project root, with the daemon's entire `.harmonik/` tree relocated under the fixture root), pin the twin-binary search-path source and twin-marker predicate, and pin the orchestration cancel API the harness calls.

## Findings

### BLOCKER

#### B1. Production-daemon-entry-point and "no `.harmonik/` mutation" cannot both hold without a project-root indirection.

**SH-014 / SH-016 / SH-017 / PL-001 / PL-002 / PL-003 / PL-004 / PL-005.** SH-017 says the harness "MUST drive scenarios by invoking the same daemon entry-point and the same orchestrator startup sequence as production `daemon` mode per [process-lifecycle.md §4.2.PL-005]." PL-005 step 0 mints a `daemon_instance_id` written to `.harmonik/daemon.instance-id`; step 1 acquires a flock on `.harmonik/daemon.pid`; step 3a binds `.harmonik/daemon.sock`; step 6 calls `br ready` against the project's actual Beads store; step 8 dispatches reconciliation against the project's git history; PL-INV-001 asserts ONE daemon per project. SH-014 then says the harness "MUST NOT mutate the production `.harmonik/` tree" — and SH-016 says all scenario fixture state goes under a per-suite ephemeral fixture root.

These cannot both hold under a literal reading. If "the daemon entry-point" means literally the same function with the operator's project root, then PL-002b's pidfile write (under `<repo>/.harmonik/`) is a mutation of the production `.harmonik/` tree the spec forbids — and worse, if a real daemon is running, SH-017's invocation will exit with code 5 immediately. If "the daemon entry-point" means the same composition-root function with a per-scenario synthetic project root rooted under the fixture root, the spec needs to say so explicitly and pin (a) where the synthetic `.harmonik/` lives, (b) whether the synthetic project is a real git repo or a fake one, (c) whether `br ready` is invoked against a per-scenario Beads SQLite, (d) whether reconciliation runs at all (PL-005 step 8), and (e) what `daemon_instance_id` semantics look like for a daemon that lives for ~5s.

What an implementer would do: invent a "harness mode" project root pointing at `<fixture-root>/<scenario-name>/project/`, copy or symlink the operator's git repo into it, instantiate a fresh `.harmonik/` under that synthetic root, instantiate a per-scenario Beads SQLite, and either skip reconciliation or stub it. None of those four decisions are in the spec; each is load-bearing and changes the test surface meaningfully.

Suggested fix: add a normative requirement (call it SH-016a) that pins "the harness project root" as a per-scenario directory under the fixture root, declare the synthetic project's structure (git repo seed, `.harmonik/` skeleton, Beads SQLite scope), and either declare "reconciliation runs against the empty per-scenario state" (likely a no-op) or carve it out for the harness with a normative reason. Then SH-014's "no `.harmonik/` mutation" can be re-phrased as "MUST NOT mutate the operator's `.harmonik/` tree" (clear) rather than "the production `.harmonik/` tree" (overloaded).

#### B2. SH-INV-003 demands a "twin or not?" predicate that no cited spec defines.

**SH-INV-003 / SH-009 / HC-035 / HC-036.** SH-INV-003 requires "every handler binary launched during scenario execution MUST be a twin"; the sensor verifies "every resolved binary path is a twin (declared via the scenario's `agent_overrides` or matched against the configured twin-binary search-path prefix)." But "is this binary a twin?" is not a property the binary carries — HC-036 says only that the binary *name* is `<real>-twin` (e.g., `claude-twin`) "or a declared alias in configuration." The sensor needs a deterministic predicate.

What an implementer would do: pick a heuristic — name suffix matches `-twin`, OR the binary is found via the search-path prefix, OR the binary advertises `--is-twin` and exits 0 — and ship it. Different harnesses pick different heuristics; conformance scenarios pass for some and fail for others.

Suggested fix: pin the "twin?" predicate normatively. Likely the simplest: a binary is a twin iff (a) its absolute path is under the configured twin-binary search-path prefix per SH-009, OR (b) the scenario file's `agent_overrides` has it referenced AND that override resolved to the search-path prefix. That eliminates the name-based heuristic and ties the predicate to the resolution mechanism.

#### B3. The "orchestration cancel surface" cited by SH-026 is undefined.

**SH-026 / HC-018.** SH-026 says "the harness MUST cancel the orchestration via the daemon's normal cancellation surface (operator-stop equivalent)." HC-018 owns "bounded cancellation" inside a session, but PL doesn't expose a "cancel a run" API on the daemon socket — `claim-next` and `emit-outcome` are agent-facing; `pause`, `resume`, `stop`, `upgrade` are the CLI-facing socket methods (per PL-003a). `stop` halts the daemon, not a single run. There's no `cancel-run` method named anywhere.

What an implementer would do: invent a JSON-RPC method (`cancel-scenario` or `force-terminate-run`), or send SIGTERM to the daemon process and rely on graceful-drain. Either is invasive and changes the harness's relationship with PL-005's startup sequence.

Suggested fix: pin the cancel surface. If "operator-stop equivalent" means `stop` (graceful drain), say so and accept that timeouts will be bounded by the drain timeout, not by `timeout_secs`. If it means a new method, declare the method name, its signature, and either (a) cite where it's defined in PL or (b) add a coordination-request OQ asking PL to add it.

#### B4. Workspace-snapshot capture is normative but unspecified.

**SH-015 / SH-022 / ScenarioResult.workspace_snapshot_path.** SH-015 (d) says teardown "MUST record a `workspace_snapshot_path` pointing at the post-teardown workspace state for debugging"; SH-022 says workspace-state predicates "MUST be evaluated against the post-orchestration workspace tree... as captured at fixture teardown per SH-015." But SH-015 is silent on what "snapshot" means: a copy of the worktree? A `git stash`? A `git rev-parse HEAD` reference? A read-only mount? An archive?

What an implementer would do: pick `cp -r` or `tar` and dump everything under the worktree. This is wrong for git-ref predicates (`git_ref_at`, `commit_trailer_present` per WorkspacePredicate) — snapshotting the working tree doesn't preserve `.git/` state in the form the assertions need.

Suggested fix: pin the snapshot mechanism. Options: (a) leave the worktree in place at the fixture root (the SH-016 "do not auto-delete" rule already implies persistence) and have `workspace_snapshot_path` simply name the worktree path; (b) `git bundle` or `tar`; (c) two-phase snapshot for working-tree files plus git refs. Whichever is chosen, the spec needs to name how `workspace_state` predicates resolve `git_ref_at` against the snapshot — directly against the worktree's `.git/` is the only form that works for arbitrary refs.

#### B5. SH-028 mandates network sandboxing without specifying the enforcement mechanism.

**SH-028.** "Every scenario MUST execute without any outbound network call... a scenario that issues an outbound call MUST fail with `harness-internal-error`." The only "verification mechanism" named is "integration tests at [scenario-harness conformance §10.2] run with network sandboxing enabled," but §10.2 just says the same thing in prose. There's no normative declaration of (a) what sandbox mechanism is used (network namespace? unshare? OS-level firewall? library interposition? in-process detection of net.Dial?), (b) which subprocess tree it covers (does it constrain the daemon? the twin? both?), (c) whether localhost is permitted (the daemon's own socket binding? Beads SQLite-over-network?), or (d) what "outbound" means (loopback?).

What an implementer would do: pick a Linux-only `unshare(CLONE_NEWNET)` and call the macOS path "unsupported," or use a build-time net-stub injection, or grep the daemon's syscall log post-hoc. None of these are portable; CI lanes will differ.

Suggested fix: at minimum, declare which calls are allowed (loopback for the daemon socket and Beads SQLite filesystem), name a mechanism floor (e.g., "Linux: network namespace; macOS: deferred to OQ"), and acknowledge that the requirement applies to harness invocations, not to operator daemon mode.

### MAJOR

#### M1. Workflow-ref resolution is split-brained between scenario file and PL-005 step 5.

**SH-001 / SH-004 / ScenarioFile.workflow_ref / PL-005 step 5.** ScenarioFile has `workflow_ref: String -- DOT workflow file path or workflow_id`. SH-004 says an "unknown workflow" is `scenario-load-failure`. But PL-005 step 5 walks `git for-each-ref refs/heads/run/*` to find in-flight runs, not to find workflow definitions. EM-001 says workflows are DOT files, but the spec doesn't say where the harness loads them from: in-tree `workflows/` directory? per-scenario `fixture_setup`? operator's repo at the time the scenario was authored?

What an implementer would do: invent a `workflows/` dir convention or include the DOT inline in `FixtureSetup.files`. Either choice changes scenario authoring conventions.

Suggested fix: add a sentence to SH-001 or SH-012 pinning the workflow-load source — most likely "DOT workflow files are loaded from the synthetic project root per SH-016a (B1 above), seeded by `FixtureSetup.files` if the scenario provides them, OR resolved against an in-tree `scenarios/_workflows/` directory if the scenario references a shared workflow."

#### M2. SH-021 declares four assertion kinds; AssertionResult enumerates four; ScenarioFile splits them into three lists.

**SH-021 / §6.1 ScenarioFile / §6.1 AssertionResult.** ScenarioFile has `expected_events`, `expected_workspace`, `expected_outcome` — three lists. SH-021 names four assertion kinds; AssertionResult has a single `assertion_kind` enum with four values. This means `event_present` and `event_absent` share the `expected_events` list (distinguished by `EventExpectation.kind`), `workspace_state` is `expected_workspace`, and `exit_code` is `expected_outcome`. That's coherent, but the relationship is implicit. An implementer working from the schema alone (especially one auto-generating Go types) would likely produce one merged list, not three.

What an implementer would do: produce three Go structs and a switch over `AssertionResult.assertion_kind`. Fine, but there's no normative ordering between event vs workspace vs exit-code assertions in the result — does `assertion_results` preserve scenario-file declaration order across the three categories?

Suggested fix: state the ordering rule. Either "events first in declaration order, then workspace, then outcome," or "all assertions in a single combined list whose order matches the union of the three lists in scenario-file order." The latter is more flexible for SH-023's "every declared assertion MUST be evaluated."

#### M3. EventExpectation.payload_match is `Map<String, JSONValue>`; no path/JSONPath grammar.

**§6.1 EventExpectation.** `payload_match: Map<String, JSONValue> | None` — "optional field-equals predicates." For an event like `agent_failed{error_category=ErrStructural, sub_reason=watcher_panic}`, the implementer needs to know whether `payload_match` keys are top-level keys only, or dotted JSONPath (e.g., `error.category`), or something else. Event payloads have nested structure (per event-model §6.1); top-level only would be insufficient.

What an implementer would do: pick top-level only and ship; scenarios that need to match nested fields go unused.

Suggested fix: pin the key grammar. Top-level only is the simplest; `key.dotted.path` is the next step up and matches event-model semantics. Either is fine but it must be pinned.

#### M4. WorkspacePredicate.kind has five values; none have evaluation semantics.

**§6.1 WorkspacePredicate.** Kinds: `file_exists`, `file_contents_equal`, `file_contents_match`, `git_ref_at`, `commit_trailer_present`. The `expected: String | None` field is described as "value or pattern depending on kind." For `file_exists` `expected` is presumably absent; for `file_contents_equal` it's the literal contents; for `file_contents_match` it's a regex (Go regexp? PCRE?); for `git_ref_at` it's a commit SHA or another ref; for `commit_trailer_present` it's the trailer key (e.g., `Harmonik-Run-ID`) — but trailers carry values too, so does "trailer present" mean key-only or key+value?

What an implementer would do: invent a half-grammar per kind and ship.

Suggested fix: add a per-kind row in §6.1 (or a tabular schema in §6.3) declaring the `expected` field's interpretation per kind, including: regex flavor, ref-vs-SHA permitted forms for `git_ref_at`, trailer-key-only vs key+value semantics for `commit_trailer_present`.

#### M5. SH-024 vs SH-021/§7.1 step 4 — what counts as a "log read" failure?

**SH-024.** "If the captured JSONL is unreadable (truncated past the post-fsync tail rule of [event-model.md §6.2], directory missing, permissions error)." event-model §6.2 actually says the post-fsync torn tail is *expected and tolerated*; readers must skip it gracefully. So "truncated past the post-fsync tail rule" is contradictory — the tail rule says torn tails are normal. An implementer reads this and is unsure: is a torn tail a load failure, or not?

What an implementer would do: classify any JSON-decode error past the last well-formed line as `harness-internal-error`, including ordinary post-fsync torn tails. This will produce false-positive `harness-internal-error` verdicts.

Suggested fix: rephrase to "truncated in a manner inconsistent with the torn-tail rule of [event-model.md §6.2]" or "the JSONL file is missing, or a parse error occurs at a position other than the post-fsync tail."

#### M6. `OutcomeExpectation.outcome_status` cites EM-005, but the enum's terminal-emission semantics aren't spelled out.

**§6.1 OutcomeExpectation / SH-021 / EM-005.** `outcome_status` is "one of `{SUCCESS, FAIL, RETRY, PARTIAL_SUCCESS}`" per EM-005. SH-021 (d) says `exit_code` checks "the orchestration drive's terminal `Outcome`." But an orchestration drive's terminal state is a `run_completed` or `run_failed` event (per EM-015b), not a single Outcome — `Outcome` is per-node. Which Outcome does the assertion check? The terminal node's? The first failure? The last node executed?

What an implementer would do: check the Outcome attached to whatever node fired `run_completed` or `run_failed`. If the workflow has a multi-terminal-node structure, the choice is ambiguous.

Suggested fix: state explicitly that `OutcomeExpectation.outcome_status` is matched against the terminal-emission event payload (e.g., `run_completed{status=SUCCESS}`) — i.e., it's the run-level outcome, not a node-level one. If multi-terminal-node ambiguity exists, defer to OQ.

#### M7. CLI surface is undefined.

The spec mentions `--cadence <tag>` (SH-029) but never declares the harness binary's name, exit codes, full flag set, or invocation grammar. An implementer needs: binary name, default behavior (run all? run smoke?), `--scenario <path>` for single-scenario invocation, `--fixture-root <path>` for SH-016 override, `--twin-search-path <path>` for SH-009 search path, exit codes (0/1/2 for pass/fail/error?), output format flag (human / JSON).

What an implementer would do: invent a CLI surface; CI scripts written against version 0.1 break under v0.2 when the surface is normative.

Suggested fix: add a §4.x or §6.x subsection (or an appendix A.x) declaring the CLI grammar at MVH: binary name, flag inventory, exit-code mapping to suite-verdict.

#### M8. Twin-binary search-path source is unspecified.

**SH-009.** "A name resolved against a configured twin-binary search-path prefix delivered to the harness at startup." How is it delivered? CLI flag? Environment variable (e.g., `HARMONIK_TWIN_PATH`)? Config file at `scenarios/_config.yaml`? Per-scenario? Inherited from a global default?

What an implementer would do: pick `--twin-path <dir>` as a CLI flag and ignore env vars. Operators on different machines will be surprised.

Suggested fix: pin the source ordering. "CLI `--twin-search-path` overrides env var `HARMONIK_TWIN_SEARCH_PATH` overrides default `<repo-root>/twins/`." Or whichever model fits; just pin one.

#### M9. SH-013 says workspaces "MUST be disjoint from every other scenario's workspace." Not enforced by anything.

**SH-013.** This is a property; the verification mechanism would be a path-uniqueness check at suite-load. SH-INV-002 sensor checks process tree and lease registry, not workspace disjointness. If two scenarios happen to declare the same fixture-root-relative path (e.g., `fixture_setup.git_seed` ops that produce the same workspace name), the harness has no protection.

What an implementer would do: silently overwrite or fail at the second scenario's `git worktree add`. The first scenario's verdict is potentially corrupted.

Suggested fix: declare a workspace-path naming convention (e.g., `<fixture-root>/<scenario-name>/workspace/` with name uniqueness already guaranteed by SH-005), and a sensor (lint at fixture-up) that fails fast on collision.

#### M10. SH-031's "at most one orchestration active at any time" is vague on registry events / reconciliation.

**SH-031.** The intent is sequential scenario execution. But within a single scenario, the daemon's startup sequence (PL-005) may dispatch reconciliation workflows that run concurrently with the scenario's main run (PL-005 step 8: "auto-resolvable categories resolve inline; investigator-required categories dispatch reconciliation workflows"). Are those "concurrent orchestrations"? The harness probably wants reconciliation to be a no-op for synthetic per-scenario projects (B1), but if not, this requires a stance.

What an implementer would do: ignore the question and claim conformance by counting only top-level scenario runs.

Suggested fix: clarify that "concurrent execution" means "concurrent scenario runs"; reconciliation runs internal to a single scenario do not count.

#### M11. SH-030 matrix expansion produces synthetic names; the lexicographic-order rule of SH-007 doesn't define ordering for matrix variants.

**SH-007 / SH-030.** "Lexicographic order of the `name` field." Matrix expansion produces names like `<scenario>[<param>=<value>...]`. With multiple params, the ordering of params inside the brackets affects the lexicographic order of the synthetic names. Is the param key order alphabetical? Source order from the scenario file? It also affects whether the synthetic name is stable across scenario-file edits.

What an implementer would do: sort params alphabetically by key. Should work; should be pinned.

Suggested fix: state that synthetic matrix names render params in alphabetical order by key, with values formatted using their YAML scalar form.

#### M12. `payload_match` JSONValue type is undefined.

**§6.1.** `JSONValue` is referenced in `EventExpectation.payload_match`, `AssertionResult.actual_value`, and `AssertionResult.expected_value`. The spec template's §6.1 type list is `String, Integer, Bool, UUID, Timestamp, Bytes, List<T>, Map<K, V>`. `JSONValue` is not in the list and is not declared inline.

What an implementer would do: assume `interface{}` / `any` and ship.

Suggested fix: declare `JSONValue` in §6.1 (or refer to event-model's JSONL value type).

#### M13. `fixture_setup.files` is a `Map<String, String>` — encoding unspecified.

**§6.1 FixtureSetup.** `files: Map<String, String> | None -- optional path → contents map applied to worktree before orchestration.` String contents only — no support for binary files. A test that needs to seed an executable shell script or a JPEG cannot.

What an implementer would do: write strings as UTF-8 files. Acceptable for MVH; should be flagged.

Suggested fix: either (a) add a base64-decode escape (`@base64:`-prefixed strings, etc.), or (b) declare binary-file seeding out of scope at v0.1 and add an OQ.

#### M14. `GitSeedOp` is referenced but not defined.

**§6.1 FixtureSetup.** `git_seed: List<GitSeedOp> | None`. `GitSeedOp` is named but no record is given. An implementer doesn't know what ops are supported (commit? branch? tag? merge? `git checkout`?) or what fields they carry.

What an implementer would do: invent a minimal grammar (`commit/branch/tag` with `message`, `parent`, `name`); scenarios authored against this set don't compose with future expansions.

Suggested fix: add a `RECORD GitSeedOp:` block in §6.1 with explicit op kinds and fields, or pin a "we'll support arbitrary `git` commands via a `cmd: String` field" escape.

#### M15. Cleanup-failed precedence rule is described twice with different wording.

**SH-015 / §7.1 / §8.8.** SH-015: "Teardown failure MUST classify as `cleanup-failed` per §8 and the prior verdict MUST be downgraded to `error` only if no prior failure class has higher precedence." §7.1: "if verdict_already_set: keep prior verdict; record cleanup-failed in error_detail." §8.8: "if a prior verdict (pass / fail / timeout) is already set, keep it and record `cleanup-failed` in `error_detail`."

The §7.1 and §8.8 rules say "keep prior verdict, record in error_detail." SH-015 says "downgrade to error only if no prior failure class has higher precedence" — implying there IS a precedence ordering. What's the ordering? Is `pass` lower precedence than `cleanup-failed` (yes, would downgrade) or higher (no, keep `pass`)?

What an implementer would do: read §7.1's plain rule and ship it (always keep prior verdict). The SH-015 phrasing suggests something more elaborate but no implementer would invent it.

Suggested fix: align the wording. The §7.1/§8.8 rule is the simpler one; rewrite SH-015 to match.

#### M16. SH-INV-001's grep sensor is brittle.

**SH-INV-001 / SH-018 / §10.2.** "A corpus-grep at the harness conformance test layer for `scenarioMode`, `isTest`, `isTwin`, `harnessMode`; any match is a fail." This is a spelling-based check. A determined or careless implementer can write `if testMode { ... }` (different spelling), `runtime.IsTest()` (function call), or `if cfg.Special { ... }` (renamed flag) and pass the grep.

What an implementer would do: pass the grep, ship a violation.

Suggested fix: either (a) make the grep more comprehensive (regex over `if .*[Tt]est|[Tt]win|[Ss]cenario|[Hh]arness.*Mode`), or (b) pin the verification to a positive contract (e.g., the daemon's composition root never reads an env var named `HARMONIK_*_MODE` for branching). Probably both.

#### M17. SH-027's "byte-identity not required, semantic identity required" leaves the diff target undefined.

**SH-027 / SH-INV-004.** "Byte-identity of the captured JSONL is NOT required... semantic identity of asserted observables is." So only assertion outputs need to match, not event-log contents. Fair. But SH-INV-004's sensor "diffs verdicts and failure classes across runs" — that's coarse; it would miss a regression where an unasserted-on event count or order changes silently.

What an implementer would do: implement the coarse diff (verdict + failure_class only) and ship. Some classes of nondeterminism go undetected for the lifetime of the regression suite.

Suggested fix: declare which fields in `ScenarioResult` are part of the determinism contract (verdict + failure_class + assertion_results.passed + assertion_results.actual_value? — that's likely a fuller surface), and have the SH-INV-004 sensor diff that field set.

### MINOR

#### m1. SH-002's `.yml` rejection is heavy-handed.

**SH-002.** "`.yml` MUST be rejected at parse time so the on-disk shape is uniform." Reasonable, but the wording implies parsing the file then rejecting; clearer to say "MUST be rejected at suite-load discovery time" so the file isn't even opened.

#### m2. SH-007 lexicographic-order assertion uses byte order or Unicode codepoint order?

**SH-007.** "Lexicographic order of their `name` field." With multibyte names this matters. Likely wants Go's `<` over strings (byte order); state it.

#### m3. §3 glossary entry for "scenario file" cross-references "see §4.1, §6.1" — but §4.1 is "Scenario file format," which is fine, and §6.1 is "Scenario file and result records," also fine. Good.

#### m4. SH-005's "fail the suite (not just the duplicates)" reads as "do not just fail the duplicates" — slightly awkward; rephrase as "the entire suite fails, not merely the duplicate scenarios."

#### m5. §7.1 pseudocode uses `RETURN ScenarioResult(verdict=error, failure_class=scenario-load-failure)` for step 1, but step 1 is "occurs in the suite-load phase" per the comment. The pseudocode would more naturally not include step 1 at all, since the run_scenario function is invoked AFTER suite-load.

#### m6. §6.1 ScenarioResult's `assertion_results` description says "empty if not reached," but for `verdict=fail` (assertion-failed), it's populated. A clarifying note: "empty if scenario terminated before §7.1 step 4."

#### m7. SH-016 says "MUST NOT delete the fixture root automatically on suite completion." Fine, but says nothing about cleanup *between* suite invocations — operators with CI agents should know. Add a note: "subsequent suite invocations create their own fixture roots; prior fixture roots accumulate until operator deletes them."

#### m8. §6.1 schemas use both `String` (capitalized) and `String` consistently, but `Enum (...)` is sometimes parenthesized and sometimes uses pipe-separated form (`Enum (event_present | event_absent)`). Template §6.1 grammar is consistent; minor conformance.

#### m9. §10.1 conformance scenarios reference paths like `smoke/twin-launch-and-ready.yaml` — these are scenario-relative paths, not implementation-source paths. A reader could be confused. State: "located at `scenarios/smoke/twin-launch-and-ready.yaml` per SH-002."

#### m10. SH-029 defines cadence supersets (`regression⊃smoke`, `nightly⊃regression`) verbally. The matrix would be cleaner as a §6.3 table:
| Filter flag | Includes cadence tags |
|---|---|
| `--cadence smoke` | smoke |
| `--cadence regression` | smoke, regression |
| `--cadence nightly` | smoke, regression, nightly |
| (omitted) | smoke, regression, nightly |

#### m11. §6.1 ScenarioFile lacks a `scenario_path` or `source_path` field, but ScenarioResult similarly lacks a `source_scenario_path`. Operators triaging a fail will want to know which file declared the scenario. Add `source_path: String` to ScenarioResult.

#### m12. The "SH-INV-005" sensor (`§5`) calls out a "fixed allow-list YAML parser." Naming a specific parser library (e.g., `gopkg.in/yaml.v3` with `Strict()` mode and `KnownFields(true)`) would help an implementer not pick a permissive library.

#### m13. Several requirements (SH-010, SH-019, SH-024) say `ScenarioResult.error_detail` "MUST" be populated, but `error_detail` is `String | None` in §6.1 — define what "populated" means (non-empty? must include stack trace? must include the underlying err.Error()?). A note like "non-empty operator-readable string" suffices.

#### m14. OQ-SH-005 has a defensible default but should also be tracked as a sensor question — how does the SH-INV-004 sensor avoid false-positive failures from wall-clock-mode twin scenarios? Defaulting them to `nightly` only solves it via cadence; cross-link OQ-SH-005 to SH-INV-004 explicitly.

#### m15. SH-002 forbids `scenarios/` outside the repo root, but the spec doesn't pin where "the repo root" is detected from. `git rev-parse --show-toplevel`? cwd?

## Coverage notes

Things the spec OMITS that an implementer needs.

- **Logger / output channel.** No mention of the harness's own logger. Where do harness-internal log messages go? A `--verbose` flag? Operator-readable progress on stderr? A log file under the fixture root?
- **Concurrency primitives inside a single scenario.** SH-031 covers cross-scenario; nothing constrains the harness's own goroutines (e.g., the timeout watchdog goroutine, the event-log tail-read goroutine). If the implementer multiplexes wrong, deadlocks happen at scale.
- **Signal handling.** What does the harness do on SIGINT mid-suite? Mid-scenario? Does it run teardown for the current scenario? Skip subsequent scenarios? Emit a partial SuiteResult?
- **PID / lock files for the harness itself.** Two `harmonik harness` invocations against the same repo at the same time — is that allowed? Forbidden? A no-op (one waits for the other)?
- **SuiteResult emission.** §6.1 declares the record but no requirement says how it's written or to which file/stdout. JSON to stdout? File under fixture root?
- **Per-scenario stdout/stderr capture.** The twin binaries write to stdout/stderr per HC-007. Are those captured per-scenario? Buffered? Discarded?
- **Skill-injection wiring.** `FixtureSetup.skill_search_paths` is mentioned; HC-049/HC-049a's skill provisioning is not consumed normatively. Does the harness short-circuit `skills_provisioned` per HC-051? Or does it run real skill provisioning?
- **Beads scope per scenario.** B1 mentions this; the spec is silent on whether each scenario gets a synthetic Beads SQLite, a shared one, or none at all.
- **Workflow registration.** EM-001's Workflow record requires `start_node_id` and `terminal_node_ids`; the scenario's `workflow_ref` resolves to a workflow definition somewhere. If the scenario inlines a DOT in `fixture_setup.files`, does the daemon load it via PL-005 step 7? At a different time?
- **Reconciliation interaction.** The harness's synthetic per-scenario project root will trigger PL-005 step 4 (Cat 0 pre-check) and step 8 (reconciliation dispatch). For a freshly-seeded fixture, both should pass trivially, but there's no normative confirmation. A scenario authored such that reconciliation has nontrivial work to do (e.g., the fixture seeds a half-completed run) lacks a behavioral contract.
- **Twin-binary --version probe.** HC-042 / HC-043 require launch-path verification including `--version` log; SH-009 mirrors this for twins via reference, but doesn't say whether the harness probes twins for version compatibility against the harmonik build.
- **CI exit-code mapping.** Per M7 above, the harness's own exit codes are not declared. CI lanes need stable mappings.
- **`--list` / `--dry-run`.** The harness should plausibly support listing the discovered scenario set without executing anything (for CI sharding). Not in scope; should be tracked in OQ if intentional.

## Strengths

- **The five-step lifecycle in §7.1** is executable as written and gives the implementer a concrete skeleton; the explicit `return_after_teardown` helper is a nice touch that makes cleanup unambiguous.
- **The failure-class taxonomy is small (8) and well-scoped.** Every class has detection / response / escalation / EM analog rows; the 1:1 mapping from §4 emission sites to §8 classes is verifiable. SH-024 and SH-INV-003 routing log-corruption and real-handler-launch into `harness-internal-error` rather than misclassifying as data failures is a thoughtful distinction.
- **The "no test-mode branches in production" position (SH-018, SH-INV-001) is sharp and load-bearing.** Even if the grep sensor is brittle (M16), the architectural posture is the right one.
- **Open questions are well-named and proportionate.** OQ-SH-002 (concurrency), OQ-SH-005 (heartbeat-mode determinism), OQ-SH-006 (composite predicates), OQ-SH-007 (schema versioning) are exactly the right deferrals; defaults are workable.
- **The conformance scenario floor (§10.1: three scenarios) is concrete.** An implementer can write the three YAMLs and know they're done at MVH; this is rare in foundation specs and worth preserving.
- **Cross-references to handler-contract, workspace-model, event-model, and execution-model are precise** (down to specific HC/WM/EV/EM/PL IDs); the integrator should preserve these during patching.
