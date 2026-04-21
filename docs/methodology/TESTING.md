---
title: Harmonik Testing Methodology
status: seed
type: methodology
sources: [docs/goals/end-to-end-testability.md, docs/subsystems/scenario-harness.md, docs/concepts/digital-twins.md]
related: [docs/methodology/METHODOLOGY.md]
created: 2026-04-21
updated: 2026-04-21
---

# Harmonik Testing Methodology

> How harmonik verifies its own correctness. This doc is about **what we test and at what layer**; S07 scenario-harness owns the execution engine for end-to-end tests, and G07 is the goal that drives this.

## Principle

**Every non-trivial behavior is testable without tokens.** Harmonik orchestrates probabilistic agents; if every test required real model calls, the test suite would be slow, expensive, non-deterministic, and under-run. The design posture that makes this work: twin binaries for every agent type, a scenario harness that drives full workflows against twins, and discipline at every testing layer.

This methodology defines the test **types** harmonik uses, what each proves, and when they run. The scenario-harness subsystem (S07) executes the expensive layers; the other layers are conventional Go testing.

## Testing layers

Harmonik tests at five layers, each answering a different question.

### 1. Unit tests

- **What they prove:** a single function or method does what its contract says, given well-formed inputs.
- **Scope:** one package, no subsystem-crossing.
- **No external dependencies.** No git, no filesystem (beyond `t.TempDir()`), no subprocesses, no network, no LLM.
- **Deterministic.** Same input, same output, every run.
- **Run:** always, on every push. Must complete in under 30 seconds for the full package.
- **Coverage target:** 80% line coverage per package; 100% coverage of error paths for code that parses external input (policy YAML, workflow DOT, event JSONL, checkpoint commit trailers).
- **Examples:** edge-selection cascade given a state and candidate edges; outcome-payload parsing; workflow-definition DOT ingestion.

### 2. Integration tests

- **What they prove:** two or more components inside a single subsystem cooperate correctly.
- **Scope:** one subsystem, no cross-subsystem boundaries (except through typed interfaces that are mocked).
- **Filesystem allowed** via `t.TempDir()`. Git allowed via per-test temp repo. No network, no LLM.
- **Deterministic** where possible; clock-dependent paths use an injectable clock.
- **Run:** always, on every push. Completes in under 2 minutes per subsystem.
- **Coverage target:** every public interface of the subsystem exercised by at least one integration test; every error category returned across the interface exercised by at least one test.
- **Examples:** event-bus producer + consumer with a real JSONL file; workspace-manager create/lease/merge lifecycle against a real git worktree; control-points evaluator evaluating a real predicate against a synthetic outcome.

### 3. Scenario tests (end-to-end)

- **What they prove:** a full workflow executes correctly against twin agents, with the full subsystem stack wired together.
- **Scope:** end-to-end — orchestrator + event bus + policy + workspace + handler (twin) + memory.
- **Real processes.** The scenario harness launches real harmonik binaries and real twin-agent binaries. No production model calls; the twins' behavior is scripted.
- **Run:** on every push. Completes in under 10 minutes for the standard suite; nightly suite may be longer.
- **Coverage target:** one scenario per workflow-library entry. Plus scenarios for every operator-control code path (pause, stop, upgrade) and every failure category.
- **Scenario categories we name:**
  - **Golden path.** Workflow completes through its success branch.
  - **Partial failure.** An agent fails with a specific category (`transient`, `structural`, `deterministic`, `canceled`, `budget_exhausted`, `compilation_loop`); the workflow's recovery path is exercised and observed.
  - **Rate limit.** Twin emits `agent_rate_limited`; policy-driven retry or escalation is observed.
  - **Hang / timeout.** Twin doesn't emit progress within the timeout budget; cancellation and cleanup are observed.
  - **Malformed output.** Twin emits output that fails schema validation; defensive handling is observed.
  - **Adversarial output.** Twin emits content intended to subvert a hook or policy (prompt-injection patterns); defenses hold.
  - **Crash recovery** (see §4 below).
- **Examples:** "builder agent completes a task, review agent approves, merge succeeds"; "builder fails with compilation_loop three times, escalation fires"; "operator issues `stop --graceful` mid-run, workspace reaches terminal state, no data lost."

### 4. Crash-recovery tests

