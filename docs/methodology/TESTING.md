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

Harmonik tests at six layers, each answering a different question.

### 1. Unit tests

- **What they prove:** a single function or method does what its contract says, given well-formed inputs.
- **Scope:** one package, no subsystem-crossing.
- **No external dependencies.** No git, no filesystem (beyond `t.TempDir()`), no subprocesses, no network, no LLM.
- **Deterministic.** Same input, same output, every run.
- **Run:** always, on every push. Must complete in under 30 seconds for the full package.
- **Coverage obligation:** every error path exercised for code that parses external input (policy YAML, workflow DOT, event JSONL, checkpoint commit trailers). **No line-percentage target** — coverage is a diagnostic, never a goal. The question is *which real behavior is unprotected*, not *what percent*.
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

### 6. Docker cross-container E2E (remote-substrate) — REQUIRED, localhost-SSH-independent

- **What it proves:** the remote-substrate lifecycle — `git worktree add` on a remote worker plus `git fetch ssh://worker…` back to box A — works over a **real SSH transport across two separate containers**, not just a localhost loopback.
- **Why it's its own tier:** the §3 scenario twin of this test (`internal/daemon/scenario_remote_substrate_localhost_test.go`) drives the same path over localhost SSH, which silently passes or skips when the host's SSH loopback is misconfigured. The Docker tier is **localhost-SSH-independent**: it stands up a daemon container ("box A") and an sshd worker container on compose's bridge network, so a broken host loopback cannot mask a real regression. It runs with `HARMONIK_REQUIRE_REMOTE_E2E=1` — a broken SSH path fails **loud**, never skips green.
- **Scope + technique:** two containers, real sshd, keypair handed off at compose-up through a shared `keys` volume, `origin.git` + worker clone on a shared volume at the identical `/shared` path in both. The daemon execs a compiled scenario test binary against the worker.
- **Run:** `make test-docker-e2e` (repo root). REQUIRED tier — a green run is the single signal that the two-container remote-substrate path passed. Needs a working Docker; needs no host `~/.ssh` setup.
- **Docs:** [`test/docker/README.md`](../../test/docker/README.md) — full topology, key-handoff, the `HARMONIK_E2E_SHARED_ROOT=/shared` identical-path requirement, and the WS4 credential mount point (mounted `~/.claude`, **never** `ANTHROPIC_API_KEY`).

### 7. Subprocess daemon-boot (real binary as a separate OS process)

- **What it proves:** the *compiled* `harmonik` binary boots as its own OS process — not an in-process `daemon.Start` — surfaces its unix socket, accepts a bead over the real CLI (`harmonik queue submit`), and drives that bead to a **terminal** run outcome. This is the full subprocess pipeline: process boot → socket → CLI-submit → dispatch → terminal signal, with nothing stubbed at the process boundary.
- **Why it's its own tier:** every §3 scenario test drives the daemon *in-process*, so it can't catch a regression that only manifests when `harmonik` runs as a real subprocess (composition-root wiring, flag parsing, socket handoff across a process boundary). This tier closes that gap.
- **Two legs (M6 WS2.4):**
  - **Non-docker smoke** — `cmd/harmonik/subprocess_boot_smoke_test.go`, behind a dedicated `subprocess` build tag (never compiled by default `go build`/`go test ./...`). Billing-free: it points `--codex-binary` at the compiled `generic-twin`, whose handshake fails fast so the run reaches a terminal `run_failed` without tmux, a real agent, or the network. Run: `make test-subprocess` (or `go test -tags subprocess -run TestSubprocessDaemonBootSmoke ./cmd/harmonik/`).
  - **Docker/containerized variant** — the §6 Docker cross-container E2E (`make test-docker-e2e`) **is** the subprocess form of this tier: the daemon container runs the real `harmonik` binary and execs a compiled scenario binary against the worker over real SSH. It additionally asserts a clean bead-*close* through the real binary, which the non-docker smoke deliberately leaves out of scope.
