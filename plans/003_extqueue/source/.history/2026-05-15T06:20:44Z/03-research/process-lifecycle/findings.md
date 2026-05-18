# process-lifecycle — CLI + socket research findings

Patterns and evidence for adding `hk queue` CLI family routed over `.harmonik/daemon.sock`. Research-only.

## Findings (file:line)

### Q1 — Method-set naming convention

`specs/process-lifecycle.md:194` (PL-003a) enumerates JSON-RPC method names verbatim. Three groups, all **bare-kebab verbs** (no `noun.verb` namespacing):

- agent-facing: `claim-next`, `emit-outcome`, `dispatch-status`, plus Beads-CLI-skill proxy methods (BI-027).
- CLI-facing: `status`, `pause`, `resume`, `stop`, `upgrade`, `attach`, `enqueue`, `list` (per ON-041).
- daemon-internal / introspection: `get-agent-count` (reply `{count: integer ≥ 0}`).

PL-003a closes: "Method payload schemas are intentionally deferred; the names are the stable surface." Daemon dispatch matches — `internal/daemon/socket.go:245` switches on `case "emit-outcome":` / `case "claim-next":`; `internal/lifecycle/prereject_pl003b.go:29` lists `"claim-next"` / `"emit-outcome"` as the agent-method allowlist.

The proposed extqueue names in `02-components.md:67` (`queue.submit`, `queue.status`, …) would be the **first dotted methods on the wire** — existing surface is exclusively bare-kebab.

### Q2 — PL-028 CLI subcommand template

`specs/process-lifecycle.md:677` (PL-028) lists each subcommand as a one-line bullet: name, one-sentence purpose, flag list, cross-spec ownership pointer.

Two existing patterns to mirror:
- **`harmonik enqueue`** (line 680): "enqueue a bead via the socket (§PL-003, §PL-003a). Method: JSON-RPC `enqueue`; payload schema owned by [beads-integration.md §4.4]."
- **`harmonik status`** (line 681): "report daemon status over the socket. MUST report the §6.1 DaemonStatus enum value… Semantic content beyond the enum… owned by [operator-nfr.md §4.1 ON-002]."

PL-028 discipline: declare command-dispatch + socket routing only; defer semantics to operator-nfr / beads-integration / payload-owner spec.

Exit codes: PL-028 itself names none — codes come from `operator-nfr.md` §8 taxonomy (rows at lines ~776–805). `process-lifecycle.md:301` enumerates §8 codes PL consumes (5, 6, 7, 8, 9, 10, 14, 19, 22, 23). Per ON-001 (`operator-nfr.md:139`), every operator-invoked command MUST return a structured exit code; 0=success; non-zero maps 1:1 to a §8 category; stable across N-1 window.

Argument parsing pattern in `cmd/harmonik/main.go:70-103` (`tmux-start`) and `:106-115` (`hook-relay`): subcommand check happens **before** `flag.Parse` so global flags don't consume subcommand args; minimal hand-rolled flag scan (accepts both `--foo value` and `--foo=value`); subcommand returns its own exit-code int from a dedicated package (`tmux.RunTmuxStart`, `hookrelay.Run`).

Stdout/stderr shape: not normatively specified in PL-028 or §8. The one detailed example is `harmonik list` columns in ON-041 (`operator-nfr.md:532`). Otherwise PL-028 leaves stdout shape to the owning spec.

### Q3 — `harmonik enqueue`: **name-only, not built**

Evidence:
- `cmd/harmonik/main.go:51+` — `run()` dispatches `tmux-start` and `hook-relay` only; no `enqueue` case anywhere.
- `internal/daemon/socket.go:245-258` — server dispatch handles only `"emit-outcome"` and `"claim-next"`. Handler interface (`socket.go:93-104`) exposes `EmitOutcome` / `ClaimNext` only.
- `internal/core/operatorcommand.go:27` declares `OperatorCommandEnqueue = "enqueue"`; `internal/operatornfr/commandcodes.go:24` declares `CommandEnqueue` — both are **registry constants**, not handlers.
- `internal/lifecycle/clicommands_pl028_test.go:61,167` and `internal/lifecycle/prereject_pl003b_test.go:108` reference `"enqueue"` in spec-audit name-set tests.
- `internal/lifecycle/queueempty_pl013_test.go:154` is comment-only ("Simulate a subsequent harmonik enqueue").

extqueue can repurpose or retire the name without colliding with shipping behavior. (`02-components.md:67` already proposes this.)

### Q4 — Socket bind lifecycle

- **Create:** `specs/process-lifecycle.md:235` PL-005 step 3a — "Bind Unix socket at `.harmonik/daemon.sock` per PL-003. Begin accepting connections." After pidfile-lock acquisition (step 1), before Cat 0 pre-check (step 4).
- **Stale-file detection:** `specs/process-lifecycle.md:187` PL-003 — "The daemon MUST remove a stale socket file on startup before binding." (Pidfile-lock at step 1 is the real cross-instance guard per PL-INV-002 `specs/process-lifecycle.md:747`.)
- **Permissions:** `specs/process-lifecycle.md:187` PL-003 — `chmod(0600)`, owner = daemon's effective uid; filesystem-perm auth (HC-044). Restated `specs/process-lifecycle.md:112`.
- **Bind-failure exit:** §8 code 6 `socket-bind-failed` (`specs/operator-nfr.md` §8 row 6; `specs/process-lifecycle.md:301`).
- **Removal on shutdown:** `specs/process-lifecycle.md:386` PL-011 step 8 — "Release the pidfile lock AND remove the pidfile on clean shutdown… Remove the socket file." On crash, socket survives until next startup's PL-003 stale-removal.
- **Upgrade continuity:** `specs/process-lifecycle.md:658` PL-027(iii) — listener fd passed across `execve` (clear `FD_CLOEXEC`, env var `HARMONIK_LISTENER_FD`, `net.FileListener`); new binary skips `bind()`.

