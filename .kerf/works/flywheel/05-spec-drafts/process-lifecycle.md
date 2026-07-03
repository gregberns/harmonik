# Process-lifecycle.md — flywheel change-set (draft)

> Spec-draft for `specs/process-lifecycle.md` additions/changes required by the flywheel design. Source: spec-draft sub-agent (sonnet), 2026-05-30. Three changes: (1) PL-019 promotion to a normative cognition-process spec; (2) PL-006d orphan-sweep exclusion for coordinator/orchestrator sessions (addresses hk-hc3qq); (3) PL-028 command-surface addition for `harmonik supervise` + `harmonik digest`.

## Change-set summary

Three targeted amendments. (1) PL-019 is promoted from an informative "optional/post-MVH" stub to a normative section specifying the orchestrator/cognition process as a separately-managed, lock-guarded process spawned by `harmonik supervise start` — decoupled from the daemon's lifecycle. (2) A new PL-006-series clause (PL-006d) carves coordinator/orchestrator tmux sessions out of the orphan sweep's kill list, directly addressing bead hk-hc3qq (the daemon-restart-kills-flywheel bug). (3) PL-028 gains two new command bullets enumerating `harmonik supervise` and `harmonik digest`, consistent with the existing `hk queue *` pattern.

---

### Change 1: PL-019 — Promote orchestrator-agent from informative stub to normative cognition-process spec

**Before (existing PL-019, §4.6):**
```
#### PL-019 — Orchestrator-agent is a separate Claude Code session
An orchestrator-agent MUST be a separate Claude Code session sitting on top of the
daemon. It MUST interact with the daemon through the CLI ... The orchestrator-agent
is OPTIONAL in MVH ... post-MVH.
Tags: cognition
> INFORMATIVE: ...
```

**After (revised PL-019, §4.6):**
```
#### PL-019 — Orchestrator/cognition process is a separately-managed long-running process

(a) Process separation. The orchestrator/cognition process (hereafter "supervisor
process") MUST run as a separate OS process from the daemon. It MUST NOT run as a
thread, goroutine, or sub-module within the daemon binary; it MUST have its own
PID. The daemon MUST remain LLM-free per PL-018; the supervisor process is the
sole cognition-bearing layer and MAY call an LLM or host an agent session. The
supervisor process MUST interact with the daemon exclusively through the daemon's
CLI and socket surface (§PL-003a, §PL-028): `hk queue submit`, `harmonik status`,
`harmonik digest`, `kerf next`, priority triage, and backlog grooming are its
normative interaction points.

(b) Spawn surface. The supervisor process MUST be started via `harmonik supervise
start` (§PL-028, PL-028d). The daemon MUST NOT start, stop, or monitor the
supervisor process; their lifecycles are explicitly separated. `harmonik supervise
start` MUST refuse with exit code 17 (`multi-daemon-target-missing` per ON §8 /
PL-008a) if the daemon's socket at `.harmonik/daemon.sock` is not reachable
(ECONNREFUSED). `harmonik supervise start` MUST NOT attempt to start the daemon
on behalf of the operator.

(c) Singleton lock. Exactly one supervisor process per project MUST be permitted
at any instant. `harmonik supervise start` MUST acquire a fd-lifetime advisory
lock on `.harmonik/cognition/supervisor.lock` via `flock(LOCK_EX|LOCK_NB)` (same
primitive as PL-002a). On `EWOULDBLOCK` the command MUST exit with ON §8 code 25
(`supervisor-already-running`, PL-INTERIM pending ON absorption) and MUST print
the path of the running supervisor's pidfile to stderr. The lock MUST be held for
the lifetime of the supervisor process or its watch-shim wrapper; the kernel
releases it on process exit whether clean or crash.

(d) Pidfile. After acquiring the lock, the supervisor process (or the watch-shim
on its behalf) MUST write the inner process's PID to
`.harmonik/cognition/supervisor.pid` following PL-002b discipline (single ASCII
decimal PID line terminated by `\n`). `harmonik supervise status` MUST read this
file to probe liveness via `kill(pid, 0)`.

(e) Config file. `harmonik supervise start` MUST atomically write a configuration
snapshot to `.harmonik/cognition/config.json` (temp+rename+fsync per WM-026)
before launching. The file MUST carry a `schema_version` integer (N-1-readable
per ON-018); `daemon_instance_id` from the running daemon's pidfile (line 3 per
PL-002b) MUST be recorded so the supervisor process can validate it is talking
to the same daemon instance. The supervisor MUST re-read config at startup; it
MUST NOT hot-reload mid-run (parameter changes require `harmonik supervise
restart`).

(f) Tmux pane and crash posture. `harmonik supervise start` MUST create a tmux
session named `harmonik-<project_hash>-flywheel` per PL-006a, using `tmux
new-session -d -s <name>` when the session does not already exist. The pane MUST
be created with `set-option remain-on-exit on` so a supervisor crash leaves the
pane visible for operator inspection. This session name carries the
`harmonik-<project_hash>-` prefix and is therefore within the orphan-sweep
namespace; the exclusion clause of PL-006d MUST be honored by the daemon's orphan
sweep to prevent inadvertent kills on daemon restart.
When `--watch-restart` is supplied, the command MUST interpose a lightweight
wrapper shim between the lock-holder and the supervisor process: the shim holds
the advisory lock fd for the duration, monitors the supervisor's PID via
`wait(2)`, and respawns on non-zero exit. Without `--watch-restart`, pane
`remain-on-exit` leaves the window open for the operator to issue `harmonik
supervise restart` manually.

(g) File surface. The supervisor process's per-project files reside under
`.harmonik/cognition/`:
  - `supervisor.lock` — fd-lifetime advisory lock (item c).
  - `supervisor.pid` — inner process PID (item d).
  - `config.json` — atomic config snapshot (item e).
  - `heartbeat.json` — periodic: `{schema_version, pid, uptime_s,
    context_fullness_pct, active_beads, pending_beads, written_at}`. Read by
    `harmonik supervise status`. The daemon MUST NOT read or write this file.
  - `watermark.json` — consistency watermark owned by the supervisor per
    [cognition-loop.md §CL-consistency]. The daemon MUST NOT read or write.
  - `notes.jsonl` — append-only durable notes log owned by the supervisor per
    [cognition-loop.md §CL-digest]. The daemon MUST NOT read or write.
The daemon's PL-004 file-surface inventory MUST NOT enumerate files under
`.harmonik/cognition/`; that subtree is the supervisor process's exclusive domain.
The daemon MAY assert at startup that `.harmonik/cognition/` is absent-or-empty
as a Cat 0 pre-check carve-out for an incompatible upgrade — but MUST NOT remove
cognition files during the orphan sweep (see PL-006d).

(h) Relationship to PL-018. This requirement is an extension of the
daemon-vs-orchestrator-agent distinction established by PL-018 and PL-019's
original framing. The daemon remains a deterministic Go binary with no LLM calls.
The supervisor process is the sole cognition-bearing layer and is explicitly
out-of-process.

Tags: cognition
> NOTE: The conformance profile for PL-019 is post-MVH: an implementation MAY
> conform to Core MVH without ever running a supervisor process. §10.1 retains
> the "Post-MVH" carve-out. The normative text above is the stable contract for
> any post-MVH implementation; its presence here (rather than only in
> cognition-loop.md) is because the daemon-side obligations — refusing to start
> the supervisor, honoring the orphan-sweep exclusion, and keeping the
> `.harmonik/cognition/` subtree unowned — are daemon-lifecycle requirements.
```

