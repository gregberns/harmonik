# Pi-in-a-sandbox — Task decomposition (beads)

> The v1 build order (HANDOFF.md §8) as concrete beads. All carry label `codename:pi-sandbox`
> (kerf `bead_filter: label=codename:pi-sandbox`). None are assigned — the daemon/captain staffs
> them later. The spike gates the rest.

| Bead | Title | Pri | Depends on |
|---|---|---|---|
| `hk-f39ny` | SPIKE: de-risk network + Go-CLI-TLS under srt (GATE) | P1 | — |
| `hk-p7smp` | Profile/settings generator: `internal/daemon/sandboxprofile.go` | P1 | `hk-f39ny` |
| `hk-rlxgx` | Argv-wrap srt in the substrate: `internal/daemon/tmuxsubstrate.go` | P1 | `hk-f39ny` |
| `hk-6596l` | `sandbox:` config block + threading (projectconfig/workloop/root) | P1 | `hk-f39ny` |
| `hk-i0377` | Acceptance scenario: commit-inside / write-to-main-denied / merges | P1 | `hk-p7smp`, `hk-rlxgx`, `hk-6596l` |
| `hk-ryi34` | Attachability check: `tmux attach` into sandboxed Pi | P2 | `hk-i0377` |
| `hk-5zviv` | Linux pass: literal-path bwrap settings + scenario on Linux node | P2 | `hk-i0377` |

## Dependency graph

```
hk-f39ny (spike, gate)
   ├──▶ hk-p7smp (generator) ─┐
   ├──▶ hk-rlxgx (argv wrap) ─┼──▶ hk-i0377 (acceptance scenario) ──▶ hk-ryi34 (attach)
   └──▶ hk-6596l (config)    ─┘                                    └──▶ hk-5zviv (Linux)
```

Only `hk-f39ny` is ready to dispatch now; the rest unblock as their dependencies close.

## Discipline notes

- No bead was set to `in_progress` and none was assigned — daemon owns terminal transitions; captain
  staffs.
- Beads are standalone tasks (no epic parent) to avoid the epic-dep-blocks-dispatch footgun.
- Priorities encode the dependency order (spike + core wiring P1; validation passes P2).
