# Harmonik Foundation — Load-Bearing Positions

> The foundation plan (docs/foundation/components.md + problem-space.md) commits to the positions below. Scan, react. Anything you dispute is a flag to adjust or defer.

## Architectural frame

1. **Deterministic skeleton, probabilistic organs** — A deterministic Go daemon owns workflow state, routing, and dispatch; all cognition lives in agent subprocesses. (architecture §1.1, §1.8)
2. **Centralized controller** — Agent-to-agent coordination routes through the daemon; no file-based handoff or ad hoc IPC between agents. Explicit inverse of Gas Town polecats/mayors. (architecture §1.8)
3. **ZFC classification test** — Every evaluation point is tagged `mechanism` or `cognition`; cognition-tagged points must name a delegation path. Framework-level semantic judgment is banned. (architecture §1.2)
4. **Four-axis determinism tagging** — Every cross-subsystem type/interface/evaluation point is tagged on LLM-freedom, I/O determinism, replay-safety, idempotency. (architecture §1.1, SC-10)
5. **Search + verifier + traces required triple** — All three must exist; verification is a node type (not a subsystem), traces are durable AlphaGo decision records distinct from events. (architecture §1.3)
6. **Three-artifact separation** — `spec`, `workflow graph`, `bead` are three distinct artifacts; none is a projection of another; "feature" is NOT a product primitive. (architecture §1.9)

## Execution model

1. **DOT for workflows, YAML for policies, JSONL for observational events** — Three serialization formats, non-overlapping responsibilities. DOT attributes reference YAML by name. (execution-model §2.1)
2. **Run = one workflow execution against one input** — Replaces ambiguous "task"/"cycle"/"work item." A bead may produce multiple runs across its lifetime. (execution-model §2.1, §2.5)
3. **Git checkpoint trail is the state-reconstruction source** — JSONL is observational, never replayed for state. Every durable transition commits; git always knows last durable state. (execution-model §2.1a, §2.2)
4. **Transition records live as sibling files** — Canonical path `.harmonik/transitions/<transition_id>.json` in the checkpoint commit tree; commit trailers are a cheap index. `transition_event` in JSONL is a projection, not authoritative. (execution-model §2.1b, §2.2)
5. **Per-node idempotency_class tag drives reconciliation** — Nodes declare `idempotent` / `non-idempotent` / `recoverable-non-idempotent`; reconciliation reads the tag to classify crashed nodes. (execution-model §2.1c)
6. **Failure commits NOT required for MVH** — Failure events record failures; only successful durable states commit. Revisit if improvement loop needs `git bisect` over failures. (execution-model §2.1a, reconciliation §9.7)

## External integrations

