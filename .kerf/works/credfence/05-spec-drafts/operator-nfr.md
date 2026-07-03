# Operator NFR

```yaml
---
title: Operator NFR and Control Semantics
spec-id: operator-nfr
requirement-prefix: ON
spec-category: foundation-cross-cutting
status: reviewed
spec-shape: requirements-first
version: 0.4.3
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-05-15
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
- `harmonik upgrade` contract obligation (binary-source mechanism, hash check, drain interaction, cross-version state contract, fd-passed socket continuity).
- Graceful-shutdown ordering and `stop --immediate` semantics.
- Restart RTO target (30s p95 nominal, 300s hard ceiling) measured SIGTERM → daemon `ready`.
- Resource budgets cross-subsystem (declared in policy, enforced at dispatch, attributed in observability).
- Multi-daemon commands obligation (`harmonik list`, `--socket` / `--cwd` / `--daemon-id` identification, machine-level agent-subprocess ceiling) and multi-tenancy deferral acknowledgment.
- Startup failure-mode catalog obligation.
- Silent-hang detection obligation (named here, specified in [handler-contract.md §4.6]).
- Reconciliation operator override (pre-execution verdict confirmation) cross-reference.
- Exit-code taxonomy obligation and config-inventory obligation.

### 2.1a Operational-posture assumption (formerly ON-INV-002)

- **Direct-to-main MVH development.** This spec assumes foundation specs and MVH-target subsystem specs operate under direct-to-main development per `docs/foundation/project-level/build-practices.md`. No PR-based merge gate is the MVH enforcement model; agent-reviewer-every-commit + post-push CI surfacing is the discipline. Subsystem specs SHOULD NOT design contracts that assume a pre-merge human review gate for MVH. Post-MVH restoration of PR-based gating is an additive concern to subsystem design (it affects process, not contract shape). This is an operational posture, not a runtime invariant; it is captured here as a scope assumption to replace the retired ON-INV-002.

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
- Reconciliation category classifier internals — owned by [reconciliation/spec.md §4.2, §4.3] and [reconciliation/spec.md §8]; this spec consumes the reconciliation status for pause carve-out only.
- Beads SQLite schema internals — managed upstream; this spec names the overlay-compat contract, not the bead wire schema.

## 3. Glossary

- **task (operator sense)** — one complete run of a workflow, from `run_started` to `run_completed` or `run_failed`. The operator-facing term "task" = the execution-model's `run` (see [execution-model.md §3 run]); this spec resolves the naming: operator surfaces use "task" for user-friendliness, specs use `run` for precision. (see §4.3)
- **in_flight(run)** — predicate naming runs not yet terminal. Mechanically: `in_flight(run) ≡ run.state ∉ {completed, failed, canceled}`, where `run.state` is the orchestrator's authoritative in-memory model of the run's lifecycle state per the run-state convention of [execution-model.md §3] (in-flight-run glossary entry) and the state-machine of [execution-model.md §7.1]. The predicate is evaluated against the orchestrator-core's in-memory run table; subsystems holding observational views MUST consult the authoritative table via the `dispatch-status` JSON-RPC method per [process-lifecycle.md §4.1 PL-003a] rather than rely on a parallel cache. A run in a "parked" lifecycle position (loaded bead not yet dispatched) is bead-state-only and is NOT a run; it is excluded from `in_flight(run)` by virtue of having no `run.state` to evaluate. (see §4.3)
- **between-task invariant** — pause and upgrade operator controls complete in-flight runs (per the glossary predicate above) before taking effect; only `stop --immediate` aborts in-flight runs. (see §4.3)
- **improvement-pause** — a subtype of pause with a scheduled or triggered onset, used by the S09 improvement cycle; resumes automatically when the improvement loop completes. (see §4.3)
- **RTO** — recovery time objective; measured SIGTERM → daemon `ready` status event per §4.8.
- **N-1 compatibility** — a writer at schema version N must produce artifacts readable by a reader at schema version N-1 (the immediately prior published version). Applies to event schemas, checkpoint format, and queue per §4.4–§4.6 and [event-model.md §4.7].
- **commit-hash integrity gate** — the MVH binary-integrity check: the to-be-installed binary's source-commit hash must match the operator-supplied expected hash. Deferred post-MVH is full binary signing. (see §4.2)
- **health-check interface** — a function returning `health_status ∈ {OK, degraded, failed}` with an optional reason string; every subsystem exposes one per §4.9.
- **liveness signal** — a heartbeat event on a defined cadence; missing heartbeats beyond tolerance trigger a degraded classification per §4.9.
- **audit record** — a subset of traces where `actor_role` is privileged and `chosen_action` affected policy, role permissions, or budget. (see §4.9)
- **reconciliation carve-out** — the rule that pause does not interrupt reconciliation workflows; pauses issued during `reconciling` are queued and applied when reconciliation completes. Normative statement at §4.3.ON-010. (see §4.3)
- **drain** — the ordered shutdown sequence on `stop --graceful` or SIGTERM that completes in-flight runs to their next checkpoint before exiting. (see §4.7)
- **multi-daemon coordination** — operator commands that list, target, and bound multiple per-project daemons on one machine (`harmonik list`, flag-based daemon identification, machine-level agent-subprocess ceiling). (see §4.10)
- **`degraded`** — operator-observable health status. Used in two scopes: (a) **subsystem-level `degraded`** per ON-036/ON-037 — a specific subsystem fails its health probe or misses heartbeats; aggregated by ON-036's health surface. (b) **daemon-level `degraded`** per §6.1 (the `DaemonStatus` enum value) — the daemon as a whole has entered the `degraded` status for one of: Cat 0 prerequisite failure (per [process-lifecycle.md §4.3 PL-010]), RTO ceiling breach (per ON-031), or the silent-hang-fan-out aggregator. The daemon-level `degraded` is the aggregation; subsystem-level `degraded` is the input. Both surfaces are operator-visible; consumers MUST check the source field on the emitted event to distinguish.

## 4. Normative requirements

### 4.a Subsystem envelope

> INFORMATIVE: ON is `spec-category: foundation-cross-cutting` per §0; per [architecture.md §4.0] AR-052, foundation-cross-cutting specs are EXEMPT from the runtime-subsystem envelope obligation of AR-053. This §4.a block is published as a voluntary declaration shaped to the AR-053 template because ON DOES emit events and reference cross-subsystem surfaces, and downstream subsystem specs benefit from a canonical statement of what ON contributes to the shared event bus and cross-cutting interface surface. Envelope requirement IDs use the reserved `ON-ENV-NNN` range so they do not consume topical `ON-NNN` ID space.

#### ON-ENV-001 — Foundation-cross-cutting envelope declaration

(a) Events produced (emitted by the operator-control path of §4.3 / §4.6, registered in [event-model.md §8.7]):

- `operator_pause_status` — paired-phase per [event-model.md §8.9(h)].
- `operator_resuming`
- `operator_stopped`
- `operator_upgrading`
- `operator_upgrade_completed`
- `operator_upgrade_rejected`
- `operator_command_rejected`
- `dispatch_deferred`

(b) Events consumed (subscriber / observer classes; observation only):

- `daemon_ready` from [event-model.md §8.7] — §4.3 entry-gate.
- `reconciliation_category_assigned` from [event-model.md §8.6] — §4.3.ON-010 pause carve-out.
- `budget_warning` / `budget_exhausted` / `budget_accrual` from [event-model.md §8.4] — §4.11 attribution aggregation (read-side).
- `agent_warning_silent_hang` from [event-model.md §8.3] — §4.9.ON-040 operator-observable consequence.

(c) Types introduced (cross-subsystem) — none. This spec references existing types owned by other specs (Event envelope per [event-model.md §6.1]; Policy schema per [control-points.md §6.3]; Workspace record per [workspace-model.md §6.1]).

(d) Handlers implemented — none (ON is foundation-cross-cutting; no runtime handler package).

(e) State owned — none persistently. The operator-control state-machine state (§7.1) is daemon-in-memory; reconstruction on restart is per §4.8.ON-030 via git + Beads.

(f) Control points provided — none directly. ON cites [control-points.md §4.5] (budgets), [control-points.md §4.7] (precedence), [control-points.md §6.3] (policy schema), and [control-points.md §4.11] (skills).

(g) NFRs inherited / overridden — ON IS the NFR home; no inheritance. Runtime-subsystem specs inherit ON-001–ON-049 and ON-INV-001 / ON-INV-003 / ON-INV-005 / ON-INV-006 per §10.1.

(h) Boundary classification per operation:

| Operation | `Tags:` | Axes |
|---|---|---|
| `pause` / `upgrade` command ingress | mechanism | llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent |
| drain-step execution (ON-027 1–7) | mechanism | llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent |
| commit-hash integrity check | mechanism | llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent |
| restart-RTO measurement | mechanism | llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent |
| operator-observability emission (structured logs, events, heartbeat) | mechanism | llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent |

Tags: mechanism

### 4.1 Exit-code taxonomy and failure-mode catalog obligations

#### ON-001 — Operator-observable exit codes are structured

Every operator-invoked harmonik command (`daemon`, `attach`, `enqueue`, `status`, `pause`, `stop`, `upgrade`, and all multi-daemon commands per §4.10) MUST return a structured exit code. Zero MUST mean success. Non-zero codes MUST map one-to-one to a failure category declared in the exit-code taxonomy of §8. The mapping MUST be stable: a given code MUST refer to the same category across releases within the N-1 compatibility window.

Tags: mechanism

#### ON-002 — Exit-code taxonomy obligation

The spec-draft pass MUST produce a normative exit-code taxonomy naming every non-zero exit code emitted by any operator command. The taxonomy MUST specify, for each code: the failure category, the operator-observable symptom, the emitted event type (if any), and the operator remediation pointer. The taxonomy lives in §8 of this spec; cross-references from other specs (e.g., [process-lifecycle.md §4.10 harmonik status]) MUST resolve to §8 entries.

Review-loop termination paths (`iteration_cap_hit`, `BLOCK` verdict, `no_progress_detected`) MUST NOT be assigned a new top-level exit-code category. These are run-level terminations, not daemon-level exits; the operator-observable signal is the bead's `needs-attention` label per §4.3.ON-009a plus the `review_loop_cycle_complete` event's `completion_reason` field per [event-model.md §8.1]. Implementations MUST NOT extend §8 to accommodate these termination paths.

Tags: mechanism

#### ON-003 — Startup failure-mode catalog obligation

The spec-draft pass MUST produce a normative startup failure-mode catalog, co-owned with [process-lifecycle.md §4.2]. For every daemon-startup prerequisite failure (git bad state, Beads SQLite unavailable, Beads schema version unsupported, checkpoint schema version unsupported, stale-pidfile race, filesystem unwritable, disk-full during checkpoint commit, socket bind failure), the catalog MUST specify: detection rule, exit code per §4.1.ON-001, operator remediation procedure, emitted event type per [event-model.md §6.3] (envelope at [event-model.md §6.1]), and the reconciliation Cat 0 classification per [reconciliation/spec.md §4.3]. The catalog is the authoritative input to `harmonik status`'s infrastructure-prereq reporting per [process-lifecycle.md §4.10].

Tags: mechanism

#### ON-004 — Config inventory obligation

The spec-draft pass MUST produce a normative config inventory enumerating every operator-configurable knob referenced across foundation specs. For each knob, the inventory MUST specify: the precedence layer (runtime override / operator-policy file / workflow definition / default, per [control-points.md §4.7] CP-037), the default value, the allowed range or enumeration, and the change-takes-effect semantics (next operator pause, immediate, next daemon start, etc.). At minimum the inventory covers the timer-flush cadence ([event-model.md §4.4]), budget warning threshold ([control-points.md §4.5]), drain timeout (§4.7), RTO thresholds (§4.8), Cat 0 pre-check retry cadence ([reconciliation/spec.md §4.3]), per-Cat reconciliation budgets ([reconciliation/spec.md §4.4]), the `workflow_mode` knob per §4.1.ON-004a, and the credential & spend-governance knobs per §4.1.ON-004b–ON-004g (credential injection source, per-day USD cap, max-runs, Pi model tiers, daemon `claude` baseline, dry-run mode).

Tags: mechanism

#### ON-004a — Workflow-mode config-inventory entry

The config inventory of §4.1.ON-004 MUST include an entry for the `workflow_mode` knob with the following fields:

- **Allowed enumeration:** `{single, review-loop, dot}`.
- **Default value:** `single` (built-in fallback).
- **Precedence layers** (four tiers, evaluated highest-to-lowest at claim time):
  1. Per-task `workflow:<mode>` label on the bead per [beads-integration.md §4.3].
  2. Per-project policy (reserved tier; not populated at MVH).
  3. Daemon default per [process-lifecycle.md §4.1].
  4. Built-in fallback `single`.
- **Change-takes-effect semantics:** per-task at claim time (the resolved mode is sealed into the Run record per [execution-model.md §4.3] and is immutable for the run's lifetime); daemon default on next daemon start.
- **Runtime tunability:** NOT runtime-tunable per §4.3.ON-013d.
- **Iteration cap (review-loop):** hardcoded at 3 for MVH; NOT operator-tunable.

Tags: mechanism

#### ON-004b — Credential injection-source config-inventory entry

The config inventory of §4.1.ON-004 MUST include an entry for the `supervise start` credential injection source per [credential-isolation.md §4.4 CI-006]:

- **Knob:** the source from which `harmonik supervise start` injects the credential into the Pi cognition (holder) process.
- **Allowed values:** an explicit operator env export, or a gitignored credential file at repo root (`.env`) read ONLY by `supervise start`.
- **Default value:** the gitignored repo-root `.env` (read only by `supervise start`; never read by the daemon; never unioned into a child env).
- **Precedence layers** (highest-to-lowest): (1) explicit operator env export; (2) gitignored repo-root `.env`; (3) no source resolves → fail-closed error (the holder process MUST NOT start unauthenticated).
- **Change-takes-effect semantics:** next `supervise start`.
- **Runtime tunability:** NOT runtime-tunable (boot-time).

Tags: mechanism

#### ON-004c — Per-day USD budget cap config-inventory entry

The config inventory of §4.1.ON-004 MUST include an entry for the unified per-day spend cap per [cognition-loop.md §4.11 CL-090]:

- **Knob:** `FLYWHEEL_BUDGET_USD_PER_DAY` / `--budget-usd-per-day`. Caps the unified meter that sums Pi turns AND daemon-spawned `claude` session cost.
- **Allowed values:** a positive number (USD), or `unlimited` / empty for an explicit opt-out.
- **Default value:** FINITE (recommended 20 USD; the default MUST NOT be unbounded).
- **Precedence layers** (highest-to-lowest): (1) runtime flag `--budget-usd-per-day`; (2) `FLYWHEEL_BUDGET_USD_PER_DAY` env; (3) finite built-in default.
- **Change-takes-effect semantics:** next daemon/loop start; the cap total resets at the local-midnight day-boundary rollover.
- **Runtime tunability:** NOT runtime-tunable mid-day at v0.1.

Tags: mechanism

#### ON-004d — Per-day max-runs ceiling config-inventory entry

The config inventory of §4.1.ON-004 MUST include an entry for the per-day max-runs ceiling per [cognition-loop.md §4.11 CL-090a]:

- **Knob:** the per-day max-runs ceiling (count of daemon `run_started` events since the last day-boundary rollover). The loss-proof backstop alongside the USD cap.
- **Allowed values:** a positive integer.
- **Default value:** a FINITE count (the default MUST NOT be unbounded).
- **Precedence layers:** runtime flag > env > finite built-in default (mirrors ON-004c).
- **Change-takes-effect semantics:** next daemon/loop start; the counter resets on the same day-boundary rollover as the USD total.
- **Runtime tunability:** NOT runtime-tunable mid-day at v0.1.

Tags: mechanism

#### ON-004e — Pi model-tier config-inventory entry

The config inventory of §4.1.ON-004 MUST include an entry for the Pi judgment-model tiers per [cognition-loop.md §4.11 CL-090b]:

- **Knob:** `FLYWHEEL_MODEL_TIER1`, `FLYWHEEL_MODEL_TIER2`, `FLYWHEEL_MODEL_TIER3` (the model IDs the cognition loop uses per tier).
- **Allowed values:** any valid Anthropic model ID per tier.
- **Default value:** tier-1 Haiku, tier-2 Sonnet, **tier-3 (judgment) Sonnet** — Opus is gated behind an explicit operator opt-in (the cost-posture default).
- **Precedence layers** (highest-to-lowest): (1) the `FLYWHEEL_MODEL_TIER*` env override; (2) the extension built-in default.
- **Change-takes-effect semantics:** next loop start (wired only at the composition root per [cognition-loop.md §4.12 CL-100]).
- **Runtime tunability:** NOT runtime-tunable at v0.1.

Tags: mechanism

#### ON-004f — Daemon `claude` baseline-model config-inventory entry

The config inventory of §4.1.ON-004 MUST include an entry for the single operator-facing default model the daemon's `claude` implementer/reviewer sessions run at:

- **Knob:** the daemon `claude` baseline model default.
- **Allowed values:** any valid model ID.
- **Default value:** the daemon built-in baseline (Sonnet/medium at v0.1).
- **Precedence layers** (highest-to-lowest): (1) per-bead `model:` label (existing per-task override); (2) the operator-facing daemon-baseline default; (3) the daemon built-in.
- **Change-takes-effect semantics:** next daemon start. Hot-reload of the baseline is a SHOULD, NOT a MUST; the normative obligation is that ONE operator-facing default exists. The exact configuration surface (env name vs config field) is an implementation choice and is NOT bound to a specific literal by this spec.
- **Runtime tunability:** the MUST is a single operator default; hot-reload is optional.

Tags: mechanism

#### ON-004g — Dry-run / plan-only mode config-inventory entry

The config inventory of §4.1.ON-004 MUST include an entry for the daemon `--dry-run`/plan-only mode per [cognition-loop.md §4.11 CL-090d]:

- **Knob:** `--dry-run` (daemon plan-only mode).
- **Allowed values:** present (plan-only) / absent (live).
- **Default value:** OFF (live).
- **Behavior:** previews the intended spawn set (per bead: would-launch implementer + reviewer at model X, across M beads) WITHOUT launching any `claude`, reading the credential source ([credential-isolation.md §4.4 CI-006]), or emitting spend. Mirrors the `harmonik queue dry-run` validate-without-execute behavior.
- **Change-takes-effect semantics:** per invocation.
- **Runtime tunability:** per-invocation flag.

Tags: mechanism

### 4.2 Integrity gate for binary install

#### ON-005 — Commit-hash integrity gate is the MVH binary-install check

The pause-to-upgrade path (§4.3, §4.6) MUST verify the to-be-installed binary's source-commit hash against an operator-supplied expected hash before the daemon's exec-replacement step. The check MUST fail-closed: on mismatch or missing hash, the daemon MUST NOT exec-replace and MUST remain in the `paused` state with an `operator_upgrade_rejected` event emitted per [event-model.md §8.7]. Handler binaries installed via [handler-contract.md §4.10] MUST ALSO carry the commit-hash check; this requirement names the daemon-level invariant.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### ON-005a — Commit-hash source

The daemon's `actual_hash` for the integrity gate of ON-005 MUST be computed from a build-time embedded ldflags stamp (`-ldflags="-X main.commitHash=<sha>"` at compile). Binaries lacking the embedded stamp MUST fail the integrity gate immediately on `harmonik upgrade` invocation with §8 code 14 (`upgrade-hash-mismatch`); the failure mode `failure_mode=binary-stamp-missing` distinguishes this case from operator-supplied-hash-mismatch. The operator's `expected_commit_hash` source is operator-discretion (Slack, release-page, `git rev-parse`); ON-005's gate is a version-identity check, not a cryptographic-integrity check; signing is post-MVH per ON-006.

Tags: mechanism

#### ON-006 — Full binary signing is deferred post-MVH

Full cryptographic signing of binaries (code-signing certificates, reproducible build attestations, signature-chain verification) is deferred post-MVH. Conforming MVH implementations MUST NOT be required to verify signatures beyond the commit-hash match of §4.2.ON-005. Post-MVH introduction of signing is additive and does NOT invalidate MVH conformance.

Tags: mechanism

### 4.3 Operator-control semantics between tasks

#### ON-007 — Operator "task" equals execution-model "run"

For operator-facing documentation, CLI output, and event payload human-readable fields, "task" MUST denote one complete run of a workflow, from `run_started` to `run_completed` or `run_failed` per [execution-model.md §4.3]. Normative spec text, event payload field names, and internal logs MUST use `run` / `run_id`. Surfaces that render human-facing copy MAY translate `run` to "task"; wire formats MUST NOT.

Tags: mechanism

#### ON-008 — Pause and upgrade respect the between-task invariant

An operator `pause` or `upgrade` command issued while the daemon status is `ready` MUST NOT interrupt any in-flight run (per §3 `in_flight(run)`). The daemon MUST transition to `pausing`, allow every in-flight run to proceed to its next durable checkpoint per [execution-model.md §4.5], AND complete the full drain sequence of §4.7.ON-027 (all eight steps) before transitioning to `paused`. The `pausing → paused` transition is gated on drain-completion: entry into `paused` is forbidden until (a) no run satisfies `in_flight(run)` AND (b) every drain step of ON-027 has completed (or the drain-timeout escalation path of §4.7.ON-029 has fired). `upgrade` further transitions `paused` → `upgrading` → (exec-replace) → `running` under the contract of §4.6.

Tags: mechanism

#### ON-008a — Credential injection and budget-exhaustion operator surface

`harmonik supervise start` MUST inject the credential into the Pi cognition (holder) process from the non-committed scoped source per [credential-isolation.md §4.4 CI-006] (config-inventory entry ON-004b), so a fresh boot authenticates without a manual operator export. The daemon process and every daemon-spawned `claude` child MUST NOT receive the credential per [credential-isolation.md §4.1 CI-001].

The `budget-paused` pause-reason ([cognition-loop.md §6]) — entered when the unified per-day spend meter exhausts per [cognition-loop.md §4.11 CL-090] and the budget-exhaustion handler-pause policy fires ([handler-pause.md §4 HP-012]) — MUST be surfaced to the operator per §9 alongside `circuit-tripped`. The operator clears the budget-exhaustion handler pause via the existing handler-resume surface (`harmonik supervise resume`); reset is not automatic.

Tags: mechanism

For runs with `workflow_mode = single` or `workflow_mode = dot` (per [handler-contract.md §4.2 HC-006]), the durable checkpoint at which the run yields to the drain gate is the between-task checkpoint per [execution-model.md §4.5]; this is the default semantics. For runs with `workflow_mode = review-loop`, the durable checkpoint set is EXTENDED to include intra-run iteration boundaries: the interval between emission of a `reviewer_verdict` event (per [event-model.md §8.1]) and the next `implementer_resumed` event of the same cycle is a legitimate pause checkpoint. A `pause` issued mid-iteration of a `review-loop` run MUST be honored at the next such iteration-boundary checkpoint OR at the end of the cycle, whichever arrives first; the pause MUST NOT be deferred beyond the next iteration boundary. The amended pause checkpoint set applies ONLY when the run's resolved `workflow_mode` is `review-loop`; the original between-task invariant is unchanged for `single` and `dot` modes. `stop --immediate` aborts mid-iteration per §4.3.ON-009 regardless of mode; the run is left in the standard canceled-and-reconciled state.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### ON-009 — `stop --immediate` is the only control that aborts in-flight runs

`stop --immediate` and SIGKILL (treated equivalently) MUST abort in-flight runs (per §3 `in_flight(run)`). Aborted runs MUST emit `run_failed` with class `canceled` per [execution-model.md §8.4] once the daemon restarts and reconciliation classifies them per [reconciliation/spec.md §4.3]. No other operator control is permitted to abort in-flight runs; proposals to add `pause --immediate` or `upgrade --immediate` MUST be rejected as violations of §4.3.ON-008.

Tags: mechanism

#### ON-009a — Needs-attention queue drain discipline

A bead closed under any non-success `review-loop` termination reason — `iteration_cap_hit` (the cycle exhausted the hardcoded iteration cap of 3 per §4.1.ON-004a), reviewer `BLOCK` verdict (per [workspace-model.md §4.7 WM-027a] and [event-model.md §8.1a.3] reviewer phase emission), or `no_progress_detected` — MUST be marked with the bead label `needs-attention` per [beads-integration.md §4.3]. The daemon's ready-work query per [beads-integration.md §4.5] MUST treat `needs-attention`-labeled beads as out-of-scope for automatic claim. There MUST NOT be an auto-retry path: no subsystem MAY re-dispatch a `needs-attention`-labeled bead without operator intervention. Operators drain the queue manually by either (a) removing the `needs-attention` label (which restores claimability on the next ready-work scan) after triage, or (b) closing the bead as `wontfix`. Phantom auto-retry logic — any code path that removes the label or re-dispatches the bead without an operator-issued command — is a structural violation of this requirement.

**Terminology note.** The "queue" in this requirement is the *needs-attention bead set* — a Beads-side concept defined by the `needs-attention` label per [beads-integration.md §4.3]. It is NOT the daemon's execution queue defined in [queue-model.md §1] and persisted at `.harmonik/queue.json`. The two are layered: the needs-attention set governs which beads an orchestrator MAY enqueue into the execution queue; the execution queue governs which queued beads the daemon dispatches. Operator drain actions in this requirement (label removal, `wontfix` closure) mutate the bead set, not the execution queue.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### ON-010 — Reconciliation carve-out: pause queues during `reconciling`

Pause MUST NOT interrupt reconciliation workflows per [reconciliation/spec.md §4.1]. The daemon's status progression is `starting` → (optional `degraded`) → `reconciling` → `ready` per [process-lifecycle.md §4.2]; the between-task invariant of §4.3.ON-008 applies only after the daemon reaches `ready`. An operator pause issued during `reconciling` MUST be queued and applied at the boundary event "all reconciliation runs have either resumed into normal flow or produced a verdict."

Tags: mechanism

#### ON-011 — Operator-control state machine

The daemon MUST implement the operator-control state machine defined in §7.1. States are `running`, `pausing`, `paused`, `resuming`, `stopped` (terminal-recoverable via `start`), and `upgrading`. Improvement-pause is NOT a distinct state: it reuses `pausing` / `paused` with the `pause_reason=improvement` discriminator per §4.3.ON-012. Transitions and emitted events are normative per §7.1.

Operator-control state-machine transitions MUST be serializable: the daemon MUST hold a single mutex (or equivalent CAS primitive) guarding the transition function. Concurrent operator commands (per OQ-ON-004) are arbitrated by mutex acquisition order; the loser observes the post-winner state. The mutex MUST be acquired before evaluating a transition guard and held until the transition's emission and durable-marker write per ON-030a complete.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### ON-012 — Improvement-pause is a subtype of pause

The S09 improvement cycle MUST NOT introduce a new operator-control state. An improvement-pause MUST transition `running` → `pausing` → `paused` via the same path as an operator pause, sharing the identical state table with the `pause_reason` discriminator distinguishing operator-initiated (`pause_reason=operator`) from improvement-initiated (`pause_reason=improvement`). The additional invariant for improvement-pause is that the `paused` → `resuming` transition is triggered automatically when the improvement loop completes (no operator action required). The discriminator is carried on `operator_pause_status` per [event-model.md §6.3] structured-fields mechanism; it is NOT a separate pair of state-machine states. The earlier framing of separate `improvement-pausing` / `improvement-paused` states is retired; all runs go through one `running → pausing → paused` chain with the `pause_reason` payload field identifying the origin.

Tags: mechanism

#### ON-013 — Operator-control events are emitted per state transition

The daemon MUST emit one typed event per operator-control state transition. Per the paired-phase rule at [event-model.md §8.9(h)], pause `pausing` and `paused` are lifecycle phases of a single pause and MUST be emitted as a single paired-phase event type, `operator_pause_status`, with a `status ∈ {pausing, paused}` field (emitted on each status transition, forbidden as keepalive re-emission):

- `operator_pause_status` with `status=pausing` — emitted on `running` → `pausing` (including the improvement-pause path, where `pause_reason=improvement` is tagged per the §7.1 guard); see [event-model.md §8.7.6] for payload shape; the emission site is responsible for tagging the pause reason via EV's structured-fields mechanism per [event-model.md §6.3].
- `operator_pause_status` with `status=paused` — emitted on `pausing` → `paused` (including the improvement-pause path), i.e., only after all ON-027 drain steps have completed per §4.3.ON-008; see [event-model.md §8.7.6] for payload shape. On the `status=paused` emission (drain-completion), the emission MUST include a `drain_summary` field naming each ON-027 step's completion timestamp and any drain-timeout escalations. Cross-spec coordination request to EV: extend §8.7.6 payload to carry `drain_summary?` as an optional field.
- `operator_resuming` — emitted on `paused` → `resuming`.
- `operator_stopped` — emitted on entry to `stopped`; the `mode` field distinguishes `graceful` vs `immediate` per [event-model.md §8.7.8].
- `operator_upgrading` — emitted on `paused` → `upgrading`; the `upgrade_version` field carries the operator-supplied expected commit hash per [event-model.md §8.7.9].
- `operator_upgrade_completed` — emitted on `upgrading` → `running` after exec-replace.
- `operator_upgrade_rejected` — emitted when §4.2.ON-005 commit-hash check fails or cross-version schema check refuses.
- `operator_command_rejected` — emitted when an operator command is invalid for the current state-machine state (§8 code 16).
- `dispatch_deferred` — emitted when a dispatch is blocked by §4.10.ON-041 machine-ceiling or other deferral condition (§8 code 18).

Payload schemas live in [event-model.md §8.7]; emission timing is normative here.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### ON-013a — Per-command supervision

Every operator-command-dispatch goroutine (the goroutine handling `pause`, `stop`, `upgrade`, `attach`, and the `queue-submit` / `queue-append` / `queue-status` / `queue-dry-run` JSON-RPC methods per [process-lifecycle.md §4.1 PL-003a]) MUST install a `defer recover()` barrier. On panic, the barrier MUST: (a) emit `operator_command_failed{command, panic_class, run_id?}` (cross-spec coordination request to EV); (b) revert any partial state-machine transition by clearing the `.harmonik/daemon.state` marker's pending-transition field; (c) escalate to `degraded` if the panic occurred during a drain step. The top-level PL-018a panic barrier remains the daemon's outer-defense; ON-013a is the operator-command inner-defense.

Tags: mechanism

#### ON-013c — Operator-command idempotency

Operator commands MUST be idempotent on no-op transitions: a `pause` issued while already `paused` MUST return success (exit code 0) with `operator_pause_status{status=paused}` re-emitted at most once per command (deduplicated via session_id). A `stop` issued while `stopped` MUST return success with no event emission. A `resume` issued while `running` MUST return success with no transition. The operator's CLI MUST NOT see a different exit code for "already in target state" vs "transitioned successfully."

Tags: mechanism

#### ON-013d — Workflow mode is not an operator-control surface

The daemon's `workflow_mode` (per §4.1.ON-004a and [handler-contract.md §4.2 HC-006]) MUST be observable via `harmonik status` — both the daemon's default mode and the per-run resolved value for any in-flight run — but MUST NOT be mutable via any operator command. There MUST NOT be a `harmonik set-mode` command or any equivalent runtime tuning surface; there MUST NOT be a `pause-then-set-mode` workflow. Operators wishing to change the daemon default MUST restart the daemon with a different config; operators wishing to change a per-task value MUST edit the bead's `workflow:<mode>` label (via `br update` per [beads-integration.md §4.3]) BEFORE the bead is claimed. Once a bead is claimed, the resolved `workflow_mode` is sealed into the Run record per [execution-model.md §4.3] and is immutable for the run's lifetime. The iteration cap (3 for `review-loop`, per §4.1.ON-004a) MUST NOT be operator-tunable at runtime. Proposals to introduce a runtime mode-mutation surface MUST be rejected as violations of this requirement.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### ON-014 — Reconciliation operator override (pre-execution verdict pause)

The spec-draft pass MUST produce a normative per-reconciliation-workflow policy option to pause the daemon's verdict-execution step (per [reconciliation/spec.md §4.5]) until an operator confirms or vetoes the verdict. The naming convention for the operator commands is: `harmonik confirm-verdict <run_id>` / `harmonik veto-verdict <run_id> [--promote-to escalate-to-human]`. Default: execution proceeds without operator confirmation; operators opt in by policy. This obligation applies to all investigator-dispatched reconciliation categories (Cat 2, 3, 6a per [reconciliation/spec.md §4.2] and [reconciliation/spec.md §8.12]). Foundation owns the naming convention; [reconciliation/spec.md §4.5] owns the execution-step specifics.

Tags: mechanism

### 4.4 Queue-format compatibility contract

#### ON-015 — Beads is the queue; overlay schema is harmonik's half

Beads is the catalog of work — the authoritative store for bead identity, status, and `blocks` edges per [beads-integration.md §4.1]–[beads-integration.md §4.3]. The daemon's execution queue (dispatch order and group structure) is the execution plan layered on top, owned by [queue-model.md §2] and persisted in `.harmonik/queue.json` per ON-018. Queue-format compatibility MUST be the union of (a) Beads schema compat (managed upstream) AND (b) harmonik's overlay schema compat: the `Harmonik-Bead-ID` trailers in checkpoint commits per [execution-model.md §4.4], the bead-ID references in events per [event-model.md §6.3], and the session-log bead-ID metadata per [workspace-model.md §4.7]. Both halves MUST be N-1 readable.

Tags: mechanism

#### ON-016 — Queue schema version check on daemon startup

On daemon startup, the daemon MUST check both the Beads SQLite schema version and harmonik's overlay schema version against the running binary's supported set (current N and prior N-1). An unsupported version MUST cause startup failure with the exit code assigned to category "queue-format-unsupported" per §8, naming the required migration release in the failure event payload. The check is part of the startup failure-mode catalog of §4.1.ON-003.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### ON-017 — Beads pre-1.0 breakage is absorbed, not forked

Harmonik MUST version-pin Beads per the external-inputs protocol (problem-space §External inputs) and MUST route all Beads interactions through the `br`-CLI adapter of [beads-integration.md §4.2]. A Beads breaking change MUST produce one localized adapter update; harmonik MUST NOT fork Beads. This requirement is a structural obligation on the adapter boundary, not on every caller.

Tags: mechanism

### 4.5 Schema compatibility window

#### ON-018 — N-1 compatibility is the MVH compat window

Every versioned on-disk or wire artifact declared by foundation specs — event-envelope schema ([event-model.md §6.1]), event payload schemas ([event-model.md §6.3]), checkpoint trailers and sibling files ([execution-model.md §4.4]), queue overlay (§4.4.ON-015), queue execution plan ([queue-model.md §3], persisted as `.harmonik/queue.json` with a `schema_version` field), policy schema ([control-points.md §6.3]) — MUST maintain N-1 readability. A reader pinned to version N-1 MUST successfully parse and interpret artifacts written by version N, with additive fields treated as unknown but non-fatal. Breaking changes MUST be accompanied by a migration release and MUST NOT be introduced mid-run; they MUST land at an operator pause per §4.3.

Tags: mechanism

#### ON-019 — Migration releases are operator-paused boundaries

A migration release (any release that bumps an N-1-covered schema version to break the compat window — i.e., a change no longer readable by readers at the current N) MUST require an operator pause before installation. The `harmonik upgrade` contract of §4.6 MUST refuse to exec-replace into a migration release unless the daemon is in the `paused` state AND the on-disk state's schema version is within the new binary's supported set. Installing a migration release MUST NOT auto-migrate on-disk state; a dedicated migration workflow (post-MVH) is the path.

Tags: mechanism

### 4.6 `harmonik upgrade` contract

#### ON-020 — `harmonik upgrade` contract obligation

The spec-draft pass MUST produce a normative `harmonik upgrade` contract specifying, at minimum: (a) binary-source mechanism (repo-relative path and/or explicit hash-supply flag); (b) operator-supplied expected commit-hash check procedure per §4.2.ON-005; (c) drain-vs-reconciliation interaction (what `upgrade` does if reconciliation workflows are in-flight per §4.3.ON-010 — MUST queue; MUST NOT interrupt); (d) cross-version state contract (what upgrade does if the new binary's schema-version is N-1, N, or N+1 vs the on-disk state — MUST succeed for same and N-1; MUST refuse for broader mismatches per §4.5.ON-019); (e) **socket continuity.** The daemon MUST preserve the bound listener fd across exec-replace via fd-passing per [process-lifecycle.md §4.9 PL-027(iii)]: outgoing daemon clears `FD_CLOEXEC` and passes the fd via `HARMONIK_LISTENER_FD`; new binary adopts via `net.FileListener` and re-sets `FD_CLOEXEC`. Adoption is gap-free; clients observe no `ECONNREFUSED` window. ON does NOT mandate a separate retry mechanism on the operator-facing surface; the daemon-side mechanism of PL-027(iii) is the contract; (f) **Rollback as first-class.** On `harmonik upgrade --rollback` invocation while the daemon is `paused` after a successful upgrade, the daemon MUST exec-replace back to the prior binary (located at `.harmonik/daemon.binary.prev`, written during the original upgrade per (g)). The rollback follows the same exec-replace mechanism as PL-027; the `expected_commit_hash` for rollback is the prior binary's stamp. Rollback during the live upgrade window (between drain-complete and exec-replace) is not supported; the operator MUST resume and retry the upgrade or stop the daemon; (g) **Post-exec-replace failure.** If the new binary's startup fails per [process-lifecycle.md §4.2 PL-005] step 0, the daemon's pidfile and socket are stale. The operator MUST be able to recover by invoking `harmonik upgrade --rollback`, which removes the stale pidfile/socket, restores `.harmonik/daemon.binary.prev`, and starts the prior binary. The original `.harmonik/daemon.upgrading` marker per ON-020a is consumed during rollback to determine the prior binary's commit hash for the integrity gate. The contract lives in §4.6 of this spec; referring specs cross-reference here.

Tags: mechanism

#### ON-020a — Upgrade-intent durable marker

When `harmonik upgrade` enters the drain phase, the daemon MUST atomically write `.harmonik/daemon.upgrading` containing: (a) the operator-supplied `expected_commit_hash` per ON-005; (b) the upgrade-initiation timestamp; (c) the operator's session_id (per ON-013b daemon-instance handshake). The write MUST follow temp+rename+fsync atomicity. On daemon startup, [process-lifecycle.md §4.2 PL-005] step 0 MUST read this marker; if present and the on-disk binary's commit hash matches the marker's `expected_commit_hash`, startup proceeds normally and the marker is removed; if present and the hash does not match, the daemon MUST refuse startup with §8 code 14 (`upgrade-hash-mismatch`) and emit `daemon_startup_failed{failure_mode=upgrade-hash-mismatch-on-restart}`.

The PL-027(iv) marker, currently informative, is hereby promoted to normative per this requirement. PL's next revision MUST update PL-027(iv) accordingly (cross-spec coordination request).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### ON-020b — Binary-source mechanism

The `harmonik upgrade` command MUST accept the to-be-installed binary via two mechanisms: (1) a repo-relative or absolute path supplied as a positional argument (`harmonik upgrade <path>`), and (2) an explicit `--binary <path>` flag that overrides any positional argument. Exactly one source MUST be provided; absence of any binary-source argument MUST fail with §8 code 16 (`operator-control-invalid-state`) and a diagnostic naming the missing argument. The daemon MUST NOT fetch or derive the binary path from the environment; path resolution is operator-discretion. The resolved path MUST be an executable regular file; symlinks are permitted and followed; directories and non-executable files MUST fail immediately with §8 code 16.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### ON-020c — Operator-supplied hash check for upgrade

The `harmonik upgrade` command MUST accept the operator-supplied expected commit hash via the `--expected-hash <sha>` flag per §4.2.ON-005 and ON-005a. The flag is REQUIRED; absence MUST fail with §8 code 14 (`upgrade-hash-mismatch`) and `failure_mode=expected-hash-missing`. The daemon computes the to-be-installed binary's `actual_hash` from its build-time embedded ldflags stamp per ON-005a; if the stamp is absent, the daemon MUST fail with §8 code 14 / `failure_mode=binary-stamp-missing`. On mismatch between `expected_hash` and `actual_hash`, the daemon MUST emit `operator_upgrade_rejected` per [event-model.md §8.7] and remain in the `paused` state. The hash check MUST execute before any exec-replace step and before the upgrade-intent marker of ON-020a is written.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### ON-020d — Drain-vs-reconciliation interaction for upgrade

If a reconciliation workflow (per [reconciliation/spec.md §4.2]) is in-flight when the operator issues `harmonik upgrade`, the upgrade MUST queue behind the active reconciliation: the daemon enters `pausing` per §4.3.ON-008 and waits for reconciliation to complete before transitioning to `paused`. The daemon MUST NOT interrupt any in-flight reconciliation workflow (per §4.3.ON-010 — reconciliation carve-out applies). While `pausing`, the daemon MUST NOT accept new bead dispatches. Once reconciliation completes and the eight drain steps of §4.7.ON-027 finish, the daemon transitions to `paused` and the upgrade proceeds per §7.3. `stop --immediate` aborts reconciliation per §4.3.ON-009 regardless of upgrade queuing; `stop --immediate` during a queued upgrade MUST discard the queued upgrade and abort runs normally.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### ON-020e — Cross-version state contract

Before exec-replacing the new binary, the daemon MUST read the new binary's declared supported-schema set (via a `--schema-version-query` introspection flag on the new binary) and compare it to the on-disk state's schema versions across all N-1-covered artifacts: event envelope, checkpoint trailer, queue overlay, and policy schema. The upgrade MUST succeed (proceed to exec-replace) if the on-disk schema version is within the new binary's supported set, covering the same-version (N) and N-1 cases. The upgrade MUST refuse (emit `operator_upgrade_rejected{reason=schema-incompatible}`, exit §8 code 15 `upgrade-schema-incompatible`, remain `paused`) if any artifact's on-disk version is outside the new binary's supported set (the N+2 or wider-mismatch case). A migration release per §4.5.ON-019 is the operator path for schema-breaking upgrades; the daemon MUST NOT auto-migrate on-disk state.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### ON-020f — Socket fd-passing continuity across exec-replace

The outgoing daemon MUST preserve the bound Unix listener fd across exec-replace using fd-passing per [process-lifecycle.md §4.9 PL-027(iii)]: before `exec.Command`/`syscall.Exec`, the outgoing daemon MUST clear the `FD_CLOEXEC` flag on the listener fd and pass the fd number via the `HARMONIK_LISTENER_FD` environment variable to the new process. The new binary MUST adopt the fd via `net.FileListener` at startup before accepting new connections, and MUST re-set `FD_CLOEXEC` on the adopted fd immediately after adoption. The adoption MUST be gap-free: from the outgoing daemon's last `accept` to the new daemon's first `accept`, no `ECONNREFUSED` window is observable by CLI clients. ON does NOT mandate a separate client-side retry mechanism; the gap-free fd-passing is the full contract. Any failure to clear `FD_CLOEXEC` or adopt the fd MUST cause the new binary to fail startup with §8 code 6 (`socket-bind-failed`).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### ON-020g — Rollback as first-class operator command

The daemon MUST support `harmonik upgrade --rollback` as a first-class command. `--rollback` is valid only while the daemon is `paused` after a successful upgrade (i.e., `.harmonik/daemon.binary.prev` exists, written atomically by the original upgrade before exec-replace). On `harmonik upgrade --rollback`, the daemon MUST exec-replace back to `.harmonik/daemon.binary.prev` using the same fd-passing mechanism of ON-020f; the `expected_commit_hash` for the rollback integrity check is derived from the `.harmonik/daemon.upgrading` marker per ON-020a (which records the prior binary's hash). Rollback during the live upgrade window (between drain-complete and exec-replace) is not supported; the operator MUST either resume and retry `harmonik upgrade`, or `stop --immediate` the daemon. `harmonik upgrade --rollback` while `running` (not post-upgrade `paused`) MUST fail with §8 code 16 (`operator-control-invalid-state`). `.harmonik/daemon.binary.prev` MUST be written atomically (temp+rename+fsync per [workspace-model.md §4.7 WM-026]) by the outgoing daemon before exec-replace; absence of the file MUST fail rollback with §8 code 16.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### ON-020h — Post-exec-replace failure recovery

If the new binary's startup fails per [process-lifecycle.md §4.2 PL-005] step 0 after exec-replace, the daemon pidfile (`.harmonik/daemon.pid`) and socket (`.harmonik/daemon.sock`) are stale — no running daemon owns them. The operator MUST be able to recover without manual filesystem surgery by invoking `harmonik upgrade --rollback` from any shell, which performs the following steps in order: (1) removes the stale pidfile and socket if they exist; (2) reads `.harmonik/daemon.upgrading` to determine the prior binary's `expected_commit_hash`; (3) validates `.harmonik/daemon.binary.prev` against that hash per ON-020c; (4) starts the prior binary as the new daemon process (not exec-replace, since there is no running daemon to exec-replace from; the rollback CLI starts the prior binary directly); (5) removes `.harmonik/daemon.upgrading` and `.harmonik/daemon.binary.prev` on successful prior-binary startup. If `.harmonik/daemon.upgrading` is absent during post-failure rollback, the operator MUST be told that the recovery path is unavailable and MUST start the prior binary manually. The CLI MUST distinguish in-process rollback (from a live `paused` daemon per ON-020g) from post-failure rollback (no running daemon) by the presence or absence of a reachable daemon socket.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### ON-021 — Upgrade preserves in-flight run recoverability

An `upgrade` operation MUST NOT make any in-flight run unrecoverable. The recoverability invariant holds iff entry into `paused` implies drain-completion (per §4.3.ON-008 and §4.7.ON-027): because the `pausing → paused` transition is gated on all eight drain steps completing AND no run satisfying `in_flight(run)` remaining, the state the new binary inherits MUST be reconstructible from git + Beads per [execution-model.md §4.7] and the restart RTO of §4.8. The cross-version state contract of §4.6.ON-020 MUST reject upgrades that would violate this invariant.

Tags: mechanism

### 4.7 Secrets redaction and graceful shutdown

#### ON-022 — Secrets are injected at handler launch and never logged

Secrets (API keys, tokens, credentials) MUST be injected at handler launch per [handler-contract.md §4.7]. Secrets MUST NOT appear in the event log under any circumstance. Secrets MUST NOT appear in session logs without redaction. Redaction is mechanism-tagged and MUST be enforced pre-emission: handler implementations MUST apply prefix-regex matching and per-handler redaction patterns before any write to a durable sink (event bus, session log, audit record).

The redactor MUST fail-closed: if the redactor itself panics or returns an error during emission of any event/log/audit-record carrying typed `Secret` fields, the daemon MUST abort the emission, MUST emit `redaction_failed{event_type, run_id?, error_class}` (cross-spec coordination request to EV: add `redaction_failed` to §8 taxonomy), and MUST NOT fall through to a non-redacted emission. Repeated redactor panics within T_redact_fail (default 60s, operator-tunable) MUST escalate the daemon to `degraded` per ON-037.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### ON-023 — Secrets-redaction compile-time payload-schema check

Event payload schemas declared per [event-model.md §6.3] MUST NOT declare any field typed as `Secret` or equivalent. A compile-time check (lint pass or generated-code assertion) MUST reject any payload schema that would carry a secret through the event bus. This closes the redaction-obligation loop: redaction cannot be forgotten at an emission site because no emission site is permitted to carry secret-typed fields.

Tags: mechanism

#### ON-024 — Command-execution sandbox invariant

Agents MUST execute within a leased workspace directory per [workspace-model.md §4.3]. Escape attempts — symlinks resolving outside the workspace, path-traversal patterns, git hooks sourced from untrusted paths — MUST be prevented. Specific enforcement is owned by the subsystem specs for S04 (handler runner) and S06 (workspace manager); this spec states the cross-cutting invariant.

Tags: mechanism

#### ON-025 — Network egress and skill-injection policy enforcement

Network egress MUST be governed by policy per [control-points.md §6.3]; a policy MAY whitelist domains for agent access. Skills provisioned per [handler-contract.md §4.11] MUST honor the egress policy: a provisioned skill that would require egress to a non-whitelisted domain MUST fail provisioning, and the handler MUST emit a `skills_provisioned` event (per [event-model.md §8.3]) listing only the skills actually installed. Skills requiring filesystem access outside the workspace MUST fail provisioning per §4.7.ON-024.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### ON-026 — Prompt-injection defense is handler-owned

Input sanitization for user-provided content in the input workspace MUST be the handler's responsibility per [handler-contract.md §4.1]. Handlers MUST NOT let user-provided content in the input workspace alter the agent's system-prompt instructions. This spec states the obligation; the enforcement mechanism lives in handler-contract.

Tags: mechanism

#### ON-027 — Graceful-shutdown ordering (eight drain steps)

On `stop --graceful` or SIGTERM — AND on `pause` / `upgrade` per the §4.3.ON-008 drain-gate — the daemon MUST execute the shutdown/drain sequence in the following order, each step completing before the next begins: (1) the daemon stops advancing the queue: no new dispatches are issued from the active group, and pending groups do not advance; in-flight runs proceed per step (2); the queue's status field transitions to `paused-by-drain` per [queue-model.md §5]; (2) every run for which `in_flight(run)` holds (per §3) proceeds to its next checkpoint, then suspends per [execution-model.md §4.5]; (3) agent runners wait for handler subprocesses to complete or reach the drain timeout; (3a) the `br`-CLI adapter intent-log per [beads-integration.md §4.10 BI-029/BI-030] MUST be drained to completion: every pending intent-log entry's terminal-transition write MUST resolve (success or BI-031 status-check classification) before step 4 proceeds. Drain failures (e.g., `BrUnavailable` per [beads-integration.md §6.1a]) escalate to step 4 with the failed entries marked for next-restart Cat 3a routing; (4) event bus flushes pending events (fsync per [event-model.md §4.4]); (5) memory layer flushes indexing; (6) workspace manager unlocks leased workspaces and cleans up incomplete adze setups per [workspace-model.md §4.3]; (7) orchestrator exits with code 0 if clean, or the exit code for "drain-timeout-escalated" per §8 if any step exceeded its bound. In the pause/upgrade path the exit of step 7 is replaced by "enter `paused`" (no process exit); the step sequence is identical. Completion of ALL eight steps is the precondition for the `pausing → paused` transition of §4.3.ON-008.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### ON-028 — `stop --immediate` skips drain steps 2–3

On `stop --immediate` or SIGKILL, the daemon MUST skip steps 2 and 3 of §4.7.ON-027. In-flight agent subprocesses MUST be killed (SIGTERM with a short bounded window, then SIGKILL). In-flight run state is recoverable on next startup via checkpoint + reconciliation per [reconciliation/spec.md §4.2], but the in-flight agent subprocesses are not gracefully stopped.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### ON-029 — Drain timeout is operator-configurable

The drain timeout of §4.7.ON-027 is configurable per-step (`timeout.step_2`, `timeout.step_3`, etc.); the §7.2 pseudocode is normative for the per-step apportionment. A single `drain_timeout_total` MAY be declared as the sum-bound; if declared, individual `timeout.step_N` MUST sum to ≤ `drain_timeout_total`. The drain timeout knobs MUST be operator-configurable per the config inventory of §4.1.ON-004. Default values are specified in the config inventory; the change-takes-effect semantics is "next daemon start" (drain timeouts are read once at startup).

Tags: mechanism

#### ON-027a — Drain step atomicity and crash-recovery

The eight steps of ON-027 MUST execute strictly sequentially on a single goroutine; no parallel execution between steps is permitted. Each step's completion MUST be marked durably (in the `.harmonik/daemon.state` marker per ON-030a, augmented with a `drain_step_completed` field) before the next step begins. On crash mid-drain, the next daemon startup MUST resume drain from the next-uncompleted step rather than restart from step 1; resumption MUST be idempotent on completed steps (each step's effect is replay-safe).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

### 4.8 Restart RTO

#### ON-030 — Restart reconstruction path

Daemon restart MUST reconstruct the in-memory model by walking the git checkpoint trail per [execution-model.md §4.7] and querying Beads via `br` per [beads-integration.md §4.2]. The JSONL event log MUST NOT be replayed for state reconstruction (per [event-model.md §4.4] and [event-model.md §4.5], and locked decision #12 — no DTW). Reconciliation workflows MUST spawn for in-flight runs (per §3 `in_flight(run)`) per [reconciliation/spec.md §4.2].

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### ON-030a — Pause-state durable marker

The operator-control state machine of §7.1 MUST persist its current state via an atomic-written marker file `.harmonik/daemon.state` containing the current `DaemonStatus` plus the pause-reason discriminator (when applicable; one of `operator`, `improvement`, `upgrade-prepare`). The write MUST use temp+rename+fsync+parent-fsync per [workspace-model.md §4.7 WM-026] and MUST happen synchronously on every state transition that produces a `paused`, `pausing`, `upgrade-prepare`, or `stopped` state. On daemon startup, [process-lifecycle.md §4.2 PL-005] step 0 MUST read `.harmonik/daemon.state`; if the marker indicates a paused or upgrade-prepare state, the daemon MUST initialize into that state rather than `running`, preserving operator intent across crashes. The marker MUST be removed on clean transition to `running` or process exit via the normal-startup path.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### ON-031 — Restart RTO target

Restart MUST reach the pre-restart state within the RTO target, measured from SIGTERM (or crash) to the daemon emitting the `ready` status event per [process-lifecycle.md §4.2]. The MVH RTO target is **30 seconds nominal fixture target** (p95 under the standard fixture defined in §4.8.ON-032 criterion 1) with a **300-second hard ceiling** (§4.8.ON-032 criterion 3). The sensor for this requirement is a restart-RTO test harness backed by the standard fixture (`≤ 500 open beads`, `≤ 50 in-flight runs`, git-log depth `≤ 10,000` commits, `≤ 100` Cat-3-pending runs, `≤ 10` active investigator workflows, sized per §4.8.ON-032 criterion 1); see OQ-ON-005 for residual ambiguity on auto-escalate vs notify-only behavior on ceiling breach.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=best-effort; replay-safety=safe; idempotency=idempotent

#### ON-032 — RTO criteria and hard ceiling

The RTO target of §4.8.ON-031 MUST be set against the following criteria:

- **Criterion 1 — operator expectation (nominal fixture).** MVH assumes single-operator, single-instance deployment. Target: **≤ 30 seconds at 95th percentile** under the standard fixture. **Standard fixture for RTO measurement:** `≤ 500 open beads, ≤ 50 in-flight runs, git-log depth ≤ 10,000 commits since the oldest open bead's first checkpoint, ≤ 100 reconciliation-Cat-3-pending runs, ≤ 10 active investigator workflows.` These bounds are the harness's reference state; deviations from the fixture MAY produce out-of-target measurements without breaching ON-INV-005's invariant. This target is the sensor's green-threshold.
- **Criterion 2 — reconstruction complexity.** Restart time is proportional to (a) git-log walk depth since the oldest open bead's first checkpoint, and (b) Beads query latency for ready + in-flight bead sets. JSONL event count is NOT a restart-time factor (it is not read on restart per §4.8.ON-030).
- **Criterion 3 — hard ceiling.** **300 seconds.** Beyond this the operator MUST be notified (the daemon MUST enter `degraded` reporting `reconciling` with progress markers; operator intervention is permitted). Criterion 3 is non-negotiable. Criterion 1 MAY be relaxed with reason (documented in OQ-ON-005) if measurements show 30 seconds is unachievable at MVH scale.