---

### Change 2: PL-006d — Orphan-sweep exclusion for coordinator/orchestrator tmux sessions

**Insert after PL-006c (currently last PL-006-series clause), before PL-007:**

```
#### PL-006d — Orphan-sweep exclusion for coordinator/orchestrator tmux sessions

The orphan sweep of §PL-006 MUST NOT kill tmux sessions or windows that are
actively owned by a live supervisor process per §PL-019. Without this exclusion,
a daemon restart's sweep would inadvertently kill the flywheel pane (session name
`harmonik-<project_hash>-flywheel`), terminating the cognition loop mid-cycle.
This defect is tracked as hk-hc3qq.

Exclusion mechanism (sentinel-file approach). When `harmonik supervise start`
creates the supervisor tmux session, it MUST write a sentinel file at
`.harmonik/cognition/supervisor.sentinel` (content: `schema_version=1\n`) before
issuing `tmux new-session`. The sentinel file MUST be removed by `harmonik
supervise stop` or by the watch-shim on clean exit; it survives a supervisor
crash and is re-written on `harmonik supervise restart`.

The orphan sweep MUST check for the sentinel BEFORE `tmux kill-session` against
any session matching the `harmonik-<project_hash>-` prefix:
  1. For each candidate session whose name matches the prefix, probe
     `.harmonik/cognition/supervisor.sentinel` via `stat(2)`.
  2. If the sentinel is present AND `kill(supervisor_pid, 0) == 0` for the PID
     in `.harmonik/cognition/supervisor.pid`, the session MUST be SKIPPED. The
     daemon MUST emit a structured-log entry at INFO with key
     `orphan_sweep_skipped_coordinator_session` naming the session.
  3. If the sentinel is absent OR the sentinel is present but the supervisor PID
     is no longer live (stale sentinel from a prior crash), the session is
     treated as an ordinary orphan and killed per the normal PL-006 path. The
     sweep MUST remove the stale sentinel via `unlink` +
     `fsync(parent_directory_fd)`.

Alternative (noted for record, not adopted). The supervisor session could be
named OUTSIDE the `harmonik-<project_hash>-` scope (e.g.,
`harmonik-flywheel-<project_hash>`). Rejected because (a) it breaks PL-006a's
ALL-harmonik-owned-tmux-resources-carry-the-prefix invariant, and (b) it hides
the session from operator tooling that enumerates harmonik-session-prefixed
panes for attach/status. Sentinel approach preserves the convention and keeps
the exclusion logic explicit and auditable.

Impact on `daemon_orphan_sweep_completed` event. The payload MUST gain a new
integer field `coordinator_sessions_skipped: <integer ≥ 0>`. Additive payload
extension consistent with PL-021c precedent; consumers MUST tolerate unknown
integer fields per [event-model.md §6.3] N-1 compatibility.

Cross-spec coordination: [event-model.md §8.7.14] `daemon_orphan_sweep_completed`
payload schema requires the `coordinator_sessions_skipped` field addition.

Tags: mechanism
Axes: llm-freedom=none; io-determinism=deterministic; replay-safety=safe; idempotency=idempotent
Refs: hk-hc3qq (orphan sweep kills flywheel pane on daemon restart).
```