- **Run:** the non-docker smoke is fast, deterministic, and zero-token, but is **deliberately not wired into a CI workflow** — it runs locally / as an assessor-forced gate (`make test-subprocess`). This is settled, not a pending follow-up: the smoke `t.Skip`s when `br` is not on `PATH`, and no CI runner provisions `br`, so wiring it into CI would either skip straight to green (the exact "green CI skip masks a regression" failure mode the forced-local split exists to prevent — see the note under the gate map) or force a `br`-version pin into CI (the project does not bind to external tool versions). It stays forced-local alongside the other real-daemon tiers. The Docker variant runs per §6 (also assessor-forced, not CI). See the gate map below for exactly where each tier is required.

## Gate tiers & risk-tiering

The single authoritative map of **which test tier runs where** and **which tier a given change is required to pass**. When the two sources below disagree, the *workflow files are ground truth* and this table is stale — fix the table.

> **Two independent "tier" numberings — do not conflate.** The **layer** numbers above (§1 unit … §7 subprocess) name *kinds* of test. The **risk tiers** (R1/R2/R3) below name *how much gate a change must clear*. A high-risk change (R1) is required to pass more layers than a low-risk one (R3).

### Where each layer runs (CI vs local)

| Test layer | Invocation | CI workflow | Merge-blocking? |
| --- | --- | --- | --- |
| §1–§5 unit / integration / scenario(in-proc) / crash-recovery(fast) / property — via `-short` | `make full` | `ci.yml` → *make full* | **Yes** — blocks merge |
| gofumpt+gci / vet / build / golangci-lint | `make fast` (inside `make full`) | `ci.yml` → *make full* | **Yes** |
| Core set (CHARTER §3 pipeline) **with no `-short`** — the real-daemon E2E tier included | `make core` | *(none — local / assessor)* | Assessor sign-off gate. This is the target that answers "can this run beads through the queue?", so it runs the 45 tests `-short` skips, `TestScenario_HappyPath_N1` and `TestSmokeLoop` among them. About 250s. |
| commit-message trailers / secret scan | `scripts/validate-commit-msg.sh` · `make secret-scan` (via `/check`) | *(agent-driven; git hooks retired)* | No |
| §3 scenario suite (full, `-tags=scenario`, incl. `internal/daemon` scenario files) | `make test-scenario` | `scenario.yml` → *scenario (Tier 3)* | **No today** (`continue-on-error`); WS1.1 flips the **`./test/scenario/...`-only** invocation to a required check — never the daemon bundle, which `t.Skipf`s green on sshd-less runners |
| full `-race`, no `-short`, uncapped parallel | `make test-race-nightly` | `nightly-race.yml` | No (nightly shake-out) |
| §6 Docker cross-container remote-substrate E2E | `make test-docker-e2e` | *(none — local / assessor-forced)* | Assessor gate, not CI |
| §7 subprocess daemon-boot (non-docker) | `make test-subprocess` | *(none — local / assessor-forced; deliberately off CI — `br`-on-PATH dep would skip-to-green)* | Assessor gate, not CI |
| Core-loop LT gate (real pi/codex/claude cells, forced) | `make core-loop-lt` | *(none — forced-local, WS4-5)* | Assessor LT-leg gate, never CI (non-zero on any non-green cell; emits `MATRIX_JSON` grid) |
| Real-agent conformance (twin↔real) | *(rare, expensive)* | *(none — on-demand)* | Assessor gate, not CI |

The localhost-SSH + Docker + subprocess tiers are **the assessor's forced-local gate**, deliberately kept off the CI required-check path (a broken host loopback must fail loud locally, never mask a regression as a green CI skip — see §6).

### Risk-tiering rule — which tier a change must clear

Every change gets a **risk tier**; the risk tier sets the *minimum* set of layers that must pass before it lands.

- **R1 (highest) — daemon / lifecycle core.** A diff touching `internal/daemon/**` or `internal/lifecycle/**` is **auto-R1** by path-glob **floor**. R1 requires: CI (`make full`) green **and** the full scenario suite (§3) green **and** the assessor-forced Docker remote-substrate E2E (§6) green. These paths carry the highest blast radius (dispatch, crash-recovery, promote/reconcile), so the false-green-proof tiers are mandatory.
- **R2 — other product code** (`internal/**` outside the R1 globs, `cmd/**`): CI Tier 2 green **and** any §3 scenario that exercises the touched path green. The assessor raises to R1 when a change reaches into a daemon/lifecycle seam indirectly (e.g. a shared type a daemon path depends on).
- **R3 — docs / test-only / tooling** (`docs/**`, `*_test.go` with no product-source change, `Makefile`/CI-config where the change is self-evidently inert): CI Tier 2 green. No scenario/Docker requirement.