Reconciliation-workflow dispatch time is part of the RTO; reconciliation-workflow execution time (investigator-agent LLM calls per [reconciliation/spec.md §4.4]) is NOT — it is bounded by that workflow's own policy per [reconciliation/spec.md §4.4].

Tags: mechanism

#### ON-033 — RTO measurement boundary

The RTO of §4.8.ON-031 MUST be measured using a monotonic-corrected clock source: SIGTERM receipt and `daemon_ready` emission timestamps MUST both carry a `_at_ns_since_boot` companion field (cross-spec coordination request to EV: add `shutdown_at_ns_since_boot` and `ready_at_ns_since_boot` to `daemon_shutdown` and `daemon_ready` payloads in §8.7.2 and §8.7.3 respectively). On boot-transition (post-reboot), monotonic-clock comparison is undefined; the RTO MUST be marked `rto_undefined` for the boot-transition cycle and excluded from the p95 measurement. SIGKILL terminations have no `daemon_shutdown` emission; the RTO is `rto_undefined` for those cycles as well. Measurement MUST NOT start from `harmonik daemon` invocation time (which excludes crash-to-restart-trigger latency); the boundary is SIGTERM (or daemon crash timestamp recorded by the OS) to the daemon's `ready` status event emission per [process-lifecycle.md §4.2].

