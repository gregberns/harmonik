# Component 2 — Research findings

Scope: build `internal/lifecycle/tmux` and `hk tmux-start` with locked decisions D2 (no ntm), D3 (reuse `$TMUX` if set; else refuse and direct to `hk tmux-start`), D4 (window names from `(bead_id, phase, iteration_count)`).

## 1. PTY surface options surveyed

Three Go-level approaches for getting a "live, attachable terminal experience" for a claude subprocess:

### (a) Tmux owns the pty; daemon never touches a pty fd

The daemon shells out `tmux new-window -d -t <session> -n <window> -c <cwd> <argv...>`. Tmux allocates the pty inside the tmux server process. The daemon's only relationship to the process is the tmux server as parent. No Go pty library needed. Cost: the daemon cannot read claude's stdout/stderr stream directly — that wire is owned by tmux. To observe the live byte stream the daemon issues `tmux pipe-pane -o -t <session>:<window> "cat >> <logfile>"` and tails the logfile. This is `pipe-pane` + tail.

Pros: zero new Go dependencies; the operator's `tmux attach`/`select-window` Just Works because tmux owns the pty; the daemon has no pty to leak; PL-006-style orphan recovery is a direct fit (kill the window via tmux).

Cons: stdout NDJSON observation requires the `pipe-pane → file → tail` indirection — adds disk I/O on the hot bridge path; pipe-pane's "lines" boundary is line-buffered terminal output, which interacts badly with claude's interactive rendering (ANSI escapes, partial lines, cursor moves). Worse, claude's terminal mode UI is mixed with the wire-protocol NDJSON the daemon needs to consume. The bridge currently expects the daemon to read NDJSON from a clean stdout pipe (`internal/handler/session.go:Stdout`), which is incompatible with sharing the pty with a TUI.

### (b) Daemon owns the pty via `creack/pty`; tmux attaches to it

The daemon opens a pty (`pty.Open()`), forks claude as a child with the slave fd as its stdin/stdout/stderr, then asks tmux to attach to the master fd. This conflates two ownership models and doesn't actually compose: tmux insists on owning the pty for any pane it manages.

Verdict: rejected on composition grounds.

### (c) Split the wire: tmux owns the interactive pty; bridge messages flow via the existing Unix socket

Claude's interactive UI runs in the tmux-owned pty. The bridge wire format (`handler_capabilities`, `agent_ready`, heartbeats, `outcome_emitted`) does NOT flow through the pty at all — it flows through the daemon's existing Unix socket via the `harmonik hook-relay` subcommand, which is already wired (CHB-010..017, see `cmd/harmonik/main.go:64–74`). The Stop hook from claude triggers `harmonik hook-relay Stop` inside the worktree; that subprocess opens the daemon socket and pushes the envelope.

This is the architecture the bridge spec already mandates (CHB-018..025). The daemon never needs to read claude's stdout. The pty is purely for operator ergonomics — `tmux attach -t harmonik-<hash>-<bead_id>` shows a live interactive session.

Verdict: **adopted**. Approach (a) with the corrected understanding that the daemon does not need pipe-pane output at all — the bridge wire is the socket, not stdout.

## 2. Tmux command surface needed

Minimum primitive set the adapter must wrap:

| Operation | Command |
|---|---|
| Probe presence | `tmux -V` (used at PL-021a-style Cat 0 startup check) |
| List sessions (already in orphansweep.go) | `tmux list-sessions -F '#{session_name}'` |
| Create session detached | `tmux new-session -d -s <name>` |
| List windows in session | `tmux list-windows -t <session> -F '#{window_name}'` |
| Create window detached, exec argv | `tmux new-window -d -t <session>: -n <window-name> -c <cwd> -e KEY=VAL ... -- <binary> <args...>` |
| Kill specific window | `tmux kill-window -t <session>:<window-name>` |
| Check `$TMUX` reuse | env lookup, not a tmux call |
| New session + exec hk inside (for `hk tmux-start`) | `tmux new-session -s <name> -c <cwd> -- <hk-binary> [args]` |
| Switch operator focus (optional, MAY) | `tmux select-window -t <session>:<window-name>` |

