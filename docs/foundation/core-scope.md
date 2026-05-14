# Core Scope — Alignment Log

> Running record of alignment decisions from the section-by-section walkthrough that started 2026-04-24. NOT a spec. When specs get written, these decisions feed in.

## Ground rules

- **Recovery / reconciliation is non-core for MVH.** Can be stubbed (crash = restart, no investigator needed). The foundation work previously over-weighted this area; specs for it come later.
- **Pi handler is post-MVH.** Claude Code + twin is enough to prove the pattern.
- **Adze not in foundation.** MVH workspaces are `git worktree add <subfolder>`. No provisioning layer.
- **Dogfood the process.** Harmonik's declared pipeline (idea → spec → review → decompose → tasks → beads → implement) is the process we use for building harmonik itself. No shortcuts to code.
- **Attractor adoption by default.** Harmonik uses Attractor's DOT spec + composition model verbatim where possible. Divergences are explicit and named.

## Section 1 — Workflow composition (aligned 2026-04-24)

**What a workflow is.** Directed graph authored as `.dot` file, in a workflow library. Node types (five, exclusive): `agentic`, `non-agentic`, `gate`, `control-point`, `sub-workflow`. Edges have optional policy-expression conditions.

**Composition.** ONLY via sub-workflow nodes. No inheritance, no runtime rewriting. Parent references sub-workflow by name; expands in place at runtime.

**Run model.** One run per whole workflow execution including nested sub-workflows. Sub-workflow does NOT spawn a child run. Parent's checkpoint trail covers the nested execution.

**Immutability at runtime.** No runtime edge rewriting or node insertion. Revision = edge back to earlier node, not graph mutation.

**Input.** One run = one input = one bead. No multi-bead workflows in MVH.

**Cycles.** Allowed (for revision loops). Bounded by per-edge traversal caps.

**Conditionals.** Edge conditions (policy expressions on edges). `control-point` nodes also select edges via evaluator output. No separate "if node" type.

**Authoring.** Human-editable DOT, reviewed in PRs. NL→DOT ingestion is post-MVH.

### Pre-run validation is first-class

Before any node executes:
1. DOT parseability.
2. Sub-workflow resolution, transitive. Every `sub-workflow` ref resolves; each resolved sub-workflow is validated recursively.
3. Policy / handler / skill reference resolution.
4. Attribute type check (`idempotency_class` from enum, timeouts positive, required attrs present).
5. Reachability — every node reachable from start; every node can reach a terminal.
6. Cycle bound check — every cycle has a declared traversal cap.

Any failure = validation error, workflow does NOT start. Agents generating DOT run validation before submitting.

### Sub-workflow lifecycle events

`sub_workflow_entered` / `sub_workflow_exited` carrying parent `run_id` + sub-workflow name. Logs and observability track nesting.

### Deferred to when specs get written

- **Sub-workflow output/error propagation.** Adopt Attractor's semantics verbatim (state propagation, output payload, error surfacing).
- **Node input/output contract.** Adopt Attractor's `Outcome.context_updates` + shared context model verbatim.

### Deferred post-MVH

- **Fan-in / parallel branches within a workflow.** Sequential workflows don't need fan-in. When a workflow needs parallel branches with convergence, revisit: Kilroy's fast-forward-only (simpler) vs Gas Town's merge-based (richer). No pre-commitment.
- **NL → DOT generation.** Kilroy supports this; not in MVH scope.

### Forward-looking notes

- **Queue-as-workflow would require parallel/merge.** If the task queue (a graph of tasks with dependencies) is ever represented as a workflow itself, fan-in semantics become load-bearing. That's the trigger condition for revisiting the fan-in deferral.

### Recon references

- Attractor: `.kerf/recon/attractor-findings.md`
- Kilroy: `.kerf/recon/kilroy-findings.md`

## Section 2 — Event + checkpoint model (aligned 2026-04-24)

**Two separate things, one substrate.**

**Checkpoints (git commits)** = durable state. Every successful durable state transition produces one git commit with structured trailers (`Harmonik-Run-ID`, `Harmonik-State-ID`, `Harmonik-Transition-ID`, `Harmonik-Bead-ID` optional, `Harmonik-Schema-Version`) + a sibling file at `.harmonik/transitions/<transition_id>.json` in the commit's tree with the full transition record. Commit trail = authoritative run history. State reconstruction reads git + sibling files; no JSONL involved.