Tags: mechanism

#### ON-053 — Post-panic forensic file

When the daemon's top-level panic barrier ([process-lifecycle.md §4.6 PL-018a]) intercepts a panic and exits with §8 code 19 (`runtime-panic`), the daemon MUST atomically write a forensic file to `.harmonik/panic-<timestamp>.log` containing: (a) the Go runtime panic message and stack trace; (b) the daemon's PID, PGID, project_hash, and binary commit hash; (c) the timestamp of the panic in both wall-clock (RFC 3339) and `time.Since(boot)` monotonic forms; (d) the last-emitted run_id / node_id / event_id (best-effort from the in-memory cursor). The write MUST follow the temp+rename+fsync+parent-fsync atomicity discipline of [workspace-model.md §4.7 WM-026]. The file is NOT consumed by reconciliation; it exists for operator post-mortem inspection. Multiple panic files MAY accumulate; ON does NOT mandate cleanup at this revision (tracked under OQ-ON-010).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

### 4.9 Observability envelope

#### ON-034 — Every subsystem emits typed events

Every subsystem MUST emit events per [event-model.md §6.3]. Event taxonomy additions introduced by a subsystem MUST be declared via the subsystem envelope (per [architecture.md §4.4] AR-013) and registered per [event-model.md §4.6]. Every event MUST carry the four-axis and mechanism/cognition tags per [architecture.md §4.1].

