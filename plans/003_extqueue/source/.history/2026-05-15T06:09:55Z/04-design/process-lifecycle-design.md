# process-lifecycle — Change Design

## Current state

`specs/process-lifecycle.md` v0.4.1 declares the daemon's socket, CLI surface, and startup/shutdown order. Three sites interact with extqueue:

**PL-003a method-set enumeration** (`process-lifecycle.md:194`, verbatim):

> The JSON-RPC method names exposed on the daemon socket are: agent-facing (`claim-next`, `emit-outcome`, `dispatch-status`, Beads-CLI-skill proxy methods per [beads-integration.md §4.9 BI-027]); CLI-facing (`status`, `pause`, `resume`, `stop`, `upgrade`, `attach`, `enqueue`, `list` per [operator-nfr.md §4.10 ON-041]); daemon-internal / introspection (`get-agent-count` …).

**PL-028 subcommand bullets** (`process-lifecycle.md:677–685`, condensed):

- `harmonik daemon` — start the daemon headless.
- `harmonik attach` — open observability TUI over socket.
- `harmonik runner` — solo-dev convenience.
- `harmonik enqueue` — enqueue a bead via socket. Method: JSON-RPC `enqueue`; payload schema owned by [beads-integration.md §4.4]. *(name-only — no live handler per research Q3)*
- `harmonik status` — daemon status.
- `harmonik pause` / `stop` / `upgrade` / `hk tmux-start` — operator commands.

**PL-013 queue-empty wording** (`process-lifecycle.md:410`, verbatim):

> When all beads are closed or deferred and nothing is in-flight, the daemon MUST sleep (suspend CPU consumption) and wait for a subsequent `harmonik enqueue` or for external changes to the Beads store (periodically re-queried at a configurable cadence). The daemon MUST NOT exit on queue-empty.

The rationale block names the re-query cadence as the load-bearing knob.

**PL-005 step 8a** (`process-lifecycle.md:241–244`) reads `.harmonik/daemon.state` and `.harmonik/daemon.upgrading`, with "marker file unreadable / corrupt → treat as absent + structured-log warning per ON-035".

**PL-027(iii)** (`process-lifecycle.md:658`) defines listener-fd passing across `execve` (`FD_CLOEXEC` cleared, `HARMONIK_LISTENER_FD` env, `net.FileListener`).

**PL-004 file-surface inventory** (`process-lifecycle.md:208`) enumerates every `.harmonik/`-owned file the daemon may touch.

## Target state

### PL-003a — method-set amendment

Replace the CLI-facing list in the verbatim enumeration. Remove `enqueue`. Add four v0.1 queue methods, bare-kebab, mirroring `claim-next` / `emit-outcome` style:

> CLI-facing (`status`, `pause`, `resume`, `stop`, `upgrade`, `attach`, `list` per [operator-nfr.md §4.10 ON-041]; plus the v0.1 queue method set: `queue-submit`, `queue-append`, `queue-status`, `queue-dry-run` per [queue-model.md §6]).

Rationale-paragraph addendum after the method list: "Queue method payload schemas and validation error codes (`-32010..-32019`) are owned by [queue-model.md §6] and [operator-nfr.md §8]; PL-003a fixes only the wire names. Append-only at v0.1; the remove/pause/resume/clear methods sketched in `02-components.md §4` are deferred to v0.2 per [queue-model.md §8] pause-by-failure recovery."

The `enqueue` retire is total: no alias row, no compatibility shim. PL-003a is the only authoritative method-name registry, so removal here is sufficient; cascade edits to ON-013a, ON-041 (operator-nfr §4.10), and PL-028b conformance tests are tracked by operator-nfr design pass.

### PL-028 — subcommand amendments

Remove the existing `harmonik enqueue` bullet at line 680 entirely. Add four bullets in the same one-line style (purpose, JSON-RPC method, payload-schema owner) under PL-028 in stable insertion order (alphabetical by verb):

- **`hk queue append <group-index> <bead-id ...>`** — append items to a stream group via the socket (§PL-003, §PL-003a). Method: JSON-RPC `queue-append`; payload schema owned by [queue-model.md §7]. Validation errors per QM-020..QM-024 surface as JSON-RPC error codes `-32010..-32014`.
- **`hk queue dry-run <queue-file>`** — submit-validate a queue file without mutating state; returns the resolved plan including ledger-dep parallelism narrowing. Method: JSON-RPC `queue-dry-run`; payload schema owned by [queue-model.md §6].
- **`hk queue status [--queue-id <uuid>]`** — report the daemon's current queue (groups, item statuses, queue-level `status` per [queue-model.md §2]). Method: JSON-RPC `queue-status`; output shape owned by [queue-model.md §2].
- **`hk queue submit <queue-file>`** — submit a queue JSON document to the daemon. Method: JSON-RPC `queue-submit`; payload schema owned by [queue-model.md §2, §6]. Mints `queue_id` on accept and persists `.harmonik/queue.json` per QM-001.

