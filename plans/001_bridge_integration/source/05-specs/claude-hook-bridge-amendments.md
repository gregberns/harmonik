# claude-hook-bridge.md amendments — tmux-substrate twin parity

**Resolution.** No CHB amendment is needed. The twin runs as a pipe-mode subprocess of the handler, NOT inside a tmux window of its own.

## Rationale

The CHB §4.8 twin-parity contract is wire-format parity: CHB-021 binds the twin to emit the same NDJSON sequence over the same Unix socket the relay would, and CHB-022 binds the daemon to be twin-blind (zero `if isTwin` branches, single `(run_id, claude_session_id)` routing key). Both invariants are stated at the daemon's session-watcher boundary — they say nothing about the OS-process surface (parentage, tty, tmux pane) under the handler.

Tmux substrate is a property of the handler's launch surface, owned by [process-lifecycle.md §4.4 PL-021/PL-021b] and (with this integration) [workspace-model.md §4.1 WM-002a]. The handler launches:

- **Real Claude.** Inside a tmux window named by WM-002a, because operator inspectability of the agent's tty is the whole point of the substrate (success criterion SC3).

- **Twin.** As a direct pipe-mode subprocess of the handler with no tty attached. The twin is a scenario-harness fixture; it has no interactive surface to inspect, no shell to attach to, and no user-facing artifact a tmux window would expose. Running it under tmux would (i) add a fixture-only code path the daemon would have to either ignore (violating WM-002a's "every agent process" scope) or special-case, (ii) introduce a tmux dependency into scenario tests that today run cleanly in CI containers without tmux, and (iii) gain nothing — the twin's wire output reaches the daemon over the same Unix socket either way, which is precisely what CHB-021 already binds.

The substrate-scope asymmetry between real-Claude and twin does NOT break CHB-022 because the daemon's twin-blindness invariant is scoped to the message-routing surface (the watcher acceptor), not to the launch surface. The handler — which owns the asymmetric launch — already carries scenario-vs-real branching at startup per [handler-contract.md §4.6 HC-026a] (the scripted-heartbeat carve-out); the tmux-vs-pipe launch choice rides on that same pre-existing branch.

Adding a CHB clause restating this would be a redundant cross-reference, not a new normative constraint.

**Therefore:** no CHB-028 is filed. The substrate decision is captured in WM-002a's scope language combined with HC-026a's existing scenario-harness carve-out, and CHB-021/022 stand unmodified.

If, post-MVH, an operator-inspectability requirement extends to twin runs, this decision MAY be revisited by filing CHB-028 and amending WM-002a's scope.

**Tags:** process-decision; no-normative-change.
