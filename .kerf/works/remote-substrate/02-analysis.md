# remote-substrate — Analyze pass (existing-code constraints)

> What the Phase-1 change must integrate with. Traceable to files (from RESEARCH-NOTES §R6).
> Feeds decompose (`03-components.md`) and the build beads (`07-tasks.md`).

## The seam we build on (already exists)

- **`handler.Substrate`** (`internal/handler/substrate.go:30`): `SpawnWindow(ctx,
  SubstrateSpawn{Cwd,Env,Argv}) (SubstrateSession, error)`. **Already transport-neutral** —
  `Cwd/Env/Argv` carry no localhost assumption. Injected into dispatch via
  `LaunchSpec.Substrate` (`handler.go:114`) and `deps.substrate` (`workloop.go`). A remote impl
  slots in here without touching the dispatch loop. **This is the load-bearing fact.**
- **Local-tmux impl:** `tmuxSubstrate` (`internal/daemon/tmuxsubstrate.go:101`) → `tmux.OSAdapter`
  shelling `exec.CommandContext(ctx,"tmux",…)` (`osadapter.go`): `new-window`, `display-message`
  (`#{pane_pid}`/`#{pane_id}`), `kill-window`, `new-session`, `load-buffer`/`paste-buffer`/`send-keys`.
  The cwd it passes is a local FS worktree path; env is forwarded verbatim.

## Coupling that a remote impl must satisfy (the work)

1. **Paste + liveness optional interfaces** (`pasteinject.go`): `pasteInjecter` (l.76),
   `enterSender` (l.118), `quitSender` (l.136), `paneLivenessChecker` (l.180),
   `paneOutputSizer` (l.154); plus raw local probes `hasChildProcess`/`hasAnyDirectChild`/
   `commandMatchesLiveAgent` via `pgrep -P`/`ps -o comm=` (l.253-294), and
   `tmuxSubstrateSession.Wait`'s local-PID poll (`tmuxsubstrate.go:36-58`, ESRCH/EPERM). A remote
   session must implement these **over the wire** (`ssh host -- tmux/pgrep/ps`). The PID returned
   by `display-message` is a **remote** PID — local `os.FindProcess`-style checks are meaningless
   remotely and must be replaced by remote probes.
2. **Worktree free functions** (`internal/workspace`): `CreateWorktree`/`WorktreePath`/
   `ReviewerWorktreePath` are hard-wired to local `git` + local FS (`.harmonik/worktrees/<run_id>/`).
   Remote needs these to run **on the worker** over SSH (seam C), or a shared FS (rejected per R8).
3. **Comms bus** (`socket.go:290`): unix-domain socket, `chmod 0600`, all clients dial `"unix"`,
   zero TCP/SSH in tree. **Out of v1 scope** (crews stay local, D3) — do NOT touch.
4. **Config loading:** follow the existing `branching.yaml` pattern (`yaml.Unmarshal`, read at
   daemon boot, CLI-flag override) for the new `workers.yaml`.

## Constraints the change must honor

- **NFR7 — no local regression:** with zero workers configured, every path must behave exactly
  as today. The local-tmux substrate stays the default; remote is additive selection.
- **Billing fail-closed (D2/NFR4):** the daemon must guarantee `ANTHROPIC_API_KEY` is absent from
  a remote session's spawn env (the worker's own login provides subscription auth).
- **Merge stays centralized on box A:** the one-at-a-time merge/skip-on-conflict flow is unchanged;
  a remote run only *produces a pushed branch*. Do NOT distribute merging.
- **Error/structured-event conventions:** failures surface as typed events in
  `.harmonik/events/events.jsonl` (e.g. a new `worker_unreachable`/`worker_unhealthy` event) and
  map onto the existing `run_stale` recovery, rather than ad-hoc logging.

## Testing convention — LOAD-BEARING for bead design (DEC-D)

- The daemon's per-bead build gate **skips `//go:build scenario` tests**, and scenario tests boot
  REAL daemons → they blow the 30-min commit budget (see project memory "Scenario-Test Authoring").
- ⇒ **Beads dispatched through the daemon must carry FAST unit/integration tests** that the gate
  runs — i.e. exercise the ssh-remote substrate with an **injected fake command-runner**
  (assert it emits the right `ssh host -- tmux …` / `git …` argv and parses output), NOT a real
  ssh round-trip or a daemon boot.
- The **real `ssh-to-localhost` end-to-end** validation (no second machine needed) is a separate
  **scenario test**, authored via a worktree sub-agent + fast gate + cherry-pick — it must NOT be
  the gating test inside a daemon-dispatched bead.
- This splits cleanly: a **`CommandRunner` interface** (local `exec` vs `ssh`) is the unit-test
  seam AND the production abstraction (likely DEC-A's core type).

## Relevant recent history

The substrate/crew-spawn/keeper paths were recently reworked (session-nesting fix `ff23633c`,
spawn-semaphore fixes hk-hzj/hk-yaj, pasteinject watchdogs hk-trjef/hk-5s7tg). The remote impl
must not regress the spawn-semaphore slot accounting or the pasteinject auto-recovery — reuse
them, don't fork them.