Per-env injection for `new-window` uses `-e KEY=VAL` (tmux 3.0+). This is how `HARMONIK_PROJECT_HASH`, `CLAUDE_*` env vars, and the worktree-relative `CLAUDE_SETTINGS_DIR` reach the child claude process. Per `tmux(1)`, `-e` may be repeated; argv after `--` becomes the window's command. The current minimum supported tmux version is 3.0 (matches existing `OSTmuxSessionKiller` assumptions); document in PL-021b.

## 3. Similar Go projects referenced

- **harpoon / ntm internals** (cited at PL-021/022) — uses `os/exec` shellouts directly, no pty lib. Confirms (a) is the idiom.
- **gotmux** (github.com/jubnzv/go-tmux) — typed wrapper but heavy; pulls in display-message parsing we don't need. Not adopted; cited as evidence the shellout pattern is the prevailing choice.
- **creack/pty** — relevant only if we go with approach (b), which we rejected.
- Internal precedent: `internal/lifecycle/orphansweep.go` already shells out via `exec.CommandContext`. The new adapter mirrors that exact pattern.

## 4. Risks

- **R1 — `-e` env-var injection on tmux <3.0.** Mitigation: PL-021b mandates tmux ≥3.0; Cat 0 startup probe asserts. Fail-fast with exit 22.
- **R2 — Window-name collision in a shared operator session (D3 reuse path).** Two beads with the same `bead_id` running concurrently are pathologically impossible (a bead is single-claim), but a replay or stale leftover window from a crashed hk could collide. Mitigation: window-name collision is detected at window-create time (`tmux list-windows`); on collision, the adapter kills the stale window first (PL-021c orphan recovery within sweep).
- **R3 — Session-name reconciliation in D3.** The operator's existing `$TMUX` session is not named `harmonik-<hash>-...`. The orphan sweep filters by that prefix (PL-006a). Resolution discussed in design — adopted: tag windows with a sentinel `hk-<hash6>-` prefix when running inside an operator session, so sweep can filter by `tmux list-windows` window-name patterns in addition to session-name patterns.
- **R4 — Pipe-pane temptation.** A future contributor will be tempted to add `pipe-pane` to "watch what claude is doing." This is a footgun (interleaves ANSI with NDJSON). Document explicitly in the design that the daemon MUST NOT read pane output; the bridge wire is the socket only.
- **R5 — `hk tmux-start` infinite recursion.** If `hk tmux-start` is run from inside an existing `$TMUX`, naive impl could create a nested session. Mitigation: subcommand refuses with exit non-zero and a directive message when `$TMUX` is non-empty.
- **R6 — Daemon-process re-exec under tmux.** `hk tmux-start` execs `hk` *inside* the new tmux session. That hk inherits `$TMUX` set by tmux, so it takes the D3 reuse path and creates handler windows in the just-created session. This is a feature (one binary, one path) but care is needed: the start subcommand must not call `daemon.Start` itself.

## 5. Cross-checks against existing code

- `internal/lifecycle/orphansweep.go:SweepOrphanTmuxSessions` filters sessions by `TmuxSessionPrefix(projectHash)`. In the D3 reuse path the operator's session won't match. The design resolves this by extending sweep to also kill orphan **windows** whose names match a project-hash-tagged pattern, via a new `WindowLister`/`WindowKiller` pair adjacent to the existing session pair.
- `internal/lifecycle/provenance.go:TmuxSessionName` is the create-side counterpart we will call from `EnsureSession`. No change to the function; reuse as-is.
- `internal/handler/handler.go:LaunchSpec` does not yet carry a substrate seam. The design adds an optional `Substrate` field; when nil, current behavior preserved (parallel to the existing optional `StdoutWrapper`).
- Spec `PL-028` already prescribes a four-step `harmonik runner` flow including tmux-session creation; `hk tmux-start` is a refinement/rename. The design proposes `hk tmux-start` as the operator-facing name and keeps `harmonik runner` semantics as the underlying contract.