**The path-glob is a floor, not a ceiling.** The assessor can only **raise** a change's tier above its glob floor, never lower it. An R1 path is R1 even if "it's just a one-liner." Conversely a nominally-R3 doc change that alters a *gate definition* (this file, a workflow, `.golangci.yml`) is raised by the assessor because it changes what "green" means.

## What we deliberately do NOT test this way

- **LLM output quality.** Harmonik can't unit-test the quality of a model's output. That lives in the real-agent conformance suite (see §Twin conformance below), which is rare and expensive.
- **Policy semantic correctness.** We test that the policy engine executes the rules operators wrote; we don't test whether the rules themselves are "right" for the operator's intent. That's operator-review territory.
- **Cosmetic UX.** Test the semantics of operator controls (does `stop --immediate` actually stop?), not the wording of the CLI output.

## Twin conformance

Twins must stay honest — their behavior must track what real agents actually do. Drift detection is not built yet but is a known gap. The scope belongs to S07 scenario-harness. Placeholder plan:

- **Conformance suite.** A small set of scenarios run against a real agent AND its twin. Assertions on the event stream. Drift = test fails.
- **Cadence.** On every real-agent version bump; on every twin update; monthly in CI.
- **Ownership.** S07 owns the suite; foundation specifies the obligation.

## Scenario fixture determinism recipe

Scenario tests boot a real daemon against twin binaries in a temporary git repo, which introduces four classes of non-determinism. The canonical recipe, discoverable in `internal/daemon/scenario_concurrent_multiqueue_hkumemp_test.go` and `internal/daemon/run_w3cp1_boiwe_hiqrl_test.go`, eliminates each one.

### 1. No-commit guard — `WithWorktreeFactory(emptyCommitWorktreeFactory)`

The daemon's merge path checks that `HEAD` has advanced past the bead's base SHA before merging (`hk-mmh8f` no-commit guard). Twins that emit protocol events but do not run `git commit` would trip this check and produce a `no_commit` `run_failed` event.

Fix: inject `emptyCommitWorktreeFactory` via `daemon.WithWorktreeFactory`. It wraps `ExportedProductionWorktreeFactory` and, immediately after creating the worktree, runs `git commit --allow-empty -m "test: advance HEAD for <run_id>"`. HEAD advances past the base SHA before the handler binary starts. Using `--allow-empty` is critical: a file-based commit adds a `D <file>` to `git status` in the brief window between `update-ref` and `reset --hard` in `mergeRunBranchToMain`, triggering a false positive in `checkMainWorkingTreeDirty` for any concurrently-running bead.

### 2. Concurrent-merge race — `WithMergeMutex(&mergeMu)`

When `MaxConcurrent > 1`, multiple bead goroutines may finish their rebase and attempt to push to the shared bare-repo origin simultaneously. The second push sees a non-fast-forward ref and emits `push_failed` → `run_failed`, even when both worktrees contain correct commits.

Fix: declare a `sync.Mutex` in the test and inject it via `daemon.WithMergeMutex(&mergeMu)`. This overrides the production per-daemon merge mutex and serialises the full `rebase → update-ref → push` sequence across all concurrent goroutines. The mutex is test-local, so parallel test cases do not contend.

### 3. Phase-aware twin wrapper (review-loop mode)

Scenario tests that run under `WorkflowModeDefault: core.WorkflowModeReviewLoop` launch two handler invocations per bead: implementer phase and reviewer phase. A twin binary that always runs the same scenario will skip writing a `review.json` verdict in the reviewer phase, causing the review loop to stall with `verdict absent at iteration 1`.

Fix: write a `/bin/sh` wrapper script rather than invoking the twin binary directly. The wrapper detects the current phase by checking for `.harmonik/review-target.md`, which the daemon writes only into the reviewer's isolated worktree:

```sh
#!/bin/sh
set -e
if [ -f "$PWD/.harmonik/review-target.md" ]; then
  mkdir -p "$PWD/.harmonik"
  printf '{"schema_version":1,"verdict":"APPROVE","flags":[],"notes":"scenario review-loop happy path"}' \
    > "$PWD/.harmonik/review.json"
  exit 0
fi
exec "/path/to/harmonik-twin-generic" --scenario single-happy-path
```

