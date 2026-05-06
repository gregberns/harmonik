# S07 Scenario Harness v0.1 — Critic R1 Review

**Date:** 2026-05-05
**Reviewer:** critic
**Scope:** specs/scenario-harness.md (adversarial pass)

## Summary

The spec is unusually well-structured — five-step lifecycle, eight failure classes, five invariants, three-scenario conformance floor — and reads as authored by someone who has internalized the foundation-corpus discipline. But adversarial reading turns up a pattern of weakness that undermines the spec's strongest claim, namely that scenarios are deterministic regression nets. Three systemic concerns dominate. First, the spec leans heavily on observable surfaces (event log, workspace tree) without naming the read mechanism: when does the harness observe? what's the absence-window? what bytes count as "the same"? Second, the spec inherits failure modes from the daemon stack (one-daemon-per-project per PL-001, single JSONL per project per EV-015, lease lock canonical path) but never confronts them — running scenarios sequentially against the production daemon entry-point is incompatible with PL-001 unless either (a) each scenario starts and stops a daemon, or (b) the harness creates per-scenario projects, and the spec is silent about which. Third, several invariants name a sensor without naming the sensor's specificity: the "no test-mode branches" grep is `scenarioMode|isTest|isTwin|harnessMode`, which catches naïve violations but is trivially evaded by `isFakeRunner`, `useStub`, `agent_kind == "twin"`, etc.

The eight failure classes are clean but the precedence rules between them are stated only narratively and are inconsistent (cleanup-failed vs. timeout-with-failed-cancel vs. assertion-failed-during-cleanup are all under-specified). The wall-clock budget contract has no clock-source declaration. The fixture-lifecycle has no notion of partial-setup-rollback. Twin-binary discovery has a security-shaped requirement (no unrestricted PATH lookup) but no anti-symlink-escape, no commit-hash check (HC-045 is referenced in §11.OQ-SH-003 but never normatively required of the harness). The "harness has no outbound network" requirement points at "verification mechanism: integration tests run with network sandboxing enabled" — that's describing the test, not declaring what counts as compliance, and a scenario that does outbound DNS resolution to localhost passes the loose form.

Overall: the spec is in good shape for v0.1 and the gaps below are mostly addressable by tightening verbs, naming numbers, and pulling decisions out of OQs. But OQ-SH-005 in particular is a punt that the spec depends on; OQ-SH-003 (twin distribution) has implications for SH-009 and SH-INV-003 that the v0.1 contract does not absorb.

## Findings by axis

### Normative force

**BLOCKER — SH-015 teardown is "MUST execute" but the per-step verbs are split.** The clause reads "Teardown MUST: (a) terminate any still-live handler subprocesses…; (b) release any worktree leases…; (c) close the event-log file; (d) record a `workspace_snapshot_path`…" but the framing makes (d) sound symmetric with (a)–(c). (d) is a recording obligation; (a)–(c) are termination obligations. Worse, the requirement does not state what happens if (a) succeeds but (b) fails halfway: are remaining leases retried? Bounded? Conformance becomes non-mechanical. Cite: spec §4.4 SH-015. Recommend splitting into one requirement per terminal action, naming a per-action timeout, and stating the precedence when one action fails.

**BLOCKER — SH-009 says "MUST NOT perform unrestricted `$PATH` lookup" but does not require a commit-hash check.** HC-045 in handler-contract.md states *twin* binaries MUST be launched from a known repo-relative path with an expected-commit-hash check. SH-009 mirrors HC-042 (the launch-path rule) but silently drops HC-043's commit-hash analog (HC-045). The harness can therefore conform while running the wrong twin version. Cite: spec §4.3 SH-009; cf handler-contract.md §4.10 HC-045. Recommend an explicit SH-009a "twin commit-hash verification at scenario load" requirement OR a normative cross-reference making HC-045 binding on the harness.

**MAJOR — SH-013 "Each scenario uses an isolated workspace" uses MUST but does not say what disjoint means at the filesystem level.** "Disjoint from every other scenario's workspace" is a pairwise property; "disjoint from the operator's working tree" is a stronger property; both are stated in the same sentence and both reduce to "no path overlap." But path overlap on POSIX includes case-collision on case-insensitive filesystems, symlink cycles, bind mounts, and overlapfs. The verb is "MUST" but the predicate is not mechanical. Cite: spec §4.4 SH-013.

**MAJOR — SH-027 "Identical inputs MUST produce identical observable verdicts" — but "identical inputs" includes wall-clock and PID, which the harness cannot stabilize.** The requirement waves at the issue with "Byte-identity of the captured JSONL is NOT required (UUIDv7 event IDs and wall-clock timestamps drift)." But the inputs are different on every run by definition (PID, ephemeral fixture path, daemon_instance_id per PL-005 step 0). The spec needs to say "given identical inputs UP TO the input-equivalence relation defined as <…>." Cite: spec §4.8 SH-027. The OQ-SH-005 punt makes this worse: it says wall-clock-mode twin scenarios are exempt, leaving SH-027 with an undefined applicability scope.

**MAJOR — SH-019 "MUST classify the scenario as `verdict=error` with `failure_class=orchestration-internal-error`" — but the detector ("error from the daemon's startup, dispatch, or shutdown that is NOT a scenario-attributable failure") is not mechanical.** "Scenario-attributable" is defined later as "handler error, twin scripted failure, gate denial, etc." The "etc." makes this an open list. A reviewer cannot run a corpus check that says "every error you saw was correctly classified" because the boundary is unwritten. Cite: spec §4.5 SH-019.

**MAJOR — SH-024 "harness MUST classify the scenario as `verdict=error` with `failure_class=harness-internal-error`" cites EV §6.2's torn-tail rule but does not adopt EV §6.2's classification.** EV §6.2 distinguishes torn-tail (silently discard in post-crash recovery; emit `store_divergence_detected` otherwise) from mid-file corruption (halt the reader). The harness's read window is post-orchestration on a scenario that should have terminated cleanly — neither "post-crash recovery" nor "live daemon read." Which classification does the harness use? Cite: spec §4.6 SH-024 vs. event-model.md §6.2.

**MAJOR — SH-026 cancellation MUST honor HC-018's "bounded cancellation" — but HC-018's bound is per-handler (T_cancel) and applies to handlers, not to the orchestrator's overall cancel-and-teardown bound.** The spec assumes that orchestrator-side cancellation completes within HC-018, but HC-018 is a per-subprocess cleanup ceiling. A scenario with 10 active subprocesses gets 10× T_cancel in the worst case. The spec does not name the harness-side wall-clock bound for "fixture teardown completion within HC-018 bounds" (cited verbatim in §10.2 SH-026 test obligation). Cite: spec §4.7 SH-026.

**MAJOR — SH-005 says "fail the suite (not just the duplicates)" but SH-006 says "Per-scenario load failures discovered during suite-load are still recorded as individual `ScenarioResult` entries with `verdict=error`."** These are in tension: the duplicate-name case is a per-scenario load failure that nonetheless aborts the entire suite. The two requirements are reconcilable but the reader has to do the work. Cite: spec §4.1 SH-005 vs §4.2 SH-006.

**MINOR — SH-002 "`.yml` MUST be rejected at parse time" is good rigor but the spec does not say where (suite-load-failure vs. directory-walk-warning).** Probably suite-load-failure but not stated. Cite: spec §4.1 SH-002.

**MINOR — SH-016 "MUST NOT delete the fixture root automatically on suite completion" is a strong negative obligation; reasonable but hides a question: must the harness expose a `--cleanup` flag, or must operators rm-rf manually?** The spec does not commit either way. Cite: spec §4.4 SH-016.

**MINOR — SH-029 "regression includes smoke; nightly includes both" — supersets are stated but the order is not declared as a partial order; what does `--cadence smoke` against an empty-smoke suite do?** Empty-result handling is not defined. Cite: spec §4.9 SH-029.

**MINOR — SH-031 sequential-execution "MUST execute scenarios sequentially at MVH" — the "MUST" verb is appropriate, but the requirement does not enumerate the consequences (what an implementer is allowed/forbidden to do).** May the implementer pre-fetch the next scenario's fixture in parallel with the current scenario's teardown? May setup of scenario N+1 begin while the assertion phase of scenario N runs? The strict reading "at most one orchestration drive active" allows concurrent fixture setup; the loose reading forbids it. Spec is silent on the boundary.

