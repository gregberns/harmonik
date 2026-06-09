# Process Lifecycle

```yaml
---
title: Process Lifecycle
spec-id: process-lifecycle
requirement-prefix: PL
status: reviewed
spec-shape: requirements-first
spec-category: runtime-subsystem
version: 0.4.8
spec-template-version: 1.1
owner: foundation-author
last-updated: 2026-05-15
depends-on:
  - architecture
  - execution-model
  - event-model
  - handler-contract
  - control-points
  - reconciliation
  - beads-integration
  - workspace-model
  - queue-model
---
```

## 1. Purpose

This spec defines harmonik's process lifecycle: the per-project headless daemon, its startup and shutdown sequences, the local socket and pidfile layout, the agent-subprocess relationship, the composition-root boundary between the deterministic daemon and any cognition-bearing orchestrator-agent, and the crash semantics that make restart deterministic. It names cross-cutting obligations (the `harmonik upgrade` contract and the silent-hang detection obligation) that other specs cite by reference.

The spec exists separately from `operator-nfr.md` because the daemon is the process-shape carrier — its pre-`ready` states and its composition root are the structural preconditions every operator-control requirement ([operator-nfr.md §4.3]), restart-RTO measurement ([operator-nfr.md §4.8]), and graceful-shutdown ordering ([operator-nfr.md §4.7]) depends on. Folding this content into `operator-nfr.md` would entangle structural process rules with non-functional operator surfaces.

## 2. Scope

### 2.1 In scope

- Per-project daemon scope: one daemon per `.harmonik/` directory, socket and pidfile layout.
- Startup sequence, including the pre-classification orphan sweep (tmux, worktree locks, subprocesses, stale intent files) and the composition-root bootstrap (event bus, registries, JSONL writer).
- Ready-state transition criteria and the `starting` → `reconciling` → `ready` prefix of the daemon status machine (plus the `degraded` pre-ready side state).
- Shutdown semantics: graceful (drain in-flight runs) and immediate (abort).
- Agent-subprocess spawning, parentage, and daemon-ward socket communication.
- Daemon-vs-orchestrator-agent distinction: the daemon is a deterministic Go binary with no LLM logic.
- ntm adapter scope: bounded to process/tmux concerns; SwarmPlan, checkpoint/recovery, and Agent Mail explicitly NOT consumed; absence-detection and version-pinning for the ntm dependency.
- Crash semantics for daemon crash, crash during startup reconciliation, and agent-subprocess crash.
- Daemon-internal mechanics of the `harmonik upgrade` contract (exec-replacement, socket continuity, pidfile handoff); operator-facing semantics are owned by operator-nfr.
- Named obligation for silent-hang detection (owned by [handler-contract.md §4.6]).
- CLI command surface the daemon exposes; wire format of the daemon socket.

### 2.2 Out of scope

- Reconciliation classification, category taxonomy, investigator contract, and verdict vocabulary — owned by [reconciliation/spec.md §4.2, §4.3, §4.4, §4.5, §8].
- Handler launch mechanics, watcher goroutine, subprocess error propagation, and cancellation timing — owned by [handler-contract.md §4.1, §4.3, §4.4, §4.6].
- Workspace leasing, worktree creation, and lease-lock implementation — owned by [workspace-model.md §4.1, §4.3, §4.8].
- Operator command semantics for pause / stop / upgrade beyond daemon state-prefix — owned by [operator-nfr.md §4.3, §4.6, §4.7]. `harmonik status` reporting of the daemon-status-enum values defined in §6.1 is owned by this spec; reporting of semantic content beyond the enum is owned by operator-nfr.
- Operator-facing `harmonik upgrade` contract content (binary-source flag, operator-supplied hash-check procedure, cross-version schema compat) — owned by [operator-nfr.md §4.6 ON-020].
- Graceful-shutdown step ordering across subsystems beyond daemon-level sequencing — owned by [operator-nfr.md §4.7 ON-027].
- Restart RTO numeric target and its measurement criteria — owned by [operator-nfr.md §4.8].
- Event payload schemas for lifecycle events — owned by [event-model.md §6.3, §8.7].
- Startup failure-mode catalog content (exit codes per prerequisite failure) — owned by [operator-nfr.md §4.1 ON-003] spec-draft obligation + [reconciliation/spec.md §4.3 RC-012] Cat 0 pre-check.
- Queue document schema, queue-method payload schemas, queue persistence layout, and queue-status semantics — owned by [queue-model.md §2, §3, §6, §7]. PL owns the socket transport, the persisted-queue startup read, the CLI dispatch shape for `hk queue *`, and the file-surface inventory entry only.

## 3. Glossary

- **daemon** — the per-project headless Go process that owns workflow state, dispatches runs, and exposes the local socket. Deterministic; never calls an LLM. (see §4.1, §4.6)
- **orchestrator-agent** — a separate Claude Code session (cognition-bearing) that drives the daemon via its CLI. Post-MVH; NOT a component of the daemon. (see §4.6)
- **project** — a git repo containing a `.harmonik/` directory. One daemon per project. (see §4.1)
- **pidfile** — the file at `.harmonik/daemon.pid` holding the running daemon's PID; lock asserts per-project singularity. (see §4.1)
- **socket** — the local Unix socket at `.harmonik/daemon.sock` used for daemon ↔ agent-subprocess and daemon ↔ CLI-client communication. (see §4.1, §4.5)
- **orphan** — a harmonik-owned resource (tmux session, worktree lock, subprocess, stale intent file) surviving a prior daemon instance that no current daemon tracks in memory. (see §4.2)
- **ntm adapter** — the thin layer that consumes ntm's process/tmux capabilities (spawning agents in panes, profile-based ready-state detection, rate-limit signals) while ignoring ntm's workflow-semantic features. (see §4.7)
- **composition root** — the `internal/daemon` Go package that wires all subsystems together and is the only package allowed to import most subsystems (per [architecture.md §4.4] subsystem-envelope rule).
- **project hash** — a stable identifier derived from the project root path used to scope tmux sessions and process-provenance markers to a single per-project daemon. (see §4.2 PL-006a)

## 4. Normative requirements

### 4.a Subsystem envelope

#### PL-ENV-001 — Envelope declaration

Envelope for the process-lifecycle subsystem (daemon core / `internal/daemon`) per [architecture.md AR-053].

(a) Events produced:
  - `daemon_started` — emitted on transition (init) → `starting` per §7.1; payload schema in [event-model.md §8.7.1].
  - `daemon_ready` — emitted on transition to `ready` per PL-009; payload schema in [event-model.md §8.7.2].
  - `daemon_shutdown` — emitted during drain per PL-011a; payload schema in [event-model.md §8.7.3].
  - `daemon_startup_failed` — emitted on fatal startup prerequisite failure per PL-008; payload schema in [event-model.md §8.7.4].
  - `daemon_degraded` — emitted on Cat 0 prerequisite failure per PL-010; payload schema in [event-model.md §8.7.5]. (Post-ready degradation reasons are owned by health-surface consumers; see OQ-PL-009.)
  - `daemon_orphan_sweep_completed` — emitted on completion of PL-006; payload schema in [event-model.md §8.7.14].
  - `infrastructure_unavailable` — emitted on Cat 0 prerequisite failure per PL-010; payload schema in [event-model.md §8.7.15].
  - `dispatch_deferred` — emitted when the per-daemon concurrency ceiling (PL-014a) defers a dispatch; payload schema in [event-model.md §8.7.13].

(b) Events consumed:
  - `reconciliation_category_assigned` — consumed at PL-005 step 7 to decide dispatch; payload schema in [event-model.md §8.6] and emission per [reconciliation/spec.md §4.3 RC-013].
  - `reconciliation_verdict_executed` — consumed to clear investigator workflows that blocked `ready` reporting; payload schema in [event-model.md §8.6].
  - `operator_pause_status`, `operator_resuming`, `operator_stopped`, `operator_upgrading`, `operator_upgrade_completed`, `operator_upgrade_rejected`, `operator_command_rejected` — consumed by the daemon status layer per [operator-nfr.md §4.3]; emission is daemon-core-sourced (this subsystem emits them too — see [event-model.md §8.7.6–§8.7.12]). PL owns emission timing for the `starting/reconciling/ready/degraded` prefix; operator-nfr owns timing for the `paused/draining/stopped/upgrading` suffix.

(c) Types introduced (cross-subsystem):
  | Type | `Tags:` | `Axes:` (if non-baseline) |
  |---|---|---|
  | `DaemonStatus` (§6.1 ENUM) | mechanism | baseline |
  | `PidfileLock` (§6.1) | mechanism | `io-determinism=deterministic; idempotency=non-idempotent` (fd-lifetime lock) |
  | `ProjectHash` (§6.1) | mechanism | baseline |
  | `ProvenanceMarker` (§6.1) | mechanism | `io-determinism=deterministic; idempotency=non-idempotent` |
  | `OrphanSweepReport` (§6.1) | mechanism | baseline |
  | `SocketWireRequest` / `SocketWireResponse` (§6.1) | mechanism | baseline |

(d) Handlers implemented: none. The daemon core hosts handlers via the handler-contract surface; the concrete handler packages are separate subsystems per [handler-contract.md §4.12].

(e) State owned:
  - `DaemonStatus` (in-memory, observable via socket and `harmonik status`).
  - `daemon_instance_id` (in-memory; UUIDv7 minted at PL-005 step 0; persisted to `.harmonik/daemon.instance-id` and pidfile line 3).
  - Pidfile at `.harmonik/daemon.pid` (fd-lifetime advisory lock per PL-002a; three lines: PID / PGID / `daemon_instance_id`).
  - Unix socket at `.harmonik/daemon.sock` (mode `0600`; HC-044 authentication model).
  - `.harmonik/daemon.instance-id` file (atomic write per PL-005 step 0).
  - `.harmonik/daemon.upgrading` marker (atomic write per PL-027(iv); content owned by [operator-nfr.md §4.6 ON-020a]).
  - `workflow_mode_default` (in-memory enum value in `{single, review-loop, dot}`; loaded once at PL-005 step 0 per PL-004a; observable via `harmonik status`).
  - Project-hash derived tmux-session namespace.
  - Composition-root-resident registries (event bus, handler registry, skill registry, control-point registry, policy registry) per AR-INV-007 (PL-020a).
  - Daemon-emitted file surface per PL-004.

(f) Control points provided: none. The daemon core is mechanism-tagged; it HOSTS the control-point registry but does not itself declare gates, hooks, guards, or budgets per [control-points.md §4.1]. Control points are declared by their owning subsystems and registered inside the daemon process per PL-020a.