- **Implementer phase** (`review-target.md` absent): runs the twin with `--scenario single-happy-path`. The twin emits protocol events; `emptyCommitWorktreeFactory` provides the commit.
- **Reviewer phase** (`review-target.md` present): writes an APPROVE verdict and exits. The reviewer must NOT commit (its worktree gets no pre-commit from the factory).

### 4. Skip flags — disable expensive test-incompatible paths

Three `daemon.Config` fields disable operations that are either unnecessary in tests or introduce timing hazards:

| Flag | Why |
|---|---|
| `SkipWALCheckpoint: true` | Avoids SQLite WAL checkpoint on exit, which races with test teardown |
| `SkipBrHistoryRotation: true` | Avoids rotating the br history log, which touches the live `.beads/` tree |
| `SkipRestartBackoff: true` | Removes the exponential backoff on daemon restart so tests don't wait |

### Putting it together

```go
var mergeMu sync.Mutex
go func() {
    startDone <- daemon.StartForTesting(ctx, daemon.Config{
        ProjectDir:            projectDir,
        JSONLLogPath:          jsonlPath,
        HandlerBinary:         twinWrapperScript,
        NoAutoPull:            true,
        MaxConcurrent:         2,
        SkipWALCheckpoint:     true,
        SkipBrHistoryRotation: true,
        SkipRestartBackoff:    true,
        AgentReadyTimeout:     5 * time.Second,
        WorkflowModeDefault:   core.WorkflowModeReviewLoop,
    },
        daemon.WithWorktreeFactory(emptyCommitWorktreeFactory),
        daemon.WithMergeMutex(&mergeMu),
    )
}()
```

All four elements are required for deterministic concurrent-scenario tests. Omitting any one produces intermittent failures that vary with scheduler timing.

## Flake policy — de-flake, quarantine, re-verify, or file-a-bug

> Standing convention owned by **validation-net** (VN11, `hk-s2psr`, folding in two earlier flake-policy threads whose ids no longer resolve). When a test is red or intermittently red, classify it into exactly one of four categories below, then act. The §Coverage enforcement rule "No skipped tests in main" still holds — quarantine is `t.Skip` *under `-short` only*, never a silent disable, and never a delete.

The decision tree, in order. Stop at the first branch that matches:

1. **Is the disk/environment unhealthy?** → **RE-VERIFY (no change).**
2. **Is it a fast unit/integration test that flakes on contention, a data race, or a too-tight timeout?** → **DE-FLAKE (fix root cause).**
3. **Is it a legitimately slow real-daemon / real-binary E2E that doesn't belong in the fast per-bead gate?** → **QUARANTINE from `-short`** (still runs in full CI).
4. **Did the flake expose a genuine product/infra bug?** → **FILE A BUG** (the test stays; never silently disabled).

### 1. Re-verify (no change) — the red was environment-induced

The test is correct and the product is correct; the red came from the *machine*, not the code. Confirm environment health **first**, before touching any test.