Tags: mechanism

#### ON-035 — Every subsystem emits structured logs

Every subsystem MUST emit structured logs. Unstructured log lines (free-form text only) are forbidden at spec-declared emission points. Structured logs are the MVH substrate for observability; Prometheus / OpenTelemetry wire formats are post-MVH per §4.10.ON-043.

Structured-log wire format is OWNED by this spec (promoted from an unreferenced `[event-model.md §3.8]` target; the original citation did not resolve in EV). The minimum structured-log shape is a newline-delimited JSON record carrying the fields: `ts` (RFC 3339 with ms), `log_schema_version` (string, current `"1.0"`), `level` ∈ `{debug, info, warn, error}`, `subsystem`, `source_subsystem` (per [event-model.md §4.9 EV-034a]), `run_id?`, `node_id?`, `event_id?` (UUIDv7 correlation per [event-model.md §4.1] when the log corresponds to an event emission), `msg` (short human-readable), and `fields` (map of typed values). The `event_id` correlation field MUST be present when a log record is the subsystem's own emission of an event tracked in JSONL. Secrets-redaction per §4.7.ON-022 MUST apply to structured logs before emission; the redaction direction is producer-side, and consumers MUST NOT re-redact. Log files MUST rotate at 100 MiB or 24 hours (whichever comes first), with prior files moved to `.harmonik/logs/<subsystem>-<rotated_at>.jsonl`. The `log_schema_version` is under N-1 compatibility per ON-INV-001 (cross-spec coordination request: add structured logs to ON-018's enumeration; track as new OQ-ON-011 if the addition is too invasive for this revision). The detailed schema (including typed-field enumeration and the consumer-side parser contract) and the sensor for compliance are deferred to a dedicated `quality-checks.md` / logging-wire-format work and tracked in OQ-ON-007.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### ON-035a — Review-loop cycle observability via JSONL

Operator visibility into `review-loop` cycle progression MUST be supplied entirely via the existing JSONL event-consumption path (`harmonik status`, `harmonik logs`, `jq` against `events.jsonl`). The cycle's observability surface is the set of review-loop event types declared in [event-model.md §8.1a] — `implementer_resumed`, `reviewer_launched`, `reviewer_verdict`, `iteration_cap_hit`, `no_progress_detected`, and `review_loop_cycle_complete`. No new operator command surface (e.g., `harmonik review-status`) MUST be introduced; review-loop information is rendered inline in `harmonik status` when a run's resolved `workflow_mode` is `review-loop`. The operator's diagnostic recipe for a stuck cycle is: filter `events.jsonl` by `run_id` and intersect with the named event types. Pause-reason discriminators reported by `harmonik status` per §4.3.ON-054 MUST NOT add a `review-loop`-specific reason; review-loop pause checkpoints (per §4.3.ON-008 amendment) reuse the existing `operator-pause` reason with the iteration-boundary checkpoint observable in the `drain_summary` field.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### ON-036 — Every subsystem exposes a health-check interface

Every subsystem MUST expose a health-check interface returning `health_status ∈ {OK, degraded, failed}` with an optional reason string. The orchestrator MUST aggregate subsystem health into a harmonik-wide health status exposed via `harmonik status` per [process-lifecycle.md §4.10].

Tags: mechanism

#### ON-037 — Every subsystem emits liveness heartbeats

Every subsystem MUST emit a liveness heartbeat event on a defined cadence. Missing heartbeats beyond tolerance MUST trigger a `degraded` classification for that subsystem and raise the aggregated harmonik-wide health accordingly. The cadence and tolerance are operator-configurable per §4.1.ON-004.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### ON-038 — Audit records are a subset of traces

Audit records MUST be produced as a subset of transition records per [execution-model.md §4.4]: the subset where `actor_role` is in a privileged role (per [architecture.md §4.8]) AND the `chosen_action` affected policy, role permissions, or budget. No separate audit-log store is introduced; audit is a query over the transition-record sibling files and their projections.

Tags: mechanism

#### ON-039 — All observability operations are mechanism-tagged

Every observability operation (health-check evaluation, heartbeat emission, metric emission, log emission, audit-record derivation) MUST be mechanism-tagged per [architecture.md §4.1, §4.2]. Any operation that requires cognition to produce the observability signal MUST be represented as a separate verification node per [architecture.md §4.3], NOT folded into the observability protocol.

Tags: mechanism

#### ON-040 — Silent-hang detection obligation

Detection of silent hangs (handler subprocesses producing no output and no heartbeat within a bounded window) is obligated under [handler-contract.md §4.6]. This spec names the obligation to ensure the operator-observable consequence (the `agent_warning_silent_hang` event per [event-model.md §8.3] — canonical name, emitted per [handler-contract.md §7.1] — and a subsystem `degraded` classification per §4.9.ON-037) is not silently deferred. The enforcement mechanism lives in handler-contract.

If a drain timeout escalation per ON-029 produces a SIGKILL to a still-running agent subprocess, the daemon MUST synthesize an `agent_warning_silent_hang{reason=drain_forced, run_id, node_id}` event prior to the SIGKILL emission, even if no prior silent-hang detection had fired. This ensures the silent-hang surface is consistent: every agent subprocess that did not exit cleanly produces a silent-hang record. The synthesis MUST occur within drain step 4's wait window per ON-027.

Tags: mechanism

#### ON-055 — Subscribe is read-only observation, not a control surface

The `subscribe` socket op (and its CLI wrapper `harmonik subscribe`) is a long-running observation surface that streams NDJSON event envelopes to the connecting client. It MUST NOT mutate daemon state. It MUST NOT abort, pause, resume, or otherwise affect any in-flight run. It is therefore exempt from §4.3.ON-INV-006's "no new control surface" prohibition by construction: it is not a control surface.

Specifically: `subscribe` (a) registers a transient observer-class consumer on the live event bus per [event-model.md §4.2 EV-012]; (b) emits only connection-local payloads (`heartbeat`, `subscription_gap`) that are NOT bus-published and do NOT appear in `events.jsonl`; (c) applies drop-oldest back-pressure to slow subscribers so a wedged client cannot stall the bus emission goroutine (EV-012 invariant). The op is allowlisted in `internal/specaudit/oninv006_no_control_surface_bypass_test.go` with a citation to this section.

Tags: mechanism

### 4.10 Multi-daemon coordination and multi-tenancy deferral

#### ON-041 — Multi-daemon commands obligation

The spec-draft pass MUST produce normative definitions for machine-level operator commands: (a) `harmonik list` — enumerate running daemons machine-wide with project path, pid, socket path, and current status; (b) daemon-identification flags on all daemon-communicating commands (`stop`, `pause`, `attach`, `status`, `upgrade`, and `queue` with its subcommands `submit`, `status`, `append`, `dry-run`) — at minimum `--socket <path>`, `--cwd <path>`, and `--daemon-id <id>`; (c) a machine-level agent-subprocess ceiling mechanism — a cross-daemon bound on concurrently running agent subprocesses enforced by a shared lock or a machine-level coordinator process. These commands are the minimum operator-visible concession foundation makes to multi-tenancy in MVH.

**Scope clarification.** The per-daemon concurrency ceiling per [process-lifecycle.md §4.5] PL-014a applies WITHIN one daemon; the machine-level ceiling in (c) above is a SEPARATE cross-daemon bound enforced by a shared-lock or coordinator mechanism. The multi-tenancy deferral of §4.10.ON-042 applies to multi-daemon coordination concerns (shared LLM budgets, shared identities, shared skill registries) only; the per-daemon ceiling and the machine-level ceiling are both in-scope for MVH. That is, "deferred" covers multi-daemon coordination policy questions, not the ceiling mechanisms themselves.

**Daemon-discovery mechanism.** `harmonik list` discovers running daemons by scanning the operator's home directory for the pattern `**/.harmonik/daemon.pid` (default scope: `$HOME` and any path declared in `$HARMONIK_PROJECT_ROOTS` env var, colon-separated). For each discovered pidfile, the command queries `.harmonik/daemon.sock` via JSON-RPC `status` (per [process-lifecycle.md §4.1 PL-003a]) to obtain live state. Pidfiles whose socket is unreachable or whose pidfile-PID does not respond to `kill(pid, 0)` are reported as `stale` rather than `running`.