- **What they prove:** harmonik survives process death, machine death, and mid-operation interruption without corrupting state.
- **Scope:** end-to-end, like scenario tests, but with injected termination at controlled points.
- **Technique:** the scenario harness offers an "interrupt" primitive — mid-workflow, send SIGTERM or SIGKILL to the orchestrator, wait, restart, and assert.
- **Assertions:** no duplicate work (no bead claimed twice); no lost work (every committed state advance survives restart); no false completes (no bead marked closed without the merge commit existing); workspace state resolves to a well-defined terminal state.
- **Run:** on every push for the fast subset (3–5 scenarios); nightly for the full suite.
- **Coverage target:** named interrupt-points for every vulnerable state-machine transition. Non-exhaustive starter set: between bead claim and first commit, mid-commit (SIGKILL during write), after merge before queue update, during workspace cleanup, during operator pause drain.
- **Harmonik-specific:** these tests especially exercise the git-history-as-source-of-truth reconciliation path on restart — given partial Beads state and partial queue state, does harmonik correctly derive the true completion state from git?

### 5. Property tests

- **What they prove:** invariants hold across a space of inputs that unit tests can't enumerate.
- **Scope:** algorithmic code — edge selection, graph traversal, cycle detection, schema validation, policy evaluation.
- **Technique:** Go's `testing/quick` or `gopter` generates inputs; test asserts an invariant.
- **Run:** on every push, with a seeded generator for determinism; nightly with random seeds.
- **Examples:** "edge-selection cascade is deterministic for identical state + candidate set"; "cycle-detection never false-positives on a DAG"; "checkpoint-commit-message round-trips through parse/emit."
- **Coverage target:** every mechanism-tagged algorithm has at least one property test.

## What we deliberately do NOT test this way

- **LLM output quality.** Harmonik can't unit-test the quality of a model's output. That lives in the real-agent conformance suite (see §Twin conformance below), which is rare and expensive.
- **Policy semantic correctness.** We test that the policy engine executes the rules operators wrote; we don't test whether the rules themselves are "right" for the operator's intent. That's operator-review territory.
- **Cosmetic UX.** Test the semantics of operator controls (does `stop --immediate` actually stop?), not the wording of the CLI output.

## Twin conformance

Twins must stay honest — their behavior must track what real agents actually do. Drift detection is NOT in MVH but is a known gap. The scope belongs to S07 scenario-harness. Placeholder plan:

- **Conformance suite.** A small set of scenarios run against a real agent AND its twin. Assertions on the event stream. Drift = test fails.
- **Cadence.** On every real-agent version bump; on every twin update; monthly in CI.
- **Ownership.** S07 owns the suite; foundation specifies the obligation.

## Test infrastructure conventions

- **No external services in CI.** Beads SQLite is embedded; event log is a tempdir; workspaces are tempdir worktrees. CI runs do not touch the network.
- **Seeded determinism.** Every test that uses randomness accepts a seed. CI uses the committed seed; nightly uses random seeds for shake-out.
- **Test data as code.** Scenarios, policies, and workflow DOTs used in tests live under `testdata/` in their owning subsystem. Not shared across subsystems unless explicitly named as a shared fixture.
- **Clock injection.** Anything that consults the clock takes a `Clock` interface. Tests use a `FakeClock`.
- **Twin invocation uniform.** Handler code launches whatever binary the workflow/policy specifies. No code branches on "is this a twin?"

## Coverage enforcement

- **CI gate.** The unit + integration + scenario suite passes on every push; the crash-recovery fast subset passes on every push. Nightly: full property suite, full crash-recovery suite, conformance suite (once implemented).
- **Coverage thresholds** (per package): 80% line, 100% error-path for boundary parsers. CI fails the merge if thresholds regress.
- **No skipped tests in main.** A skipped test is a lie about coverage; either fix, delete, or document as `//go:build integration` behind a gate.

## Testing during the bootstrap phase

While harmonik is being hand-built (Phase 1 per `docs/bootstrap.md`), the test suite is bootstrapped alongside:

1. Twin binaries exist from day 1 of handler-contract implementation.
2. The scenario harness has its first scenario before the orchestrator's first scheduled run.
3. Crash-recovery tests are in place before harmonik is entrusted with self-build cycles — otherwise a self-break cycle can destroy itself with no regression net.
4. Property tests get added per algorithm as algorithms land.

## Testing during self-build (Phase 2)

Every self-build cycle passes the scenario suite that the prior version passed. This is the "can harmonik still build harmonik?" regression net. A cycle that regresses the suite does not merge.

## Cross-references

- [G07: End-to-End Testability](../goals/end-to-end-testability.md) — the goal this methodology serves
- [S07: Scenario Harness](../subsystems/scenario-harness.md) — the subsystem that runs the expensive layers
- [Digital Twins](../concepts/digital-twins.md) — the mechanism that makes all this cheap
- [docs/methodology/METHODOLOGY.md](METHODOLOGY.md) — the KB methodology (parallel doc)
- [docs/bootstrap.md §6](../bootstrap.md) — risks specific to self-build, each of which has a test-type here that catches it