**MINOR — SH-006 "A suite-load that fails (any duplicates per SH-005, any parse errors, any schema errors) MUST abort the entire suite with a single suite-level error AND MUST NOT execute any scenarios."** But the same paragraph then says "Per-scenario load failures discovered during suite-load are still recorded as individual `ScenarioResult` entries with `verdict=error` so the operator sees the inventory." So the suite is aborted, but per-scenario results still get written. The on-disk artifact (a SuiteResult with `suite_verdict=fail` and `results: [...all-with-verdict=error...]`) is well-defined; but "MUST NOT execute any scenarios" then means each result is a stub. The contract is reconcilable but the prose makes the reader work to construct it. Cite: spec §4.2 SH-006.

### Vague language

**MAJOR — SH-001 footnote "tracked as an open question (OQ-SH-001) for a post-MVH revision" treats Go-as-code as a future feature, but OQ-SH-001 itself says "additive, not blocking the v0.1 contract."** Either the contract is YAML-only forever (in which case the OQ should be a Decision-Made not an Open Question) or it is YAML-only-for-now (in which case SH-001's "Other formats… are forbidden at v0.1" is fine but the "v0.1" hedge needs to be excised from the requirement and moved to a non-normative note).

**MAJOR — SH-011 "harness MUST NOT inspect the twin's internal state, log file shape, or process tree" — what does "process tree" mean when the harness is responsible for SH-INV-002's "no live process spawned by the scenario remains" (which DOES require process-tree inspection at teardown)?** These two contradict on their face. The reconciliation is presumably "post-teardown process-tree inspection is a sanity check, not behavioral observation," but the spec does not say so. Cite: spec §4.3 SH-011 vs. §5 SH-INV-002.

**MAJOR — §8.7 `harness-internal-error` "Detection. Any harness-internal failure not covered by the above" — that "any" includes everything, which means the class has no detector and acts as a catch-all.** The spec lists examples (event-log unreadable, real-model handler launch attempt, outbound network call attempted, harness panic) but does not commit to those being exhaustive. Cite: spec §8.7. Compare to event-model.md §8 where every event has a *detection rule*; here the detection rule is "everything else."

**MAJOR — SH-022 "as captured at fixture teardown per SH-015" — but SH-015 does not say the workspace is captured at a specific point during teardown.** SH-015 lists four teardown obligations: (a) kill subprocesses, (b) release leases, (c) close event-log, (d) record snapshot path. If the snapshot is taken AFTER (b) lease release, the merge back to integration branch may have happened (per WM-019), changing the workspace tree. Is the snapshot the per-run worktree, the integration branch, or the post-merge state? Cite: spec §4.6 SH-022.

**MAJOR — SH-028 "verification mechanism: integration tests at [scenario-harness conformance §10.2] run with network sandboxing enabled."** The verification mechanism is the test, not the requirement. What counts as an outbound network call — DNS lookup against systemd-resolved on localhost? Unix socket connection (a "network call" in Linux's sense)? `connect(127.0.0.1)`? The spec does not commit. Cite: spec §4.8 SH-028.

**MAJOR — SH-021(b) `event_absent` "no event of declared `type` matching declared predicates was emitted during the scenario's wall-clock window" — but the wall-clock window is defined indirectly (start = "moment fixture-setup completes"; end = "moment the orchestrator emits a terminal `run_completed`/`run_failed` event").** What if the scenario times out (no terminal event)? The window has no end. Does the absence-predicate hold over the timeout window? Over the cancellation window? The spec doesn't say. Cite: spec §4.6 SH-021.

**MINOR — SH-023 "scenarios are short by construction and full evaluation is cheap" — cost-vs-debuggability tradeoff justified informatively.** Fine, but "short by construction" is not enforced anywhere — `timeout_secs` has no upper bound. A 3600s scenario with 100 assertions is "short" by the prose but not by reality. Cite: spec §4.6 SH-023.

**MINOR — §8.4 "review escalation is automatic" — the word "automatic" is the kind of word an implementer can implement as `print("escalation")`.** Mechanism is unspecified. Cite: spec §8.4.

**MINOR — SH-014 "MUST NOT mutate the production `.harmonik/` tree" — does that include reading `.harmonik/daemon.sock` (which the harness must do if it goes through PL-005's daemon entry-point)?** Reading is not mutation, but the spec's prose does not exclude it. Cite: spec §4.4 SH-014.

### Hidden assumptions

**BLOCKER — Concurrency model is unstated; SH-031 says "sequential" but does not say within what scope.** "At most one scenario's orchestration drive may be active at any time" is silent on whether the harness itself is single-threaded, single-goroutine, or runs scenarios on a worker pool with size 1. The difference matters: a worker-pool implementation with size 1 may still leak goroutines between scenarios. Cite: spec §4.11 SH-031.

**BLOCKER — One-daemon-per-project (PL-001) makes SH-017's "harness uses the production daemon entry-point" non-trivial.** PL-001 says exactly one daemon per project; the harness running scenarios must either (a) start a fresh daemon per scenario (which is slow and wastes the suite-load phase's investment), (b) run all scenarios against one daemon (which violates SH-013's per-scenario isolation if scenarios share a `.harmonik/` tree), or (c) treat each scenario as its own ephemeral project with its own `.harmonik/`. The spec does not commit. SH-014 nudges toward (c) by requiring per-scenario event-log path overrides, but PL-001's pidfile contract (`.harmonik/daemon.pid`) and lease-lock canonical path (`.harmonik/lease.lock` per WM-013a) all use the same `.harmonik/` directory; if every scenario gets its own `.harmonik/`, the harness must construct one per scenario. The spec does not say how. Cite: spec §4.4 SH-014, §4.5 SH-017 vs. process-lifecycle.md §4.1 PL-001 and workspace-model.md §4.3 WM-013a.

**MAJOR — Filesystem assumptions are unstated.** SH-002 asserts `.yaml` over `.yml` for "uniformity," which presumes case-sensitivity (otherwise `Foo.YAML` is the same as `foo.yaml`); the spec does not state the case-sensitivity assumption. SH-013's "isolated workspace" assumes atomic-rename semantics for fixture creation; not stated. SH-016's "OS-temp directory" assumes the OS-temp directory is on the same filesystem as the harmonik repo (otherwise rename is a copy-and-delete and atomicity weakens); not stated.

**MAJOR — Time / clock assumptions are unstated.** SH-026 measures `timeout_secs` "from the moment fixture-setup completes to the moment the orchestrator emits a terminal `run_completed` / `run_failed` event." But which clock? Wall-clock can regress (NTP); monotonic clock cannot but doesn't give the operator a meaningful timestamp for the verdict report. Compare event-model.md §6.1 which specifies BOTH `timestamp_wall` (RFC 3339) AND `timestamp_mono_nsec` (optional monotonic ns). The harness should pick one for the deadline, not leave it implicit.

**MAJOR — OS assumptions are unstated.** Fixture cleanup, process-tree inspection (for SH-INV-002), signal handling (for SH-026 cancellation), pidfile probing (for stale-daemon detection — relevant to whether the harness-launched daemon contends with an operator daemon) all depend on OS primitives that differ Linux/Darwin/Windows. The spec is silent. Compare process-lifecycle.md §4.1 PL-002a which carries a careful Linux-vs-Darwin disambiguation; the harness inherits that discipline by importing the daemon, but the spec does not say so.

**MAJOR — Network sandboxing mechanism is unstated.** SH-028 says "integration tests run with network sandboxing enabled" but does not name the mechanism (Linux network namespaces? `iptables`? a runtime-injected DNS resolver? macOS `pf`? a process-level seccomp filter?). The mechanism is load-bearing for the invariant. Cite: spec §4.8 SH-028.

**MAJOR — Signal handling assumption: SH-026 cancels via the daemon's "operator-stop equivalent" but does not name the signal or the IPC primitive.** Per process-lifecycle.md §PL-003a the daemon socket carries a JSON-RPC `stop` method; is that what the harness invokes? Or does the harness send a UNIX signal to the daemon process? The harness needs deterministic cancellation, but the spec is silent on the cancellation mechanism's reliability properties.

**MAJOR — JSONL-on-disk assumption: SH-014 "captures events into a dedicated JSONL file under the scenario's fixture root" and SH-020 "MUST read the captured JSONL event log."** Per EV-015 the JSONL is at `.harmonik/events/events.jsonl` — a fixed path relative to the project root. The harness's per-scenario override must hook this path; the harness assumes the event-log writer accepts a path override. Per HC-INV-002 (twins indistinguishable from real), this override surface must exist for production code, not just for the harness. The spec does not declare the override surface (does it exist? where is it declared?). Cite: spec §4.4 SH-014.

**MAJOR — Subprocess permissions assumption: SH-013 says workspaces must be disjoint from the operator's working tree.** But the harness invokes the production daemon (SH-017), which spawns handler subprocesses (HC-044). Those subprocesses run with the daemon's uid/gid. If the harness runs as the operator, then handler subprocesses are also the operator. There is no privilege boundary in the spec; a scripted twin that does `rm -rf $HOME` would succeed. The spec does not commit to a privilege-isolation posture (sandbox, container, separate uid).

**MINOR — UUIDv7 high-water-mark assumption: per EV-002c the daemon writes `.harmonik/event_id_hwm` for cross-restart monotonicity.** Per-scenario daemons (assuming option (a) above) reset the HWM each scenario, which is fine; but the spec does not say so. Cite: spec §4.4 SH-014.

**MINOR — Daemon-instance-id assumption: per PL-005 step 0, every daemon mints a UUIDv7 `daemon_instance_id` and writes it to `.harmonik/daemon.instance-id`.** If scenarios share a `.harmonik/`, the instance-id file is overwritten each scenario; if scenarios have separate `.harmonik/`s, there's no contention. The spec doesn't pin which.

**MINOR — Beads (SQLite) assumption: per beads-integration.md the daemon talks to a `br` CLI which works on a SQLite-backed bead store.** A scenario that exercises beads writes implicitly relies on the bead store being writable. The harness's per-scenario isolation does not name the bead store; sharing across scenarios is a hidden assumption.

**MINOR — Locale assumption: lexicographic sort (SH-007) is locale-sensitive.** Go's default sort is byte-comparison (ASCII order), which is what the spec almost certainly means; but on systems with `LC_COLLATE` set, naive `sort` gives different orders. Spec should pin "byte-lexicographic" or "ASCII-collate."

### Ignored edge cases

#### Scenario load

**MAJOR — Scenario file > some-bound: not bounded.** YAML parsers can DoS on a 10 MB scenario file, or a 10 KB scenario with a million-element list (YAML expansion bombs are real). SH-003's "no `!eval` / `!!python/object`" guard catches code-execution but not resource-exhaustion. Cite: spec §4.1 SH-003.

**MAJOR — UTF-8 / BOM / encoding: not specified.** A scenario file with a UTF-16 BOM is YAML-parsed by some parsers, rejected by others; either way, the spec doesn't say. Cite: spec §4.1.

**MAJOR — Workflow ref resolution: SH-001 references `workflow_ref` as "DOT workflow file path or workflow_id per [execution-model.md §4.1]" but SH-004's `scenario-load-failure` covers "references an unknown workflow."** What about a scenario that references a workflow at a path the harness can read but the daemon's composition root cannot (path-resolution mismatch)? Where is the mismatch caught — at scenario-load time (before the daemon starts) or at daemon-startup time? The spec does not say. BLOCKER if the scenario passes scenario-load and then fails at orchestration-internal-error (verdict=error, but the error is mis-classified — should be scenario-load-failure).

**MAJOR — `ScenarioFile.name` regex / character set: unspecified.** The name must be unique repository-wide (SH-005) and is the file system path (lexicographic order per SH-007). Names with `/`, `\`, `..`, control characters, or whitespace can break path operations and ordering. Compare workspace-model.md §4.2 WM-006a which carefully specifies ref-safe substitution; the scenario-name surface has no equivalent.

**MAJOR — Two scenarios at the same lexicographic position: SH-007 says lexicographic order on `name` field; if two names tie, behavior is undefined.** SH-005 says names must be unique, so tie-on-name is impossible — but `matrix` (SH-030) generates synthetic names that include parameter values, and parameter-value strings can produce ties (e.g., values `[1, "1"]` both render as `=1`). The spec does not prevent this. Cite: spec §4.10 SH-030.

**MINOR — Matrix expansion bomb: SH-030 cartesian product can be unbounded (10 params × 10 values each = 10 billion cells).** No guard.

**MINOR — Circular scenario reference: SH-030's matrix is per-file, so circular-import is not possible at v0.1. But the spec does not commit to "no scenario-to-scenario references," it just doesn't define them.**

#### Fixture lifecycle

**BLOCKER — Fixture setup partial-success: SH-012 says "Fixture setup MUST: (a) create a fresh workspace; (b) create an isolated event-log directory; (c) prepare the twin-binary search path."** What if (a) succeeds and (b) fails? The spec says "Fixture-setup failure MUST classify as `fixture-setup-failed` per §8 and MUST NOT proceed to orchestration." But it does not say "and MUST roll back step (a) before recording the failure." Cite: spec §4.4 SH-012. Compare workspace-model.md §4.1 WM-003 which has well-specified atomicity for `git worktree add -b`. Here the rollback is silent.

**MAJOR — Fixture teardown failure after assertion success: SH-015 says "Teardown failure MUST classify as `cleanup-failed` per §8 and the prior verdict MUST be downgraded to `error` only if no prior failure class has higher precedence."** But §8.8 says "if a prior verdict (pass / fail / timeout) is already set, keep it and record `cleanup-failed` in `error_detail`." These are in tension: §4.4 says "downgrade to error only if no prior failure class has higher precedence" (downgrade is an action); §8.8 says "keep it and record in `error_detail`" (no downgrade). The reader can't tell which is normative. Cite: spec §4.4 SH-015 vs. §8.8.

**MAJOR — Worktree leases held by an unrelated lease lock: per WM-013a the lease-lock file is at `<workspace_path>/.harmonik/lease.lock` and is keyed by `run_id`.** If a scenario's run_id collides with a stale lock file from a prior crashed scenario in the same fixture root, what happens? SH-016 says "MUST NOT delete the fixture root automatically on suite completion," so prior scenarios' leases CAN survive across suite invocations. The spec does not say how the harness reconciles. Cite: spec §4.4 SH-013, SH-016.

**MAJOR — Multiple scenarios target the same git ref: a scenario whose `fixture_setup.git_seed` writes to a branch named `run/<run_id>` has a collision-on-rerun problem.** The fixture root is per-suite, so per-scenario worktrees should be per-suite too — but the spec doesn't tie those together. If two suite runs back-to-back use the same fixture root and the same scenario, the second run sees the first run's leftover branches. Cite: spec §4.4 SH-013, SH-016.

**MINOR — Disk-full during fixture setup: not addressed; classified as `fixture-setup-failed` by §8.3 default.** OK by default but operator-impacting.

#### Twin substitution

**BLOCKER — Twin binary is wrong version (stale build): SH-009 has no commit-hash check; HC-045 (in handler-contract) does, but the spec does not normatively bind it.** A scenario can pass against last week's twin while real production runs against this week's handler. Cite: spec §4.3 SH-009 vs. handler-contract.md §4.10 HC-045. Also covered under Normative Force above; double-listed because it surfaces both as a verb gap and as an edge case.

**MAJOR — Twin binary refuses to exit on signal: handler-contract.md §4.4 HC-018 puts a per-handler T_cancel bound; SH-026 inherits it.** But what if a (defective) twin ignores SIGTERM/SIGKILL? On macOS, SIGKILL cannot be caught, but a process can be in uninterruptible-sleep (D-state on Linux, similar on Darwin). The harness has no escalation past SIGKILL. SH-INV-002's "no live process spawned by the scenario remains" then becomes unfulfillable. The spec does not handle this. Cite: spec §4.7 SH-026, §5 SH-INV-002.

**MAJOR — Twin binary forks children that outlive it (orphan reaping): handler-contract.md §4.10 HC-044 mentions `PR_SET_PDEATHSIG(SIGTERM)` for handlers on Linux.** Twins-that-fork (e.g., a twin script that uses a shell pipeline) leave orphan grandchildren. SH-INV-002 requires all spawned processes terminate. Mechanism is unspecified.

**MAJOR — Twin binary opens network connections: SH-028 covers harness-issued outbound calls; what about twin-issued?** SH-028 says "from the harness, the daemon, the orchestrator, the watchers, the agent-runner, or the twin binaries" — twin binaries are listed. Good. But a twin can call `localhost:8080` to a shadow service the harness has running. Is that an outbound call? The spec is silent. Cite: spec §4.8 SH-028.

**MAJOR — Two twins write to the same workspace path simultaneously: WM-011 says "One active agent at a time inside a workspace."** The harness invokes the production daemon, which enforces WM-011. So this is enforced by inheritance. But if a scenario's `agent_overrides` defines two roles that both target the same workspace concurrently, the spec doesn't prevent the configuration; it relies on the daemon to refuse at runtime. The harness should reject the scenario at scenario-load time. Not stated. Cite: spec §4.3 SH-008.

**MAJOR — Twin binary writes stderr in unexpected format: HC-036 says twins emit the same progress-stream messages as real handlers.** But a twin that crashes writes a Go panic to stderr. Does the harness capture stderr at all? SH-020 says "the harness MUST read the captured JSONL event log" — stderr is not the JSONL. The spec is silent on twin stderr capture. For debugging a failed scenario, the operator needs the twin's stderr. Cite: spec §4.6 SH-020.

#### Orchestration drive

**MAJOR — Orchestrator advances a state the scenario didn't expect: classified as `orchestration-internal-error`?** The detector for §8.4 is "error from the daemon's startup, dispatch, or shutdown that is NOT a scenario-attributable failure." An orchestrator that takes an unexpected branch is not an error — it's a different valid execution. The harness should classify this as `assertion-failed` (the assertions didn't match) not `orchestration-internal-error`. Spec is silent. Cite: spec §8.4.

**MAJOR — Orchestrator hangs (no event for N seconds): silent-hang detection runs at the handler level (HC-026 with T=600s default).** The orchestrator-level hang (e.g., the orchestrator goroutine is stuck) is caught by … nothing in this spec. The harness's `timeout_secs` catches it eventually, but with `verdict=timeout` not `harness-internal-error`. That's arguably wrong: a stuck orchestrator is a defect, not a slow scenario. Cite: spec §4.7 SH-026.

**MAJOR — Orchestrator crashes mid-run: the daemon process exits.** SH-019 says "error from the daemon's startup, dispatch, or shutdown" — but daemon crash is a process exit, not a returned error. The harness must detect daemon process death; the spec does not say how. Compare process-lifecycle.md §PL-002a for advisory-lock-on-fd detection of daemon liveness.

**MAJOR — Daemon goes into `degraded` state per PL-010 mid-scenario: the harness has no degraded-state handling.** PL-005 step 4's Cat 0 pre-check can drive the daemon into degraded; the daemon's status events (`daemon_degraded`) are observable on the bus. The harness should classify a scenario whose daemon entered degraded as `orchestration-internal-error`, not silently watch the timeout fire. The spec doesn't say.

**MAJOR — Orchestrator emits more events than the assertion list expected: a scenario asserting `event_present(A)` and `event_present(B)` passes when A, B, C, D are all emitted.** That's correct behavior for a "presence" predicate, but a defective implementation that emits spurious events (e.g., a duplicate `agent_completed`) passes. The harness should at minimum offer (or commit to in v0.2) a "no events of class X beyond declared expectations" mode. Spec doesn't address.

**MINOR — Orchestrator-level retry loop: per execution-model.md §8 a node may retry on transient failure.** A scenario whose twin scripts a transient failure exercises retry; the event log will contain multiple `agent_started` events for the same node. The harness's `event_present(agent_started)` assertion fires positive without distinguishing retry-count. Not a defect; a v0.2 expressiveness gap.

#### Event log

**BLOCKER — Event log filesystem fills up: EV-016 makes JSONL append fsync-bounded for F-class events; SH-020 reads the event log.** A full disk produces a write error mid-fsync; the daemon emits `bus_overflow` per EV-011a or fails the session. The harness's classification — `assertion-failed`? `harness-internal-error`? `orchestration-internal-error`? — is undefined. Cite: spec §4.6 SH-020, §4.6 SH-024 vs. event-model.md §4.4 EV-016.

**MAJOR — Two scenarios write to the same event log path: SH-014 says "MUST configure the daemon's event-log path per scenario at startup."** Sequential execution (SH-031) makes simultaneous writes impossible. But cleanup of a prior scenario's event-log file is not specified — does the harness append, overwrite, or refuse on collision? The spec doesn't say. Cite: spec §4.4 SH-014.

**MINOR — Event log writer crashes mid-write (partial JSONL): SH-024 covers "truncated past the post-fsync tail rule."** Event-model.md §6.2 distinguishes torn-tail (silently discardable) from mid-file corruption (halt-and-emit-divergence). The harness's classification under each case is not precisely stated. Cite: spec §4.6 SH-024.

#### Assertions

**MAJOR — Assertion absence-predicate: how long do we wait before declaring "absent"? SH-021(b) says "during the scenario's wall-clock window."** The window's end is the terminal event OR the timeout. So `event_absent` evaluations must be deferred until the scenario ends. But that means a fast scenario (terminates in 1 second) and a slow scenario (terminates in 100 seconds) both produce "absent" verdicts despite different observation windows. Statistical confidence in absence is wildly different. The spec does not address this. Cite: spec §4.6 SH-021.

**MAJOR — Assertion compares structures; equality semantics unspecified.** `EventExpectation.payload_match` is a `Map<String, JSONValue>`. Comparison semantics: byte-equal? canonical-JSON-equal? value-equal (where `1 == 1.0`)? Numeric-tolerance equal? The spec is silent. Cite: spec §6.1 EventExpectation.

**MAJOR — Two assertions disagree (race in event ordering): EV-008 says UUIDv7 supplies ms-resolution total ordering across processes.** Two events at the same millisecond produce undefined order across processes. A scenario with `event_present(A)` and `event_present(B, after=A)` (which v0.1 doesn't have, but the principle is general) would race. v0.1's four-kind vocabulary doesn't expose ordering predicates explicitly, but the implicit ordering shows up via `expected_workspace` predicates that depend on which event happened first. Cite: spec §4.6 SH-021.

**MAJOR — Assertion expects event N at time T; event arrives at T+ε — pass or fail? v0.1 has no temporal predicates, so "at time T" doesn't apply.** But scenarios with timeouts close to the actual run duration will see events that arrive after the timeout-induced cancel; the spec does not say whether those events are observable to assertions. Cite: spec §4.6 SH-021.

**MINOR — `event_absent` over a payload-match-without-type: SH-021 says event types are required; what about predicate-only assertions ("no event with `error_class=ErrStructural` was emitted")?** Not supported by v0.1; OQ-SH-006 covers composite predicates but does not commit. Cite: spec §4.6 SH-021.

**MINOR — `workspace_state` predicates over symlinks: SH-022 says "absolute-path predicates are forbidden."** A repo-relative symlink that escapes the worktree is not absolute but is path-traversal. Not addressed. Cite: spec §4.6 SH-022.

**MINOR — Floating-point comparisons in `payload_match`: JSONValue includes numbers; numbers in JSON are double-precision floats by spec.** Two payloads with `{ "x": 0.1 + 0.2 }` and `{ "x": 0.3 }` are not byte-equal but are operationally equal. Spec doesn't say how to handle. Likely fine for v0.1 (no current event-payload field is a computed float), but the contract should commit.

**MINOR — Unicode normalization in payload_match: `{ "name": "café" }` (NFC) vs. `{ "name": "café" }` (NFD) compare unequal byte-for-byte but equal canonically.** Spec is silent.

#### Result emission

**MAJOR — Harness crashes after assertions but before writing ScenarioResult: there is no result.** The suite's `SuiteResult.results` list is shorter by one. Operators cannot tell whether the scenario passed. The spec has no "ScenarioResult durability" requirement. Cite: spec §6.1 ScenarioResult.

**MAJOR — ScenarioResult is described as a record but the on-disk format is not declared.** Where is it written? Is it appended to a per-scenario file, the SuiteResult, both? Schema evolution is mentioned (§6.3) but the file location is not. Compare event-model.md §6.2 which carefully pins JSONL-on-disk; here, nothing.

**MINOR — Multiple result-writers (CI + local) — locking?** If two harness invocations run concurrently against different cadences, do their SuiteResults clobber each other? Sequential execution (SH-031) is per-suite, not per-machine. Spec is silent.

#### Cleanup

**MAJOR — Process tree leak: SH-INV-002 names the sensor (process tree inspection at suite end), but does not say what the sensor inspects.** PIDs, process-group IDs, parent-PID lineage, env-var provenance markers (PL-006a)? The sensor needs an unambiguous predicate. Cite: spec §5 SH-INV-002.

**MAJOR — Lock files left behind on crash: per WM-013c lease-lock discovery is by filesystem scan at startup.** The harness's per-suite ephemeral fixture root accumulates lease locks across suite runs unless the harness sweeps them. SH-016 says fixture-root deletion is operator-driven, so locks survive between suites. The spec does not say whether the harness's per-scenario daemon (or per-suite daemon) sweeps prior-suite lease locks. Cite: spec §4.4 SH-016.

**MAJOR — Workspace files held open by another process at teardown time: macOS allows file deletion under an open fd; Linux also allows it.** No problem on POSIX, but `git worktree remove` can fail if a process is still holding worktree files open. SH-INV-002's lease-release obligation depends on this not happening. Cite: spec §5 SH-INV-002.

**MAJOR — Sub-process accounting between scenarios: SH-INV-002 says "no live process spawned by the scenario remains" but the harness invokes the production daemon, which itself spawns subprocesses across scenarios.** If the harness keeps a single daemon alive across scenarios (interpretation (b) above), the daemon's subprocesses (handler, br, watcher) are NOT scenario-spawned in any direct sense — they're daemon-spawned in service of a scenario's run. The accounting boundary is unclear. Cite: spec §5 SH-INV-002 vs. §4.5 SH-017.

**MAJOR — Cleanup-after-cleanup-failed: the `cleanup-failed` class has no recursion bound.** §8.8 says "Record the failure; if a prior verdict … is already set, keep it and record `cleanup-failed` in `error_detail`." But teardown itself involves multiple actions (kill processes, release leases, close logs); if killing processes succeeds but releasing leases fails, the next scenario inherits a held lease. The spec does not say teardown is run-to-completion best-effort vs. fail-fast.

**MAJOR — Suite-level cleanup: SH-016 says fixture-root deletion is operator-driven, but does not address suite-level resource cleanup beyond the fixture root.** A scenario that registers a global subscriber on the event bus, opens a long-lived socket, or stashes state in a non-`.harmonik/` location persists past suite end. SH-INV-001's "no test-mode branches" check forbids the daemon from knowing it's in a harness, so the daemon cannot have special suite-end cleanup. Spec doesn't address this.

**MINOR — Disk space unrecovered after teardown: per SH-016 fixture-root deletion is operator-driven; not really an "unrecovered" issue if it's by design.**

**MINOR — Long-running `br` (Beads CLI) subprocesses spawned by the daemon during scenario orchestration: per process-lifecycle.md PL-006, the orphan sweep extends to `br` subprocesses.** SH-INV-002 mentions "no live process spawned by the scenario remains" — does that include `br` subprocesses? The harness inherits PL-006's discipline but the spec doesn't explicitly include `br` in the SH-INV-002 sensor scope.

**MINOR — Symlink loops in fixture root: a `fixture_setup.files` map with `{ "a": "→b", "b": "→a" }` creates a loop; OS calls following the symlinks loop forever (or hit ELOOP).** Not addressed. Likely a v0.1 minor since the typical scenario doesn't use symlinks.

### Invariant strength

**SH-INV-001 — "No test-mode branches in production code."** Sensor: corpus-grep for `scenarioMode|isTest|isTwin|harnessMode`. The grep is too narrow. A defective implementation could write `if config.AgentKind == "twin"` (matches `isTwin` heuristically but the literal grep token is `agent_type == "*-twin"` — the spec lists this separately in §4.5 SH-018). What about `if r.handlerName == "claude-twin"`? `if isFakeRunner`? `if useStub`? `agent_type.HasSuffix("-twin")`? The grep set must either be expanded comprehensively or replaced with a typed annotation (e.g., a Go build tag check). Cite: spec §5 SH-INV-001 and §4.5 SH-018.

**SH-INV-002 — "Workspace state is fully reset on teardown."** Sensor: post-suite OS-process-tree inspection + worktree-lease-registry inspection. "Fully" is not mechanically defined. (a) "no live process spawned by the scenario remains" — what about reaped-but-zombie processes? (b) "no worktree lease is still held by the scenario's `run_id`" — the lease-lock file at `<workspace_path>/.harmonik/lease.lock` is the registry; what's the scope of the inspection (per-fixture-root or system-wide)? (c) "no event-log file descriptor is still open" — fd inspection requires `lsof` or platform equivalents; not declared. Cite: spec §5 SH-INV-002.

**SH-INV-003 — "Harness operates only on twin binaries."** Sensor: pre-launch check at handler-config resolution verifies every resolved binary path is a twin. The spec defines "is a twin" as "declared via the scenario's `agent_overrides` or matched against the configured twin-binary search-path prefix." But that is honor-system: a scenario `agent_overrides[role]` referencing a binary at `/usr/bin/claude` (the real Claude CLI) passes the "declared" gate. The check should additionally require the binary to satisfy HC-035's twin-parity contract (different binary name, no LLM calls), but the spec's check is only "the path is one we resolved." Cite: spec §5 SH-INV-003 vs. handler-contract.md §4.8 HC-035.

**SH-INV-004 — "Scenarios produce identical observable verdicts on rerun."** Sensor: nightly cadence task reruns the regression-cadence suite N times and diffs verdicts and failure classes. N is unspecified. SH-027 says "every run" but the sensor's N is finite; statistical confidence depends on N. The spec does not commit to a value. (§10.2 SH-027 names N≥10 in test-obligation prose, but the invariant's sensor doesn't bind that.) Cite: spec §5 SH-INV-004.

**SH-INV-005 — "Scenario files are declarative-loadable without code execution."** Sensor: a corpus lint at suite-load time runs every scenario file through a fixed allow-list YAML parser. The "fixed allow-list YAML parser" is unspecified — Go's `gopkg.in/yaml.v3`? `sigs.k8s.io/yaml`? Custom? Each parser has different default behaviors around aliases, anchors, merge keys, and tag handling. Spec is honor-system. Cite: spec §5 SH-INV-005.

### Open-question discipline

**OQ-SH-005 (twin-introduced non-determinism) is a punt the spec depends on.** SH-027's determinism contract assumes scenarios use the scripted heartbeat carve-out (per HC-026a). The OQ admits this and proposes "Default-if-unresolved: SH-027's determinism requirement applies to verdicts and failure classes only … wall-clock-mode twin scenarios are permitted but are tagged `nightly` cadence and explicitly exempt from determinism rerun checks." That is a decision, not an open question — it should be folded into SH-027 normatively. As an OQ it leaves SH-027 with an undefined applicability scope. **Recommendation: convert OQ-SH-005 to a Decision-Made and pull the carve-out into SH-027.**

**OQ-SH-003 (twin distribution) materially affects SH-009 and SH-INV-003.** If twins are vendored separately, the harness must verify a pinned version, which is exactly what HC-045 (commit-hash check) is for. If twins ship in-tree, the commit-hash collapses to the build's hash. The OQ defers the decision but the spec's twin-integrity story (or lack of one) depends on it. **Recommendation: pick one and move on.**

**OQ-SH-001 (Go-as-code scenarios) is real (additive), as the spec correctly notes.** No issue.

**OQ-SH-002 (concurrent scenario execution) is real.** No issue.

**OQ-SH-004 (auto-generated scenarios from S09) is real.** No issue.

**OQ-SH-006 (composite predicates) is real.** No issue.

**OQ-SH-007 (schema versioning) is real, but the default-if-unresolved ("missing schema_version is treated as 0.1.0") is in tension with §6.3's "every scenario file is an implicit `version: 0.1.0`."** If the implicit version is the spec's front matter version, then a `0.2.x` scenario file produced under the spec's `0.2.0` front matter and read by a `0.1.0` harness will be assumed to be `0.1.0` — which is wrong. The migration shim is incompletely specified. **Recommendation: state in §6.3 that v0.1 harness MUST refuse files declaring an unknown `schema_version` (forward-incompat) instead of silently coercing.**

**OQ-SH-008 (drift detection) is real.** No issue.

**OQ-SH-009 (testing.md migration) is real, common across the corpus.** No issue.

**OQ-SH-010 (failure-class extension) is fine.** Friction-level decision, no urgent gap.

### Prose-vs-normative drift

**MINOR — §1 last paragraph "It is load-bearing: per [docs/foundation/components.md §10] (bootstrap citation; migrates to component subsystem refs once finalized)."** The bootstrap-citation form is correct per the template's bootstrap clause. No drift, but the parenthetical is informative material in the §1 normative section; it should move to a `> NOTE:` callout.

**MINOR — §2.2 out-of-scope item on twin-conformance drift detection: "declared in scope of S07 by [handler-contract.md §4.8.HC-038], deferred from this v0.1 draft to a post-MVH revision (tracked at OQ-SH-001 if relevant)."** The OQ pointer says "if relevant" — OQ-SH-008 is the actual tracker, not OQ-SH-001. Either a typo in cross-reference or an inconsistency. Cite: spec §2.2 vs. §11 OQ-SH-008.

**MINOR — §4.5 SH-018 normative text concludes: "Reviewers MUST reject any production-package PR that introduces a `if scenarioMode` / `if isTest` / `if agent_type == "*-twin"` branch."** "Reviewers MUST reject" is a meta-normative statement; it's an obligation on the review process, not on the code. Belongs in §10 conformance, not §4. Cite: spec §4.5 SH-018.

**MINOR — §4.6 SH-023 informative tail: "This is a deliberate cost-vs-debuggability tradeoff: scenarios are short by construction and full evaluation is cheap."** Informative content inside a normative block. Move to `> RATIONALE:`.

**MINOR — §7.1 final note "INFORMATIVE: Every branch above maps to a §8 failure class; reviewers MUST verify the §8 enum is exhaustive over the lifecycle's failure transitions."** The keyword `MUST` inside an `> INFORMATIVE` block violates RFC 2119 discipline. Move to §10 reviewer-obligation prose. Cite: spec §7.1.

**MINOR — §8.7 informative tail (paragraph after §8.8): "INFORMATIVE: The taxonomy is deliberately small. Adding a class is a foundation amendment per [architecture.md §4.6]."** "Adding a class is a foundation amendment" is normative process content; it should be a numbered requirement, not a callout. Cite: spec §8 trailing paragraph.

**MINOR — §10.2 SH-008–SH-011 "corpus grep at the harness conformance test layer for forbidden production-package branches."** Test-obligation prose contains the substantive list of forbidden tokens. Either the list lives in §5 SH-INV-001 (where it currently does) and §10.2 cross-references it, OR it lives in §10.2. Currently both repeat (slightly differently — §4.5 SH-018 lists `if scenarioMode / isTest / agent_type == "*-twin"`; §5 SH-INV-001 lists `scenarioMode, isTest, isTwin, harnessMode`; §10.2 lists `scenarioMode, isTest, isTwin, harnessMode`). The three lists drift. Cite: spec §4.5, §5, §10.2.

### Tags / Axes discipline

Per the spec template §4.N+1, every requirement carries a `Tags:` line; the `Axes:` line is required when the requirement deviates from baseline OR involves LLM invocation, external I/O, state mutation, or non-idempotent side effects. The harness spec is mechanism-tagged throughout (correct — the harness is a deterministic test rig with no cognition). Spot-check:

**MAJOR — SH-006 (suite-load) carries `Axes: io-determinism=deterministic` etc. — but suite-load is filesystem I/O (read every YAML file under `scenarios/`), which is non-trivial state-aware I/O.** The baseline `replay-safety=safe; idempotency=idempotent` is right (re-reading scenarios is idempotent), but the requirement performs disk reads on a directory tree whose contents may have changed between invocations. The Axes line is present (good); the values are correct (subtle — io-determinism=deterministic holds only if the file set is stable, which the spec assumes). Acceptable but worth a note.

**MAJOR — SH-018 (no test-mode branches) carries `Tags: mechanism` only — no `Axes:` line.** SH-018 is a structural / declaration-style requirement (it forbids a class of code patterns). Per the template's "Exemption — declaration-only requirements," omission of Axes is permitted. OK.

**MAJOR — SH-007 (deterministic execution order) carries `Tags: mechanism` only.** Determinism on order is observable behavior — but the requirement itself is structural (it pins a sort algorithm) and the runtime baseline applies. OK to omit Axes.

**MINOR — SH-005 (name uniqueness) carries `Tags: mechanism` only — no Axes.** Suite-load failure on duplicate is observable behavior with state mutation (writing the SuiteResult). The Axes baseline applies but the spec author chose not to be explicit. Borderline; not a blocker.

**MINOR — SH-008 carries `Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent`** — all baseline values. Per the template, baseline omits Axes; SH-008's Axes line is redundant. Several other SH-NNN requirements also carry redundant baseline Axes lines (SH-002, SH-003, SH-004, SH-006, SH-009, SH-010, SH-019). Not blockers individually, but the pattern is sloppy. Pattern fix: drop redundant baseline Axes lines per template §4.N+1 ("Authors who tag reflexively will produce nothing because baseline-shaped requirements omit the line entirely").

**MINOR — SH-INV-002 carries `Axes: io-determinism=best-effort`** — correct deviation from baseline (teardown is best-effort given OS-level vagaries). Good.

**MINOR — SH-026 carries `Axes: io-determinism=best-effort; idempotency=non-idempotent`** — correct (timeout cancellation is best-effort within HC-018 bounds). Good.

### Conformance executability

**MAJOR — §10.2 SH-017–SH-019 "Cross-package tests verifying that the harness's daemon entry-point invocation matches the production `daemon` mode entry-point byte-for-byte (same composition-root function)."** "Same composition-root function" is checkable (Go function identity), but "byte-for-byte" of the invocation is not — invocations carry per-process state (PID, stack, gen-ids). The test obligation is over-stated.

**MAJOR — §10.2 SH-027–SH-028 "rerun the regression-cadence suite N≥10 times and diff verdicts and failure classes."** N≥10 is a number (good), but the diff predicate is "diff verdicts and failure classes." That's mechanical (good). However, the regression-cadence suite is itself defined by tag (SH-029); a scenario that drops to nightly between two N-runs could silently weaken the SH-INV-004 sensor. Spec doesn't pin the cadence at sensor-execution-time.

**MAJOR — §10.2 SH-031 "Suite-execution tests verify at most one scenario's orchestration is active at any time (e.g., active-scenario count never exceeds 1 across a regression-cadence run)."** The "active-scenario count" is observable — but what counts a scenario as "active"? Fixture-up is started? Orchestration drive returns? Teardown begins? The boundary is not specified; the test obligation can be satisfied by a check that fires only between scenario-i's teardown completion and scenario-(i+1)'s fixture-up start. Cite: spec §10.2 SH-031.

**MINOR — §10.1 conformance scenario set names three concrete scenarios but does not commit to where they live in the repo.** "`smoke/twin-launch-and-ready.yaml`" is a relative path, not the repo-root-relative form `scenarios/smoke/twin-launch-and-ready.yaml`. The reader must infer. Cite: spec §10.1.

**MINOR — §10.1 "smoke/twin-launch-and-ready.yaml — Asserts `event_present(agent_ready)`, `event_present(agent_completed)`, `exit_code(success)`."** `exit_code(success)` is shorthand for `OutcomeExpectation(outcome_status=SUCCESS)` per §6.1. The shorthand is informal; the actual scenario file conforms to §6.1 exactly, so the assertion list reads loose. Minor.

**MINOR — §10.2 SH-001..SH-007 obligations include "negative tests covering each malformed-scenario class."** "Each class" is enumerated (parse error, schema mismatch, unknown workflow, unknown cadence, duplicate name, `.yml` extension) — but the spec's `scenario-load-failure` class also covers "parameter-expansion failure" (per §8.1 detection rule). Negative-test obligation should mirror §8.1's full enumeration; currently misses parameter-expansion.

**MINOR — §10.2 SH-012..SH-016 obligation: "OS-process-tree assertion at suite end verifies SH-INV-002."** The "suite end" timing is vague — after the last scenario's teardown? After the SuiteResult is written? Process accounting drift between those two moments matters (a slow-exiting child can skew the assertion). Spec doesn't pin.

**MINOR — §10.2 SH-029..SH-030 obligation: "matrix-expansion unit tests verify the cartesian product and template substitution."** "Verify the cartesian product" is mechanical; "verify template substitution" depends on the substitution semantics (which template engine? lexical match on `{{ name }}`? Go template?). Spec doesn't pin the engine. SH-030 says "template-based" without naming the templating dialect. Minor — but operational interop will hit this.

### Definitional gaps in §3 Glossary

**MINOR — "scenario" entry says "a declarative test definition (a single YAML file under `scenarios/`)..."** Then §6.1 introduces a `ScenarioFile` record without saying the YAML file IS the on-disk projection of that record. The glossary's "scenario file" entry says "the on-disk YAML form of a scenario, parsed into the `ScenarioFile` record" — good. But a careful reader notices that "scenario" and "scenario file" are very nearly the same thing under v0.1. Acceptable, but could be sharpened.

**MINOR — "fixture" entry says "the per-scenario filesystem and process state set up before orchestration starts and torn down after the verdict is emitted."** But fixture teardown happens AS PART of producing the verdict (§7.1 step 5), not after. Minor inaccuracy; the timing is "after assertions complete," not "after the verdict is emitted."

**MINOR — "harness verdict" entry enumerates `pass | fail | timeout | error` — good, matches §6.1 ScenarioResult.verdict.** But the relationship between verdict and `failure_class` is informal ("absent iff verdict=pass") — this is a normative invariant about the record, not just a glossary gloss. Either move the constraint to §6.1 schema (with a `MUST` clause) or keep it as glossary and lose the normative force.

### Schema-field weaknesses (additional)

These are findings keyed to §6.1 record fields rather than to numbered requirements. Schema-level under-specification is a different failure mode from requirement-level under-specification — the records are the wire shape that authors and tooling will rely on.

**MAJOR — `ScenarioFile.workflow_ref: String` "DOT workflow file path or workflow_id."** The field is union-typed but the discriminator is implicit (a path-shaped string vs. an ID-shaped string). EM does not pin a workflow_id format here; the harness's parser must guess. Recommend: split into `workflow_path` and `workflow_id` (mutually exclusive, exactly one set) or carry a `kind` discriminator.

**MAJOR — `ScenarioFile.agent_overrides: Map<String, AgentOverride>` "key: agent role."** "Agent role" is undefined in this spec and is not pinned in any depended-on spec by a stable schema reference. A scenario referencing a role that the workflow does not declare is an error class — but which one? `scenario-load-failure` (caught at scenario-load) or `orchestration-internal-error` (caught at workflow-load)? Spec is silent.

**MAJOR — `AgentOverride.args: List<String> | None` "additional CLI args delivered as part of LaunchSpec."** Per HC §6.1 LaunchSpec, the args list is part of the binary's invocation. But the harness adds the scenario's args to whatever the production composition root would have set; merge semantics (append? replace? prepend?) are not stated.

**MAJOR — `FixtureSetup.git_seed: List<GitSeedOp> | None`** — The `GitSeedOp` type is named but not declared. The §6.1 record list does not include a `RECORD GitSeedOp:` block. Either it lives in a depended-on spec (workspace-model has commit/tag/branch operations but no `GitSeedOp` shape) or the type is undefined. This is a schema gap, not just a vagueness.

**MAJOR — `FixtureSetup.files: Map<String, String> | None` "optional path → contents map applied to worktree before orchestration."** Path is the key (good — repo-relative implied?). Contents is `String`. What about binary fixtures (a PNG, a TAR file)? `String` doesn't carry binary cleanly. Compare to HC §4.7 redaction which is byte-aware. Recommend: type contents as `Bytes | String` with an explicit encoding field.

**MAJOR — `EventExpectation.payload_match: Map<String, JSONValue>`** is shallow-merged or deep-merged against the actual payload? A nested predicate `{ "outer": { "inner": 1 } }` matches what — exact equality of `outer`, or "inner" appears within outer? The semantics determine whether scenarios can match against partial payloads. Spec does not commit.

**MAJOR — `WorkspacePredicate.kind: file_exists | file_contents_equal | file_contents_match | git_ref_at | commit_trailer_present`** — Five values; SH-021 says "four kinds" of assertion. The four §6.1 assertion-result kinds are `event_present | event_absent | workspace_state | exit_code`. So WorkspacePredicate.kind is the inner-shape of `workspace_state`, not a top-level kind. Fine, but the inconsistency between "four assertion kinds" (SH-021) and "five workspace-predicate sub-kinds" (§6.1) is jarring. Also: `file_contents_match` is regex? glob? Equal-after-normalization? Not stated.

**MAJOR — `ScenarioResult.event_log_path: String` "absolute path."** Absolute paths defeat portability of result records across machines. A SuiteResult exported from CI to a developer's laptop loses the ability to follow `event_log_path`. Compare WM-022 which carefully handles repo-relative paths. Recommend: relative to fixture_root, with the fixture_root field already in SuiteResult.

**MAJOR — `ScenarioResult.error_detail: String | None` "non-empty when verdict=error."** "Non-empty" is the only constraint. A panic stack trace, a YAML parse error message, and a string-formatted Go error all coexist in this field. No structure. Operator tooling that wants to filter/aggregate cannot.

**MAJOR — `AssertionResult.actual_value, expected_value: JSONValue`** — Comparison semantics not pinned (see Ignored Edge Cases above for `event_present` payload comparison). For `workspace_state` predicates over file contents, `actual_value` is what — the file contents (could be megabytes), a hash, the first N bytes? Spec does not say.

**MINOR — `SuiteResult.cadence_filter: Enum (smoke | regression | nightly | all)`.** "all" is the unfiltered case. SH-029 names three cadences plus implicit "all"; consistent. But `--cadence` flag in SH-029 does not document `all` as an accepted value — unfiltered is the no-flag case. Recommend: declare `all` (or whatever literal) in SH-029 explicitly.

**MINOR — `ScenarioResult.started_at, completed_at: Timestamp` (RFC 3339 wall-clock).** RFC 3339 supports timezone offsets and sub-second precision; the spec does not pin which (UTC? local? milliseconds? nanoseconds?). Compare event-model.md §6.1 which is explicit: "timestamp_wall (RFC 3339 wall-clock time at the emitter)." Even there, precision is ambient, not pinned.

**MINOR — `SuiteResult.suite_verdict: pass | fail` "pass iff every result.verdict==pass."** A SuiteResult can have `verdict=error` results (scenario-load-failure, harness-internal-error). Those force suite_verdict=fail; the prose is correct but the predicate "iff every result.verdict==pass" requires verifying that other verdicts (timeout, error) all map to suite_verdict=fail. Implementer might miss "error" as a fail-cause. Recommend: explicit "any non-pass result implies suite-verdict=fail."

### Lifecycle pseudocode and §7.1 specifics

**MAJOR — §7.1 step 5 "fixture-down + verdict" inconsistency.** Step 5 begins with `err = fixture_teardown(scenario)` — but earlier steps (2, 3, 4) all already invoke `return_after_teardown(...)` on failure. So if any prior step failed, teardown has already run; step 5's teardown invocation is a second teardown. Idempotency of teardown is not declared. Cite: spec §7.1.

**MAJOR — §7.1 deadline check `IF deadline_exceeded` is positioned AFTER `orchestration_drive(scenario, deadline)` returns — but the function signature does not say whether `deadline_exceeded` is a return-value bit, an error variant, or a separate state.** The spec leaves it implicit. If `orchestration_drive` returns a non-nil error AND the deadline is exceeded, which branch fires? Order-of-checks matters; spec is silent. Cite: spec §7.1 step 3.

**MAJOR — §7.1 step 4 "log = read_event_log()" is invoked AFTER orchestration completes — but on a timeout (deadline_exceeded), step 3 returns early via `return_after_teardown(verdict=timeout, ...)` and step 4 is skipped.** Consequence: a scenario that times out has no `assertion_results` in its `ScenarioResult`. Operators investigating a timeout cannot see which assertions had passed-up-to-then. The spec's debuggability story (SH-023's "no short-circuit on first assertion failure") is undercut by the timeout path's full short-circuit. Recommend: assertions should be evaluated even on timeout (best-effort), with `assertion_results` populated and the verdict still being `timeout`.

### Cross-spec contract gaps

**MAJOR — The harness inherits HC-INV-004 ("agent_ready precedes work dispatch") but does not say so.** A scenario with an `event_present(agent_ready)` assertion should not be able to "succeed" in a build that violated HC-INV-004 — if the daemon dispatched work before agent_ready, the work-dispatch event arrived before the ready event in the JSONL. The harness can detect this if its assertion vocabulary supported ordering predicates, but v0.1's vocabulary does not. So the scenario harness — the regression net for self-build — cannot detect HC-INV-004 regressions at v0.1. Cite: spec §4.6 SH-021 vs. handler-contract.md §5 HC-INV-004.

**MAJOR — The harness inherits EV-INV-002 (consumers tolerate observability gap on overflow) but assertions don't tolerate it.** A scenario whose `event_absent` assertion fires because the bus shed events under overload would mis-classify a production-defect-of-load as a scenario pass. The harness has no way to detect that bus_overflow occurred during the scenario. Cite: spec §4.6 SH-021 vs. event-model.md §4.4 EV-011a.

**MAJOR — The harness drives scenarios via the production daemon entry-point (SH-017), but PL-005's startup sequence dispatches reconciliation per [reconciliation/spec.md §4.2 RC-008] in step 8 BEFORE step 9 (ready).** A scenario with a fixture that resembles a Cat 1–6 reconciliation condition may trigger a reconciliation workflow at startup, polluting the event log before the scenario's "real" orchestration begins. Spec does not address how the harness ensures startup-reconciliation is empty. Cite: spec §4.5 SH-017 vs. process-lifecycle.md §4.2 PL-005 step 8.

**MINOR — The harness's "twin-binary search-path prefix delivered to the harness at startup" (SH-009) is configured how?** A flag, an env var, a config file? Compare HC-042's repo-relative-prefix mechanism, which is "configured at daemon startup" via an unstated mechanism (typical convention: daemon config file). The harness inherits the same vague configuration surface. Not blocking, but a usability gap.

**MINOR — `smoke/checkpoint-and-merge.yaml` (§10.1) asserts `event_present(workspace_merge_status{status=merged})`.** Per workspace-model.md §4.5 WM-021, `workspace_merge_status` is paired-phase (`pending`, `merged`). The conformance scenario asserts only on `merged`. A defective implementation that emits `merged` without `pending` passes this assertion. Recommend: assert ordered pair, or at minimum both events present.

### Cleanup precedence and verdict downgrade matrix

This subsection is one consolidated finding broken out because it crosses §4.4, §7.1, and §8.

**MAJOR — The verdict-downgrade rule is stated three times, inconsistently.**

Statement 1 (§4.4 SH-015): "Teardown failure MUST classify as `cleanup-failed` per §8 and the prior verdict MUST be downgraded to `error` only if no prior failure class has higher precedence."

Statement 2 (§7.1 step 5): "IF err != nil: IF verdict_already_set: keep prior verdict; record cleanup-failed in error_detail. ELSE: verdict=error, failure_class=cleanup-failed."

Statement 3 (§8.8 default response): "Record the failure; if a prior verdict (pass / fail / timeout) is already set, keep it and record `cleanup-failed` in `error_detail`. If no prior verdict is set (rare — implies the scenario reached teardown directly), emit `ScenarioResult(verdict=error, failure_class=cleanup-failed)`."

The three statements use three different semantic frames:
- §4.4 introduces "precedence" — a partial order over failure classes.
- §7.1 introduces "verdict_already_set" — a boolean.
- §8.8 introduces "if a prior verdict (pass / fail / timeout) is already set" — explicit verdict enumeration.

A scenario that completed assertions cleanly (`pass`), then teardown failed: §4.4 says downgrade to error if cleanup-failed has higher precedence than (no failure class) — the answer depends on the precedence table the spec doesn't supply. §7.1 says keep `pass`. §8.8 says keep `pass`. So §4.4 is in tension with §7.1 and §8.8.

A scenario that hit `assertion-failed` (verdict=fail), then teardown failed: §4.4 says downgrade to error if cleanup-failed has higher precedence than assertion-failed — depends on the precedence table. §7.1 says keep `fail`. §8.8 says keep `fail`. Same tension.

**Recommendation:** define a precedence table: `harness-internal-error > orchestration-internal-error > scenario-load-failure > twin-binary-not-found > fixture-setup-failed > cleanup-failed > scenario-timeout > assertion-failed > (pass)`. State that on co-occurrence of failure classes, the highest-precedence wins. Update §4.4 SH-015, §7.1 step 5, and §8.8 to consistently reference the table.

### Inter-spec contract conflicts (additional)

**MAJOR — SH-026 timeout cancellation invokes "the daemon's normal cancellation surface (operator-stop equivalent)."** Per operator-nfr.md §4.7 ON-029 (drain-timeout escalation), an operator-initiated drain that exceeds the drain-timeout escalates to SIGKILL of still-running agent subprocesses; ON-040 specifies the synthesis of `agent_warning_silent_hang{reason=drain_forced}`. Per HC-026b the synthesized event is ON-classified, NOT HC-classified. Question: when the harness initiates the cancel-on-timeout, does the synthesized event use `reason=drain_forced` (the ON taxonomy) or a harness-specific reason? The spec doesn't say. Cite: spec §4.7 SH-026 vs. operator-nfr.md §4.7 / handler-contract.md HC-026b.

**MAJOR — SH-014 / SH-016 fixture-path overrides imply the production daemon accepts a path-override surface.** No depended-on spec declares this surface. event-model.md §6.2 hardcodes `.harmonik/events/events.jsonl`. process-lifecycle.md §4.1 PL-004 hardcodes `.harmonik/`-rooted paths. The harness assumes a configuration mechanism that does not exist in any depended-on spec. Either:

1. The harness creates a per-scenario `.harmonik/` and chdirs (or sets `HARMONIK_PROJECT_ROOT` env var) — but no spec declares such an env var.
2. The harness gets a path-prefix knob into the daemon — but no spec declares such a knob.
3. The harness mutates the production code to thread a path through the composition root — but SH-INV-001 forbids this.

Resolution: the spec needs a normative §4.4 requirement declaring how fixture paths reach the daemon's I/O surface, or the harness's contract is unimplementable. Cite: spec §4.4 SH-014, SH-016.

**MAJOR — The harness's `--cadence` flag (SH-029) implies a CLI surface, but the spec does not declare CLI invocation contracts (binary name, arg parsing, exit codes).** Compare operator-nfr.md §8 which carefully enumerates exit codes for the operator CLI. The harness CLI gets none. An implementer can pick exit codes; cross-tool interop (CI runners) needs them stable.

## Top systemic patterns

- **The spec specifies "what" but not "how" at points where mechanism choice affects determinism.** Repeated cases: SH-022 (workspace snapshot taken when?), SH-026 (cancellation via what IPC?), SH-028 (network sandbox via what mechanism?), SH-INV-002 (process-tree inspection how?), SH-INV-005 (which YAML parser?). Each gap reduces conformance to honor-system and weakens the determinism invariant. Pattern fix: every "MUST" requirement that names a behavior should also name (or cross-reference) the mechanism that delivers it.
- **The spec inherits behavior from depended-on specs (handler-contract, process-lifecycle, workspace-model, event-model) but does not always confront the inheritance's edge cases.** Most consequential: PL-001 (one daemon per project) intersects with SH-031 (sequential scenarios) and SH-014 (per-scenario event-log path) in ways the spec does not resolve. HC-045 (twin commit-hash check) is referenced informatively but not made normative on the harness. EV §6.2's torn-tail/mid-file distinction is cited but not applied (SH-024). The composite gap: SH-014/SH-016 imply a daemon-side path-override surface that no depended-on spec declares; the harness's contract is unimplementable without that surface, but the spec does not require it. Pattern fix: trace each fixture-path / event-log / lease-lock cross-reference to a depended-on spec's normative declaration; add new requirements where the declaration is missing.
- **Failure-class precedence is implied but not stated in a single decision table.** §8 lists eight classes; §7.1 walks the lifecycle but cleanup-failed-after-assertion-success and timeout-with-failed-cancel each have inconsistent treatment (three different framings — "precedence", "verdict_already_set", "prior verdict (pass/fail/timeout)"). A precedence table over failure classes would make the lifecycle pseudocode mechanical and remove the three-way inconsistency. Pattern fix: add a §8.0 precedence table and reference it from §4.4 SH-015, §7.1 step 5, §8.8.
- **OQ-SH-005 leaves a determinism contract dangling; OQ-SH-007 leaves the schema-version coercion semantics dangling.** Both have default-if-unresolved fields with concrete decisions but the decisions are not pulled into the normative body. As written, SH-027's scope and the harness's behavior on a future v0.2 file are technically undefined. Pattern fix: any OQ whose default-if-unresolved is a real decision should be promoted to normative text; the OQ remains only if the answer can't be inferred from the existing surface.

## Strengths

- The five-step lifecycle pseudocode in §7.1 is uncommonly clear; every transition lands on a numbered failure class. This is the spec's strongest section. The pattern (lifecycle pseudocode where every branch maps to a failure-class enum) is exemplary and worth keeping even after the precedence-table fix above.
- The two-surface-mutation discipline (handler-config override + fixture-path override) per SH-008 / SH-014 / SH-016 is a sharp, testable boundary. SH-INV-001's no-test-mode-branches invariant is the strongest single normative claim in the spec — it gives the implementer one rule to reason about and one grep to run.
- Cross-references to the foundation-10 corpus are accurate (spot-checked HC-018, HC-026a, HC-035–HC-040, HC-044, HC-045, EM-005, EV-021, EV §6.2, WM-001, WM-013b, PL-005). The depends-on list names every spec the body actually touches; no over-claim, no under-claim. Citation hygiene is at corpus-level quality.
- The cadence-tag-with-superset semantics (SH-029) is a clean operator surface — three discrete tags with a partial order — and avoids the trap of arbitrary tag combinations. The decision to make cadence-tag mandatory (no harness-default) is the right discipline for keeping cadence-bloat under control.
- The conformance scenario set (§10.1, three scenarios) is concrete, falsifiable, and small enough to be a v0.1 floor without being a strait-jacket. Adding a scenario as a foundation amendment is the right friction level for what's effectively a regression-net definition. The scenarios cover the canonical happy-path (twin launch + ready), a workspace-positive path (checkpoint + merge), and a structural-failure path (twin crash without outcome) — three orthogonal axes in three scenarios is good packaging.
- The eight-class failure taxonomy is right-sized and orthogonal: each class names a distinct detection mechanism, default response, and EM analog (when applicable). The catch-all `harness-internal-error` is the weakest link (see Vague Language above), but the other seven classes pull their weight.
- The OQ list is honest about what's deferred: OQ-SH-001 (Go-as-code), OQ-SH-002 (concurrency), OQ-SH-006 (composite predicates), OQ-SH-008 (drift detection) are all genuinely open, not punts. Each names a trigger condition that, when met, escalates the OQ to a decision. This is good OQ discipline.