**Output columns.** `harmonik list` output rows MUST include the columns: `daemon_id` (project_hash from PL-006a), `project_root`, `pid`, `status` (per §6.1), `socket_path`, `started_at`, `last_exit_code` (the most recent non-zero exit observed by the daemon's parent process per PL pidfile lineage; "n/a" if not observable), `budget_summary` (per ON-049 attribution; rolled up to per-daemon totals — `tokens_consumed`, `wall_clock_consumed`, `iterations_consumed`). Operators MUST be able to filter by `status` and project-root substring.

Tags: mechanism

#### ON-042 — Multi-tenancy is explicitly deferred post-MVH

Per-project daemon isolation (one daemon per project per [process-lifecycle.md §4.1]) is the MVH answer to multi-tenancy. Per-tenant cost attribution is out of scope for MVH; running N daemons does not auto-partition costs. The following concerns are acknowledged as real and explicitly deferred, not solved:

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

#### ON-050 — `harmonik attach` minimum surface

`harmonik attach` MUST: (a) connect to `.harmonik/daemon.sock` and verify the daemon-instance handshake per ON-013b (when ON-013b lands; for MVH, accept any daemon); (b) stream a live event tap (subset of `daemon_*`, `run_*`, `node_*`, `operator_*`, `error` events per [event-model.md §8]) to the operator's terminal; (c) present a periodic status snapshot (output of `harmonik status`) every T_attach_status (default 10s, operator-tunable); (d) accept operator commands inline (subset of `pause`, `resume`, `stop`); (e) detach cleanly on operator SIGINT or `:detach` command without affecting the daemon's state. The attach session_id MUST be carried in any operator-command emission for audit-trail correlation per ON-039.

Tags: mechanism

#### ON-051 — Multi-attach arbitration

Multiple operators MAY attach simultaneously to the same daemon. Each attach session has a unique session_id; each operator-command emission carries the originating session_id. Concurrent commands are serialized per ON-011's mutex discipline; losers observe the post-winner state per ON-013c idempotency. Detach by one operator MUST NOT affect other attached operators or the daemon's state.

Tags: mechanism

#### ON-054 — `harmonik status` reports pause-reason

When the daemon is in `paused` (or `pausing`) status per §7.1, `harmonik status` MUST report the pause-reason discriminator: `operator-pause` (issued via `harmonik pause`), `improvement-pause` (per ON-012), `upgrade-prepare` (when `harmonik upgrade` is in progress per ON-019). The discriminator MUST match the `operator_pause_status` payload's pause-reason tag (per [event-model.md §6.3]) and is sourced from the durable pause-state marker of ON-030a. An operator inspecting `harmonik status` during a pause MUST be able to distinguish these three reasons without consulting the event log.

Tags: mechanism

### 4.11 Resource budgets

#### ON-045 — Budgets are declared, enforced, and attributed cross-subsystem

Resource budgets (token, wall-clock, iterations) MUST be declared in policy per [control-points.md §4.5], enforced at dispatch by the agent runner per [control-points.md §4.5], and attributed in observability per run, per role, aggregated to per-workflow and per-harmonik-instance. Cost attribution per tenant is out of scope for MVH per §4.10.ON-042.

Tags: mechanism

#### ON-046 — Budget events are operator-observable

Budget-threshold events (`budget_warning`, `budget_exhausted`, `budget_accrual` per [event-model.md §8.4] and [control-points.md §4.5]) MUST be operator-observable via `harmonik status` and the attach UI per [process-lifecycle.md §4.10]. Operator-observable MUST NOT require parsing the raw JSONL; a summarized view is adequate.

Tags: mechanism

#### ON-047 — Category defaults for resource budgets

Every policy MUST have a declared default value for each budget category when a node / role / workflow does not declare one explicitly. The foundation-level category defaults table below defines the fallback values used when no policy override is present:

| Category | Default | Applies to | Override locus |
|---|---|---|---|
| Token budget (per-run) | 200_000 tokens | any agentic node | node-level OR role-level policy per [control-points.md §4.5] |
| Wall-clock budget (per-run) | 30 minutes | any agentic node | node-level OR role-level policy |
| Iterations budget (per-run) | 50 iterations (tool-use cycles) | any agentic node | node-level OR role-level policy |
| Wall-clock budget (per-reconciliation-workflow) | 10 minutes | reconciliation-dispatched investigator runs | [reconciliation/spec.md §4.4] policy |
| Warning threshold | 80% of budget | all categories | [control-points.md §4.5] CP-025 |

Category defaults are operator-configurable via the policy schema of [control-points.md §6.3] and are registered in the config inventory per §4.1.ON-004. These defaults exist to make "no policy declared" a safe state; operator policy SHOULD tune them per workload.

Tags: mechanism

#### ON-048 — Exhaustion protocol

On budget exhaustion (any category reaches 100% of its bound), the enforcing subsystem (agent runner for per-run budgets per [control-points.md §4.5]; reconciliation policy for reconciliation-workflow budgets) MUST:

1. Emit `budget_exhausted` per [event-model.md §8.4.3]; the emitter MUST tag `category` and `scope` via EV's structured-fields mechanism (payload shape — including `run_id`, `session_id?`, `budget_ref`, `attempted_dispatch_cost` — is EV-owned).
2. Terminate the in-flight LLM call or tool invocation at the next safe boundary (post-chunk for token budgets; post-iteration for iterations budgets; post-step for wall-clock budgets).
3. Route the run through the exhaustion-routing policy: default is `pause-and-escalate` — the run transitions to a failed state with a fallback verdict per [reconciliation/spec.md §4.4] RC-018, and the daemon MAY enter the paused state if the policy declares `pause-on-exhaustion=true` (default: false).
4. Emit `dispatch_deferred` per §8 code 18 if the exhaustion cascades to a multi-run ceiling breach.

The exhaustion protocol is deterministic (mechanism-tagged); the decision of whether to pause-vs-escalate is a per-policy operator decision.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### ON-049 — Attribution shape (who-consumed-what)

Budget attribution MUST be surfaced at the conceptual shape `(run_id, role, node_id, category, amount)` plus the `delegation_path` identifier per [control-points.md §4.8] CP-039 for cost incurred by a cognition-tagged step, so that cost can be traced to the specific model-class invoked. Concrete payload-field placement on `budget_warning`, `budget_consumed`, and `budget_exhausted` is owned by [event-model.md §8.4]; ON is normative for the attribution surface (the five fields plus `delegation_path` for cognition-tagged steps), EV is normative for the on-wire field names and their envelope placement. Aggregation levels are: per-run (indexed on `run_id`), per-role (indexed on `role`), per-workflow (indexed on `workflow_id`), and per-harmonik-instance (total over the daemon's lifetime). Aggregation is a read-side projection; the emission side MUST surface the attribution on every budget-affecting operation.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

## 5. Invariants

#### ON-INV-001 — N-1 compat window holds across every versioned artifact

Every versioned on-disk or wire artifact declared by foundation specs MUST hold the N-1 readability property of §4.5.ON-018 simultaneously. A release that breaks N-1 for any single artifact is a migration release per §4.5.ON-019 and MUST require an operator pause for install. This invariant constrains event-model, execution-model, control-points, and beads-integration together.

**Sensor.** Corpus-wide compat-matrix test harness: for every artifact declared by foundation specs (event envelope, event payload schemas per [event-model.md §4.7], checkpoint trailer per [execution-model.md §4.4], queue overlay, queue execution plan per [queue-model.md §3] (`.harmonik/queue.json`), policy schema per [control-points.md §6.3]), produce writer output at version N and parse at a reader pinned to N-1; failure of ANY pair flips the invariant. Sensor runs corpus-level per [architecture.md §4.1] AR-004.

Tags: mechanism

#### ON-INV-002 — (retired in v0.3)

**Retired.** The content of the former "No PR-gated rollout for MVH" invariant is operational (build-process) posture, not a runtime invariant. It has been moved into the scope assumption of §2.1a (naming `docs/foundation/project-level/build-practices.md` as the operational source). This ID is permanently retired; never reuse.

Tags: mechanism

#### ON-INV-003 — Secrets never appear in durable sinks unredacted

For every event-model-declared sink (event log per [event-model.md §4.4], dead-letter log per [event-model.md §4.3], session log per [workspace-model.md §4.7]), secrets MUST NOT appear unredacted. The invariant holds jointly across §4.7.ON-022, §4.7.ON-023, and the handler-contract secrets-injection rule — losing any one breaks the invariant.

**Sensor.** Two-part sensor: (a) compile-time schema linter (per §4.7.ON-023) that rejects any payload field typed as `Secret`; (b) regression test harness that writes each durable sink under a fixture whose secrets-injection set is known, then scans the sink's output for any fixture-secret substring. Sensor failure on either part flips the invariant.

Tags: mechanism

#### ON-INV-004 — (retired in v0.3)

**Retired.** The former "Between-task invariant covers pause, upgrade, and improvement-pause" content is a restatement of §4.3.ON-008, §4.6.ON-020, §4.6.ON-021, and §4.3.ON-012; per the template §5 selection test, content fitting inside §4 subsystems without cross-subsystem constraint is a requirement, not an invariant. This ID is permanently retired; never reuse. The normative obligations remain in §4.3 and §4.6.

Tags: mechanism

#### ON-INV-005 — Every subsystem MUST report its reconstruction contribution

Every subsystem MUST expose a reconstruction-contribution interface such that restart-recovery per §4.8.ON-030 can enumerate and verify its part of the reconstructed state. The specific interface (a Go method or a startup-probe event) is per subsystem, but the invariant requires that (a) NO subsystem reconstruct silently, (b) every subsystem's reconstruction terminates (bounded) before the daemon emits `ready`, and (c) any subsystem that cannot reconstruct MUST cause the daemon to fail startup with a categorized exit code (per §8) rather than enter a silently-partial `ready` state.

**Sensor.** Fixture-backed restart-recovery test harness: inject a known pre-restart state across every subsystem, restart the daemon, and assert each subsystem emits a reconstruction-completed signal before `ready`. Missing or silent reconstruction flips the invariant.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### ON-INV-006 — No subsystem introduces a control surface bypassing the between-task invariant

No subsystem MAY introduce a new control surface (a CLI command, an API endpoint, a signal handler, a socket protocol action) that aborts an in-flight run without routing through `stop --immediate` per §4.3.ON-009 OR the drain-gated pause/upgrade path of §4.3.ON-008. Subsystems MUST NOT add local escape-hatches (e.g., `kill-run`, `abandon-run`, `skip-reconciliation`) that would bypass the drain gate or the reconciliation carve-out.

**Sensor.** Corpus-wide grep-plus-reviewer audit: every subsystem spec's §4.a Subsystem envelope (per [architecture.md §4.4] AR-013) is inspected for operator-control-affecting operations; any such operation not routing through the state machine of §7.1 flips the invariant. Reviewer-enforced pending a mechanical lint for control-surface declarations.

Tags: mechanism

## 6. Schemas and data shapes

This spec does not introduce new persistent data types. Schemas referenced:

- **Event envelope and payloads** — [event-model.md §6.1] and [event-model.md §6.3]. Co-owned events emitted by operator-control transitions are listed in §6.5 below.
- **Checkpoint commit trailers and transition-record sibling file** — [execution-model.md §4.4].
- **Queue overlay** (bead-ID propagation) — [beads-integration.md §4.6].
- **Policy schema** — [control-points.md §6.3].
- **Health-check interface** — described inline in §4.9.ON-036 as `health_status ∈ {OK, degraded, failed}` plus optional reason string; no persistent representation.
- **Operator-control state set** — inline enum in §7.1.
- **Structured-log record** — inline wire-format shape in §4.9.ON-035; full schema deferred to OQ-ON-007 (`quality-checks.md`).

> §6.1, §6.2, §6.3 are intentionally omitted — this spec introduces no persistent data types, no YAML/JSON snippets, no tabular schemas of its own. See the owning specs ([event-model.md §6.3], [beads-integration.md §6.1], etc.) for the referenced shapes.

### 6.4 Schema evolution

Every artifact this spec references holds the N-1 compatibility window of §4.5.ON-018 (normative statement there). Version fields are owned by the defining spec (checkpoint schema-version trailer in execution-model, event schema-version in event-model, queue overlay version in beads-integration, policy schema version in control-points). This spec is normative for the N-1 window; defining specs are normative for the version field location and increment policy.

### 6.5 Co-owned event payloads

The following events are EMITTED by this spec's operator-control path (§4.3, §4.6) and REGISTERED in [event-model.md §8.7]:

- `operator_pause_status` — paired-phase event per [event-model.md §8.9(h)]; emitted once on `running → pausing` with `status=pausing`, and once on `pausing → paused` with `status=paused` (both in operator-initiated and improvement-initiated paths per §7.1). Payload schema in [event-model.md §8.7.6]; ON is normative for *when* the emission fires; EV is normative for *shape*. The pause-reason discriminator (operator vs improvement vs upgrade-prepare) is tagged via EV's structured-fields mechanism per [event-model.md §6.3].
- `operator_resuming` — emitted on `paused` → `resuming`; payload schema in [event-model.md §8.7].
- `operator_stopped` — emitted on entry to `stopped`; payload schema in [event-model.md §8.7.8]; the `mode` field distinguishes `graceful` vs `immediate`.
- `operator_upgrading` — emitted on `paused` → `upgrading`; payload schema in [event-model.md §8.7.9]; the `upgrade_version` field carries the operator-supplied expected commit hash.
- `operator_upgrade_completed` — emitted on `upgrading` → `running` post-exec-replace; payload in [event-model.md §8.7].
- `operator_upgrade_rejected` — emitted when §4.2.ON-005 commit-hash check fails or cross-version schema check refuses; payload in [event-model.md §8.7].
- `operator_command_rejected` — emitted when an operator command is invalid for the current state-machine state (§8 code 16); payload in [event-model.md §8.7].
- `dispatch_deferred` — emitted when a dispatch is blocked by the machine-ceiling mechanism of §4.10.ON-041 or other deferral condition (§8 code 18); payload in [event-model.md §8.7].

This spec is normative for the *when*; event-model is normative for the *shape*.

## 7. Protocols and state machines

### 7.1 Operator-control state machine

States: `running`, `pausing`, `paused`, `resuming`, `stopped`, `upgrading`. Improvement-pause and operator-pause share the `pausing` / `paused` states; the `pause_reason` payload field distinguishes them per ON-012.

| From | Event | Guard | To | Emits |
|---|---|---|---|---|
| `running` | `pause` | daemon status = `ready`; no-op if `reconciling` (queued per §4.3.ON-010) | `pausing` | `operator_pause_status` (`status=pausing`, `pause_reason=operator`) |
| `running` | improvement-loop trigger | improvement policy active | `pausing` | `operator_pause_status` (`status=pausing`, `pause_reason=improvement`) |
| `pausing` | all drain steps (ON-027 steps 1–7, including 3a) complete | no run satisfies `in_flight(run)` per §3 AND every ON-027 step has completed (or drain-timeout escalated per ON-029) | `paused` | `operator_pause_status` (`status=paused`; `pause_reason` preserved from the pausing-edge tag) |
| `paused` | `resume` (operator) OR improvement-loop completion (when `pause_reason=improvement`) | none | `resuming` | `operator_resuming` |
| `resuming` | dispatch loop re-entered | none | `running` | — |
| `paused` | `upgrade <hash>` | commit-hash matches §4.2.ON-005 | `upgrading` | `operator_upgrading` |
| `paused` | `upgrade <hash>` | commit-hash mismatch | `paused` | `operator_upgrade_rejected` |
| `upgrading` | exec-replace succeeds | new-binary schema ≥ current N-1 per §4.5.ON-018 | `running` (new binary) | `operator_upgrade_completed` |
| `running` | `stop --graceful` | none | drain → `stopped` | `operator_stopped` (`stop_mode=graceful`) |
| any | `stop --immediate` | none | `stopped` | `operator_stopped` (`stop_mode=immediate`) |
| `running` | `resume` | already in target state (no-op per ON-013c) | `running` (unchanged) | — (success, exit code 0; no event emitted) |
| any operator command | command invalid for current state | e.g., `upgrade` while `running`; truly invalid (not a no-op idempotency case) | (unchanged) | `operator_command_rejected` (per §8 code 16) |
| `stopped` | `start` | none | `running` (after normal startup per [process-lifecycle.md §4.2]) | startup events per process-lifecycle |

> INFORMATIVE: The state machine above is the operator-control half. The daemon-startup status progression (`starting` → optional `degraded` → `reconciling` → `ready`) is owned by [process-lifecycle.md §4.2, §4.3]; operator-control entry (`running`) occurs only at `ready`.

### 7.2 Drain protocol pseudocode

```
FUNCTION drain_graceful(timeout):
    -- §4.7.ON-027 eight drain steps (1, 2, 3, 3a, 4, 5, 6, 7)
    stop_queue_advancement()                             -- step 1: no new dispatches; queue → `paused-by-drain` per [queue-model.md §5]
    wait_for_runs_to_checkpoint(timeout.step_2)          -- step 2
    wait_for_handler_subprocess_exit(timeout.step_3)     -- step 3
    drain_br_intent_log(timeout.step_3a)                 -- step 3a; per beads-integration BI-029/BI-030
    flush_event_bus()                                    -- step 4; fsync per event-model
    flush_memory_indexing()                              -- step 5
    unlock_leased_workspaces()                           -- step 6; per workspace-model
    IF any_step_exceeded_its_bound:                      -- step 7
        RETURN exit_code("drain-timeout-escalated")      -- step 7: exit or enter paused (pause/upgrade path)
    ELSE:
        RETURN exit_code("success")                      -- step 7: exit or enter paused (pause/upgrade path)
```

Every branch corresponds to a normative requirement: steps 1–7 (and 3a, enumerated as eight steps) to ON-027; timeout escalation to ON-029; stop-immediate skip-steps to ON-028; step-atomicity and crash-recovery to ON-027a.

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
    exec_replace(new_binary_path)   -- fd-passed listener adopted gap-free per §4.6.ON-020(e) + [process-lifecycle.md §4.9 PL-027(iii)]
    -- new process resumes, runs startup per [process-lifecycle.md §4.2]
    -- on `ready`, transitions to `running`, emits `operator_upgrade_completed`
```

Branch points map to requirements: paused-precondition to ON-008, hash check to ON-005, schema-compat check to ON-019 and ON-021, exec-replace + fd-passed socket continuity to ON-020(e).

## 8. Error and failure taxonomy

Exit-code taxonomy. Every non-zero code maps to one category. Category names are stable across the N-1 window per §4.1.ON-001.

| Exit code | Category | Detection rule | Emitted event | Remediation pointer |
|---|---|---|---|---|
| 0 | success | — | — | — |
| 1 | generic-failure | Fallback for uncategorized failure; MUST be rare; presence in a release indicates missing taxonomy entry. | `run_failed` or subsystem-specific | Operator files incident; foundation amends taxonomy. |
| 2 | queue-format-unsupported | Beads schema version or harmonik overlay version not in supported set per §4.4.ON-016, OR `.harmonik/queue.json` (execution plan per [queue-model.md §3]) `schema_version` not in supported set per [process-lifecycle.md §4.2 PL-005 step 8a]. | `daemon_startup_failed` | Install migration release per §4.5.ON-019. |
| 3 | checkpoint-schema-unsupported | Checkpoint trailer or sibling-file schema version not in supported set per §4.5.ON-018. | `daemon_startup_failed` | Install migration release. |
| 4 | event-schema-unsupported | Event envelope or payload schema version not in supported set per [event-model.md §4.7]. | `daemon_startup_failed` | Install migration release. |
| 5 | pidfile-locked | Another daemon holds the pidfile lock for this project per [process-lifecycle.md §4.1]. | `daemon_startup_failed` | Identify running daemon via `harmonik list`; stop or target with `--daemon-id`. |
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
| 19 | runtime-panic | The daemon's top-level panic barrier per [process-lifecycle.md §4.6] PL-018a intercepted an uncaught Go runtime panic; daemon exits non-zero to avoid silent corruption. | `daemon_startup_failed` OR (at steady-state) the last-emitted run/node event plus a subsystem-specific crash event | Inspect structured-log records around the panic timestamp (per §4.9.ON-035); file incident with the panic stack. |
| 20 | signal-terminated | Daemon received a non-graceful termination signal (e.g., SIGKILL via external operator, OOM-killer, SIGBUS, SIGSEGV not intercepted by the panic barrier). | — (no clean emission path) | Next-restart reconciliation per [reconciliation/spec.md §4.2] classifies surviving runs; operator inspects OS-level logs for the signal source. |
| 21 | drain-step-errored | A specific drain step of §4.7.ON-027 (distinct from timeout escalation at code 11) encountered a non-recoverable error, e.g., fsync failure at step 4, workspace lock-release failure at step 6. | `daemon_shutdown` (with `mode=graceful`, augmented with `drain_error={step, error_category}`) | Inspect the step-specific error category; apply the remediation for that subsystem's owning failure taxonomy. |
| 22 | ntm-unavailable | `ntm` not on PATH, incompatible version, or tmux missing per [process-lifecycle.md §4.7 PL-021a]. | `infrastructure_unavailable{failed_prerequisite=ntm_unavailable}` plus `daemon_startup_failed` | Install/upgrade `ntm`; verify tmux available. |
| 23 | orchestrator-agent-unavailable | `harmonik runner --orchestrator-agent` cannot locate Claude Code per [process-lifecycle.md §4.10 PL-028]. | `daemon_startup_failed` | Install Claude Code or run without `--orchestrator-agent`. |

Additional codes may be added within the N-1 window as long as existing code-to-category mappings remain stable (normative code-stability rule at §4.1.ON-001). Taxonomy additions are reflected in the config inventory per §4.1.ON-004 and in the startup failure-mode catalog per §4.1.ON-003 where applicable.

> INFORMATIVE: Codes 1–23 are the MVH surface. Subsystem specs MAY declare additional subsystem-specific exit codes, which are registered against this taxonomy during spec-draft.

## 9. Cross-references

### 9.1 Depends on

- **[architecture.md §4.1]** — four-axis classification; every requirement and observability operation is tagged on the axes defined there.
- **[architecture.md §4.2]** — ZFC test; §4.9.ON-039 asserts observability operations are mechanism-tagged.
- **[architecture.md §4.8]** — role taxonomy; audit record's privileged-role subset per §4.9.ON-038.
- **[architecture.md §4.9]** — centralized-controller principle; the operator-control state machine is daemon-owned.
- **[event-model.md §6.1, §6.3]** — event envelope and payload registry; §6.5 co-owned events register there.
- **[event-model.md §8.7]** — operator-control event row-entries; §4.3.ON-013 and §6.5 cite here.
- **[event-model.md §8.9(h)]** — paired-phase rule for `operator_pause_status`; §4.3.ON-013 and §7.1 depend here.
- **[event-model.md §4.4, §4.7]** — fsync policy and schema compat; §4.5.ON-018 and §4.7.ON-027 step 4 depend here.
- **[event-model.md §4.3]** — dead-letter routing and consumer taxonomy; §4.7.ON-022 secret-redaction applies to dead-letter paths. Bus-internal events `consumer_failed` (§8.8.2) and `dead_letter_enqueued` (§8.8.3) are the operator-observable signals for consumer errors and dead-letter activity.
- **[event-model.md §4.9]** EV-034a — `source_subsystem` registration; §4.9.ON-035 structured-log wire format references.
- **[event-model.md §4.5]** — replay semantics; §4.8.ON-030 depends here for the "no JSONL replay on restart" rule.
- **[execution-model.md §4.3]** — run definition; §4.3.ON-007 maps operator "task" to `run`.
- **[execution-model.md §4.4, §4.5]** — checkpoint contract and cadence; §4.3.ON-008 and §4.7.ON-027 step 2 depend here.
- **[execution-model.md §4.7]** — state-reconstruction contract; §4.8.ON-030 depends here.
- **[execution-model.md §8]** — failure taxonomy; §4.3.ON-009 maps `stop --immediate` to class `canceled`.
- **[handler-contract.md §4.1]** — handler outcome / input sanitization; §4.7.ON-026 depends here.
- **[handler-contract.md §4.6]** — silent-hang detection; §4.9.ON-040 obligates its naming here.
- **[handler-contract.md §4.7]** — secrets injection; §4.7.ON-022 depends here.
- **[handler-contract.md §4.10]** — handler binary launch and commit-hash check; §4.2.ON-005 aligns.
- **[handler-contract.md §4.11]** — skill-injection obligation; §4.7.ON-025 depends here.
- **[control-points.md §6.3]** — policy schema; §4.5.ON-018 and §4.7.ON-025 depend here.
- **[control-points.md §4.7]** CP-037 — config loading precedence; §4.1.ON-004 depends here.
- **[control-points.md §4.5]** — budget control point; §4.11.ON-045, §4.11.ON-046, §4.11.ON-047, §4.11.ON-048, §4.11.ON-049 depend here.
- **[control-points.md §4.8]** CP-039 — cognition-tagged evaluator delegation path; §4.11.ON-049 cites for attribution.
- **[control-points.md §4.11]** — skill declaration; §4.7.ON-025 depends here.
- **[process-lifecycle.md §4.1]** — per-project daemon scope; §4.10.ON-042 depends here.
- **[process-lifecycle.md §4.2]** — startup sequence; §4.1.ON-003 co-owns the failure-mode catalog.
- **[process-lifecycle.md §4.3]** — ready-state transition; §4.8.ON-031 and §4.8.ON-033 cite the `ready` status event.
- **[process-lifecycle.md §4.4]** — shutdown; §4.1.ON-004 references the queue-empty / re-query cadence knob; §4.7.ON-027 coordinates with PL-011 drain.
- **[process-lifecycle.md §4.5]** PL-014a — per-daemon concurrency ceiling; §4.10.ON-041 distinguishes per-daemon from machine-level.
- **[process-lifecycle.md §4.6]** PL-018a — panic-recovery barrier; §8 exit code 19 (runtime-panic) cites here.
- **[process-lifecycle.md §4.10]** — command surface; §4.1.ON-002, §4.10.ON-041, §4.11.ON-046 reference here.
- **[reconciliation/spec.md §4.1]** — reconciliation-as-workflow; §4.3.ON-010 carve-out depends here.
- **[reconciliation/spec.md §4.2]** and **[reconciliation/spec.md §8]** — reconciliation categories and action mapping; §4.3.ON-014 operator override applies here; §4.7.ON-028 restart-recovery cite.
- **[reconciliation/spec.md §8.12]** — action-mapping layer; §4.3.ON-014 per-category scope references.
- **[reconciliation/spec.md §4.3]** — Cat 0 detector; §4.1.ON-003 startup catalog depends here.
- **[reconciliation/spec.md §4.4]** — investigator-agent contract, wall-clock budget, and fallback verdict; §4.8.ON-032 separates dispatch time from execution time; §4.11.ON-047 cites budget defaults; §4.11.ON-048 cites RC-018.
- **[reconciliation/spec.md §4.5]** — verdict execution; §4.3.ON-014 operator-override attaches here.
- **[beads-integration.md §4.1, §4.2, §4.3, §4.6]** — Beads is the queue; bead-ID propagation; `br` CLI adapter; §4.4.ON-015–§4.4.ON-017 depend here.
- **[workspace-model.md §4.3]** — workspace leasing; §4.7.ON-024 depends here.
- **[workspace-model.md §4.7]** — session-log metadata; §4.4.ON-015 references.

### 9.2 Reverse dependencies

> INFORMATIVE: Reverse dependencies are computed on demand from the foundation corpus. Populated at finalize.

### 9.3 Co-references (read-only consumption)

- **[docs/foundation/project-level/build-practices.md §Branch model]** — direct-to-main MVH development; §2.1a consumes this operational posture (formerly ON-INV-002, retired v0.3 — content preserved as a scope assumption).
- **[docs/foundation/problem-space.md §Locked decisions]** — locked decision #10 (operator controls between tasks) and locked decision #12 (no DTW); §4.3 and §4.8 derive from these positions.
- **[STATUS.md §Decisions Locked In]** — the ten locked decisions; amendment protocol per [architecture.md §4.6] applies to relaxing any requirement here that rests on a locked decision.

## 10. Conformance

### 10.1 Conformance profiles

**Core MVH.** An implementation conforming to Core MVH MUST pass every requirement ON-001 through ON-049 (ON-041 through ON-046 span §4.10–§4.11; ON-047 through ON-049 added in v0.3 for budget defaults / exhaustion protocol / attribution shape) and every non-retired invariant (ON-INV-001, ON-INV-003, ON-INV-005, ON-INV-006; ON-INV-002 and ON-INV-004 retired v0.3 — IDs never reusable), subject to the following bootstrap allowances:

- `ON-002` (exit-code taxonomy), `ON-003` (startup failure-mode catalog), `ON-004` (config inventory), `ON-014` (reconciliation operator override), `ON-020` (harmonik upgrade contract), and `ON-041` (multi-daemon commands) are **obligation** requirements that are satisfied when their named artifact exists in this spec or a cross-referenced spec. The §8 taxonomy table satisfies ON-002; production of a co-owned startup failure-mode catalog by spec-draft satisfies ON-003; production of a config-inventory appendix (see OQ-ON-001) satisfies ON-004; the naming convention in ON-014 plus [reconciliation/spec.md §4.5] satisfies ON-014; §4.6 and §7.3 satisfy ON-020; §4.10 ON-041 satisfies the obligation. Implementations consume these artifacts; they do not re-satisfy them per implementation.

**Post-MVH extensions.** Full binary signing (§4.2.ON-006), metrics exposition format (§4.10.ON-043), distributed tracing (§4.10.ON-044), and the multi-tenancy concerns of §4.10.ON-042 are deferred additive extensions; none is required for Core MVH conformance.

### 10.2 Test-surface obligations

During bootstrap (before `testing.md` exists) test obligations are named in prose. Each requirement group's test obligation:

- **ON-001 — ON-004 (exit codes and obligations).** Negative-path tests covering every exit code listed in §8; static-check test verifying that every requirement with a cross-reference to §4.1 resolves to a §8 entry.
- **ON-005 — ON-006 (integrity gate).** Upgrade scenario tests with matching and mismatched commit hashes; verify `operator_upgrade_rejected` on mismatch; verify post-MVH signing extension does not break MVH conformance.
- **ON-007 — ON-014 (operator-control semantics).** State-machine scenario tests enumerating every transition in §7.1; verify reconciliation-carve-out queueing of pause during `reconciling`; verify improvement-pause auto-resumes; verify `stop --immediate` aborts in-flight runs and emits `run_failed` with class `canceled` on next restart.
- **ON-015 — ON-017 (queue-format compat).** Upgrade scenario tests with N-1, N, and N+1 Beads schemas; verify startup failure on unsupported; verify `br` adapter localizes a simulated Beads breaking change.
- **ON-018 — ON-019 (schema compat window).** Cross-artifact compat tests: write at N, read at N-1, for every listed artifact; verify migration release refusal to install without a pause.
- **ON-020 — ON-021 (upgrade contract).** Full upgrade scenario tests covering all seven normative sub-rules of ON-020b–ON-020h: binary-source path resolution; hash-check pass and fail paths (stamp-missing, expected-hash-missing, mismatch); drain-vs-reconciliation queueing when reconciliation is in-flight; cross-version state contract (same-version succeeds, N-1 succeeds, N+2 refused); gap-free socket fd-passing (no ECONNREFUSED window across exec-replace); rollback from post-upgrade `paused` (daemon live); post-exec-replace failure recovery via `--rollback` from CLI with no running daemon. Verify cross-version state preservation across same-version and N-1 upgrades per ON-021.
- **ON-022 — ON-029 (security and shutdown).** Secret-redaction tests covering every sink; schema-level tests asserting no field is typed as `Secret` in any payload; sandbox escape-attempt tests; graceful-shutdown scenario tests for all eight steps; `stop --immediate` tests verifying steps 2–3 are skipped.
- **ON-030 — ON-033 (restart RTO).** Restart scenario benchmarks measuring SIGTERM-to-`ready` across representative hardware at MVH scale; verify 30s p95 nominal and 300s hard ceiling.
- **ON-034 — ON-040 (observability envelope).** Per-subsystem-conformance tests verifying typed event emission, structured log emission, health-check interface presence, liveness heartbeat cadence, audit-record derivation, and mechanism-tagging of every observability operation; obligation test for silent-hang detection per [handler-contract.md §4.6].
- **ON-041 — ON-046 (multi-daemon, budgets).** Multi-daemon scenario tests verifying `harmonik list`, flag-based targeting, and machine-ceiling enforcement; budget tests verifying declared-enforced-attributed pipeline.
- **ON-047 — ON-049 (budget defaults, exhaustion, attribution).** Policy-default application tests (run without explicit budgets consumes defaults); exhaustion-protocol tests verifying the emit-and-terminate sequence at category boundaries; attribution-shape tests asserting every budget-affecting event carries the five-field tuple and, where applicable, the `delegation_path`.

Migration to `[testing.md §<layer>]` cross-references occurs within one revision cycle once testing.md lands; this obligation is tracked in OQ-ON-002.

### 10.3 Excluded conformance claims

- This spec does NOT grant conformance over: the specific CLI flag surface (deferred per §2.2); operator dashboard UI (deferred per §2.2); full binary signing (§4.2.ON-006 deferred); Prometheus / OpenTelemetry wire formats (§4.10.ON-043 deferred); distributed tracing (§4.10.ON-044 deferred); per-tenant cost attribution (§4.10.ON-042 deferred).
- This spec does NOT guarantee throughput or latency bounds beyond the restart RTO of §4.8; subsystem-internal performance targets are owned by subsystem specs.
- This spec does NOT own the implementation of pause / stop / upgrade state-machine transitions; those live in the S01 orchestrator-core subsystem spec. This spec is normative for the state set, the allowed transitions, the emitted events, and the between-task invariant.

## 11. Open questions

#### OQ-ON-001 — Config inventory authoritative location (UNRESOLVED)

Status: **unresolved** as of v0.3.
Question: Should the normative config inventory obligated by ON-004 live as an appendix to this spec, as a sibling file under `specs/operator-nfr/config-inventory.md`, or as a top-level `specs/config-inventory.md` cross-referenced from every affected spec?
Rationale for unresolved: The inventory aggregates knobs from eight specs; deciding its home without knowing its final size or whether non-NFR specs will own non-trivial slices of it would prematurely commit to a layout. Architect-honest: this is a layout decision the user has not yet been asked to make.
Owner: foundation-author (decision surface user; committed after user signals preference).
Blocks: ON-004 completeness (the obligation is named; the artifact location is undecided).
Default-if-unresolved: Sibling file under `specs/operator-nfr/config-inventory.md`, cross-referenced from every knob-declaring spec. Migration to a top-level spec if the inventory grows beyond ~300 lines or serves multiple non-NFR owners.

#### OQ-ON-002 — Migrate test-obligation prose to testing.md references

Question: §10.2 currently names test obligations in prose. The template expects cross-references to `[testing.md §<layer>]` once testing.md lands.
Owner: foundation-author
Blocks: none (MVH prose obligations are in place).
Default-if-unresolved: Keep prose obligations; migrate within one revision cycle after testing.md is finalized.

#### OQ-ON-003 — Machine-ceiling coordinator implementation locus (UNRESOLVED)

Status: **unresolved** as of v0.3.
Question: ON-041 obligates a machine-level agent-subprocess ceiling enforced by a shared lock or a machine-level coordinator process. Which is the MVH shape — filesystem-based shared-counter lock (simpler, has races at scale) or a single coordinator daemon (more complex, needs its own lifecycle)?
Rationale for unresolved: Architect-honest: the choice between a shared-lock and a coordinator daemon is load-bearing because a coordinator daemon introduces a second process-lifecycle contract (its own startup, crash semantics, upgrade path) that we have not yet specified. Promoting either option to normative without a user decision would either commit us to a lock-with-races approach or force a coordinator-daemon spec we haven't scoped.
Owner: foundation-author (decision surface user; committed after user signals preference).
Blocks: ON-041 implementation shape.
Default-if-unresolved: Filesystem-based shared-counter lock at `~/.harmonik/machine-ceiling.lock` with advisory locking. Revisit if contention measurements show thrash or a coordinator surfaces as necessary.

#### OQ-ON-004 — Concurrent-operator-attach arbitration

Question: Multiple simultaneous `harmonik attach` sessions are allowed per [process-lifecycle.md §4.10]. Two operators simultaneously issuing `pause` or `upgrade` — which wins, and is there a lock?
Owner: foundation-author
Blocks: none (OVERVIEW.md §8 names this as a known silence).
Default-if-unresolved: The second command observes the state-machine in the post-first-command state and either no-ops (both paused) or errors (if incompatible). No explicit lock. Single-operator MVH assumption makes this acceptable; revisit when multi-operator deployments appear.

#### OQ-ON-005a — RTO ceiling behavior — notify-only vs auto-escalate

Question: ON-032 criterion 3 says "operator is notified" on 300-second breach. Is notification the only action, or should the daemon auto-escalate (e.g., refuse to come `ready` until operator acknowledges)?
Owner: foundation-author
Blocks: none (default below).
Default-if-unresolved: Notify-only via `daemon_degraded` event; daemon continues reconstruction and transitions `ready` when complete. Operator intervention is permitted but not required.

#### OQ-ON-005b — RTO target relaxation vs fixture tightening

Question: v0.3 set the RTO target to 30s nominal fixture / 300s ceiling (§4.8.ON-031, §4.8.ON-032). If fixture-based measurement at implementation time shows 30s is unachievable under realistic MVH loads, does the nominal target get relaxed or does the fixture itself shrink?
Owner: foundation-author
Blocks: none (default below).
Default-if-unresolved: Fixture adjustment is preferred over target relaxation; revisit on first RTO measurement.

#### OQ-ON-006 — PL adopting ON-027 drain steps (cross-spec coordination)

Question: §4.7.ON-027 specifies the eight-step drain sequence used by both `stop --graceful` and by the §4.3.ON-008 pause/upgrade drain gate. [process-lifecycle.md §4.4] PL-011 also specifies a shutdown drain; the two need to be coordinated so that PL's drain adopts (or consistently references) ON-027's step sequence. PL's integration cycle (not this integration) owns the edit on its side.
Owner: foundation-author; resolution paired with PL's next revision cycle.
Blocks: none (ON is normative for the eight-step sequence; PL's alignment is the deferred coordination).
Default-if-unresolved: ON-027 is authoritative for the drain step list. PL-011 already names drain-to-checkpoint behavior (matching ON-027 step 2); per-step alignment (steps 3–7, including the new step 3a intent-log drain) is deferred to PL's next revision.

#### OQ-ON-007 — Structured-log wire format home (`quality-checks.md`)

Question: §4.9.ON-035 promotes structured-log ownership to ON (the prior `[event-model.md §3.8]` citation did not resolve in EV). The minimum wire shape is named inline in ON-035. A full schema — including typed-field enumeration, log-rotation policy, parser contract on the consumer side, and the compliance sensor — needs a dedicated home. Candidate filename: `specs/quality-checks.md`. Does the corpus want such a spec, or should structured-log details stay inline in ON?
Owner: foundation-author.
Blocks: none (minimum shape is in §4.9.ON-035).
Default-if-unresolved: Inline minimum shape in ON-035 is sufficient for MVH. Promote to a dedicated `quality-checks.md` if the structured-log surface grows beyond ~100 lines OR if the parser contract acquires consumers beyond the local `harmonik attach` UI.

#### OQ-ON-008 — Daemon-discovery scope for `harmonik list`

Question: §4.10.ON-041 names the daemon-discovery scope as `$HOME` plus `$HARMONIK_PROJECT_ROOTS`. Is this sufficient, or does MVH need a system-wide registry (e.g., `/var/lib/harmonik/daemons.d/`)?
Owner: foundation-author.
Blocks: none (default is the ON-041 mechanism).
Default-if-unresolved: Stays as ON-041 / E6 mechanism (`$HOME` + `$HARMONIK_PROJECT_ROOTS`). A system-wide registry is post-MVH; cross-user discovery is not an MVH need.

#### OQ-ON-009 — Migration-release manual procedure documentation home

Question: ON-019 refers to a "dedicated migration workflow (post-MVH)" for breaking schema changes; the manual MVH-era procedure (operator-paused boundary, schema-version verification, on-disk state inspection) needs a documentation home. Where does it live?
Owner: foundation-author.
Blocks: none.
Default-if-unresolved: Release notes for any migration release; a dedicated migration playbook document is post-MVH.

#### OQ-ON-010 — Panic-file cleanup policy and rotation

Question: ON-053 specifies post-panic forensic file accumulation (`.harmonik/panic-<timestamp>.log`) but does NOT mandate cleanup. Should the daemon trim panic files on a schedule, by count, or by age?
Owner: foundation-author.
Blocks: none.
Default-if-unresolved: Operator manually trims; rotation policy is post-MVH. ON does not at this revision impose a daemon-side cleanup obligation.

#### OQ-ON-011 — Structured logs under N-1 compatibility window

Question: §4.9.ON-035 introduces a `log_schema_version` field but does not amend ON-018's enumeration of N-1 covered artifacts. Should structured logs be added to the ON-018 enumeration, or carry an explicit exemption?
Owner: foundation-author.
Blocks: none (the field is normative; the enumeration update is bookkeeping).
Default-if-unresolved: Structured logs are N-1 governed; ON-018 enumeration is updated in ON's next revision.

> **Cross-ref note (OQ-RC-009 resolution, v0.4.1).** Reconciliation's [reconciliation/spec.md §11 OQ-RC-009] asked whether ON should declare a normative `quarantined` daemon-status. The resolution adopted at ON v0.4.1 (and consistent with the OQ's default-if-unresolved): **decline to add a normative `quarantined` state at MVH.** Rationale: per [reconciliation/schemas.md §6.2 Verdict-execution table] (`escalate-to-human` row) and [reconciliation/spec.md §4.5 RC-025], quarantine is the operator-escalation OUTCOME — the outer run remains in its current state-machine state and `operator_escalation_required` is emitted (consumed via the operator-observable surface per ON-002) — NOT a daemon-status enum value. ON's `DaemonStatus` enum (§3 glossary, §6.1) consequently does NOT include `quarantined` and no §6.1 / §7.1 / §3 update is required. Should a future revision reverse this resolution (post-MVH), the addition would be additive to `DaemonStatus` and would land via a foundation amendment with an accompanying RC schema cite.

## 12. Revision history

| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-05-31 | 0.4.4 | agent (kerf `credfence` work) | Added six credential & spend-governance config-inventory entries (ON-004b credential injection source per [credential-isolation.md CI-006]; ON-004c per-day USD cap; ON-004d max-runs ceiling; ON-004e Pi model tiers, tier-3 judgment defaults Sonnet with Opus opt-in; ON-004f single daemon `claude` baseline default, hot-reload SHOULD-not-MUST, surface left to implementation; ON-004g `--dry-run` plan-only mode), extended the ON-004 "at minimum" list to name them, and added ON-008a (the §4.3 operator-surface note: `supervise start` injects the credential from the scoped source per CI-006; `budget-paused` surfaced and cleared via the existing handler-resume). New IDs: ON-004b, ON-004c, ON-004d, ON-004e, ON-004f, ON-004g, ON-008a (7 new). All additive; no existing requirement reversed. Source: kerf `credfence` change design. |
| 2026-05-15 | 0.4.3 | foundation-author | ON-020 spec-draft obligation fulfilled: 7 normative sub-rules added as ON-020b–ON-020h within §4.6. **ON-020b** — binary-source mechanism: positional arg or `--binary <path>` flag; absence fails §8 code 16; path MUST resolve to executable regular file. **ON-020c** — operator-supplied hash check: `--expected-hash <sha>` required; absence fails §8 code 14 / `failure_mode=expected-hash-missing`; stamp-absent fails `binary-stamp-missing`; check executes before exec-replace and before ON-020a marker write. **ON-020d** — drain-vs-reconciliation: in-flight reconciliation queues the upgrade (reconciliation carve-out per ON-010 applies); `stop --immediate` discards queued upgrade and aborts normally. **ON-020e** — cross-version state contract: introspect new binary's `--schema-version-query`; succeed for on-disk schema ∈ supported-set (same-version or N-1); refuse with §8 code 15 for N+2 or wider mismatch; no auto-migrate. **ON-020f** — socket fd-passing continuity: outgoing daemon clears `FD_CLOEXEC`, passes fd via `HARMONIK_LISTENER_FD`; new binary adopts via `net.FileListener`, re-sets `FD_CLOEXEC`; adoption gap-free; fd-adoption failure → §8 code 6. **ON-020g** — rollback as first-class: `harmonik upgrade --rollback` exec-replaces back to `.harmonik/daemon.binary.prev`; hash from ON-020a marker; live-window rollback unsupported; `--rollback` while `running` → §8 code 16; `.harmonik/daemon.binary.prev` written atomically before exec-replace. **ON-020h** — post-exec-replace failure recovery: CLI `--rollback` from no running daemon: removes stale pidfile/socket, reads `.harmonik/daemon.upgrading` for prior hash, validates `.harmonik/daemon.binary.prev`, starts prior binary directly, removes marker and prev-binary on success. **§10.2 ON-020–ON-021 test obligation** rewritten to enumerate all seven new sub-rules as test scenarios. **New IDs (net):** ON-020b, ON-020c, ON-020d, ON-020e, ON-020f, ON-020g, ON-020h (7 new requirement IDs). No invariants added or retired. No §8 exit codes added (new sub-rules reference existing codes 14, 15, 16, 6). Status remains `reviewed`. Refs: hk-sx9r.24. |
| 2026-04-23 | 0.1.0 | foundation-author | Initial draft from components.md Component 7 + round-2 amendments. |
| 2026-04-24 | 0.2.0 | foundation-author | Corpus-wide cleanup pass (no semantic changes). Migrated legacy architecture.md citation anchors to the §4.N map per the v0.2 NOTE: §1.1→§4.1 (×2 in §4.9 event-envelope clause and §9 cross-refs), §1.2→§4.2 (×2 in §4.9 ZFC-tag clause and §9 cross-refs), §1.3→§4.3 (×1 in §4.9 verification-node clause), §1.5→§4.6 (×1 in §10 STATUS cross-ref), §1.6→§4.8 (×2 in §4.9 audit-privileged-role clause and §9 cross-refs), §1.8→§4.9 (×1 in §9 cross-refs). No requirement IDs, invariants, or schemas were touched. |
| 2026-04-24 | 0.4.0 | foundation-author | R2 integration pass (skeptic + crash-adversary + operator-persona reviews). Status flips `draft` → `reviewed`. **GROUP A — Critical fabrication / cross-spec / payload fixes (BLOCKING):** A1 — rewrote §3 `in_flight(run)` glossary entry; the prior fabricated `RunState` enum citation to [execution-model.md §7.1] (with non-existent `{PARKED, COMPLETED, FAILED, CANCELED}`) is replaced by the lowercased predicate `run.state ∉ {completed, failed, canceled}` aligned to EM's actual glossary entry, with a "parked" lifecycle position explicitly excluded as bead-state-only (no `run.state` to evaluate); the predicate is now evaluated via the `dispatch-status` JSON-RPC of [process-lifecycle.md §4.1 PL-003a] for non-orchestrator-core consumers. A2 — absorbed PL-INTERIM exit codes 22 (`ntm-unavailable` per [process-lifecycle.md §4.7 PL-021a]) and 23 (`orchestrator-agent-unavailable` per [process-lifecycle.md §4.10 PL-028]) into §8; "MVH surface" prose updated from 1–21 to 1–23. A3 — stripped payload-field-name redeclaration from ON-013 / ON-048 / ON-049 per the §6.5 co-ownership rule (ON owns *when*, EV owns *shape*); ON-013's `pause_reason`/`stop_mode`/`expected_commit_hash` field-naming replaced with citations to [event-model.md §8.7.6/§8.7.8/§8.7.9] (the structured-fields mechanism per [event-model.md §6.3] tags pause-reason); ON-048's `category, scope, exhausted_at` field naming replaced with cite to [event-model.md §8.4.3]; ON-049 reframed to declare the conceptual attribution shape `(run_id, role, node_id, category, amount)` plus `delegation_path` with concrete payload-field placement deferred to EV §8.4. A4 — reconciled ON-020(e) socket-rebind with PL-027(iii) fd-passing: replaced the prior "re-bind same socket path" prose with the gap-free fd-passing contract (`HARMONIK_LISTENER_FD` env var, `net.FileListener` adoption, `FD_CLOEXEC` discipline); §7.3 pseudocode and §2.1 summary updated. **GROUP B — Operator-surface forensic / status (BLOCKING per operator-persona Tier 1):** B1 — added ON-053 post-panic forensic file (atomic-written `.harmonik/panic-<timestamp>.log` carrying panic stack, PID/PGID/project_hash/binary commit hash, dual-form timestamp, last-emitted run/node/event ids; temp+rename+fsync+parent-fsync per [workspace-model.md §4.7 WM-026]; not consumed by reconciliation; cleanup deferred to OQ-ON-010). B2 — added ON-054 `harmonik status` reports pause-reason discriminator (`operator-pause` / `improvement-pause` / `upgrade-prepare`) sourced from the durable pause-state marker of ON-030a. **GROUP C — Crash-adversary durability fixes (BLOCKING):** C1 — added ON-030a pause-state durable marker (atomic-written `.harmonik/daemon.state` synchronously on every `paused`/`pausing`/`upgrade-prepare`/`stopped` transition; on startup PL-005 step 0 reads the marker and initializes into the persisted state) — strongest crash-adversary finding; pause intent now survives daemon crash. C2 — added ON-020a upgrade-intent durable marker (atomic-written `.harmonik/daemon.upgrading`; PL-005 step 0 consumes; mismatched commit hash on restart fails with §8 code 14); promotes PL-027(iv) from informative to normative (cross-spec coordination request to PL). C3 — added ON-027a drain step atomicity (sequential single-goroutine execution; per-step durable completion mark; mid-drain crash resumes from next-uncompleted step, idempotent on completed steps). C4 — amended ON-027 to insert step 3a (`br`-CLI adapter intent-log drain per [beads-integration.md §4.10 BI-029/BI-030]; `BrUnavailable` failures escalate to step 4 with next-restart Cat 3a routing); "seven-step" language updated to "eight-step" throughout body, §7.1, §7.2 pseudocode, §7.3, OQ-ON-006, §A.4. C5 — amended ON-022 with redactor fail-closed (panic/error during emission MUST abort the emission, MUST emit `redaction_failed`, MUST NOT fall through; repeated failures within T_redact_fail escalate to `degraded`; cross-spec coordination request to EV: add `redaction_failed` to §8). C6 — amended ON-011 with state-machine serializability (single mutex guarding the transition function; concurrent operator commands per OQ-ON-004 arbitrated by mutex acquisition order; mutex acquired before guard evaluation, held until durable-marker write per ON-030a completes). **GROUP D — Crash-adversary should-land items:** D1 — amended ON-033 with dual-source RTO timestamps (monotonic-corrected via `_at_ns_since_boot` companion fields; boot-transition cycles marked `rto_undefined`; SIGKILL terminations marked `rto_undefined`; cross-spec coordination request to EV: add monotonic-companion fields to `daemon_shutdown` §8.7.2 and `daemon_ready` §8.7.3). D2 — added ON-013a per-command supervision (`defer recover()` barrier on every operator-command-dispatch goroutine; emits `operator_command_failed`; reverts partial state-machine transition; escalates to `degraded` if mid-drain; cross-spec coordination request to EV: add `operator_command_failed`). D3 — added ON-013c operator-command idempotency (no-op transitions return success exit code 0; `operator_pause_status{paused}` deduplicated via session_id; CLI MUST NOT see different exit codes for "already in target state" vs "transitioned"). D4 — amended ON-040 with drain-forced silent-hang synthesis (drain-timeout SIGKILL to a still-running agent subprocess MUST synthesize `agent_warning_silent_hang{reason=drain_forced}` prior to the SIGKILL; ensures every uncleanly-exited agent produces a silent-hang record). **GROUP E — Skeptic important items:** E1 — collapsed `improvement-pausing` / `improvement-paused` states in §7.1 into the single `pausing` / `paused` chain with `pause_reason` discriminator (operator vs improvement vs upgrade-prepare); ON-011 / ON-012 / §6.5 / §7.1 / ON-030a updated. E2 — disambiguated `degraded` in §3 glossary: subsystem-level `degraded` (per ON-036/ON-037, input) vs daemon-level `degraded` (per §6.1 `DaemonStatus`, aggregation of Cat 0 failure / RTO ceiling breach / silent-hang fan-out). E3 — resolved ON-029 per-step vs global drain-timeout: per-step (`timeout.step_2`, `timeout.step_3`, etc.) is normative; an optional `drain_timeout_total` may be declared as the sum-bound. E4 — concretized ON-032 fixture bounds: `≤ 500 open beads, ≤ 50 in-flight runs, git-log depth ≤ 10,000 commits, ≤ 100 reconciliation-Cat-3-pending runs, ≤ 10 active investigator workflows`; ON-031 sensor description updated to match. E5 — added Axes lines to ON-025 (skill provisioning, non-idempotent), ON-030 (reconstruction, idempotent), ON-049 (attribution emission, non-idempotent). E6 — named the `harmonik list` daemon-discovery mechanism on ON-041: scans `**/.harmonik/daemon.pid` under `$HOME` plus `$HARMONIK_PROJECT_ROOTS` (colon-separated); queries `.harmonik/daemon.sock` via JSON-RPC `status`; pidfiles with unreachable sockets or unreachable PIDs reported as `stale`. E7 — added ON-005a binary-stamp source: `actual_hash` MUST come from build-time embedded ldflags stamp (`-ldflags="-X main.commitHash=<sha>"`); binaries lacking the stamp fail integrity gate immediately with §8 code 14 / `failure_mode=binary-stamp-missing`. **GROUP F — Operator-persona Tier 2:** F1 — added ON-050 `harmonik attach` minimum surface (handshake / live event tap / periodic status snapshot / inline operator commands / clean detach; attach session_id carried in operator-command emissions for ON-039 audit correlation) and ON-051 multi-attach arbitration (per-session session_id; serialization per ON-011's mutex; idempotency per ON-013c; one operator's detach MUST NOT affect others). F2 — amended ON-013 to include `drain_summary` field on `operator_pause_status{status=paused}` emission (per-step completion timestamps + drain-timeout escalations; cross-spec coordination request to EV: extend §8.7.6 payload with `drain_summary?`). F3 — added `harmonik list` column set on ON-041: `daemon_id` / `project_root` / `pid` / `status` / `socket_path` / `started_at` / `last_exit_code` / `budget_summary` (per-daemon roll-up of `tokens_consumed` / `wall_clock_consumed` / `iterations_consumed` per ON-049 attribution); filterable by `status` and project-root substring. F4 — amended ON-035 structured-log shape: added `log_schema_version` (current "1.0"), `source_subsystem` per [event-model.md §4.9 EV-034a], `event_id?` UUIDv7 correlation per [event-model.md §4.1] when the log emits a tracked event; producer-side redaction (consumers MUST NOT re-redact); rotation at 100 MiB or 24 hours to `.harmonik/logs/<subsystem>-<rotated_at>.jsonl`. F5 — added ON-020(f) `harmonik upgrade --rollback` first-class (exec-replace back to `.harmonik/daemon.binary.prev`; live-window rollback unsupported) and ON-020(g) post-exec-replace failure recovery (rollback removes stale pidfile/socket, restores prior binary, consumes `.harmonik/daemon.upgrading` marker for hash determination). **GROUP G — OQ updates:** OQ-ON-005 split into OQ-ON-005a (auto-escalate vs notify-only) and OQ-ON-005b (fixture-tightening vs target-relaxation); added OQ-ON-008 (daemon-discovery scope; default: `$HOME` + `$HARMONIK_PROJECT_ROOTS`), OQ-ON-009 (migration-release procedure documentation home; default: release notes), OQ-ON-010 (panic-file cleanup policy; default: operator manually trims, rotation post-MVH), OQ-ON-011 (structured logs in ON-018 N-1 enumeration; default: structured logs are N-1 governed). **GROUP H — Bookkeeping:** H1 — added §6.1/§6.2/§6.3 omission declaration. H2 — front matter version 0.3.0 → 0.4.0; status draft → reviewed; last-updated unchanged. **New IDs (net):** ON-005a, ON-013a, ON-013c, ON-020a, ON-027a, ON-030a, ON-050, ON-051, ON-053, ON-054 (10 new requirement IDs). **Cross-spec coordination requests:** PL v0.4.1 must promote PL-027(iv) to normative (per ON-020a); add `daemon_instance_id` mint at PL-005 step 0; PL-002b pidfile gains line-3 daemon_instance_id; PL-009/PL-011a payloads gain `_at_ns_since_boot` fields. EV v0.3.1 must add `operator_command_failed`, `redaction_failed`, `operator_escalation_cleared` events; confirm `daemon_shutdown` class F (resolves OQ-PL-012); add monotonic-companion fields to `daemon_ready`/`daemon_shutdown`/`operator_pause_status` payloads. BI v0.3.1 may add a drain-time fail-fast mode on BI-031. RC R2 should cite `operator_escalation_cleared`. HC §4.6 should accept the drain-forced silent-hang synthesis per ON-040 amendment. Absorbed PL-INTERIM exit codes 22 (ntm-unavailable) and 23 (orchestrator-agent-unavailable) into §8 per the cross-spec coordination request from PL v0.4.0; PL's next revision will drop the PL-INTERIM markers. Stripped payload-field redeclaration from ON-013 / ON-048 / ON-049; field names now reference EV-owned payloads. Cross-spec coordination request to EV: consider promoting `pause_reason` to a top-level `operator_pause_status` payload field if the structured-fields mechanism is too implicit for operators. |
| 2026-05-14 | 0.4.2 | foundation-author | extqueue reconciliation pass. Surgical amendments aligning ON with the new `specs/queue-model.md` (extqueue work). **ON-004** — quietly deleted the `queue-empty re-query cadence ([process-lifecycle.md §4.4])` line-item from the config inventory; the daemon no longer polls under extqueue (orchestrator submits via `queue-submit` over the daemon socket). No knob is renamed or relocated; the slot is removed, not reassigned. Precedent: invariant-level retirement exists (ON-INV-002/-004 retired v0.3) but no precedent for line-item retirement; quiet deletion + this changelog entry chosen over an explicit "Retired in v0.4.2" sub-bullet to avoid inventing an affordance. **ON-009a** — appended a disambiguation note distinguishing the needs-attention bead set (Beads-side, this requirement) from the daemon execution queue ([queue-model.md §1], persisted as `.harmonik/queue.json`); heading unchanged for inbound-cite safety. **ON-013a** — replaced the operator-command enumeration's `enqueue` entry with the explicit v0.1 `queue-*` JSON-RPC methods (`queue-submit`, `queue-append`, `queue-status`, `queue-dry-run`) plus a forward-ref to [process-lifecycle.md §4.1 PL-003a] for the canonical method list. **ON-015** — rewrote sentence 1 only: "Beads is the catalog of work … the daemon's execution queue is the execution plan layered on top, owned by [queue-model.md §2] and persisted in `.harmonik/queue.json` per ON-018." Sentence 2 (overlay-schema compat for trailers/event bead-IDs/session-log metadata) and the rest of the paragraph unchanged. Heading unchanged. **ON-018** — extended the N-1 artifact enumeration with `queue execution plan ([queue-model.md §3], persisted as .harmonik/queue.json with a schema_version field)`, placed between `queue overlay (§4.4.ON-015)` and `policy schema`. **ON-027 step (1)** — reworded from "orchestrator stops pulling new tasks from the queue" to "the daemon stops advancing the queue: no new dispatches are issued …; the queue's status field transitions to `paused-by-drain` per [queue-model.md §5]". Steps (2)–(7) and (3a) unchanged. **§7.2 drain pseudocode** — parallel edit: renamed `stop_dispatch_loop()` to `stop_queue_advancement()` with updated comment mirroring ON-027 step (1). **ON-041 step (b)** — added `queue` (with subcommands `submit`, `status`, `append`, `dry-run`) to the daemon-communicating-commands list; daemon-id flags carry uniformly. **ON-050 step (d)** — removed `enqueue` from the `harmonik attach` inline-command subset; the subset is now `{pause, resume, stop}`. `enqueue` is retired with no alias (no spec text requires backward compat on CLI command names). **ON-INV-001 Sensor** — parallel artifact-enumeration edit to match ON-018: added `queue execution plan per [queue-model.md §3] (.harmonik/queue.json)`. Invariant body unchanged. **ID-FREEZE preserved.** No ON-NNN added or retired by this revision. No invariants added or retired. No §8 exit codes touched (new `queue_validation_failed` failure modes live in queue-model.md's JSON-RPC error space, not in ON §8 exit-code taxonomy). Cross-spec coordination requests: `specs/queue-model.md` is a NEW spec (drafted in the extqueue kerf work) and is a prerequisite for these citations to resolve; process-lifecycle.md is amended in the same work to declare PL-003a's queue-method extension. Status remains `reviewed`. |
| 2026-04-25 | 0.4.1 | foundation-author | OQ-RC-009 resolution acknowledgment. RC's [reconciliation/spec.md §11 OQ-RC-009] asked whether ON should declare a normative `quarantined` daemon-status. The resolution: **decline to add a normative `quarantined` state at MVH** (consistent with OQ-RC-009's default-if-unresolved). Rationale: quarantine is the operator-escalation OUTCOME per RC's `escalate-to-human` mechanical action ([reconciliation/schemas.md §6.2], [reconciliation/spec.md §4.5 RC-025]) — the outer run remains in its current state-machine state and `operator_escalation_required` is emitted, with the operator-observable surface delivered per ON-002 — not a daemon-state. ON's `DaemonStatus` enum (§3 glossary, §6.1) already does NOT contain `quarantined` (consistent with RC's R2 schemas.md §6.2 fix that dropped a fabricated `quarantined`-state cite); consequently this revision required no §6.1 / §7.1 / §3 / pause-state-FSM edit. The decision is recorded as a one-paragraph cross-ref note appended to §11 (after OQ-ON-011) so future readers can find the resolution from the ON side. No requirement IDs added or retired; no invariants, schemas, or §8 exit-codes touched; ID FREEZE preserved. Status remains `reviewed`. |
| 2026-04-24 | 0.3.0 | foundation-author | R1 integration pass (implementer + cross-spec-architect + critic). Status remains `draft` (R2 will transition to `reviewed`). **Front matter:** added `spec-category: foundation-cross-cutting` per [architecture.md §4.0] AR-052; retained `depends-on` including process-lifecycle (PL will drop ON from its depends-on in PL's own integration to resolve the PL↔ON cycle; this integration does not edit PL). **BLOCKING findings applied:** B1 — defined `in_flight(run)` mechanically in §3 using [execution-model.md §7.1] RunState enum (`state ∉ {PARKED, COMPLETED, FAILED, CANCELED}`); propagated the predicate through ON-008, §7.1 state-machine guards, ON-027 drain-step 2, ON-009 stop-immediate, and ON-030 reconciliation dispatch. B2 — rewrote §7.1 `pausing → paused` transition guard to require completion of ALL seven ON-027 drain steps AND no remaining in-flight run; updated ON-008 text and tightened ON-021 recoverability invariant to the iff form "paused ⇒ drain-completed." B3 — applied the migration table to ~30 stale citations across body text: `[event-model.md §3.1]→§6.1`, `§3.2→§8.7`/`§6.3`/`§8.3`/`§8.4` (context), `§3.4→§4.4`, `§3.5→§4.7`, `§3.6→§4.5`, `§3.7→§4.3`, `§3.8` promoted to ON-owned per I6; `[beads-integration.md §10.1]→§4.1` etc.; `[workspace-model.md §5.1]→§4.3`, `§5.3→§4.7`; `[process-lifecycle.md §8.1]→§4.1`, `§8.2→§4.2`, `§8.3→§4.10`, `§8.4→§4.4`; all `[reconciliation.md §9.N]` → `[reconciliation/spec.md §4.N]`/`§8` per context; `[control-points.md §6.5]→§6.3` (policy schema) or `§4.7` CP-037 (precedence) per context; `§6.8→§4.7`, `§6.9→§4.5`, `§6.11→§4.11`. **IMPORTANT findings applied:** I1 — added §A.4 Reverse-drift migration map publishing ON's legacy `§7.N → §4.N` anchors for downstream inbound citations. I2 — renamed `handler_silent_hang → agent_warning_silent_hang` in ON-040 per EV §8.3.10 / HC §7.1. I3 — collapsed `operator_pausing` + `operator_paused` into `operator_pause_status` paired-phase event per [event-model.md §8.9(h)]; ON-013, §6.5, §7.1 rewritten. I4 — added `operator_command_rejected` (§8 code 16) and `dispatch_deferred` (§8 code 18) to §6.5 and ON-013. I5 — flagged PL drain-adoption as OQ-ON-006 (not edited in PL). I6 — promoted structured-logs ownership to ON-owned in ON-035 with a minimum wire-format shape; detailed schema deferred to OQ-ON-007 (`quality-checks.md`). I7 — expanded exit-code taxonomy (§8) with codes 19 (runtime-panic), 20 (signal-terminated), 21 (drain-step-errored). I8 — §5 invariants audit: retained ON-INV-001 (sensor added), ON-INV-003 (sensor added); retired ON-INV-002 (operational posture moved to §2.1a scope assumption) and ON-INV-004 (restatement of §4); rewrote ON-INV-005 as cross-subsystem reconstruction-contribution invariant with sensor; added ON-INV-006 (no control surface bypasses the between-task invariant) with sensor. I9 — resolved RTO target X: ON-031 set to 30s nominal fixture / 300s ceiling with declared restart-RTO test harness sensor; residual ambiguity on auto-escalate-vs-notify and fixture-tightening tracked in revised OQ-ON-005. I10 — expanded §4.11 with ON-047 (category defaults table: token/wall-clock/iterations/reconciliation/warning-threshold), ON-048 (exhaustion protocol: emit `budget_exhausted`, terminate at safe boundary, route through pause-or-escalate policy, cascade to `dispatch_deferred` on machine-ceiling), ON-049 (attribution shape `(run_id, role, node_id, category, amount)` plus `delegation_path` for cognition-tagged steps). I11 — clarified ON-041: machine-ceiling applies per-daemon vs machine-level distinction; multi-tenancy deferral of ON-042 applies to multi-daemon coordination policy only, not to the ceiling mechanisms themselves. I12 — marked OQ-ON-001 and OQ-ON-003 explicitly as "unresolved" with architect-honest rationale. **Template obligations:** added §4.a Subsystem envelope (ON-ENV-001) declaring (a)–(h) envelope elements. Although AR-053 exempts foundation-cross-cutting specs, this envelope is published voluntarily because ON emits cross-subsystem events and downstream specs benefit from a canonical envelope citation. Envelope requirement IDs use the reserved `ON-ENV-NNN` range and do not consume topical `ON-NNN` ID space. Every new requirement carries a `Tags:` line; new requirements with I/O or state mutation carry `Axes:` lines (ON-048). **New IDs (net):** ON-047, ON-048, ON-049 (three new §4.11 requirements); ON-INV-006 (new invariant); ON-ENV-001 (envelope). **Retired IDs (never reusable):** ON-INV-002 (operational posture, moved to §2.1a), ON-INV-004 (restatement of §4). **New OQs:** OQ-ON-006 (PL adopting ON-027 drain steps — cross-spec coordination with PL's next revision), OQ-ON-007 (quality-checks.md wire-format home for structured logs). **Cross-spec coordination deferred:** PL drain-adoption (OQ-ON-006), quality-checks wire format (OQ-ON-007), multi-operator attach arbitration (OQ-ON-004 existing), structured-log parser contract + sensor (OQ-ON-007). Reverse-drift §A.4 added to help downstream specs migrate inbound `§7.N` / `§8.N` citations to current `§4.N`/`§6.N`/`§8.N` anchors. IDs preserved throughout; no ON-NNN renumbering. |

## A. Appendices

### A.3 Rationale

**Why operator controls are spec'd as semantics, not as a CLI surface.** The CLI surface (flag names, argument order, output formatting) is churny and should be free to change without triggering a normative revision of every subsystem that depends on operator-control semantics. Splitting semantics into this spec and surface into a separate spec (deferred) protects the between-task invariant from flag-renaming noise. See [problem-space.md §Non-goals] Q-F1 resolution.

**Why the between-task invariant is a locked decision.** Allowing pause or upgrade to abort in-flight runs would make every run's durability contract contingent on "unless operator pauses mid-run," which destroys the checkpoint-trail guarantee of [execution-model.md §4.5] and the state-reconstruction contract of [execution-model.md §4.7]. `stop --immediate` is the single carve-out because emergency abort is a real operational need; forcing graceful shutdown in every case would leave operators unable to recover from a genuinely stuck daemon. This is locked decision #10 and reopening it requires strong new evidence.

**Why N-1 and not N-2 or wider.** N-1 is the smallest window that lets operators upgrade without coordinating the daemon with the on-disk state. Wider windows (N-2, N-3) increase reader code complexity without proportional benefit — MVH is single-operator, single-machine, and the migration cost at a break is a short operator-paused ritual. Post-MVH the window may widen if multi-operator or fleet-scale deployments appear.

**Why the 300-second RTO hard ceiling is non-negotiable.** Below 300 seconds an operator can reasonably wait at the terminal for startup to complete; above 300 seconds the operator will start investigating, and the daemon must be able to distinguish "still starting" from "stuck." The ceiling is the boundary where the degraded-notification obligation kicks in. Choosing a ceiling is unavoidable; 300s matches operator-patience research (cited from [problem-space.md] recon findings) and leaves headroom above the nominal 30s p95 target.

**Why multi-tenancy is deferred, not solved.** Per-project daemon isolation is a genuine MVH answer for the common solo-developer case, and it scales gracefully to "a few projects at once on one machine." What it does NOT address — shared LLM quotas, shared skill installations, shared operator identity — is not tractable at MVH without a machine-level coordinator that would itself need a process-lifecycle contract, a failure story, and a reconciliation protocol. Deferring is cheaper than committing to a half-designed coordinator. The acknowledgment in §4.10.ON-042 is load-bearing: "deferred ≠ dismissed" is the posture that keeps the door open for post-MVH amendment without re-opening the foundation.

### A.4 Reverse-drift migration map — §7.N / §8.N legacy → §4.N current

This table is published to help downstream specs migrate their inbound citations. Multiple peer specs (both reviewed and drafted) cite ON at legacy `§7.N` (operator-control, drain, RTO) and `§8.N` (exit-code taxonomy) anchors that derived from an earlier components.md layout and no longer resolve against ON v0.3. Each peer spec's next revision cycle SHOULD apply this mapping. The migration is tracked corpus-wide in OQ-WM-011 and its successors.

| Legacy anchor pattern | Current ON v0.3 anchor | Content |
|---|---|---|
| `[operator-nfr.md §7.1]` (legacy operator-control) | `§4.3` (between-task semantics) PLUS `§7.1` (state-machine table, ON v0.3 retains this number) | Operator-control state machine and between-task invariant |
| `[operator-nfr.md §7.2]` (legacy drain protocol) | `§4.7` (ON-027 eight-step drain) PLUS `§7.2` (pseudocode, retained) | Graceful-shutdown ordering |
| `[operator-nfr.md §7.3]` (legacy upgrade protocol) | `§4.6` (ON-020, ON-021 upgrade contract) PLUS `§7.3` (pseudocode, retained) | `harmonik upgrade` contract |
| `[operator-nfr.md §7.4]` (legacy observability envelope) | `§4.9` (ON-034 through ON-040) | Observability envelope and silent-hang obligation |
| `[operator-nfr.md §7.5]` (legacy schema compat window) | `§4.5` (ON-018, ON-019) | N-1 compat window |
| `[operator-nfr.md §7.6]` (legacy multi-daemon) | `§4.10` (ON-041 through ON-044) | Multi-daemon coordination and multi-tenancy deferral |
| `[operator-nfr.md §7.7]` (legacy resource budgets) | `§4.11` (ON-045 through ON-049) | Resource budgets, defaults, exhaustion, attribution |
| `[operator-nfr.md §7.8]` (legacy queue-format compat) | `§4.4` (ON-015 through ON-017) | Queue-format compatibility contract |
| `[operator-nfr.md §7.9]` (legacy secrets + shutdown) | `§4.7` (ON-022 through ON-029) | Secrets redaction and graceful shutdown |
| `[operator-nfr.md §7.10]` (legacy restart RTO) | `§4.8` (ON-030 through ON-033) | Restart RTO target, criteria, measurement boundary |
| `[operator-nfr.md §7.11]` (legacy integrity gate) | `§4.2` (ON-005, ON-006) | Commit-hash integrity gate |
| `[operator-nfr.md §7.12]` (legacy exit-code taxonomy) | `§8` (table) | Exit-code taxonomy |
| `[operator-nfr.md §8.N exit-code rows]` | `§8` (table, codes 0–21 stable across N-1) | Exit-code taxonomy rows |

Downstream specs inbound-citing ON events (`operator_pause_status`, `operator_stopped`, `operator_upgrading`, etc.) MUST target `[event-model.md §8.7]` for payload shape and `[operator-nfr.md §4.3]` / `§6.5` for emission timing (per EV-025 payload-shape ownership rule).
