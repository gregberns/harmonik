# DRAFT — Amendments to `specs/handler-contract.md`

Additive. Insert after referenced sections. Numbering reserves HC-054..HC-056; adjust at merge time if higher numbers are already taken.

## HC-054 — `Session.Attach()` contract refinement for claude-code

**Insert after §6.1 Session.Attach (existing prose).**

> **HC-054 — `Session.Attach()` for `agent_type=claude-code` returns a live tty stream.**
>
> For sessions where `agent_type == "claude-code"` running under the tmux-pane substrate (PL-021b), `Session.Attach()` MUST return an `io.Reader` that streams the live contents of the pane's pty — not a tail of a log file. The reader MUST remain open for the lifetime of the session; reads MAY block when no bytes are available. The reader MUST NOT buffer beyond a single line ahead, so an attached operator observes claude's TUI in real time.
>
> Closing the reader MUST NOT terminate the session; the session terminates only via cancellation of the enclosing context or invocation of `Session.Kill`.
>
> Multiple concurrent `Attach()` calls are permitted; each call returns an independent reader fed from the same underlying pty. Implementations MAY coalesce readers into a single tee.
>
> **Axes tags:** *mechanism*, *S04*, *additive*.
> **Cross-refs:** PL-021b (pane substrate), CHB-018 (pre-exec emission ordering — unaffected).

## HC-055 — Allowed `claude` CLI flags

**Insert as a new clause under §4 (handler-side responsibilities), parallel to the CHB-007 forbidden-flag list.**

> **HC-055 — Allowed `claude` CLI flags at MVH.**
>
> The daemon's claude-launch path MUST construct `argv` from exactly the following allow-list:
>
>   - `--session-id <uuid>` — passed when phase is `single`, `implementer-initial`, or `reviewer` (CHB-008).
>   - `--resume <uuid>` — passed when phase is `implementer-resume` (CHB-008).
>
> Operator-supplied additional arguments (`Config.HandlerArgs`) are appended after the allow-listed flags and forwarded verbatim. `Config.HandlerArgs` MUST be validated against the CHB-007 deny-list before exec.
>
> The following flags MUST NOT be passed at MVH (in addition to the CHB-007 deny-list):
>
>   - `--print` / `-p` — incompatible with interactive tmux substrate.
>   - `--add-dir` — workspace boundary is `cmd.Dir`; additional dirs defer to a follow-up bead.
>   - `--allowed-tools`, `--disallowed-tools` — tool policy lives in worktree-materialized `.claude/settings.json` (CHB-001..005); CLI overrides would silently shadow policy.
>   - `--mcp-server`, `--mcp-config` — out of scope. Follow-up bead.
>   - `--permission-mode` — same shadowing concern.
>
> A daemon that detects any of these flags in `Config.HandlerArgs` MUST refuse to launch with a structural error.
>
> **Axes tags:** *mechanism*, *S04*, *additive*, *security-relevant*.
> **Cross-refs:** CHB-006 (env), CHB-007 (forbidden flags), CHB-001..005 (settings.json materialization).

## HC-056 — `agent_ready` timeout

**Insert as a new clause under §4.9 (ready-state, near HC-041).**

> **HC-056 — `agent_ready` timeout.**
>
> The daemon MUST observe an `agent_ready` event from each launched session within `agent_ready_timeout` of process start. The default value is **30 seconds**; operators MAY tune via `Config.AgentReadyTimeout`.
>
> On timeout, the daemon MUST:
>   1. Cancel the session's context (which triggers `Session.Kill`).
>   2. Reap the subprocess via `Session.Wait`.
>   3. Emit `agent_failed{class=structural, sub_reason=agent_ready_timeout, exit_code=<observed>}`.
>   4. Reopen the bead with reason `agent_ready_timeout`.
>
> The 30 s default is informed by claude's observed cold-start latency (≤ 5 s typical, 10–15 s under cold disk caches); margin accommodates skill provisioning and one-time `.claude/` filesystem warm-up. Tighten in a follow-up bead once telemetry from real-claude smokes lands.
>
> The timeout MUST fire from the same goroutine that owns the session's lifecycle to ensure ordered Kill/Wait. Concurrent agent_ready arrival and timeout expiry race is resolved in favour of agent_ready (last-second arrival wins).
>
> **Axes tags:** *mechanism*, *S04*, *additive*, *closes hk-do7te*.
> **Cross-refs:** HC-041 (DetectReady), CHB-018 (emission ordering), CHB-020 (terminal-event mapping).

## HC-057 — Heartbeat-emission ownership for claude-code

**Insert near HC-041 or as continuation of CHB-019 cross-reference.**

> **HC-057 — Heartbeat-emission ownership for `claude-code` at MVH.**
>
> For `agent_type == "claude-code"`, the daemon MAY emit `agent_heartbeat{phase:"reasoning"}` events on the handler-process's behalf at the CHB-019 cadence (300 s). This is a permissive carve-out from CHB-019's "handler-process emits" language, justified by the absence of a distinct claude-handler wrapper binary at MVH. Subscribers MUST treat daemon-emitted heartbeats as semantically equivalent to handler-emitted heartbeats; no payload distinction is required.
>
> Post-MVH, when a `harmonik claude-handler` shim binary lands, heartbeat emission MUST migrate to the shim and this clause is retired.
>
> **Axes tags:** *mechanism*, *S04*, *additive*, *MVH-carve-out*.
> **Cross-refs:** CHB-019.