Add a one-line disambiguation note under the new bullets: "`hk queue status` is namespaced and distinct from `harmonik status` (PL-028 above); the former reports queue state, the latter daemon state."

### PL-028 sub-subsection — `hk queue` argument parsing

New small subsection after PL-028b, before PL-029 (if present), titled **"PL-028c — `hk queue` subcommand-family pre-flag.Parse dispatch"**:

> The `hk queue` family MUST be dispatched in `cmd/harmonik/main.go` before `flag.Parse` is called on the global flag set, mirroring the `tmux-start` and `hook-relay` precedent of `cmd/harmonik/main.go:70-115`. The dispatch recognizes `os.Args[1] == "queue"` and reads `os.Args[2]` as the verb (`submit`, `append`, `status`, `dry-run`). Each verb owns a hand-rolled flag scan accepting both `--flag value` and `--flag=value` shapes. Unrecognized verbs MUST exit code 2 (`usage-error` per ON §8) with a one-line stderr message naming the v0.1 verb set. Each verb routes through a dedicated package (suggested: `internal/queue/cli`) returning an int exit code, consistent with `tmux.RunTmuxStart` / `hookrelay.Run` patterns.

The daemon-not-running path is uniform across verbs: socket-probe `ECONNREFUSED` → exit code **17** (`multi-daemon-target-missing` per ON §8 / operator-nfr.md:800). Decision rationale in §"Exit code 17 reuse" below.

### PL-005 step 8a — queue.json read

Amend step 8a to add a third marker. Insert after the existing `daemon.upgrading` and `daemon.state` bullets, before the "Both markers absent" bullet:

