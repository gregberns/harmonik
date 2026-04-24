# Operator NFR

```yaml
---
title: Operator NFR and Control Semantics
spec-id: operator-nfr
requirement-prefix: ON
status: draft
spec-shape: requirements-first
version: 0.1.0
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-04-23
depends-on:
  - architecture
  - event-model
  - execution-model
  - handler-contract
  - control-points
  - process-lifecycle
  - reconciliation
  - beads-integration
---
```

## 1. Purpose

This spec defines harmonik's cross-cutting non-functional requirements (observability, reliability, performance, security, cost) and operator-control semantics between tasks. It is the normative home for invariants every subsystem must honor regardless of its internal design: the N-1 compatibility window for event schemas, checkpoint format, and queue; restart RTO targets; the commit-hash integrity gate; secrets-redaction obligations; the pause / stop / upgrade state machine; and the operator-surface obligations (exit-code taxonomy, startup failure-mode catalog, `harmonik upgrade` contract, multi-daemon coordination) that foundation must not silently defer.

This spec owns *semantics* (what must happen between tasks, what must be readable across versions, what must be observable); the concrete CLI flag surface is a separate spec work per [docs/foundation/components.md §7.10].

## 2. Scope

### 2.1 In scope

- Observability envelope: typed events, structured logs, health-check interface, liveness heartbeats, audit-record subset of traces, operator-observable exit codes.
- Security posture: secrets-redaction obligation, command-execution sandbox invariant, network-egress policy, prompt-injection defense, skill-injection policy enforcement, commit-hash integrity gate for pause-to-upgrade.
- Operator-control semantics between tasks: pause / stop / upgrade / improvement-pause state machine, reconciliation carve-out, event emissions per state transition (locked decision #10).
- Queue-format (Beads + harmonik overlay) compatibility contract with N-1 readable window.
- Checkpoint-format and event-schema N-1 compatibility cross-references.
- `harmonik upgrade` contract obligation (binary-source mechanism, hash check, drain interaction, cross-version state contract, socket retry).
- Graceful-shutdown ordering and `stop --immediate` semantics.
- Restart RTO target (30s p95 nominal, 300s hard ceiling) measured SIGTERM → daemon `ready`.
- Resource budgets cross-subsystem (declared in policy, enforced at dispatch, attributed in observability).
- Multi-daemon commands obligation (`harmonik list`, `--socket` / `--cwd` / `--daemon-id` identification, machine-level agent-subprocess ceiling) and multi-tenancy deferral acknowledgment.
- Startup failure-mode catalog obligation.
- Silent-hang detection obligation (named here, specified in [handler-contract.md §4.6]).
- Reconciliation operator override (pre-execution verdict confirmation) cross-reference.
- Exit-code taxonomy obligation and config-inventory obligation.

### 2.2 Out of scope

- Specific CLI flag surface (flag names, subcommand argument order, TUI rendering) — a separate operator-CLI-surface spec work owns this; foundation specifies semantics only.
- Operator dashboard UI — post-MVH; deferred to a future UX spec.
- Full binary signing — post-MVH per §4.2; commit-hash check is the MVH gate per locked decisions (see [handler-contract.md §4.10]).
- Implementation of pause / stop / upgrade state machines inside the orchestrator core — owned by the subsystem spec for S01 (orchestrator core); this spec defines the between-task invariant and the state set.
- Metrics exposition wire format (Prometheus / OpenTelemetry) — post-MVH; structured logs + typed events are the MVH observability surface per §4.9.
- Distributed tracing across multiple harmonik instances — post-MVH; per-project daemon isolation makes multi-instance tracing an OS-process concern per §4.10.
- Per-tenant cost attribution — no multi-tenancy in MVH per §4.10; shared LLM-budget / shared skill-registry / shared operator-identity concerns acknowledged and deferred.
- Observability overhead budget — post-MVH.
- Multi-repo workflow support — post-MVH; MVH operates against one repository at a time per problem-space constraints.
- Reconciliation category classifier internals — owned by [reconciliation.md §9.2, §9.3]; this spec consumes the reconciliation status for pause carve-out only.
- Beads SQLite schema internals — managed upstream; this spec names the overlay-compat contract, not the bead wire schema.

## 3. Glossary

- **task (operator sense)** — one complete run of a workflow, from `run_started` to `run_completed` or `run_failed`. The operator-facing term "task" = the execution-model's `run` (see [execution-model.md §3 run]); this spec resolves the naming: operator surfaces use "task" for user-friendliness, specs use `run` for precision. (see §4.3)
- **between-task invariant** — pause and upgrade operator controls complete in-flight runs before taking effect; only `stop --immediate` aborts in-flight runs. (see §4.3)
- **improvement-pause** — a subtype of pause with a scheduled or triggered onset, used by the S09 improvement cycle; resumes automatically when the improvement loop completes. (see §4.3)
- **RTO** — recovery time objective; measured SIGTERM → daemon `ready` status event per §4.8.
- **N-1 compatibility** — a writer at schema version N must produce artifacts readable by a reader at schema version N-1 (the immediately prior published version). Applies to event schemas, checkpoint format, and queue per §4.4–§4.6 and [event-model.md §3.5].
- **commit-hash integrity gate** — the MVH binary-integrity check: the to-be-installed binary's source-commit hash must match the operator-supplied expected hash. Deferred post-MVH is full binary signing. (see §4.2)
- **health-check interface** — a function returning `health_status ∈ {OK, degraded, failed}` with an optional reason string; every subsystem exposes one per §4.9.
- **liveness signal** — a heartbeat event on a defined cadence; missing heartbeats beyond tolerance trigger a degraded classification per §4.9.
- **audit record** — a subset of traces where `actor_role` is privileged and `chosen_action` affected policy, role permissions, or budget. (see §4.9)
- **reconciliation carve-out** — the rule that pause does not interrupt reconciliation workflows; pauses issued during `reconciling` are queued and applied when reconciliation completes. Normative statement at §4.3.ON-010. (see §4.3)
- **drain** — the ordered shutdown sequence on `stop --graceful` or SIGTERM that completes in-flight runs to their next checkpoint before exiting. (see §4.7)
- **multi-daemon coordination** — operator commands that list, target, and bound multiple per-project daemons on one machine (`harmonik list`, flag-based daemon identification, machine-level agent-subprocess ceiling). (see §4.10)

## 4. Normative requirements

### 4.1 Exit-code taxonomy and failure-mode catalog obligations

#### ON-001 — Operator-observable exit codes are structured

Every operator-invoked harmonik command (`daemon`, `attach`, `enqueue`, `status`, `pause`, `stop`, `upgrade`, and all multi-daemon commands per §4.10) MUST return a structured exit code. Zero MUST mean success. Non-zero codes MUST map one-to-one to a failure category declared in the exit-code taxonomy of §8. The mapping MUST be stable: a given code MUST refer to the same category across releases within the N-1 compatibility window.

Tags: mechanism

#### ON-002 — Exit-code taxonomy obligation

The spec-draft pass MUST produce a normative exit-code taxonomy naming every non-zero exit code emitted by any operator command. The taxonomy MUST specify, for each code: the failure category, the operator-observable symptom, the emitted event type (if any), and the operator remediation pointer. The taxonomy lives in §8 of this spec; cross-references from other specs (e.g., [process-lifecycle.md §8.3 harmonik status]) MUST resolve to §8 entries.

Tags: mechanism

#### ON-003 — Startup failure-mode catalog obligation

The spec-draft pass MUST produce a normative startup failure-mode catalog, co-owned with [process-lifecycle.md §8.2]. For every daemon-startup prerequisite failure (git bad state, Beads SQLite unavailable, Beads schema version unsupported, checkpoint schema version unsupported, stale-pidfile race, filesystem unwritable, disk-full during checkpoint commit, socket bind failure), the catalog MUST specify: detection rule, exit code per §4.1.ON-001, operator remediation procedure, emitted event type per [event-model.md §3.2], and the reconciliation Cat 0 classification per [reconciliation.md §9.3]. The catalog is the authoritative input to `harmonik status`'s infrastructure-prereq reporting per [process-lifecycle.md §8.3].

Tags: mechanism

#### ON-004 — Config inventory obligation

The spec-draft pass MUST produce a normative config inventory enumerating every operator-configurable knob referenced across foundation specs. For each knob, the inventory MUST specify: the precedence layer (runtime override / operator-policy file / workflow definition / default, per [control-points.md §6.8]), the default value, the allowed range or enumeration, and the change-takes-effect semantics (next operator pause, immediate, next daemon start, etc.). At minimum the inventory covers the timer-flush cadence ([event-model.md §3.4]), budget warning threshold ([control-points.md §6.9]), drain timeout (§4.7), RTO thresholds (§4.8), queue-empty re-query cadence ([process-lifecycle.md §8.4]), Cat 0 pre-check retry cadence ([reconciliation.md §9.3]), and per-Cat reconciliation budgets ([reconciliation.md §9.4a]).

Tags: mechanism

### 4.2 Integrity gate for binary install

#### ON-005 — Commit-hash integrity gate is the MVH binary-install check

The pause-to-upgrade path (§4.3, §4.6) MUST verify the to-be-installed binary's source-commit hash against an operator-supplied expected hash before the daemon's exec-replacement step. The check MUST fail-closed: on mismatch or missing hash, the daemon MUST NOT exec-replace and MUST remain in the `paused` state with a `operator_upgrade_rejected` event emitted per [event-model.md §3.2]. Handler binaries installed via [handler-contract.md §4.10] MUST ALSO carry the commit-hash check; this requirement names the daemon-level invariant.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### ON-006 — Full binary signing is deferred post-MVH

Full cryptographic signing of binaries (code-signing certificates, reproducible build attestations, signature-chain verification) is deferred post-MVH. Conforming MVH implementations MUST NOT be required to verify signatures beyond the commit-hash match of §4.2.ON-005. Post-MVH introduction of signing is additive and does NOT invalidate MVH conformance.

Tags: mechanism

### 4.3 Operator-control semantics between tasks

#### ON-007 — Operator "task" equals execution-model "run"

For operator-facing documentation, CLI output, and event payload human-readable fields, "task" MUST denote one complete run of a workflow, from `run_started` to `run_completed` or `run_failed` per [execution-model.md §4.3]. Normative spec text, event payload field names, and internal logs MUST use `run` / `run_id`. Surfaces that render human-facing copy MAY translate `run` to "task"; wire formats MUST NOT.

Tags: mechanism

#### ON-008 — Pause and upgrade respect the between-task invariant

An operator `pause` or `upgrade` command issued while the daemon status is `ready` MUST NOT interrupt any in-flight run. The daemon MUST transition to `pausing`, allow each in-flight run to proceed to its next durable checkpoint per [execution-model.md §4.5], and only then transition to `paused`. `upgrade` further transitions `paused` → `upgrading` → (exec-replace) → `running` under the contract of §4.6.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### ON-009 — `stop --immediate` is the only control that aborts in-flight runs

`stop --immediate` and SIGKILL (treated equivalently) MUST abort in-flight runs. Aborted runs MUST emit `run_failed` with class `canceled` per [execution-model.md §8.4] once the daemon restarts and reconciliation classifies them per [reconciliation.md §9.3]. No other operator control is permitted to abort in-flight runs; proposals to add `pause --immediate` or `upgrade --immediate` MUST be rejected as violations of §4.3.ON-008.

Tags: mechanism

#### ON-010 — Reconciliation carve-out: pause queues during `reconciling`

Pause MUST NOT interrupt reconciliation workflows per [reconciliation.md §9.1]. The daemon's status progression is `starting` → (optional `degraded`) → `reconciling` → `ready` per [process-lifecycle.md §8.2]; the between-task invariant of §4.3.ON-008 applies only after the daemon reaches `ready`. An operator pause issued during `reconciling` MUST be queued and applied at the boundary event "all reconciliation runs have either resumed into normal flow or produced a verdict."

Tags: mechanism

#### ON-011 — Operator-control state machine

The daemon MUST implement the operator-control state machine defined in §7.1. States are `running`, `pausing`, `paused`, `resuming`, `stopped` (terminal-recoverable via `start`), `upgrading`, and `improvement-pausing` / `improvement-paused` (subtype of pause per §4.3.ON-012). Transitions and emitted events are normative per §7.1.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### ON-012 — Improvement-pause is a subtype of pause

The S09 improvement cycle MUST NOT introduce a new operator-control state. An improvement-pause MUST transition `running` → `pausing` → `paused` via the same path as an operator pause, with the additional invariant that the `paused` → `resuming` transition is triggered automatically when the improvement loop completes (no operator action required). Events MUST distinguish the subtype via a payload field (e.g., `pause_reason = improvement`).

Tags: mechanism

#### ON-013 — Operator-control events are emitted per state transition

The daemon MUST emit one typed event per operator-control state transition: `operator_pausing` (on `running` → `pausing`), `operator_paused` (on `pausing` → `paused`), `operator_resuming` (on `paused` → `resuming`), `operator_stopped` (on entry to `stopped`), `operator_upgrading` (on `paused` → `upgrading`), and `operator_upgrade_completed` (on `upgrading` → `running` after exec-replace). Payload schemas live in [event-model.md §3.2]; emission timing is normative here.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### ON-014 — Reconciliation operator override (pre-execution verdict pause)

The spec-draft pass MUST produce a normative per-reconciliation-workflow policy option to pause the daemon's verdict-execution step (per [reconciliation.md §9.5b]) until an operator confirms or vetoes the verdict. The naming convention for the operator commands is: `harmonik confirm-verdict <run_id>` / `harmonik veto-verdict <run_id> [--promote-to escalate-to-human]`. Default: execution proceeds without operator confirmation; operators opt in by policy. This obligation applies to all investigator-dispatched reconciliation categories (Cat 2, 3, 6a per [reconciliation.md §9.2a]). Foundation owns the naming convention; [reconciliation.md §9.5b] owns the execution-step specifics.

Tags: mechanism

### 4.4 Queue-format compatibility contract

#### ON-015 — Beads is the queue; overlay schema is harmonik's half

The operator's pending-tasks list and harmonik's dispatchable queue are the same store: Beads (SQLite, `Dicklesworthstone/beads_rust`) per [beads-integration.md §10.1–§10.3]. Queue-format compatibility MUST be the union of (a) Beads schema compat (managed upstream) AND (b) harmonik's overlay schema compat: the `Harmonik-Bead-ID` trailers in checkpoint commits per [execution-model.md §4.4], the bead-ID references in events per [event-model.md §3.2], and the session-log bead-ID metadata per [workspace-model.md §5.3]. Both halves MUST be N-1 readable.

Tags: mechanism

#### ON-016 — Queue schema version check on daemon startup

On daemon startup, the daemon MUST check both the Beads SQLite schema version and harmonik's overlay schema version against the running binary's supported set (current N and prior N-1). An unsupported version MUST cause startup failure with the exit code assigned to category "queue-format-unsupported" per §8, naming the required migration release in the failure event payload. The check is part of the startup failure-mode catalog of §4.1.ON-003.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### ON-017 — Beads pre-1.0 breakage is absorbed, not forked

Harmonik MUST version-pin Beads per the external-inputs protocol (problem-space §External inputs) and MUST route all Beads interactions through the `br`-CLI adapter of [beads-integration.md §10.8]. A Beads breaking change MUST produce one localized adapter update; harmonik MUST NOT fork Beads. This requirement is a structural obligation on the adapter boundary, not on every caller.

Tags: mechanism

### 4.5 Schema compatibility window

#### ON-018 — N-1 compatibility is the MVH compat window

Every versioned on-disk or wire artifact declared by foundation specs — event-envelope schema ([event-model.md §3.1]), event payload schemas ([event-model.md §3.2]), checkpoint trailers and sibling files ([execution-model.md §4.4]), queue overlay (§4.4.ON-015), policy schema ([control-points.md §6.5]) — MUST maintain N-1 readability. A reader pinned to version N-1 MUST successfully parse and interpret artifacts written by version N, with additive fields treated as unknown but non-fatal. Breaking changes MUST be accompanied by a migration release and MUST NOT be introduced mid-run; they MUST land at an operator pause per §4.3.

Tags: mechanism

#### ON-019 — Migration releases are operator-paused boundaries

A migration release (any release that bumps an N-1-covered schema version to break the compat window — i.e., a change no longer readable by readers at the current N) MUST require an operator pause before installation. The `harmonik upgrade` contract of §4.6 MUST refuse to exec-replace into a migration release unless the daemon is in the `paused` state AND the on-disk state's schema version is within the new binary's supported set. Installing a migration release MUST NOT auto-migrate on-disk state; a dedicated migration workflow (post-MVH) is the path.

Tags: mechanism

### 4.6 `harmonik upgrade` contract

#### ON-020 — `harmonik upgrade` contract obligation

The spec-draft pass MUST produce a normative `harmonik upgrade` contract specifying, at minimum: (a) binary-source mechanism (repo-relative path and/or explicit hash-supply flag), (b) operator-supplied expected commit-hash check procedure per §4.2.ON-005, (c) drain-vs-reconciliation interaction (what `upgrade` does if reconciliation workflows are in-flight per §4.3.ON-010 — MUST queue; MUST NOT interrupt), (d) cross-version state contract (what upgrade does if the new binary's schema-version is N-1, N, or N+1 vs the on-disk state — MUST succeed for same and N-1; MUST refuse for broader mismatches per §4.5.ON-019), (e) socket/client-CLI retry behavior during exec-replacement (clients MUST retry on broken socket for a bounded window; daemon MUST re-bind the same socket path after exec-replace). The contract lives in §4.6 of this spec; referring specs cross-reference here.

Tags: mechanism

#### ON-021 — Upgrade preserves in-flight run recoverability

An `upgrade` operation MUST NOT make any in-flight run unrecoverable. Because §4.3.ON-008 requires pause to complete in-flight runs to their next checkpoint before the `pausing` → `paused` transition, the state the new binary inherits MUST be reconstructible from git + Beads per [execution-model.md §4.7] and the restart RTO of §4.8. The cross-version state contract of §4.6.ON-020 MUST reject upgrades that would violate this invariant.

Tags: mechanism

### 4.7 Secrets redaction and graceful shutdown

#### ON-022 — Secrets are injected at handler launch and never logged

Secrets (API keys, tokens, credentials) MUST be injected at handler launch per [handler-contract.md §4.7]. Secrets MUST NOT appear in the event log under any circumstance. Secrets MUST NOT appear in session logs without redaction. Redaction is mechanism-tagged and MUST be enforced pre-emission: handler implementations MUST apply prefix-regex matching and per-handler redaction patterns before any write to a durable sink (event bus, session log, audit record).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### ON-023 — Secrets-redaction compile-time payload-schema check

Event payload schemas declared per [event-model.md §3.2] MUST NOT declare any field typed as `Secret` or equivalent. A compile-time check (lint pass or generated-code assertion) MUST reject any payload schema that would carry a secret through the event bus. This closes the redaction-obligation loop: redaction cannot be forgotten at an emission site because no emission site is permitted to carry secret-typed fields.

Tags: mechanism

#### ON-024 — Command-execution sandbox invariant

Agents MUST execute within a leased workspace directory per [workspace-model.md §5.1]. Escape attempts — symlinks resolving outside the workspace, path-traversal patterns, git hooks sourced from untrusted paths — MUST be prevented. Specific enforcement is owned by the subsystem specs for S04 (handler runner) and S06 (workspace manager); this spec states the cross-cutting invariant.

Tags: mechanism

#### ON-025 — Network egress and skill-injection policy enforcement

Network egress MUST be governed by policy per [control-points.md §6.5]; a policy MAY whitelist domains for agent access. Skills provisioned per [handler-contract.md §4.11] MUST honor the egress policy: a provisioned skill that would require egress to a non-whitelisted domain MUST fail provisioning, and the handler MUST emit a `skills_provisioned` event (per [event-model.md §3.2]) listing only the skills actually installed. Skills requiring filesystem access outside the workspace MUST fail provisioning per §4.7.ON-024.

Tags: mechanism

#### ON-026 — Prompt-injection defense is handler-owned

Input sanitization for user-provided content in the input workspace MUST be the handler's responsibility per [handler-contract.md §4.1]. Handlers MUST NOT let user-provided content in the input workspace alter the agent's system-prompt instructions. This spec states the obligation; the enforcement mechanism lives in handler-contract.

Tags: mechanism

#### ON-027 — Graceful-shutdown ordering

On `stop --graceful` or SIGTERM, the daemon MUST execute the shutdown sequence in the following order, each step completing before the next begins: (1) orchestrator stops pulling new tasks from the queue; (2) in-flight runs proceed to their next checkpoint, then suspend per [execution-model.md §4.5]; (3) agent runners wait for handler subprocesses to complete or reach the drain timeout; (4) event bus flushes pending events (fsync per [event-model.md §3.4]); (5) memory layer flushes indexing; (6) workspace manager unlocks leased workspaces and cleans up incomplete adze setups per [workspace-model.md §5.1]; (7) orchestrator exits with code 0 if clean, or the exit code for "drain-timeout-escalated" per §8 if any step exceeded its bound.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### ON-028 — `stop --immediate` skips drain steps 2–3

On `stop --immediate` or SIGKILL, the daemon MUST skip steps 2 and 3 of §4.7.ON-027. In-flight agent subprocesses MUST be killed (SIGTERM with a short bounded window, then SIGKILL). In-flight run state is recoverable on next startup via checkpoint + reconciliation per [reconciliation.md §9.2], but the in-flight agent subprocesses are not gracefully stopped.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### ON-029 — Drain timeout is operator-configurable

The drain timeout (the bound on steps 2 and 3 of §4.7.ON-027) MUST be operator-configurable per the config inventory of §4.1.ON-004. The default value is specified in the config inventory; the change-takes-effect semantics is "next daemon start" (drain timeout is read once at startup).

Tags: mechanism

### 4.8 Restart RTO

#### ON-030 — Restart reconstruction path

Daemon restart MUST reconstruct the in-memory model by walking the git checkpoint trail per [execution-model.md §4.7] and querying Beads via `br` per [beads-integration.md §10.8]. The JSONL event log MUST NOT be replayed for state reconstruction (per [event-model.md §3.4, §3.6] and locked decision #12 — no DTW). Reconciliation workflows MUST spawn for in-flight runs per [reconciliation.md §9.2].

Tags: mechanism

#### ON-031 — Restart RTO target

Restart MUST reach the pre-restart state within **X seconds**, measured from SIGTERM (or crash) to the daemon emitting the `ready` status event per [process-lifecycle.md §8.2]. The target X MUST satisfy all three criteria of §4.8.ON-032 simultaneously; the 300-second hard ceiling is non-negotiable.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent

#### ON-032 — RTO criteria and hard ceiling

The RTO target X of §4.8.ON-031 MUST be set against the following criteria:

- **Criterion 1 — operator expectation.** MVH assumes single-operator, single-instance deployment. Target: X ≤ 30 seconds for 95th percentile under nominal conditions (≤ a few hundred open beads, ≤ a few dozen in-flight runs).
- **Criterion 2 — reconstruction complexity.** Restart time is proportional to (a) git-log walk depth since the oldest open bead's first checkpoint, and (b) Beads query latency for ready + in-flight bead sets. JSONL event count is NOT a restart-time factor (it is not read on restart per §4.8.ON-030).
- **Criterion 3 — hard ceiling.** 300 seconds. Beyond this the operator MUST be notified (the daemon MUST enter `degraded` reporting `reconciling` with progress markers; operator intervention is permitted). Criterion 3 is non-negotiable. Criterion 1 MAY be relaxed with reason if measurements show 30 seconds is unachievable at MVH scale.

Reconciliation-workflow dispatch time is part of the RTO; reconciliation-workflow execution time (investigator-agent LLM calls per [reconciliation.md §9.4]) is NOT — it is bounded by that workflow's own policy per [reconciliation.md §9.4a].

Tags: mechanism

#### ON-033 — RTO measurement boundary

The RTO of §4.8.ON-031 MUST be measured from SIGTERM (or daemon crash timestamp recorded by the OS) to the daemon's `ready` status event emission per [process-lifecycle.md §8.2]. Measurement MUST NOT start from `harmonik daemon` invocation time (which excludes crash-to-restart-trigger latency).

Tags: mechanism

### 4.9 Observability envelope

#### ON-034 — Every subsystem emits typed events

Every subsystem MUST emit events per [event-model.md §3.2]. Event taxonomy additions introduced by a subsystem MUST be declared via the subsystem envelope and registered per [event-model.md §3.2]. Every event MUST carry the four-axis and mechanism/cognition tags per [architecture.md §1.1].

Tags: mechanism

#### ON-035 — Every subsystem emits structured logs

Every subsystem MUST emit structured logs per [event-model.md §3.8]. Unstructured log lines (free-form text only) are forbidden at spec-declared emission points. Structured logs are the MVH substrate for observability; Prometheus / OpenTelemetry wire formats are post-MVH per §4.10.ON-043.

Tags: mechanism

#### ON-036 — Every subsystem exposes a health-check interface

Every subsystem MUST expose a health-check interface returning `health_status ∈ {OK, degraded, failed}` with an optional reason string. The orchestrator MUST aggregate subsystem health into a harmonik-wide health status exposed via `harmonik status` per [process-lifecycle.md §8.3].

Tags: mechanism

#### ON-037 — Every subsystem emits liveness heartbeats

Every subsystem MUST emit a liveness heartbeat event on a defined cadence. Missing heartbeats beyond tolerance MUST trigger a `degraded` classification for that subsystem and raise the aggregated harmonik-wide health accordingly. The cadence and tolerance are operator-configurable per §4.1.ON-004.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### ON-038 — Audit records are a subset of traces

Audit records MUST be produced as a subset of transition records per [execution-model.md §4.4]: the subset where `actor_role` is in a privileged role (per [architecture.md §1.6]) AND the `chosen_action` affected policy, role permissions, or budget. No separate audit-log store is introduced; audit is a query over the transition-record sibling files and their projections.

Tags: mechanism

#### ON-039 — All observability operations are mechanism-tagged

Every observability operation (health-check evaluation, heartbeat emission, metric emission, log emission, audit-record derivation) MUST be mechanism-tagged per [architecture.md §1.1, §1.2]. Any operation that requires cognition to produce the observability signal MUST be represented as a separate verification node per [architecture.md §1.3], NOT folded into the observability protocol.

Tags: mechanism

#### ON-040 — Silent-hang detection obligation

Detection of silent hangs (handler subprocesses producing no output and no heartbeat within a bounded window) is obligated under [handler-contract.md §4.6]. This spec names the obligation to ensure the operator-observable consequence (a `handler_silent_hang` event or equivalent per [event-model.md §3.2], and a subsystem `degraded` classification per §4.9.ON-037) is not silently deferred. The enforcement mechanism lives in handler-contract.

Tags: mechanism

### 4.10 Multi-daemon coordination and multi-tenancy deferral

#### ON-041 — Multi-daemon commands obligation

The spec-draft pass MUST produce normative definitions for machine-level operator commands: (a) `harmonik list` — enumerate running daemons machine-wide with project path, pid, socket path, and current status; (b) daemon-identification flags on all daemon-communicating commands (stop, pause, attach, status, upgrade) — at minimum `--socket <path>`, `--cwd <path>`, and `--daemon-id <id>`; (c) a machine-level agent-subprocess ceiling mechanism — a cross-daemon bound on concurrently running agent subprocesses enforced by a shared lock or a machine-level coordinator process. These commands are the minimum operator-visible concession foundation makes to multi-tenancy in MVH.

Tags: mechanism

#### ON-042 — Multi-tenancy is explicitly deferred post-MVH

Per-project daemon isolation (one daemon per project per [process-lifecycle.md §8.1]) is the MVH answer to multi-tenancy. Per-tenant cost attribution is out of scope for MVH; running N daemons does not auto-partition costs. The following concerns are acknowledged as real and explicitly deferred, not solved:

- **Shared operator LLM budgets.** The Anthropic quota is a per-account limit; running N daemons does not create N quotas. A machine-level budget coordinator is required post-MVH.
- **Shared operator identity and auth.** `harmonik attach` across N daemons is the same human with the same skills and the same `br` binary; global install conflicts and access controls are shared concerns.
- **Shared skill registries.** Skills are typically installed machine-wide (e.g., Claude Code skills under `~/.claude/skills/`); a provisioning failure in one project is a global failure surface.

"Deferred" here means "not solved by per-project-daemon isolation"; it does NOT mean "dismissed." Post-MVH amendments to address these concerns are a foundation amendment, not an incremental change.

Tags: mechanism

#### ON-043 — Metrics exposition format is deferred post-MVH

Prometheus and OpenTelemetry wire formats for metric exposition are deferred post-MVH. MVH observability is structured logs (§4.9.ON-035) plus typed events (§4.9.ON-034). An implementation MAY additionally expose Prom/OTel endpoints but MUST NOT require them for MVH conformance.

Tags: mechanism

#### ON-044 — Distributed tracing across daemons is deferred post-MVH

Distributed tracing across multiple harmonik instances is deferred post-MVH. Per-project daemon isolation means multi-instance tracing is an OS-process-isolation concern, not a harmonik-code concern — each daemon is a separate process with its own event log and its own state. Cross-daemon correlation (if ever needed) is an external-tooling layer, not a foundation spec.

Tags: mechanism

### 4.11 Resource budgets

#### ON-045 — Budgets are declared, enforced, and attributed cross-subsystem

Resource budgets (token, wall-clock, iterations) MUST be declared in policy per [control-points.md §6.9], enforced at dispatch by the agent runner per [control-points.md §6.9], and attributed in observability per run, per role, aggregated to per-workflow and per-harmonik-instance. Cost attribution per tenant is out of scope for MVH per §4.10.ON-042.

Tags: mechanism

#### ON-046 — Budget events are operator-observable

Budget-threshold events (`budget_warning`, `budget_exhausted`, `budget_accrual` per [event-model.md §3.2] and [control-points.md §6.9]) MUST be operator-observable via `harmonik status` and the attach UI per [process-lifecycle.md §8.3]. Operator-observable MUST NOT require parsing the raw JSONL; a summarized view is adequate.

Tags: mechanism

## 5. Invariants

#### ON-INV-001 — N-1 compat window holds across every versioned artifact

Every versioned on-disk or wire artifact declared by foundation specs MUST hold the N-1 readability property of §4.5.ON-018 simultaneously. A release that breaks N-1 for any single artifact is a migration release per §4.5.ON-019 and MUST require an operator pause for install. This invariant constrains event-model, execution-model, control-points, and beads-integration together.

Tags: mechanism

#### ON-INV-002 — No PR-gated rollout for MVH

Foundation specs and MVH-target subsystem specs MUST assume direct-to-main development per `docs/foundation/project-level/build-practices.md`. No PR-based merge gate is the MVH enforcement model; agent-reviewer-every-commit + post-push CI surfacing is the discipline. Subsystem specs MUST NOT design contracts that assume a pre-merge human review gate for MVH. Post-MVH restoration of PR-based gating is an additive concern to subsystem design (it affects process, not contract shape).

Tags: mechanism

#### ON-INV-003 — Secrets never appear in durable sinks unredacted

For every event-model-declared sink (event log per [event-model.md §3.4], dead-letter log per [event-model.md §3.7], session log per [workspace-model.md §5.3]), secrets MUST NOT appear unredacted. The invariant holds jointly across §4.7.ON-022, §4.7.ON-023, and the handler-contract secrets-injection rule — losing any one breaks the invariant.

Tags: mechanism

#### ON-INV-004 — Between-task invariant covers pause, upgrade, and improvement-pause

Pause (§4.3.ON-008), upgrade (§4.6.ON-020, §4.6.ON-021), and improvement-pause (§4.3.ON-012) MUST all complete in-flight runs to their next durable checkpoint before taking effect. Only `stop --immediate` (§4.3.ON-009) is exempt. This invariant is the operator-facing expression of locked decision #10; reopening it requires strong new evidence per [STATUS.md §Decisions Locked In].

Tags: mechanism

#### ON-INV-005 — Restart RTO hard ceiling is non-negotiable

The 300-second hard ceiling of §4.8.ON-032 criterion 3 MUST hold across every MVH-conforming implementation. No policy knob, no config override, and no implementation choice MAY relax the hard ceiling. Violation MUST trigger operator notification per §4.8.ON-032 and a `degraded` classification per §4.9.ON-036.

Tags: mechanism

## 6. Schemas and data shapes

This spec does not introduce new persistent data types. Schemas referenced:

- **Event envelope and payloads** — [event-model.md §3.1, §3.2]. Co-owned events emitted by operator-control transitions are listed in §6.5 below.
- **Checkpoint commit trailers and transition-record sibling file** — [execution-model.md §4.4].
- **Queue overlay** (bead-ID propagation) — [beads-integration.md §10.6].
- **Policy schema** — [control-points.md §6.5].
- **Health-check interface** — described inline in §4.9.ON-036 as `health_status ∈ {OK, degraded, failed}` plus optional reason string; no persistent representation.
- **Operator-control state set** — inline enum in §7.1.

### 6.4 Schema evolution

Every artifact this spec references holds the N-1 compatibility window of §4.5.ON-018 (normative statement there). Version fields are owned by the defining spec (checkpoint schema-version trailer in execution-model, event schema-version in event-model, queue overlay version in beads-integration, policy schema version in control-points). This spec is normative for the N-1 window; defining specs are normative for the version field location and increment policy.

### 6.5 Co-owned event payloads

The following events are EMITTED by this spec's operator-control path (§4.3, §4.6) and REGISTERED in [event-model.md §3.2]:

- `operator_pausing` — emitted on `running` → `pausing`; payload schema in [event-model.md §3.2]; carries `pause_reason` (operator | improvement).
- `operator_paused` — emitted on `pausing` → `paused`; payload schema in [event-model.md §3.2].
- `operator_resuming` — emitted on `paused` → `resuming`; payload schema in [event-model.md §3.2].
- `operator_stopped` — emitted on entry to `stopped`; payload schema in [event-model.md §3.2]; carries `stop_mode` (graceful | immediate).
- `operator_upgrading` — emitted on `paused` → `upgrading`; payload schema in [event-model.md §3.2]; carries `expected_commit_hash`.
- `operator_upgrade_completed` — emitted on `upgrading` → `running` post-exec-replace; payload in [event-model.md §3.2].
- `operator_upgrade_rejected` — emitted when §4.2.ON-005 commit-hash check fails; payload in [event-model.md §3.2].

This spec is normative for the *when*; event-model is normative for the *shape*.

## 7. Protocols and state machines

### 7.1 Operator-control state machine

States: `running`, `pausing`, `paused`, `resuming`, `stopped`, `upgrading`, `improvement-pausing`, `improvement-paused`.

| From | Event | Guard | To | Emits |
|---|---|---|---|---|
| `running` | `pause` | daemon status = `ready`; no-op if `reconciling` (queued per §4.3.ON-010) | `pausing` | `operator_pausing` (`pause_reason=operator`) |
| `running` | improvement-loop trigger | improvement policy active | `improvement-pausing` | `operator_pausing` (`pause_reason=improvement`) |
| `pausing` | last in-flight run reaches next checkpoint | all runs drained | `paused` | `operator_paused` |
| `improvement-pausing` | last in-flight run reaches next checkpoint | all runs drained | `improvement-paused` | `operator_paused` (`pause_reason=improvement`) |
| `paused` | `resume` | none | `resuming` | `operator_resuming` |
| `improvement-paused` | improvement loop completes | none | `resuming` | `operator_resuming` |
| `resuming` | dispatch loop re-entered | none | `running` | — |
| `paused` | `upgrade <hash>` | commit-hash matches §4.2.ON-005 | `upgrading` | `operator_upgrading` |
| `paused` | `upgrade <hash>` | commit-hash mismatch | `paused` | `operator_upgrade_rejected` |
| `upgrading` | exec-replace succeeds | new-binary schema ≥ current N-1 per §4.5.ON-018 | `running` (new binary) | `operator_upgrade_completed` |
| `running` | `stop --graceful` | none | drain → `stopped` | `operator_stopped` (`stop_mode=graceful`) |
| any | `stop --immediate` | none | `stopped` | `operator_stopped` (`stop_mode=immediate`) |
| `stopped` | `start` | none | `running` (after normal startup per [process-lifecycle.md §8.2]) | startup events per process-lifecycle |

> INFORMATIVE: The state machine above is the operator-control half. The daemon-startup status progression (`starting` → optional `degraded` → `reconciling` → `ready`) is owned by [process-lifecycle.md §8.2]; operator-control entry (`running`) occurs only at `ready`.

### 7.2 Drain protocol pseudocode

```
FUNCTION drain_graceful(timeout):
    -- §4.7.ON-027 steps 1-7
    stop_dispatch_loop()                                 -- step 1
    wait_for_runs_to_checkpoint(timeout.step_2)          -- step 2
    wait_for_handler_subprocess_exit(timeout.step_3)     -- step 3
    flush_event_bus()                                    -- step 4; fsync per event-model
    flush_memory_indexing()                              -- step 5
    unlock_leased_workspaces()                           -- step 6; per workspace-model
    IF any_step_exceeded_its_bound:
        RETURN exit_code("drain-timeout-escalated")
    ELSE:
        RETURN exit_code("success")
```

Every branch corresponds to a normative requirement: steps 1–7 to ON-027; timeout escalation to ON-029; stop-immediate skip-steps to ON-028.

### 7.3 Upgrade protocol pseudocode

```
FUNCTION upgrade(expected_hash, new_binary_path):
    -- §4.6.ON-020
    IF daemon.status != "paused":
        RETURN exit_code("upgrade-requires-paused")
    actual_hash = compute_commit_hash(new_binary_path)
    IF actual_hash != expected_hash:
        emit("operator_upgrade_rejected", expected_hash, actual_hash)
        RETURN exit_code("upgrade-hash-mismatch")
    new_schema_set = read_supported_schemas(new_binary_path)
    IF not compatible(on_disk_schema_version, new_schema_set):
        emit("operator_upgrade_rejected", reason="schema-incompatible")
        RETURN exit_code("upgrade-schema-incompatible")
    daemon.state = "upgrading"
    emit("operator_upgrading", expected_hash)
    exec_replace(new_binary_path)   -- same socket path; clients retry per §4.6.ON-020
    -- new process resumes, runs startup per [process-lifecycle.md §8.2]
    -- on `ready`, transitions to `running`, emits `operator_upgrade_completed`
```

Branch points map to requirements: paused-precondition to ON-008, hash check to ON-005, schema-compat check to ON-019 and ON-021, exec-replace + socket retry to ON-020.

## 8. Error and failure taxonomy

Exit-code taxonomy. Every non-zero code maps to one category. Category names are stable across the N-1 window per §4.1.ON-001.

| Exit code | Category | Detection rule | Emitted event | Remediation pointer |
|---|---|---|---|---|
| 0 | success | — | — | — |
| 1 | generic-failure | Fallback for uncategorized failure; MUST be rare; presence in a release indicates missing taxonomy entry. | `run_failed` or subsystem-specific | Operator files incident; foundation amends taxonomy. |
| 2 | queue-format-unsupported | Beads schema version or harmonik overlay version not in supported set per §4.4.ON-016. | `daemon_startup_failed` | Install migration release per §4.5.ON-019. |
| 3 | checkpoint-schema-unsupported | Checkpoint trailer or sibling-file schema version not in supported set per §4.5.ON-018. | `daemon_startup_failed` | Install migration release. |
| 4 | event-schema-unsupported | Event envelope or payload schema version not in supported set per [event-model.md §3.5]. | `daemon_startup_failed` | Install migration release. |
| 5 | pidfile-locked | Another daemon holds the pidfile lock for this project per [process-lifecycle.md §8.2]. | `daemon_startup_failed` | Identify running daemon via `harmonik list`; stop or target with `--daemon-id`. |
| 6 | socket-bind-failed | Socket path cannot be bound (permission, stale socket). | `daemon_startup_failed` | Per startup failure-mode catalog per §4.1.ON-003. |
| 7 | git-bad-state | Git log walk fails (corrupt repo, missing refs, unreadable objects). | `daemon_startup_failed` | Per startup failure-mode catalog. |
| 8 | beads-unavailable | `br` CLI invocation fails or Beads SQLite is unreadable. | `daemon_startup_failed` | Per startup failure-mode catalog. |
| 9 | filesystem-unwritable | Workspace root or `.harmonik/` directory is not writable. | `daemon_startup_failed` | Per startup failure-mode catalog. |
| 10 | disk-full | Filesystem full during checkpoint commit attempt. | `daemon_startup_failed` or `run_failed` | Per startup failure-mode catalog. |
| 11 | drain-timeout-escalated | Any step of §4.7.ON-027 exceeded its bound during graceful shutdown. | `operator_stopped` (`stop_mode=graceful`, `drain_timeout=true`) | Increase drain timeout per §4.7.ON-029; investigate stuck handler. |
| 12 | rto-hard-ceiling-exceeded | Restart exceeded 300-second ceiling per §4.8.ON-032. | `daemon_degraded` | Operator intervention per §4.8.ON-032. |
| 13 | upgrade-requires-paused | `upgrade` invoked while daemon is not `paused`. | `operator_upgrade_rejected` | Issue `pause`, then retry `upgrade`. |
| 14 | upgrade-hash-mismatch | §4.2.ON-005 commit-hash check failed. | `operator_upgrade_rejected` | Re-verify binary source; supply correct hash. |
| 15 | upgrade-schema-incompatible | New binary's schema version is outside the N-1 window vs on-disk state per §4.5.ON-019. | `operator_upgrade_rejected` | Install migration release. |
| 16 | operator-control-invalid-state | Operator issued a command incompatible with the current state-machine state (e.g., `resume` while `running`). | `operator_command_rejected` | Inspect `harmonik status`; issue valid command. |
| 17 | multi-daemon-target-missing | A daemon-communicating command's `--socket` / `--cwd` / `--daemon-id` target cannot be resolved per §4.10.ON-041. | — | Use `harmonik list` to identify running daemons. |
| 18 | machine-ceiling-exhausted | Machine-level agent-subprocess ceiling per §4.10.ON-041 blocks a dispatch. | `dispatch_deferred` | Reduce concurrent workload or raise ceiling. |

Additional codes may be added within the N-1 window as long as existing code-to-category mappings remain stable (normative code-stability rule at §4.1.ON-001). Taxonomy additions are reflected in the config inventory per §4.1.ON-004 and in the startup failure-mode catalog per §4.1.ON-003 where applicable.

> INFORMATIVE: Codes 1–18 are the MVH surface. Subsystem specs MAY declare additional subsystem-specific exit codes, which are registered against this taxonomy during spec-draft.

## 9. Cross-references

### 9.1 Depends on

- **[architecture.md §1.1]** — four-axis classification; every requirement and observability operation is tagged on the axes defined there.
- **[architecture.md §1.2]** — ZFC test; §4.9.ON-039 asserts observability operations are mechanism-tagged.
- **[architecture.md §1.6]** — role taxonomy; audit record's privileged-role subset per §4.9.ON-038.
- **[architecture.md §1.8]** — centralized-controller principle; the operator-control state machine is daemon-owned.
- **[event-model.md §3.1, §3.2]** — event envelope and payload registry; §6.5 co-owned events register there.
- **[event-model.md §3.4, §3.5]** — fsync policy and schema compat; §4.5.ON-018 and §4.7.ON-027 step 4 depend here.
- **[event-model.md §3.7]** — dead-letter log; §4.7.ON-022 secret-redaction applies.
- **[event-model.md §3.8]** — structured log schema; §4.9.ON-035 depends here.
- **[execution-model.md §4.3]** — run definition; §4.3.ON-007 maps operator "task" to `run`.
- **[execution-model.md §4.4, §4.5]** — checkpoint contract and cadence; §4.3.ON-008 and §4.7.ON-027 step 2 depend here.
- **[execution-model.md §4.7]** — state-reconstruction contract; §4.8.ON-030 depends here.
- **[execution-model.md §8]** — failure taxonomy; §4.3.ON-009 maps `stop --immediate` to class `canceled`.
- **[handler-contract.md §4.1]** — handler outcome / input sanitization; §4.7.ON-026 depends here.
- **[handler-contract.md §4.6]** — silent-hang detection; §4.9.ON-040 obligates its naming here.
- **[handler-contract.md §4.7]** — secrets injection; §4.7.ON-022 depends here.
- **[handler-contract.md §4.10]** — handler binary launch and commit-hash check; §4.2.ON-005 aligns.
- **[handler-contract.md §4.11]** — skill-injection obligation; §4.7.ON-025 depends here.
- **[control-points.md §6.5]** — policy schema; §4.5.ON-018 and §4.7.ON-025 depend here.
- **[control-points.md §6.8]** — config loading precedence; §4.1.ON-004 depends here.
- **[control-points.md §6.9]** — budget control point; §4.11.ON-045 and §4.11.ON-046 depend here.
- **[control-points.md §6.11]** — skill declaration; §4.7.ON-025 depends here.
- **[process-lifecycle.md §8.1]** — per-project daemon scope; §4.10.ON-042 depends here.
- **[process-lifecycle.md §8.2]** — startup sequence; §4.1.ON-003 co-owns the failure-mode catalog.
- **[process-lifecycle.md §8.3]** — command surface; §4.1.ON-002, §4.10.ON-041, §4.11.ON-046 reference here.
- **[process-lifecycle.md §8.4]** — queue-empty behavior; §4.1.ON-004 references the re-query cadence knob.
- **[reconciliation.md §9.1]** — reconciliation-as-workflow; §4.3.ON-010 carve-out depends here.
- **[reconciliation.md §9.2, §9.2a]** — reconciliation categories and action mapping; §4.3.ON-014 operator override applies here.
- **[reconciliation.md §9.3]** — Cat 0 detector; §4.1.ON-003 startup catalog depends here.
- **[reconciliation.md §9.4, §9.4a]** — investigator-agent contract and wall-clock budget; §4.8.ON-032 separates dispatch time from execution time.
- **[reconciliation.md §9.5b]** — verdict execution; §4.3.ON-014 operator-override attaches here.
- **[beads-integration.md §10.1, §10.3, §10.6, §10.8]** — Beads is the queue; bead-ID propagation; `br` CLI adapter; §4.4.ON-015–§4.4.ON-017 depend here.
- **[workspace-model.md §5.1]** — workspace leasing; §4.7.ON-024 depends here.
- **[workspace-model.md §5.3]** — session-log metadata; §4.4.ON-015 references.

### 9.2 Reverse dependencies

> INFORMATIVE: Reverse dependencies are computed on demand from the foundation corpus. Populated at finalize.

### 9.3 Co-references (read-only consumption)

- **[docs/foundation/project-level/build-practices.md §Branch model]** — direct-to-main MVH development; §5.ON-INV-002 consumes this operational posture without depending on the build-practices doc's internals.
- **[docs/foundation/problem-space.md §Locked decisions]** — locked decision #10 (operator controls between tasks) and locked decision #12 (no DTW); §4.3 and §4.8 derive from these positions.
- **[STATUS.md §Decisions Locked In]** — the ten locked decisions; amendment protocol per [architecture.md §1.5] applies to relaxing any requirement here that rests on a locked decision.

## 10. Conformance

### 10.1 Conformance profiles

**Core MVH.** An implementation conforming to Core MVH MUST pass every requirement ON-001 through ON-046 and every invariant ON-INV-001 through ON-INV-005, subject to the following bootstrap allowances:

- `ON-002` (exit-code taxonomy), `ON-003` (startup failure-mode catalog), `ON-004` (config inventory), `ON-014` (reconciliation operator override), `ON-020` (harmonik upgrade contract), and `ON-041` (multi-daemon commands) are **obligation** requirements that are satisfied when their named artifact exists in this spec or a cross-referenced spec. The §8 taxonomy table satisfies ON-002; production of a co-owned startup failure-mode catalog by spec-draft satisfies ON-003; production of a config-inventory appendix (see OQ-ON-001) satisfies ON-004; the naming convention in ON-014 plus [reconciliation.md §9.5b] satisfies ON-014; §4.6 and §7.3 satisfy ON-020; §4.10 ON-041 satisfies the obligation. Implementations consume these artifacts; they do not re-satisfy them per implementation.

**Post-MVH extensions.** Full binary signing (§4.2.ON-006), metrics exposition format (§4.10.ON-043), distributed tracing (§4.10.ON-044), and the multi-tenancy concerns of §4.10.ON-042 are deferred additive extensions; none is required for Core MVH conformance.

### 10.2 Test-surface obligations

During bootstrap (before `testing.md` exists) test obligations are named in prose. Each requirement group's test obligation:

- **ON-001 — ON-004 (exit codes and obligations).** Negative-path tests covering every exit code listed in §8; static-check test verifying that every requirement with a cross-reference to §4.1 resolves to a §8 entry.
- **ON-005 — ON-006 (integrity gate).** Upgrade scenario tests with matching and mismatched commit hashes; verify `operator_upgrade_rejected` on mismatch; verify post-MVH signing extension does not break MVH conformance.
- **ON-007 — ON-014 (operator-control semantics).** State-machine scenario tests enumerating every transition in §7.1; verify reconciliation-carve-out queueing of pause during `reconciling`; verify improvement-pause auto-resumes; verify `stop --immediate` aborts in-flight runs and emits `run_failed` with class `canceled` on next restart.
- **ON-015 — ON-017 (queue-format compat).** Upgrade scenario tests with N-1, N, and N+1 Beads schemas; verify startup failure on unsupported; verify `br` adapter localizes a simulated Beads breaking change.
- **ON-018 — ON-019 (schema compat window).** Cross-artifact compat tests: write at N, read at N-1, for every listed artifact; verify migration release refusal to install without a pause.
- **ON-020 — ON-021 (upgrade contract).** Full upgrade scenario tests covering all five sub-obligations of ON-020; verify cross-version state preservation across same-version and N-1 upgrades.
- **ON-022 — ON-029 (security and shutdown).** Secret-redaction tests covering every sink; schema-level tests asserting no field is typed as `Secret` in any payload; sandbox escape-attempt tests; graceful-shutdown scenario tests for all seven steps; `stop --immediate` tests verifying steps 2–3 are skipped.
- **ON-030 — ON-033 (restart RTO).** Restart scenario benchmarks measuring SIGTERM-to-`ready` across representative hardware at MVH scale; verify 30s p95 nominal and 300s hard ceiling.
- **ON-034 — ON-040 (observability envelope).** Per-subsystem-conformance tests verifying typed event emission, structured log emission, health-check interface presence, liveness heartbeat cadence, audit-record derivation, and mechanism-tagging of every observability operation; obligation test for silent-hang detection per [handler-contract.md §4.6].
- **ON-041 — ON-046 (multi-daemon, budgets).** Multi-daemon scenario tests verifying `harmonik list`, flag-based targeting, and machine-ceiling enforcement; budget tests verifying declared-enforced-attributed pipeline.

Migration to `[testing.md §<layer>]` cross-references occurs within one revision cycle once testing.md lands; this obligation is tracked in OQ-ON-002.

### 10.3 Excluded conformance claims

- This spec does NOT grant conformance over: the specific CLI flag surface (deferred per §2.2); operator dashboard UI (deferred per §2.2); full binary signing (§4.2.ON-006 deferred); Prometheus / OpenTelemetry wire formats (§4.10.ON-043 deferred); distributed tracing (§4.10.ON-044 deferred); per-tenant cost attribution (§4.10.ON-042 deferred).
- This spec does NOT guarantee throughput or latency bounds beyond the restart RTO of §4.8; subsystem-internal performance targets are owned by subsystem specs.
- This spec does NOT own the implementation of pause / stop / upgrade state-machine transitions; those live in the S01 orchestrator-core subsystem spec. This spec is normative for the state set, the allowed transitions, the emitted events, and the between-task invariant.

## 11. Open questions

#### OQ-ON-001 — Config inventory authoritative location

Question: Should the normative config inventory obligated by ON-004 live as an appendix to this spec, as a sibling file under `specs/operator-nfr/config-inventory.md`, or as a top-level `specs/config-inventory.md` cross-referenced from every affected spec?
Owner: foundation-author
Blocks: ON-004 completeness (the obligation is named; the artifact location is undecided).
Default-if-unresolved: Sibling file under `specs/operator-nfr/config-inventory.md`, cross-referenced from every knob-declaring spec. Migration to a top-level spec if the inventory grows beyond ~300 lines or serves multiple non-NFR owners.

#### OQ-ON-002 — Migrate test-obligation prose to testing.md references

Question: §10.2 currently names test obligations in prose. The template expects cross-references to `[testing.md §<layer>]` once testing.md lands.
Owner: foundation-author
Blocks: none (MVH prose obligations are in place).
Default-if-unresolved: Keep prose obligations; migrate within one revision cycle after testing.md is finalized.

#### OQ-ON-003 — Machine-ceiling coordinator implementation locus

Question: ON-041 obligates a machine-level agent-subprocess ceiling enforced by a shared lock or a machine-level coordinator process. Which is the MVH shape — filesystem-based shared-counter lock (simpler, has races at scale) or a single coordinator daemon (more complex, needs its own lifecycle)?
Owner: foundation-author
Blocks: ON-041 implementation shape.
Default-if-unresolved: Filesystem-based shared-counter lock at `~/.harmonik/machine-ceiling.lock` with advisory locking. Revisit if contention measurements show thrash.

#### OQ-ON-004 — Concurrent-operator-attach arbitration

Question: Multiple simultaneous `harmonik attach` sessions are allowed per [process-lifecycle.md §8.3]. Two operators simultaneously issuing `pause` or `upgrade` — which wins, and is there a lock?
Owner: foundation-author
Blocks: none (OVERVIEW.md §8 names this as a known silence).
Default-if-unresolved: The second command observes the state-machine in the post-first-command state and either no-ops (both paused) or errors (if incompatible). No explicit lock. Single-operator MVH assumption makes this acceptable; revisit when multi-operator deployments appear.

#### OQ-ON-005 — RTO ceiling behavior — notify-only vs auto-escalate

Question: ON-032 criterion 3 says "operator is notified" on 300-second breach. Is notification the only action, or should the daemon auto-escalate (e.g., refuse to come `ready` until operator acknowledges)?
Owner: foundation-author
Blocks: none (default below).
Default-if-unresolved: Notify-only via `daemon_degraded` event; daemon continues reconstruction and transitions `ready` when complete. Operator intervention is permitted but not required.

## 12. Revision history

| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-04-23 | 0.1.0 | foundation-author | Initial draft from components.md Component 7 + round-2 amendments. |

## A. Appendices

### A.3 Rationale

**Why operator controls are spec'd as semantics, not as a CLI surface.** The CLI surface (flag names, argument order, output formatting) is churny and should be free to change without triggering a normative revision of every subsystem that depends on operator-control semantics. Splitting semantics into this spec and surface into a separate spec (deferred) protects the between-task invariant from flag-renaming noise. See [problem-space.md §Non-goals] Q-F1 resolution.

**Why the between-task invariant is a locked decision.** Allowing pause or upgrade to abort in-flight runs would make every run's durability contract contingent on "unless operator pauses mid-run," which destroys the checkpoint-trail guarantee of [execution-model.md §4.5] and the state-reconstruction contract of [execution-model.md §4.7]. `stop --immediate` is the single carve-out because emergency abort is a real operational need; forcing graceful shutdown in every case would leave operators unable to recover from a genuinely stuck daemon. This is locked decision #10 and reopening it requires strong new evidence.

**Why N-1 and not N-2 or wider.** N-1 is the smallest window that lets operators upgrade without coordinating the daemon with the on-disk state. Wider windows (N-2, N-3) increase reader code complexity without proportional benefit — MVH is single-operator, single-machine, and the migration cost at a break is a short operator-paused ritual. Post-MVH the window may widen if multi-operator or fleet-scale deployments appear.

**Why the 300-second RTO hard ceiling is non-negotiable.** Below 300 seconds an operator can reasonably wait at the terminal for startup to complete; above 300 seconds the operator will start investigating, and the daemon must be able to distinguish "still starting" from "stuck." The ceiling is the boundary where the degraded-notification obligation kicks in. Choosing a ceiling is unavoidable; 300s matches operator-patience research (cited from [problem-space.md] recon findings) and leaves headroom above the nominal 30s p95 target.

**Why multi-tenancy is deferred, not solved.** Per-project daemon isolation is a genuine MVH answer for the common solo-developer case, and it scales gracefully to "a few projects at once on one machine." What it does NOT address — shared LLM quotas, shared skill installations, shared operator identity — is not tractable at MVH without a machine-level coordinator that would itself need a process-lifecycle contract, a failure story, and a reconciliation protocol. Deferring is cheaper than committing to a half-designed coordinator. The acknowledgment in §4.10.ON-042 is load-bearing: "deferred ≠ dismissed" is the posture that keeps the door open for post-MVH amendment without re-opening the foundation.