1. **Beads (SQLite, `Dicklesworthstone/beads_rust`) is the task ledger** — Adopted wholesale; harmonik layers workflow orchestration on top. Pre-1.0 risk absorbed via a thin `br`-CLI adapter. (beads-integration §10.1, §10.8)
2. **`br` CLI only — no MCP server** — All Beads interactions go through the CLI; `br serve` is explicitly rejected. Agents get a Beads-CLI skill via handler-contract skill injection. (beads-integration §10.2, §10.9)
3. **Harmonik writes to Beads only at terminal transitions** — claim / close / reopen. Intra-run state never thrashes Beads. Writes are idempotent via an on-disk intent log + derived keys. (beads-integration §10.4, §10.8a)
4. **No DTW adoption** — Temporal / Restate / DBOS are references only. Applicability conditions: single-machine, cheap re-execution, no irreversible side effects, no multi-day waits. Revisiting requires a foundation amendment. (problem-space locked decision #12)
5. **Digital twins are separate binaries** — Real and twin handlers implement the same Go interface, same wire protocol; selection is config-level; the runner carries zero test-mode branches. (handler-contract §4.8)
6. **ntm's role is bounded to process/tmux** — Agent spawning, ready/rate-limit signals, account rotation. Pipelines, SwarmPlan, checkpoint/recovery, and Agent Mail are explicitly NOT consumed. (process-lifecycle §8.7)

## Operator surface

1. **Per-project daemon** — Socket + pidfile at `.harmonik/`. One daemon per project; multiple projects = multiple daemons. (process-lifecycle §8.1)
2. **Headless daemon, separable attach UI** — `harmonik daemon` runs headless; `harmonik attach` opens an observability TUI; `harmonik runner` is solo-dev sugar. Multiple simultaneous attaches supported. (process-lifecycle §8.3)
3. **Operator controls operate between tasks** — Pause/upgrade complete in-flight runs before taking effect; only `stop --immediate` aborts. Reconciliation workflows carve-out: pause is queued during `reconciling`. (operator-nfr §7.3)
4. **Daemon vs. orchestrator-agent distinction** — Daemon is deterministic Go with no LLM calls. Orchestrator-agents are separate Claude Code sessions that drive the daemon via CLI. Never share process space. (process-lifecycle §8.6)
5. **Foundation specifies operator-control semantics; CLI surface is a separate spec** — Q-F1 resolved: semantics in, surface out. (problem-space §Non-goals)

## Non-functional requirements

1. **N-1 compatibility window** — Event schemas, checkpoint format, queue (Beads schema + harmonik overlay) are all N-1 readable. Breaking changes require a migration release at an operator pause. (event-model §3.5, operator-nfr §7.4–§7.6)
2. **Restart RTO target: 30s p95, 300s hard ceiling** — Measured SIGTERM → daemon `ready`. Reconstruction proportional to git-walk depth + Beads query latency; JSONL count is not a factor. (operator-nfr §7.8)
3. **Commit-hash check as MVH integrity gate** — Handler binaries are launched from a known repo-relative path; commit-hash check for in-repo binaries. Full binary signing deferred post-MVH. (handler-contract §4.10, operator-nfr §7.2)
4. **Secrets redaction is mechanism-tagged and enforced pre-emission** — Prefix regex + per-handler patterns + compile-time payload-schema check. Secrets never appear in event log or unredacted session log. (handler-contract §4.7)
5. **Fsync at run-boundaries and checkpoint_written; timer-flush optional** — Event-loss window = events since last fsync-point. Producers MUST emit idempotent events so loss-and-replay is safe. (event-model §3.4)
6. **Structured JSON logs replace distributed tracing for MVH** — Every subsystem emits typed events + structured logs. Prom/OTel wire formats are post-MVH. Multi-tenancy deferred. (operator-nfr §7.1, §7.10)

## State / durability

1. **Three-store model, each authoritative in its own domain** — git = completion + work product; Beads = bead content + typed edges + coarse status; JSONL = observational event sequence. Not caches of each other. (execution-model §2.6)
2. **Git wins on completion disagreement** — Beads `closed` without merge commit → flag, not completion. Investigator agent resolves; daemon does NOT silently auto-reconcile. (execution-model §2.6, beads-integration §10.7)
3. **Three-level branching: node commits → task branch → integration branch → main** — One commit per task on the integration branch. Small-scope changes collapse integration. Main merge style is developer choice. (workspace-model §5.8)
4. **Worktree leased by the RUN, not by agents** — Multiple agents operate sequentially in the same worktree across a run's lifetime; merge agent operates in the SAME worktree the implementer used. (workspace-model §5.1, §5.4)
5. **Reconciliation-as-workflow** — No reconciliation subsystem. Each category's recovery is a DOT workflow in the workflow library. Same primitives execute reconciliation and ordinary work. (reconciliation §9.1)
6. **Six-category reconciliation taxonomy + §9.2a action-mapping** — Detection stays 6-way (Cat 0/1/2/3/3a/3b/3c/4/5/6a/6b); action layer maps to auto-resolve / investigator / escalate. Settled 2026-04-24; not up for re-audit. (reconciliation §9.2, §9.2a)

## What the foundation deliberately does NOT commit to

- **Subsystem internals** — S01–S09 state machines, JSONL layout, CASS integration are their own spec works.
- **Operator CLI surface** — Flags, API shape, dashboard UI defer to a separate spec work; semantics are in foundation.
- **Binary signing** — Commit-hash check is MVH; full signing post-MVH.
- **Metrics exposition format** — Internal emission is in foundation; Prom/OTel external scrape defers.
- **Multi-tenancy / per-tenant cost attribution** — Per-project daemon is the only MVH answer. Shared LLM budgets, shared skill registries, shared operator identity acknowledged as real post-MVH concerns.
- **Multi-repo workflows, distributed tracing, i18n, PII handling** — Out of scope until triggering conditions appear.
- **Workflow library** — Example workflows, "self-build workflow," scenario examples come after foundation.
- **Failure-commits for `git bisect`** — Design slot; add only if improvement loop later needs it.

## ⚑ Emerged positions worth the user's eye

These came out of round-2 amendments and subagent decisions; the user did not personally adjudicate them.

1. **⚑ Investigator-agent wall-clock budget with default-verdict on exhaustion** — Every reconciliation workflow carries a mandatory wall-clock ceiling; exhaustion → default `escalate-to-human` verdict, no reconciliation commit written. Suggested defaults: Cat 2 → 600s, Cat 3 → 300s, Cat 6 → 900s. (reconciliation §9.4a)
2. **⚑ Snapshot-token binding + stale-verdict refusal** — Investigators read inputs at a captured `(git_head_hash, beads_audit_entry_id)`; before executing a verdict the daemon re-checks and refuses if the target run's state has advanced. Re-dispatches fresh reconciliation on staleness. (reconciliation §9.4b)
3. **⚑ Session-log pipeline ownership split S04 → S06 → S08** — S04 emits session log + `session_log_location` event; S06 creates directory + metadata sidecar before `workspace_leased`; S08 (CASS) ingests via filesystem watcher. Three-subsystem contract pinned. (workspace-model §5.3a)
4. **⚑ Handler skill-injection as a generalized obligation** — The handler MUST provision declared skills before the agent begins work; fail-launch on resolution failure; `skills_provisioned` event names what was installed. Beads-CLI is the motivating instance but the pattern is general. (handler-contract §4.11, control-points §6.11)
5. **⚑ Startup orphan sweep before reconciliation** — Daemon kills stale tmux sessions, clears stale worktree locks, kills reparented subprocesses, and sweeps `.harmonik/beads-intents/` before any classification runs. (process-lifecycle §8.2 step 1a)
6. **⚑ Verdict execution is a second commit with `Harmonik-Verdict-Executed: true` trailer** — A verdict commit without a verdict-executed commit is Cat 3b; auto-resolver re-attempts idempotent mechanical action. Verdict actions are idempotency-keyed through the `br` adapter. (reconciliation §9.5b)
7. **⚑ Malformed-verdict path converts cognitive failure to deterministic escalation** — Verdict schema is strict enum + typed fields; any deviation → `reconciliation_verdict_malformed` event + fallback `escalate-to-human`; daemon never interprets malformed payloads. (reconciliation §9.5a)
8. **⚑ Two-operator concurrent-attach silence** — Multiple simultaneous attaches are allowed; concurrent operator actions (two people issuing `pause` at once) have no specified arbitration. (process-lifecycle §8.3)