- **Canonical signal:** real-daemon / worktree-boot tests time out under disk starvation. `hk-3hf9n` — eight tests including `TestQueueDispatch_HappyPath` timed out at 94% disk (ENOSPC); on a healthy disk they are deterministically green.
- **How to confirm:** `df -h /System/Volumes/Data` (or the test's tempdir mount). Check for ENOSPC in the test log, OOM-kill in `dmesg`, or a since-retired CI machine.
- **Action:** re-run on a healthy machine. **Do NOT quarantine or "fix" an env-induced red** — there is no defect to fix, and quarantining hides a real test. If the environment failure is recurring infrastructure (disk fills routinely), file an *infra* bug against the environment, not the test.
- **To reproduce a red under load, use `scripts/loadgen.sh` — do not hand-roll a load generator.** Example: `scripts/loadgen.sh --workers 12 -- go test ./internal/daemon -run TestX -race`. The load stops when the command returns, and every spinner also carries its own deadline, so it dies on its own even if the shell is killed. Hand-rolled generators have leaked twice, both times through `LOADPIDS=$(jobs -p); …; kill $LOADPIDS 2>/dev/null`: the agent shell is zsh, `jobs -p` is empty inside command substitution there, the redirect hides the failed `kill`, and the success line prints anyway. The second leak ran 22 hours at load average 27. Run `make leakcheck` after any test run that spawned helpers. `make leakreap` clears the orphaned shells and test binaries it reports and nothing else — a busy orphan of an unmodelled shape, or a live parent with a runaway child pool, is reported and left alone for you to read, and the run still exits non-zero while one stands.

### 2. De-flake — fix the root cause (PREFERRED for fast tests)

The flake is a fixable test-harness defect: shared-state / lock contention, a data race, or an unrealistic timeout. **Prefer fixing over quarantining** — these tests belong in the fast per-bead gate, and the fix removes the flake for good.

- **Shared-state / lock contention** → isolate per-test state. Canonical fix: `~/.claude.json` trust-lock contention resolved via per-test config isolation — a `TestMain` that points `HARMONIK_CLAUDE_CONFIG_PATH` at a temp config so concurrent tests don't fight over one global file.
- **Data race** → remove the race, don't paper over it. Same lane: a real data race was removed by dropping `t.Parallel()` on tests that mutate package-level vars the production path reads.
- **Too-tight timeout** → wait on a milestone, not a duration. Bump the number only when there is nothing to wait on. A duration is a bet against the machine, and the machine wins whenever a neighbouring lane gets busy, so a bigger number buys a quieter failure rather than a fixed one. Where the test can observe the thing it is waiting for — an event written, a queue cleared, a status reached — wait for that and the budget stops mattering. `TestT6_10BeadSequentialDrain` was repaired this way on 2026-08-07: it polled the bead ledger, asserted on the event log, and cancelled the daemon in between, so it was fixed by waiting for the events rather than by widening a window. Same lane, earlier and weaker: a `100ms` timeout that lost races on a loaded box was simply bumped.
- **Concurrent-scenario non-determinism** → apply the four-element recipe in §Scenario fixture determinism recipe (no-commit guard, merge mutex, phase-aware twin, skip flags) before concluding a scenario test is "inherently flaky."
- **Action:** land the fix with a `Refs:` to the flake bead. The test stays in `-short` / the per-bead gate.

### 3. Quarantine — exclude from the fast per-bead gate ONLY

The test is a *legitimate* slow real-daemon E2E (multi-second socket waits, real review loops, strict cross-goroutine event ordering) that is too non-deterministic for a fast merge gate but is still valuable in full CI. Quarantine moves it out of `-short`; it **still runs** in the full CI / Tier-3 lane.

- **Canonical mechanism:** call the shared guard `skipRealDaemonE2EInShort(t)` at the top of the test (defined in `internal/daemon/shortskip_test.go`; 57 call sites across 31 files already use it). It `t.Skip`s only when `testing.Short()`, so the test is out of `make fast` and out of the whole-repo step of `make full`, and it still runs in **`make core`** (which passes no `-short`), in the tagged scenario tier, and in `make test-race-nightly`.
- **Before you reach for it, know what it costs.** Guarding a test here takes it out of the inner loop, not out of the build. Corrected 2026-08-07 (`hk-od9d4`): this section used to say the guard was a *temporary* shelving with an owning un-shelve bead, `hk-p258q`. That id resolves to nothing today, so nothing was ever going to un-shelve those tests, and for a time `make core` — the gate an assessor sign-off rests on — passed `-short` too and skipped all 45 of them, including the two end-to-end tests that prove a bead goes through the queue. The guard is now a standing decision about the inner loop, and `make core` is what runs the guarded set.
- **Canonical example:** three real-daemon review-loop bridge tests (e.g. `TestReviewLoopBridge_CHB009_ReviewerAlwaysMintsFresh`) intermittently failed under `-short` and could flake the per-bead gate for `internal/daemon` beads, so they went behind the guard. This example used to cite an id that resolves to nothing today, so the id is dropped and the description kept. Review-loop mode has since been retired, so the tests it names are gone too.
- **Hard limits:**
  - Quarantine = move out of `-short` **only**. Never `t.Skip` unconditionally, never delete, never `//nolint`-away the suite.
  - **Never quarantine a fast unit test.** A fast test that flakes has a fixable root cause (category 2) — fix it; do not hide it.
  - **A guard that names a bead must name one that resolves.** Check with `br show <id>` before you write the id, and again if you copy an existing guard's wording. A skip whose reason is an id that answers nothing reads like a plan and is not one.

    **Search before you call an id dead, and say "does not resolve", not "never existed".** Two things make the fast answer wrong. `br list` shows OPEN issues only — this ledger held 260 open against 3,384 closed on 2026-08-07 — so an id absent from `br list` may simply be closed. And the id may be quoted inside another bead's text rather than being its own record.

    Three ids in this file were checked across open and closed on 2026-08-07 and none resolves: `hk-p258q`, `hk-6ra3p`, `hk-1o0cc`. **They still named real work, and the proof is in git, which is the only part of this that travels:**

    ```
    71fafcbe0  2026-06-10  test(hk-p258q): fix macOS sun_path overflow in operator-pause socket fixture
    d61dec994  2026-06-10  test(hk-6ra3p): exclude real-daemon review-loop bridge tests from -short gate
    43f31068f  2026-06-10  test(hk-1o0cc): de-flake -short reds via ~/.claude.json isolation
    ```

    Each subject names its own bead, one to one, so the ids were dropped from the ledger rather than invented. Reach for `git log --oneline --all --grep=<id>` before you conclude anything about an id, and note that this evidence is durable where a bead body is not.

### A copied guard drifts from its use — the macOS `sun_path` case

Worth reading in full, because the shape recurs and neither file shows it alone.

A Unix-domain socket path cannot exceed 104 bytes. Three fixtures here guard against that, and the guard was copied between them. Checked in the source on 2026-08-07:

| Guard | Does a caller transform the value after the check? | State |
| --- | --- | --- |
| `internal/daemon/socket_test.go` `socketFixtureTempSockPath` | no | sound |
| `internal/daemon/daemon_test.go` `TestDaemonStart_BindsSocket`, inline | no | sound |
| `test/scenario/harness_test.go` `scenarioFixtureProjectDir` | **yes** | broken by 8 bytes |

The third one:

- `scenarioFixtureProjectDir` measures the **unresolved** `t.TempDir()` against the 104-byte limit and falls back to a short `/tmp` path when it does not fit.
- `test/scenario/event_payload_start_v2_t9_test.go` then calls `filepath.EvalSymlinks` on the directory the guard already approved. On macOS that rewrites `/var` to `/private/var` and adds exactly 8 bytes. The socket path is built from the **resolved** result.
- So the guard approves a path 8 bytes shorter than the one the daemon is handed. It surfaces as `isolated daemon socket did not start` (`hk-sunpath-guard-unresolved-y2gwy`).

It is latent, not live: it fires only when the unresolved path lands in the 8-byte window where the guard passes and the resolved path does not. Deterministic inside that window.

**The copy did not arrive broken.** It went wrong later, when somebody added a transformation *downstream of a check that had already passed*, in a different file from the guard. So when you see a validation helper copied into several fixtures, the question is not "is the helper right" — it is **"does any caller change the value between the check and the use"**. Nobody reading either file on its own would see this one.

That is also why it survived two searches. A reviewer read `internal/daemon/daemon.go`, `internal/lifecycle/daemonpaths.go` and `internal/daemon/socket_test.go`, correctly established that the daemon resolves nothing before binding, and concluded the claim was unproven. **That finding was true.** The resolution happens in the fixture. Verification is not "did somebody check" — it is "did somebody check where the seam is".

    **Expect this, because the ledger is machine-local and gitignored** (see `AGENTS.md` §Issue tracking). An id in a comment or a doc points into a database that does not travel with the repo, so it can be live where it was written and dead everywhere else. So an id is a **breadcrumb, never the explanation**. Write what broke and what the fix has to satisfy in the text itself, and let the id be the extra. A skip whose only justification is an id is one clone away from justifying nothing.

    **And put the ASSERTION TEXT in the title, not just the test name.** One defect here was filed three separate times in ten days for want of it. The original title was *"Load-sensitive flake: TestReconcileOrphanedMarkersFailsafeWakes"*. Note that it already carried the test name and a category — what it lacked was the string a searcher is actually holding, which reads *"orphan marker still present after failsafe wake"*. Nobody searching that message landed on the bead, so it was filed again on 2026-07-30 and again on 2026-08-07, and both re-filings were closed as duplicates. The second re-filing was titled `reconcile-orphan-marker-flake`, which is a category too, so it would have been just as unfindable.

    So the durable handle is the **test name plus the assertion text**, and both go in the title. Ledger ids (`hk-zt68b`, and its duplicates `hk-vw0h8` and `hk-reconcile-orphan-marker-flake-svxbw`) are a convenience, and will travel no further than the three ids above did.

### 4. File a bug — don't quarantine-and-forget

The flake is the messenger for a **genuine product or infrastructure defect**. The test is doing its job; the bug is real.

- **Canonical signals:** `hk-gq3my` (a `git worktree add` metadata race under concurrency — a real product race the test surfaced); `hk-i0hor` + `hk-numyh` (genuine daemon bugs); `hk-5pwv5` (residual `internal/daemon` reds whose root cause is product behavior, not test timing).
- **Action:** file a tracked bug bead **with a repro** (per `build-practices.md §Bug fixes require a reproducing scenario test`, the fix lands with a reproducing scenario test). The test **stays** — either red-and-tracked, or `t.Skip` with the bug ID in the skip message and a `// TODO <bead-id>` so the skip is never silent. It is **never** disabled without a tracking bead.

### Classification at a glance

| Symptom | Category | Action | Canonical refs |
|---|---|---|---|
| ENOSPC / OOM / disk-starvation; green on healthy box | Re-verify | Confirm env health first; do not touch test | `hk-3hf9n` |
| Shared global file / lock contention | De-flake | Per-test config isolation (`TestMain` + temp path) | — |
| `-race` data race; package-level var mutated under `t.Parallel()` | De-flake | Remove the race (drop parallel, or guard the var) | — |
| Too-tight timeout loses on a loaded box | De-flake | Wait on a milestone, not a duration. Bump the number only when there is nothing to wait on | `hk-fr7ht`, `hk-4f1bs` |
| Slow real-daemon / socket / review-loop E2E flakes the fast gate | Quarantine | `skipRealDaemonE2EInShort(t)` — out of `-short` only, and still runs in `make core` | `hk-od9d4` |
| Flake reveals a real product/infra race or daemon bug | File a bug | Tracked bead + repro; test stays | `hk-gq3my`, `hk-i0hor`, `hk-numyh`, `hk-5pwv5` |

**Anti-patterns (forbidden):** deleting a flaky test; `t.Skip`ing it unconditionally with no owning bead; quarantining a fast unit test instead of fixing its root cause; quarantining an env-induced red instead of fixing the environment; bumping a timeout to mask a real product slowness (that's category 4, not category 2).

## Test infrastructure conventions

- **No external services in CI.** Beads SQLite is embedded; event log is a tempdir; workspaces are tempdir worktrees. CI runs do not touch the network.
- **Seeded determinism.** Every test that uses randomness accepts a seed. CI uses the committed seed; nightly uses random seeds for shake-out.
- **Test data as code.** Scenarios, policies, and workflow DOTs used in tests live under `testdata/` in their owning subsystem. Not shared across subsystems unless explicitly named as a shared fixture.
- **Clock injection.** Anything that consults the clock takes a `Clock` interface. Tests use a `FakeClock`.
- **Twin invocation uniform.** Handler code launches whatever binary the workflow/policy specifies. No code branches on "is this a twin?"

## Coverage enforcement

- **CI gate.** The unit + integration + scenario suite passes on every push; the crash-recovery fast subset passes on every push. Nightly: full property suite, full crash-recovery suite, conformance suite (once implemented).
- **Coverage thresholds — the authority is `scripts/coverage-gate.sh`, not this document.** It enforces a
  90.0% floor, 95.0% for core packages, and a 0.3pp regression cap against the checked-in
  `coverage.baseline`; `scripts/cmd-coverage-gate.sh` ratchets `cmd/**`. Both run from `make check`.
  **CI does not run either** — CI runs `make full`. Do not restate the numbers here; they drift.
  (This bullet previously said "80% line … CI fails the merge if thresholds regress." Both halves were
  false: the wrong number, and a CI gate that does not exist.)
- **A skipped test is a lie about coverage** — fix it, delete it, or put it behind a build tag. The one
  exception is the flake-policy quarantine described above; a quarantined test must carry the bead ID
  that will remove it.

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
