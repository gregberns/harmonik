# Tmux reorg — SHARED CONTRACT (read before implementing any slice)

This is the agreed end-state. Every implementation slice must conform to these
exact names/semantics so the parallel worktrees compose without drift.

## End-state session/window layout

```
harmonik-<hash>-captain            (session)
  ├─ window "agent"                  the captain claude --remote-control pane
  └─ window "keeper"                 harmonik keeper --agent captain, targeting :agent

harmonik-<hash>-crew-<name>        (session)
  ├─ window "agent"                  the crew claude --remote-control pane
  └─ window "keeper"                 harmonik keeper --agent <name>, targeting :agent

harmonik-<hash>-default            (session, UNCHANGED)
  └─ windows <beadID>/i<n>, <beadID>/r<n>   implementer/reviewer agents

harmonik-<hash>-supervise          (session)  RENAMED from hk-<hash>-daemon-supervise
  └─ supervisor / auto-reaper

harmonik-<hash>-flywheel           (session, UNCHANGED) reaped by `supervise reap`
```

**One prefix family for the whole fleet: `harmonik-<hash>-<role>[-<name>]`.**
No more `hk-<hash>-*` or `hk-keeper-*`. `<hash>` = existing 12-hex project hash.

## Shared symbols / names (use these EXACTLY)

- Window names inside an agent session: **`"agent"`** (the LLM pane) and **`"keeper"`** (the watcher).
  - Go (daemon/lifecycle side): define `tmux.WindowAgent = "agent"` and
    `tmux.WindowKeeper = "keeper"` in `internal/lifecycle/tmux/windowname.go`;
    consumers import these.
  - Keeper package (depguard-isolated, may NOT import lifecycle): hardcode the
    string literals `"agent"`/`"keeper"` locally with a `// MUST match tmux.WindowAgent/WindowKeeper` comment (mirrors the existing tmuxresolve.go duplication pattern).
  - Shell (captain scripts): hardcode `agent` / `keeper`.
- Supervisor session name: `harmonik-<hash>-supervise` (drop `hk-` and `daemon-`).
- Crew session name: always `harmonik-<hash>-crew-<name>` — DELETE the legacy `hk-crew-<name>` path.

## Keeper inject-target contract (THE load-bearing shared change)

Today: `harmonik keeper --tmux <session>` injects `/clear` etc. into that
session's *active pane*. The keeper itself runs in a *separate session*.

New:
- The keeper RUNS in the `keeper` window of its agent's session.
- `--tmux` accepts a **`<session>:<window>` target** (e.g.
  `harmonik-<hash>-captain:agent`). The keeper injects into the **named window's
  active pane**, NOT the active pane of whatever window is focused. This prevents
  the keeper from ever pasting into its own window.
- Backward compatibility: if `--tmux` has no `:`, treat the whole value as the
  session and target its active pane (old behavior) — so a half-migrated fleet
  doesn't break.
- Keeper liveness/gauge resolution (`internal/keeper/tmuxresolve.go`) must resolve
  the AGENT window's pane for context-fill measurement, not the keeper's own.

## Restart granularity (preserves invariant I1: keeper survives restart)

- A captain/crew restart kills+recreates ONLY the `agent` window
  (`tmux respawn-window`/`kill-window` + `new-window`), NEVER the session.
- The `keeper` window persists across the agent's restart → keeper stays alive
  and re-binds to the freshly respawned `agent` window.
- Captain-launch and crew-spawn create the SESSION once (with both windows); the
  restart path operates at window granularity.

## Invariants (must still hold after the change)

- **I1** keeper survives the agent's `/clear` AND restart  → satisfied by window-granular restart + keeper in its own sibling window.
- **I2** keepers + crews survive daemon SIGTERM/restart     → satisfied: their sessions are independent of `-default`.
- **I3** the orphan reaper never kills a live agent          → `reapDeadCoordinatorSession` still targets ONLY `-flywheel`; do not broaden it to other prefixes.

## Slice ownership (file-disjoint — do NOT edit outside your set)

| Slice | Owner files | Depends on |
|-------|-------------|-----------|
| **N** naming | `internal/lifecycle/tmux/subcommand.go`, `internal/lifecycle/tmux/windowname.go` | — |
| **K** keeper contract | `cmd/harmonik/keeper*.go`, `internal/keeper/*` | — (hardcodes window strings) |
| **D** reap + supervisor refs | `cmd/harmonik/supervise*.go`, `internal/supervise/*`, `internal/daemon/orphansweep.go` | N (calls renamed `SupervisorSessionName`) |
| **B** captain nesting | `scripts/captain-tools/*`, `.claude/skills/captain/STARTUP.md` | N, K (runtime) |
| **C** crew nesting | `internal/daemon/crewstart.go`, `internal/daemon/tmuxsubstrate.go` | N, K (runtime) |

Land order: **N → K → D, B, C**. Develop N/K/D in parallel now; B/C off updated main.