**Events (JSONL)** = observational stream. Subsystems emit typed events to an in-process pub/sub bus; bus persists to single JSONL file per project. Events carry `run_id` + optional `bead_id` + typed payload. ~35+ event types. Never replayed to reconstruct state.

**Structured logs** = diagnostic channel. SEPARATE from events. Text log file (e.g., `.harmonik/logs/daemon.log`) for errors, warnings, panics, stack traces. Fsynced aggressively. This is where "why did it crash" lives.

**Three durability envelopes:**
- Git = authoritative WHAT (state).
- Structured logs = durable WHY (diagnostics).
- JSONL events = lossy-tail business stream (observability).

**Durability rules:**
- Only SUCCESSFUL durable state transitions produce commits. Failures emit events and logs, don't commit. (Revisit if improvement loop ever needs `git bisect` over failures.)
- Fsync events at run boundaries + every `checkpoint_written`.
- Events emit async; loss between fsyncs is acceptable because git is authoritative.
- Events are emitted idempotently so loss-and-replay is safe.
- Checkpoints are immutable; sibling file + commit land atomically via `git commit`.
- Panic handlers (Go `defer`+`recover`) flush logs + best-effort flush event bus before process exit. `daemon_panic` log entry with stack trace.
- OS-level crashes bypass handlers; diagnostics rely on log file's aggressive fsync + OS crash logs + post-restart reconciliation.

**Schema versioning:** Every event type + checkpoint carries `schema_version`. N-1 compatibility across releases. Breaking changes = migration release.

### Deferred / follow-up

- **JSONL rotation policy.** Unbounded append works for MVH dev-machine use; must be addressed before any production use. Options: size-triggered rotation, age-triggered, sidecar index (SQLite/DuckDB). Not MVH-blocking.

## Section 3 — Workspace model (aligned 2026-04-24)

**Per-run worktree.** Each run gets its own git worktree via `git worktree add <root>/<run_id>` on a fresh branch off the integration branch. No dependency provisioning (no adze). Worktree is a plain subfolder.

**Lease per RUN, not per agent.** Multiple agents in a run (planner → researcher → builder → reviewer → merger) operate sequentially in the same worktree. One active agent at a time.

**Worktree lifecycle.** Created at run start (`workspace_leased` event); active for duration of run; terminates on successful merge back to integration OR failure with branch preserved for audit; cleaned up after terminal state.

**Session logs.** Each agent's session log at well-known subpath inside worktree (e.g., `.harmonik/sessions/<agent_id>.log`). Committed with work product; CASS indexes post-hoc.

**Branching.** Node commits → run's task branch → integration branch (one commit per task) → main. Small-scope changes may collapse integration. Developer owns main-merge style.

**Conventions set:**
- Worktree root: `<repo>/.harmonik/worktrees/<run_id>/` (default; implementation detail, stop asking).
- Branch name: `run/<run_id>`. Integration branch fixed name (`harmonik/integration` or project-configurable).
- Merge-conflict resolver: ORIGINAL IMPLEMENTER agent (not a dedicated merge-agent type). Human escalation only as last resort. (This was an open foundation question — "workspace conflict resolution role" — resolved for MVH here.)
- Failed-run worktrees persist until operator cleans up. Beyond startup orphan sweep, no auto-cleanup.
- One run per bead at a time (Beads atomic-claim enforces).

## Section 4 — Handler contract (aligned 2026-04-24)

**What a handler is.** Go interface abstracting agent spawning, monitoring, cleanup. Daemon doesn't know Claude Code from Pi from a twin — it calls the handler interface. One handler implementation per agent type.

**MVH handlers:** `claude-code`, `claude-twin`. Post-MVH: `pi`, `pi-twin`.

**Interface surface (conceptual):**
- `Launch(LaunchSpec) → Session` spawns the agent subprocess.
- Daemon owns a watcher goroutine per session, reading handler-managed I/O and publishing events.
- Handler provides an adapter — per-agent-type callbacks for ready-state, rate-limit, clean-exit, account rotation.
- `agent_type` identifier: URN-like string, e.g., `harmonik.agent.claude-code`.