> - **`.harmonik/queue.json` present.** Read the persisted queue document per [queue-model.md §3 QM-002]. If the file parses and its `schema_version` is recognized, the in-memory queue is loaded with its persisted `status` and per-group/per-item statuses preserved. `paused-by-failure` and `paused-by-drain` queue statuses MUST be honored — no dispatch advances after step 9. If `schema_version` is unrecognized (forward-incompatible) the daemon MUST refuse startup with ON §8 code 14 (`upgrade-hash-mismatch`'s sibling case; specific allocation tracked by operator-nfr) and emit `daemon_startup_failed{failure_mode="queue-schema-incompatible"}`. The file MUST be absent in the unlinked-on-completion case per QM-003.

Update the existing "Marker file unreadable / corrupt" bullet to extend its enumeration: "Applies equally to `daemon.state`, `daemon.upgrading`, **and `queue.json`**: treat as absent; emit a structured-log warning per [operator-nfr.md §4.9 ON-035] naming the file. Do not block startup. For `queue.json` specifically, the daemon proceeds with no in-memory queue (waiting for a subsequent `queue-submit`)."

### PL-013 — queue-empty wording reconciliation

PL-013's premise — that the daemon sleeps and wakes on `harmonik enqueue` or Beads-store changes — is wholly superseded by extqueue. Under the new model the daemon's dispatch input is the in-memory queue (loaded from `.harmonik/queue.json` at step 8a or arriving via `queue-submit`); it has no Beads-ready-poll loop and no enqueue wake-up channel.

**Decision: retire PL-013 entirely.** Replace the requirement body with a normative-deletion stub:

> **PL-013 — Retired in extqueue v0.1.** The prior requirement covered the daemon's idle-wait behavior under the Beads-ready-poll dispatch model. Under [queue-model.md], the daemon's dispatch input is the in-memory queue; idle (queue absent, queue-completed, or queue-paused) is simply "no active group is advancing." The daemon MUST NOT exit on queue absence or queue completion; daemon exit occurs only on explicit `harmonik stop`, on an operator upgrade transition, or on crash (§PL-024). The previous re-query-cadence knob is removed from the config inventory per [operator-nfr.md §4.1 ON-004].

The "MUST NOT exit on idle" obligation survives — that is the load-bearing part of the original requirement. The Beads-poll mechanism is what is gone.

### PL-027(iii) — explicit non-regression

Add a one-line clarifier paragraph to PL-027(iii) to prevent the extqueue change from being read as touching exec-replace semantics:

> The queue methods of PL-003a (`queue-submit`, `queue-append`, `queue-status`, `queue-dry-run`) ride the same socket and inherit the listener-fd passing of this requirement unchanged. The in-memory queue is reconstructed post-exec via PL-005 step 8a's `.harmonik/queue.json` read (QM-002); the listener fd is preserved across `execve` exactly as for `claim-next` / `emit-outcome`.

### PL-004 — file surface inventory

Append `.harmonik/queue.json` to the file-surface enumeration at line 208, between `.harmonik/daemon.state` and the events-log entries:

> `.harmonik/queue.json` (per [queue-model.md §3 QM-001/QM-002/QM-003]; queue-manager-written via WM-026 atomic write, PL-read at PL-005 step 8a, unlinked on queue completion per QM-003).

### Exit code 17 reuse — decision

Research Q5 surfaced no dedicated `daemon-down` exit code. Three options were on the table:

1. **Reuse code 17** (current plan per `02-components.md §4`). Code 17's existing semantics (`multi-daemon-target-missing` — "a daemon-communicating command's `--socket` / `--cwd` / `--daemon-id` target cannot be resolved") fit `hk queue *` failing with `ECONNREFUSED`: the target socket exists in the filesystem but no listener is bound, which is structurally the same as "target cannot be resolved."
2. Allocate a new §8 code (e.g., 24 `daemon-not-running`). Cost: cross-spec amendment to operator-nfr §8 taxonomy, ON-041 listing, and N-1 compatibility envelope.
3. Update code 17's remediation prose to explicitly cover the single-daemon `ECONNREFUSED` case.

**Decision: option 1 + option 3 hybrid.** Reuse code 17 as-is; update its remediation prose in `operator-nfr.md §8` (cascade work — owned by operator-nfr design pass) to add: "When the failure is a single-project `hk queue *` invocation against `.harmonik/daemon.sock`, remediation is `harmonik daemon` (start the daemon for this project) rather than `harmonik list` — the multi-daemon disambiguation path applies only when `--cwd` / `--daemon-id` were provided."

PL itself does not own §8 prose; PL-008a is the consumption-by-reference site. No PL edit needed for the remediation text — the cascade is owned by operator-nfr. PL-028's new `hk queue` bullets carry the exit-code reference inline ("daemon-not-running → exit 17 per ON §8").

## Rationale

- **D5 (Unix socket reuse).** Queue methods ride `.harmonik/daemon.sock` unchanged. No second socket file, no second transport, no second auth surface (filesystem-permission auth per HC-044 is inherited). PL-003a is the single registry; PL-005 step 3a bind, PL-011 step 8 removal, PL-027(iii) exec-replace handoff all apply uniformly. The cost of a second socket would be: orphan-sweep extension (PL-006), exit-code allocation (`socket-bind-failed` already covers it, but multiplicity adds N-1 risk), and a permissions-discipline second copy. Reuse is structurally cheaper and matches operator muscle memory.
- **D6 (v0.1 minimal).** Four methods, no `remove`, `pause`, `resume`, `clear`. The append-only / submit-then-replace shape is sufficient for the orchestrator agent's first-cut workflow (see queue-model.md §8 pause-by-failure recovery: v0.1 path is restart + resubmit). Adding the other six methods later is a non-breaking PL-003a list extension; the wire surface is forward-compatible.

## Requirements traceability

| 02-components §4 requirement | PL amendment |
|---|---|
| New `hk queue` CLI subcommand family | PL-028 (four new bullets) + PL-028c (pre-flag.Parse dispatch) |
| Transport reuses existing daemon socket | PL-003 unchanged; PL-003a method-set extension only |
| PL-003a method-set extends with queue methods | PL-003a amendment (add `queue-submit` / `queue-append` / `queue-status` / `queue-dry-run`; remove `enqueue`) |
| `enqueue` operator command retired | PL-003a removal + PL-028 line-680 bullet removal |
| Daemon-not-running exit-code reuse (code 17) | PL-028 new bullets carry exit-17 reference; ON-side prose cascade noted (not PL-owned) |
| Socket bind/remove discipline inherited | PL-003 / PL-005 step 3a / PL-011 step 8 unchanged (explicit non-regression noted in §"PL-027(iii)") |
| Listener fd survives upgrade exec-replace | PL-027(iii) clarifier paragraph |
| Queue persistence read on startup | PL-005 step 8a amendment (third marker) |
| `.harmonik/queue.json` in daemon's file surface | PL-004 inventory amendment |
| Idle-wait behavior post-Beads-poll-removal | PL-013 retirement stub |
| Method-name convention (bare-kebab) | PL-003a vocabulary preserved (no dotted-method break) |

## Open for spec-draft pass (not blocking design)

- Exact JSON-RPC error-code allocation table `-32010..-32019` lives in queue-model.md §6; PL-003a references the block without enumerating.
- `queue-status` stdout shape (table-style like `harmonik list` per ON-041, or JSON per the queue-model.md §2 record) — defer to operator-nfr.
- Whether `hk queue submit` accepts queue body via stdin (`-`) in addition to a file path — defer to CLI-ergonomics review.