(g) NFRs inherited / overridden:
  - Inherited: `ON-002` exit-code taxonomy (PL-008 consumes the catalog).
  - Inherited: `ON-004` configuration-knob inventory (PL-013's prior re-query cadence knob removed per the extqueue v0.1 retirement; PL no longer carries the cadence config-surface item).
  - Inherited: `ON-020` `harmonik upgrade` operator-facing contract (PL-027 names the daemon-internal side).
  - Inherited: `ON-027` graceful-shutdown cross-subsystem ordering (PL-011 names the daemon-level sequence).
  - Inherited: `ON-031` restart RTO (PL-009 defines the `daemon_ready` measurement endpoint).
  - Inherited: `ON-041` multi-daemon coordination (PL-014a declares per-daemon concurrency ceiling; machine-level coordination is deferred to operator-nfr).
  - Overridden: none.

(h) Boundary classification per operation:
  | Operation | `Tags:` | Axes |
  |---|---|---|
  | `acquire_pidfile_lock` (§4.1 PL-002) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |
  | `bind_socket` (§4.1 PL-003) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |
  | `bootstrap_registries` (§4.2 PL-005 step 0) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `mint_daemon_instance_id` (§4.2 PL-005 step 0) | mechanism | `llm-freedom=none; io-determinism=non-deterministic; replay-safety=safe; idempotency=non-idempotent` |
  | `read_startup_markers` (§4.2 PL-005 step 8a) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `orphan_sweep` (§4.2 PL-006) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `cat_0_precheck` (§4.2 PL-005 step 3) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `dispatch_reconciliation` (§4.2 PL-005 step 7) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent` |
  | `graceful_drain` (§4.4 PL-011) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |
  | `spawn_agent_subprocess` (§4.5 PL-014) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |
  | `exec_replace_on_upgrade` (§4.9 PL-027) | mechanism | `llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent` |

Tags: mechanism

### 4.1 Per-project daemon scope

#### PL-001 — One daemon per project

Each project (a git repo containing a `.harmonik/` directory) MUST run exactly one daemon. Multiple projects on the same machine MAY run multiple independent daemons, one per project. The daemon has no awareness of other daemons on the same machine (multi-daemon operator commands are owned by [operator-nfr.md §4.10 ON-041]).

Tags: mechanism

#### PL-002 — Pidfile at `.harmonik/daemon.pid`

The daemon MUST write its PID to `.harmonik/daemon.pid` on startup and MUST hold an advisory lock on that file for the duration of its lifetime. A second daemon invocation against the same project that finds the pidfile held by a live process MUST exit with exit code `5` "pidfile-locked" per [operator-nfr.md §8], directing the operator to query the running daemon.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### PL-002a — Pidfile lock is fd-lifetime advisory

The pidfile lock MUST be acquired via a fd-lifetime advisory lock primitive: `flock(LOCK_EX|LOCK_NB)` on macOS; `flock(LOCK_EX|LOCK_NB)` or `fcntl(F_OFD_SETLK)` on Linux. The lock MUST be released automatically by the kernel on daemon-process termination (clean OR crash) so that a subsequent daemon invocation can acquire the lock on restart without operator intervention. POSIX process-lifetime `fcntl(F_SETLK)` locks are FORBIDDEN because any fd-close inside the process releases the lock, which is unsafe across goroutine/fd-lifecycle boundaries. The daemon MUST disambiguate (a) "pidfile present, lock held by live process" (second-daemon-attempt; exit 5 per PL-002) from (b) "pidfile present, no lock, recorded PID not live" (stale pidfile left by crash; remove file and proceed per PL-024) by attempting `flock` first and, on failure, reading the recorded PID and probing with `kill(pid, 0)`. The daemon MAY corroborate via `/proc/<pid>/cmdline` (Linux) or `proc_pidpath` (darwin) to disambiguate recycled PIDs; behavior on ambiguity is to refuse startup with a specific exit code (OQ-PL-007 tracks PID-reuse-on-reboot handling).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### PL-002b — Atomic pidfile write via truncate-rewrite-keep-fd

The daemon MUST write the pidfile content atomically by holding the open fd for the daemon's lifetime, using the truncate-rewrite pattern (NOT temp+rename, which would break the flock association by giving the new file a different inode):

1. `open(".harmonik/daemon.pid", O_RDWR|O_CREAT|O_CLOEXEC)` — open with create-if-absent, do NOT use `O_TRUNC`.
2. Acquire the fd-lifetime advisory lock per PL-002a (`flock(LOCK_EX|LOCK_NB)`).
3. Only after successful lock acquisition: `ftruncate(fd, 0)`.
4. Write the pidfile's three lines, each terminated by `\n`: line 1 = the daemon's PID (ASCII decimal integer); line 2 = the daemon's PGID (ASCII decimal integer); line 3 = the daemon's `daemon_instance_id` (UUIDv7 per PL-005 step 0; lowercase canonical hyphenated form, 36 ASCII characters). Short writes MUST loop. Readers MUST tolerate one-line pidfiles for backward compatibility with v0.2.x format and two-line pidfiles for backward compatibility with v0.4.0 format (a missing line 3 is treated as `daemon_instance_id = unknown` for read paths and prevents instance-correlation; line 3's absence does NOT invalidate the pidfile for liveness probing per PL-002a).
5. `fsync(fd)`; `fsync(parent_directory_fd)` (the parent-directory fsync is REQUIRED — without it, a power-loss after step 4 can lose the file content on APFS and ext4-data=ordered).
6. Retain the fd for the daemon's lifetime. Intermediate `close()` is FORBIDDEN.

Writing the PID BEFORE acquiring the lock (in step 1) is FORBIDDEN. A torn or unparseable pidfile observed by a subsequent daemon startup MUST be treated as stale per PL-024. On `flock` failure with `errno != EAGAIN && errno != EWOULDBLOCK` (e.g., `ENOLCK`, `EBADF`, `ENOTSUP` from a non-supporting filesystem), the daemon MUST exit with code 9 (`filesystem-unwritable` per ON §8) and emit `daemon_startup_failed{failure_mode="pidfile-lock-error"}`.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### PL-003 — Socket at `.harmonik/daemon.sock`

The daemon MUST listen on a local Unix socket at `.harmonik/daemon.sock`. Agent subprocesses and CLI clients (`hk queue submit`, `harmonik status`, etc.) MUST communicate with the daemon exclusively through this socket. The daemon MUST remove a stale socket file on startup before binding. After bind, the daemon MUST `chmod(0600)` the socket file and MUST ensure its owner is the daemon's effective uid; socket authenticity is filesystem-permission-based per [handler-contract.md §4.10 HC-044].

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### PL-003a — Socket wire format for CLI / agent requests

The daemon's Unix socket MUST carry a JSON-RPC 2.0 request/response stream framed as newline-delimited JSON per [handler-contract.md §4.2 HC-007a] (same NDJSON framing discipline; one JSON object per line terminated by `\n`; max line length 1 MiB; lines exceeding the cap abort the connection). CLI clients MUST issue one JSON-RPC request per connection and close the connection on receipt of the response. Agent subprocesses MAY hold their connection for the lifetime of the session per [handler-contract.md §4.3]; CLI connections MUST NOT. The JSON-RPC method set spans the CLI commands of PL-028 plus the agent-facing commands named in PL-015. The JSON-RPC method names exposed on the daemon socket are: agent-facing (`claim-next`, `emit-outcome`, `dispatch-status`, Beads-CLI-skill proxy methods per [beads-integration.md §4.9 BI-027]); CLI-facing (`status`, `pause`, `resume`, `stop`, `upgrade`, `attach`, `list` per [operator-nfr.md §4.10 ON-041]; plus the v0.1 queue method set: `queue-submit`, `queue-append`, `queue-status`, `queue-dry-run` per [queue-model.md §6]); daemon-internal / introspection (`get-agent-count` — returns `{count: <integer>}` reporting the number of currently-tracked live handler subprocesses; consumed by the cross-daemon machine-ceiling drift-reconciliation surface of [operator-nfr.md §4.10 ON-041] for periodic comparison of tracked-vs-running handler counts per project). Method payload schemas for non-queue methods are intentionally deferred; the names are the stable surface. The `get-agent-count` reply schema is pinned here (`{count: integer ≥ 0}`); semantic interpretation (drift threshold, escalation cadence) is owned by ON-041.

Queue method payload schemas and validation error codes (`-32010..-32019`) are owned by [queue-model.md §6] and [operator-nfr.md §8]; PL-003a fixes only the wire names. The queue method set is append-only at v0.1; the `queue-remove` / `queue-pause` / `queue-resume` / `queue-clear` methods sketched in `02-components.md §4` are deferred to v0.2 per [queue-model.md §8] pause-by-failure recovery. The `enqueue` method that appeared in this enumeration through v0.4.5 is retired in v0.4.6 per the queue-model.md §1 retirement of the prior bead-by-bead enqueue surface; PL-003a is the single authoritative method-name registry, so removal here is sufficient — there is no alias row, no compatibility shim.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### PL-003b — Pre-ready request rejection on unknown run_id

Between socket bind (PL-005 step 3a) and completion of the in-memory model build (PL-005 step 7), the daemon MUST reject any `emit-outcome`, `claim-next`, or other agent-originated request whose `run_id` is not recognized in the daemon's in-memory state, with a typed error `daemon_not_ready{reason="unknown_run_id"}`. CLI requests for daemon-status are exempt (they are ready-detection probes per PL-009b). Clients MUST treat the typed error as a bounded-retry condition with exponential backoff per the ready-protocol of PL-009b. This requirement closes the orphan-agent-reconnect-during-startup-window race in which an orphan agent's surviving socket connection lands on the new daemon's listener before the orphan sweep has reaped it.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### PL-004 — Daemon owns per-project files under `.harmonik/`

The daemon's per-project file surface includes: `.harmonik/daemon.pid` (PL-002, PL-002b); `.harmonik/daemon.sock` (PL-003); `.harmonik/daemon.instance-id` (PL-005 step 0; UUIDv7 per-process correlation key); `.harmonik/daemon.upgrading` (PL-027(iv); upgrade-intent durable marker per [operator-nfr.md §4.6 ON-020a]); `.harmonik/daemon.state` (per [operator-nfr.md §4.7 ON-030a]; pause-state durable marker — ON-owned content, PL-read at PL-005 step 8a); `.harmonik/queue.json` (per [queue-model.md §3 QM-001/QM-002/QM-003]; queue-manager-written via WM-026 atomic write, PL-read at PL-005 step 8a, unlinked on queue completion per QM-003); the event log and dead-letter files at `.harmonik/events/events.jsonl` and `.harmonik/events/dead-letters.jsonl` (per [event-model.md §6.2]); the event-ID high-water-mark file `.harmonik/event_id_hwm` (per [event-model.md §4.1]); per-consumer spill files at `.harmonik/events/spill-<consumer>.jsonl` (per [event-model.md §4.4]); the intent log directory at `.harmonik/beads-intents/` (per [beads-integration.md §4.10 BI-030] and [beads-integration.md §6.2]); the per-target-run reconciliation lock directory at `.harmonik/reconciliation-locks/` (per [reconciliation/spec.md §4.1 RC-002a]; reconciliation-manager-written, swept by PL-006); and the per-run lease-lock file at `<workspace_path>/.harmonik/lease.lock` (per [workspace-model.md §4.3 WM-013a] — this file is workspace-manager-written, but the daemon hosts the workspace manager in-process per PL-020a). The daemon MUST NOT read or write harmonik-owned state outside this surface; subsystem-scoped state (workspace directories, session log directories) is owned by the respective subsystems per their specs.

> NOTE: Transition-record persistence is the checkpoint-commit trail in git per [execution-model.md §4.4 EM-017]; there is no daemon-maintained `.harmonik/transitions/` directory. Earlier drafts of this spec erroneously named that directory; it has been removed.

Tags: mechanism

#### PL-004a — Default workflow mode

The daemon's project-level configuration MAY carry an optional `workflow_mode` field whose value MUST be one of the enum `{single, review-loop, dot}`. When the field is absent, the daemon's default workflow mode MUST be `single`. The field MUST be read exactly once during PL-005 step 0 (composition-root bootstrap) and MUST be cached in-process as `workflow_mode_default` for the daemon's lifetime; the daemon MUST NOT re-read the field after bootstrap. Mid-run mode change via this surface is FORBIDDEN; an operator-initiated rotation of the daemon-level default requires a daemon restart (or a `harmonik upgrade` exec-replacement per PL-027, which re-runs step 0).

The daemon-level `workflow_mode_default` is the second-lowest-precedence tier of the four-tier workflow-mode resolution chain owned by [execution-model.md §4.3]: per-task (bead `workflow:<mode>` label per [beads-integration.md §4.3 BI-009a]) → per-project → daemon-level (this requirement) → built-in fallback (`single`). The daemon MUST NOT override higher-precedence tiers (per-project or per-task) when both are present; resolution is the responsibility of the dispatch path per [execution-model.md §4.3].

The on-disk location of the `workflow_mode` field is the daemon's existing project-config surface read at PL-005 step 0; this requirement does not introduce a new config file. The field is observable via `harmonik status` (passive surface, §PL-028); it is NOT operator-controllable through any other command at runtime.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

### 4.2 Startup sequence

#### PL-005 — Startup order is deterministic

The daemon's startup sequence MUST execute the following steps in order, and each step MUST complete before the next begins:

0. **Composition-root bootstrap.** Instantiate the event bus ([event-model.md §4.3]), the control-point registry ([control-points.md §4.1]), the handler registry, the skill registry ([handler-contract.md §4.11]), and the policy registry — all in-process per [architecture.md AR-INV-007] and PL-020a. Register each subsystem's consumers and providers. Start the JSONL writer ([event-model.md §6.2]). The daemon MUST also load the `workflow_mode_default` value per §PL-004a from the project-config surface and cache it for the daemon's lifetime; absence of the field defaults the cached value to `single`. The daemon MUST also mint a `daemon_instance_id` (UUIDv7 per [event-model.md §4.1] ID-generation discipline) and MUST write it to `.harmonik/daemon.instance-id` via the temp+rename+fsync(parent_dir) atomic discipline of [workspace-model.md §4.7 WM-026]: write content to a sibling temp file `.harmonik/daemon.instance-id.tmp-<pid>`; `fsync(temp_fd)`; `rename(2)` to the canonical name; `fsync(parent_directory_fd)`. The `daemon_instance_id` is the per-process correlation key used by every lifecycle event payload, the pidfile (PL-002b line 3), and external attach/audit consumers per [operator-nfr.md §4.10 ON-041]. A new instance MUST mint a fresh UUIDv7; reuse across exec-replacement (PL-027) is FORBIDDEN — the new daemon binary mints its own UUIDv7 even when adopting the listener fd from the outgoing instance. No external state is read in this step.
1. Acquire the pidfile lock (§PL-002). Exit on lock-contention failure with exit code `5` per [operator-nfr.md §8].
2. Emit `daemon_started` (per [event-model.md §8.7.1]) with `{started_at, pid, binary_commit_hash}`.
3. Execute the orphan sweep per §PL-006.
3a. **Bind Unix socket** at `.harmonik/daemon.sock` per PL-003. Begin accepting connections.
4. Cat 0 pre-check per [reconciliation/spec.md §4.3 RC-012]; on prerequisite failure, enter `degraded` state per §PL-010 and do not proceed to the next step until prerequisites clear.
5. Walk the git log for the project's repo, collecting checkpoint commits identified by the `Harmonik-Run-ID` trailer per [execution-model.md §6.2]. The walk MUST scan commits reachable from every task branch matching the naming convention `run/<run_id>` declared in [workspace-model.md §4.2 WM-005]; detection is filesystem-based (`git for-each-ref refs/heads/run/`). No separate run-registry is maintained; branch naming is the authoritative in-flight-run index.
6. Query Beads via `br ready` for dispatchable beads and via the equivalent audit-log / in-progress reads of [beads-integration.md §4.5 BI-013, BI-016] for reconciliation input. Each `br` invocation MUST carry a request timeout T ≤ 5s per [reconciliation/spec.md §8.1]; on timeout the pre-check classifies as Cat 0 per §PL-010.
7. Build the in-memory model of completed, open, and in-flight runs from git + Beads state, using the `Run` record shape of [execution-model.md §6.1].
8. Dispatch reconciliation per [reconciliation/spec.md §4.2 RC-008] action-mapping: auto-resolvable categories resolve inline; investigator-required categories dispatch reconciliation workflows.
8a. **Read durable startup markers and gate step 9.** Read `.harmonik/daemon.state` (per [operator-nfr.md §4.7 ON-030a]), `.harmonik/daemon.upgrading` (per [operator-nfr.md §4.6 ON-020a] and PL-027(iv)), and `.harmonik/queue.json` (per [queue-model.md §3 QM-002]). Marker semantics:
   - **`.harmonik/daemon.upgrading` present.** The new daemon adopts upgrade-continuation semantics: (a) verify the on-disk binary's commit hash matches the marker's `expected_commit_hash`; on mismatch, refuse startup with ON §8 code 14 (`upgrade-hash-mismatch`) and emit `daemon_startup_failed{failure_mode="upgrade-hash-mismatch-on-restart"}` per ON-020a; (b) on match, do NOT re-run Cat 0 pre-check repeats already covered by the outgoing instance's pre-exec drain (the pre-check completed in step 4 stands as authoritative for this startup); (c) remove the marker via `unlink` followed by `fsync(parent_directory_fd)` after a clean transition to `ready` per ON-020a's removal contract. The marker MUST also be removed by the rollback path of [operator-nfr.md §4.6 ON-020(g)] when invoked.
   - **`.harmonik/daemon.state` present.** Read the persisted `DaemonStatus` plus pause-reason discriminator. If the persisted state is `paused`, `pausing`, `upgrade-prepare`, or `stopped`, the daemon MUST initialize step 9 into that persisted state rather than `ready`, preserving operator intent across crashes per ON-030a. The `paused` / `pausing` / `upgrade-prepare` states are owned by [operator-nfr.md §4.3]; PL transitions into the persisted suffix-state by emitting the corresponding operator-control event (`operator_pause_status{paused}`, etc.) at step 9 in lieu of `daemon_ready`. The `stopped` persisted state means the prior daemon completed shutdown durably; the new daemon proceeds normally to `ready` and the marker is overwritten on the first `running`-class transition. The marker MUST be removed on clean transition to `running` via the same atomic rename-or-unlink discipline ON-030a defines.
   - **`.harmonik/queue.json` present.** Read the persisted queue document per [queue-model.md §3 QM-002]. If the file parses and its `schema_version` is recognized, the in-memory queue is loaded with its persisted `status` and per-group/per-item statuses preserved. `paused-by-failure` and `paused-by-drain` queue statuses MUST be honored — no dispatch advances after step 9. If `schema_version` is unrecognized (forward-incompatible) the daemon MUST refuse startup with ON §8 code 2 (`queue-format-unsupported`, which covers both Beads overlay schema per ON-016 and `.harmonik/queue.json` schema mismatch) and emit `daemon_startup_failed{failure_mode="queue-format-unsupported"}`. The file MUST be absent in the unlinked-on-completion case per QM-003.
   - **All markers absent.** The daemon proceeds to step 9 and transitions to `ready` per the normal path.
   - **Marker file unreadable / corrupt.** Applies equally to `daemon.state`, `daemon.upgrading`, **and `queue.json`**: treat as absent; emit a structured-log warning per [operator-nfr.md §4.9 ON-035] naming the file. Do not block startup. For `queue.json` specifically, the daemon proceeds with no in-memory queue (waiting for a subsequent `queue-submit`). For the upgrading marker the operator-recovery path is `harmonik upgrade --rollback`; for the state marker the operator-recovery path is re-issuing the pause command.
9. Transition the daemon status to `ready` per §PL-009 and emit the `daemon_ready` status event UNLESS step 8a's marker-read selected a different terminal target state, in which case emit the corresponding operator-control event for that state per [operator-nfr.md §4.3] and the `daemon_ready` emission is deferred to whenever the daemon next transitions to `ready` from the persisted state.

The sequence (steps 0–9) is deterministic; no cognition participates. Investigator-workflow execution triggered by step 8 runs in parallel with `ready` and has its own per-workflow budget.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### PL-006 — Orphan sweep precedes reconciliation

Before the daemon executes the Cat 0 pre-check (§PL-005 step 4), the daemon MUST enumerate and clean up residual resources from any prior daemon instance. Every candidate for removal MUST carry a project-scoped provenance marker (per PL-006a) identifying it as this project's orphan; candidates without a valid marker MUST NOT be touched.

- **Tmux sessions.** The daemon MUST list tmux sessions matching the project's harmonik naming convention (prefix `harmonik-<project-hash>-` per PL-006a) and kill every matching session via `tmux kill-session`. Because the new daemon has no in-memory tracking at this point, every matching session in the project-scoped namespace is an orphan by definition. After kill, the daemon MUST poll for underlying process exit at a 100 ms cadence up to a 2-second ceiling (configurable per OQ-PL-002). After the ceiling expires, the daemon proceeds regardless; remaining processes are picked up by the re-parented-subprocess bullet below.
- **Worktree locks.** The daemon MUST enumerate worktrees by filesystem scan of `<repo>/.harmonik/worktrees/*/` per [workspace-model.md §4.1 WM-002]. No in-memory registry is required at sweep time. For each worktree, lease-lock files meeting the staleness criterion of [workspace-model.md §4.3 WM-013a] / §4.8 WM-033] MUST be removed. (Canonical lease-lock path alignment across HC-044a, WM-013a, and this spec is tracked as OQ-PL-004.)
- **Subprocess cleanup.** The daemon MUST identify processes that have been re-parented to init (parent pid 1) whose provenance marker per PL-006a matches this project's project hash, and kill them via SIGTERM followed by SIGKILL after a bounded 5-second interval consistent with [handler-contract.md §4.4 HC-018] cleanup bound. Identification MUST NOT rely on binary path alone; binary-path matching is insufficient on multi-project machines where the same handler binary serves multiple projects. Enumeration MUST cover BOTH (i) handler subprocesses (the agent-launching processes per PL-014) AND (ii) `br` (Beads CLI) subprocesses spawned via the BI adapter per [beads-integration.md §4.10 BI-029] — `br` subprocesses bear the same PL-006a provenance marker (env var + PGID) and re-parent to init on daemon crash exactly as handler subprocesses do; the SIGTERM-then-SIGKILL discipline is identical for both. The `br`-subprocess sweep extension was filed by the BI v0.4.0 R2 review (OQ-BI-010) and is pinned here in v0.4.1.
- **Stale intent files.** The daemon MUST enumerate `.harmonik/beads-intents/` for entries older than the current daemon's start time. Stale entries MUST be LEFT on disk for classification by the reconciliation Cat 3a detector per [reconciliation/spec.md §4.3 RC-013] during §PL-005 step 8; the orphan sweep itself MUST NOT invoke reconciliation detectors (which are gated on Cat 0 passing per [reconciliation/spec.md §4.3 RC-012] and would deadlock this pre-Cat-0 step). (Coordinated resolution with reconciliation is tracked as OQ-PL-006.)
- **Stale reconciliation locks.** The daemon MUST enumerate `.harmonik/reconciliation-locks/*.lock` (the per-target-run reconciliation locks introduced by [reconciliation/spec.md §4.1 RC-002a]). For each lock file, the daemon MUST attempt `flock(LOCK_EX|LOCK_NB)` to determine liveness (kernel auto-releases the advisory lock on the prior lock-holder's termination per PL-002a discipline); a successful acquisition followed by `flock(LOCK_UN)` confirms no live process holds the lock. Stale lock files (acquirable + the recorded creator-PID does NOT respond to `kill(pid, 0)`) MUST be removed via `unlink` followed by `fsync(parent_directory_fd)`. The sweep MUST NOT racily unlink a lock file currently being acquired by another daemon process — the `flock(LOCK_EX|LOCK_NB)` probe is the serialization point; if `EWOULDBLOCK` is observed the lock is in active use and MUST NOT be removed. Note: a stale lock file whose investigator task branch carries a `Harmonik-Verdict-Executed: true` commit per [reconciliation/spec.md §4.1 RC-002b] is also unlinked here (the lock outlived its useful purpose); a stale lock file without the executed-commit trailer routes the target run through Cat 3b per RC-002b — the orphan sweep removes the lock either way and the trailer-discriminator question is RC's, not PL's.
- **Stale `in_progress` bead markers.** The daemon MUST enumerate beads in coarse status `in_progress` via `br list --status in_progress --format json` (existing surface; see BI adapter `ListInFlightBeads`) and filter to those whose audit trail's most recent `in_progress` transition was authored by this project's daemon (provenance match via the `actor` field carrying this project's `project_hash` per PL-006a; OR — if Beads's audit `actor` field is unsuitable — by cross-referencing `claim` op entries in the daemon's own intent-log at `.harmonik/beads-intents/*.json`). For each such bead the daemon MUST apply the following exclusion conditions in order; a bead that satisfies ANY exclusion is NOT reset:
  - (a) **Live run reattached.** The in-memory model rebuilt at PL-005 step 7 re-attaches a live in-flight run to this bead (run survived as a re-parented subprocess and was reaped by the subprocess sweep, OR a `claim` intent file is still present and the BI adapter's BI-031 recovery will re-drive it).
  - (b) **Pending intent file.** A `close` or `reopen` intent file at `.harmonik/beads-intents/<key>.json` references this bead (Cat 3a handles it; the orphan sweep MUST NOT preempt the Cat 3a detector).
  - (c) **Merged commit present.** A merge commit on the target branch bears `Harmonik-Bead-ID: <bead_id>` (Cat 3c condition); the Cat 3c auto-resolver owns the close and the orphan sweep MUST NOT reset preemptively.
  - If none of the exclusions apply, the daemon MUST issue a `reset` write via the §4.8 BI adapter (BI-010d op: `in_progress → open`). The reset write MUST be idempotency-keyed as `<project_hash>:<bead_id>:reset:<daemon_start_ns>` and MUST be intent-logged identically to claim/close/reopen writes per [beads-integration.md §4.10 BI-030].
- **Event.** On completion, the daemon MUST emit `daemon_orphan_sweep_completed` (per [event-model.md §8.7.14]) with counts of tmux sessions killed, locks cleared, handler subprocesses killed, `br` subprocesses killed, reconciliation lock files removed, stale intents observed, and `bead_in_progress_reset` (count of `in_progress` beads reset to `open` by this sweep). Cross-spec coordination: the `bead_in_progress_reset` field is an additive payload extension to §8.7.14 — consistent with the precedent set by the `tmux_windows_killed` and `tmux_kill_window_survivors` additions (PL-021c); consumers MUST tolerate unknown integer fields per [event-model.md §6.3] N-1 compatibility. If [event-model.md §8.7.14] requires a schema bump rather than treating the addition as additive-tolerated, a companion EV revision bead (hk-iuaed.5) is already filed to address it.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### PL-006a — Project hash and provenance marker

The daemon MUST compute a stable `project_hash` at startup as the first 12 hexadecimal characters of `SHA-256(realpath(project_root))` (case-fold ambiguity remains tracked under OQ-PL-008). The hash MUST be stable across restarts (the same project root yields the same hash). The hash is used to:

(a) Scope tmux session names (`harmonik-<project_hash>-<session_name>`).
(b) Scope a provenance marker on every handler subprocess spawned by the daemon.

The provenance marker MUST be implemented by BOTH of the following to permit disambiguation across OS and tool differences: (i) setting the environment variable `HARMONIK_PROJECT_HASH=<project_hash>` on every spawned subprocess (readable via `/proc/<pid>/environ` on Linux); (ii) setting the subprocess's process group (PGID) to a deterministic per-project value as concretized below.

The daemon MUST call `syscall.Setsid()` immediately on startup (PL-005 step 0) before spawning any subprocess, producing a session whose PGID equals the daemon's PID at that moment. This PGID MUST be recorded in the pidfile per PL-002b (line 2). On every handler subprocess spawn, the daemon MUST set Go's `SysProcAttr{Setpgid: true, Pgid: <recorded_pgid>}` and MUST retry once on `EACCES` (the child has already called `execve`).

Subprocess trees that internally call `setsid` (e.g., handler wrappers using nohup-style tricks) escape the PGID marker; such handlers are out of conformance with PL-INV-005 and the orphan sweep cannot reap their descendants. This hazard is tracked as OQ-PL-011 (handler-side PGID-break disclosure).

The orphan sweep (PL-006) MUST match on the environment variable on Linux and on the PGID on darwin (where `/proc/<pid>/environ` is not available); darwin-specific fallback mechanics are tracked as OQ-PL-008.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### PL-006b — BeadResetter: bead-reset write extension point

The stale-`in_progress` bead-reset path of PL-006 (sixth bullet: `in_progress → open` via BI-010d) is abstracted behind a `BeadResetter` interface (`lifecycle.BeadResetter`). The interface exposes one method, `ResetBead(ctx, intentLogDir, cfg, beadID, projectHash, daemonStartNS) error`, which:

- Issues the BI-010d reset write (`in_progress → open`) for the identified bead.
- Applies the full BI-030 intent-log protocol — the write is intent-logged before execution, idempotency-keyed as `<project_hash>:<bead_id>:reset:<daemon_start_ns>`, and durably recorded identically to claim/close/reopen writes.
- Routes through the §4.8 `br`-CLI adapter; no direct `br` subprocess invocations are permitted per BI-012.

In production the interface is satisfied by `*brcli.Adapter` (`Adapter.ResetBead`); tests inject a fake. When `BeadResetter` (or the companion `InFlightBeadLedger` read surface) is nil, the bead-reset sweep is SKIPPED and the `bead_in_progress_reset` counter in the `daemon_orphan_sweep_completed` event is 0.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### PL-006c — BeadCat3cCloser: Cat 3c auto-resolver extension point

PL-006 exclusion condition (c) — a merged commit bearing `Harmonik-Bead-ID: <bead_id>` on the target branch — identifies a "subsumed-bead": the implementation has landed in git but the bead remains `in_progress`. The Cat 3c auto-resolver that owns the close for these beads is abstracted behind a `BeadCat3cCloser` interface (`lifecycle.BeadCat3cCloser`), wired into the orphan sweep directly (hk-lgtq2). The interface exposes one method, `SweepCloseBead(ctx, cfg, beadID) error`, which issues a `close` write for the identified bead.

The `close` write issued by `SweepCloseBead` routes through the BI-010b reconciliation-driven write path per [beads-integration.md §4.4 BI-010b] — it does NOT route through the BI-010d reset path and MUST NOT carry an `op=reset` idempotency key. Unlike `BeadResetter`, `SweepCloseBead` does NOT apply the BI-030 intent-log protocol: there is no associated in-flight run, so no `RunID` or `TransitionID` exists to form an intent-log key. Idempotency is provided at the Beads level — a bead already closed will not appear in the next startup's `br list --status in_progress` query and will not be presented to the sweep again.

When `BeadCat3cCloser` is non-nil and exclusion condition (c) applies, the sweep MUST call `SweepCloseBead` for the bead rather than skipping it. When `BeadCat3cCloser` is nil, exclusion condition (c) is treated as a skip: the bead remains `in_progress` until the next daemon restart supplies a non-nil closer or an operator closes it manually. Nil is the safe-but-incomplete behavior; the sweep MUST NOT issue a `reset` write for a Cat 3c bead — resetting would be incorrect (the bead should be closed, not returned to open).

In production the interface is satisfied by `*brcli.Adapter` (`Adapter.SweepCloseBead`).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### PL-006d — Orphan-sweep exclusion for coordinator/orchestrator tmux sessions

The orphan sweep of §PL-006 MUST NOT kill tmux sessions or windows that are actively owned by a live supervisor process per §PL-019. Without this exclusion, a daemon restart's sweep would inadvertently kill the flywheel pane (session name `harmonik-<project_hash>-flywheel`), terminating the cognition loop mid-cycle. This defect is tracked as hk-hc3qq.

**Exclusion mechanism (sentinel-file approach).** When `harmonik supervise start` creates the supervisor tmux session, it MUST write a sentinel file at `.harmonik/cognition/supervisor.sentinel` (content: `schema_version=1\n`) before issuing `tmux new-session`. The sentinel file MUST be removed by `harmonik supervise stop` or by the watch-shim on clean exit; it survives a supervisor crash and is re-written on `harmonik supervise restart`.

The orphan sweep MUST check for the sentinel BEFORE `tmux kill-session` against any session matching the `harmonik-<project_hash>-` prefix:
1. For each candidate session whose name matches the prefix, probe `.harmonik/cognition/supervisor.sentinel` via `stat(2)`.
2. If the sentinel is present AND `kill(supervisor_pid, 0) == 0` for the PID in `.harmonik/cognition/supervisor.pid`, the session MUST be SKIPPED. The daemon MUST emit a structured-log entry at INFO with key `orphan_sweep_skipped_coordinator_session` naming the session.
3. If the sentinel is absent OR the sentinel is present but the supervisor PID is no longer live (stale sentinel from a prior crash), the session is treated as an ordinary orphan and killed per the normal PL-006 path. The sweep MUST remove the stale sentinel via `unlink` + `fsync(parent_directory_fd)`.

**Impact on `daemon_orphan_sweep_completed` event.** The payload MUST gain a new integer field `coordinator_sessions_skipped: <integer ≥ 0>`. Additive payload extension consistent with PL-021c precedent; consumers MUST tolerate unknown integer fields per [event-model.md §6.3] N-1 compatibility.

Cross-spec coordination: [event-model.md §8.7.14] `daemon_orphan_sweep_completed` payload schema requires the `coordinator_sessions_skipped` field addition.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent
Refs: hk-hc3qq (orphan sweep kills flywheel pane on daemon restart).

#### PL-007 — Orphan sweep is deterministic and complete before classification

The orphan sweep MUST be deterministic given the filesystem + process state AND the project-scoped provenance marker of PL-006a. After the sweep completes, no harmonik-owned process bearing this project's provenance marker from a prior daemon instance is alive and no harmonik-owned worktree is locked by a prior-instance lease. The git walk (PL-005 step 5) and Beads query (step 6) operate against a quiescent project-scoped filesystem. The sweep MUST NOT match on binary path alone and MUST NOT kill a process lacking a valid project-scoped marker.

Tags: mechanism

#### PL-008 — Startup failure-mode catalog obligation

This spec DEPENDS on the normative startup failure-mode catalog produced by the [operator-nfr.md §4.1 ON-003] spec-draft obligation. The Cat 0 pre-check (§PL-005 step 4) consumes this catalog; the operator surface commands consume it for `harmonik status` reporting.

> INFORMATIVE: Expected catalog entries include git bad state, Beads SQLite locked, schema-version mismatch, stale-pidfile race, filesystem-unwritable, disk-full during checkpoint commit, and `ntm` unavailable (per PL-021a). Authoritative list is owned by operator-nfr.

Tags: mechanism

#### PL-008a — Exit-code consumption from ON §8

The daemon MUST emit exit codes from the authoritative taxonomy in [operator-nfr.md §8]. The codes consumed by this spec are: 5 (`pidfile-locked`, per PL-002), 6 (`socket-bind-failed`, per PL-003), 7 (`git-bad-state`), 8 (`beads-unavailable`), 9 (`filesystem-unwritable`, including pidfile/socket fs failure), 10 (`disk-full`), 14 (`upgrade-hash-mismatch`, emitted on startup-marker mismatch per PL-005 step 8a / [operator-nfr.md §4.6 ON-020a]; also emitted on the `queue.json` schema-incompatible sibling-case of PL-005 step 8a pending operator-nfr's specific allocation), 17 (`multi-daemon-target-missing`, consumed by `hk queue *` CLI surfaces when the project's daemon socket is not bound — the remediation prose extension for the single-project `ECONNREFUSED` case is owned by operator-nfr per [operator-nfr.md §4.1 ON-004]), 19 (`runtime-panic`, emitted by the panic barrier per PL-018a), 22 (`ntm-unavailable`, emitted by PL-021a when `ntm` is missing, version-incompatible, or absent), and 23 (`orchestrator-agent-unavailable`, emitted by PL-028 step 4 when `harmonik runner --orchestrator-agent` cannot locate Claude Code).

Codes 22 and 23 were declared PL-INTERIM in v0.4.0 pending ON's next revision; they were absorbed into [operator-nfr.md §8] in ON v0.4.0 and are now first-class entries in that taxonomy. PL consumes them by reference; no PL-side interim marker remains.

On emission, the daemon MUST also emit `daemon_startup_failed` (per [event-model.md §8.7.4]) with `{failed_at, exit_code, failure_mode}` BEFORE process exit where the event bus has been initialized (PL-005 step 0 has completed). For failures that occur BEFORE step 0, the daemon MUST emit only the exit code to stderr; the event surface is unreachable.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

### 4.3 Ready-state transition

#### PL-009 — Ready criteria

The daemon MUST transition status to `ready` only when ALL of the following conditions hold:

- The orphan sweep (§PL-006) has completed.
- The Cat 0 pre-check ([reconciliation/spec.md §4.3 RC-012]) has passed (no infrastructure prerequisite is failing).
- The git-log walk and Beads query (§PL-005 steps 5–6) have completed.
- The in-memory model has been built (§PL-005 step 7).
- Reconciliation dispatch (§PL-005 step 8) has completed for every in-flight run: either (i) the synchronous action-mapping of [reconciliation/spec.md §4.2 RC-008] has succeeded and emitted `reconciliation_category_assigned` per [reconciliation/spec.md §4.3 RC-013], or (ii) the run has been routed to an investigator workflow per PL-009a. Dispatched investigator workflows MAY remain in-flight and MUST NOT block the `ready` transition.
- Every in-flight run has received a category assignment emission per [reconciliation/spec.md §4.3 RC-013].

On transition to `ready`, the daemon MUST emit `daemon_ready` (per [event-model.md §8.7.2]) with `{ready_at, ready_at_ns_since_boot, investigator_run_ids[]}`. `ready_at` is the wall-clock time at emission (RFC 3339 with ms). `ready_at_ns_since_boot` is the monotonic-clock companion field (nanoseconds since system boot, sourced from `CLOCK_MONOTONIC` on Linux / `mach_absolute_time()` translated to ns on darwin) emitted alongside the wall-clock timestamp so that RTO measurement per [operator-nfr.md §4.8 ON-033] is robust to wall-clock skew. Restart RTO ([operator-nfr.md §4.8]) is measured from SIGTERM (with its own monotonic companion) to `daemon_ready` emission; the monotonic-companion pair is the authoritative measurement source. On boot-transition (post-reboot) the monotonic-clock comparison is undefined and the RTO MUST be marked `rto_undefined` per ON-033.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### PL-009a — Auto-resolver failure during startup dispatch

If a synchronous action-mapping auto-resolver (any of Cat 1, Cat 3a, Cat 3b, Cat 3c, Cat 4 per [reconciliation/spec.md §8.12]) fails or raises during §PL-005 step 8, the daemon MUST:

(a) emit `reconciliation_category_assigned` with the original category per [reconciliation/spec.md §4.3 RC-013];
(b) re-classify the run into Cat 3 (store disagreement, generic) per [reconciliation/spec.md §8.4];
(c) dispatch an investigator workflow per [reconciliation/spec.md §4.2 RC-008];
(d) proceed toward `ready` with the investigator workflow in-flight, contributing the run_id to the `investigator_run_ids[]` of `daemon_ready`.

The daemon MUST NOT block `ready` due to auto-resolver failure, and MUST NOT leave an in-flight run unclassified at `ready` emission. An auto-resolver failure is itself a recoverable event; permanently-stuck investigator workflows escalate via reconciliation's normal Cat 6 path per [reconciliation/spec.md §8.11a] and are NOT the daemon's concern at startup.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### PL-009b — Ready-protocol surface for external callers

External processes (operator CLI, process supervisors, `harmonik runner` per PL-028) MUST detect daemon readiness via one of the following mechanisms, in order of preference:

(a) **Socket probe.** Connect to `.harmonik/daemon.sock`, send a JSON-RPC `status` request, receive a response with `status ∈ {ready, degraded, reconciling, draining}`. `reconciling` means alive-but-not-yet-ready; the caller MUST retry with exponential backoff (initial 100 ms, max 2 s, capped at `T_ready_wait = 60 s` default tracked under OQ-PL-002). `ready` means ready.

(b) **systemd-style notify (Linux).** When launched under systemd `Type=notify` (detectable via `$NOTIFY_SOCKET` environment variable), the daemon MUST call `sd_notify("READY=1")` at the same instant as the `daemon_ready` event emission of PL-009.

(c) **Ready-file (portable fallback).** The daemon MAY write `.harmonik/daemon.ready` at the moment of PL-009 emission and MUST remove it on any transition out of `ready` (drain start, exit, degraded). This is informative; it exists for solo-dev fswatch-based setups.

External callers MUST NOT assume the daemon is ready simply because the pidfile or socket file exists. `ECONNREFUSED` from the socket means the daemon has not yet called `listen()`; connection success with `reconciling` status means the socket is bound but startup is incomplete.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### PL-010 — Degraded state on Cat 0 infrastructure failure

When the Cat 0 pre-check (§PL-005 step 4) fails, the daemon MUST transition to `degraded` status and remain there until all prerequisites clear. In `degraded`, the daemon MUST NOT classify in-flight runs, MUST NOT dispatch runs, and MUST NOT transition to `ready`. The daemon MUST emit `infrastructure_unavailable` (per [event-model.md §8.7.15]) naming the specific prerequisite that failed, and SHOULD also emit `daemon_degraded` (per [event-model.md §8.7.5]) for the health-surface consumers of [operator-nfr.md §4.9]. The daemon MUST periodically retry the pre-check at a configurable cadence (default 10s per OQ-PL-002). `harmonik status` MUST report the `degraded` state and the failing prerequisite per [operator-nfr.md §4.1 ON-002].

The `degraded` state declared by this spec is the PRE-`ready` Cat 0 side-state only; it has one entry path (PL-005 step 4 failure) and one exit path (prerequisites clear). Post-`ready` degradation (RTO breach, subsystem health aggregation failures, silent-hang fan-out) is emitted via `daemon_degraded` with `reason ∈ {rto_breach, reconstruction_notify, other}` per [event-model.md §8.7.5] but does NOT transition the §6.1 status enum; it is a health-surface concern owned by [operator-nfr.md §4.9]. Scope-widening to a reentrant status is deferred as OQ-PL-009.

On SIGTERM while `degraded`, the daemon MUST proceed to graceful shutdown per PL-011; since no in-flight runs are classified, drain is trivial and steps 2–3 complete immediately.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

### 4.4 Shutdown

#### PL-011 — Graceful shutdown drains in-flight runs

On `harmonik stop --graceful`, SIGTERM, or SIGINT, the daemon MUST execute a drain sequence:

1. Transition status to `draining`.
2. Stop pulling new beads from the queue.
3. **Classify in-flight runs and freeze dispatch.** For every in-flight run (`in_flight(run)` per [operator-nfr.md §3]) at drain entry, the daemon MUST classify the run into one of three sub-states:
   - **(i) mid-agent-work** — the agent subprocess is actively processing; the daemon waits up to T_drain (per [operator-nfr.md §4.7 ON-029]) for its watcher (per [handler-contract.md §4.3 HC-011]) to observe `agent_completed` or `agent_failed`. On observation, the run reaches a durable checkpoint per [execution-model.md §4.4 EM-017].
   - **(ii) just-checkpointed** — a checkpoint has just landed and no follow-up node has been dispatched. The daemon withholds dispatch and treats the run as quiescent.
   - **(iii) gate-pending** — the run is in a `gate-pending` sub-state of `running` per [execution-model.md §7.1]. The daemon treats this as quiescent and withholds dispatch on subsequent gate resolution.

   The step-3-complete signal to step 4 is the watcher-completion aggregation across (i)-class runs combined with the immediate quiescence of (ii)+(iii)-class runs.
4. Wait for agent-subprocess termination (bounded by the operator-configurable drain timeout per [operator-nfr.md §4.7 ON-029]). On drain-timeout expiry, the daemon MUST send SIGKILL to surviving agent subprocesses and proceed to step 5.
5. Emit `daemon_shutdown` per PL-011a.
6. Flush the event bus (fsync per [event-model.md §4.4]).
7. Release worktree leases (per [workspace-model.md §4.3 WM-013b]).
8. Release the pidfile lock AND remove the pidfile on clean shutdown (PL-024's stale-pidfile detection applies only on crash, where the pidfile is left in place). Remove the socket file.
9. Exit with code `0` on clean drain; exit with code `1` if the drain timeout escalated to forced termination at step 4.

The daemon status transitions through `draining` during this sequence (per the operator-control state machine in [operator-nfr.md §4.3]). Subsystem-level shutdown ordering is owned by [operator-nfr.md §4.7 ON-027]; this requirement names the daemon-level sequence.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### PL-011a — Shutdown event emission

The daemon MUST emit `daemon_shutdown` (per [event-model.md §8.7.3]) before the event bus flush (§PL-011 step 6). Payload: `{shutdown_at, shutdown_at_ns_since_boot, mode}`. The `mode` is `graceful` for PL-011 and `immediate` for PL-012 (for the interceptable `stop --immediate` path; SIGKILL cannot emit). `shutdown_at` is the wall-clock time at emission (RFC 3339 with ms). `shutdown_at_ns_since_boot` is the monotonic-clock companion field (nanoseconds since system boot, sourced from `CLOCK_MONOTONIC` on Linux / `mach_absolute_time()` translated to ns on darwin) emitted alongside the wall-clock timestamp for RTO measurement per [operator-nfr.md §4.8 ON-033]. SIGKILL terminations have no `daemon_shutdown` emission and are marked `rto_undefined` per ON-033.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### PL-012 — Immediate shutdown aborts in-flight runs

On `harmonik stop --immediate` (interceptable) or SIGKILL (where SIGKILL cannot be intercepted, but its effect is what this requirement models), the daemon MUST skip the drain steps (§PL-011 steps 3–4). In-flight agent subprocesses are killed; in-flight state is recoverable via the next startup's orphan sweep (§PL-006) + reconciliation per [reconciliation/spec.md §4.2]. On interceptable `stop --immediate`, the daemon MUST attempt steps 5–9 (emit `daemon_shutdown{mode=immediate}`, flush, release, exit); if any step fails, the daemon MUST proceed to exit with a non-zero code. On SIGKILL, steps 5–9 are skipped by force; recovery follows PL-024.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### PL-013 — Retired in extqueue v0.1

**PL-013 — Retired in extqueue v0.1.** The prior requirement covered the daemon's idle-wait behavior under the Beads-ready-poll dispatch model: when all beads were closed or deferred and nothing was in-flight, the daemon was obligated to sleep and wake on `harmonik enqueue` or on a periodic re-query of the Beads store. Under [queue-model.md], the daemon's dispatch input is the in-memory queue loaded at PL-005 step 8a from `.harmonik/queue.json` or arriving via `queue-submit`; idle (queue absent, queue-completed, or queue-paused) is simply "no active group is advancing." The daemon MUST NOT exit on queue absence or queue completion; daemon exit occurs only on explicit `harmonik stop`, on an operator upgrade transition (`running` → `upgrading` per [operator-nfr.md §4.3]), or on crash (§PL-024). The previous re-query-cadence knob is removed from the config inventory per [operator-nfr.md §4.1 ON-004].

The "MUST NOT exit on idle" obligation survives this retirement and is restated here as the load-bearing remainder of the original requirement. The Beads-ready-poll mechanism, the `harmonik enqueue` wake-up channel, and the cadence configuration knob are all gone.

Tags: mechanism

### 4.5 Agent-subprocess management

#### PL-014 — Agent subprocesses are children of the daemon

The daemon MUST spawn agent processes as children of the daemon process (via the ntm adapter or the equivalent platform abstraction; see §PL-020 and §4.7). Every spawn MUST carry the provenance marker of PL-006a. Child parentage is structural: it allows the OS to re-parent orphans to init on daemon crash, which the next startup's orphan sweep (§PL-006) cleans up. Subprocess supervision (watcher goroutine, cancellation timing, `cmd.Wait()` reap, crash detection) is owned by [handler-contract.md §4.3 HC-011] and [handler-contract.md §4.6 HC-024]; the daemon-side PL-014 obligation is scoped to the OS-level parentage relationship and spawn-site provenance. Launch mechanics follow [handler-contract.md §4.1 HC-001].

Every spawn MUST have exactly one Go goroutine that owns the `*exec.Cmd` and that goroutine MUST call `cmd.Wait()` exactly once to reap the child's exit status. The watcher goroutine per [handler-contract.md §4.3 HC-011] is that exclusive caller. Failure to call `cmd.Wait()` produces a zombie that persists until daemon exit (re-parented to init at exit), and MUST NOT occur on any code path; it is a conformance violation under PL-INV-005 regardless of whether `kill(pid, 0)` reports the zombie as alive.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### PL-014a — Per-daemon concurrency ceiling

The daemon MUST enforce a configurable ceiling on simultaneously-running agent subprocesses. The default ceiling, when no operator override is set, is `min(RLIMIT_NOFILE_soft / FDS_PER_HANDLER, FALLBACK_CAP)` where `FDS_PER_HANDLER = 8` (conservative, accounting for stdin/stdout/stderr/socket-conn plus transient spikes) and `FALLBACK_CAP = 1024`. The daemon MUST `getrlimit(RLIMIT_NOFILE)` at PL-005 step 0; if the soft limit is below `MIN_NOFILE = 4096`, the daemon MUST attempt `setrlimit` to raise the soft limit to `min(4096, hard)` and MUST log a warning on failure. An operator-configured ceiling per [operator-nfr.md §4.3] takes precedence; the derived value is the safety default. Exceeding the ceiling MUST emit `dispatch_deferred{reason="per_daemon_ceiling_exhausted"}` (NOT the cross-daemon `machine_ceiling_exhausted` reason of ON-041, which is the cross-daemon counterpart).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### PL-015 — Agent ↔ daemon communication routes through the socket

Agent subprocesses MUST communicate with the daemon exclusively through `.harmonik/daemon.sock` (§PL-003). The daemon-facing agent-command surface (including `claim-next`, `emit-outcome`, and Beads-CLI-skill-routed invocations) MUST route over this socket using the wire format of §PL-003a. The concrete method definitions for agent-side commands are owned by [handler-contract.md §4.2] for the wire protocol and [beads-integration.md §4.9 BI-027] for the Beads-CLI skill. PL owns "these commands route over the socket"; the command-set normative inventory is tracked as OQ-PL-005.

Tags: mechanism

#### PL-016 — Agent-subprocess failure is observed by the daemon

Agent-subprocess failure (crash, hang, policy violation) MUST be observed by the daemon's watcher goroutine per [handler-contract.md §4.3 HC-011] and MUST produce typed events (`agent_failed`, etc., per [event-model.md §8.3]). The watcher is the exclusive owner of the `*exec.Cmd` reference for its session; it is the exclusive caller of `cmd.Wait()` for reap, per [handler-contract.md §4.3]. Routing of the resulting event to retry / re-plan / terminal paths is owned by [execution-model.md §4.10 EM-046b] (RETRY outcome re-dispatch) and [reconciliation/spec.md §4.2] (reconciliation-routed paths).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### PL-017 — Silent-hang detection obligation

This spec NAMES the silent-hang detection obligation owned by [handler-contract.md §4.6]. A silent hang is an agent subprocess that remains alive but produces no output, no heartbeat, and no lifecycle signal for longer than a bounded interval. The handler-contract spec owns the detection rule, the wall-clock ceiling, and the cleanup path; this spec requires that the daemon's watcher goroutine (§PL-016) implement the handler-contract detection rule and route silent-hang outcomes into reconciliation per [reconciliation/spec.md §4.2].

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### PL-017a — Hook-bridge relay subprocesses are grandchildren of the daemon

Some handler subsystems (notably the claude-code bridge per [claude-hook-bridge.md]) cause additional short-lived subprocesses (e.g., `harmonik hook-relay`) to be spawned by an agent subprocess. These are GRANDCHILDREN of the daemon, not direct children. PL-014's "daemon child" rule applies to the handler subprocess only.

Specifically:

(a) Relay-grandchild subprocesses are NOT registered with the per-daemon concurrency ceiling (§PL-014a); their fd usage is bounded by the agent subprocess's hook-firing rate, not by harmonik's dispatch loop.

(b) The orphan-sweep §PL-006 MUST NOT target relay-grandchild subprocesses; they exit on their own when the agent subprocess completes its hook invocation, and any survivors (e.g., relay processes whose parent agent died mid-invocation) are reaped via OS init-reparenting at daemon death or by the agent subprocess's own process-tree cleanup at session-end.

(c) The relay's daemon-socket connection regime is governed by [handler-contract.md §4.10 HC-045b] (one-shot NDJSON connections, each independent), NOT by HC-007's long-lived-stream model.

Tags: mechanism

### 4.6 Daemon vs orchestrator-agent distinction

#### PL-018 — Daemon is a deterministic Go binary with no LLM logic

The daemon MUST be a deterministic Go binary. The daemon MUST NOT call any LLM, MUST NOT import any LLM SDK, and MUST NOT embed any cognition-bearing component per [architecture.md §4.2]. All cognition in harmonik lives in agent subprocesses launched via handlers (per [handler-contract.md §4.1 HC-001]) or in orchestrator-agent sessions interacting via the CLI (§PL-019). A proposal to embed cognition in the daemon (e.g., "let the daemon decide which bead to claim next using an LLM") violates this requirement.

PL-018 applies to LLM-bearing logic; reading a config enum value is exempt. Mode dispatch (workflow-graph walking, agent role selection, iteration-cap enforcement) is owned by [handler-contract.md §4.1] and [execution-model.md §4.3]; the daemon's lifecycle code stores the enum and exposes it to dispatch, nothing more.

Tags: mechanism

#### PL-018a — Panic recovery barrier in the daemon main goroutine

The daemon MUST install a top-level `recover()` barrier in its main goroutine. An unrecovered panic MUST terminate the daemon with ON §8 code 19 (runtime-panic) per [operator-nfr.md §8] and PL-008a, emit `daemon_startup_failed` (if the event bus is initialized) or `daemon_shutdown{mode=immediate}` (if `ready` has been reached) on a best-effort basis, and leave the pidfile stale; recovery follows PL-024. Panics inside handler-contract-watcher goroutines are handled by [handler-contract.md §4.3 HC-011a] and do NOT terminate the daemon; panics inside other daemon goroutines (dispatcher, reconciler, subsystem loops) MUST be caught by per-goroutine `recover()` and escalate to the top-level barrier only on repeated failure (the exact escalation threshold is implementation-defined at MVH).

A panic inside the top-level `recover()` handler (a "double panic") MAY bypass the exit-code-19 path and terminate the daemon with a Go runtime panic message and a non-19 exit code. This is an accepted limitation of the `recover()` primitive. The next daemon startup recovers via PL-024's stale-pidfile detection; the absence of a terminal lifecycle event (`daemon_shutdown`, `daemon_startup_failed`) is consumable via the pairing-tolerance rule (PL-025a). Operators observing double-panic exit codes SHOULD capture the Go runtime's stderr output for diagnostic purposes; the event surface cannot capture it.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

#### PL-019 — Orchestrator/cognition process is a separately-managed long-running process

(a) **Process separation.** The orchestrator/cognition process (hereafter "supervisor process") MUST run as a separate OS process from the daemon. It MUST NOT run as a thread, goroutine, or sub-module within the daemon binary; it MUST have its own PID. The daemon MUST remain LLM-free per PL-018; the supervisor process is the sole cognition-bearing layer and MAY call an LLM or host an agent session. The supervisor process MUST interact with the daemon exclusively through the daemon's CLI and socket surface (§PL-003a, §PL-028): `hk queue submit`, `harmonik status`, `harmonik digest`, `kerf next`, priority triage, and backlog grooming are its normative interaction points.

(b) **Spawn surface.** The supervisor process MUST be started via `harmonik supervise start` (§PL-028, PL-028d). The daemon MUST NOT start, stop, or monitor the supervisor process; their lifecycles are explicitly separated. `harmonik supervise start` MUST refuse with exit code 17 (`multi-daemon-target-missing` per ON §8 / PL-008a) if the daemon's socket at `.harmonik/daemon.sock` is not reachable (ECONNREFUSED). `harmonik supervise start` MUST NOT attempt to start the daemon on behalf of the operator.

(c) **Singleton lock.** Exactly one supervisor process per project MUST be permitted at any instant. `harmonik supervise start` MUST acquire a fd-lifetime advisory lock on `.harmonik/cognition/supervisor.lock` via `flock(LOCK_EX|LOCK_NB)` (same primitive as PL-002a). On `EWOULDBLOCK` the command MUST exit with ON §8 code 25 (`supervisor-already-running`, PL-INTERIM pending ON absorption) and MUST print the path of the running supervisor's pidfile to stderr. The lock MUST be held for the lifetime of the supervisor process or its watch-shim wrapper; the kernel releases it on process exit whether clean or crash.

(d) **Pidfile.** After acquiring the lock, the supervisor process (or the watch-shim on its behalf) MUST write the inner process's PID to `.harmonik/cognition/supervisor.pid` following PL-002b discipline (single ASCII decimal PID line terminated by `\n`). `harmonik supervise status` MUST read this file to probe liveness via `kill(pid, 0)`.

(e) **Config file.** `harmonik supervise start` MUST atomically write a configuration snapshot to `.harmonik/cognition/config.json` (temp+rename+fsync per WM-026) before launching. The file MUST carry a `schema_version` integer (N-1-readable per ON-018); `daemon_instance_id` from the running daemon's pidfile (line 3 per PL-002b) MUST be recorded so the supervisor process can validate it is talking to the same daemon instance. The supervisor MUST re-read config at startup; it MUST NOT hot-reload mid-run (parameter changes require `harmonik supervise restart`).

(f) **Tmux pane and crash posture.** `harmonik supervise start` MUST create a tmux session named `harmonik-<project_hash>-flywheel` per PL-006a, using `tmux new-session -d -s <name>` when the session does not already exist. The pane MUST be created with `set-option remain-on-exit on` so a supervisor crash leaves the pane visible for operator inspection. This session name carries the `harmonik-<project_hash>-` prefix and is therefore within the orphan-sweep namespace; the exclusion clause of PL-006d MUST be honored by the daemon's orphan sweep to prevent inadvertent kills on daemon restart. When `--watch-restart` is supplied, the command MUST interpose a lightweight wrapper shim between the lock-holder and the supervisor process.

(g) **File surface.** The supervisor process's per-project files reside under `.harmonik/cognition/`: `supervisor.lock` (fd-lifetime advisory lock), `supervisor.pid` (inner process PID), `config.json` (atomic config snapshot), `heartbeat.json` (periodic liveness report), `watermark.json` (consistency watermark per [cognition-loop.md §CL-consistency]), `notes.jsonl` (append-only durable notes log per [cognition-loop.md §CL-digest]). The daemon MUST NOT read or write files under `.harmonik/cognition/` except (i) reading `.harmonik/cognition/supervisor.sentinel` and `.harmonik/cognition/supervisor.pid` during the orphan sweep per PL-006d.

(h) **Relationship to PL-018.** This requirement is an extension of the daemon-vs-orchestrator-agent distinction established by PL-018 and PL-019's original framing. The daemon remains a deterministic Go binary with no LLM calls. The supervisor process is the sole cognition-bearing layer and is explicitly out-of-process.

Tags: cognition

> NOTE: The conformance profile for PL-019 is post-MVH: an implementation MAY conform to Core MVH without ever running a supervisor process. §10.1 retains the "Post-MVH" carve-out. The normative text above is the stable contract for any post-MVH implementation; its presence here (rather than only in cognition-loop.md) is because the daemon-side obligations — refusing to start the supervisor, honoring the orphan-sweep exclusion, and keeping the `.harmonik/cognition/` subtree unowned — are daemon-lifecycle requirements.
>
> INFORMATIVE: The cognition tag on PL-019 names the delegation path: role = supervisor/cognition process (orchestrator-agent in prior framing); model-class = Claude Code (Sonnet or Opus per project configuration); spec: [cognition-loop.md]. The PL-018 LLM-free invariant is unaffected; the daemon layer is unchanged.

#### PL-020 — Composition root is `internal/daemon`

The daemon's code organization MUST treat the `internal/daemon` Go package as the composition root. Only `internal/daemon` is allowed to import across subsystem boundaries (per [architecture.md §4.4] subsystem-envelope rule and the `go-arch-lint` enforcement declared in [core-scope.md §6]). Subsystems MUST NOT import each other directly except through the interfaces each subsystem exposes.

Tags: mechanism

#### PL-020a — Cross-subsystem registries reside in the composition root

All cross-subsystem registries declared by foundation specs — including the event bus ([event-model.md §4.3]), the control-point registry ([control-points.md §4.1]), the handler registry and skill registry ([handler-contract.md §4.1, §4.11]), and the policy registry — MUST be instantiated inside the composition root (`internal/daemon`) on startup per §PL-005 step 0. No out-of-daemon registry is permitted for MVH per [architecture.md AR-INV-007]. Post-MVH process-geometry changes that split registries across processes are tracked by architecture's post-MVH surface per [architecture.md §4.5 AR-019] and do NOT require a foundation amendment to PL per [architecture.md §4.5].

Tags: mechanism

### 4.7 ntm adapter scope

#### PL-021 — ntm adapter consumes process/tmux surface only

The ntm adapter layer MUST consume only the following ntm capabilities: (a) agent process spawning in a tmux pane, (b) agent-profile knowledge (ready-state detection per agent type, rate-limit signals, clean exit sequences), (c) lifecycle events (process start, ready, rate-limited, stopped), and (d) account rotation (for agent types that support it).

Tags: mechanism

#### PL-021a — ntm version pin and absence-detection

The daemon MUST version-pin `ntm` per the external-inputs protocol (parallel pattern to [operator-nfr.md §4.4 ON-017]); supported `ntm` versions MUST be declared in the release manifest. An `ntm` version outside the supported set MUST be detected during §PL-005 step 4 Cat 0 pre-check and MUST produce ON §8 code 22 (`ntm-unavailable`) per PL-008a plus `infrastructure_unavailable{failed_prerequisite=ntm_unavailable}` per [event-model.md §8.7.15]. `ntm` not on PATH, tmux missing, or failing the version probe MUST classify identically as Cat 0 per [reconciliation/spec.md §8.1]. The daemon MUST NOT attempt to spawn handler subprocesses without a working ntm adapter; handler spawns MUST fail-fast with the ntm-unavailable exit code rather than silently degrading to non-tmux mode (the solo-dev ergonomics carve-out of locked decision #4 requires tmux inspectability).

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### PL-021b — Direct-tmux substrate (MVH alternative to ntm adapter)

For the MVH the daemon MUST consume a direct-tmux substrate in place of the ntm adapter described in PL-021. The direct-tmux substrate is implemented by package `internal/lifecycle/tmux` and exposes the following obligations:

1. **Pane creation.** On every handler-subprocess spawn whose `agent_type` requires interactive-pty hosting, the daemon MUST create the subprocess via `tmux new-window -d -t <session>: -n <window-name> -c <cwd> -e KEY=VALUE [...] -- <binary> <argv...>`. The daemon MUST NOT spawn such subprocesses via `exec.CommandContext` directly. Subprocesses whose adapter does not request substrate hosting (e.g. unit-test twin invocations outside the daemon) remain on the direct-exec path; this carve-out preserves twin parity per [claude-hook-bridge.md §4.8 CHB-022] because adapter-registry dispatch — not binary-name branching — selects the path.
2. **Tmux availability check.** The daemon MUST probe tmux at PL-005 step 4 (Cat 0 pre-check) by invoking `tmux -V` and asserting major version ≥ 3.0. On failure the daemon MUST exit with ON §8 code 22 (`tmux-unavailable`, retitled from the v0.4.x ntm-unavailable). This obligation supersedes PL-021a's `ntm`-targeted absence-detection for the duration of the MVH; PL-021a remains in force as the long-term contract once an ntm adapter ships.
3. **Session resolution.** Before dispatching the first handler subprocess the daemon MUST resolve the tmux session it will host windows in:
   - If the environment variable `TMUX` is set at daemon startup, the daemon MUST use the session named in `$TMUX` (the operator's existing session). Windows the daemon creates in this session MUST carry a sentinel prefix `hk-<hash6>-` where `<hash6>` is the first 6 hex chars of the project hash. The daemon does NOT create or kill the operator's session.
   - If `TMUX` is unset, the daemon MUST NOT proceed to spawn handler subprocesses. The unset-`TMUX` condition is detected eagerly at startup per PL-028b (extends the Cat 0 pre-check surface); a daemon that reaches the dispatch loop without `TMUX` is a defect.
4. **Window naming.** Window names MUST be a deterministic function of `(bead_id, phase, iteration_count, project_hash, owns_session)` per [workspace-model.md §4.1 WM-002a]. The function MUST be replay-stable: replaying a recorded run reproduces the exact window name. In the `owns_session=true` mode the name is `<bead_id>` (workflow:single) or `<bead_id>/i<n>` / `<bead_id>/r<n>` (workflow:review-loop implementer / reviewer). In the `owns_session=false` ($TMUX-reuse) mode the same name is prefixed with `hk-<hash6>-`.
5. **No pane-output consumption.** The daemon MUST NOT read pane stdout/stderr through `tmux pipe-pane` or any other channel. All bridge-protocol messages ([claude-hook-bridge.md §4.7 CHB-018] pre-exec messages, [claude-hook-bridge.md §4.7 CHB-019] heartbeats, [claude-hook-bridge.md §4.7 CHB-020] terminal events, [claude-hook-bridge.md §4.10 CHB-025] outcome dedup) flow through the daemon's Unix socket per PL-003a and the `harmonik hook-relay` subcommand per [claude-hook-bridge.md §4.4 CHB-010]. The pty exists exclusively for operator ergonomics (interactive attach).
6. **Substrate seam.** The handler-package `LaunchSpec` carries an optional substrate handle. When non-nil, `Handler.Launch` MUST route subprocess creation through the substrate. The substrate handle is constructed at the daemon composition root and threaded via the adapter registry; daemon code MUST NOT branch on `LaunchSpec.Binary` to decide substrate engagement ([claude-hook-bridge.md §4.8 CHB-022] twin-blindness).
7. **Wait/kill discipline.** The substrate `Wait` operation MUST satisfy the PL-014 single-`cmd.Wait()` invariant in spirit — for substrate-hosted sessions the daemon has no `*exec.Cmd` to wait on; the substrate observes pane death by polling `tmux list-panes` at a 100ms cadence (matching PL-006 sweep cadence) and reports exit semantics via the Outcome type. The substrate `Kill` operation MUST issue `tmux kill-window`; SIGKILL escalation is delegated to tmux itself.
8. **Window-name observability.** The daemon MUST emit the resolved `tmux_window_name` as a field of the `agent_started` progress-stream message so that the window-naming determinism asserted in item 4 above is externally observable. This makes the tmux window name discoverable by the operator and by conformance tests without requiring a separate `tmux list-windows` call. The `agent_started` payload MUST include `tmux_window_name: <string>` set to the exact name passed to `tmux new-window -n`. This obligation applies only when the direct-tmux substrate is engaged (i.e., `LaunchSpec.substrate != nil`); non-substrate launches omit the field.

   Cross-spec coordination request: [event-model.md §6.3] `agent_started` payload schema requires a new optional `tmux_window_name string` field for this requirement (applicable when substrate = direct-tmux). Refs: hk-gql20.25.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### PL-021c — Pane orphan recovery within PL-006

The orphan sweep of PL-006 MUST be extended to cover orphan tmux **windows** in addition to orphan tmux **sessions**. The extension is required because the PL-021b $TMUX-reuse mode places harmonik-created windows inside an operator-owned session whose name does NOT match the `harmonik-<project_hash>-` prefix that PL-006 enumerates.

The extended sweep MUST:

1. Enumerate all live tmux sessions via `tmux list-sessions -F '#{session_name}'`.
2. For each session, enumerate its windows via `tmux list-windows -t <session> -F '#{window_name}'`.
3. For every window whose name begins with `hk-<hash6>-` where `<hash6>` is the first 6 hex chars of *this* daemon's project hash, the daemon MUST issue `tmux kill-window -t <session>:<window>`.
4. After issuing kill-window commands, the daemon MUST poll at 100 ms cadence up to a 2-second ceiling for the windows to disappear; after the ceiling, the daemon MUST proceed regardless.
5. The `daemon_orphan_sweep_completed` event payload MUST gain a new field `tmux_windows_killed: <integer ≥ 0>`.
6. **Post-ceiling survivor handling.** If the 2-second poll ceiling expires and one or more subprocesses backing a killed window are still alive (detectable via `kill(pid, 0)` against the pane's `#{pane_pid}` field), the daemon MUST: (a) log a structured warning at level WARN with message key `tmux_kill_window_survivor` naming each surviving PID; and (b) include a new field `tmux_kill_window_survivors []int` in the `daemon_orphan_sweep_completed` event payload listing the surviving PIDs. The daemon MUST NOT send SIGKILL to survivors at MVH: the operator-owned session context is not harmonik's to mass-kill, and PL-006's session-level sweep handles cases where the operator's session itself is harmonik-owned. Surviving window-subprocess PIDs are adopted by the OS init process and are not tracked further by this daemon instance.

   Cross-spec coordination request: [event-model.md §8.7] `daemon_orphan_sweep_completed` payload schema requires the `tmux_kill_window_survivors []int` field addition (companion to the `tmux_windows_killed` field from item 5).

The session-level sweep of PL-006 is NOT modified.

Cross-spec coordination: [event-model.md §8.7] `daemon_orphan_sweep_completed` payload schema requires the `tmux_windows_killed` field addition.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### PL-021d — Daemon→pane write mechanism (tmux load-buffer + paste-buffer)

PL-021b §5 forbids the daemon from *reading* pane output via `tmux pipe-pane` or any equivalent channel. This clause addresses the symmetric case — the daemon *writing* content into a pane — which is unspecified in PL-021b and needed for initial-task delivery and inter-phase message injection (see [docs/claude-session-comms-audit-2026-05-13.md §6 B2]).

**Permitted write mechanism.** When the daemon must deliver text to a pane (e.g., initial task instruction, phase-transition directive), it MUST use the `tmux load-buffer` + `tmux paste-buffer` sequence rather than `tmux send-keys` with a bare string argument:

1. Write the payload to a temporary file under `.harmonik/` using the temp+rename+fsync(parent_dir) atomic discipline of [workspace-model.md §4.7 WM-026].
2. Load the temp file into a named tmux buffer: `tmux load-buffer -b <buffer-name> <temp-file-path>`.
3. Paste the buffer into the target pane: `tmux paste-buffer -b <buffer-name> -t <pane-target>`.
4. Delete the buffer immediately after paste: `tmux delete-buffer -b <buffer-name>`.
5. Remove the temporary file.

The `send-keys -l` variant (literal-string send) is also permitted as a fallback when payload length is below 512 bytes and no newlines are present in the payload; for all other payloads the load-buffer + paste-buffer sequence MUST be used. The `send-keys` bare-string form (without `-l`) is FORBIDDEN for daemon-injected payloads because it interprets shell metacharacters.

**Buffer-name discipline.** The buffer name MUST be deterministic per agent session and write purpose:

- Format: `harmonik-<session-id>-<purpose>`
- `<session-id>` is the bead's session UUID (the same ID used in the `agent_started` event and the tmux window name).
- `<purpose>` is a short lowercase slug identifying the write's role: `task` for the initial task delivery, `phase-msg` for phase-transition directives, `feedback` for reviewer-feedback injection.
- Example: `harmonik-01hwxyz-abc123-task`

The buffer name MUST be globally unique within tmux (tmux buffer names are shared across sessions in a server). The session-id component ensures this because session IDs are unique per harmonik run. The daemon MUST NOT reuse a buffer name for a second paste without first deleting it.

**Cleanup obligation.** The daemon MUST delete the named buffer via `tmux delete-buffer -b <buffer-name>` immediately after `paste-buffer` returns, regardless of whether `paste-buffer` succeeded or failed. If `delete-buffer` fails (e.g., the buffer was already consumed), the failure MUST be logged at DEBUG level and ignored — it is not a fatal error. Buffer accumulation across sessions is not a correctness hazard (tmux garbage-collects buffers on server exit), but leaving buffers named with the `harmonik-` prefix is undesirable for operator hygiene.

**Why this is not equivalent to pipe-pane stdout reads.** `tmux pipe-pane` attaches a kernel-level pipe to the pane's pty output — it intercepts and re-routes the pty's read path, creating a side-channel that bypasses the operator's attached view. In contrast, `tmux paste-buffer` writes content into the pane's *input* via tmux's internal paste mechanism, which routes through the same TTY input path a human would use (equivalent to the operator pasting text from a tmux copy-paste buffer). No kernel-level pipe redirection occurs. The pty sees exactly what it would see from operator interaction; the operator's attached view shows the injected text entering the prompt, preserving full inspectability. The bridge-protocol messages over the Unix socket (PL-021b §5) remain the sole output channel for harmonik-structured data; the paste mechanism carries only human-readable instruction text.

**Structured-log audit.** Every daemon→pane write MUST be recorded as a structured log entry at INFO level with the following fields:

- `event`: `"daemon_pane_write"`
- `session_id`: the bead session UUID
- `pane_target`: the tmux pane target string (e.g., `harmonik-proj-session:task-window.0`)
- `buffer_name`: the buffer name used
- `purpose`: the purpose slug
- `payload_bytes`: the byte length of the payload

This ensures the operator and conformance tests can audit every daemon-injected write without parsing pane output.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### PL-022 — ntm adapter MUST NOT consume workflow-semantic features

The ntm adapter MUST NOT import or consume: (a) ntm's Pipeline System (harmonik's workflow semantics live in DOT graphs, not ntm pipelines), (b) ntm's SwarmPlan format (harmonik uses DOT per locked decision #7, not SwarmPlan), (c) ntm's checkpoint/recovery (tmux-session-resume is NOT equivalent to harmonik's git-checkpoint-based workflow-state recovery; the two solve different problems), or (d) ntm's file-reservation / Agent Mail features (harmonik uses Gas Town worktree+merge per locked decision #7; file reservations are explicitly rejected).

Tags: mechanism

#### PL-023 — Handler contract is the ntm boundary

The handler contract at [handler-contract.md §4.12] is where the ntm-vs-daemon boundary lives. Proposals that cross it by importing ntm pipeline types, SwarmPlan records, or Agent Mail primitives into the daemon MUST fail review. The ntm adapter is a thin layer bounded by the handler contract on the daemon side.

Tags: mechanism

### 4.8 Crash semantics

#### PL-024 — Daemon crash leaves a stale pidfile

On unexpected daemon termination (panic, SIGKILL, OS crash), the pidfile (§PL-002) is left stale. The next `harmonik daemon` invocation MUST detect a stale pidfile by checking that the recorded PID is no longer a live process (per PL-002a primitive selection), remove the stale pidfile, and proceed with startup per §PL-005. On restart, §PL-006 orphan sweeps residual tmux sessions, locks, and re-parented subprocesses before reconciliation classifies in-flight runs. PID-reuse-on-reboot is disambiguated per PL-002a by probing `/proc/<pid>/cmdline` on Linux (and the darwin equivalent) where available; the refusal-to-start-on-ambiguity path is tracked as OQ-PL-007.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### PL-025 — Crash during startup reconciliation re-runs from step 0

If the daemon crashes during startup reconciliation (after §PL-005 step 3 but before reaching `ready`), the next restart MUST re-run §PL-005 from step 0. Reconciliation is idempotent per [reconciliation/spec.md §4.1 RC-002]: re-running detection rules against the same git + Beads state produces the same classifications. Reconciliation workflows that were in-flight at crash time are re-classified (typically as Cat 5 for the outer run and Cat 3b for the investigator's verdict-unexecuted case) per the rules in [reconciliation/spec.md §4.2].

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### PL-025a — Lifecycle-event pairing tolerance

Consumers of `daemon_started`, `daemon_ready`, `daemon_orphan_sweep_completed`, `daemon_degraded`, and `daemon_shutdown` MUST tolerate unpaired events produced by a crash during the startup sequence. A `daemon_started` with no subsequent `daemon_ready`, `daemon_shutdown`, or `daemon_startup_failed` before the next `daemon_started` indicates a crash between PL-005 step 2 and one of those terminal emissions; the prior instance's lifecycle is treated as orphaned. The operator-nfr §4.8 RTO measurement ([operator-nfr.md §4.8 ON-031]) MUST use this pairing rule when computing restart RTO from SIGTERM to `daemon_ready`. A crash-induced unpaired `daemon_orphan_sweep_completed` is similarly orphaned and MUST NOT be misread as completion of the current daemon's sweep.

Tags: mechanism

#### PL-026 — Agent-subprocess crash routes through handler contract

An agent-subprocess crash that occurs while the daemon is alive MUST be handled per [handler-contract.md §4.6] (error propagation across async boundaries). The daemon routes the resulting outcome into reconciliation per [reconciliation/spec.md §4.2] only if the resulting run state is ambiguous; a cleanly-failed agent subprocess (explicit `FAIL` outcome, bounded exit code) produces a normal run-failure transition and does not trigger reconciliation.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

### 4.9 `harmonik upgrade` contract — daemon-internal mechanics

#### PL-027 — Upgrade contract obligation (daemon-internal side)

This spec owns the daemon-internal mechanics of `harmonik upgrade`; the operator-facing contract is owned by [operator-nfr.md §4.6 ON-020] and covers binary-source mechanism, operator-supplied hash check, drain-vs-reconciliation interaction, and cross-version state compatibility.

Daemon-internal mechanics (owned here):

(i) **Exec-replacement semantics.** The new daemon binary MUST replace the old via `execve` (or platform-equivalent) preserving the daemon PID. The new process MUST re-acquire the pidfile lock (PL-002a) on startup; the advisory lock is NOT transferred via exec on macOS `flock` — the new binary MUST call `flock(LOCK_EX|LOCK_NB)` in its own startup path per PL-005 step 1.

(ii) **Startup-sequence skip-path under exec.** When the daemon binary is launched via an `exec`-replacement from a live prior instance (detectable by the environment marker `HARMONIK_UPGRADE=1` set by the outgoing binary), the new instance MUST skip §PL-005 step 3 (orphan sweep) because no orphans exist — in-flight agent subprocesses and worktrees remain managed by the same PID. The new instance MUST still execute steps 0 (re-bootstrap registries), 1 (pidfile lock re-acquire), 2 (emit `daemon_started`), 4 (Cat 0 pre-check), 5–8 (walk + query + build + dispatch), and 9 (ready), and MUST re-emit `daemon_ready` on completion. All prior in-flight runs re-join the new instance's in-memory model at step 7.

(iii) **Socket continuity via fd-passing.** The outgoing daemon MUST clear `FD_CLOEXEC` on the listener fd (via `fcntl(listener_fd, F_SETFD, 0)`) immediately before `execve`, MUST pass the fd number to the new binary via the environment variable `HARMONIK_LISTENER_FD=<fd_number>` alongside the upgrade marker `HARMONIK_UPGRADE=1` of (ii), and MUST NOT close the fd before exec. The new binary, on detecting `HARMONIK_UPGRADE=1`, MUST NOT call `bind()` — instead it MUST call `net.FileListener(os.NewFile(fd, ""))` to adopt the existing listener, and MUST then re-set `FD_CLOEXEC` on the adopted fd to prevent leakage into future spawns. The socket path `.harmonik/daemon.sock` remains bound to the same listener inode throughout the exec window; clients observe no connection-refused gap.

The previously stated `T_rebind` interval is no longer load-bearing because adoption is gap-free; OQ-PL-002 carries any residual operator-tunable timeouts but no longer governs socket continuity.

> NON-REGRESSION NOTE (extqueue v0.1). The queue methods of PL-003a (`queue-submit`, `queue-append`, `queue-status`, `queue-dry-run`) ride the same socket and inherit the listener-fd passing of this requirement unchanged. The in-memory queue is reconstructed post-exec via PL-005 step 8a's `.harmonik/queue.json` read (per [queue-model.md §3 QM-002]); the listener fd is preserved across `execve` exactly as for `claim-next` / `emit-outcome`. PL-027(iii) is non-regressing with respect to the queue method-set extension; no second listener, no second fd, no second adoption path.

(iv) **Intermediate daemon state.** Between exec and re-bind of the socket, the daemon has no queryable status. `harmonik status` MAY observe this gap as a bounded-retry window corresponding to ON-020(e) socket/client-CLI retry behavior. The outgoing binary MUST write the upgrade-intent marker `.harmonik/daemon.upgrading` per [operator-nfr.md §4.6 ON-020a] before invoking `execve`; the file content is owned by ON-020a (operator-supplied `expected_commit_hash`, upgrade-initiation timestamp, operator's session_id). The write MUST follow the temp+rename+fsync(parent_dir) atomic discipline of [workspace-model.md §4.7 WM-026]: write content to a sibling temp file `.harmonik/daemon.upgrading.tmp-<pid>`; `fsync(temp_fd)`; `rename(2)` the temp file to `.harmonik/daemon.upgrading`; `fsync(parent_directory_fd)`. The marker MUST be present on disk and durable before `execve` is invoked. The new binary's PL-005 step 8a (per Item 6 below) reads this marker and applies upgrade-continuation semantics; on clean transition to `ready`, the marker is removed per ON-020a (also via temp+rename-style atomic unlink semantics — `rename`-into-place when replacing, `unlink` followed by parent-directory fsync when removing). This requirement was promoted from informative (v0.4.0) to normative in v0.4.1 per ON-020a coordination.

(v) **Upgrade event emissions.** The daemon MUST emit `operator_upgrading` before exec (per [event-model.md §8.7.9]) and `operator_upgrade_completed` after the new instance reaches `ready` (per [event-model.md §8.7.10]). Rejection paths emit `operator_upgrade_rejected` per [event-model.md §8.7.11].

The consistency obligation: the daemon's startup sequence (§PL-005) and shutdown sequence (§PL-011) MUST be consistent with whatever the operator-facing contract ([operator-nfr.md §4.6 ON-020]) produces; any inconsistency is a finalize-time reconciliation task.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=non-idempotent

### 4.10 Command surface (daemon side)

#### PL-028 — Daemon command surface

The daemon MUST support the following entry points:

- **`harmonik daemon`** — start the daemon headless. Blocks until signaled to stop; suitable for process-supervisor invocation. Flags: `--config <path>`, `--log-level <level>`. Behavior on a project with no `.harmonik/` directory is tracked as OQ-PL-003.
- **`harmonik attach`** — open an observability TUI over the socket. Multiple simultaneous attaches MUST be supported with no foundation-imposed upper limit; detaching MUST NOT kill the daemon. Concurrent-operator-attach arbitration is deferred to [operator-nfr.md §4.3] (see OQ-ON-004 for cross-spec coordination).
- **`harmonik runner`** — solo-dev convenience command. Executes the following in order: (1) if no daemon is running for the project, start the daemon (equivalent to `harmonik daemon &`); (2) wait for `daemon_ready` via the ready-detection protocol of PL-009b (bounded timeout per OQ-PL-002 defaults); (3) open a tmux session under the project's harmonik naming convention (prefix `harmonik-<project-hash>-`) with one pane for the daemon's log output and additional panes per active handler session (per ntm inspectability — locked decision #4); (4) if the `--orchestrator-agent` flag is supplied, spawn a Claude Code session in a separate tmux pane with the orchestrator-agent prompt and CLI access per §PL-019. On Claude Code unavailable, exit with ON §8 code 23 (`orchestrator-agent-unavailable`). On `ntm` unavailable, exit per PL-021a (ON §8 code 22 `ntm-unavailable`). `harmonik runner` is a distinct entry point with its own exit-code surface; it is NOT a shell alias for `daemon` + `attach`.
- **`hk queue append <group-index> <bead-id ...>`** — append items to a stream group via the socket (§PL-003, §PL-003a). Method: JSON-RPC `queue-append`; payload schema owned by [queue-model.md §7]. Validation errors per QM-020..QM-024 surface as JSON-RPC error codes `-32010..-32014`. Daemon-not-running → exit code 17 per ON §8 (remediation per [operator-nfr.md §4.1 ON-004]).
- **`hk queue dry-run <queue-file>`** — submit-validate a queue file without mutating state; returns the resolved plan including ledger-dep parallelism narrowing. Method: JSON-RPC `queue-dry-run`; payload schema owned by [queue-model.md §6]. Daemon-not-running → exit code 17 per ON §8 (remediation per [operator-nfr.md §4.1 ON-004]).
- **`hk queue status [--queue-id <uuid>]`** — report the daemon's current queue (groups, item statuses, queue-level `status` per [queue-model.md §2]). Method: JSON-RPC `queue-status`; output shape owned by [queue-model.md §2]. Daemon-not-running → exit code 17 per ON §8 (remediation per [operator-nfr.md §4.1 ON-004]).
- **`hk queue submit <queue-file>`** — submit a queue JSON document to the daemon. Method: JSON-RPC `queue-submit`; payload schema owned by [queue-model.md §2, §6]. Mints `queue_id` on accept and persists `.harmonik/queue.json` per [queue-model.md §3 QM-001]. Daemon-not-running → exit code 17 per ON §8 (remediation per [operator-nfr.md §4.1 ON-004]).

  > NOTE: `hk queue status` is namespaced and distinct from `harmonik status` below; the former reports queue state per [queue-model.md §2], the latter reports daemon state per §6.1 DaemonStatus.

- **`harmonik status`** — report daemon status over the socket. MUST report the §6.1 DaemonStatus enum value, and for `degraded` MUST report the failing Cat 0 prerequisite per §PL-010. Semantic content beyond the enum (operator-control subphase, health aggregation, RTO metrics) is owned by [operator-nfr.md §4.1 ON-002].
- **`harmonik pause`** — operator command; semantics owned by [operator-nfr.md §4.3 ON-008]. PL-028 obligates only command-dispatch and socket routing.
- **`harmonik stop [--graceful|--immediate]`** — shutdown command; `--graceful` is the default. Behavior per PL-011 / PL-012.
- **`harmonik upgrade`** — upgrade command; daemon-internal mechanics per PL-027; operator-facing semantics per [operator-nfr.md §4.6 ON-020].
- **`hk tmux-start`** — operator-facing tmux session bootstrap. Detailed contract in PL-028 refinement and PL-028b below.
- **`harmonik supervise <verb> [flags]`** — manage the supervisor/cognition process per §PL-019. v1 verbs: `start`, `stop`, `status`, `attach`, `restart`, `logs`. All verbs other than `status` and `logs` read or write `.harmonik/cognition/`; none routes over the daemon socket. The `attach` verb MUST `execve` into `tmux attach-session -t harmonik-<project_hash>-flywheel`, replacing the `harmonik supervise attach` process. Detailed contract per PL-028d. Exit codes: 25 on `start` when the singleton lock is held; 17 on `start` when the daemon socket is unreachable; 0 on successful start with `--detach`.
- **`harmonik digest [--project <dir>] [--watch] [--json] [--since <event_id>]`** — compute and display the current cognition digest (running/done/failed counts, stalled runs, queue depth, open notes, context-fullness if supervisor is live). Derived from `events.jsonl` + `queue.json` + `.harmonik/cognition/heartbeat.json`. Snapshot mode (no `--watch`) MUST operate without a running daemon and MUST NOT connect to the daemon socket. `--watch` MAY connect to the socket for live event streaming (gracefully degrading to file-poll when absent). `--since` restricts the window to entries after the given UUIDv7 (ScanAfter semantics per event-model.md §4.1). `--json` emits NDJSON. Exit 17 applies only when `--watch` requires the socket and the daemon is not running. Snapshot mode MUST exit 0 when the daemon is stopped; a missing `.harmonik/` is the sole Cat 0 failure (exit 7).

Agent-facing commands (`harmonik claim-next`, `harmonik emit-outcome`, and Beads-CLI-proxy methods) route over the same socket per PL-015; their concrete method set is tracked as OQ-PL-005. Command-dispatch is deterministic CLI; semantic behavior of `pause`, `stop`, and `upgrade` is owned by [operator-nfr.md §4.3, §4.6, §4.7]. All multi-daemon coordination flags (machine-level listing, ceiling config) are delegated to [operator-nfr.md §4.10 ON-041].

> NOTE (extqueue v0.1). The `harmonik enqueue` subcommand and its JSON-RPC `enqueue` method, present in v0.4.5 of this enumeration, are retired in v0.4.6 per the queue-model.md §1 retirement of the prior bead-by-bead enqueue surface. The four `hk queue <verb>` bullets above are the v0.1 replacement set; the remove / pause / resume / clear verbs are deferred to v0.2 per [queue-model.md §8].

Tags: mechanism

#### PL-028 refinement — `hk tmux-start` subcommand replaces `harmonik runner` tmux duties for MVH

The `harmonik runner` four-step lifecycle of PL-028 obligates the daemon (or runner wrapper) to open a tmux session in step 3. For the MVH this obligation is satisfied by a distinct subcommand `hk tmux-start`:

1. **Trigger conditions.** `hk tmux-start` is invoked by the operator explicitly when starting work from a non-tmux shell. MUST NOT be invoked automatically by the daemon. When the operator is already inside a tmux session (`$TMUX` set), `hk tmux-start` MUST refuse with a friendly message (exit code 0) and SHOULD print the session name they are already in.
2. **Steps.** `hk tmux-start` MUST execute:
   - **i.** Verify `$TMUX` is unset. If set, exit 0 with the directive.
   - **ii.** Compute the session name `harmonik-<project_hash>-default` per PL-006a provenance. `--session-name` flag MAY override; override MUST still carry the `harmonik-<project_hash>-` prefix.
   - **iii.** Invoke `tmux new-session -d -s <session-name> -c <project_dir>`. Idempotent if exists.
   - **iv.** `execve` `tmux attach-session -t <session-name>`, replacing the `hk tmux-start` process.
3. **`hk` started inside an `hk tmux-start`-created session.** When the operator runs `hk` from inside the session created by step 2.iv, `$TMUX` is set. `hk` therefore takes the PL-021b $TMUX-reuse path, creates handler windows in that same session.
4. **Relationship to PL-028 `harmonik runner`.** `harmonik runner` step-3 obligation is satisfied for the MVH by `hk tmux-start`; `harmonik runner` MAY be implemented as a convenience or deferred entirely until post-MVH.
5. **Exit codes.** 22 if `tmux -V` probe fails; 24 (PL-INTERIM) for any other unrecoverable failure during steps i–iv. Code 0 for the "$TMUX already set" no-op path.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### PL-028b — `hk` daemon refusal when `$TMUX` is unset

When `$TMUX` is unset at `harmonik daemon` startup, the daemon MUST refuse to enter the ready state. The refusal MUST be performed as part of the PL-005 step 4 Cat 0 pre-check: `$TMUX`-unset is added to the Cat 0 failure surface alongside `tmux-unavailable` from PL-021b §2. The daemon prints a directive that names `hk tmux-start` as the operator action and exits with ON §8 code 24 (`tmux-session-unavailable`, PL-INTERIM pending ON absorption).

Because PL-005 step 4 (Cat 0 pre-check) follows step 3a (socket bind) in the sequence, a refused daemon will have bound and immediately released its Unix-domain socket — this is acceptable because no handler subprocesses have been spawned and no event-bus state has been written beyond the `daemon_started` envelope. The daemon MUST NOT reach the ready state, MUST NOT emit `daemon_ready`, and MUST NOT advance to PL-005 step 5 or beyond.

The daemon MUST NOT silently create its own tmux session when `$TMUX` is unset; the operator must opt in explicitly via `hk tmux-start`.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### PL-028c — `hk queue` subcommand-family pre-`flag.Parse` dispatch

The `hk queue` family MUST be dispatched in `cmd/harmonik/main.go` before `flag.Parse` is called on the global flag set, mirroring the `tmux-start` and `hook-relay` precedent of `cmd/harmonik/main.go:70-115`. The dispatch MUST recognize `os.Args[1] == "queue"` and MUST read `os.Args[2]` as the verb (`submit`, `append`, `status`, `dry-run`). Each verb owns a hand-rolled flag scan accepting both `--flag value` and `--flag=value` shapes. Unrecognized verbs MUST exit with code 2 (`usage-error` per ON §8) emitting a one-line stderr message naming the v0.1 verb set. Each verb routes through a dedicated package (suggested layout: `internal/queue/cli`) returning an int exit code, consistent with the `tmux.RunTmuxStart` / `hookrelay.Run` patterns already established for `tmux-start` and `hook-relay`.

The daemon-not-running path MUST be uniform across all four verbs: socket-probe `ECONNREFUSED` against `.harmonik/daemon.sock` → exit code **17** (`multi-daemon-target-missing` per ON §8 / PL-008a). The single-project remediation prose (start `harmonik daemon` rather than `harmonik list`) is owned by [operator-nfr.md §4.1 ON-004]; PL-028c references it without restating.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

#### PL-028d — `harmonik supervise` subcommand-family verb contract

The `harmonik supervise` family MUST be dispatched in `cmd/harmonik/main.go` before `flag.Parse` is called on the global flag set, mirroring the `tmux-start`, `hook-relay`, and `hk queue` precedents of PL-028c. Dispatch MUST recognize `os.Args[1] == "supervise"` and read `os.Args[2]` as the verb. Unrecognized verbs MUST exit code 2 (`usage-error` per ON §8) and print a one-line stderr message naming the v1 verb set. Each verb routes through a dedicated package (suggested `internal/supervise/cli`) returning an int exit code.

Verb obligations:
- **`start [--model <id>] [--token-cap <n>] [--max-concurrent <n>] [--budget-cap <usd_per_day>] [--instructions <path>] [--priority-source kerf|beads|file:<path>] [--areas <list>] [--epic <codename>] [--detach] [--watch-restart]`** — acquire singleton lock, write config, create tmux session with `remain-on-exit on`, write sentinel, start supervisor. `--detach` returns to shell immediately. `--watch-restart` wraps the supervisor in a shim that respawns on non-zero exit. MUST refuse (17) if daemon socket unreachable. MUST refuse (25) if singleton lock held.
- **`stop [--timeout <duration, default 30s>]`** — read supervisor pid, send SIGTERM, wait up to `--timeout`, send SIGKILL if not exited, release lock, remove sentinel, optionally `tmux kill-session`. Mirrors PL-011 SIGTERM+bounded-wait+SIGKILL discipline.
- **`status [--json]`** — read `supervisor.pid` + `config.json` + `heartbeat.json`; probe via `kill(pid,0)`. Report pid, running, uptime, heartbeat age, `context_fullness_pct`, active/pending task counts. File-surface only.
- **`attach`** — `execve tmux attach-session -t harmonik-<project_hash>-flywheel`. MUST NOT kill the loop.
- **`restart`** — `stop` then `start` reading existing `config.json` (parameters preserved). MUST refuse (17) if daemon socket unreachable at `start`.
- **`logs [--lines <n, default 200>] [--follow]`** — `tmux capture-pane -p -S -<n>` for snapshot; `--follow` wraps `tmux pipe-pane` to a temp file. The pane IS the log surface at v1.

The daemon-not-running path for `start`/`restart` MUST be: socket-probe `ECONNREFUSED` against `.harmonik/daemon.sock` → exit 17, stderr directive `"daemon not running; start with: harmonik daemon"`.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent

## 5. Invariants

#### PL-INV-001 — One daemon per project

For each project directory at any instant, at most one daemon process MUST hold the pidfile lock at `.harmonik/daemon.pid`. This invariant spans [operator-nfr.md §4.3] (operator-control state machine requires a singular daemon to track status against), [beads-integration.md §4.10 BI-030] (the intent log is keyed against a single-writer daemon), and [workspace-model.md §4.3 WM-013a] (worktree leases assume a single leasing authority per project).

Sensor: pidfile lock held by the daemon's fd AND pidfile content parseable AND parsed PID equals `getpid()` AND parsed PGID equals `getpgrp()` AND parsed `daemon_instance_id` equals the in-memory `daemon_instance_id` minted at PL-005 step 0 (the line-3 check applies for v0.4.1+ pidfiles; v0.4.0 two-line pidfiles fall back to the PID/PGID portion of the predicate per PL-002b reader-tolerance) (PL-002 + PL-002a + PL-002b).

Tags: mechanism

#### PL-INV-002 — Daemon is deterministic

The daemon binary MUST contain no LLM invocations and no cognition-bearing components per [architecture.md §4.2]. This invariant spans [architecture.md §4.1] (four-axis classification assigns `llm-freedom=none` to the daemon as a whole), [architecture.md AR-INV-007] (centralized-controller invariant), and every subsystem spec's Go-package declaration.

Sensor: `go-arch-lint` rule on `internal/daemon` package imports asserting no LLM SDK (`github.com/anthropics/*`, `github.com/openai/*`, equivalents) appears in the transitive closure; plus a binary-level import-graph scan (§10.2).

Tags: mechanism

#### PL-INV-003 — Orphan sweep completes before reconciliation classification

§PL-006's orphan sweep MUST complete before any reconciliation detector (per [reconciliation/spec.md §4.3]) runs. This invariant is load-bearing for reconciliation correctness: detectors scope on runs ([reconciliation/spec.md RC-INV-005] detectors filter by `run_id`, never `bead_id`), and a run with a live orphan subprocess or stale worktree lock cannot be classified correctly until those orphans are cleared.

Sensor: the daemon maintains an in-memory flag `orphan_sweep_complete_at: Timestamp`; every §PL-005 step 8 reconciliation-dispatch path MUST assert the flag is non-nil before invoking any detector (per [reconciliation/spec.md §4.3]). An assertion failure is a panic per PL-018a.

Tags: mechanism

#### PL-INV-004 — Socket-path exclusivity

For each project directory at any instant, at most one bound Unix socket at `.harmonik/daemon.sock` MUST be serving daemon requests. This invariant pairs with PL-INV-001 (one daemon per project) and extends it to the socket surface: two daemons could in principle race to bind the socket if the pidfile-lock discipline were violated; this invariant forbids the observable outcome.

Sensor: the daemon that holds the pidfile lock (PL-002) is the exclusive owner of the bound socket fd. A second daemon observing `EADDRINUSE` on bind MUST exit with ON §8 code 6 (`socket-bind-failed`) per PL-008a; the exit path is the sensor.

Tags: mechanism

#### PL-INV-005 — Agent subprocess parentage is daemon-originated

Every live handler subprocess spawned during normal operation MUST have the daemon (by PID) as its initial parent; post-crash re-parenting to init (PID 1) is the only legal parentage deviation, and is cleaned by the next daemon's orphan sweep (§PL-006). This invariant is load-bearing for the orphan sweep's detection rule (PL-006a provenance marker assumes the subprocess was spawned by a daemon of this project, not by a user shell).

Sensor: every spawn site MUST set the provenance marker of PL-006a (environment variable + PGID). A subprocess without the marker is not a harmonik-owned subprocess by definition and MUST NOT be reaped by PL-006.

Tags: mechanism

## 6. Schemas and data shapes

### 6.1 Daemon status — state enumeration

Daemon status is a small enum consumed by the status event ([event-model.md §8.7.2]) and by `harmonik status`. The full operator-control state machine is owned by [operator-nfr.md §4.3]; this spec owns the `starting → reconciling → ready` prefix and the pre-`ready` `degraded` side state.

```
ENUM DaemonStatus:
    starting       -- pidfile locked; orphan sweep not yet complete
    reconciling    -- Cat 0 passed; reconciliation dispatch in progress
    degraded       -- Cat 0 prerequisite failing; classification halted (pre-ready only; see §PL-010)
    ready          -- §PL-009 criteria met; normal dispatch active
    paused         -- operator-initiated pause; in-flight runs drained (per [operator-nfr.md §4.3])
    draining       -- graceful-shutdown sequence active (§PL-011)
    stopped        -- terminal; pidfile released (per [operator-nfr.md §4.3])
```

> NOTE: Post-`ready` degradation (RTO breach, subsystem health aggregation failures, silent-hang fan-out) is emitted via the `daemon_degraded` event per [event-model.md §8.7.5] but does NOT correspond to a transition in this enum. Widening the enum to a reentrant `degraded` state is deferred as OQ-PL-009.

### 6.2 Co-owned event payloads

Events emitted by this spec whose payload schemas are registered in [event-model.md §6.3, §8.7]:

- `daemon_started` — emitted at transition (init) → `starting` per §7.1; payload `{started_at, pid, binary_commit_hash}`. Schema in [event-model.md §8.7.1].
- `daemon_ready` — emitted on transition to `ready` (§PL-009); payload `{ready_at, ready_at_ns_since_boot, investigator_run_ids[]}` (the monotonic-companion `ready_at_ns_since_boot` is required per [operator-nfr.md §4.8 ON-033]). Schema in [event-model.md §8.7.2].
- `daemon_shutdown` — emitted during drain per §PL-011a (mode `graceful`) and during interceptable immediate shutdown per §PL-012 (mode `immediate`); payload `{shutdown_at, shutdown_at_ns_since_boot, mode}` (the monotonic-companion `shutdown_at_ns_since_boot` is required per [operator-nfr.md §4.8 ON-033]). Schema in [event-model.md §8.7.3].
- `daemon_startup_failed` — emitted on fatal startup prerequisite failure per §PL-008a. Schema in [event-model.md §8.7.4].
- `daemon_degraded` — emitted on Cat 0 prerequisite failure per §PL-010 (and, post-`ready`, by the health surface consumers of [operator-nfr.md §4.9]). Schema in [event-model.md §8.7.5].
- `daemon_orphan_sweep_completed` — emitted on completion of §PL-006. Schema in [event-model.md §8.7.14].
- `infrastructure_unavailable` — emitted on Cat 0 failure (§PL-010). Schema in [event-model.md §8.7.15].
- `dispatch_deferred` — emitted when the per-daemon concurrency ceiling defers a dispatch (§PL-014a). Schema in [event-model.md §8.7.13].

The emitting spec is normative for the *when*; event-model is normative for the *shape*.

### 6.3 Schema evolution

This spec declares no persistent on-disk schema. The daemon-status enum is an in-memory and wire-format type; additions are backward-compatible when new statuses are introduced (consumers MUST tolerate unknown statuses by falling through to a default display, per [operator-nfr.md §4.5 ON-018] N-1 compatibility).

## 7. Protocols and state machines

### 7.1 Daemon status state machine

The daemon status machine's full transition set is owned by [operator-nfr.md §4.3]. This spec owns the `starting → reconciling → ready` prefix (and the pre-`ready` `degraded` side state) that operator-nfr builds on.

| From | Event | Guard | To | Emits |
|---|---|---|---|---|
| (init) | daemon process launches | pidfile lock acquired | starting | `daemon_started` |
| starting | orphan sweep complete | §PL-006 complete | reconciling | `daemon_orphan_sweep_completed` |
| starting | Cat 0 prerequisite failing | [reconciliation/spec.md §4.3 RC-012] pre-check fails | degraded | `infrastructure_unavailable` (+ `daemon_degraded`) |
| degraded | Cat 0 prerequisites cleared | retry pre-check succeeds | reconciling | — |
| reconciling | synchronous reconciliation dispatched | §PL-009 criteria met | ready | `daemon_ready` |
| ready | operator pause | per [operator-nfr.md §4.3] | (owned by operator-nfr) | — |
| ready / reconciling / degraded | SIGTERM, SIGINT, or `stop --graceful` | — | draining | `daemon_shutdown{mode=graceful}` |
| ready | `stop --immediate` (interceptable) | — | draining | `daemon_shutdown{mode=immediate}` |
| draining | drain complete | §PL-011 steps 3–8 complete | stopped | — |
| any | SIGKILL / panic | — | (crash; next startup recovers per §PL-024) | — (recovery emits `daemon_started` on restart) |

Post-`ready` degradation events (`daemon_degraded`) are emitted without a corresponding enum transition per §6.1 NOTE and OQ-PL-009. The orchestrator-agent (§PL-019) is NOT a state in this machine; it is a separate process with its own lifecycle that interacts with the daemon over the CLI.

## 8. Error and failure taxonomy

This spec does not own a failure taxonomy. Startup failure modes are cataloged per §PL-008 (obligation owned by [operator-nfr.md §4.1 ON-003]); §PL-008a names the codes this spec consumes from the authoritative ON §8 taxonomy. Codes 22 (`ntm-unavailable`) and 23 (`orchestrator-agent-unavailable`) — declared PL-INTERIM in v0.4.0 — were absorbed into ON §8 in ON v0.4.0 and are now first-class entries; no PL-side interim marker remains. Code 17 (`multi-daemon-target-missing`) is consumed by the `hk queue *` CLI surfaces per PL-028 / PL-028c; the single-project `ECONNREFUSED` remediation prose extension is owned by [operator-nfr.md §4.1 ON-004]. Run-failure taxonomy is owned by [execution-model.md §6.3]. Reconciliation-category taxonomy is owned by [reconciliation/spec.md §8]. Crash semantics (§PL-024, §PL-025, §PL-026) route through those taxonomies rather than defining their own.

## 9. Cross-references

### 9.1 Depends on

- **[architecture.md §4.1]** — four-axis classification; daemon is `llm-freedom=none` by design.
- **[architecture.md §4.2]** — ZFC mechanism/cognition test; daemon is mechanism-only (PL-018).
- **[architecture.md §4.4]** — subsystem envelope; §PL-020 composition root enforces it; §4.a declares this spec's envelope.
- **[architecture.md §4.5 AR-016]** — subsystem runtime realization as a Go package; daemon composition root is an `internal/daemon` Go package.
- **[architecture.md AR-INV-007]** — centralized-controller invariant (including daemon-owned cross-subsystem registry clause); PL-018, PL-019, PL-020, PL-020a, and PL-INV-002 enforce it.
- **[architecture.md §4.0 AR-052, AR-053]** — spec-category and envelope slot; this spec declares `spec-category: runtime-subsystem` and §4.a.
- **[execution-model.md §4.4 EM-017]** — checkpoint contract; §PL-005 step 5 walks trailers; §PL-011 step 3 relies on durable checkpoints.
- **[execution-model.md §6.1 Run]** — run record shape; §PL-005 step 7 builds the in-memory model from this.
- **[execution-model.md §6.2]** — checkpoint-commit trailer format; `Harmonik-Run-ID` is the walk key at PL-005 step 5.
- **[event-model.md §4.3]** — bus and consumer taxonomy; §PL-005 step 0 bootstraps the bus.
- **[event-model.md §4.1]** — event ID and `event_id_hwm`; §PL-004 enumerates the on-disk file.
- **[event-model.md §4.4]** — durability classes and fsync semantics; §PL-011 step 6 performs the bus flush.
- **[event-model.md §6.2]** — on-disk JSONL format; §PL-004 enumerates the file surface.
- **[event-model.md §6.3, §8.3, §8.7, §8.6]** — per-type payload schemas and the daemon-lifecycle taxonomy; §PL-006/§PL-009/§PL-010/§PL-011/§PL-014a cite emission points.
- **[handler-contract.md §4.1 HC-001]** — handler interface; daemon spawns subprocesses via handlers.
- **[handler-contract.md §4.2 HC-007a]** — NDJSON framing; §PL-003a inherits the wire-frame discipline.
- **[handler-contract.md §4.3 HC-011]** — watcher goroutine; §PL-016 routes through it.
- **[handler-contract.md §4.4 HC-018]** — cancellation bound; §PL-006 SIGTERM→SIGKILL interval is consistent.
- **[handler-contract.md §4.6]** — error propagation and silent-hang detection; §PL-017 names the obligation.
- **[handler-contract.md §4.10 HC-044, HC-044a]** — socket authenticity (mode `0600`) and subprocess pidfile placement.
- **[handler-contract.md §4.11]** — skill injection at launch; §PL-015 references Beads-CLI skill routing.
- **[handler-contract.md §4.12]** — handler-as-modularity-boundary; §PL-023 names it as the ntm boundary.
- **[control-points.md §4.1]** — control-point registry; §PL-005 step 0 bootstraps it per AR-INV-007.
- **[queue-model.md §2, §3, §6, §7, §8]** — queue document schema, persistence layout (`.harmonik/queue.json`), JSON-RPC method payload schemas, validation error codes, and pause-by-failure recovery; PL-003a names the wire methods, PL-005 step 8a reads the persisted queue, PL-004 enumerates the file surface, PL-028 and PL-028c name the CLI surface.
- **[reconciliation/spec.md §4.1 RC-002]** — reconciliation idempotence; §PL-025 depends on it.
- **[reconciliation/spec.md §4.1 RC-002a, RC-002b]** — per-target-run reconciliation lock at `.harmonik/reconciliation-locks/<target_run_id>.lock`; §PL-006 stale-reconciliation-lock sweep enumerates and removes stale entries.
- **[reconciliation/spec.md §4.2 RC-008]** — action-mapping; §PL-005 step 8 routes by it.
- **[reconciliation/spec.md §4.3 RC-012, RC-013]** — detectors and Cat 0 pre-check; §PL-005 step 4 invokes; §PL-009a routes through RC-013 emission.
- **[reconciliation/spec.md §8]** — category taxonomy; §PL-009a re-classification uses Cat 3 as a fallback.
- **[reconciliation/spec.md §8.12]** — action-mapping layer; §PL-009a enumerates auto-resolver categories.
- **[beads-integration.md §4.5 BI-013, BI-016]** — harmonik read surface; §PL-005 step 6 queries via these.
- **[beads-integration.md §4.9 BI-027]** — Beads-CLI skill; §PL-015 references skill-routed agent commands.
- **[beads-integration.md §4.10 BI-030]** — intent log and idempotency-keyed writes; §PL-004 enumerates the directory.
- **[beads-integration.md §6.2]** — intent-log on-disk layout; §PL-004 enumerates the file surface.
- **[workspace-model.md §4.1 WM-002]** — worktree path convention; §PL-006 filesystem scan relies on it.
- **[workspace-model.md §4.2 WM-005]** — task-branch naming (`run/<run_id>`); §PL-005 step 5 scans via this convention.
- **[workspace-model.md §4.3 WM-013a, WM-013b]** — lease model and lease-lock; §PL-006 and §PL-011 reference them.
- **[workspace-model.md §4.7 WM-026]** — temp+rename+fsync(parent_dir) atomic write discipline; §PL-005 step 0, §PL-027(iv), and the queue-manager's `.harmonik/queue.json` write per queue-model.md all consume this.
- **[workspace-model.md §4.8 WM-033]** — startup orphan sweep of stale lease-lock files; §PL-006 coordinates with WM on the sweep.

### 9.2 Reverse dependencies

> INFORMATIVE: Reverse dependencies are computed on demand from all specs' `depends-on` lists. At draft time, specs known to depend on this spec include `handler-contract.md` (launch + socket model per §4.2, §4.12), `workspace-model.md` (leases assume a single-daemon leasing authority per §PL-INV-001), `operator-nfr.md` (§4.3 builds on this spec's status-prefix; §4.8 measures from this spec's `daemon_ready` event; §4.6 builds on this spec's upgrade mechanics), `reconciliation/spec.md` (§4.2 assumes this spec's orphan-sweep invariant), `event-model.md` (daemon-core-emitted events at §8.7 have their emission timing owned here), `beads-integration.md` (§4.10 intent log is keyed against a single-writer daemon), and `queue-model.md` (queue persistence at `.harmonik/queue.json` and queue methods on the daemon socket rely on PL-003a / PL-005 step 8a / PL-004).

### 9.3 Co-references

- **[core-scope.md §5 Orchestrator loop]** — this spec consumes the daemon-as-sole-driver framing declared there; no reverse dependency (core-scope is a foundation document, not a spec).
- **[core-scope.md §6 Subsystem organization]** — this spec consumes the `internal/daemon` composition-root declaration; no reverse dependency.
- **[operator-nfr.md §4.1 ON-002, ON-003, ON-004]** — exit-code taxonomy, startup-failure-mode catalog, and configuration-knob inventory; §PL-008 consumes the catalog; the queue-CLI daemon-not-running remediation prose extension for code 17 is owned by ON-004; PL-013's prior re-query cadence knob is removed from the inventory per the extqueue v0.1 retirement; `harmonik status` surface (§PL-010, §PL-028).
- **[operator-nfr.md §4.3 ON-007–ON-014]** — operator-control state machine and operator-control semantics; PL owns the `starting → reconciling → ready` prefix, operator-nfr owns the rest. Also anchors concurrent-operator-attach arbitration per OQ-ON-004.
- **[operator-nfr.md §4.5 ON-018]** — N-1 schema compatibility; §6.3 consumes it.
- **[operator-nfr.md §4.6 ON-020]** — `harmonik upgrade` operator-facing contract; §PL-027 co-owns the daemon-internal mechanics.
- **[operator-nfr.md §4.6 ON-020a]** — upgrade-intent durable marker (`.harmonik/daemon.upgrading`); §PL-027(iv) writes the marker normatively; §PL-005 step 8a reads it.
- **[operator-nfr.md §4.7 ON-027, ON-029]** — graceful-shutdown cross-subsystem ordering and drain timeout; §PL-011 names the daemon-level sequence.
- **[operator-nfr.md §4.7 ON-030a]** — pause-state durable marker (`.harmonik/daemon.state`); §PL-005 step 8a reads it and gates step 9.
- **[operator-nfr.md §4.8 ON-031, ON-032]** — restart RTO and RTO-breach reporting; §PL-009 defines the measurement endpoint (`daemon_ready` emission).
- **[operator-nfr.md §4.8 ON-033]** — RTO measurement boundary requires monotonic-companion fields; §PL-009 / §PL-011a payloads carry `ready_at_ns_since_boot` and `shutdown_at_ns_since_boot` accordingly.
- **[operator-nfr.md §4.9 ON-036, ON-037]** — health surface and liveness; post-`ready` degradation is emitted via `daemon_degraded` but does not transition the §6.1 enum (see OQ-PL-009).
- **[operator-nfr.md §4.10 ON-041]** — multi-daemon commands (`harmonik list`, machine-level budget coordination); §PL-014a concurrency ceiling delegates machine-level coordination here.
- **[docs/foundation/components.md §8]** — bootstrap-era source for this spec's normative content, migrated on finalize.

> NOTE: operator-nfr is kept as a §9.3 co-reference rather than a front-matter `depends-on` to break the PL ↔ ON front-matter cycle. ON continues to `depends-on: process-lifecycle` because ON builds on PL's daemon-shape and measurement endpoints; PL names ON obligations (PL-008, PL-027) per the template §2 / §9 "named obligation" pattern which is legal without a forward dependency.

## 10. Conformance

### 10.1 Conformance profiles

**Core MVH.** An implementation conforms to Core MVH when it passes PL-001 through PL-028 (including PL-002a, PL-002b, PL-003a, PL-003b, PL-006a, PL-008a, PL-009a, PL-009b, PL-011a, PL-014a, PL-018a, PL-020a, PL-021a, PL-025a, PL-028c) and satisfies all five invariants (PL-INV-001, -002, -003, -004, -005). PL-013 is retired per extqueue v0.1 and is not a conformance gate. The operator-facing `harmonik upgrade` contract sub-obligations (PL-027 (i)–(v)) depend on operator-nfr ON-020 finalizing for full conformance; the daemon-internal mechanics (i)–(iv) are in-scope for Core MVH as declared here.

**Post-MVH.** Orchestrator-agent session integration (PL-019) is OPTIONAL in MVH. An implementation MAY conform to Core MVH without ever spawning an orchestrator-agent.

### 10.2 Test-surface obligations

> INFORMATIVE: `specs/testing.md` does not yet exist. Test obligations are named in prose here and MUST migrate to test-layer citations within one revision cycle of testing.md landing. See OQ-PL-001.

For each requirement, the implementation MUST satisfy at least one test covering the behavior:

- **PL-001, PL-002, PL-002a, PL-INV-001** — a twin-driven test that attempts to start a second daemon against the same project and asserts it exits with the pidfile-contention exit code (`5`). An additional test crashes the first daemon (SIGKILL) and asserts the second daemon's stale-pidfile detection (PL-024) via `flock` + `kill(pid, 0)` logic.
- **PL-003, PL-003a, PL-INV-004** — a binding test that asserts socket mode `0600`, socket-path exclusivity (second daemon observing `EADDRINUSE` exits with exit code `6`), and NDJSON framing correctness against a JSON-RPC client. The wire-method registry assertion MUST enumerate the queue method names (`queue-submit`, `queue-append`, `queue-status`, `queue-dry-run`) and assert that `enqueue` is NOT a registered method (extqueue v0.1 retirement).
- **PL-005, PL-006, PL-006a, PL-007, PL-INV-003, PL-INV-005** — a scenario test that seeds tmux sessions, stale worktree locks, re-parented handler subprocesses AND re-parented `br` subprocesses (with and without the provenance marker), stale `.harmonik/reconciliation-locks/*.lock` files (with and without the `Harmonik-Verdict-Executed: true` commit on the investigator branch per RC-002b), and stale intent files; then starts the daemon and asserts the orphan-sweep event payload matches expected counts (including the new `br_subprocesses_killed` and `reconciliation_locks_removed` count fields); asserts that subprocesses lacking the marker are NOT killed; asserts that locks under active acquisition by a concurrent process (simulated via `flock(LOCK_EX)` from a fixture process) are NOT removed; and asserts that the `orphan_sweep_complete_at` flag is set before any reconciliation detector runs. A companion test seeds `.harmonik/queue.json` with a recognized schema and asserts the in-memory queue is loaded at step 8a, including persisted per-group / per-item statuses and the `paused-by-failure` / `paused-by-drain` queue statuses; an additional test seeds a forward-incompatible `schema_version` and asserts the daemon refuses startup with code 2 and emits `daemon_startup_failed{failure_mode="queue-format-unsupported"}`; an additional test seeds a corrupt `.harmonik/queue.json` and asserts the daemon proceeds without an in-memory queue and emits the structured-log warning per ON-035.
- **PL-008a** — a unit test asserting every exit code consumed by this spec (5–10, 14, 17, 19, 22, 23) maps to a distinct failure, and that `daemon_startup_failed` is emitted on each (where the event bus has been initialized). Code 17 is asserted via a `hk queue *` invocation against a daemon-down project (no `.harmonik/daemon.sock` listener).
- **PL-009, PL-009a, PL-010** — scenario tests covering (a) `ready` transition only when criteria are met, (b) `degraded` persistence until Cat 0 clears, and (c) auto-resolver failure routing to Cat 3 investigator workflows without blocking `ready`.
- **PL-011, PL-011a, PL-012** — scenario tests for graceful drain (asserting in-flight runs reach a checkpoint before suspend, that `daemon_shutdown{mode=graceful}` is emitted before bus flush), and immediate abort (asserting subprocess kill + `daemon_shutdown{mode=immediate}` on interceptable path + next-startup recovery on SIGKILL).
- **PL-013** — retired per extqueue v0.1; no test obligation. The surviving "MUST NOT exit on idle" obligation is covered by the PL-011 / PL-012 scenario suite (the daemon exits only on `harmonik stop`, upgrade, or crash; a queue-empty state is observable without an exit).
- **PL-018, PL-018a, PL-INV-002** — a build-time lint (e.g., `go-arch-lint` per [core-scope.md §6]) asserting `internal/daemon` imports no LLM SDK, plus a unit test that inspects the binary's import graph. An additional test installs a panic in a goroutine and asserts the top-level `recover()` barrier terminates the daemon with ON §8 code 19 (`runtime-panic`).
- **PL-020, PL-020a** — `go-arch-lint` declaration that `internal/daemon` is the only subsystem-crossing importer; plus a wiring test that instantiates the event bus, control-point registry, handler registry, and skill registry inside the composition root on startup.
- **PL-021, PL-021a, PL-022, PL-023** — lint rule: `internal/adapter/ntm` imports only the allowed ntm surface (process/tmux); importing ntm pipeline or SwarmPlan packages is a build failure. Absence-detection test: run the daemon on a PATH without `ntm` and assert ON §8 code 22 (`ntm-unavailable`).
- **PL-024, PL-025, PL-026** — chaos-style scenario tests: SIGKILL the daemon mid-reconciliation; assert the next startup re-runs §PL-005 from step 0 deterministically and produces identical classifications.
- **PL-027** — upgrade scenario test: exec-replace the daemon binary and assert (i) pidfile lock is re-acquired, (ii) orphan sweep is skipped, (iii) listener fd is adopted gap-free via `HARMONIK_LISTENER_FD` per the fd-passing protocol (no client connection-refused gap observable across exec, including for in-flight queue-method invocations), (iv) `operator_upgrading` and `operator_upgrade_completed` emission bracket the exec.
- **PL-028, PL-028c** — CLI-surface test: dispatch each declared command, assert JSON-RPC method wiring, flag parsing, and exit-code behavior. The `hk queue` family MUST be tested for: (a) pre-`flag.Parse` dispatch (the verb is recognized when global flags would otherwise reject the argv shape), (b) `--flag value` and `--flag=value` both parse correctly per verb, (c) unrecognized verb exits with code 2 and stderr names the v0.1 verb set, (d) all four verbs exit with code 17 against a daemon-down project (no `.harmonik/daemon.sock` listener).

### 10.3 Excluded conformance claims

- `harmonik upgrade` operator-facing contract conformance (binary-source mechanism, hash-check procedure, cross-version schema compat) — owned by [operator-nfr.md §4.6 ON-020]; this spec makes conformance claims only over daemon-internal mechanics per PL-027.
- Silent-hang detection rule conformance — owned by [handler-contract.md §4.6]; this spec names the obligation but does not test the detection rule directly.
- Reconciliation classification correctness — owned by [reconciliation/spec.md §4.7]; this spec tests only the orphan-sweep precondition and the auto-resolver-failure fallback path per PL-009a.
- Restart RTO numeric target — owned by [operator-nfr.md §4.8]; this spec defines only the measurement endpoint.
- Multi-daemon commands (`harmonik list`, machine-level budget coordination) — owned by [operator-nfr.md §4.10 ON-041].
- Queue document schema, queue-method payload schemas, queue-status output shape, queue-state persistence layout, and queue-validation error semantics — owned by [queue-model.md §2, §3, §6, §7]; this spec makes conformance claims only over socket transport, the step-8a persisted-queue read, the file-surface inventory entry, and CLI dispatch shape per PL-028 / PL-028c.
- Cross-subsystem citation-anchor correctness — the corpus-wide batch-2 migration (58 stale cites across five sibling specs) is tracked separately; this spec's outbound citations have been migrated to current anchors in v0.3.0. Remaining sibling-side drift is outside this spec's conformance surface. Sibling specs MAY still cite this spec at legacy anchors (`process-lifecycle.md §8.N`); each sibling's next revision SHOULD migrate to current anchors per its own integration cycle. PL's outbound citations are clean as of v0.4.0.

## 11. Open questions

#### OQ-PL-001 — Migrate test-surface obligations to testing.md citations

Question: When `specs/testing.md` lands, §10.2's prose test obligations must migrate to citations of the form `[testing.md §<layer>]`.
Owner: foundation-author
Blocks: none (Core MVH tests can be authored against the prose obligations in the interim).
Default-if-unresolved: Tests follow the prose obligations; migrate within one revision cycle of testing.md finalizing.

#### OQ-PL-002 — Bounded timeouts for orphan-sweep sub-steps

Question: What are the normative upper bounds for the tmux-kill poll ceiling (§PL-006 "2-second ceiling"), the SIGTERM→SIGKILL interval for re-parented subprocesses (§PL-006 "5-second bounded interval"), the Cat 0 retry cadence (§PL-010 "10s default"), and the ready-detection wait `T_ready_wait` (§PL-009b "60s default")?
Owner: foundation-author (coordinated with operator-nfr for consistency with drain timeout §4.7 and RTO §4.8)
Blocks: none (MVH uses suggested values above).
Default-if-unresolved: Suggested defaults above; tune per operator feedback.

#### OQ-PL-003 — `.harmonik/` directory auto-creation

Question: If a project has a git repo but no `.harmonik/` directory, does `harmonik daemon` auto-create it, or does it fail and require `harmonik init`?
Owner: foundation-author
Blocks: PL-001, PL-028 (`harmonik daemon` behavior on a bare git repo is undefined).
Default-if-unresolved: Require `harmonik init` (explicit opt-in); daemon fails with a specific exit code if `.harmonik/` is absent.

#### OQ-PL-004 — Cross-spec lease-lock-path alignment

Question: The canonical lease-lock path disagrees across three specs: WM-013a declares `${workspace_path}/.harmonik/lease.lock`; HC-044a names `.harmonik/worktrees/<run_id>/.lock` (resolving to `${workspace_path}/.lock`); this spec's PL-006 references `.harmonik/lease.lock`. A coordinated resolution is needed before the orphan sweep can reliably remove stale lock files.
Owner: foundation-author (coordinated author for PL/HC/WM)
Blocks: PL-006 worktree-lock bullet; cross-spec citation finality.
Default-if-unresolved: PL-006 matches whichever filename was written by WM-013a on the same daemon generation; HC's fail-fast path is independent per HC-044a; final alignment lands in all three specs in lockstep (tracked at WM-level as OQ-WM-005).

#### OQ-PL-005 — Agent-command JSON-RPC method inventory (RESOLVED in v0.4.0)

Resolved in v0.4.0 — the JSON-RPC method-name inventory is pinned in PL-003a (agent-facing: `claim-next`, `emit-outcome`, `dispatch-status`, Beads-CLI-skill proxy methods; CLI-facing: `status`, `pause`, `resume`, `stop`, `upgrade`, `attach`, `list`; v0.1 queue method set added in v0.4.6 per extqueue: `queue-submit`, `queue-append`, `queue-status`, `queue-dry-run`). Method payload schemas remain intentionally deferred except for the queue methods, whose schemas are owned by [queue-model.md §6, §7]. The `enqueue` method present through v0.4.5 is retired in v0.4.6.

#### OQ-PL-006 — Orphan-sweep stale-intent handling coordination with RC

Question: PL-006 currently defers stale-intent-file classification to the reconciliation Cat 3a detector during §PL-005 step 8, NOT during the pre-Cat-0 sweep, because [reconciliation/spec.md §4.3 RC-012] gates detectors on Cat 0 passing. The original v0.2 draft had the sweep invoke the detector directly — the contradiction was a critic finding. The current resolution defers classification; reconciliation may need to adopt a pre-Cat-0 stale-intent probe path if the current deferral under-covers specific failure shapes.
Owner: foundation-author (coordinated with reconciliation)
Blocks: PL-006 stale-intent bullet full acceptance; reconciliation-side may need RC-amendment.
Default-if-unresolved: As declared in PL-006 (defer to step 8); revisit if reconciliation R2 surfaces under-coverage.

#### OQ-PL-007 — Pidfile PID-reuse-on-reboot disambiguation

Question: After an OS reboot, the previously-recorded PID in `.harmonik/daemon.pid` may be reused by an unrelated process. PL-002a names the corroboration path (`/proc/<pid>/cmdline` on Linux, `proc_pidpath` on darwin), but the refusal-to-start-on-ambiguity path is not named.
Owner: foundation-author
Blocks: PL-002a edge-case path.
Default-if-unresolved: On ambiguity (unable to probe, or probe returns a non-harmonik binary), the daemon treats the PID as stale, removes the pidfile, and proceeds with startup per PL-024. Warning logged.

#### OQ-PL-008 — macOS provenance-marker mechanism

Question: On darwin, `/proc/<pid>/environ` is not available, so the PL-006a environment-variable side of the provenance marker is not readable by the orphan sweep. The PGID side remains available. Is PGID alone sufficient for darwin, or is a filesystem-based fallback (a per-pid marker file at `/tmp/harmonik-<project-hash>-<pid>.marker`) required?
Owner: foundation-author
Blocks: PL-006a darwin path; PL-INV-005 sensor correctness on darwin.
Default-if-unresolved: PGID is the primary marker on darwin; environment variable is set for consistency but not read by the sweep. No filesystem fallback at MVH.

#### OQ-PL-009 — Post-`ready` degradation scope

Question: The §6.1 DaemonStatus enum's `degraded` value is pre-`ready` only (Cat 0 side state). `daemon_degraded` events carry `reason ∈ {rto_breach, reconstruction_notify, other}` which are post-`ready` triggers per event-model §8.7.5. Should the §6.1 enum widen to a reentrant `degraded` state with a `ready` → `degraded` → `ready` cycle (option a), or should PL-010 remain narrowly-scoped and post-`ready` degradation stay a health-surface concern (option b)?
Owner: foundation-author (coordinated with operator-nfr)
Blocks: PL-010 scope finality; event-model §8.7.5 reason enum semantics.
Default-if-unresolved: Option b — pre-`ready` `degraded` only in the §6.1 enum; post-`ready` `daemon_degraded` events do not transition the enum.

#### OQ-PL-010 — `FDS_PER_HANDLER` and `FALLBACK_CAP` ceiling defaults

Question: The PL-014a derived ceiling defaults (`FDS_PER_HANDLER = 8`, `FALLBACK_CAP = 1024`) are conservative engineering picks awaiting fixture observation.
Owner: foundation-author
Blocks: nothing critical (PL-014a default is conservative).
Default-if-unresolved: `FDS_PER_HANDLER=8`, `FALLBACK_CAP=1024`.

#### OQ-PL-011 — Handler-side `setsid` / PGID-break disclosure

Question: PL-006a relies on the recorded PGID for orphan-sweep coverage on darwin; handlers that internally call `setsid` (e.g., nohup-style wrappers) escape the marker and the orphan sweep cannot reap their descendants.
Owner: handler-contract
Blocks: PL-006a sweep coverage on darwin.
Default-if-unresolved: Handlers MUST NOT internally `setsid`; the orphan sweep treats setsid-detached descendants as out-of-scope and may leak them on darwin.

#### OQ-PL-012 — `daemon_shutdown` durability class confirmation

Question: PL-011a emits `daemon_shutdown` before the bus flush; the durability class for this event must be confirmed against [event-model.md §8.7.3].
Owner: foundation-author (coordinated with event-model)
Blocks: durability claim in PL-011a.
Default-if-unresolved: Assume class F (fsync-boundary); coordinate with EV in next revision.

#### OQ-PL-013 — Mid-run ENOSPC routing to Cat 0

Question: Disk-full conditions encountered mid-run (not at startup) must route through the Cat 0 surface so that the daemon transitions to `degraded` and resumes safely after the operator clears the condition.
Owner: foundation-author (coordinated with reconciliation)
Blocks: full Cat 0 coverage of mid-run failures.
Default-if-unresolved: Emit `infrastructure_unavailable{failed_prerequisite=disk_full}` on a best-effort basis and route through PL-010 retry cadence.

#### OQ-PL-014 — Per-spawn ntm version probe (mid-session drift)

Question: PL-021a probes ntm version at startup; mid-session drift (operator upgrades ntm under a running daemon) is not detected.
Owner: foundation-author
Blocks: mid-session ntm-drift detection.
Default-if-unresolved: Probe at startup only; mid-session drift detected by reconciliation Cat 0 pre-check periodic retry.

#### OQ-PL-015 — Operator-facing surface for discovering the tmux window name

Question: PL-021b §4 asserts window-naming is deterministic and replay-stable, and PL-021b §8 makes `tmux_window_name` available in the `agent_started` event. However, there is no operator-facing command (e.g., `hk attach --list-windows`) that surfaces the active window name without requiring the operator to parse event payloads or run `tmux list-windows` manually. An ergonomic attach flow requires the operator to know the window name to attach to.
Owner: foundation-author
Blocks: ergonomic `hk attach` workflow for $TMUX-reuse mode.
Default-if-unresolved: Operator uses `tmux list-windows` filtered by the `hk-<hash6>-` sentinel to locate harmonik windows. Post-MVH, `hk attach --list-windows` or equivalent SHOULD be added to the operator command surface.
Cross-ref: PL-021b §4 (window-naming determinism), PL-021b §8 (window-name in `agent_started` payload), [workspace-model.md §4.1 WM-002a] (deterministic window-naming spec). Refs: hk-gql20.25.

## 12. Revision history

| Date | Version | Author | Summary |
|---|---|---|---|
| 2026-05-30 | 0.5.0 | agent (flywheel spec-bundle hk-j7o3i) | **PL-019 promotion + PL-006d + PL-028d/digest additions (flywheel design).** (1) **PL-019** expanded from an informative post-MVH stub to a full normative section (a–h) specifying the supervisor/cognition process: process separation, spawn surface (`harmonik supervise start`), singleton flock at `.harmonik/cognition/supervisor.lock`, pidfile discipline, config JSON with `daemon_instance_id`, tmux session `harmonik-<project_hash>-flywheel` with `remain-on-exit`, file-surface inventory under `.harmonik/cognition/`, PL-018 relationship confirmation (daemon LLM-free invariant unaffected). Daemon-side obligations: refuse to start the supervisor, do NOT read/write cognition subtree beyond sentinel probe per PL-006d. (2) **PL-006d** (new, after PL-006c): orphan-sweep exclusion for coordinator tmux sessions via `.harmonik/cognition/supervisor.sentinel` file; sentinel-present+PID-live → SKIP with structured-log `orphan_sweep_skipped_coordinator_session`; stale-sentinel → kill + remove sentinel; adds `coordinator_sessions_skipped` integer to `daemon_orphan_sweep_completed` payload per [event-model.md §8.7.14] additive-tolerance precedent. Refs: hk-hc3qq. (3) **PL-028** gains `harmonik supervise <verb>` and `harmonik digest` bullets. **PL-028d** (new, after PL-028c): full verb contract for `supervise` family — pre-`flag.Parse` dispatch on `os.Args[1]=="supervise"`; 6 verbs (`start`, `stop`, `status`, `attach`, `restart`, `logs`) with per-verb obligations; exit codes 17/25/0; daemon-not-running path. §3 Glossary gains `supervisor process`, `cognition subtree`, `coordinator sentinel`. PL-004 file-surface inventory gains cognition-subtree exclusion note. §10.1 conformance profile: supervisor integration is post-MVH; PL-006d sentinel-probe MUST be present even with no supervisor (degrades gracefully). Refs: hk-j7o3i, hk-hc3qq (orphan sweep kills flywheel pane). |
| 2026-05-19 | 0.4.8 | agent (hk-o0yft) | **PL-006b, PL-006c — BeadResetter and BeadCat3cCloser extension points (normative subsections).** Adds two new subsections between PL-006a and PL-007. **PL-006b** names `lifecycle.BeadResetter` as the normative Go interface abstraction for the bead-reset write path (`in_progress → open` via BI-010d): specifies the `ResetBead` method signature, BI-030 intent-log obligation, idempotency-key formula (`<project_hash>:<bead_id>:reset:<daemon_start_ns>`), production satisfaction by `*brcli.Adapter.ResetBead`, and the skip-when-nil contract (bead-reset sweep is skipped; `bead_in_progress_reset` counter in `daemon_orphan_sweep_completed` is 0). **PL-006c** names `lifecycle.BeadCat3cCloser` as the normative Go interface abstraction for Cat 3c auto-resolution (closing subsumed beads — `in_progress` where a `Harmonik-Bead-ID` merge commit is already on the target branch; hk-lgtq2): specifies `SweepCloseBead` method signature, BI-010b reconciliation-driven write routing, absence of BI-030 intent-log (no associated run, no RunID/TransitionID), Beads-level idempotency, MUST-call-when-non-nil obligation (sweep MUST NOT reset a Cat 3c bead), skip-when-nil contract (safe-but-incomplete; bead remains in_progress), and production satisfaction by `*brcli.Adapter.SweepCloseBead`. Cross-spec coordination: both subsections reference existing BI-010b / BI-010d / BI-012 / BI-030 anchors; no new cross-spec obligations created. Refs: hk-o0yft, hk-iuaed (bead-reset path), hk-lgtq2 (Cat 3c auto-reconciler). |
| 2026-05-15 | 0.4.7 | agent (imrest/hk-iuaed.2) | **PL-006 — Stale `in_progress` bead marker reset (sixth orphan-sweep bullet).** Adds a new `Stale in_progress bead markers` bullet after `Stale reconciliation locks` in §4.5 PL-006. The daemon MUST enumerate `in_progress` beads via `br list --status in_progress --format json`, filter to those authored by this project's daemon (provenance via `actor` field carrying `project_hash` per PL-006a, or cross-referenced against `claim` op intent-log entries), then apply three ordered exclusions: (a) live run reattached in the PL-005 step-7 in-memory model rebuild; (b) pending `close`/`reopen` intent file present (Cat 3a owns it); (c) merge commit on target branch bears `Harmonik-Bead-ID: <bead_id>` (Cat 3c owns it). Beads satisfying none of the exclusions MUST be reset via the §4.8 BI adapter BI-010d op (`in_progress → open`), idempotency-keyed as `<project_hash>:<bead_id>:reset:<daemon_start_ns>` and intent-logged per BI-030. **Event payload extension.** The `daemon_orphan_sweep_completed` event (§8.7.14) gains a `bead_in_progress_reset` count field (number of beads reset by this sweep); declared as an additive payload extension consistent with the `tmux_windows_killed` / `tmux_kill_window_survivors` precedent of PL-021c; consumers MUST tolerate the new integer field per event-model.md §6.3 N-1 compatibility. Cross-spec coordination: if [event-model.md §8.7.14] requires a schema bump rather than additive tolerance, hk-iuaed.5 is already filed to address it. Cross-references: [beads-integration.md §4.8 BI-010d] for the `reset` op semantics and idempotency-key formula; [beads-integration.md §4.10 BI-030] for intent-log discipline. Refs: hk-iuaed.2, codename imrest. |
| 2026-05-14 | 0.4.6 | agent (extqueue) | **External queue (extqueue v0.1) amendments.** Adds the v0.1 queue surface to PL alongside the new `queue-model.md` foundation spec. **PL-003a method-set extension.** CLI-facing JSON-RPC inventory adds `queue-submit`, `queue-append`, `queue-status`, `queue-dry-run` (payload schemas owned by [queue-model.md §6, §7]; validation errors `-32010..-32019` owned by queue-model and ON §8); the `enqueue` method is RETIRED in this revision — PL-003a is the single authoritative method-name registry so removal here is sufficient, no alias row, no compatibility shim. The append-only / submit-then-replace shape is the v0.1 surface; `queue-remove` / `queue-pause` / `queue-resume` / `queue-clear` (sketched in 02-components.md §4) are deferred to v0.2 per [queue-model.md §8] pause-by-failure recovery. **PL-028 subcommand amendments.** The `harmonik enqueue` bullet is REMOVED. Four new bullets in alphabetical order: `hk queue append`, `hk queue dry-run`, `hk queue status`, `hk queue submit`, each naming purpose, JSON-RPC method, payload-schema owner, and exit-code 17 (`multi-daemon-target-missing`) on daemon-not-running; the `hk queue status` vs `harmonik status` namespacing disambiguation is noted inline. **PL-028c (new).** Pre-`flag.Parse` dispatch obligation for the `hk queue` family in `cmd/harmonik/main.go`, mirroring the `tmux-start` and `hook-relay` precedent; verb-recognition reads `os.Args[2]`; both `--flag value` and `--flag=value` shapes supported per verb; unrecognized verb exits with code 2 and stderr names the v0.1 verb set; daemon-not-running path uniform across verbs at exit-code 17. **PL-005 step 8a — third marker.** Adds `.harmonik/queue.json` (per [queue-model.md §3 QM-002]) to the durable-startup-markers read alongside `daemon.state` and `daemon.upgrading`: recognized schema_version loads the persisted queue with per-group / per-item statuses honored (including `paused-by-failure` / `paused-by-drain`); forward-incompatible schema_version refuses startup with code 2 (`queue-format-unsupported`, reused for queue.json schema mismatch) and emits `daemon_startup_failed{failure_mode="queue-format-unsupported"}`; corrupt file treated as absent with structured-log warning per ON-035, daemon proceeds with no in-memory queue awaiting a subsequent `queue-submit`. The "Both markers absent" bullet is generalized to "All markers absent." **PL-013 RETIRED.** Body replaced with a normative-deletion stub. The Beads-ready-poll dispatch model that PL-013 protected (sleep + wake on `harmonik enqueue` or periodic Beads-store re-query) is wholly superseded by extqueue: the daemon's dispatch input is the in-memory queue (loaded from `.harmonik/queue.json` at step 8a or arriving via `queue-submit`); the re-query cadence knob is removed from the ON-004 config inventory. The load-bearing "MUST NOT exit on queue-empty / queue-absent / queue-completed" obligation survives — only the Beads-poll mechanism and the cadence knob are gone. Conformance §10.1 notes PL-013 is no longer a Core MVH gate. **PL-004 file-surface inventory** adds `.harmonik/queue.json` (queue-manager-written via WM-026 atomic write, PL-read at PL-005 step 8a, unlinked on queue completion per QM-003) between `.harmonik/daemon.state` and the event-log entries. **PL-027(iii) non-regression clarifier.** A NON-REGRESSION NOTE block confirms that queue methods ride the same listener fd as `claim-next` / `emit-outcome` and inherit the listener-fd-passing protocol of PL-027(iii) unchanged; the in-memory queue is reconstructed post-exec via the step-8a `.harmonik/queue.json` read; no second listener, no second fd, no second adoption path. **PL-008a code-17 entry.** Code 17 (`multi-daemon-target-missing`) is now an explicit member of PL's consumed-codes list, with a parenthetical noting the single-project `ECONNREFUSED` remediation prose extension is owned by [operator-nfr.md §4.1 ON-004] (cascade — not PL-owned). **§8 taxonomy** restated to surface code 17's role in the `hk queue *` surfaces. **§9.1 depends-on** adds `queue-model` to the front-matter list; new bullet for `[queue-model.md §2, §3, §6, §7, §8]` covering document schema, persistence layout, JSON-RPC payloads, validation errors, and pause-by-failure recovery; new bullet for `[workspace-model.md §4.7 WM-026]` (already cited elsewhere but called out as the queue-write disciplinary anchor). **§9.2 reverse dependencies** notes queue-model now reverse-depends on PL. **§9.3 co-references** ON entry expanded to include ON-004 for the configuration-knob inventory (PL-013 cadence removal) and the code-17 remediation prose extension. **§10.2 test obligations** extended: PL-003a method-registry test asserts the four queue method names are registered and `enqueue` is NOT; PL-005 step-8a queue.json tests cover recognized schema_version load (including paused statuses), forward-incompatible schema_version refusal with code 14, and corrupt-file warning + no-queue-loaded proceeding; PL-008a tests code 17; PL-013 has no test obligation (retired; PL-011 / PL-012 cover the surviving idle-no-exit obligation); PL-027 test asserts gap-free fd adoption applies to in-flight queue-method invocations; PL-028 / PL-028c tests cover pre-`flag.Parse` verb dispatch, both flag shapes, unrecognized-verb exit code 2, and daemon-down exit code 17 for all four verbs. **§10.3 excluded conformance claims** adds the queue document schema / payload / persistence / validation surface as queue-model-owned. **§4.a envelope (g) NFRs inherited** explicitly notes the ON-004 entry now reflects the PL-013 cadence-knob removal. **PL-019 prose** updated: the orchestrator-agent example CLI call is `hk queue submit` instead of `harmonik enqueue`. **PL-003 prose** updated: the example client list cites `hk queue submit` instead of `harmonik enqueue`. No requirement IDs renumbered; PL-013's ID is retained as a retirement stub for traceability. All other PL requirements unchanged. Cross-spec coordination requests: ON must add code-17 remediation prose extension per [operator-nfr.md §4.1 ON-004] (single-project `ECONNREFUSED` → `harmonik daemon` rather than `harmonik list`); ON must remove the prior re-query cadence knob from the ON-004 configuration-knob inventory; queue-model.md (new spec) must finalize §2 / §3 / §6 / §7 schemas before PL conformance tests can be authored. Refs: extqueue v0.1 design pass (.kerf/projects/gregberns-harmonik/extqueue/04-design/process-lifecycle-design.md). |
| 2026-05-13 | 0.4.5 | agent (hk-ultyu) | **Daemon→pane write mechanism (PL-021d, new, §4.7).** Fills the symmetric gap left by PL-021b §5 (which forbids `pipe-pane` reads but left the write direction unspecified). **PL-021d** permits `tmux load-buffer -b <name> <file>` + `tmux paste-buffer -b <name> -t <pane>` as the daemon→pane write mechanism; `send-keys -l` is allowed as a fallback for payloads ≤ 512 bytes with no newlines; bare `send-keys` (without `-l`) is FORBIDDEN. Buffer-name discipline: `harmonik-<session-id>-<purpose>` (deterministic per session, globally unique, slug-named purpose e.g. `task`, `phase-msg`, `feedback`). Cleanup obligation: `tmux delete-buffer` immediately after paste; failure logged at DEBUG and ignored. Justification added for why this is not equivalent to `pipe-pane`: writes route through the TTY input path (same as operator paste), not a kernel-level pipe on the output side. Structured-log audit: `daemon_pane_write` event at INFO with `session_id`, `pane_target`, `buffer_name`, `purpose`, `payload_bytes`. Refs: hk-ultyu, docs/claude-session-comms-audit-2026-05-13.md §6 B2. |
| 2026-05-13 | 0.4.4 | agent (hk-gql20.25) | **Bridge-integration spec review findings (MEDIUM + MINOR).** **PL-021c §6** (new): post-ceiling survivor handling — if subprocesses are still alive after the 2-second `tmux kill-window` poll ceiling, the daemon MUST log a structured WARN with key `tmux_kill_window_survivor` naming each surviving PID and include a new `tmux_kill_window_survivors []int` field in the `daemon_orphan_sweep_completed` event payload; no SIGKILL escalation at MVH (operator session is not harmonik's to mass-kill). Cross-spec coordination: [event-model.md §8.7] `daemon_orphan_sweep_completed` payload requires `tmux_kill_window_survivors []int` addition. **PL-021b §8** (new): window-name observability — the daemon MUST emit the resolved `tmux_window_name` as a field of the `agent_started` progress-stream message so that the determinism asserted in PL-021b §4 is externally observable. Cross-spec coordination: [event-model.md §6.3] `agent_started` payload requires new optional `tmux_window_name string` field. **OQ-PL-015** (new): deferred OQ for operator-facing window-name surface (e.g., `hk attach --list-windows`); cross-ref PL-021b §4, §8, WM-002a. No existing clauses revised. Refs: hk-gql20.25. |
| 2026-05-13 | 0.4.3 | bridge-integration | **Direct-tmux substrate + tmux-start amendments (hk-gql20.1).** Additive amendments for the direct-tmux substrate at MVH. **PL-021b** (new, §4.7) — direct-tmux substrate obligations: pane creation via `tmux new-window`, tmux availability probe at PL-005 step 4 with ON §8 code 22 on failure, session resolution distinguishing `$TMUX`-set ($TMUX-reuse with `hk-<hash6>-` sentinel) vs unset (refuse with code 24), deterministic window naming per WM-002a, no pane-output consumption (bridge wire is the Unix socket per PL-003a / CHB-010), substrate seam threaded via adapter registry preserving CHB-022 twin-blindness, wait/kill via `tmux list-panes` polling / `tmux kill-window`. **PL-021c** (new, §4.7) — window-level orphan sweep keyed on `hk-<hash6>-` prefix, 100ms/2s polling, `tmux_windows_killed` payload field. **PL-028 refinement** (§4.10) — `hk tmux-start` subcommand: $TMUX guard, ensure-session, syscall.Exec into attach, exit codes 0/22/24. **PL-028b** (new, §4.10) — daemon refusal when `$TMUX` unset (exit 24, no pidfile/socket bind). Cross-spec coordination: ON §8 to absorb code 24 (`tmux-session-unavailable`, declared PL-INTERIM); EV §8.7 `daemon_orphan_sweep_completed` payload to add `tmux_windows_killed`; WM-002a adds deterministic window-naming clause; HC-054/055/056/057 add Attach pty contract, claude flag allow-list, agent_ready timeout, heartbeat ownership. No existing PL IDs renumbered. Status remains `reviewed`. |
| 2026-05-12 | 0.4.2 | foundation-author | Add PL-017a in §4.5 (gap-filler after PL-017, avoiding collision with existing PL-018 "Daemon is a deterministic Go binary" in §4.6) clarifying that hook-bridge relay subprocesses spawned by an agent subprocess are grandchildren of the daemon and not subject to PL-014, PL-014a, or PL-006. Companion to [claude-hook-bridge.md] new spec. No prior IDs renumbered. Status remains `reviewed`. |
| 2026-04-25 | 0.4.1 | foundation-author | Cross-spec coordination patch wave landing the 9 items filed against PL by the four R2 integrations of 2026-04-24 (ON, RC, BI, EV). Status remains `reviewed`; all PL IDs FROZEN; no renumbering. See prior revision history for itemized list. |
| 2026-04-24 | 0.4.0 | foundation-author | R2 integration pass (skeptic + crash-adversary + daemon-author). See prior revision history for full detail. |
| 2026-04-24 | 0.3.0 | foundation-author | R1 integration pass (implementer + cross-spec-architect + critic). See prior revision history for full detail. |
| 2026-04-24 | 0.2.0 | foundation-author | Corpus-wide cleanup pass (no semantic changes); architecture.md citation anchor migration. |
| 2026-04-23 | 0.1.0 | foundation-author | Initial draft migrated from [docs/foundation/components.md §8] per spec-template 1.1. |