**LaunchSpec fields:** `run_id`, `workflow_id`, `node_id`, `agent_type`, `workspace_path`, `required_skills[]`, `skill_search_paths[]`, `bead_id` (optional), `timeout`, `budget`, `freedom_profile_ref`.

**Skill injection.** Handler MUST provision declared skills before agent starts. Fail-launch on resolution failure. `skills_provisioned` event names what got installed. Beads-CLI is first motivating instance; pattern is general.

**Session lifecycle events.** `agent_started`, `agent_output_chunk`, `agent_completed`/`agent_failed`, `agent_rate_limited`/`agent_rate_limit_cleared`.

**Goroutine ownership split.** Daemon owns watcher goroutine per session; handler subsystem (S04) owns per-agent-type adapter as non-goroutine callbacks. Adding a new agent type = write an adapter.

**Twin equivalence.** Real and twin handlers implement SAME interface, SAME wire protocol. Selection at workflow/policy config (DOT `handler_ref` attribute). Daemon has zero test-mode branches.

**Session-log pipeline (round 2).** S04 emits log + `session_log_location` event; S06 pre-creates directory + metadata sidecar before `workspace_leased`; S08 ingests via filesystem watcher. Three-subsystem contract.

**Conventions set:**
- Handlers compiled-in (not plugins). Adding a handler = code + recompile.
- Daemon ↔ handler in-process Go (`Launch` is a function call).
- Agent ↔ daemon out-of-process (child subprocess; transport is handler's choice — pipe, socket, stdout capture).
- Skill resolution mechanism-tagged (deterministic given skill name).
- Session-log format is handler-specific (no unified schema across agent types).
- Rate-limit handling in adapter; daemon policy = exponential backoff within wall-clock budget.

### Testing strategy for handlers (MVH)

**Tier 1 — CI-mandatory, every commit:**
- Unit tests on handler interface (output parsing, lifecycle signals, rate-limit detection).
- Twin-based scenario tests (token-free, deterministic).
- Recorded-fixture tests for claude-code handler: canned real-agent output replayed in tests, validates handler interprets real wire-format correctly. Fixtures manually refreshed when Claude Code changes format.

**Tier 2 — Budget-capped smoke tests, nightly/pre-release:**
- Small set of end-to-end workflows against real claude-code. Hard $ cap.
- Same workflows also run through claude-twin; divergences flag drift candidates.

**Post-MVH:** twin-conformance suite (automated periodic real-vs-twin comparison with tolerance alerts).

### Deferred / follow-up

- **Handler-divergence testing** is both a handler-spec concern and a project-level testing concern. Verify `docs/methodology/TESTING.md`'s 5 layers cover handler-divergence explicitly; revise if not. Item for project-level testing section.

## Section 5 — Orchestrator loop (aligned 2026-04-24)

**The daemon's main loop:**

```
startup: check for in-flight runs (light reconciliation), resume or clean
loop forever:
  eligible_beads = beads.Query(status=open, dependencies_met, no_pending_gate)
  if empty: idle until a new bead arrives
  bead = pick_any(eligible_beads)           # oldest is a tiebreaker, not priority
  workflow = resolve_workflow(bead)          # from bead's workflow_name field
  run = create_run(bead, workflow)           # allocate run_id, create worktree
  execute_workflow(run)                      # node-by-node
  on terminal: merge-back + close bead (OR fail-and-reopen)
  check operator signals (pause? stop? upgrade?)
```

**Node execution, at each node:**
1. Dispatch — via handler (agentic), invoke (non-agentic), wait (gate), evaluate (control-point), recurse (sub-workflow).
2. Await completion signal from watcher goroutine.
3. Commit checkpoint if the transition is durable (per §2).
4. Evaluate outgoing edges against node output + run context.
5. Advance to next node, OR reach terminal.

**Merge-back is a final node in the workflow.** A `merge` node (agentic or mechanical) integrates the run's branch to the integration branch. Success → bead closes. Conflict → escalates to original implementer agent (per §3). Workflow terminates either way.

**Operator controls run BETWEEN runs** (locked decision #10). Pause/stop/upgrade complete the current run, don't interrupt. Only `stop --immediate` aborts mid-run.

**Daemon is the sole driver for MVH.** No orchestrator-agent (LLM session) required. Daemon auto-picks eligible beads, dispatches workflows, commits, closes. Orchestrator-agent layer (separate Claude Code session driving daemon via CLI) is optional, post-MVH.

**Conventions set:**
- Concurrency is operator-configurable via `--max-concurrent N`. The default of 1 is a soft default, not a hard cap; multiple eligible beads queue when in-flight count reaches N.
- Bead-to-workflow binding lives on the bead (a `workflow_name` field or typed edge). Daemon resolves to a DOT workflow in the library.
- Bead selection: pick ANY eligible bead; oldest-first is a tiebreaker, not a priority. No prioritization or scoring in MVH.
- `internal/daemon/workloop.go` runs goroutine-per-active-bead up to `MaxConcurrent`. Within a run, one node at a time. Handler I/O is async (watcher goroutine) but each run awaits node completion.
- No workflow-level transactionality. A run that commits 3 nodes and fails on node 4 leaves 3 checkpoints durable; no rollback. State-at-failure preserved in git.

### Deferred / follow-up

- **Operator-configurable concurrency** — `--max-concurrent` flag is live, gated by a claim semaphore in `RunRegistry` (hk-e61c3.3). Follow-ups: per-project cap, machine-level budget coordination across daemons.
- **Bead prioritization** — post-MVH. Options: priority field on bead, orchestrator-agent-driven selection, SLA-based scheduling.

## Section 6 — Subsystem organization (aligned 2026-04-24)

Full recommendation at `docs/foundation/project-level/subsystem-organization.md`. Key alignments:

- **Single Go module, single binary.** Module `github.com/gregberns/harmonik`. Daemon + subcommands at `cmd/harmonik/`; twin binaries separate (`cmd/harmonik-twin-{generic,claude,pi}/`); `harmonik-twin-generic` is the NDJSON back-half test handler; `harmonik-twin-claude` (hk-w5vra.2) will mirror real Claude lifecycle.
- **Subsystems under `internal/`.** Each of S01–S09 plus `handler/{contract,claudecode,pi,twin}`, `adapter/{br,ntm}`, and `daemon` (composition root).
- **Shared types in `internal/core`** (leaf package, imports no subsystem). Types: `RunID`, `StateID`, `TransitionID`, `BeadID`, event envelope + taxonomy, `Outcome`/`Transition`/`Checkpoint`, four-axis tag types.
- **`pkg/` deliberately empty at MVH.** No public library surface.
- **Dependency layering enforced by `go-arch-lint`.** Single YAML at repo root declares components + allowed edges; CI fails on violations naming the specific forbidden edge. Chosen over `depguard` (architecture-as-graph is native to go-arch-lint; depguard scatters rules across linter config).

Conventions set:
- Module path: `github.com/gregberns/harmonik` (confirm before `go mod init`).
- Handlers live under `internal/handler/*` (not under `agentrunner`).
- `internal/daemon` is the composition root; only package allowed to import most subsystems.
- `go-arch-lint` version pinned; `depguard` is the drop-in fallback if unmaintained.

### Deferred / follow-up

- **Sub-package structure inside each subsystem** — owned by each subsystem's own spec work (e.g., `orchestrator/runner`, `orchestrator/router`).
- **Whether to split `internal/core`** into finer-grained shared packages (`ids`, `events`, `outcome`). Cheap refactor later.

## Section 7 — Testing strategy (aligned 2026-04-24)

Full recommendation at `docs/foundation/project-level/testing.md`. Key alignments:

**Libraries (locked, enforced by CI import-allowlist linter):**
- Runner: stdlib `testing`. Assertions: `testify/require` only (no `assert`, no `suite`).
- Property tests: `pgregory.net/rapid`.
- Golden files: `gotest.tools/v3/{golden,fs,icmd}`.
- Mocking: hand-written fakes in `internal/<pkg>/faketest/`. No `gomock`, no `mockery`.
- Banned: testify/suite, testify/mock, Ginkgo, Gomega, gopter, dockertest.

**Five test layers with build tags:** unit (default) / integration / scenario / crash / property.

**Coverage targets (user-endorsed aggressive numbers, revised upward 2026-04-24):**

| Layer / class | Line coverage | Branch / path |
|---|---|---|
| Core subsystems | 95% | 100% of error returns |
| Boundary parsers | 95% | 100% err + fuzz corpus |
| Handler adapters (real) | 90% | recorded fixtures |
| Handler adapters (twin) | 100% | — |
| Utility / glue | 85% | — |
| Overall floor | 90% | CI fails on >0.3% drop vs main |

Coverage-gaming rule: a line marked `// unreachable: <why>` covered by an assert-panic test counts as covered.

**Handler-divergence, three tiers:**
- Tier A (every push): recorded-fixture parse tests (captured claude-code wire output replayed, handler methods asserted to parse into golden structs).
- Tier B (every push): twin-driven scenario tests (daemon↔handler wiring end-to-end, token-free).
- Tier C (nightly): budget-capped real-agent smoke (hard `$5/night` cap; diffs open auto-PR on fixture drift).
- Post-MVH: automated real-vs-twin event-stream diff (twin conformance suite).

**Crash-recovery via `faultpoint` package** behind `//go:build crash`. Named injection sites (`mid-checkpoint-commit`, `after-commit-before-beads-write`, `mid-jsonl-fsync`, etc.). Production builds: faultpoints are no-ops (dead-code-eliminated). Crash builds: `faultpoint.Arm(site, SIGKILL)` causes the next hit to kill the process. Fast 3-site subset per push; full set nightly.

**CI gates (all must pass for merge):** `go vet`, `staticcheck`, `gofmt -l` empty, `go test -race` unit+property <3min, integration <5min, scenario <10min, crash fast subset <2min, coverage gate, import-allowlist linter, no `t.Skip()` in committed tests.

### Deferred / follow-up

- **Twin-conformance automated diff** — Tier C writes captures; full real-vs-twin diff with tolerance is post-MVH.
- **Fuzz corpora for boundary parsers** — continuous fuzz infra post-MVH; committed seed corpus is MVH.
- **Benchmark regression gate** — `benchstat` once RTO-sensitive paths have specs.

## Section 8 — Quality checks (aligned 2026-04-24, post-reviewer-convergence)

Full recommendation at `docs/foundation/project-level/quality-checks.md` (post-reviewer revision). Key alignments:

**Toolchain.** Go 1.25 via `go.mod toolchain go1.25.x` (enables `log/slog`, `testing/synctest`, `os.Root`, `go.mod tool`). `golangci-lint` v2.3+ explicit (v2 schema). `lefthook` as hook manager. `gofumpt` as formatter; `gci` for imports.

**Linters: 30 enabled.** Original 25 plus **`testifylint`, `errchkjson`, `exhaustive`, `containedctx`, `fatcontext`** per Go-ecosystem reviewer. Explicit NO list unchanged.

**`depguard` alone handles component-graph AND linter rules** (per convergence of Go-ecosystem + Skeptic reviewers). `go-arch-lint` dropped; config moves to `.golangci.yml` under `linters-settings.depguard`.

**User-endorsed invariant: every check executable locally AND in CI. Same commands both sides.** No CI-only checks.

**Three-tier identical gauntlet:**
- `make check-fast` (<15s) — pre-commit hook. Delta lint + staged format + vet + build + short unit tests.
- `make check` (~3-5min) — pre-push + WIP verification. Full-tree linters, race tests, coverage gate, govulncheck, go mod tidy diff.
- `make check-full` (~10-15min) — **agent declared-done MUST pass this.** Everything + integration + scenario + fast crash tests.

Excluded from agent done-check (CI-nightly or on-demand): real-agent smoke tests, full property-seed runs, full fault-injection site set, weekly govulncheck deep scan.

**Agent bypass prevention — hardened:**
1. GitHub branch protection on main with required CI status checks. `--no-verify` skips local but CI re-runs.
2. `nolintlint` blocks bulk suppression (`//nolint:all` fails).
3. CI posts nolint-density delta to PR summary.
4. No admin-bypass on main.
5. **Protected rule files** (NEW per Critic): `.golangci.yml`, `.depguard.yml`, `forbid-import.go`, `coverage-gate.sh`, `.github/workflows/*`, `Makefile`, `.lefthook.yml`. Edits trigger automatic `rule-change` PR labeling, require kerf-codename citation in PR description, surface prominently in CI summary. Prevents "agent relaxes gate then passes own gate."

**Error handling.** `errcheck` blocking. Prefer `%w` wrapping at subsystem boundaries; `errorlint` enforces correctness-when-wrapping but can't detect missing-wraps (deferred to custom `go/analysis` pass). Reviewer-agent flags missing-wraps on subsystem-boundary imports until the analyzer ships.

**Logging.** `log/slog` as the structured logger, JSON schema to stdout → `.harmonik/logs/daemon.log`. `fmt.Print*` / `log.Print*` banned outside `main` and tests (`forbidigo` enforces). Subsystem loggers carry subsystem name + `run_id` as default attributes.

### Deferred / follow-up

- **Custom `go/analysis` analyzers** (both needed):
  - Subsystem-boundary-aware wrap enforcement (replaces wishful "agents MUST wrap" rule).
  - AST-level anti-coverage-gaming check: every `Test*` function reaches an assertion on every return path. Blocks assertion-free table-loop tests.
- **`gosec` blocking** (currently advisory) — elevate when daemon opens network ports or handles credentials.
- **Mutation testing** (`gremlins`) — post-MVH once unit suite is substantive.
- **Branch protection with approval gate** — solo-dev: required-approvals slot empty. Revisit when collaborators join.

## Section 9 — Go build practices (aligned 2026-04-24, per user direction)

Full recommendation at `docs/foundation/project-level/build-practices.md` (revised per user direction). Key alignments:

**User direction:** No PRs at MVH or post-MVH until the product has real users. Direct commits to `main`. Agent reviewers do all code review. User reads committed code asynchronously, never gates. Streamline for speed without lowering quality bars.

**Branch model: direct-to-main.**
- `main` is the working branch. Agents commit directly.
- Ephemeral `agent/<codename>` branches allowed ONLY for parked-across-sessions work; squash-merge to main on resume.
- No long-lived feature/topic branches.
- (Unchanged: workspace-model §5.8 three-level branching for WORKFLOW RUNS — that's runtime orchestrator behavior, not build-of-harmonik practice.)

**Agent review on every non-trivial commit (required, not optional).**
- Before commit: agent runs `make check-full` (full local gauntlet) AND invokes `agent-reviewer` against own diff.
- Reviewer checks: spec alignment, idiom compliance, test adequacy, unwanted-abstraction detection, bead/codename match.
- Verdict `BLOCK` → agent fixes before committing (never lands). `REQUEST_CHANGES` → agent addresses or commits with rationale in body. `APPROVE` → commit lands.
- Trivial commits (typo, one-line obvious fix) MAY skip reviewer; still run `make check-full`.
- **Human review is asynchronous after commit** — user reads `git log` + diffs. Never a gate.

**Commit conventions: Conventional Commits, closed type set.** `feat` / `fix` / `refactor` / `test` / `docs` / `chore` / `spec` / `build` / `perf`. Scopes preferred (subsystem ID or package). Subject ≤72 chars, imperative.

**Required trailers when applicable:**
- `Refs: <bead-id|kerf-codename>`
- `Co-Authored-By:` for agent-assisted
- `Reviewed-By: agent-reviewer (APPROVE|REQUEST_CHANGES)` on every non-trivial commit — makes review outcome auditable via `git log`
- `BREAKING CHANGE:` footer for incompatible changes

**Commit body for non-trivial commits:** Why / What / Spec alignment / Test plan / Risk sections (same info the old PR template had, now embedded in the commit).

**Versioning: Semver `0.y.z` pre-1.0.** `y` on breaking changes; `z` on everything else. 1.0 after foundation stable ≥30 days + bootstrap workflow runs end-to-end + N-1 compat honored ≥2 releases.

**Release: tag-triggered on main.** `git tag -s v0.y.z`. CI runs `goreleaser` for the binary matrix.

**CI model without PRs:**
- Pre-commit (local, lefthook): `make check-fast`.
- Agent done-check (local, agent responsibility): `make check-full` + `agent-reviewer`.
- Post-commit CI on main (remote): re-runs `make check-full`; failure surfaces as red main; agent fixes forward.
- Branch protection prevents force-push + admin-bypass but does NOT reject pushes (no PRs = no merge gate).

### Deferred / follow-up

- **PR-based workflow, human review gates, branch protection with required approvals** — restored when the product has real users or multiple human contributors. Currently direct-to-main with agent review.
- **Post-commit CI details** — platform choice (GitHub Actions assumed) to be confirmed at bootstrap.
- **Signed commits** — `git commit -S` as nice-to-have now; revisit signing policy at 1.0.
- **Commit size monitoring** — no hard LOC cap now; add a cap if agents start producing megacommits.

## Section 10 — Agent configuration (aligned 2026-04-24)

Full recommendation at `docs/foundation/project-level/agent-configuration.md` (post-user-direction). Key alignments:

**AGENTS.md canonical; CLAUDE.md is a symlink** (per user direction 2026-04-24). Single source of truth. Repo-root `AGENTS.md` under 120 lines. Per-directory AGENTS.md paired with sibling CLAUDE.md symlinks; created lazily when local rules justify it. Claude-specific content lives in `## Claude-specific` section inside AGENTS.md.

**`CONSTITUTION.md` as non-recursive trust anchor** (per user direction 2026-04-24). Single file listing immutable foundational decisions (three-store model, centralized controller, three-artifact separation, direct-to-main + agent-reviewer-every-commit, deterministic-skeleton + probabilistic-organs, the 10 locked decisions). Edits require `Constitution-Edit-Approved-By: <human>` commit trailer; agents MUST NOT commit edits without it. In Protected rule files; edits surface automatically in CI.

**JSON-structured `agent-reviewer` verdict** (per user direction 2026-04-24). Two commit trailers:
- `Reviewed-By: agent-reviewer` (presence marker)
- `Review-Verdict: {"verdict": "APPROVE|REQUEST_CHANGES", "flags": [...], "notes": "..."}` (JSON, schema-validated pre-commit; unparseable = blocks commit)

Prevents prompt injection; enables audit/metrics. Schema lives in the `agent-reviewer` skill docs; versioned via `schema_version` field.

**Skills at `.claude/skills/` in repo** (load-bearing MVH set): `beads-cli`, `kerf-workflow`, `go-subsystem-add`, `project-quality-gates`, `git-task-commit`, `spec-finalize`, `agent-config-reviewer`, `agent-reviewer` (load-bearing, must not rot).

**Rule categories**: git ops (direct-to-main, agent-reviewer-every-commit), Go procedures (`make check-full` before declared-done), commit style (Conventional Commits + required trailers), commit creation (Why/What/Spec/Test/Risk in body), spec adherence, Protected rule files.

**Memory system**: `project_/feedback_/user_` prefixes; `MEMORY.md` index; new files for durable content only.

**Cross-session continuity**: `SESSION_HANDOFF.md` on session exit if non-trivial work landed.

**Three-tier update cadence**: Tier 1 (session end, self-check + `##Config review` stanza) / Tier 2 (kerf pass advance, `agent-config-reviewer` runs) / Tier 3 (kerf finalize, widest review + new skills registered).

### Deferred / follow-up

- **`agent-reviewer` verdict schema** — owned by the `agent-reviewer` skill; write concretely at bootstrap.
- **CONSTITUTION.md content** — concrete text is authored at bootstrap (small file, but author with care).
- **Hook-based enforcement post-MVH** — Gas Town Hooks for mechanical rule enforcement beyond what linters cover.
- **Pi-specific configuration** — when Pi arrives, `## Pi-specific` section in AGENTS.md + any per-agent skill variants.

---

## All 10 sections complete (2026-04-24)

Five core sections (workflow composition, event+checkpoint, workspace, handler contract, orchestrator loop) + five project-level sections (subsystem organization, testing, quality checks, build practices, agent configuration) aligned with user.

### Next steps (not part of walkthrough)

- Foundation recommendation docs (5 files) at `docs/foundation/project-level/` are post-reviewer-revision.
- Foundation spec corpus (10 components + problem-space) still lives at `docs/foundation/components.md` + `problem-space.md` — not yet recast as normative specs.
- Path to code: write actual specs per component (in `specs/`) following the kerf pipeline: idea → spec → review → decompose → tasks → beads → implement. Dogfood the process harmonik defines.
