# Research — restart-now tmux-target resolution (B4)

`restartnow.go:83-86`: `RunOnDemand` aborts with `no_tmux_target` whenever `cfg.TmuxTarget == ""`. The
field is populated one layer up, in `cmd/harmonik/keeper_restart_now_*.go`, via `tmuxresolve.go`'s target
derivation (the `:agent` convention). Live incident: a healthy watcher was already bound to a real pane,
but the resolution call returned empty — the abort fired anyway, stranding the admiral.

Fix shape: the resolution seam must be exercised against the real live-session naming (not a hand-passed
literal), and `RunOnDemand` must not abort when a pane is demonstrably live and bound — i.e. resolve
before checking, or widen the resolution fallback to cover the naming class that previously mangled
(hk-5266t, `:a`-style targets).

Test seam: `tmuxRunFn` package-var (fake `list-sessions` shape) for L-fake-tmux; `cmd/harmonik-twin-session`
for the full L-twin drive proving the actual ACK→`/clear`→resume sequence completes.