---

### Change 3: PL-028 — Add `harmonik supervise` and `harmonik digest` to the command surface

**Insert two new bullets in PL-028 (§4.10), after `hk tmux-start`:**

```
- harmonik supervise <verb> [flags] — manage the supervisor/cognition process per
  §PL-019. v1 verbs: start, stop, status, attach, restart, logs. All verbs other
  than status and logs read or write `.harmonik/cognition/`; none routes over the
  daemon socket. The attach verb MUST `execve` into
  `tmux attach-session -t harmonik-<project_hash>-flywheel`, replacing the
  `harmonik supervise attach` process (mirrors `hk tmux-start` step iv of
  PL-028 refinement). Detailed contract per PL-028d. Exit codes: 25 on `start`
  when the singleton lock is held; 17 on `start` when the daemon socket is
  unreachable; 0 on successful start with `--detach`.

- harmonik digest [--project <dir>] [--watch] [--json] [--since <event_id>] —
  compute and display the current cognition digest (running/done/failed counts,
  stalled runs, queue depth, open notes, context-fullness if supervisor is live).
  Derived from events.jsonl + queue.json + .harmonik/cognition/heartbeat.json.
  Snapshot mode (no --watch) MUST operate without a running daemon and MUST NOT
  connect to the daemon socket. --watch MAY connect to the socket for live event
  streaming (gracefully degrading to file-poll when absent). --since restricts
  the window to entries after the given UUIDv7 (ScanAfter semantics per
  event-model.md §4.1). --json emits NDJSON. Exit 17 applies only when --watch
  requires the socket and the daemon is not running. Snapshot mode MUST exit 0
  when the daemon is stopped; a missing .harmonik/ is the sole Cat 0 failure
  (exit 7).
```

**New PL-028d sub-clause (insert after PL-028c):**

```
#### PL-028d — `harmonik supervise` subcommand-family verb contract

The `harmonik supervise` family MUST be dispatched in `cmd/harmonik/main.go`
before `flag.Parse` is called on the global flag set, mirroring the `tmux-start`,
`hook-relay`, and `hk queue` precedents of PL-028c. Dispatch MUST recognize
`os.Args[1] == "supervise"` and read `os.Args[2]` as the verb. Unrecognized verbs
MUST exit code 2 (`usage-error` per ON §8) and print a one-line stderr message
naming the v1 verb set. Each verb routes through a dedicated package (suggested
`internal/supervise/cli`) returning an int exit code.

Verb obligations:
- start [--model <id>] [--token-cap <n>] [--max-concurrent <n>]
  [--budget-cap <usd_per_day>] [--instructions <path>]
  [--priority-source kerf|beads|file:<path>] [--areas <list>]
  [--epic <codename>] [--detach] [--watch-restart] — acquire singleton lock,
  write config, create tmux session with `remain-on-exit on`, start supervisor.
  --detach returns to shell immediately. --watch-restart wraps the supervisor in
  a shim that respawns on non-zero exit. MUST refuse (17) if daemon socket
  unreachable. MUST refuse (25) if singleton lock held.
- stop [--timeout <duration, default 30s>] — read supervisor pid, send SIGTERM,
  wait up to --timeout, send SIGKILL if not exited, release lock, optionally
  `tmux kill-session`. Mirrors PL-011 SIGTERM+bounded-wait+SIGKILL discipline.
- status [--json] — read supervisor.pid + config.json + heartbeat.json; probe
  via kill(pid,0). Report pid, running, uptime, heartbeat age,
  context_fullness_pct, active/pending task counts. File-surface only.
- attach — `execve tmux attach-session -t harmonik-<project_hash>-flywheel`.
  MUST NOT kill the loop.
- restart — `stop` then `start` reading existing config.json (parameters
  preserved). MUST refuse (17) if daemon socket unreachable at `start`.
- logs [--lines <n, default 200>] [--follow] — `tmux capture-pane -p -S -<n>`
  for snapshot; --follow wraps `tmux pipe-pane` to a temp file. The pane IS the
  log surface at v1.

The daemon-not-running path for start/restart MUST be: socket-probe
`ECONNREFUSED` against `.harmonik/daemon.sock` → exit 17, stderr directive
`"daemon not running; start with: harmonik daemon"`.

Tags: mechanism
Axes: llm-freedom=mechanical; io-determinism=deterministic; replay-safety=replay-safe; idempotency=idempotent
```