### Q5 — Daemon-not-running CLI behavior

- **Probe path:** `specs/process-lifecycle.md:344-352` PL-009b — socket probe; `ECONNREFUSED` means daemon not listening yet. Line 352: "External callers MUST NOT assume the daemon is ready simply because the pidfile or socket file exists."
- **Closest §8 category:** code 17 `multi-daemon-target-missing` (`specs/operator-nfr.md:800`) — "A daemon-communicating command's `--socket` / `--cwd` / `--daemon-id` target cannot be resolved." Remediation text points to `harmonik list`.
- **No dedicated "daemon-down" code in §8.** ON-041 (`specs/operator-nfr.md:530`) describes `kill(pid, 0)` + socket probe to discriminate `running` vs `stale`. PL-009b retry envelope (initial 100ms, max 2s, cap `T_ready_wait=60s`) applies when daemon is up-but-not-yet-ready, not when it's absent.
- `02-components.md:68` proposes reuse of existing taxonomy ("no new category needed in §8"). Code 17 is the available reuse.

### Q6 — JSON-RPC errors

- **Wire format:** `specs/process-lifecycle.md:194` PL-003a — JSON-RPC 2.0 over NDJSON per HC-007a (one JSON object per line, max 1 MiB).
- **Error-object shape** from `internal/lifecycle/prereject_pl003b.go:130-145`:
  `{"jsonrpc":"2.0","id":<id>,"error":{"code":<int>,"message":<string>}}`
- **Only allocated code:** `-32001` (`prereject_pl003b.go:111-115`) — `daemon_not_ready{"reason":"unknown_run_id"}`. Comment: "-32001 is in the implementation-defined error range per JSON-RPC 2.0 spec." Spec source PL-003b (`specs/process-lifecycle.md:201`).
- **Message convention:** typed-error string `<error_type>{"<key>":"<value>"}`; watcher matches on substring (`prereject_pl003b.go:117-122`).
- **No central JSON-RPC error-code registry exists.** PL-003b is the only allocation so far. New `queue.*` errors should pick numbers in `-32000..-32099` (JSON-RPC implementation-defined range) with typed-error strings per class.

## Patterns to adopt

- **Method names: bare-kebab-verb.** Existing wire surface is `claim-next`, `emit-outcome`, `get-agent-count`, `dispatch-status`. The dotted `queue.submit` proposal in `02-components.md:67` deviates. Either adopt `queue-submit` / `queue-append` / `queue-status` (consistent), or introduce dotted naming as a deliberate, documented break with rationale in queue-model.md.
- **PL-028 bullet template:** one line per subcommand naming (a) purpose, (b) JSON-RPC method, (c) payload-schema owning spec. Defer semantics; PL-028 only obligates command-dispatch + socket routing.
- **Exit codes:** allocate any new §8 codes in `operator-nfr.md` §8, not in PL. Reuse code 17 (`multi-daemon-target-missing`) for daemon-down per `02-components.md:68`; reuse code 6 (`socket-bind-failed`) for new bind hazards.
- **CLI plumbing in `cmd/harmonik/main.go`:** subcommand dispatched before `flag.Parse`; per-subcommand package owns its flags and returns an exit-code int. Mirror `tmux.RunTmuxStart` (`cmd/harmonik/main.go:102`).
- **JSON-RPC errors:** `{code, message}` object; code in `-32000..-32099`; message = typed string `<class>{"<key>":"<value>"}`; document next to each method.
- **ON-013a panic barrier:** `specs/operator-nfr.md:273` enumerates `pause, stop, upgrade, attach, enqueue`. Extend explicitly to `queue.*` per `02-components.md:98`.
- **Socket bind/remove discipline:** unchanged; queue methods inherit PL-003 / PL-005-step-3a / PL-011-step-8 lifecycle. No second socket.

## Risks / conflicts

- **Naming-convention break.** PL-003a's enumeration is normative bare-kebab; dotted `queue.*` adds a new vocabulary axis. Real spec decision, not cosmetic.
- **`enqueue` retire-vs-alias.** Name appears in PL-003a, ON-013a, ON-041, ON-050 (attach inline commands), `OperatorCommand` enum, exit-code allocation table, and PL-028b conformance tests. Retire path touches all six; alias is cheaper. No live wire handler, so neither breaks behavior.
- **No central JSON-RPC error-code registry.** Only `-32001` is allocated. Adding `queue.*` errors invites future collisions. Mitigation: queue-model.md claims a code block (e.g., `-32010..-32019`), or operator-nfr §8 grows a JSON-RPC error-code subtable.
- **Daemon-not-running has no dedicated code.** Code 17's remediation text names `harmonik list` and `--daemon-id` / `--cwd` flags. If `hk queue submit` lacks those flags, remediation pointer is misleading. Decide: extend code 17's prose, or allocate a new code.
- **`hk queue status` vs `harmonik status` nominal collision.** Disambiguated by the `queue` namespace word but worth a one-line note in PL-028.
- **PL-013 "queue-empty" wording.** `specs/process-lifecycle.md:410` PL-013 names "queue-empty" plus the "harmonik enqueue" wakeup — this is the *Beads* readiness-poll, not the extqueue execution queue. `02-components.md:100` retires the re-query-cadence knob; PL-013 prose needs reconciliation in the spec-draft pass.