---

### Supporting additions

**§3 Glossary entries (insert alphabetically):**
```
- supervisor process — the per-project cognition-bearing process launched by
  `harmonik supervise start`. A separate OS process from the daemon; owns the
  `.harmonik/cognition/` subtree; holds a singleton flock at
  `.harmonik/cognition/supervisor.lock`. Post-MVH; see §PL-019.
- cognition subtree — the directory `.harmonik/cognition/` exclusively owned
  by the supervisor process. The daemon MUST NOT read or write files under
  this path except to probe the sentinel file during the orphan sweep per
  PL-006d.
- coordinator sentinel — the file `.harmonik/cognition/supervisor.sentinel`
  that signals to the orphan sweep that the
  `harmonik-<project_hash>-flywheel` tmux session is actively supervised and
  MUST NOT be killed per PL-006d. Refs: hk-hc3qq.
```

**§4.a PL-ENV-001 (b) Events consumed:**
```
- supervisor_started, supervisor_stopped — NOT consumed by the daemon; these
  are supervisor-process-emitted events owned by [cognition-loop.md]. Listed
  here as an exclusion to confirm the daemon does not consume or react to
  supervisor lifecycle events.
```

**§4.a PL-ENV-001 (e) State owned:**
```
- .harmonik/cognition/supervisor.sentinel (read-only probe by orphan sweep
  per PL-006d; written and owned by `harmonik supervise start`/`stop`).
```

**§4 PL-004 file-surface inventory (after `.harmonik/daemon.state`):**
```
The `.harmonik/cognition/` subtree is NOT part of the daemon's file-surface
inventory; it is exclusively owned by the supervisor process per §PL-019(g).
The daemon MUST NOT enumerate, read, or write files under `.harmonik/cognition/`
except (i) reading `.harmonik/cognition/supervisor.sentinel` and
`.harmonik/cognition/supervisor.pid` during the orphan sweep per PL-006d, and
(ii) reading `.harmonik/cognition/supervisor.pid` as a probe when resolving
whether a supervisor is active.
```

**§9.1 Depends on additions:**
```
- [cognition-loop.md §CL-consistency, §CL-digest, §CL-operator] — watermark,
  notes, and operator-control obligations owned by the cognition-loop spec
  (not yet finalized; cross-referenced here for completeness when that spec
  lands).
```

**§10.1 conformance profile update (append to Post-MVH):**
```
Supervisor process integration (PL-019 items b–h, PL-006d, PL-028d) is
post-MVH. An implementation MAY conform to Core MVH without implementing
`harmonik supervise` or `harmonik digest`; PL-006d's sentinel-probe path MUST
still be present in the orphan sweep even when no supervisor has ever run
(degrades gracefully to "sentinel absent → kill as normal").
```

**§12 revision history entry (template):**
```
| 2026-05-30 | 0.5.0 | agent (flywheel spec-draft) | PL-019 promotion + PL-006d
+ PL-028d/digest additions (flywheel design). (1) PL-019 expanded from an
informative post-MVH stub to a full normative section (a–h) specifying the
supervisor/cognition process; daemon-side obligations: refuse to start, do NOT
read/write cognition subtree beyond sentinel probe. Clarifies PL-018
(daemon determinism) unaffected. (2) PL-006d: orphan-sweep exclusion for
coordinator sessions via sentinel file; stale-sentinel → kill + remove
sentinel; adds `coordinator_sessions_skipped` to `daemon_orphan_sweep_completed`.
Refs: hk-hc3qq. (3) PL-028 gains `harmonik supervise` and `harmonik digest`
bullets. New PL-028d: full verb contract, pre-flag.Parse dispatch, exit codes
17/25/0. Glossary gains supervisor process, cognition subtree, coordinator
sentinel. PL-004 file-surface inventory adds cognition-subtree exclusion.
§4.a envelope adds sentinel-probe + supervisor-event exclusion. §10.1
conformance profile extended. |
```
